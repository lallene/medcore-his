package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/lallene/medcore-his/backend/internal/testsupport/pgtest"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// migrateSchemaDB opens an empty, schema-isolated PostgreSQL database for cmd/migrate.
func migrateSchemaDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := strings.Replace(pgtest.DSN(t), "-pooler", "", 1)
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("migrate_%d", time.Now().UnixNano())
	ident := pgx.Identifier{schema}.Sanitize()
	if err := admin.Exec("CREATE SCHEMA " + ident).Error; err != nil {
		t.Fatal(err)
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RuntimeParams == nil {
		cfg.RuntimeParams = map[string]string{}
	}
	cfg.RuntimeParams["search_path"] = ident
	sqlDB := stdlib.OpenDB(*cfg, stdlib.OptionAfterConnect(func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET search_path TO "+ident)
		return err
	}))
	sqlDB.SetMaxOpenConns(4)
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	adminSQL, _ := admin.DB()
	t.Cleanup(func() {
		_ = sqlDB.Close()
		_ = admin.Exec("DROP SCHEMA IF EXISTS " + ident + " CASCADE").Error
		_ = adminSQL.Close()
	})
	return db
}

// LOT30-A′ — cmd/migrate creates every LOT29 financial structure on an empty database,
// is idempotent, and the new CHECK constraints are enforced by PostgreSQL.
func TestLOT30APrime_MigrationsCreateFinancialSchema(t *testing.T) {
	db := migrateSchemaDB(t)
	if err := applyMigrations(db); err != nil {
		t.Fatalf("first migration: %v", err)
	}
	if err := applyMigrations(db); err != nil {
		t.Fatalf("second migration must be idempotent: %v", err)
	}

	for _, table := range []string{
		"billing_invoices", "billing_payments", "billing_payment_reversals", "billing_financial_parties",
		"billing_credit_notes", "billing_credit_ledger_entries", "billing_credit_applications",
		"billing_credit_application_reversals", "billing_refunds", "billing_refund_executions",
		"billing_document_number_series", "cash_sessions", "cash_receipts", "cash_movements",
		"cash_correction_executions", "cash_correction_execution_audits", "performed_acts",
		"insurance_authorizations", "insurance_settlements",
	} {
		var ok bool
		if err := db.Raw("SELECT to_regclass(current_schema() || '.' || ?) IS NOT NULL", table).Scan(&ok).Error; err != nil || !ok {
			t.Errorf("table %s missing (err=%v)", table, err)
		}
	}
	for _, idx := range []string{
		"ux_billing_active_billable_key", "ux_cash_sessions_open_register",
		"ux_cash_movements_payment_reversal_ref", "ux_cash_movements_correction_exec_ref",
		"ux_cash_movements_refund_exec_ref", "ux_billing_refund_exec_external_ref",
		"ux_billing_refunds_refund_number", "ux_performed_acts_source",
		"ux_insurance_authorizations_active_reference", "ux_credit_ledger_source",
		"ux_doc_number_series_type_year",
	} {
		var n int64
		db.Raw("SELECT COUNT(*) FROM pg_indexes WHERE schemaname = current_schema() AND indexname = ?", idx).Scan(&n)
		if n != 1 {
			t.Errorf("index %s missing", idx)
		}
	}
	for _, con := range []string{
		"billing_invoice_status_valid", "billing_payment_payer_provenance_valid",
		"billing_credit_ledger_entry_type_valid", "billing_refund_status_valid",
		"billing_refund_execution_method_valid", "billing_financial_party_kind_valid",
		"cash_session_status_valid", "cash_movement_direction_valid",
		"billing_payment_cash_requires_session",
	} {
		var n int64
		db.Raw(`SELECT COUNT(*) FROM pg_constraint c JOIN pg_namespace n ON n.oid = c.connamespace
			WHERE n.nspname = current_schema() AND c.conname = ?`, con).Scan(&n)
		if n != 1 {
			t.Errorf("constraint %s missing", con)
		}
	}

	// DB-enforced: a new sessionless CASH payment and an unknown ledger entry type are rejected.
	expectViolation := func(name, stmt string, args ...any) {
		t.Helper()
		err := db.Exec(stmt, args...).Error
		if err == nil || !strings.Contains(err.Error(), "23514") {
			t.Errorf("%s: expected CHECK violation, got %v", name, err)
		}
	}
	if err := db.Exec(`INSERT INTO billing_invoices (number, patient_id, status, gross_amount, insurance_amount, patient_amount, paid_amount, balance_amount, created_by, updated_by, created_at, updated_at)
		VALUES ('INV-MIG-1', 1, 'ISSUED', 100, 0, 100, 0, 100, 1, 1, now(), now())`).Error; err != nil {
		t.Fatal(err)
	}
	var invoiceID uint
	db.Raw("SELECT id FROM billing_invoices WHERE number='INV-MIG-1'").Scan(&invoiceID)
	expectViolation("sessionless CASH", `INSERT INTO billing_payments (invoice_id, amount, payment_method, idempotency_key, paid_at, received_by, payer_provenance, created_at)
		VALUES (?, 100, 'CASH', 'mig-cash-1', now(), 1, 'CAPTURED', now())`, invoiceID)
	if err := db.Exec(`INSERT INTO billing_payments (invoice_id, amount, payment_method, idempotency_key, paid_at, received_by, payer_provenance, created_at)
		VALUES (?, 100, 'CARD', 'mig-card-1', now(), 1, 'CAPTURED', now())`, invoiceID).Error; err != nil {
		t.Fatalf("sessionless CARD must stay allowed: %v", err)
	}
	expectViolation("ledger entry type", `INSERT INTO billing_credit_ledger_entries (holder_party_id, patient_id, entry_type, amount, source_type, source_id, created_by, created_at, idempotency_key)
		VALUES (1, 1, 'BOGUS', 10, 'TEST', 1, 1, now(), 'mig-ledger-1')`)
	expectViolation("invoice status", `UPDATE billing_invoices SET status='REFUNDED' WHERE id=?`, invoiceID)
}

