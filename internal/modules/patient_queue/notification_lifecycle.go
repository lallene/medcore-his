package patient_queue

import (
	"strings"
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// appointmentAllowsBookNotificationRepair reports whether BookAppointment notification
// side effects may run (fresh create or idempotent repair). Terminal / non-active
// statuses are a no-op so idempotent replay cannot rearm reminders after cancel/no-show.
func appointmentAllowsBookNotificationRepair(status string) bool {
	return status == ApptScheduled
}

// applyBookNotificationIntentsTx enqueues BOOKED + optional REMINDER_T24H (LOG only) inside the book TX.
// Idempotent reuse of an active SCHEDULED appointment may repair missing intents.
// Replay against CANCELLED / NO_SHOW / COMPLETED / other non-SCHEDULED rows is a no-op
// (does not rearm CANCELLED reminders or fabricate a new BOOKED lifecycle intent).
func (s *Service) applyBookNotificationIntentsTx(tx *gorm.DB, appt Appointment, now time.Time) error {
	if !appointmentAllowsBookNotificationRepair(appt.Status) {
		return nil
	}
	if _, err := s.enqueueLifecycleLogTx(tx, appt, NotifKindBooked, now); err != nil {
		return err
	}
	return s.ensureReminderT24HLogTx(tx, appt, now)
}

// applyRescheduleNotificationIntentsTx suppresses the old-occurrence reminder, enqueues RESCHEDULED,
// and ensures a reminder for the new scheduled instant (rearm if same-key CANCELLED).
func (s *Service) applyRescheduleNotificationIntentsTx(tx *gorm.DB, oldScheduledAt time.Time, appt Appointment, now time.Time) error {
	oldKey := OccurrenceKeyFromScheduledAt(oldScheduledAt)
	if _, err := s.suppressActiveReminderIntentsTx(tx, appt.ID, oldKey); err != nil {
		return err
	}
	if _, err := s.enqueueLifecycleLogTx(tx, appt, NotifKindRescheduled, now); err != nil {
		return err
	}
	return s.ensureReminderT24HLogTx(tx, appt, now)
}

// applyCancelNotificationIntentsTx suppresses active reminders and enqueues CANCELLED (LOG).
func (s *Service) applyCancelNotificationIntentsTx(tx *gorm.DB, appt Appointment, now time.Time) error {
	if _, err := s.suppressActiveReminderIntentsTx(tx, appt.ID, ""); err != nil {
		return err
	}
	_, err := s.enqueueLifecycleLogTx(tx, appt, NotifKindCancelled, now)
	return err
}

// applyNoShowNotificationSuppressTx suppresses active reminders only (no CANCELLED lifecycle intent).
func (s *Service) applyNoShowNotificationSuppressTx(tx *gorm.DB, appointmentID uint) error {
	_, err := s.suppressActiveReminderIntentsTx(tx, appointmentID, "")
	return err
}

func (s *Service) enqueueLifecycleLogTx(tx *gorm.DB, appt Appointment, kind string, now time.Time) (*AppointmentNotificationIntent, error) {
	_, payload, err := BuildNotificationPayload(appt.ID, appt.ScheduledAt, "", "", "")
	if err != nil {
		return nil, err
	}
	sendAfter := now.UTC()
	if sendAfter.IsZero() {
		sendAfter = time.Now().UTC()
	}
	return s.enqueueNotificationIntentTx(tx, EnqueueNotificationIntentInput{
		AppointmentID: appt.ID,
		PatientID:     appt.PatientID,
		Kind:          kind,
		Channel:       NotifChannelLog,
		OccurrenceKey: OccurrenceKeyFromScheduledAt(appt.ScheduledAt),
		SendAfter:     sendAfter,
		PayloadJSON:   payload,
	})
}

// ensureReminderT24HLogTx enqueues or rearms REMINDER_T24H/LOG when eligible.
// Does not reactivate SENT/FAILED/SKIPPED. Explicit CANCELLED → PENDING rearm only.
func (s *Service) ensureReminderT24HLogTx(tx *gorm.DB, appt Appointment, now time.Time) error {
	if !ReminderT24HEligible(appt.ScheduledAt, now) {
		return nil
	}
	_, payload, err := BuildNotificationPayload(appt.ID, appt.ScheduledAt, "", "", "")
	if err != nil {
		return err
	}
	key := OccurrenceKeyFromScheduledAt(appt.ScheduledAt)
	sendAfter := ReminderSendAfterT24H(appt.ScheduledAt)
	row, err := s.enqueueNotificationIntentTx(tx, EnqueueNotificationIntentInput{
		AppointmentID: appt.ID,
		PatientID:     appt.PatientID,
		Kind:          NotifKindReminderT24H,
		Channel:       NotifChannelLog,
		OccurrenceKey: key,
		SendAfter:     sendAfter,
		PayloadJSON:   payload,
	})
	if err != nil {
		return err
	}
	switch row.Status {
	case NotifStatusPending, NotifStatusProcessing:
		return nil
	case NotifStatusCancelled:
		_, err = s.rearmCancelledReminderTx(tx, appt.ID, key, sendAfter, payload)
		return err
	case NotifStatusSent, NotifStatusFailed, NotifStatusSkipped:
		// Terminal — do not silently reactivate.
		return nil
	default:
		return coreerrors.Internal("statut reminder inattendu: " + row.Status)
	}
}

// suppressActiveReminderIntentsTx cancels PENDING/PROCESSING REMINDER_T24H LOG intents.
// If occurrenceKey is non-empty, only that occurrence is suppressed; otherwise all for the appointment.
func (s *Service) suppressActiveReminderIntentsTx(tx *gorm.DB, appointmentID uint, occurrenceKey string) (int64, error) {
	if appointmentID == 0 {
		return 0, coreerrors.BadRequest("appointmentId requis")
	}
	now := time.Now().UTC()
	q := tx.Model(&AppointmentNotificationIntent{}).
		Where("appointment_id = ? AND kind = ? AND channel = ? AND status IN ?",
			appointmentID, NotifKindReminderT24H, NotifChannelLog,
			[]string{NotifStatusPending, NotifStatusProcessing})
	if strings.TrimSpace(occurrenceKey) != "" {
		q = q.Where("occurrence_key = ?", occurrenceKey)
	}
	res := q.Updates(map[string]any{
		"status":                NotifStatusCancelled,
		"cancelled_at":          now,
		"updated_at":            now,
		"processing_started_at": nil,
	})
	if res.Error != nil {
		return 0, coreerrors.Internal(res.Error.Error())
	}
	return res.RowsAffected, nil
}

// rearmCancelledReminderTx explicitly reactivates CANCELLED → PENDING for REMINDER_T24H/LOG
// with the exact occurrence key. Does not reopen other kinds or terminal SENT/FAILED/SKIPPED.
func (s *Service) rearmCancelledReminderTx(
	tx *gorm.DB,
	appointmentID uint,
	occurrenceKey string,
	sendAfter time.Time,
	payloadJSON string,
) (*AppointmentNotificationIntent, error) {
	if appointmentID == 0 || strings.TrimSpace(occurrenceKey) == "" {
		return nil, coreerrors.BadRequest("appointmentId et occurrenceKey requis")
	}
	if sendAfter.IsZero() {
		return nil, coreerrors.BadRequest("sendAfter requis")
	}
	if _, err := ValidatePersistedNotificationPayload(payloadJSON, appointmentID); err != nil {
		return nil, err
	}
	var row AppointmentNotificationIntent
	if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("appointment_id = ? AND kind = ? AND channel = ? AND occurrence_key = ?",
			appointmentID, NotifKindReminderT24H, NotifChannelLog, occurrenceKey).
		First(&row).Error; e != nil {
		if e == gorm.ErrRecordNotFound {
			return nil, coreerrors.NotFound("NotificationIntent")
		}
		return nil, coreerrors.Internal(e.Error())
	}
	if row.Status != NotifStatusCancelled {
		return nil, coreerrors.Conflict("rearm reminder réservé au statut CANCELLED (statut=" + row.Status + ")")
	}
	now := time.Now().UTC()
	res := tx.Model(&AppointmentNotificationIntent{}).
		Where("id = ? AND status = ?", row.ID, NotifStatusCancelled).
		Updates(map[string]any{
			"status":                NotifStatusPending,
			"send_after":            sendAfter.UTC(),
			"payload_json":          strings.TrimSpace(payloadJSON),
			"cancelled_at":          nil,
			"sent_at":               nil,
			"processing_started_at": nil,
			"updated_at":            now,
		})
	if res.Error != nil {
		return nil, coreerrors.Internal(res.Error.Error())
	}
	if res.RowsAffected == 0 {
		return nil, coreerrors.Conflict("rearm reminder concurrent")
	}
	var out AppointmentNotificationIntent
	if e := tx.First(&out, row.ID).Error; e != nil {
		return nil, coreerrors.Internal(e.Error())
	}
	return &out, nil
}

// RearmCancelledReminder is the public non-TX helper for tests/tools (dedicated rearm path).
func (s *Service) RearmCancelledReminder(appointmentID uint, occurrenceKey string, sendAfter time.Time, payloadJSON string) (*AppointmentNotificationIntent, error) {
	var out *AppointmentNotificationIntent
	err := s.db.Transaction(func(tx *gorm.DB) error {
		row, e := s.rearmCancelledReminderTx(tx, appointmentID, occurrenceKey, sendAfter, payloadJSON)
		if e != nil {
			return e
		}
		out = row
		return nil
	})
	return out, err
}
