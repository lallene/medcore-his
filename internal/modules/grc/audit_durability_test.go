package grc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lallene/medcore-his/backend/internal/core/audit"
	"gorm.io/gorm"
)

func countModel(t *testing.T, db *gorm.DB, model any) int64 {
	t.Helper()
	var n int64
	if err := db.Model(model).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func TestGRCAuditIsAtomicWithTheMutation(t *testing.T) {
	f := newAssetFixture(t)
	svc := NewService(f.db)
	req := validAssetRequest(f)
	req.Description = "SECRET-ASSET-DESC"

	created, err := svc.SaveAsset(0, 41, req)
	if err != nil {
		t.Fatal(err)
	}
	if countModel(t, f.db, &Asset{}) != 1 || countModel(t, f.db, &audit.AuditLog{}) != 1 {
		t.Fatalf("assets=%d audits=%d", countModel(t, f.db, &Asset{}), countModel(t, f.db, &audit.AuditLog{}))
	}
	var row audit.AuditLog
	if err = f.db.First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.Module != auditModuleGRCAsset || row.Action != auditActionCreate || row.RecordID != created.ID || row.UserID == nil || *row.UserID != 41 {
		t.Fatalf("audit row %+v", row)
	}
	if strings.Contains(row.NewValue, "SECRET-ASSET-DESC") || strings.Contains(row.OldValue, "SECRET-ASSET-DESC") {
		t.Fatal("asset description was copied into the audit snapshot")
	}

	req.Code = "APP-MEDCORE"
	if _, err = svc.SaveAsset(0, 41, req); err == nil {
		t.Fatal("duplicate asset was accepted")
	}
	if countModel(t, f.db, &Asset{}) != 1 || countModel(t, f.db, &audit.AuditLog{}) != 1 {
		t.Fatal("a failed business mutation wrote a success audit")
	}
}

func TestGRCAuditFailureRollsBackTheMutation(t *testing.T) {
	f := newAssetFixture(t)
	svc := NewService(f.db)
	previous := insertGRCAuditRow
	insertGRCAuditRow = func(tx *gorm.DB, row *audit.AuditLog) error {
		return errGRCAuditRejected
	}
	t.Cleanup(func() { insertGRCAuditRow = previous })

	if _, err := svc.SaveAsset(0, 41, validAssetRequest(f)); err == nil {
		t.Fatal("audit failure was ignored")
	}
	if countModel(t, f.db, &Asset{}) != 0 || countModel(t, f.db, &audit.AuditLog{}) != 0 {
		t.Fatal("audit failure left a business row or an audit row")
	}
}

func TestGRCMutationsUseTheTransactionalAuditWriter(t *testing.T) {
	files := []string{
		"service.go",
		"compliance_service.go",
		"evidence_service.go",
		"audit_engagement_service.go",
		"assessment_service.go",
		"finding_service.go",
		"remediation_action_service.go",
		"security_incident_service.go",
		"governance_policy_service.go",
	}
	for _, name := range files {
		body, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatal(err)
		}
		text := string(body)
		if !strings.Contains(text, "writeGRCAudit(") && !strings.Contains(text, "saveWithAudit(") {
			t.Fatalf("%s does not use the transactional audit writer", name)
		}
		if strings.Contains(text, "DefaultBus.Publish") || strings.Contains(text, "publishGRCAudit(") {
			t.Fatalf("%s still publishes a best-effort audit event", name)
		}
	}
}
