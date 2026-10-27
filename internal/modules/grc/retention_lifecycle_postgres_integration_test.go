package grc

import (
	"context"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/core/audit"
)

// GRC15-B09 — no retention purge exists. These checks pin the PostgreSQL facts
// that any future purge decision depends on, in an ephemeral schema only.
func TestPostgresGRC15B09RetentionLifecycle(t *testing.T) {
	f := newPGEvidenceActionFixture(t)
	if err := f.db.AutoMigrate(&GovernancePolicy{}, &GRCReminderIntent{}); err != nil {
		t.Fatal(err)
	}
	var schema string
	if err := f.db.Raw("SELECT current_schema()").Scan(&schema).Error; err != nil || schema == "public" {
		t.Fatalf("expected an ephemeral schema, got %q (%v)", schema, err)
	}
	ctx := context.Background()
	windows := DefaultReminderWindows()

	auditRows := func(module string, id uint) int64 {
		t.Helper()
		var n int64
		if err := f.db.Model(&audit.AuditLog{}).Where("module = ? AND record_id = ?", module, id).Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		return n
	}
	intents := func(column string, id uint) []GRCReminderIntent {
		t.Helper()
		var rows []GRCReminderIntent
		if err := f.db.Where(column+" = ?", id).Order("id ASC").Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		return rows
	}
	// A worker delivery, without mail: the row the purge would target.
	markDelivered := func(id uint, at time.Time) {
		t.Helper()
		if err := f.db.Model(&GRCReminderIntent{}).Where("id = ?", id).
			Updates(map[string]any{"status": ReminderStatusDelivered, "delivered_at": at, "attempt_count": 1}).Error; err != nil {
			t.Fatal(err)
		}
	}
	detect := func(at time.Time) {
		t.Helper()
		if _, err := f.svc.DetectGRCReminders(ctx, at, windows); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("audit history outlives a deleted business row", func(t *testing.T) {
		asset, err := f.svc.SaveAsset(0, 1, AssetRequest{Code: "AST-B09", Name: "Actif B09", Type: "SERVER", Criticality: "LOW", Status: "ACTIVE"})
		if err != nil {
			t.Fatal(err)
		}
		if got := auditRows(auditModuleGRCAsset, asset.ID); got != 1 {
			t.Fatalf("audit rows before delete = %d, want 1", got)
		}
		// No product path deletes a GRC record; this raw DELETE stands in for
		// a hypothetical purge. audit_logs has no FK to it, so nothing cascades.
		if err := f.db.Exec("DELETE FROM grc_assets WHERE id = ?", asset.ID).Error; err != nil {
			t.Fatal(err)
		}
		if got := auditRows(auditModuleGRCAsset, asset.ID); got != 1 {
			t.Fatalf("audit rows after delete = %d, want 1 (history must survive)", got)
		}
	})

	t.Run("a reminder intent pins its target", func(t *testing.T) {
		soon := pgNow.Add(2 * time.Hour)
		evidence := pgReminderEvidence(t, f, "EV-B09-PIN", &f.profile.ID, &soon)
		detect(pgNow)
		if len(intents("evidence_id", evidence.ID)) != 1 {
			t.Fatal("expected one evidence intent")
		}
		requirePostgresError(t, f.db.Delete(&Evidence{}, evidence.ID).Error, pgForeignKeyViolation, "fk_grc_reminder_intents_evidence")
	})

	t.Run("purging a delivered overdue reminder would send it again", func(t *testing.T) {
		due := pgNow.Add(-48 * time.Hour)
		action := pgReminderAction(t, f, "ACT-B09-OVERDUE", &f.profile.ID, &due)
		detect(pgNow)
		rows := intents("remediation_action_id", action.ID)
		if len(rows) != 1 || rows[0].OccurrenceKey != ReminderOccurrenceActionOverdue(action.ID, due) {
			t.Fatalf("overdue intents = %+v", rows)
		}
		markDelivered(rows[0].ID, pgNow)
		later := pgNow.Add(24 * time.Hour)
		detect(later)
		if got := len(intents("remediation_action_id", action.ID)); got != 1 {
			t.Fatalf("delivered intent must block a second one, got %d", got)
		}
		// The unique (recipient, occurrence) row is the only memory of the send.
		if err := f.db.Delete(&GRCReminderIntent{}, rows[0].ID).Error; err != nil {
			t.Fatal(err)
		}
		detect(later)
		again := intents("remediation_action_id", action.ID)
		if len(again) != 1 || again[0].Status != ReminderStatusPending || again[0].OccurrenceKey != rows[0].OccurrenceKey {
			t.Fatalf("after purge the same occurrence must reappear as PENDING, got %+v", again)
		}
	})

	t.Run("a past evidence occurrence is not recreated", func(t *testing.T) {
		soon := pgNow.Add(3 * time.Hour)
		evidence := pgReminderEvidence(t, f, "EV-B09-PAST", &f.profile.ID, &soon)
		detect(pgNow)
		rows := intents("evidence_id", evidence.ID)
		if len(rows) != 1 {
			t.Fatalf("evidence intents = %d, want 1", len(rows))
		}
		markDelivered(rows[0].ID, pgNow)
		if err := f.db.Delete(&GRCReminderIntent{}, rows[0].ID).Error; err != nil {
			t.Fatal(err)
		}
		detect(soon.Add(time.Hour))
		if got := len(intents("evidence_id", evidence.ID)); got != 0 {
			t.Fatalf("expired evidence must not be re-detected, got %d", got)
		}
		var kept Evidence
		if err := f.db.First(&kept, evidence.ID).Error; err != nil || kept.Status != EvidenceStatusValid {
			t.Fatalf("evidence must stay unchanged after expiry: %+v %v", kept, err)
		}
	})
}
