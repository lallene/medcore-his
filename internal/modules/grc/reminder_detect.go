package grc

import (
	"context"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"gorm.io/gorm/clause"
)

// ReminderWindows is the detection horizon in days. Each value is 1..365,
// the same bounds as the dashboard windows. Zero is not a default.
type ReminderWindows struct {
	EvidenceDays int
	ActionDays   int
	PolicyDays   int
}

// DefaultReminderWindows returns the 30-day dashboard window for every kind.
func DefaultReminderWindows() ReminderWindows {
	return ReminderWindows{
		EvidenceDays: DashboardDefaultWindowDays,
		ActionDays:   DashboardDefaultWindowDays,
		PolicyDays:   DashboardDefaultWindowDays,
	}
}

// ValidateReminderWindowDays accepts 1..365 inclusive.
func ValidateReminderWindowDays(days int) error {
	return validateDashboardWindow(days, "reminderWindowDays")
}

func (w ReminderWindows) validate() error {
	if err := ValidateReminderWindowDays(w.EvidenceDays); err != nil {
		return err
	}
	if err := ValidateReminderWindowDays(w.ActionDays); err != nil {
		return err
	}
	return ValidateReminderWindowDays(w.PolicyDays)
}

// Occurrence keys are deterministic. The instant is UTC Unix nanoseconds, so
// the same moment in another location does not create a second reminder.
// A changed expiry, due date, or effective end is a new key. The recipient
// is not part of the key; uniqueness is (recipient_profile_id, occurrence_key).
func ReminderOccurrenceEvidence(id uint, expiresAt time.Time) string {
	return fmt.Sprintf("evidence-expiring:%d:%d", id, expiresAt.UTC().UnixNano())
}

func ReminderOccurrenceActionDueSoon(id uint, dueAt time.Time) string {
	return fmt.Sprintf("action-due-soon:%d:%d", id, dueAt.UTC().UnixNano())
}

func ReminderOccurrenceActionOverdue(id uint, dueAt time.Time) string {
	return fmt.Sprintf("action-overdue:%d:%d", id, dueAt.UTC().UnixNano())
}

func ReminderOccurrencePolicy(id uint, effectiveTo time.Time) string {
	return fmt.Sprintf("policy-expiring:%d:%d", id, effectiveTo.UTC().UnixNano())
}

type reminderCandidate struct {
	ID                 uint
	Reference          string
	Title              string
	Status             string
	ExpiresAt          *time.Time
	DueAt              *time.Time
	EffectiveTo        *time.Time
	RecipientProfileID uint
	Email              string
}

// DetectGRCReminders inserts one PENDING intent per new logical occurrence.
// It does not update evidence, actions, or policies, and it does not send mail.
// A duplicate (recipient, occurrence) is ignored.
//
// Recipients, with no fallback:
//   - evidence: OwnerProfileID
//   - action: AssigneeProfileID
//   - policy: OwnerProfileID
//
// A missing owner, an inactive profile, an inactive or deleted user, or an
// unusable email produces no intent. Existing intents are left in place.
func (s *GRCService) DetectGRCReminders(ctx context.Context, now time.Time, windows ReminderWindows) (int, error) {
	if err := windows.validate(); err != nil {
		return 0, err
	}
	now = now.UTC()
	created := 0
	n, err := s.detectEvidenceReminders(ctx, now, windows.EvidenceDays)
	if err != nil {
		return created, err
	}
	created += n
	n, err = s.detectActionReminders(ctx, now, windows.ActionDays)
	if err != nil {
		return created, err
	}
	created += n
	n, err = s.detectPolicyReminders(ctx, now, windows.PolicyDays)
	if err != nil {
		return created, err
	}
	return created + n, nil
}

func (s *GRCService) detectEvidenceReminders(ctx context.Context, now time.Time, days int) (int, error) {
	limit, err := evidenceSoonLimit(now, days)
	if err != nil {
		return 0, err
	}
	var rows []reminderCandidate
	err = s.db.WithContext(ctx).
		Table("grc_evidence").
		Select("grc_evidence.id, grc_evidence.reference, grc_evidence.title, grc_evidence.status, grc_evidence.expires_at, grc_evidence.owner_profile_id AS recipient_profile_id, users.email").
		Joins("JOIN staff_profiles ON staff_profiles.id = grc_evidence.owner_profile_id AND staff_profiles.active = ?", true).
		Joins("JOIN users ON users.id = staff_profiles.user_id AND users.is_active = ? AND users.deleted_at IS NULL", true).
		Where("grc_evidence.owner_profile_id IS NOT NULL").
		Where(evidenceExpiringSoonSQL, EvidenceStatusValid, now, limit).
		Order("grc_evidence.id ASC").
		Scan(&rows).Error
	if err != nil {
		return 0, err
	}
	created := 0
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return created, err
		}
		if row.ExpiresAt == nil || !usableReminderEmail(row.Email) {
			continue
		}
		// The SQL fragment is evidenceExpiringSoonSQL. The helper is the authority.
		if EvidenceValidity(row.Status, row.ExpiresAt, now, limit) != EvidenceValidityExpiringSoon {
			continue
		}
		ok, err := s.insertReminderIntent(ctx, GRCReminderIntent{
			Kind:               ReminderKindEvidenceExpiring,
			Status:             ReminderStatusPending,
			EvidenceID:         &row.ID,
			RecipientProfileID: row.RecipientProfileID,
			OccurrenceKey:      ReminderOccurrenceEvidence(row.ID, *row.ExpiresAt),
			ScheduledFor:       now,
		})
		if err != nil {
			return created, err
		}
		if ok {
			created++
		}
	}
	return created, nil
}

