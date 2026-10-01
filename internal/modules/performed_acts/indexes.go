package performed_acts

import (
	"fmt"

	"gorm.io/gorm"
)

// EnsurePerformedActIndexes creates the LOT27D producer uniqueness constraint.
// Partial unique index: at most one PerformedAct per (source_type, source_id) when both are set.
func EnsurePerformedActIndexes(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("EnsurePerformedActIndexes: nil db")
	}
	const stmt = `
CREATE UNIQUE INDEX IF NOT EXISTS ux_performed_acts_source
ON performed_acts (source_type, source_id)
WHERE source_type IS NOT NULL AND source_type <> '' AND source_id IS NOT NULL`
	if err := db.Exec(stmt).Error; err != nil {
		return fmt.Errorf("ux_performed_acts_source: %w", err)
	}
	return nil
}
