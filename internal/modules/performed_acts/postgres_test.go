package performed_acts

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/modules/act_catalog"
	"github.com/lallene/medcore-his/backend/internal/modules/billing"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const performedActsIsolationConfigError = "TEST_DATABASE_URL connection mode cannot preserve session search_path for performed_acts PG isolation; use a direct/session-mode PostgreSQL endpoint"

func performedActsDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL absent: tests PostgreSQL performed_acts ignorés")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("performed_acts_%d", time.Now().UnixNano())
	schemaIdent := pgx.Identifier{schema}.Sanitize()
	if err = admin.Exec("CREATE SCHEMA " + schemaIdent).Error; err != nil {
		t.Fatal(err)
	}

	pgConfig, err := pgx.ParseConfig(dsn)
	if err != nil {
		_ = admin.Exec("DROP SCHEMA IF EXISTS " + schemaIdent + " CASCADE")
		t.Fatal(err)
	}
	if pgConfig.RuntimeParams == nil {
		pgConfig.RuntimeParams = map[string]string{}
	}
	pgConfig.RuntimeParams["search_path"] = schemaIdent

	sqlDB := stdlib.OpenDB(*pgConfig, stdlib.OptionAfterConnect(func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET search_path TO "+schemaIdent)
		return err
	}))
	sqlDB.SetMaxOpenConns(5)
	sqlDB.SetMaxIdleConns(5)

	pingCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {
		_ = sqlDB.Close()
		_ = admin.Exec("DROP SCHEMA IF EXISTS " + schemaIdent + " CASCADE")
		msg := err.Error()
		if strings.Contains(msg, "unsupported startup parameter") || strings.Contains(strings.ToLower(msg), "search_path") {
			t.Fatal(performedActsIsolationConfigError)
		}
		t.Fatal(err)
	}

	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		_ = sqlDB.Close()
		_ = admin.Exec("DROP SCHEMA IF EXISTS " + schemaIdent + " CASCADE")
		t.Fatal(err)
	}

	adminSQL, err := admin.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
		_ = admin.Exec("DROP SCHEMA IF EXISTS " + schemaIdent + " CASCADE").Error
		_ = adminSQL.Close()
	})

	assertPerformedActsIsolation(t, db, schema)

	if err = db.AutoMigrate(&patients.Patient{}, &act_catalog.Entry{}, &Act{}, &billing.Invoice{}, &billing.InvoiceLine{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func assertPerformedActsIsolation(t *testing.T, db *gorm.DB, schema string) {
	t.Helper()
	var currentSchema string
	if err := db.Raw("SELECT current_schema()").Scan(&currentSchema).Error; err != nil {
		t.Fatalf("isolation assert current_schema: %v", err)
	}
	if currentSchema != schema {
		t.Fatalf("performed_acts isolation failed: current_schema=%q want=%q", currentSchema, schema)
	}
	var schemas []string
	if err := db.Raw("SELECT unnest(current_schemas(false))").Scan(&schemas).Error; err != nil {
		t.Fatalf("isolation assert current_schemas: %v", err)
	}
	if len(schemas) == 0 || schemas[0] != schema {
		t.Fatalf("performed_acts isolation failed: current_schemas(false)=%v want %q first", schemas, schema)
	}
}

func seedPG(t *testing.T, db *gorm.DB) (patients.Patient, act_catalog.Entry) {
	t.Helper()
	p := patients.Patient{CodePatient: "PG-P1", NumeroDossier: "PG-D1", Nom: "Doe"}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	cat := act_catalog.Entry{
		Code: "PROC-SUTURE", Label: "Suture", Description: "Suture simple", Category: "PROCEDURE",
		BasePrice: 10000, Currency: "XOF", Billable: true, InsuranceEligible: true, IsActive: true,
		CreatedBy: 1, UpdatedBy: 1,
	}
	if err := db.Create(&cat).Error; err != nil {
		t.Fatal(err)
	}
	return p, cat
}

func TestPostgresSnapshotSurvivesCatalogMutation(t *testing.T) {
	db := performedActsDB(t)
	s := NewService(db)
	p, cat := seedPG(t, db)

	act, err := s.Create(CreateRequest{PatientID: p.ID, ActCatalogEntryID: cat.ID, Quantity: 1}, 9)
	if err != nil {
		t.Fatal(err)
	}
	if act.ActCode != "PROC-SUTURE" || act.BasePrice != 10000 || !act.InsuranceEligible {
		t.Fatalf("snapshot=%+v", act)
	}

	cat.Label = "Suture complexe"
	cat.BasePrice = 99999
	cat.InsuranceEligible = false
	cat.UpdatedBy = 2
	if err := db.Save(&cat).Error; err != nil {
		t.Fatal(err)
	}

	reloaded, err := s.GetByID(act.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.ActLabel != "Suture" || reloaded.BasePrice != 10000 || !reloaded.InsuranceEligible {
		t.Fatalf("historical snapshot mutated: %+v", reloaded)
	}
}

func TestPostgresVoidLifecycle(t *testing.T) {
	db := performedActsDB(t)
	s := NewService(db)
	p, cat := seedPG(t, db)

	act, err := s.Create(CreateRequest{PatientID: p.ID, ActCatalogEntryID: cat.ID}, 3)
	if err != nil {
		t.Fatal(err)
	}
	voided, err := s.Void(act.ID, VoidRequest{Reason: "erreur de saisie"}, 4)
	if err != nil {
		t.Fatal(err)
	}
	if voided.Status != StatusVoided || voided.VoidReason == "" || voided.VoidedBy == nil {
		t.Fatalf("voided=%+v", voided)
	}
	if voided.BasePrice != 10000 {
		t.Fatal("void changed snapshot price")
	}
	if _, err := s.GetByID(act.ID); err != nil {
		t.Fatal(err)
	}
	_, err = s.Void(act.ID, VoidRequest{Reason: "again"}, 5)
	if err == nil {
		t.Fatal("second void accepted")
	}
	var app *coreerrors.AppError
	if !errors.As(err, &app) || app.Status != 409 {
		t.Fatalf("want 409, got %T %v", err, err)
	}
}

func TestPostgresQuantityAndInactiveCatalog(t *testing.T) {
	db := performedActsDB(t)
	s := NewService(db)
	p, cat := seedPG(t, db)

	if _, err := s.Create(CreateRequest{PatientID: p.ID, ActCatalogEntryID: cat.ID, Quantity: -2}, 1); err == nil {
		t.Fatal("negative quantity accepted")
	}
	db.Model(&cat).Update("is_active", false)
	if _, err := s.Create(CreateRequest{PatientID: p.ID, ActCatalogEntryID: cat.ID}, 1); err == nil {
		t.Fatal("inactive catalogue accepted")
	}
}

func TestPostgresInsuranceEligibleWithoutAuthorization(t *testing.T) {
	db := performedActsDB(t)
	s := NewService(db)
	p, cat := seedPG(t, db)
	// No insurance_authorizations table required — create must succeed.
	act, err := s.Create(CreateRequest{PatientID: p.ID, ActCatalogEntryID: cat.ID}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !act.InsuranceEligible {
		t.Fatal("expected insuranceEligible snapshot true")
	}
	var n int64
	if err := db.Table("insurance_authorizations").Count(&n).Error; err == nil && n > 0 {
		t.Fatal("unexpected authorization rows")
	}
}

func TestPostgresBillingNonMutation(t *testing.T) {
	db := performedActsDB(t)
	s := NewService(db)
	p, cat := seedPG(t, db)

	var invBefore, lineBefore int64
	db.Model(&billing.Invoice{}).Count(&invBefore)
	db.Model(&billing.InvoiceLine{}).Count(&lineBefore)

	if _, err := s.Create(CreateRequest{PatientID: p.ID, ActCatalogEntryID: cat.ID}, 1); err != nil {
		t.Fatal(err)
	}

	var invAfter, lineAfter int64
	db.Model(&billing.Invoice{}).Count(&invAfter)
	db.Model(&billing.InvoiceLine{}).Count(&lineAfter)
	if invAfter != invBefore || lineAfter != lineBefore {
		t.Fatalf("billing mutated: invoices %d→%d lines %d→%d", invBefore, invAfter, lineBefore, lineAfter)
	}
}

func TestPostgresConstraints(t *testing.T) {
	db := performedActsDB(t)
	p, cat := seedPG(t, db)
	bad := Act{
		PatientID: p.ID, ActCatalogEntryID: cat.ID, ActCode: "X", ActLabel: "X", ActCategory: "OTHER",
		BasePrice: -1, Currency: "XOF", Quantity: 1, PerformedAt: time.Now(), PerformedBy: 1,
		Status: StatusPerformed, CreatedBy: 1, UpdatedBy: 1,
	}
	if err := db.Create(&bad).Error; err == nil {
		t.Fatal("negative base price accepted at DB")
	}
	bad2 := Act{
		PatientID: p.ID, ActCatalogEntryID: cat.ID, ActCode: "X", ActLabel: "X", ActCategory: "OTHER",
		BasePrice: 0, Currency: "XOF", Quantity: 0, PerformedAt: time.Now(), PerformedBy: 1,
		Status: StatusPerformed, CreatedBy: 1, UpdatedBy: 1,
	}
	if err := db.Create(&bad2).Error; err == nil {
		t.Fatal("zero quantity accepted at DB")
	}
}
