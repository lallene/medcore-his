package hospitalizations

import (
	"fmt"

	"gorm.io/gorm"
)

// EnsureHospitalizationIndexes creates LOT28D partial uniqueness:
// at most one ADMITTED hospitalization per patient.
func EnsureHospitalizationIndexes(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("EnsureHospitalizationIndexes: nil db")
	}
	const stmt = `
CREATE UNIQUE INDEX IF NOT EXISTS ux_hospitalizations_patient_admitted
ON hospitalizations (patient_id)
WHERE status = 'ADMITTED' AND deleted_at IS NULL`
	if err := db.Exec(stmt).Error; err != nil {
		return fmt.Errorf("ux_hospitalizations_patient_admitted: %w", err)
	}
	return nil
}
