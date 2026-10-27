package main

import (
	"strings"
	"testing"
)

// GRC15-B09 — on the schema built by cmd/migrate, nothing deletes GRC history
// implicitly: every foreign key that touches a grc_* table is RESTRICT, no GRC
// registry carries a soft-delete column, and audit_logs references no GRC row.
func TestGRC15B09_SchemaKeepsGRCHistory(t *testing.T) {
	db := migrateSchemaDB(t)
	if err := applyMigrations(db); err != nil {
		t.Fatalf("migration: %v", err)
	}

	var keys []struct {
		Name       string
		Table      string
		RefTable   string
		DeleteType string
	}
	if err := db.Raw(`SELECT c.conname AS name, rel.relname AS "table", ref.relname AS ref_table,
			c.confdeltype::text AS delete_type
		FROM pg_constraint c
		JOIN pg_class rel ON rel.oid = c.conrelid
		JOIN pg_namespace n ON n.oid = rel.relnamespace
		JOIN pg_class ref ON ref.oid = c.confrelid
		WHERE c.contype = 'f' AND n.nspname = current_schema()
			AND (rel.relname LIKE 'grc\_%' OR ref.relname LIKE 'grc\_%')`).Scan(&keys).Error; err != nil {
		t.Fatal(err)
	}
	if len(keys) < 40 {
		t.Fatalf("only %d GRC foreign keys found: schema not migrated?", len(keys))
	}
	t.Logf("%d foreign keys touch grc_* tables", len(keys))
	for _, key := range keys {
		// r = RESTRICT. a (NO ACTION), c (CASCADE), n (SET NULL), d (SET DEFAULT) are refused.
		if key.DeleteType != "r" {
			t.Errorf("%s (%s -> %s) ON DELETE %q, want RESTRICT", key.Name, key.Table, key.RefTable, key.DeleteType)
		}
		if key.Table == "audit_logs" {
			t.Errorf("audit_logs must not reference %s (%s)", key.RefTable, key.Name)
		}
	}

	var softDelete []string
	if err := db.Raw(`SELECT table_name FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name LIKE 'grc\_%' AND column_name = 'deleted_at'`).Scan(&softDelete).Error; err != nil {
		t.Fatal(err)
	}
	if len(softDelete) > 0 {
		t.Errorf("GRC tables with deleted_at: %s", strings.Join(softDelete, ", "))
	}

	var auditKeys int64
	if err := db.Raw(`SELECT COUNT(*) FROM pg_constraint c
		JOIN pg_class rel ON rel.oid = c.conrelid
		JOIN pg_namespace n ON n.oid = rel.relnamespace
		WHERE c.contype = 'f' AND n.nspname = current_schema() AND rel.relname = 'audit_logs'`).Scan(&auditKeys).Error; err != nil {
		t.Fatal(err)
	}
	if auditKeys != 0 {
		t.Errorf("audit_logs has %d foreign keys, want 0: deleting a referenced row must never cascade into the audit", auditKeys)
	}
}
