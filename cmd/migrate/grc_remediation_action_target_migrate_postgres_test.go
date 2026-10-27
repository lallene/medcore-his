package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/lallene/medcore-his/backend/internal/modules/grc"
	"gorm.io/gorm"
)

type historicalRemediationAction struct {
	ID                      uint
	Reference               string
	ControlImplementationID *uint
	RiskID                  *uint
	FindingID               *uint
	Priority                string
	Status                  string
}

// rebuildPre01D03RemediationActions turns the migrated table back into its
// GRC-01C/01D.02 shape: no finding_id column (its FK and index go with it) and
// the legacy target CHECK instead of the v2 one.
func rebuildPre01D03RemediationActions(t *testing.T, db *gorm.DB) {
	t.Helper()

	for _, stmt := range []string{
		"ALTER TABLE grc_remediation_actions DROP CONSTRAINT " + grc.RemediationActionTargetConstraint,
		"ALTER TABLE grc_remediation_actions DROP COLUMN finding_id",
		"ALTER TABLE grc_remediation_actions ADD CONSTRAINT " + grc.RemediationActionLegacyTargetConstraint +
			" CHECK (control_implementation_id IS NOT NULL OR risk_id IS NOT NULL)",
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("rebuild pre-01D.03 state: %v", err)
		}
	}
}

func requireRemediationActionTargetUpgrade(t *testing.T, db *gorm.DB, pass int, findingID uint, historical []historicalRemediationAction) {
	t.Helper()

	columns := pgColumns(t, db, "grc_remediation_actions")
	requirePGColumn(t, columns, "grc_remediation_actions", "finding_id", "bigint", true, 0)
	requirePGIndexOn(t, db, "grc_remediation_actions", "finding_id", false)

	key, ok := pgForeignKeys(t, db, "grc_remediation_actions")["finding_id"]
	if !ok || key.RefTable != "grc_findings" || key.UpdateType != "c" || key.DeleteType != "r" {
		t.Fatalf("pass %d: finding_id FK = %+v, want -> grc_findings CASCADE/RESTRICT", pass, key)
	}

	checks := pgCheckConstraints(t, db, "grc_remediation_actions")
	if definition, ok := checks[grc.RemediationActionLegacyTargetConstraint]; ok {
		t.Fatalf("pass %d: legacy CHECK still present: %s", pass, definition)
	}
	definition, ok := checks[grc.RemediationActionTargetConstraint]
	if !ok {
		t.Fatalf("pass %d: %s missing (found %v)", pass, grc.RemediationActionTargetConstraint, checks)
	}
	for _, fragment := range []string{"control_implementation_id IS NOT NULL", "risk_id IS NOT NULL", "finding_id IS NOT NULL"} {
		if !strings.Contains(definition, fragment) {
			t.Fatalf("pass %d: %s = %s, missing %q", pass, grc.RemediationActionTargetConstraint, definition, fragment)
		}
	}

	for _, want := range historical {
		var got historicalRemediationAction
		if err := db.Raw(`SELECT id, reference, control_implementation_id, risk_id, finding_id, priority, status
			FROM grc_remediation_actions WHERE id = ?`, want.ID).Scan(&got).Error; err != nil {
			t.Fatal(err)
		}
		if got.ID != want.ID || got.Reference != want.Reference || got.Priority != want.Priority ||
			got.Status != want.Status || !sameOptionalID(got.ControlImplementationID, want.ControlImplementationID) ||
			!sameOptionalID(got.RiskID, want.RiskID) || got.FindingID != nil {
			t.Fatalf("pass %d: historical action changed: got %+v want %+v", pass, got, want)
		}
	}

	insert := func(reference string, findingTarget any) error {
		return db.Exec(`INSERT INTO grc_remediation_actions (reference, title, description, finding_id,
				priority, status, created_by, updated_by, created_at, updated_at)
			VALUES (?, 'Action constat', '', ?, 'LOW', 'OPEN', 1, 1, now(), now())`, reference, findingTarget).Error
	}
	if err := insert(fmt.Sprintf("ACT-FINDING-%d", pass), findingID); err != nil {
		t.Fatalf("pass %d: finding-only action rejected: %v", pass, err)
	}
	requirePGViolation(t, insert(fmt.Sprintf("ACT-NONE-%d", pass), nil), "23514", grc.RemediationActionTargetConstraint)
	requirePGViolation(t, db.Exec("DELETE FROM grc_findings WHERE id = ?", findingID).Error, "23503", "")
}

