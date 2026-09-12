package patient_queue

import (
	"errors"
	"strings"
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// EnqueueNotificationIntentInput creates or returns an existing durable intent (idempotent).
type EnqueueNotificationIntentInput struct {
	AppointmentID uint
	PatientID     uint
	Kind          string
	Channel       string
	OccurrenceKey string
	SendAfter     time.Time
	PayloadJSON   string
}

// EnqueueNotificationIntent inserts a PENDING intent. Duplicate unique key returns the existing row
// (idempotent success) — never duplicates.
func (s *Service) EnqueueNotificationIntent(in EnqueueNotificationIntentInput) (*AppointmentNotificationIntent, error) {
	return s.enqueueNotificationIntentTx(s.db, in)
}

func (s *Service) enqueueNotificationIntentTx(tx *gorm.DB, in EnqueueNotificationIntentInput) (*AppointmentNotificationIntent, error) {
	if in.AppointmentID == 0 || in.PatientID == 0 {
		return nil, coreerrors.BadRequest("appointmentId et patientId requis")
	}
	if err := ValidateNotificationKind(in.Kind); err != nil {
		return nil, err
	}
	if err := ValidateNotificationChannel(in.Channel); err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.OccurrenceKey) == "" {
		return nil, coreerrors.BadRequest("occurrenceKey requis")
	}
	if in.SendAfter.IsZero() {
		return nil, coreerrors.BadRequest("sendAfter requis")
	}
	if _, err := ValidatePersistedNotificationPayload(in.PayloadJSON, in.AppointmentID); err != nil {
		return nil, err
	}
	payload := strings.TrimSpace(in.PayloadJSON)
	now := time.Now().UTC()
	row := AppointmentNotificationIntent{
		AppointmentID: in.AppointmentID,
		PatientID:     in.PatientID,
		Kind:          in.Kind,
		Channel:       in.Channel,
		OccurrenceKey: in.OccurrenceKey,
		SendAfter:     in.SendAfter.UTC(),
		Status:        NotifStatusPending,
		PayloadJSON:   payload,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	err := tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "appointment_id"},
			{Name: "kind"},
			{Name: "channel"},
			{Name: "occurrence_key"},
		},
		DoNothing: true,
	}).Create(&row).Error
	if err != nil {
		return nil, coreerrors.Internal("échec enqueue notification: " + err.Error())
	}
	if row.ID != 0 {
		return &row, nil
	}
	var existing AppointmentNotificationIntent
	if e := tx.Where(
		"appointment_id = ? AND kind = ? AND channel = ? AND occurrence_key = ?",
		in.AppointmentID, in.Kind, in.Channel, in.OccurrenceKey,
	).First(&existing).Error; e != nil {
		return nil, coreerrors.Internal("échec lecture intent existant: " + e.Error())
	}
	return &existing, nil
}

// FindNotificationIntent loads by primary key.
func (s *Service) FindNotificationIntent(id uint) (*AppointmentNotificationIntent, error) {
	if id == 0 {
		return nil, coreerrors.BadRequest("identifiant invalide")
	}
	var row AppointmentNotificationIntent
	if err := s.db.First(&row, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, coreerrors.NotFound("NotificationIntent")
		}
		return nil, coreerrors.Internal(err.Error())
	}
	return &row, nil
}

// ListPendingDue returns PENDING intents with send_after <= asOf (UTC), ordered by send_after.
func (s *Service) ListPendingDue(asOf time.Time, limit int) ([]AppointmentNotificationIntent, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	var rows []AppointmentNotificationIntent
	err := s.db.Where("status = ? AND send_after <= ?", NotifStatusPending, asOf.UTC()).
		Order("send_after ASC, id ASC").
		Limit(limit).
		Find(&rows).Error
	if err != nil {
		return nil, coreerrors.Internal(err.Error())
	}
	return rows, nil
}

// CancelPendingForAppointment cancels all PENDING intents for an appointment.
func (s *Service) CancelPendingForAppointment(appointmentID uint) (int64, error) {
	if appointmentID == 0 {
		return 0, coreerrors.BadRequest("appointmentId requis")
	}
	now := time.Now().UTC()
	res := s.db.Model(&AppointmentNotificationIntent{}).
		Where("appointment_id = ? AND status = ?", appointmentID, NotifStatusPending).
		Updates(map[string]any{
			"status":       NotifStatusCancelled,
			"cancelled_at": now,
			"updated_at":   now,
		})
	if res.Error != nil {
		return 0, coreerrors.Internal(res.Error.Error())
	}
	return res.RowsAffected, nil
}

