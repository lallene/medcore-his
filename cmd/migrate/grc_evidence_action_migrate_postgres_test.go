package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

type pgColumn struct {
	DataType  string
	Nullable  string
	MaxLength *int
}

func pgColumns(t *testing.T, db *gorm.DB, table string) map[string]pgColumn {
	t.Helper()

	var rows []struct {
		ColumnName             string
		DataType               string
		IsNullable             string
		CharacterMaximumLength *int
	}
	if err := db.Raw(`SELECT column_name, data_type, is_nullable, character_maximum_length
		FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = ?`, table).Scan(&rows).Error; err != nil {
		t.Fatal(err)
	}
	columns := map[string]pgColumn{}
	for _, row := range rows {
		columns[row.ColumnName] = pgColumn{DataType: row.DataType, Nullable: row.IsNullable, MaxLength: row.CharacterMaximumLength}
	}
	return columns
}

func requirePGColumn(t *testing.T, columns map[string]pgColumn, table, name, dataType string, nullable bool, maxLength int) {
	t.Helper()

	column, ok := columns[name]
	if !ok {
		t.Errorf("%s.%s missing", table, name)
		return
	}
	wantNullable := "NO"
	if nullable {
		wantNullable = "YES"
	}
	if column.DataType != dataType || column.Nullable != wantNullable {
		t.Errorf("%s.%s = %s nullable=%s, want %s nullable=%s", table, name, column.DataType, column.Nullable, dataType, wantNullable)
	}
	if maxLength > 0 && (column.MaxLength == nil || *column.MaxLength != maxLength) {
		t.Errorf("%s.%s max length=%v, want %d", table, name, column.MaxLength, maxLength)
	}
}

// requirePGIndexOn asserts an index exists on exactly one column and returns
// its name; GORM generates index names, so only the column is asserted.
func requirePGIndexOn(t *testing.T, db *gorm.DB, table, column string, unique bool) string {
	t.Helper()

	prefix := "CREATE INDEX"
	if unique {
		prefix = "CREATE UNIQUE INDEX"
	}
	var names []string
	if err := db.Raw(`SELECT indexname FROM pg_indexes
		WHERE schemaname = current_schema() AND tablename = ?
		AND indexdef LIKE ? AND indexdef LIKE ?`,
		table, prefix+" %", "%("+column+")").Scan(&names).Error; err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 {
		t.Errorf("%s: %s on (%s) found %v, want exactly one", table, prefix, column, names)
		return ""
	}
	return names[0]
}

type pgForeignKey struct {
	Name       string
	ColumnName string
	RefTable   string
	UpdateType string
	DeleteType string
}

func pgForeignKeys(t *testing.T, db *gorm.DB, table string) map[string]pgForeignKey {
	t.Helper()

	var rows []pgForeignKey
	if err := db.Raw(`SELECT c.conname AS name, a.attname AS column_name, ref.relname AS ref_table,
			c.confupdtype::text AS update_type, c.confdeltype::text AS delete_type
		FROM pg_constraint c
		JOIN pg_class rel ON rel.oid = c.conrelid
		JOIN pg_namespace n ON n.oid = rel.relnamespace
		JOIN pg_class ref ON ref.oid = c.confrelid
		JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = ANY (c.conkey)
		WHERE c.contype = 'f' AND n.nspname = current_schema() AND rel.relname = ?`, table).Scan(&rows).Error; err != nil {
		t.Fatal(err)
	}
	keys := map[string]pgForeignKey{}
	for _, row := range rows {
		keys[row.ColumnName] = row
	}
	return keys
}

func pgCheckConstraints(t *testing.T, db *gorm.DB, table string) map[string]string {
	t.Helper()

	var rows []struct {
		Name       string
		Definition string
	}
	if err := db.Raw(`SELECT c.conname AS name, pg_get_constraintdef(c.oid) AS definition
		FROM pg_constraint c
		JOIN pg_class rel ON rel.oid = c.conrelid
		JOIN pg_namespace n ON n.oid = rel.relnamespace
		WHERE c.contype = 'c' AND n.nspname = current_schema() AND rel.relname = ?`, table).Scan(&rows).Error; err != nil {
		t.Fatal(err)
	}
	checks := map[string]string{}
	for _, row := range rows {
		checks[row.Name] = row.Definition
	}
	return checks
}

func requirePGViolation(t *testing.T, err error, code, constraint string) {
	t.Helper()

	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("expected PostgreSQL error %s (%s), got %v", code, constraint, err)
	}
	// PostgreSQL 18 reports ON DELETE RESTRICT as 23001. A requested 23503
	// still means "the foreign key rejected the statement".
	if pgErr.Code != code && !(code == "23503" && pgErr.Code == "23001") ||
		(constraint != "" && pgErr.ConstraintName != constraint) {
		t.Fatalf("PostgreSQL error %s/%s, want %s/%s", pgErr.Code, pgErr.ConstraintName, code, constraint)
	}
}

