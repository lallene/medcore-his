package grc

import (
	"fmt"

	"gorm.io/gorm"
)

const (
	// RemediationActionLegacyTargetConstraint is the GRC-01C target CHECK
	// (control implementation or risk), superseded by the v2 constraint.
	RemediationActionLegacyTargetConstraint = "grc_remediation_action_target_present"
	// RemediationActionTargetConstraint requires at least one target among
	// control implementation, risk and finding (GRC-01D.03).
	RemediationActionTargetConstraint = "grc_remediation_action_target_present_v2"

	// remediationActionTargetCheck must stay identical to the check tag on
	// RemediationAction.RiskID.
	remediationActionTargetCheck = "control_implementation_id IS NOT NULL OR risk_id IS NOT NULL OR finding_id IS NOT NULL"
)

// EnsureRemediationActionTargetConstraint replaces the legacy target CHECK by
// the v2 one on PostgreSQL. AutoMigrate creates the finding_id column, its
// foreign key and a missing v2 CHECK, but never drops an existing CHECK: the
// legacy one would keep rejecting finding-only actions. Every existing row
// satisfies v2, so no data is touched. Idempotent; schema-relative
// (search_path). Must run after AutoMigrate.
func EnsureRemediationActionTargetConstraint(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("EnsureRemediationActionTargetConstraint: nil db")
	}

	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`ALTER TABLE grc_remediation_actions DROP CONSTRAINT IF EXISTS ` +
			RemediationActionLegacyTargetConstraint).Error; err != nil {
			return fmt.Errorf("%s drop: %w", RemediationActionLegacyTargetConstraint, err)
		}

		var n int64
		if err := tx.Raw(`SELECT COUNT(*) FROM pg_constraint
			WHERE conname = ? AND conrelid = 'grc_remediation_actions'::regclass`,
			RemediationActionTargetConstraint).Scan(&n).Error; err != nil {
			return fmt.Errorf("%s lookup: %w", RemediationActionTargetConstraint, err)
		}
		if n > 0 {
			return nil
		}

		if err := tx.Exec(`ALTER TABLE grc_remediation_actions ADD CONSTRAINT ` +
			RemediationActionTargetConstraint + ` CHECK (` + remediationActionTargetCheck + `)`).Error; err != nil {
			return fmt.Errorf("%s: %w", RemediationActionTargetConstraint, err)
		}
		return nil
	})
}
