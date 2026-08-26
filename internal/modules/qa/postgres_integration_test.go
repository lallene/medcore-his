package qa

import (
	"fmt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func qaPostgres(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL absent: tests PostgreSQL QA ignorés")
	}
	admin, e := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if e != nil {
		t.Fatal(e)
	}
	schema := fmt.Sprintf("qa_%d", time.Now().UnixNano())
	if e = admin.Exec(`CREATE SCHEMA "` + schema + `"`).Error; e != nil {
		t.Fatal(e)
	}
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	db, e := gorm.Open(postgres.Open(dsn+sep+"search_path="+url.QueryEscape(schema)), &gorm.Config{})
	if e != nil {
		t.Fatal(e)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { sqlDB.Close(); admin.Exec(`DROP SCHEMA IF EXISTS "` + schema + `" CASCADE`) })
	if e = db.AutoMigrate(&Campaign{}, &TestResult{}, &Artifact{}); e != nil {
		t.Fatal(e)
	}
	return db
}
func TestPostgresQAForeignKeysUniquenessAndRollback(t *testing.T) {
	db := qaPostgres(t)
	now := time.Now()
	campaign := Campaign{RunID: "RUN-1", Environment: "ci", StartedAt: now, Status: StatusRunning}
	if e := db.Create(&campaign).Error; e != nil {
		t.Fatal(e)
	}
	if e := db.Create(&Campaign{RunID: "RUN-1", Environment: "ci", StartedAt: now, Status: StatusRunning}).Error; e == nil {
		t.Fatal("run_id uniqueness missing")
	}
	if e := db.Create(&TestResult{CampaignID: 999999, Suite: "smoke", TestKey: "X", Title: "X", Status: StatusPassed}).Error; e == nil {
		t.Fatal("campaign foreign key missing")
	}
	e := db.Transaction(func(tx *gorm.DB) error {
		if x := tx.Create(&TestResult{CampaignID: campaign.ID, Suite: "smoke", TestKey: "ROLLBACK", Title: "rollback", Status: StatusFailed}).Error; x != nil {
			return x
		}
		return fmt.Errorf("forced")
	})
	if e == nil {
		t.Fatal("forced rollback absent")
	}
	var n int64
	db.Model(&TestResult{}).Where("test_key='ROLLBACK'").Count(&n)
	if n != 0 {
		t.Fatal("rollback failed")
	}
}
