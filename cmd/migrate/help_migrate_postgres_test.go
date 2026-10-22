package main

import (
	"strings"
	"testing"

	"github.com/lallene/medcore-his/backend/internal/modules/help"
)

// LOT AIDE — cmd/migrate creates ui_help_entries and synchronizes the versioned catalog
// idempotently (second run: same rows, no duplicate), with DB-enforced key uniqueness.
func TestHelpCatalogSynchronizedByMigrations(t *testing.T) {
	db := migrateSchemaDB(t)
	entries, _, err := help.LoadCatalog()
	if err != nil {
		t.Fatal(err)
	}
	for run := 1; run <= 2; run++ {
		if err := applyMigrations(db); err != nil {
			t.Fatalf("migration run %d: %v", run, err)
		}
		var total, active, distinct int64
		db.Raw("SELECT COUNT(*), COUNT(*) FILTER (WHERE is_active), COUNT(DISTINCT help_key) FROM ui_help_entries").Row().Scan(&total, &active, &distinct)
		if total != int64(len(entries)) || active != total || distinct != total {
			t.Fatalf("run %d: total=%d active=%d distinct=%d catalog=%d", run, total, active, distinct, len(entries))
		}
	}
	var unique int64
	db.Raw("SELECT COUNT(*) FROM pg_indexes WHERE schemaname=current_schema() AND tablename='ui_help_entries' AND indexdef ILIKE '%UNIQUE%' AND indexdef ILIKE '%help_key%'").Scan(&unique)
	if unique != 1 {
		t.Fatal("unique index on help_key missing")
	}
	err = db.Exec(`INSERT INTO ui_help_entries (help_key,module,page,element_type,element_label,short_help,created_at,updated_at)
		VALUES ('billing.x.y','billing','/billing','widget','x','x',now(),now())`).Error
	if err == nil || !strings.Contains(err.Error(), "23514") {
		t.Fatalf("element_type CHECK must be DB-enforced, got %v", err)
	}
}