func (s *GRCService) detectActionReminders(ctx context.Context, now time.Time, days int) (int, error) {
	limit, err := actionSoonLimit(now, days)
	if err != nil {
		return 0, err
	}
	var rows []reminderCandidate
	// Open actions whose due instant is overdue or inside the soon window.
	// Terminal statuses are excluded by the same SQL the due-state helper uses.
	err = s.db.WithContext(ctx).
		Table("grc_remediation_actions").
		Select("grc_remediation_actions.id, grc_remediation_actions.reference, grc_remediation_actions.title, grc_remediation_actions.status, grc_remediation_actions.due_at, grc_remediation_actions.assignee_profile_id AS recipient_profile_id, users.email").
		Joins("JOIN staff_profiles ON staff_profiles.id = grc_remediation_actions.assignee_profile_id AND staff_profiles.active = ?", true).
		Joins("JOIN users ON users.id = staff_profiles.user_id AND users.is_active = ? AND users.deleted_at IS NULL", true).
		Where("grc_remediation_actions.assignee_profile_id IS NOT NULL").
		Where("grc_remediation_actions.due_at IS NOT NULL").
		Where("(due_at < ? OR (due_at >= ? AND due_at <= ?)) AND status NOT IN ?", now, now, limit, terminalActionStatuses).
		Order("grc_remediation_actions.id ASC").
		Scan(&rows).Error
	if err != nil {
		return 0, err
	}
	created := 0
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return created, err
		}
		if row.DueAt == nil || !usableReminderEmail(row.Email) {
			continue
		}
		var key string
		switch RemediationActionDueState(row.Status, row.DueAt, now, limit) {
		case RemediationActionDueSoon:
			key = ReminderOccurrenceActionDueSoon(row.ID, *row.DueAt)
		case RemediationActionDueOverdue:
			key = ReminderOccurrenceActionOverdue(row.ID, *row.DueAt)
		default:
			continue
		}
		ok, err := s.insertReminderIntent(ctx, GRCReminderIntent{
			Kind:                ReminderKindActionDue,
			Status:              ReminderStatusPending,
			RemediationActionID: &row.ID,
			RecipientProfileID:  row.RecipientProfileID,
			OccurrenceKey:       key,
			ScheduledFor:        now,
		})
		if err != nil {
			return created, err
		}
		if ok {
			created++
		}
	}
	return created, nil
}

func (s *GRCService) detectPolicyReminders(ctx context.Context, now time.Time, days int) (int, error) {
	if err := ValidateReminderWindowDays(days); err != nil {
		return 0, err
	}
	limit := now.AddDate(0, 0, days)
	var rows []reminderCandidate
	err := s.db.WithContext(ctx).
		Table("grc_governance_policies").
		Select("grc_governance_policies.id, grc_governance_policies.reference, grc_governance_policies.title, grc_governance_policies.effective_to, grc_governance_policies.owner_profile_id AS recipient_profile_id, users.email").
		Joins("JOIN staff_profiles ON staff_profiles.id = grc_governance_policies.owner_profile_id AND staff_profiles.active = ?", true).
		Joins("JOIN users ON users.id = staff_profiles.user_id AND users.is_active = ? AND users.deleted_at IS NULL", true).
		Where("grc_governance_policies.status = ?", GovernancePolicyStatusPublished).
		Where("grc_governance_policies.owner_profile_id IS NOT NULL").
		Where("grc_governance_policies.effective_to IS NOT NULL AND grc_governance_policies.effective_to >= ? AND grc_governance_policies.effective_to <= ?", now, limit).
		Order("grc_governance_policies.id ASC").
		Scan(&rows).Error
	if err != nil {
		return 0, err
	}
	created := 0
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return created, err
		}
		if row.EffectiveTo == nil || !usableReminderEmail(row.Email) {
			continue
		}
		if row.EffectiveTo.Before(now) || row.EffectiveTo.After(limit) {
			continue
		}
		ok, err := s.insertReminderIntent(ctx, GRCReminderIntent{
			Kind:               ReminderKindPolicyExpiring,
			Status:             ReminderStatusPending,
			GovernancePolicyID: &row.ID,
			RecipientProfileID: row.RecipientProfileID,
			OccurrenceKey:      ReminderOccurrencePolicy(row.ID, *row.EffectiveTo),
			ScheduledFor:       now,
		})
		if err != nil {
			return created, err
		}
		if ok {
			created++
		}
	}
	return created, nil
}

func (s *GRCService) insertReminderIntent(ctx context.Context, intent GRCReminderIntent) (bool, error) {
	intent.Status = ReminderStatusPending
	intent.ScheduledFor = intent.ScheduledFor.UTC()
	res := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "recipient_profile_id"},
			{Name: "occurrence_key"},
		},
		DoNothing: true,
	}).Create(&intent)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func usableReminderEmail(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	parsed, err := mail.ParseAddress(raw)
	return err == nil && parsed != nil && parsed.Name == "" && parsed.Address == raw
}
