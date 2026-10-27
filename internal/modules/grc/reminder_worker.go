package grc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"strings"
	"time"

	"github.com/lallene/medcore-his/backend/internal/shared/email"
	"gorm.io/gorm"
)

// ReminderWorker detects intents and delivers them by email. It is meant to
// run in its own process. The API does not start it.
type ReminderWorker struct {
	svc     *GRCService
	mail    email.Transport
	windows ReminderWindows
	lease   time.Duration
	batch   int
	log     *slog.Logger
}

func NewReminderWorker(svc *GRCService, mail email.Transport, windows ReminderWindows, lease time.Duration, log *slog.Logger) (*ReminderWorker, error) {
	if svc == nil {
		return nil, errors.New("grc reminder worker: service requis")
	}
	if mail == nil {
		return nil, errors.New("grc reminder worker: transport e-mail requis")
	}
	if err := windows.validate(); err != nil {
		return nil, err
	}
	if lease <= 0 {
		return nil, errors.New("grc reminder worker: bail non positif")
	}
	if log == nil {
		log = slog.Default()
	}
	return &ReminderWorker{
		svc:     svc,
		mail:    mail,
		windows: windows,
		lease:   lease,
		batch:   ReminderClaimBatch,
		log:     log,
	}, nil
}

// Run polls until ctx is cancelled. The interval must be positive.
// RunOnce is the testable pass; this loop only supplies the clock.
func (w *ReminderWorker) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		return errors.New("grc reminder worker: intervalle non positif")
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := w.RunOnce(ctx, time.Now().UTC()); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			w.log.Error("grc reminder run", "error", err.Error())
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// RunOnce detects, recovers expired leases, claims, then delivers.
// Mail is sent only after the claim transaction has committed.
func (w *ReminderWorker) RunOnce(ctx context.Context, now time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	now = now.UTC()
	w.log.Info("grc reminder scan start")
	created, err := w.svc.DetectGRCReminders(ctx, now, w.windows)
	if err != nil {
		return err
	}
	w.log.Info("grc reminder intents created", "count", created)
	recovered, err := w.svc.RecoverStaleGRCReminders(ctx, now, w.batch)
	if err != nil {
		return err
	}
	claimed, err := w.svc.ClaimDueGRCReminders(ctx, now, w.lease, w.batch)
	if err != nil {
		return err
	}
	w.log.Info("grc reminder claims", "recovered", recovered, "claimed", len(claimed))
	for _, intent := range claimed {
		if err := ctx.Err(); err != nil {
			return err
		}
		logReminderDelivery(w.log, intent, w.deliverOne(ctx, now, intent))
	}
	return nil
}

// deliverOne sends one already-claimed intent. It does not open a transaction
// before Send. The status update is a separate statement after Send returns.
func (w *ReminderWorker) deliverOne(ctx context.Context, now time.Time, intent GRCReminderIntent) error {
	var current GRCReminderIntent
	if err := w.svc.db.WithContext(ctx).
		Select("id", "kind", "status", "evidence_id", "remediation_action_id", "governance_policy_id", "recipient_profile_id", "occurrence_key", "attempt_count").
		First(&current, intent.ID).Error; err != nil {
		return err
	}
	if current.Status != ReminderStatusProcessing {
		return fmt.Errorf("rappel %d non réclamé", current.ID)
	}
	to, rerr := w.svc.resolveReminderRecipient(ctx, current.RecipientProfileID)
	if rerr != nil {
		return w.svc.markReminderFailed(ctx, current.ID, now, rerr.Error(), true)
	}
	facts, ferr := w.eligibleReminderFacts(ctx, now, current)
	if ferr != nil {
		switch {
		case errors.Is(ferr, errReminderObsolete):
			return w.svc.markReminderFailed(ctx, current.ID, now, reminderErrObsolete, true)
		case errors.Is(ferr, errReminderRetryable):
			return w.svc.markReminderFailed(ctx, current.ID, now, reminderErrTransient, false)
		default:
			return w.svc.markReminderFailed(ctx, current.ID, now, ferr.Error(), true)
		}
	}
	msg, merr := renderReminderMessage(to, current, facts)
	if merr != nil {
		return w.svc.markReminderFailed(ctx, current.ID, now, reminderErrPermanent, true)
	}
	if _, err := w.mail.Send(ctx, msg); err != nil {
		message, terminal := classifyReminderSendError(err)
		return w.svc.markReminderFailed(ctx, current.ID, now, message, terminal)
	}
	return w.svc.markReminderDelivered(ctx, current.ID, now)
}

func logReminderDelivery(log *slog.Logger, intent GRCReminderIntent, err error) {
	if err != nil {
		log.Error("grc reminder delivery failed", "intentId", intent.ID, "kind", intent.Kind, "outcome", reminderOutcome(err))
		return
	}
	log.Info("grc reminder delivered", "intentId", intent.ID, "kind", intent.Kind)
}

