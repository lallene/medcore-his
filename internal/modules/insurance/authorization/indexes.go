package authorization

import (
	"fmt"

	"gorm.io/gorm"
)

// EnsureAuthorizationIndexes creates the LOT27E partial unique index matching
// Create duplicate semantics: at most one non-CANCELLED authorization per
// (patient, coverage, reference_type, reference_id). CANCELLED rows may recreate.
func EnsureAuthorizationIndexes(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("EnsureAuthorizationIndexes: nil db")
	}
	const stmt = `
CREATE UNIQUE INDEX IF NOT EXISTS ux_insurance_authorizations_active_reference
ON insurance_authorizations (patient_id, patient_coverage_id, reference_type, reference_id)
WHERE status <> 'CANCELLED'`
	if err := db.Exec(stmt).Error; err != nil {
		return fmt.Errorf("ux_insurance_authorizations_active_reference: %w", err)
	}
	return nil
}