// Historical sessionless CASH rows survive the NOT VALID constraint (never backfilled).
func TestLOT30APrime_CashConstraintKeepsLegacyRows(t *testing.T) {
	db := migrateSchemaDB(t)
	if err := applyMigrations(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ALTER TABLE billing_payments DROP CONSTRAINT billing_payment_cash_requires_session").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO billing_invoices (number, patient_id, status, gross_amount, insurance_amount, patient_amount, paid_amount, balance_amount, created_by, updated_by, created_at, updated_at)
		VALUES ('INV-MIG-L', 1, 'PAID', 100, 0, 100, 100, 0, 1, 1, now(), now())`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO billing_payments (invoice_id, amount, payment_method, idempotency_key, paid_at, received_by, payer_provenance, created_at)
		SELECT id, 100, 'CASH', 'legacy-cash-1', now(), 1, 'LEGACY_UNCONFIRMED', now() FROM billing_invoices WHERE number='INV-MIG-L'`).Error; err != nil {
		t.Fatal(err)
	}
	// Re-running the migration over legacy data must succeed and keep the row.
	if err := applyMigrations(db); err != nil {
		t.Fatalf("migration over legacy sessionless CASH: %v", err)
	}
	var legacy int64
	db.Raw("SELECT COUNT(*) FROM billing_payments WHERE idempotency_key='legacy-cash-1' AND cash_session_id IS NULL").Scan(&legacy)
	if legacy != 1 {
		t.Fatal("legacy sessionless CASH row must be preserved")
	}
	var validated bool
	db.Raw(`SELECT convalidated FROM pg_constraint c JOIN pg_namespace n ON n.oid = c.connamespace
		WHERE n.nspname = current_schema() AND c.conname = 'billing_payment_cash_requires_session'`).Scan(&validated)
	if validated {
		t.Fatal("constraint must be NOT VALID (historical rows untouched)")
	}
}