// reminderOutcome is the only delivery text written to logs. Unknown errors,
// including database and provider text, stay "internal".
func reminderOutcome(err error) string {
	if err == nil {
		return ""
	}
	switch err.Error() {
	case reminderErrInactive, reminderErrNoEmail, reminderErrMissing, reminderErrTargetMissing,
		reminderErrObsolete, reminderErrTransient, reminderErrPermanent, reminderErrAmbiguous,
		reminderErrNotConfigured, reminderErrStale:
		return err.Error()
	default:
		return "internal"
	}
}

func classifyReminderSendError(err error) (string, bool) {
	switch {
	case errors.Is(err, email.ErrAmbiguousDelivery):
		return reminderErrAmbiguous, true
	case errors.Is(err, email.ErrPermanent), errors.Is(err, email.ErrInvalidMessage):
		return reminderErrPermanent, true
	case errors.Is(err, email.ErrNotConfigured):
		return reminderErrNotConfigured, true
	default:
		return reminderErrTransient, false
	}
}

type reminderRecipientRow struct {
	Active     bool
	UserActive bool
	Email      string
	Name       string
	DeletedAt  *time.Time
}

func (s *GRCService) resolveReminderRecipient(ctx context.Context, profileID uint) (email.Address, error) {
	var row reminderRecipientRow
	err := s.db.WithContext(ctx).
		Table("staff_profiles").
		Select("staff_profiles.active AS active, users.is_active AS user_active, users.email AS email, users.name AS name, users.deleted_at AS deleted_at").
		Joins("JOIN users ON users.id = staff_profiles.user_id").
		Where("staff_profiles.id = ?", profileID).
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return email.Address{}, errors.New(reminderErrMissing)
	}
	if err != nil {
		return email.Address{}, err
	}
	if !row.Active || !row.UserActive || row.DeletedAt != nil {
		return email.Address{}, errors.New(reminderErrInactive)
	}
	address := strings.TrimSpace(row.Email)
	parsed, perr := mail.ParseAddress(address)
	if perr != nil || parsed == nil || parsed.Name != "" || parsed.Address != address {
		return email.Address{}, errors.New(reminderErrNoEmail)
	}
	return email.Address{Address: parsed.Address, DisplayName: singleLine(row.Name)}, nil
}

var (
	errReminderObsolete  = errors.New(reminderErrObsolete)
	errReminderRetryable = errors.New("rappel non vérifié")
)

// eligibleReminderFacts re-reads the target at delivery time. The intent is
// obsolete when the deadline no longer matches the occurrence, or when the
// single-source state is no longer the one that created that occurrence.
// A stale intent is not delivered and does not change the business object.
func (w *ReminderWorker) eligibleReminderFacts(ctx context.Context, now time.Time, intent GRCReminderIntent) (reminderFacts, error) {
	at, err := reminderInstant(intent.OccurrenceKey)
	if err != nil {
		return reminderFacts{}, err
	}
	now = now.UTC()
	switch intent.Kind {
	case ReminderKindEvidenceExpiring:
		return w.eligibleEvidenceFacts(ctx, now, intent, at)
	case ReminderKindActionDue:
		return w.eligibleActionFacts(ctx, now, intent, at)
	case ReminderKindPolicyExpiring:
		return w.eligiblePolicyFacts(ctx, now, intent, at)
	default:
		return reminderFacts{}, errors.New(reminderErrTargetMissing)
	}
}

func sameReminderInstant(stored *time.Time, occurrence time.Time) bool {
	return stored != nil && stored.UTC().UnixNano() == occurrence.UTC().UnixNano()
}

func (w *ReminderWorker) eligibleEvidenceFacts(ctx context.Context, now time.Time, intent GRCReminderIntent, at time.Time) (reminderFacts, error) {
	if intent.EvidenceID == nil {
		return reminderFacts{}, errors.New(reminderErrTargetMissing)
	}
	limit, err := evidenceSoonLimit(now, w.windows.EvidenceDays)
	if err != nil {
		return reminderFacts{}, err
	}
	var row struct {
		Reference string
		Title     string
		Status    string
		ExpiresAt *time.Time
	}
	err = w.svc.db.WithContext(ctx).Table("grc_evidence").
		Select("reference, title, status, expires_at").
		Where("id = ?", *intent.EvidenceID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return reminderFacts{}, errors.New(reminderErrTargetMissing)
	}
	if err != nil {
		return reminderFacts{}, fmt.Errorf("%w: %w", errReminderRetryable, err)
	}
	if !sameReminderInstant(row.ExpiresAt, at) || EvidenceValidity(row.Status, row.ExpiresAt, now, limit) != EvidenceValidityExpiringSoon {
		return reminderFacts{}, errReminderObsolete
	}
	return reminderFacts{Reference: row.Reference, Title: row.Title, At: at}, nil
}

