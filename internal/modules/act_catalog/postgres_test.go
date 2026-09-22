package act_catalog

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
	"github.com/lallene/medcore-his/backend/internal/modules/billing"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const actCatalogIsolationConfigError = "TEST_DATABASE_URL connection mode cannot preserve session search_path for act_catalog PG isolation; use a direct/session-mode PostgreSQL endpoint"

// actCatalogDB opens an ephemeral schema for act_catalog PG tests.
// Isolation: RuntimeParams search_path + AfterConnect SET + fail-fast assert.
// Never prints DSN / credentials.
func actCatalogDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL absent: tests PostgreSQL act_catalog ignorés")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("act_catalog_%d", time.Now().UnixNano())
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
			t.Fatal(actCatalogIsolationConfigError)
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

	assertActCatalogIsolation(t, db, schema)

	if err = db.AutoMigrate(&Entry{}, &billing.Invoice{}, &billing.InvoiceLine{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func assertActCatalogIsolation(t *testing.T, db *gorm.DB, schema string) {
	t.Helper()
	var currentSchema string
	if err := db.Raw("SELECT current_schema()").Scan(&currentSchema).Error; err != nil {
		t.Fatalf("isolation assert current_schema: %v", err)
	}
	if currentSchema != schema {
		t.Fatalf("act_catalog isolation failed: current_schema=%q want=%q", currentSchema, schema)
	}
	var schemas []string
	if err := db.Raw("SELECT unnest(current_schemas(false))").Scan(&schemas).Error; err != nil {
		t.Fatalf("isolation assert current_schemas: %v", err)
	}
	if len(schemas) == 0 || schemas[0] != schema {
		t.Fatalf("act_catalog isolation failed: current_schemas(false)=%v want %q first", schemas, schema)
	}
}

func boolPtr(v bool) *bool { return &v }

func TestPostgresCreateGetUpdateLifecycle(t *testing.T) {
	db := actCatalogDB(t)
	s := NewService(db)

	created, err := s.Create(CreateRequest{
		Code: "  cons-gen ", Label: "Consultation générale", Category: "consultation",
		BasePrice: 5000, Description: "CS",
	}, 7)
	if err != nil {
		t.Fatal(err)
	}
	if created.Code != "CONS-GEN" || created.Category != "CONSULTATION" || created.Currency != "XOF" {
		t.Fatalf("created=%+v", created)
	}
	if !created.Billable || !created.InsuranceEligible || !created.IsActive {
		t.Fatalf("defaults=%+v", created)
	}

	got, err := s.GetByID(created.ID)
	if err != nil || got.Label != "Consultation générale" {
		t.Fatalf("get=%+v %v", got, err)
	}

	updated, err := s.Update(created.ID, UpdateRequest{
		Label: "Consultation générale (maj)", Category: "CONSULTATION", BasePrice: 7500,
		Billable: boolPtr(true), InsuranceEligible: boolPtr(false), IsActive: boolPtr(false),
	}, 8)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Code != "CONS-GEN" {
		t.Fatal("code mutated")
	}
	if updated.BasePrice != 7500 || updated.IsActive || updated.InsuranceEligible {
		t.Fatalf("updated=%+v", updated)
	}

	// Inactive remains readable by id.
	if _, err := s.GetByID(created.ID); err != nil {
		t.Fatal(err)
	}

	// Default list excludes inactive.
	page, err := s.List(ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range page.Data {
		if row.ID == created.ID {
			t.Fatal("inactive row in default list")
		}
	}

	inactive := false
	page, err = s.List(ListFilter{Active: &inactive})
	if err != nil || page.Total != 1 {
		t.Fatalf("inactive filter total=%d err=%v", page.Total, err)
	}

	// Reactivate.
	_, err = s.Update(created.ID, UpdateRequest{
		Label: updated.Label, Category: "CONSULTATION", BasePrice: 7500, IsActive: boolPtr(true),
		InsuranceEligible: boolPtr(false),
	}, 9)
	if err != nil {
		t.Fatal(err)
	}
	page, err = s.List(ListFilter{Search: "CONS-GEN", Category: "CONSULTATION"})
	if err != nil || page.Total != 1 {
		t.Fatalf("search total=%d err=%v", page.Total, err)
	}
}

func TestPostgresDuplicateCodeConflict(t *testing.T) {
	db := actCatalogDB(t)
	s := NewService(db)
	if _, err := s.Create(CreateRequest{Code: "LAB-NFS", Label: "NFS", Category: "LABORATORY", BasePrice: 3000}, 1); err != nil {
		t.Fatal(err)
	}
	_, err := s.Create(CreateRequest{Code: "lab-nfs", Label: "NFS dup", Category: "LABORATORY", BasePrice: 3000}, 1)
	if err == nil {
		t.Fatal("duplicate accepted")
	}
	var app *coreerrors.AppError
	if !errors.As(err, &app) || app.Status != 409 {
		t.Fatalf("want 409 AppError, got %T %v", err, err)
	}
}

func TestPostgresNegativePriceRejected(t *testing.T) {
	db := actCatalogDB(t)
	s := NewService(db)
	if _, err := s.Create(CreateRequest{Code: "NEG", Label: "Neg", Category: "OTHER", BasePrice: -5}, 1); err == nil {
		t.Fatal("negative price accepted at service")
	}
	// DB boundary: bypass service validation.
	item := Entry{
		Code: "NEG-DB", Label: "Neg DB", Category: "OTHER", BasePrice: -1, Currency: "XOF",
		Billable: true, InsuranceEligible: true, IsActive: true, CreatedBy: 1, UpdatedBy: 1,
	}
	if err := db.Create(&item).Error; err == nil {
		t.Fatal("negative price accepted at DB")
	}
}

func TestPostgresInvalidCategoryRejected(t *testing.T) {
	db := actCatalogDB(t)
	s := NewService(db)
	if _, err := s.Create(CreateRequest{Code: "MED-X", Label: "Med", Category: "MEDICATION", BasePrice: 0}, 1); err == nil {
		t.Fatal("MEDICATION accepted")
	}
	item := Entry{
		Code: "BAD-CAT", Label: "Bad", Category: "MEDICATION", BasePrice: 0, Currency: "XOF",
		Billable: true, InsuranceEligible: true, IsActive: true, CreatedBy: 1, UpdatedBy: 1,
	}
	if err := db.Create(&item).Error; err == nil {
		t.Fatal("invalid category accepted at DB")
	}
}

func TestPostgresBasePriceChangeDoesNotMutateInvoiceLines(t *testing.T) {
	db := actCatalogDB(t)
	s := NewService(db)

	entry, err := s.Create(CreateRequest{Code: "PROC-1", Label: "Suture", Category: "PROCEDURE", BasePrice: 10000}, 1)
	if err != nil {
		t.Fatal(err)
	}

	// Standalone invoice line snapshot (no ActCatalog FK — billing remains independent).
	inv := billing.Invoice{
		Number: "INV-ACTCAT-1", PatientID: 1, Status: billing.InvoiceDraft,
		GrossAmount: 10000, InsuranceAmount: 0, PatientAmount: 10000, PaidAmount: 0, BalanceAmount: 10000,
		CreatedBy: 1, UpdatedBy: 1,
	}
	if err := db.Create(&inv).Error; err != nil {
		t.Fatal(err)
	}
	line := billing.InvoiceLine{
		InvoiceID: inv.ID, TariffID: 1, ActType: "PROCEDURE", ReferenceID: 1, ClinicalReferenceID: 1,
		BillableKey: "PROCEDURE:1", Description: "Suture", Quantity: 1, UnitPrice: 10000,
		GrossAmount: 10000, InsuranceAmount: 0, PatientAmount: 10000, CoverageResolution: "NONE", IsActive: true,
	}
	if err := db.Create(&line).Error; err != nil {
		t.Fatal(err)
	}

	if _, err := s.Update(entry.ID, UpdateRequest{Label: "Suture", Category: "PROCEDURE", BasePrice: 99999}, 2); err != nil {
		t.Fatal(err)
	}

	var reloaded billing.InvoiceLine
	if err := db.First(&reloaded, line.ID).Error; err != nil {
		t.Fatal(err)
	}
	if reloaded.UnitPrice != 10000 {
		t.Fatalf("invoice line unit price mutated: %d", reloaded.UnitPrice)
	}
}
