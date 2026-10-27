package grc

import (
	"strings"
	"testing"

	"github.com/lallene/medcore-his/backend/internal/core/audit"
	"gorm.io/gorm"
)

func TestPostgresGRC15AuditDurability(t *testing.T) {
	f := newPGAssetFixture(t)
	svc := NewService(f.db)
	req := validAssetRequest(f)
	req.Description = "SECRET-ASSET-DESC"

	created, err := svc.SaveAsset(0, 41, req)
	if err != nil {
		t.Fatal(err)
	}
	if countModel(t, f.db, &Asset{}) != 1 || countModel(t, f.db, &audit.AuditLog{}) != 1 {
		t.Fatal("successful mutation did not keep exactly one asset and one audit row")
	}
	var row audit.AuditLog
	if err = f.db.Where("record_id = ?", created.ID).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.Module != auditModuleGRCAsset || row.UserID == nil || *row.UserID != 41 || strings.Contains(row.NewValue, "SECRET-ASSET-DESC") {
		t.Fatalf("audit row %+v", row)
	}

	previous := insertGRCAuditRow
	insertGRCAuditRow = func(tx *gorm.DB, row *audit.AuditLog) error {
		return errGRCAuditRejected
	}
	t.Cleanup(func() { insertGRCAuditRow = previous })

	req.Code = "APP-OTHER"
	if _, err = svc.SaveAsset(0, 41, req); err == nil {
		t.Fatal("audit failure was committed")
	}
	if countModel(t, f.db, &Asset{}) != 1 || countModel(t, f.db, &audit.AuditLog{}) != 1 {
		t.Fatal("rolled-back audit failure changed the committed rows")
	}
}
