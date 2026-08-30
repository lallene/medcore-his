package patient_queue

import (
	"os"
	"strconv"
	"strings"
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/core/scheduling"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// EnvEarlyCheckinMinutes — earliest check-in = scheduled_at − N minutes (MEDCORE_TIMEZONE-aware wall compare via UTC instants).
const EnvEarlyCheckinMinutes = "MEDCORE_APPOINTMENT_EARLY_CHECKIN_MINUTES"

// DefaultEarlyCheckinMinutes when env unset.
const DefaultEarlyCheckinMinutes = 60

func earlyCheckinMinutes() int {
	v := strings.TrimSpace(os.Getenv(EnvEarlyCheckinMinutes))
	if v == "" {
		return DefaultEarlyCheckinMinutes
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return DefaultEarlyCheckinMinutes
	}
	return n
}

// assertCheckInTiming: reject only if now is before (scheduled_at − early window).
// Late arrival is allowed while status remains SCHEDULED (no auto no-show).
func assertCheckInTiming(scheduledAt, now time.Time) error {
	_ = scheduling.Location() // document timezone coupling; compare absolute instants
	earliest := scheduledAt.UTC().Add(-time.Duration(earlyCheckinMinutes()) * time.Minute)
	if now.UTC().Before(earliest) {
		return coreerrors.BadRequest("Check-in trop tôt pour ce rendez-vous")
	}
	return nil
}

func assertCheckInLifecycle(status string) error {
	switch status {
	case ApptScheduled, ApptArrived:
		return nil
	case ApptCheckedIn:
		return nil // handled as idempotent reuse
	case ApptInProgress:
		return coreerrors.Conflict("Check-in interdit: rendez-vous en cours")
	case ApptCompleted:
		return coreerrors.Conflict("Check-in interdit: rendez-vous terminé")
	case ApptCancelled:
		return coreerrors.Conflict("Check-in interdit: rendez-vous annulé")
	case ApptNoShow:
		return coreerrors.Conflict("Check-in interdit: rendez-vous en no-show")
	default:
		return coreerrors.Conflict("Check-in interdit pour ce statut")
	}
}

// validateAppointmentTicketLink checks immutable bidirectional check-in linkage.
// Does NOT inspect DoctorTakenBy (LOT 20 TakeDoctor) — only ExpectedDoctorID when appointment scheduled one.
// Persisted-state conflicts → 409 Conflict (MedCore convention for invariant violations of this class).
func validateAppointmentTicketLink(appt Appointment, ticket Ticket) error {
	if appt.QueueTicketID == nil {
		return coreerrors.Conflict("intégrité: rendez-vous sans queue_ticket_id")
	}
	if *appt.QueueTicketID != ticket.ID {
		return coreerrors.Conflict("intégrité: queue_ticket_id ne correspond pas au ticket")
	}
	if ticket.AppointmentID == nil {
		return coreerrors.Conflict("intégrité: ticket sans appointment_id")
	}
	if *ticket.AppointmentID != appt.ID {
		return coreerrors.Conflict("intégrité: ticket.appointment_id incohérent")
	}
	if ticket.PatientID != appt.PatientID {
		return coreerrors.Conflict("intégrité: ticket.patient_id incohérent")
	}
	if ticket.ServiceID != appt.ServiceID {
		return coreerrors.Conflict("intégrité: ticket.service_id incohérent")
	}
	// Immutable scheduled practitioner copied at check-in. DoctorTakenBy is operational and ignored.
	if appt.ExpectedDoctorID != nil {
		if ticket.ExpectedDoctorID == nil || *ticket.ExpectedDoctorID != *appt.ExpectedDoctorID {
			return coreerrors.Conflict("intégrité: expected_doctor_id incohérent")
		}
	}
	return nil
}

// validateCompletedCheckInReuse requires a finished check-in row pair before idempotent success.
// Orphan tickets (appointment still SCHEDULED / unlinked) → conflict, no auto-repair.
func validateCompletedCheckInReuse(appt Appointment, ticket Ticket) error {
	switch appt.Status {
	case ApptCheckedIn, ApptInProgress, ApptCompleted:
		// operational progression after check-in is legitimate
	default:
		return coreerrors.Conflict("intégrité: ticket lié à un rendez-vous non check-in (orphelin / incomplet)")
	}
	return validateAppointmentTicketLink(appt, ticket)
}

// CheckInAppointment creates exactly one WAITING_TRIAGE ticket linked to the appointment (LOT 23F).
// Returns (ticket, reused, error). reused=true → already checked in (HTTP 200).
func (s *Service) CheckInAppointment(appointmentID uint, r AppointmentCheckInRequest, a Access) (*Ticket, bool, error) {
	if !s.has(a, "queue.checkin") && !s.has(a, "*") {
		return nil, false, coreerrors.Forbidden("Permission check-in requise")
	}
	if a.UserID == 0 {
		return nil, false, coreerrors.Unauthorized("Utilisateur non authentifié")
	}
	if !r.IdentityConfirmed {
		return nil, false, coreerrors.BadRequest("Confirmation d'identité requise")
	}

	var out *Ticket
	var reused bool
	err := s.db.Transaction(func(tx *gorm.DB) error {
		// Lock order: appointment FOR UPDATE → patient advisory (compatible with 23E cancel/no-show/reschedule).
		var appt Appointment
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&appt, appointmentID).Error; err != nil {
			return coreerrors.NotFound("Rendez-vous")
		}
		if err := s.assertCanAccessService(appt.ServiceID, a); err != nil {
			return coreerrors.NotFound("Rendez-vous")
		}

		// Idempotent reuse: already linked → return existing ticket only if fully coherent.
		if appt.Status == ApptCheckedIn || appt.QueueTicketID != nil {
			if appt.QueueTicketID == nil {
				return coreerrors.Conflict("intégrité: CHECKED_IN sans ticket")
			}
			var existing Ticket
			if err := tx.First(&existing, *appt.QueueTicketID).Error; err != nil {
				return coreerrors.Conflict("intégrité: ticket de check-in introuvable")
			}
			if err := validateCompletedCheckInReuse(appt, existing); err != nil {
				return err
			}
			out = &existing
			reused = true
			return nil
		}

		if err := assertCheckInLifecycle(appt.Status); err != nil {
			return err
		}
		now := time.Now().UTC()
		if err := assertCheckInTiming(appt.ScheduledAt, now); err != nil {
			return err
		}

		if err := s.advisoryLockPatient(tx, appt.PatientID); err != nil {
			return err
		}

		var active int64
		if err := tx.Model(&Ticket{}).Where("patient_id=? AND status=?", appt.PatientID, StatusActive).Count(&active).Error; err != nil {
			return coreerrors.Internal(err.Error())
		}
		if active > 0 {
			return coreerrors.Conflict("Le patient a déjà un parcours actif")
		}

		fin, err := s.EvaluateFinance(appt.PatientID)
		if err != nil {
			return err
		}
		if (fin == FinancePaymentRequired || fin == FinanceBlocked) && !r.FinanceOverride {
			return coreerrors.Conflict("Paiement requis avant check-in (finance=" + fin + ")")
		}
		if r.FinanceOverride && !s.has(a, "queue.checkin") && !s.has(a, "*") {
			return coreerrors.Forbidden("Override finance non autorisé")
		}

		prio := r.Priority
		if prio == "" {
			prio = PriorityNormal
		}
		if PriorityRank(prio) == 99 {
			return coreerrors.BadRequest("Priorité invalide")
		}
		ref, err := s.nextReference(tx)
		if err != nil {
			return coreerrors.Internal(err.Error())
		}

		arrived := now
		if appt.ArrivedAt != nil {
			arrived = *appt.ArrivedAt
		} else {
			appt.ArrivedAt = &now
		}
		// Do NOT persist transient ARRIVED — go straight to CHECKED_IN.

		fromApptStatus := appt.Status
		t := Ticket{
			Reference:           ref,
			PatientID:           appt.PatientID, // appointment authoritative
			AppointmentID:       &appt.ID,
			Source:              SourceAppointment,
			ServiceID:           appt.ServiceID, // appointment authoritative
			ExpectedDoctorID:    appt.ExpectedDoctorID,
			ArrivedAt:           arrived,
			CheckedInAt:         now,
			Stage:               StageWaitingTriage,
			Status:              StatusActive,
			Priority:            prio,
			FinanceStatus:       fin,
			FinanceOverride:     r.FinanceOverride,
			FinanceOverrideNote: r.FinanceOverrideNote,
			IdentityConfirmed:   true,
			Version:             1,
			CreatedBy:           a.UserID,
			CreatedAt:           now,
			UpdatedAt:           now,
		}
		if err := tx.Create(&t).Error; err != nil {
			msg := strings.ToLower(err.Error())
			if strings.Contains(msg, "ux_pq_tickets_appointment") || strings.Contains(msg, "unique") || strings.Contains(msg, "duplicate") {
				// Unique index hit: only soft-success if appointment already fully checked in to that ticket.
				var existing Ticket
				if e := tx.Where("appointment_id = ?", appt.ID).First(&existing).Error; e != nil {
					return coreerrors.Conflict("Check-in concurrent en conflit")
				}
				// Re-read appointment under same FOR UPDATE for post-commit concurrent winner state.
				if e := tx.First(&appt, appointmentID).Error; e != nil {
					return coreerrors.Internal(e.Error())
				}
				if err := validateCompletedCheckInReuse(appt, existing); err != nil {
					return err
				}
				out = &existing
				reused = true
				return nil
			}
			return coreerrors.Internal(err.Error())
		}

		appt.Status = ApptCheckedIn
		appt.CheckedInAt = &now
		appt.QueueTicketID = &t.ID
		appt.UpdatedAt = now
		if err := tx.Save(&appt).Error; err != nil {
			return coreerrors.Internal(err.Error())
		}
		payload := `{"ticketId":` + strconv.FormatUint(uint64(t.ID), 10) + `}`
		if err := s.writeAppointmentHistory(tx, appt.ID, a.UserID, ApptHistCheckedIn, fromApptStatus, ApptCheckedIn, "check-in", payload); err != nil {
			return err
		}
		if err := s.writeHistory(tx, t.ID, a.UserID, StageReception, StageWaitingTriage, "CHECK_IN", "appointment"); err != nil {
			return err
		}
		if r.FinanceOverride {
			_ = s.writeHistory(tx, t.ID, a.UserID, StageReception, StageWaitingTriage, "FINANCE_OVERRIDE", r.FinanceOverrideNote)
		}
		out = &t
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return out, reused, nil
}