func (w *ReminderWorker) eligibleActionFacts(ctx context.Context, now time.Time, intent GRCReminderIntent, at time.Time) (reminderFacts, error) {
	if intent.RemediationActionID == nil {
		return reminderFacts{}, errors.New(reminderErrTargetMissing)
	}
	stage, err := actionStageFromKey(intent.OccurrenceKey)
	if err != nil {
		return reminderFacts{}, err
	}
	limit, err := actionSoonLimit(now, w.windows.ActionDays)
	if err != nil {
		return reminderFacts{}, err
	}
	var row struct {
		Reference string
		Title     string
		Status    string
		DueAt     *time.Time
	}
	err = w.svc.db.WithContext(ctx).Table("grc_remediation_actions").
		Select("reference, title, status, due_at").
		Where("id = ?", *intent.RemediationActionID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return reminderFacts{}, errors.New(reminderErrTargetMissing)
	}
	if err != nil {
		return reminderFacts{}, fmt.Errorf("%w: %w", errReminderRetryable, err)
	}
	if !sameReminderInstant(row.DueAt, at) || RemediationActionDueState(row.Status, row.DueAt, now, limit) != stage {
		return reminderFacts{}, errReminderObsolete
	}
	return reminderFacts{Reference: row.Reference, Title: row.Title, At: at, Stage: stage}, nil
}

func (w *ReminderWorker) eligiblePolicyFacts(ctx context.Context, now time.Time, intent GRCReminderIntent, at time.Time) (reminderFacts, error) {
	if intent.GovernancePolicyID == nil {
		return reminderFacts{}, errors.New(reminderErrTargetMissing)
	}
	if err := ValidateReminderWindowDays(w.windows.PolicyDays); err != nil {
		return reminderFacts{}, err
	}
	limit := now.AddDate(0, 0, w.windows.PolicyDays)
	var row struct {
		Reference   string
		Title       string
		Status      string
		EffectiveTo *time.Time
	}
	err := w.svc.db.WithContext(ctx).Table("grc_governance_policies").
		Select("reference, title, status, effective_to").
		Where("id = ?", *intent.GovernancePolicyID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return reminderFacts{}, errors.New(reminderErrTargetMissing)
	}
	if err != nil {
		return reminderFacts{}, fmt.Errorf("%w: %w", errReminderRetryable, err)
	}
	inWindow := row.EffectiveTo != nil && !row.EffectiveTo.Before(now) && !row.EffectiveTo.After(limit)
	if row.Status != GovernancePolicyStatusPublished || !sameReminderInstant(row.EffectiveTo, at) || !inWindow {
		return reminderFacts{}, errReminderObsolete
	}
	return reminderFacts{Reference: row.Reference, Title: row.Title, At: at}, nil
}

func reminderInstant(key string) (time.Time, error) {
	i := strings.LastIndex(key, ":")
	if i < 0 || i == len(key)-1 {
		return time.Time{}, errors.New(reminderErrTargetMissing)
	}
	var n int64
	if _, err := fmt.Sscan(key[i+1:], &n); err != nil {
		return time.Time{}, errors.New(reminderErrTargetMissing)
	}
	return time.Unix(0, n).UTC(), nil
}

func actionStageFromKey(key string) (string, error) {
	switch {
	case strings.HasPrefix(key, "action-due-soon:"):
		return RemediationActionDueSoon, nil
	case strings.HasPrefix(key, "action-overdue:"):
		return RemediationActionDueOverdue, nil
	default:
		return "", errors.New(reminderErrTargetMissing)
	}
}

