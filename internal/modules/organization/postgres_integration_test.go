package organization

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func organizationPostgres(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL absent: tests PostgreSQL Organization ignorés")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("organization_%d", time.Now().UnixNano())
	if err = admin.Exec(`CREATE SCHEMA "` + schema + `"`).Error; err != nil {
		t.Fatal(err)
	}
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	scoped := dsn + sep + "search_path=" + url.QueryEscape(schema)
	db, err := gorm.Open(postgres.Open(scoped), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { sqlDB.Close(); admin.Exec(`DROP SCHEMA IF EXISTS "` + schema + `" CASCADE`) })
	if err = db.AutoMigrate(&Department{}, &Service{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestPostgresOrganizationConstraintsAndRollback(t *testing.T) {
	db := organizationPostgres(t)
	units, err := SeedReference(db, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 20 {
		t.Fatalf("services=%d", len(units))
	}
	duplicate := Service{DepartmentID: units["URG"].DepartmentID, Code: "URG", Name: "duplicate", ServiceType: TypeOther, Active: true, CreatedBy: 1, UpdatedBy: 1}
	if err = db.Create(&duplicate).Error; err == nil {
		t.Fatal("unique service code not enforced")
	}
	bad := Service{DepartmentID: 999999, Code: "BAD", Name: "bad", ServiceType: TypeOther, Active: true, CreatedBy: 1, UpdatedBy: 1}
	if err = db.Create(&bad).Error; err == nil {
		t.Fatal("department foreign key not enforced")
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		x := Department{Code: "ROLLBACK", Name: "Rollback", Active: true, CreatedBy: 1, UpdatedBy: 1}
		if e := tx.Create(&x).Error; e != nil {
			return e
		}
		return fmt.Errorf("forced rollback")
	})
	if err == nil {
		t.Fatal("rollback error absent")
	}
	var n int64
	db.Model(&Department{}).Where("code='ROLLBACK'").Count(&n)
	if n != 0 {
		t.Fatal("transaction was not rolled back")
	}
}
