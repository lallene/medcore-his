package cash

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/lallene/medcore-his/backend/internal/modules/billing"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type cashPatient struct {
	ID                        uint `gorm:"primaryKey"`
	Nom, Prenoms, CodePatient string
	Telephone                 string `gorm:"size:50"`
}

func (cashPatient) TableName() string { return "patients" }

type cashUser struct {
	ID   uint `gorm:"primaryKey"`
	Name string
}

func (cashUser) TableName() string { return "users" }

// cashDB opens a schema-isolated PG pool with per-connection search_path.
// DSN query search_path is not reliable with pgx (same as billingDB).
func cashDB(t *testing.T) *gorm.DB {
	t.Helper()
	d := os.Getenv("TEST_DATABASE_URL")
	if d == "" {
		t.Skip("TEST_DATABASE_URL absent: tests PostgreSQL Cash ignorés")
	}
	d = strings.Replace(d, "-pooler", "", 1)
	admin, e := gorm.Open(postgres.Open(d), &gorm.Config{})
	if e != nil {
		t.Fatal(e)
	}
	schema := fmt.Sprintf("cash_%d", time.Now().UnixNano())
	schemaIdent := pgx.Identifier{schema}.Sanitize()
	if e = admin.Exec("CREATE SCHEMA " + schemaIdent).Error; e != nil {
		t.Fatal(e)
	}
	pgConfig, e := pgx.ParseConfig(d)
	if e != nil {
		_ = admin.Exec("DROP SCHEMA IF EXISTS " + schemaIdent + " CASCADE")
		t.Fatal(e)
	}
	if pgConfig.RuntimeParams == nil {
		pgConfig.RuntimeParams = map[string]string{}
	}
	pgConfig.RuntimeParams["search_path"] = schemaIdent
	sqlDB := stdlib.OpenDB(*pgConfig, stdlib.OptionAfterConnect(func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET search_path TO "+schemaIdent)
		return err
	}))
	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetMaxIdleConns(10)
	pingCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if e = sqlDB.PingContext(pingCtx); e != nil {
		_ = sqlDB.Close()
		_ = admin.Exec("DROP SCHEMA IF EXISTS " + schemaIdent + " CASCADE")
		t.Fatal(e)
	}
	db, e := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
	if e != nil {
		_ = sqlDB.Close()
		_ = admin.Exec("DROP SCHEMA IF EXISTS " + schemaIdent + " CASCADE")
		t.Fatal(e)
	}
	adminSQL, _ := admin.DB()
	t.Cleanup(func() {
		_ = sqlDB.Close()
		if adminSQL != nil {
			_, _ = adminSQL.Exec("DROP SCHEMA IF EXISTS " + schemaIdent + " CASCADE")
			_ = adminSQL.Close()
		}
	})
	if e = db.AutoMigrate(&cashPatient{}, &cashUser{}, &Register{}, &Session{}, &billing.Invoice{}, &billing.InvoiceLine{}, &billing.Payment{}, &billing.PaymentReversal{}, &billing.CreditNote{}, &billing.CreditLedgerEntry{}, &billing.FinancialParty{}, &Receipt{}, &CashMovement{}, &CashMovementAudit{}, &CashCorrectionExecution{}, &CashCorrectionExecutionAudit{}); e != nil {
		t.Fatal(e)
	}
	if e = EnsureReceiptSessionNullable(db); e != nil {
		t.Fatal(e)
	}
	if e = db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS ux_cash_sessions_open_register ON cash_sessions(cash_register_id) WHERE status='OPEN'").Error; e != nil {
		t.Fatal(e)
	}
	if e = db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS ux_cash_movements_payment_reversal_ref ON cash_movements (reference_type, reference_id) WHERE reference_type = 'PAYMENT_REVERSAL' AND reference_id IS NOT NULL").Error; e != nil {
		t.Fatal(e)
	}
	if e = db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS ux_cash_movements_correction_exec_ref ON cash_movements (reference_type, reference_id) WHERE reference_type = 'CASH_CORRECTION_EXECUTION' AND reference_id IS NOT NULL").Error; e != nil {
		t.Fatal(e)
	}
	return db
}
func TestPostgresCashLifecycle(t *testing.T) {
	db := cashDB(t)
	db.Create(&cashUser{ID: 9, Name: "Caissier Test"})
	db.Create(&cashPatient{ID: 2, Nom: "Patient", Prenoms: "Test", CodePatient: "P-CASH"})
	s := NewService(db)
	reg, e := s.SaveRegister(0, RegisterRequest{Code: "CASH-1", Name: "Principale"}, 9)
	if e != nil {
		t.Fatal(e)
	}
	session, e := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 50000, IdempotencyKey: "open-cash-1"}, 9)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Open(OpenRequest{CashRegisterID: reg.ID, IdempotencyKey: "open-cash-2"}, 9); e == nil {
		t.Fatal("double open accepted")
	}
	inv := billing.Invoice{Number: "INV-CASH", PatientID: 2, Status: billing.InvoiceIssued, GrossAmount: 50000, InsuranceAmount: 35000, PatientAmount: 15000, BalanceAmount: 15000, CreatedBy: 9, UpdatedBy: 9}
	db.Create(&inv)
	req := PaymentRequest{InvoiceID: inv.ID, Amount: 5000, PaymentMethod: "CASH", IdempotencyKey: "cash-key", Payer: &PayerRequest{Mode: "PATIENT"}}
	rec, e := s.Pay(session.Session.ID, req, 9)
	if e != nil {
		t.Fatal(e)
	}
	if rec.ReceiptNumber != "REC-000001" || rec.PaidBefore != 0 || rec.BalanceAfter != 10000 || rec.InsuranceAmount != 35000 {
		t.Fatalf("receipt=%+v", rec)
	}
	again, e := s.Pay(session.Session.ID, req, 9)
	if e != nil || again.ID != rec.ID {
		t.Fatal("idempotency")
	}
	summary, _ := s.Get(session.Session.ID)
	if summary.ExpectedCash != 55000 || summary.OperationCount != 1 {
		t.Fatalf("summary=%+v", summary)
	}
	if _, e = s.Close(session.Session.ID, CloseRequest{CountedCashAmount: 53000, IdempotencyKey: "close-short"}, 9, false); e == nil {
		t.Fatal("missing justification")
	}
	closed, e := s.Close(session.Session.ID, CloseRequest{CountedCashAmount: 55000, IdempotencyKey: "close-ok"}, 9, false)
	if e != nil || *closed.Session.CashDifference != 0 {
		t.Fatal(e)
	}
	if _, e = s.Pay(session.Session.ID, PaymentRequest{InvoiceID: inv.ID, Amount: 10000, PaymentMethod: "CASH", IdempotencyKey: "late", Payer: &PayerRequest{Mode: "PATIENT"}}, 9); e == nil {
		t.Fatal("closed payment")
	}
}