// ClaimDueGRCReminders moves due PENDING or retryable FAILED rows to
// PROCESSING. The transaction commits before the caller sends mail.
// Two workers cannot receive the same id: the claim is FOR UPDATE SKIP LOCKED.
// The attempt counter increments in that same update.
func (s *GRCService) ClaimDueGRCReminders(ctx context.Context, now time.Time, lease time.Duration, batch int) ([]GRCReminderIntent, error) {
	if err := s.requireReminderPostgres(); err != nil {
		return nil, err
	}
	if lease <= 0 {
		return nil, errors.New("grc reminder worker: bail non positif")
	}
	batch = reminderBatch(batch)
	now = now.UTC()
	leaseUntil := now.Add(lease)
	var claimed []GRCReminderIntent
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var ids []uint
		if e := tx.Raw(`
			SELECT id FROM grc_reminder_intents
			WHERE scheduled_for <= ?
			  AND (status = ? OR (status = ? AND attempt_count < ?))
			ORDER BY scheduled_for ASC, id ASC
			LIMIT ?
			FOR UPDATE SKIP LOCKED
		`, now, ReminderStatusPending, ReminderStatusFailed, ReminderMaxAttempts, batch).Scan(&ids).Error; e != nil {
			return e
		}
		if len(ids) == 0 {
			return nil
		}
		var locked []uint
		if e := tx.Raw(`
			UPDATE grc_reminder_intents
			SET status = ?, claimed_at = ?, lease_expires_at = ?, attempt_count = attempt_count + 1, updated_at = ?
			WHERE id IN ?
			  AND scheduled_for <= ?
			  AND (status = ? OR (status = ? AND attempt_count < ?))
			RETURNING id
		`, ReminderStatusProcessing, now, leaseUntil, now, ids, now, ReminderStatusPending, ReminderStatusFailed, ReminderMaxAttempts).Scan(&locked).Error; e != nil {
			return e
		}
		if len(locked) == 0 {
			return nil
		}
		return tx.Select("id", "kind", "status", "evidence_id", "remediation_action_id", "governance_policy_id", "recipient_profile_id", "occurrence_key", "attempt_count", "scheduled_for").
			Where("id IN ?", locked).
			Order("scheduled_for ASC, id ASC").
			Find(&claimed).Error
	})
	if err != nil {
		return nil, err
	}
	return claimed, nil
}

// RecoverStaleGRCReminders releases PROCESSING rows whose lease has expired.
// The attempt counted at claim time is kept. Rows under the max become
// retryable FAILED; rows already at the max stay FAILED and are not claimed.
// A lease that expires exactly at now is still live (lease_expires_at < now).
func (s *GRCService) RecoverStaleGRCReminders(ctx context.Context, now time.Time, batch int) (int, error) {
	if err := s.requireReminderPostgres(); err != nil {
		return 0, err
	}
	batch = reminderBatch(batch)
	now = now.UTC()
	message := clampReminderError(reminderErrStale)
	var recovered []uint
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var ids []uint
		if e := tx.Raw(`
			SELECT id FROM grc_reminder_intents
			WHERE status = ?
			  AND lease_expires_at IS NOT NULL
			  AND lease_expires_at < ?
			ORDER BY lease_expires_at ASC, id ASC
			LIMIT ?
			FOR UPDATE SKIP LOCKED
		`, ReminderStatusProcessing, now, batch).Scan(&ids).Error; e != nil {
			return e
		}
		if len(ids) == 0 {
			return nil
		}
		return tx.Raw(`
			UPDATE grc_reminder_intents
			SET status = ?, claimed_at = NULL, lease_expires_at = NULL, last_attempt_at = ?, last_error = ?, updated_at = ?
			WHERE id IN ?
			  AND status = ?
			  AND lease_expires_at IS NOT NULL
			  AND lease_expires_at < ?
			RETURNING id
		`, ReminderStatusFailed, now, message, now, ids, ReminderStatusProcessing, now).Scan(&recovered).Error
	})
	if err != nil {
		return 0, err
	}
	return len(recovered), nil
}

func (s *GRCService) markReminderDelivered(ctx context.Context, id uint, now time.Time) error {
	now = now.UTC()
	res := s.db.WithContext(ctx).Model(&GRCReminderIntent{}).
		Where("id = ? AND status = ?", id, ReminderStatusProcessing).
		Updates(map[string]any{
			"status":           ReminderStatusDelivered,
			"delivered_at":     now,
			"last_attempt_at":  now,
			"claimed_at":       nil,
			"lease_expires_at": nil,
			"last_error":       nil,
			"updated_at":       now,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return fmt.Errorf("rappel %d non finalisé", id)
	}
	return nil
}

func (s *GRCService) markReminderFailed(ctx context.Context, id uint, now time.Time, message string, terminal bool) error {
	now = now.UTC()
	message = clampReminderError(message)
	updates := map[string]any{
		"status":           ReminderStatusFailed,
		"last_attempt_at":  now,
		"last_error":       message,
		"claimed_at":       nil,
		"lease_expires_at": nil,
		"updated_at":       now,
	}
	if terminal {
		updates["attempt_count"] = ReminderMaxAttempts
	}
	res := s.db.WithContext(ctx).Model(&GRCReminderIntent{}).
		Where("id = ? AND status = ?", id, ReminderStatusProcessing).
		Updates(updates)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return fmt.Errorf("rappel %d non finalisé", id)
	}
	return errors.New(message)
}

func (s *GRCService) requireReminderPostgres() error {
	if s.db == nil || s.db.Dialector == nil || s.db.Dialector.Name() != "postgres" {
		return errors.New("le claim des rappels GRC exige PostgreSQL")
	}
	return nil
}

func reminderBatch(batch int) int {
	if batch <= 0 || batch > ReminderClaimBatch {
		return ReminderClaimBatch
	}
	return batch
}
