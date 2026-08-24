package cash

import (
	"fmt"
	"github.com/lallene/medcore-his/backend/internal/modules/billing"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type cashPatient struct {
	ID                        uint `gorm:"primaryKey"`
	Nom, Prenoms, CodePatient string
}

func (cashPatient) TableName() string { return "patients" }

type cashUser struct {
	ID   uint `gorm:"primaryKey"`
	Name string
}

func (cashUser) TableName() string { return "users" }
func cashDSN(d, s string) string {
	if strings.Contains(d, "://") {
		x := "?"
		if strings.Contains(d, "?") {
			x = "&"
		}
		return d + x + "search_path=" + url.QueryEscape(s)
	}
	return d + " search_path=" + s
}
func cashDB(t *testing.T) *gorm.DB {
	d := os.Getenv("TEST_DATABASE_URL")
	if d == "" {
		t.Skip("TEST_DATABASE_URL absent: tests PostgreSQL Cash ignorés")
	}
	admin, e := gorm.Open(postgres.Open(d), &gorm.Config{})
	if e != nil {
		t.Fatal(e)
	}
	schema := fmt.Sprintf("cash_%d", time.Now().UnixNano())
	if e = admin.Exec(`CREATE SCHEMA "` + schema + `"`).Error; e != nil {
		t.Fatal(e)
	}
	db, e := gorm.Open(postgres.Open(cashDSN(d, schema)), &gorm.Config{})
	if e != nil {
		t.Fatal(e)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { sqlDB.Close(); admin.Exec(`DROP SCHEMA IF EXISTS "` + schema + `" CASCADE`) })
	if e = db.AutoMigrate(&cashPatient{}, &cashUser{}, &Register{}, &Session{}, &billing.Invoice{}, &billing.Payment{}, &Receipt{}); e != nil {
		t.Fatal(e)
	}
	if e = db.Exec("CREATE UNIQUE INDEX ux_cash_sessions_open_register ON cash_sessions(cash_register_id) WHERE status='OPEN'").Error; e != nil {
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
	session, e := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 50000}, 9)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Open(OpenRequest{CashRegisterID: reg.ID}, 9); e == nil {
		t.Fatal("double open accepted")
	}
	inv := billing.Invoice{Number: "INV-CASH", PatientID: 2, Status: billing.InvoiceIssued, GrossAmount: 50000, InsuranceAmount: 35000, PatientAmount: 15000, BalanceAmount: 15000, CreatedBy: 9, UpdatedBy: 9}
	db.Create(&inv)
	req := PaymentRequest{InvoiceID: inv.ID, Amount: 5000, PaymentMethod: "CASH", IdempotencyKey: "cash-key"}
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
	if _, e = s.Close(session.Session.ID, CloseRequest{CountedCashAmount: 53000}, 9); e == nil {
		t.Fatal("missing justification")
	}
	closed, e := s.Close(session.Session.ID, CloseRequest{CountedCashAmount: 55000}, 9)
	if e != nil || *closed.Session.CashDifference != 0 {
		t.Fatal(e)
	}
	if _, e = s.Pay(session.Session.ID, PaymentRequest{InvoiceID: inv.ID, Amount: 10000, PaymentMethod: "CASH", IdempotencyKey: "late"}, 9); e == nil {
		t.Fatal("closed payment")
	}
}

func TestPostgresConcurrentCashIdempotence(t *testing.T) {
	db := cashDB(t)
	db.Create(&cashUser{ID: 9, Name: "Cashier"})
	db.Create(&cashPatient{ID: 2, Nom: "P", Prenoms: "C", CodePatient: "PC"})
	s := NewService(db)
	reg, _ := s.SaveRegister(0, RegisterRequest{Code: "CONC", Name: "Concurrent"}, 9)
	session, _ := s.Open(OpenRequest{CashRegisterID: reg.ID}, 9)
	inv := billing.Invoice{Number: "INV-CONC", PatientID: 2, Status: billing.InvoiceIssued, GrossAmount: 10000, PatientAmount: 10000, BalanceAmount: 10000, CreatedBy: 9, UpdatedBy: 9}
	db.Create(&inv)
	req := PaymentRequest{InvoiceID: inv.ID, Amount: 5000, PaymentMethod: "CASH", IdempotencyKey: "same-key"}
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