func TestPostgresConcurrentCashIdempotence(t *testing.T) {
	db := cashDB(t)
	db.Create(&cashUser{ID: 9, Name: "Cashier"})
	db.Create(&cashPatient{ID: 2, Nom: "P", Prenoms: "C", CodePatient: "PC"})
	s := NewService(db)
	reg, _ := s.SaveRegister(0, RegisterRequest{Code: "CONC", Name: "Concurrent"}, 9)
	session, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, IdempotencyKey: "open-conc"}, 9)
	inv := billing.Invoice{Number: "INV-CONC", PatientID: 2, Status: billing.InvoiceIssued, GrossAmount: 10000, PatientAmount: 10000, BalanceAmount: 10000, CreatedBy: 9, UpdatedBy: 9}
	db.Create(&inv)
	req := PaymentRequest{InvoiceID: inv.ID, Amount: 5000, PaymentMethod: "CASH", IdempotencyKey: "same-key", Payer: &PayerRequest{Mode: "PATIENT"}}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := s.Pay(session.Session.ID, req, 9); errs <- e }()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	var payments, receipts int64
	db.Model(&billing.Payment{}).Count(&payments)
	db.Model(&Receipt{}).Count(&receipts)
	if payments != 1 || receipts != 1 {
		t.Fatalf("payments=%d receipts=%d", payments, receipts)
	}
}

func TestLOT29D_B_CashSessionReceiptUnchanged(t *testing.T) {
	// RB13 — cash-session payment → receipt remains session-bound after billing convergence.
	db := cashDB(t)
	db.Create(&cashUser{ID: 9, Name: "Caissier Test"})
	db.Create(&cashPatient{ID: 2, Nom: "Patient", Prenoms: "Test", CodePatient: "P-CASH"})
	s := NewService(db)
	reg, e := s.SaveRegister(0, RegisterRequest{Code: "RB13", Name: "Principale"}, 9)
	if e != nil {
		t.Fatal(e)
	}
	session, e := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 50000, IdempotencyKey: "open-rb13"}, 9)
	if e != nil {
		t.Fatal(e)
	}
	inv := billing.Invoice{Number: "INV-RB13", PatientID: 2, Status: billing.InvoiceIssued, GrossAmount: 50000, PatientAmount: 15000, BalanceAmount: 15000, CreatedBy: 9, UpdatedBy: 9}
	db.Create(&inv)
	rec, e := s.Pay(session.Session.ID, PaymentRequest{InvoiceID: inv.ID, Amount: 5000, PaymentMethod: "CASH", IdempotencyKey: "rb13", Payer: &PayerRequest{Mode: "PATIENT"}}, 9)
	if e != nil {
		t.Fatal(e)
	}
	if rec.CashSessionID == nil || *rec.CashSessionID != session.Session.ID {
		t.Fatalf("RB13 session %+v", rec)
	}
	if rec.RegisterCode != "RB13" || rec.RegisterName != "Principale" || rec.ReceiptNumber != "REC-000001" {
		t.Fatalf("RB13 receipt %+v", rec)
	}
	again, e := s.Receipt(rec.ID)
	if e != nil || again.ID != rec.ID || again.ReceiptNumber != rec.ReceiptNumber {
		t.Fatalf("RB14 reprint/read mutated %+v %v", again, e)
	}
	var n int64
	db.Model(&Receipt{}).Count(&n)
	if n != 1 {
		t.Fatalf("RB14 receipts=%d", n)
	}
}