// GRC-01C — the official migration creates grc_evidence and
// grc_remediation_actions with the expected columns, timestamptz instants,
// CHECK, UNIQUE and RESTRICT foreign keys, enforced by PostgreSQL.
func TestGRC01C_MigrationsCreateEvidenceActionSchema(t *testing.T) {
	db := migrateSchemaDB(t)
	if err := applyMigrations(db); err != nil {
		t.Fatalf("first migration: %v", err)
	}
	if err := applyMigrations(db); err != nil {
		t.Fatalf("second migration must be idempotent: %v", err)
	}

	for _, table := range []string{"grc_evidence", "grc_remediation_actions"} {
		var ok bool
		if err := db.Raw("SELECT to_regclass(current_schema() || '.' || ?) IS NOT NULL", table).Scan(&ok).Error; err != nil || !ok {
			t.Fatalf("table %s missing (err=%v)", table, err)
		}
		var pk []string
		db.Raw(`SELECT a.attname FROM pg_constraint c
			JOIN pg_class rel ON rel.oid = c.conrelid
			JOIN pg_namespace n ON n.oid = rel.relnamespace
			JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = ANY (c.conkey)
			WHERE c.contype = 'p' AND n.nspname = current_schema() AND rel.relname = ?`, table).Scan(&pk)
		if strings.Join(pk, ",") != "id" {
			t.Errorf("%s primary key = %v, want [id]", table, pk)
		}
	}

	const (
		bigint      = "bigint"
		varchar     = "character varying"
		text        = "text"
		timestamptz = "timestamp with time zone"
	)

	evidence := pgColumns(t, db, "grc_evidence")
	for _, c := range []struct {
		name, dataType string
		nullable       bool
		maxLength      int
	}{
		{"id", bigint, false, 0},
		{"reference", varchar, false, 60},
		{"title", varchar, false, 220},
		{"description", text, true, 0},
		{"control_implementation_id", bigint, false, 0},
		{"type", varchar, false, 30},
		{"status", varchar, false, 20},
		{"external_reference", varchar, true, 500},
		{"collected_at", timestamptz, true, 0},
		{"valid_from", timestamptz, true, 0},
		{"expires_at", timestamptz, true, 0},
		{"owner_profile_id", bigint, true, 0},
		{"owning_service_id", bigint, true, 0},
		{"created_by", bigint, false, 0},
		{"updated_by", bigint, false, 0},
		{"created_at", timestamptz, false, 0},
		{"updated_at", timestamptz, false, 0},
	} {
		requirePGColumn(t, evidence, "grc_evidence", c.name, c.dataType, c.nullable, c.maxLength)
	}
	if _, ok := evidence["expired"]; ok {
		t.Error("computed Expired must not be persisted")
	}

	actions := pgColumns(t, db, "grc_remediation_actions")
	for _, c := range []struct {
		name, dataType string
		nullable       bool
		maxLength      int
	}{
		{"id", bigint, false, 0},
		{"reference", varchar, false, 60},
		{"title", varchar, false, 220},
		{"description", text, true, 0},
		{"control_implementation_id", bigint, true, 0},
		{"risk_id", bigint, true, 0},
		{"finding_id", bigint, true, 0},
		{"assignee_profile_id", bigint, true, 0},
		{"owning_service_id", bigint, true, 0},
		{"priority", varchar, false, 20},
		{"status", varchar, false, 20},
		{"due_at", timestamptz, true, 0},
		{"completed_at", timestamptz, true, 0},
		{"created_by", bigint, false, 0},
		{"updated_by", bigint, false, 0},
		{"created_at", timestamptz, false, 0},
		{"updated_at", timestamptz, false, 0},
	} {
		requirePGColumn(t, actions, "grc_remediation_actions", c.name, c.dataType, c.nullable, c.maxLength)
	}
	if _, ok := actions["overdue"]; ok {
		t.Error("computed Overdue must not be persisted")
	}

	requirePGIndexOn(t, db, "grc_evidence", "reference", true)
	requirePGIndexOn(t, db, "grc_remediation_actions", "reference", true)
	for _, column := range []string{
		"control_implementation_id", "type", "status", "expires_at",
		"owner_profile_id", "owning_service_id", "created_by", "updated_by",
	} {
		requirePGIndexOn(t, db, "grc_evidence", column, false)
	}
	for _, column := range []string{
		"control_implementation_id", "risk_id", "assignee_profile_id", "owning_service_id",
		"priority", "status", "due_at", "created_by", "updated_by",
	} {
		requirePGIndexOn(t, db, "grc_remediation_actions", column, false)
	}

	for table, want := range map[string]map[string]string{
		"grc_evidence": {
			"control_implementation_id": "grc_control_implementations",
			"owner_profile_id":          "staff_profiles",
			"owning_service_id":         "organization_services",
		},
		"grc_remediation_actions": {
			"control_implementation_id": "grc_control_implementations",
			"risk_id":                   "grc_risks",
			"finding_id":                "grc_findings",
			"assignee_profile_id":       "staff_profiles",
			"owning_service_id":         "organization_services",
		},
	} {
		keys := pgForeignKeys(t, db, table)
		if len(keys) != len(want) {
			t.Errorf("%s has %d foreign keys, want %d: %+v", table, len(keys), len(want), keys)
		}
		for column, refTable := range want {
			key, ok := keys[column]
			if !ok {
				t.Errorf("%s.%s foreign key missing", table, column)
				continue
			}
			// confupdtype 'c' = CASCADE, confdeltype 'r' = RESTRICT.
			if key.RefTable != refTable || key.UpdateType != "c" || key.DeleteType != "r" {
				t.Errorf("%s.%s FK %s -> %s update=%s delete=%s, want -> %s CASCADE/RESTRICT",
					table, column, key.Name, key.RefTable, key.UpdateType, key.DeleteType, refTable)
			}
		}
	}

	for table, want := range map[string]map[string][]string{
		"grc_evidence": {
			"grc_evidence_type_valid": {"DOCUMENT", "CONFIGURATION", "LOG", "SCREENSHOT",
				"REPORT", "TEST_RESULT", "CERTIFICATE", "OTHER"},
			"grc_evidence_status_valid":   {"DRAFT", "VALID", "REVOKED"},
			"grc_evidence_validity_range": {"expires_at IS NULL", "valid_from IS NULL", "expires_at >= valid_from"},
		},
		"grc_remediation_actions": {
			"grc_remediation_action_priority_valid": {"LOW", "MEDIUM", "HIGH", "CRITICAL"},
			"grc_remediation_action_status_valid":   {"OPEN", "IN_PROGRESS", "BLOCKED", "DONE", "CANCELLED"},
			// GRC-01D.03 superseded the 01C target CHECK by the v2 one.
			"grc_remediation_action_target_present_v2": {"control_implementation_id IS NOT NULL", "risk_id IS NOT NULL", "finding_id IS NOT NULL"},
			"grc_remediation_action_completion":        {"DONE", "completed_at IS NOT NULL"},
		},
	} {
		checks := pgCheckConstraints(t, db, table)
		for name, fragments := range want {
			definition, ok := checks[name]
			if !ok {
				t.Errorf("%s: CHECK %s missing (found %v)", table, name, checks)
				continue
			}
			for _, fragment := range fragments {
				if !strings.Contains(definition, fragment) {
					t.Errorf("%s.%s = %s, missing %q", table, name, definition, fragment)
				}
			}
		}
		for name, definition := range checks {
			lower := strings.ToLower(definition)
			if strings.Contains(lower, "now(") || strings.Contains(lower, "current_timestamp") ||
				strings.Contains(lower, "current_date") || strings.Contains(lower, "localtimestamp") {
				t.Errorf("%s.%s depends on the clock: %s", table, name, definition)
			}
		}
		if strings.Contains(checks["grc_evidence_status_valid"], "EXPIRED") {
			t.Error("EXPIRED must not be an accepted persisted evidence status")
		}
	}

	// Direct writes on the migrated schema, bypassing services on purpose.
	var controlID, assetID, implementationID, riskID uint
	mustScan := func(dest *uint, stmt string, args ...any) {
		t.Helper()
		if err := db.Raw(stmt, args...).Scan(dest).Error; err != nil {
			t.Fatal(err)
		}
	}
	mustScan(&controlID, `INSERT INTO grc_controls (code, name, description, category, status, created_by, updated_by, created_at, updated_at)
		VALUES ('CTL-MIG', 'Contrôle', '', '', 'ACTIVE', 1, 1, now(), now()) RETURNING id`)
	mustScan(&assetID, `INSERT INTO grc_assets (code, name, type, description, criticality, status, created_by, updated_by, created_at, updated_at)
		VALUES ('AST-MIG', 'Actif', 'APPLICATION', '', 'HIGH', 'ACTIVE', 1, 1, now(), now()) RETURNING id`)
	mustScan(&implementationID, `INSERT INTO grc_control_implementations (control_id, asset_id, status, implementation_details, created_by, updated_by, created_at, updated_at)
		VALUES (?, ?, 'IMPLEMENTED', '', 1, 1, now(), now()) RETURNING id`, controlID, assetID)
	mustScan(&riskID, `INSERT INTO grc_risks (reference, title, description, threat, vulnerability, likelihood, impact, inherent_score,
			residual_likelihood, residual_impact, residual_score, treatment, status, created_by, updated_by, created_at, updated_at)
		VALUES ('RISK-MIG', 'Risque', 'Risque', '', '', 2, 3, 6, 1, 3, 3, 'REDUCE', 'OPEN', 1, 1, now(), now()) RETURNING id`)

	insertEvidence := func(reference, evidenceType, status string, validFrom, expiresAt any) error {
		return db.Exec(`INSERT INTO grc_evidence (reference, title, description, control_implementation_id, type, status,
				external_reference, valid_from, expires_at, created_by, updated_by, created_at, updated_at)
			VALUES (?, 'Preuve', '', ?, ?, ?, '', ?, ?, 1, 1, now(), now())`,
			reference, implementationID, evidenceType, status, validFrom, expiresAt).Error
	}

	// Same instant written with two offsets: the range check compares instants.
	if err := insertEvidence("EVD-SAME-INSTANT", "REPORT", "VALID", "2026-10-25T10:00:00Z", "2026-10-25T12:00:00+02:00"); err != nil {
		t.Fatalf("equal instants with different offsets rejected: %v", err)
	}
	requirePGViolation(t, insertEvidence("EVD-RANGE", "REPORT", "VALID", "2026-10-25T10:00:00Z", "2026-10-25T11:59:59+02:00"),
		"23514", "grc_evidence_validity_range")
	requirePGViolation(t, insertEvidence("EVD-TYPE", "PASSWORD", "DRAFT", nil, nil), "23514", "grc_evidence_type_valid")
	requirePGViolation(t, insertEvidence("EVD-EXPIRED", "REPORT", "EXPIRED", nil, nil), "23514", "grc_evidence_status_valid")
	requirePGViolation(t, insertEvidence("EVD-SAME-INSTANT", "REPORT", "DRAFT", nil, nil), "23505", "")

	insertAction := func(reference string, implementation, risk any, priority, status string, completedAt any) error {
		return db.Exec(`INSERT INTO grc_remediation_actions (reference, title, description, control_implementation_id, risk_id,
				priority, status, completed_at, created_by, updated_by, created_at, updated_at)
			VALUES (?, 'Action', '', ?, ?, ?, ?, ?, 1, 1, now(), now())`,
			reference, implementation, risk, priority, status, completedAt).Error
	}

	if err := insertAction("ACT-BOTH", implementationID, riskID, "HIGH", "OPEN", nil); err != nil {
		t.Fatalf("valid action rejected: %v", err)
	}
	if err := insertAction("ACT-DONE", nil, riskID, "LOW", "DONE", "2026-10-25T12:00:00+02:00"); err != nil {
		t.Fatalf("DONE action with completed_at rejected: %v", err)
	}
	requirePGViolation(t, insertAction("ACT-PRIO", implementationID, nil, "URGENT", "OPEN", nil), "23514", "grc_remediation_action_priority_valid")
	requirePGViolation(t, insertAction("ACT-STATUS", implementationID, nil, "LOW", "CLOSED", nil), "23514", "grc_remediation_action_status_valid")
	requirePGViolation(t, insertAction("ACT-NONE", nil, nil, "LOW", "OPEN", nil), "23514", "grc_remediation_action_target_present_v2")
	requirePGViolation(t, insertAction("ACT-DONE-NULL", implementationID, nil, "LOW", "DONE", nil), "23514", "grc_remediation_action_completion")
	requirePGViolation(t, insertAction("ACT-OPEN-DATE", implementationID, nil, "LOW", "OPEN", "2026-10-25T10:00:00Z"), "23514", "grc_remediation_action_completion")
	requirePGViolation(t, insertAction("ACT-BOTH", implementationID, nil, "LOW", "OPEN", nil), "23505", "")
	requirePGViolation(t, db.Exec(`UPDATE grc_remediation_actions SET status = 'DONE' WHERE reference = 'ACT-BOTH'`).Error,
		"23514", "grc_remediation_action_completion")
	requirePGViolation(t, db.Exec(`UPDATE grc_remediation_actions SET completed_at = NULL WHERE reference = 'ACT-DONE'`).Error,
		"23514", "grc_remediation_action_completion")

	// Referenced parents cannot be deleted.
	requirePGViolation(t, db.Exec(`DELETE FROM grc_control_implementations WHERE id = ?`, implementationID).Error, "23503", "")
	requirePGViolation(t, db.Exec(`DELETE FROM grc_risks WHERE id = ?`, riskID).Error, "23503", "")
}