func sameOptionalID(a, b *uint) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// GRC-01D.03 — upgrading a GRC-01C/01D.02 database replaces the legacy target
// CHECK by the v2 one, adds finding_id with its RESTRICT FK, keeps historical
// actions and is idempotent.
func TestGRC01D03_RemediationActionTargetUpgrade(t *testing.T) {
	db := migrateSchemaDB(t)
	if err := applyMigrations(db); err != nil {
		t.Fatalf("fresh migration: %v", err)
	}

	// A fresh schema only has the v2 constraint.
	fresh := pgCheckConstraints(t, db, "grc_remediation_actions")
	if _, ok := fresh[grc.RemediationActionLegacyTargetConstraint]; ok {
		t.Fatal("fresh schema must not contain the legacy target CHECK")
	}
	if _, ok := fresh[grc.RemediationActionTargetConstraint]; !ok {
		t.Fatal("fresh schema misses the v2 target CHECK")
	}

	rebuildPre01D03RemediationActions(t, db)
	if _, ok := pgColumns(t, db, "grc_remediation_actions")["finding_id"]; ok {
		t.Fatal("pre-01D.03 state must not have finding_id")
	}

	var controlID, assetID, implementationID, riskID, auditID, findingID uint
	mustScan := func(dest *uint, stmt string, args ...any) {
		t.Helper()
		if err := db.Raw(stmt, args...).Scan(dest).Error; err != nil {
			t.Fatal(err)
		}
	}
	mustScan(&controlID, `INSERT INTO grc_controls (code, name, description, category, status, created_by, updated_by, created_at, updated_at)
		VALUES ('CTL-UPG', 'Contrôle', '', '', 'ACTIVE', 1, 1, now(), now()) RETURNING id`)
	mustScan(&assetID, `INSERT INTO grc_assets (code, name, type, description, criticality, status, created_by, updated_by, created_at, updated_at)
		VALUES ('AST-UPG', 'Actif', 'APPLICATION', '', 'HIGH', 'ACTIVE', 1, 1, now(), now()) RETURNING id`)
	mustScan(&implementationID, `INSERT INTO grc_control_implementations (control_id, asset_id, status, implementation_details, created_by, updated_by, created_at, updated_at)
		VALUES (?, ?, 'IMPLEMENTED', '', 1, 1, now(), now()) RETURNING id`, controlID, assetID)
	mustScan(&riskID, `INSERT INTO grc_risks (reference, title, description, threat, vulnerability, likelihood, impact, inherent_score,
			residual_likelihood, residual_impact, residual_score, treatment, status, created_by, updated_by, created_at, updated_at)
		VALUES ('RISK-UPG', 'Risque', 'Risque', '', '', 2, 3, 6, 1, 3, 3, 'REDUCE', 'OPEN', 1, 1, now(), now()) RETURNING id`)
	mustScan(&auditID, `INSERT INTO grc_audit_engagements (reference, title, description, scope, type, status, created_by, updated_by, created_at, updated_at)
		VALUES ('AUD-UPG', 'Audit', '', '', 'INTERNAL', 'PLANNED', 1, 1, now(), now()) RETURNING id`)
	mustScan(&findingID, `INSERT INTO grc_findings (reference, title, description, audit_engagement_id, category, severity, status,
			closure_note, created_by, updated_by, created_at, updated_at)
		VALUES ('FND-UPG', 'Constat', '', ?, 'CONTROL_DEFICIENCY', 'HIGH', 'OPEN', '', 1, 1, now(), now()) RETURNING id`, auditID)

	// Historical 01C actions, written before the upgrade.
	historical := []historicalRemediationAction{
		{Reference: "ACT-HIST-CI", ControlImplementationID: &implementationID, Priority: "HIGH", Status: "OPEN"},
		{Reference: "ACT-HIST-RISK", RiskID: &riskID, Priority: "LOW", Status: "IN_PROGRESS"},
	}
	for i := range historical {
		h := &historical[i]
		mustScan(&h.ID, `INSERT INTO grc_remediation_actions (reference, title, description, control_implementation_id, risk_id,
				priority, status, created_by, updated_by, created_at, updated_at)
			VALUES (?, 'Action historique', '', ?, ?, ?, ?, 1, 1, now(), now()) RETURNING id`,
			h.Reference, h.ControlImplementationID, h.RiskID, h.Priority, h.Status)
	}

	// The rebuilt state really enforces the legacy rule.
	requirePGViolation(t, db.Exec(`INSERT INTO grc_remediation_actions (reference, title, description, priority, status,
			created_by, updated_by, created_at, updated_at)
		VALUES ('ACT-LEGACY-NONE', 'Sans cible', '', 'LOW', 'OPEN', 1, 1, now(), now())`).Error,
		"23514", grc.RemediationActionLegacyTargetConstraint)

	for pass := 1; pass <= 2; pass++ {
		if err := applyMigrations(db); err != nil {
			t.Fatalf("upgrade migration pass %d: %v", pass, err)
		}
		requireRemediationActionTargetUpgrade(t, db, pass, findingID, historical)
	}
}