func (s *Service) transitionIntent(id uint, from, to string, extra map[string]any) (*AppointmentNotificationIntent, error) {
	if err := AssertNotificationTransition(from, to); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	updates := map[string]any{
		"status":     to,
		"updated_at": now,
	}
	for k, v := range extra {
		updates[k] = v
	}
	res := s.db.Model(&AppointmentNotificationIntent{}).
		Where("id = ? AND status = ?", id, from).
		Updates(updates)
	if res.Error != nil {
		return nil, coreerrors.Internal(res.Error.Error())
	}
	if res.RowsAffected == 0 {
		cur, err := s.FindNotificationIntent(id)
		if err != nil {
			return nil, err
		}
		return nil, coreerrors.Conflict("transition notification concurrente ou invalide (statut=" + cur.Status + ")")
	}
	return s.FindNotificationIntent(id)
}

// MarkNotificationProcessing: PENDING → PROCESSING.
func (s *Service) MarkNotificationProcessing(id uint) (*AppointmentNotificationIntent, error) {
	return s.transitionIntent(id, NotifStatusPending, NotifStatusProcessing, nil)
}

// MarkNotificationSent: PROCESSING → SENT.
func (s *Service) MarkNotificationSent(id uint) (*AppointmentNotificationIntent, error) {
	now := time.Now().UTC()
	return s.transitionIntent(id, NotifStatusProcessing, NotifStatusSent, map[string]any{"sent_at": now})
}

// MarkNotificationFailed: PROCESSING → FAILED.
func (s *Service) MarkNotificationFailed(id uint) (*AppointmentNotificationIntent, error) {
	return s.transitionIntent(id, NotifStatusProcessing, NotifStatusFailed, nil)
}

// MarkNotificationSkipped: PENDING → SKIPPED.
func (s *Service) MarkNotificationSkipped(id uint) (*AppointmentNotificationIntent, error) {
	return s.transitionIntent(id, NotifStatusPending, NotifStatusSkipped, nil)
}

// MarkNotificationPendingRetry: PROCESSING → PENDING (worker retry reclaim).
func (s *Service) MarkNotificationPendingRetry(id uint) (*AppointmentNotificationIntent, error) {
	return s.transitionIntent(id, NotifStatusProcessing, NotifStatusPending, nil)
}

// RecordNotificationAttempt appends an attempt with monotonic attempt_no (unique per intent).
// Serializes writers per intent via SELECT … FOR UPDATE on the parent intent row.
// Does not store message bodies or patient contact PHI.
func (s *Service) RecordNotificationAttempt(intentID uint, provider string, providerMessageID, errMsg *string) (*AppointmentNotificationAttempt, error) {
	if intentID == 0 {
		return nil, coreerrors.BadRequest("intentId requis")
	}
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return nil, coreerrors.BadRequest("provider requis")
	}
	var attempt AppointmentNotificationAttempt
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var parent AppointmentNotificationIntent
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&parent, intentID).Error; e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return coreerrors.NotFound("NotificationIntent")
			}
			return coreerrors.Internal(e.Error())
		}
		var maxNo int
		if e := tx.Model(&AppointmentNotificationAttempt{}).
			Where("intent_id = ?", parent.ID).
			Select("COALESCE(MAX(attempt_no), 0)").
			Scan(&maxNo).Error; e != nil {
			return coreerrors.Internal(e.Error())
		}
		now := time.Now().UTC()
		attempt = AppointmentNotificationAttempt{
			IntentID:          parent.ID,
			AttemptNo:         maxNo + 1,
			Provider:          provider,
			ProviderMessageID: providerMessageID,
			Error:             truncatePtr(errMsg, 500),
			CreatedAt:         now,
		}
		if e := tx.Create(&attempt).Error; e != nil {
			return coreerrors.Internal("échec enregistrement tentative: " + e.Error())
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &attempt, nil
}

func truncatePtr(s *string, max int) *string {
	if s == nil {
		return nil
	}
	v := *s
	if len(v) > max {
		v = v[:max]
	}
	return &v
}
