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

// CancelPendingForAppointment cancels all PENDING intents for an appointment (legacy helper).
func (s *Service) CancelPendingForAppointment(appointmentID uint) (int64, error) {
	if appointmentID == 0 {
		return 0, coreerrors.BadRequest("appointmentId requis")
	}
	now := time.Now().UTC()
	res := s.db.Model(&AppointmentNotificationIntent{}).
		Where("appointment_id = ? AND status = ?", appointmentID, NotifStatusPending).
		Updates(map[string]any{
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

func (s *Service) transitionIntent(id uint, from, to string, extra map[string]any) (*AppointmentNotificationIntent, error) {
	if err := AssertNotificationTransition(from, to); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	updates := map[string]any{
		"status":     to,
		"updated_at": now,
	}
	// Clear lease when leaving PROCESSING (or never set when entering non-processing).
	if from == NotifStatusProcessing || to != NotifStatusProcessing {
		if to != NotifStatusProcessing {
			updates["processing_started_at"] = nil
		}
	}
	if to == NotifStatusProcessing {
		updates["processing_started_at"] = now
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

// MarkNotificationProcessing: PENDING → PROCESSING (+ lease).
func (s *Service) MarkNotificationProcessing(id uint) (*AppointmentNotificationIntent, error) {
	now := time.Now().UTC()
	return s.transitionIntent(id, NotifStatusPending, NotifStatusProcessing, map[string]any{
		"processing_started_at": now,
	})
}

// MarkNotificationSent: PROCESSING → SENT.
func (s *Service) MarkNotificationSent(id uint) (*AppointmentNotificationIntent, error) {
	now := time.Now().UTC()
	return s.transitionIntent(id, NotifStatusProcessing, NotifStatusSent, map[string]any{
		"sent_at":               now,
		"processing_started_at": nil,
	})
}

// MarkNotificationFailed: PROCESSING → FAILED.
func (s *Service) MarkNotificationFailed(id uint) (*AppointmentNotificationIntent, error) {
	return s.transitionIntent(id, NotifStatusProcessing, NotifStatusFailed, map[string]any{
		"processing_started_at": nil,
	})
}

// MarkNotificationSkipped: PENDING → SKIPPED (pre-claim skip).
func (s *Service) MarkNotificationSkipped(id uint) (*AppointmentNotificationIntent, error) {
	return s.transitionIntent(id, NotifStatusPending, NotifStatusSkipped, map[string]any{
		"processing_started_at": nil,
	})
}

// MarkNotificationSkippedFromProcessing: PROCESSING → SKIPPED (post-claim pre-send guard / Noop).
func (s *Service) MarkNotificationSkippedFromProcessing(id uint) (*AppointmentNotificationIntent, error) {
	return s.transitionIntent(id, NotifStatusProcessing, NotifStatusSkipped, map[string]any{
		"processing_started_at": nil,
	})
}

// MarkNotificationPendingRetry: PROCESSING → PENDING (worker retry reclaim).
func (s *Service) MarkNotificationPendingRetry(id uint, sendAfter time.Time) (*AppointmentNotificationIntent, error) {
	if sendAfter.IsZero() {
		return nil, coreerrors.BadRequest("sendAfter requis")
	}
	return s.transitionIntent(id, NotifStatusProcessing, NotifStatusPending, map[string]any{
		"send_after":            sendAfter.UTC(),
		"processing_started_at": nil,
	})
}

// ClaimDueNotificationIntents atomically claims due PENDING LOG intents (FOR UPDATE SKIP LOCKED).
// Does not hold the transaction open during adapter I/O — returns claimed rows already PROCESSING.
func (s *Service) ClaimDueNotificationIntents(asOf time.Time, batch int) ([]AppointmentNotificationIntent, error) {
	if batch <= 0 {
		batch = NotificationClaimBatchDefault
	}
	if batch > NotificationClaimBatchMax {
		batch = NotificationClaimBatchMax
	}
	var claimed []AppointmentNotificationIntent
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var ids []uint
		if e := tx.Raw(`
			SELECT id FROM appointment_notification_intents
			WHERE status = ? AND send_after <= ? AND channel = ?
			ORDER BY send_after ASC, id ASC
			LIMIT ?
			FOR UPDATE SKIP LOCKED
		`, NotifStatusPending, asOf.UTC(), NotifChannelLog, batch).Scan(&ids).Error; e != nil {
			return coreerrors.Internal(e.Error())
		}
		if len(ids) == 0 {
			return nil
		}
		now := time.Now().UTC()
		res := tx.Model(&AppointmentNotificationIntent{}).
			Where("id IN ? AND status = ?", ids, NotifStatusPending).
			Updates(map[string]any{
				"status":                NotifStatusProcessing,
				"processing_started_at": now,
				"updated_at":            now,
			})
		if res.Error != nil {
			return coreerrors.Internal(res.Error.Error())
		}
		if e := tx.Where("id IN ? AND status = ?", ids, NotifStatusProcessing).
			Order("send_after ASC, id ASC").
			Find(&claimed).Error; e != nil {
			return coreerrors.Internal(e.Error())
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return claimed, nil
}

// CountNotificationAttempts returns the number of attempt rows for an intent.
func (s *Service) CountNotificationAttempts(intentID uint) (int, error) {
	var n int64
	if err := s.db.Model(&AppointmentNotificationAttempt{}).Where("intent_id = ?", intentID).Count(&n).Error; err != nil {
		return 0, coreerrors.Internal(err.Error())
	}
	return int(n), nil
}

// RecoverStaleProcessingClaims reclaim PROCESSING intents whose lease expired.
//
// Acquisition (short TX, multi-worker safe):
//  1. SELECT stale PROCESSING IDs FOR UPDATE SKIP LOCKED
//  2. WHILE LOCKED: refresh processing_started_at + updated_at to recoveryNow
//     so other workers no longer see these rows as stale under the same cutoff
//  3. COMMIT
//
// Then, outside the TX (no adapter I/O here): exactly one recovery attempt per
// acquired ID → PENDING+backoff or FAILED (counts toward max attempts).
func (s *Service) RecoverStaleProcessingClaims(asOf time.Time, batch int) (int, error) {
	if batch <= 0 {
		batch = NotificationClaimBatchDefault
	}
	if batch > NotificationClaimBatchMax {
		batch = NotificationClaimBatchMax
	}
	recoveryNow := asOf.UTC()
	cutoff := recoveryNow.Add(-NotificationStaleProcessing)

	acquired, err := s.acquireStaleProcessingLeases(recoveryNow, cutoff, batch)
	if err != nil {
		return 0, err
	}
	recovered := 0
	msg := "stale processing lease recovery"
	for _, id := range acquired {
		// Acquired IDs already had their lease refreshed; do not re-require processing_started_at < cutoff.
		cur, e := s.FindNotificationIntent(id)
		if e != nil || cur.Status != NotifStatusProcessing {
			continue
		}
		if e := s.failOrRetryAfterAttempt(id, "worker", nil, &msg, recoveryNow); e != nil {
			return recovered, e
		}
		recovered++
	}
	return recovered, nil
}

// acquireStaleProcessingLeases locks stale PROCESSING rows and refreshes their lease atomically.
// Returns only IDs successfully acquired in this transaction.
func (s *Service) acquireStaleProcessingLeases(recoveryNow, cutoff time.Time, batch int) ([]uint, error) {
	var acquired []uint
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var ids []uint
		if e := tx.Raw(`
			SELECT id FROM appointment_notification_intents
			WHERE status = ?
			  AND processing_started_at IS NOT NULL
			  AND processing_started_at < ?
			ORDER BY processing_started_at ASC, id ASC
			LIMIT ?
			FOR UPDATE SKIP LOCKED
		`, NotifStatusProcessing, cutoff, batch).Scan(&ids).Error; e != nil {
			return e
		}
		if len(ids) == 0 {
			return nil
		}
		// Durable lease refresh while still locked — second workers using the same
		// cutoff cannot re-select these rows after COMMIT.
		if e := tx.Raw(`
			UPDATE appointment_notification_intents
			SET processing_started_at = ?, updated_at = ?
			WHERE id IN ?
			  AND status = ?
			  AND processing_started_at IS NOT NULL
			  AND processing_started_at < ?
			RETURNING id
		`, recoveryNow, recoveryNow, ids, NotifStatusProcessing, cutoff).Scan(&acquired).Error; e != nil {
			return e
		}
		return nil
	})
	if err != nil {
		return nil, coreerrors.Internal(err.Error())
	}
	return acquired, nil
}

// failOrRetryAfterAttempt atomically records a failed attempt and transitions
// PROCESSING → PENDING (backoff) or PROCESSING → FAILED (max attempts) in one TX.
func (s *Service) failOrRetryAfterAttempt(intentID uint, provider string, providerMessageID, errMsg *string, now time.Time) error {
	_, err := s.FinalizeNotificationFailure(intentID, provider, providerMessageID, errMsg, now)
	return err
}

// FinalizeNotificationSent atomically inserts a success attempt and PROCESSING → SENT.
// Adapter I/O must already be complete outside this transaction.
func (s *Service) FinalizeNotificationSent(intentID uint, provider string, providerMessageID *string) (*AppointmentNotificationAttempt, error) {
	return s.finalizeProcessingDelivery(intentID, provider, providerMessageID, nil, func(tx *gorm.DB, parent *AppointmentNotificationIntent, _ int, ts time.Time) error {
		return applyProcessingTerminalTx(tx, parent.ID, NotifStatusSent, ts, map[string]any{"sent_at": ts})
	})
}

// FinalizeNotificationSkipped atomically inserts an attempt and PROCESSING → SKIPPED.
func (s *Service) FinalizeNotificationSkipped(intentID uint, provider string, errMsg *string) (*AppointmentNotificationAttempt, error) {
	return s.finalizeProcessingDelivery(intentID, provider, nil, errMsg, func(tx *gorm.DB, parent *AppointmentNotificationIntent, _ int, ts time.Time) error {
		return applyProcessingTerminalTx(tx, parent.ID, NotifStatusSkipped, ts, nil)
	})
}

// FinalizeNotificationFailure atomically inserts a failed attempt and either
// PROCESSING → PENDING with backoff, or PROCESSING → FAILED at max attempts.
func (s *Service) FinalizeNotificationFailure(intentID uint, provider string, providerMessageID, errMsg *string, now time.Time) (*AppointmentNotificationAttempt, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return s.finalizeProcessingDelivery(intentID, provider, providerMessageID, errMsg, func(tx *gorm.DB, parent *AppointmentNotificationIntent, attemptNo int, ts time.Time) error {
		if attemptNo >= NotificationMaxAttempts {
			return applyProcessingTerminalTx(tx, parent.ID, NotifStatusFailed, ts, nil)
		}
		backoff, ok := NotificationRetryBackoff(attemptNo)
		if !ok {
			return applyProcessingTerminalTx(tx, parent.ID, NotifStatusFailed, ts, nil)
		}
		if err := AssertNotificationTransition(NotifStatusProcessing, NotifStatusPending); err != nil {
			return err
		}
		res := tx.Model(&AppointmentNotificationIntent{}).
			Where("id = ? AND status = ?", parent.ID, NotifStatusProcessing).
			Updates(map[string]any{
				"status":                NotifStatusPending,
				"send_after":            now.UTC().Add(backoff),
				"processing_started_at": nil,
				"updated_at":            ts,
			})
		if res.Error != nil {
			return coreerrors.Internal(res.Error.Error())
		}
		if res.RowsAffected == 0 {
			return coreerrors.Conflict("transition notification concurrente ou invalide")
		}
		return nil
	})
}

type finalizeTransitionFn func(tx *gorm.DB, parent *AppointmentNotificationIntent, attemptNo int, now time.Time) error

func applyProcessingTerminalTx(tx *gorm.DB, id uint, to string, now time.Time, extra map[string]any) error {
	if err := AssertNotificationTransition(NotifStatusProcessing, to); err != nil {
		return err
	}
	updates := map[string]any{
		"status":                to,
		"processing_started_at": nil,
		"updated_at":            now,
	}
	for k, v := range extra {
		updates[k] = v
	}
	res := tx.Model(&AppointmentNotificationIntent{}).
		Where("id = ? AND status = ?", id, NotifStatusProcessing).
		Updates(updates)
	if res.Error != nil {
		return coreerrors.Internal(res.Error.Error())
	}
	if res.RowsAffected == 0 {
		return coreerrors.Conflict("transition notification concurrente ou invalide")
	}
	return nil
}

// finalizeProcessingDelivery locks a PROCESSING intent, inserts one attempt, applies terminal/retry
// transition, and commits atomically. No adapter I/O. Rolls back attempt if transition fails.
func (s *Service) finalizeProcessingDelivery(
	intentID uint,
	provider string,
	providerMessageID, errMsg *string,
	transition finalizeTransitionFn,
) (*AppointmentNotificationAttempt, error) {
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
		if parent.Status != NotifStatusProcessing {
			return coreerrors.Conflict("finalisation notification réservée au statut PROCESSING (statut=" + parent.Status + ")")
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
		if e := transition(tx, &parent, attempt.AttemptNo, now); e != nil {
			return e
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &attempt, nil
}

// RecordNotificationAttempt appends an attempt with monotonic attempt_no (unique per intent).
// Serializes writers per intent via SELECT … FOR UPDATE on the parent intent row.
// Does not store message bodies or patient contact PHI.
// Worker terminal finalization should use FinalizeNotificationSent/Skipped/Failure instead.
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
