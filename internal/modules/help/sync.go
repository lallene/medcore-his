package help

import (
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SyncResult reports what a catalog synchronization changed.
type SyncResult struct {
	Version     string
	Upserted    int
	Deactivated int64
}

// SyncCatalog upserts the embedded catalog by help_key and deactivates rows whose
// key left the catalog. Idempotent, single transaction, never deletes a row.
// Owned by cmd/migrate (versioned system data, like organization reference data).
func SyncCatalog(db *gorm.DB) (SyncResult, error) {
	entries, version, err := LoadCatalog()
	if err != nil {
		return SyncResult{}, err
	}
	return syncEntries(db, entries, version)
}

func syncEntries(db *gorm.DB, entries []CatalogEntry, version string) (SyncResult, error) {
	out := SyncResult{Version: version}
	keys := make([]string, 0, len(entries))
	err := db.Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		for _, c := range entries {
			row := c.toEntry()
			row.CreatedAt, row.UpdatedAt = now, now
			if err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "help_key"}},
				DoUpdates: clause.AssignmentColumns([]string{
					"module", "page", "element_type", "element_label", "short_help", "detailed_help",
					"action_help", "required_permissions", "is_active", "sort_order", "updated_at",
				}),
			}).Create(&row).Error; err != nil {
				return fmt.Errorf("help entry %s: %w", c.Key, err)
			}
			keys = append(keys, c.Key)
		}
		q := tx.Model(&Entry{}).Where("is_active")
		if len(keys) > 0 {
			q = q.Where("help_key NOT IN ?", keys)
		}
		res := q.Updates(map[string]any{"is_active": false, "updated_at": now})
		if res.Error != nil {
			return res.Error
		}
		out.Deactivated = res.RowsAffected
		out.Upserted = len(keys)
		return nil
	})
	return out, err
}
