package cash

import (
	"errors"
	"testing"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/modules/billing"
	"gorm.io/gorm"
)

// LOT30-A′ — physical CASH must enter an OPEN CashSession exactly once.
func TestLOT30APrime_CashSessionAuthority(t *testing.T) {
	db := cashDB(t)
	db.Create(&cashUser{ID: 31, Name: "Caissier A"})
	db.Create(&cashUser{ID: 32, Name: "Caissier B"})
	db.Create(&cashPatient{ID: 7, Nom: "Cash", Prenoms: "Pat", CodePatient: "P-30AP"})
	s := NewService(db)
	bill := billing.NewService(db)

	open := func(t *testing.T, code string, float int64, user uint) *SessionSummary {
		t.Helper()
		reg, e := s.SaveRegister(0, RegisterRequest{Code: code, Name: code}, user)
		if e != nil {
			t.Fatal(e)
		}
		out, e := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: float, IdempotencyKey: code + "-open"}, user)
		if e != nil {
			t.Fatal(e)
		}
		return out
	}
	counts := func(t *testing.T, invoiceID uint) (payments, receipts int64) {
		t.Helper()
		db.Model(&billing.Payment{}).Where("invoice_id=?", invoiceID).Count(&payments)
		db.Model(&Receipt{}).Where("invoice_id=?", invoiceID).Count(&receipts)
		return
	}
	appCode := func(e error) string {
		var app *coreerrors.AppError
		if errors.As(e, &app) {
			return app.Code
		}
		return ""
	}

	t.Run("CASH01_sessionless_cash_rejected", func(t *testing.T) {
		inv := seedCashInvoice(t, db, "INV-30AP-01", 7, 5000, 31)
		_, e := bill.Pay(inv.ID, billing.PaymentRequest{Amount: 5000, PaymentMethod: "CASH", IdempotencyKey: "30ap-01", Payer: billing.PatientPayerRequest()}, 31)
		if appCode(e) != billing.CodePaymentCashSessionRequired {
			t.Fatalf("sessionless CASH must be rejected, got %v", e)
		}
		if p, r := counts(t, inv.ID); p != 0 || r != 0 {
			t.Fatalf("no payment/receipt expected, got %d/%d", p, r)
		}
		// Non-cash sessionless collection keeps its historical behavior.
		if _, e := bill.Pay(inv.ID, billing.PaymentRequest{Amount: 5000, PaymentMethod: "CARD", IdempotencyKey: "30ap-01-card", Payer: billing.PatientPayerRequest()}, 31); e != nil {
			t.Fatalf("sessionless CARD must stay accepted: %v", e)
		}
	})

	t.Run("CASH02_open_session_cash_once", func(t *testing.T) {
		sess := open(t, "30AP-02", 10000, 31)
		inv := seedCashInvoice(t, db, "INV-30AP-02", 7, 6000, 31)
		rec, e := s.Pay(sess.Session.ID, PaymentRequest{InvoiceID: inv.ID, Amount: 6000, PaymentMethod: "CASH", IdempotencyKey: "30ap-02", Payer: &PayerRequest{Mode: "PATIENT"}}, 31)
		if e != nil {
			t.Fatal(e)
		}
		if rec.CashSessionID == nil || *rec.CashSessionID != sess.Session.ID {
			t.Fatalf("receipt must belong to session: %#v", rec.CashSessionID)
		}
		var pay billing.Payment
		db.Where("invoice_id=?", inv.ID).First(&pay)
		if pay.CashSessionID == nil || *pay.CashSessionID != sess.Session.ID {
			t.Fatal("payment must be attached to the session")
		}
		sum, _ := s.Get(sess.Session.ID)
		if sum.CashCollected != 6000 || sum.ExpectedCash != 16000 || sum.OperationCount != 1 {
			t.Fatalf("cash=%d expected=%d ops=%d", sum.CashCollected, sum.ExpectedCash, sum.OperationCount)
		}
		if p, r := counts(t, inv.ID); p != 1 || r != 1 {
			t.Fatalf("payments/receipts=%d/%d", p, r)
		}
	})

	t.Run("CASH03_closed_session_rejected", func(t *testing.T) {
		sess := open(t, "30AP-03", 0, 31)
		if _, e := s.Close(sess.Session.ID, CloseRequest{CountedCashAmount: 0, IdempotencyKey: "30ap-03-close"}, 31, false); e != nil {
			t.Fatal(e)
		}
		inv := seedCashInvoice(t, db, "INV-30AP-03", 7, 2000, 31)
		if _, e := s.Pay(sess.Session.ID, PaymentRequest{InvoiceID: inv.ID, Amount: 2000, PaymentMethod: "CASH", IdempotencyKey: "30ap-03", Payer: &PayerRequest{Mode: "PATIENT"}}, 31); !cashIsConflict(e) {
			t.Fatalf("closed session must reject, got %v", e)
		}
		if p, r := counts(t, inv.ID); p != 0 || r != 0 {
			t.Fatalf("no payment/receipt expected, got %d/%d", p, r)
		}
	})

	t.Run("CASH04_other_cashier_session_forbidden", func(t *testing.T) {
		sess := open(t, "30AP-04", 0, 31)
		inv := seedCashInvoice(t, db, "INV-30AP-04", 7, 2000, 32)
		if _, e := s.Pay(sess.Session.ID, PaymentRequest{InvoiceID: inv.ID, Amount: 2000, PaymentMethod: "CASH", IdempotencyKey: "30ap-04", Payer: &PayerRequest{Mode: "PATIENT"}}, 32); !cashIsForbidden(e) {
			t.Fatalf("only the session opener may collect, got %v", e)
		}
		sum, _ := s.Get(sess.Session.ID)
		if sum.CashCollected != 0 || sum.OperationCount != 0 {
			t.Fatalf("foreign attempt leaked into drawer: %#v", sum)
		}
	})

	t.Run("CASH05_idempotent_retry_single_effect", func(t *testing.T) {
		sess := open(t, "30AP-05", 1000, 31)
		inv := seedCashInvoice(t, db, "INV-30AP-05", 7, 3000, 31)
		req := PaymentRequest{InvoiceID: inv.ID, Amount: 3000, PaymentMethod: "CASH", IdempotencyKey: "30ap-05", Payer: &PayerRequest{Mode: "PATIENT"}}
		first, e := s.Pay(sess.Session.ID, req, 31)
		if e != nil {
			t.Fatal(e)
		}
		second, e := s.Pay(sess.Session.ID, req, 31)
		if e != nil || second.ID != first.ID {
			t.Fatalf("replay must return same receipt: %v %v", second, e)
		}
		sum, _ := s.Get(sess.Session.ID)
		if sum.CashCollected != 3000 || sum.ExpectedCash != 4000 || sum.OperationCount != 1 {
			t.Fatalf("cash=%d expected=%d ops=%d", sum.CashCollected, sum.ExpectedCash, sum.OperationCount)
		}
		if p, r := counts(t, inv.ID); p != 1 || r != 1 {
			t.Fatalf("payments/receipts=%d/%d", p, r)
		}
	})

	t.Run("CASH06_rollback_atomic", func(t *testing.T) {
		sess := open(t, "30AP-06", 500, 31)
		inv := seedCashInvoice(t, db, "INV-30AP-06", 7, 4000, 31)
		sid := sess.Session.ID
		forced := errors.New("forced rollback")
		e := db.Transaction(func(tx *gorm.DB) error {
			p, e := bill.PayInTransaction(tx, inv.ID, billing.PaymentRequest{Amount: 4000, PaymentMethod: "CASH", IdempotencyKey: "30ap-06", Payer: billing.PatientPayerRequest()}, 31, &sid)
			if e != nil {
				return e
			}
			if _, e := IssueReceiptInTx(tx, ReceiptIssueInput{PaymentID: p.ID, InvoiceID: inv.ID, PatientID: inv.PatientID, CashSessionID: &sid, Amount: p.Amount, PaymentMethod: p.PaymentMethod, IssuedBy: 31, InvoiceNumber: inv.Number}); e != nil {
				return e
			}
			return forced
		})
		if !errors.Is(e, forced) {
			t.Fatalf("expected forced rollback, got %v", e)
		}
		if p, r := counts(t, inv.ID); p != 0 || r != 0 {
			t.Fatalf("rollback left payment/receipt %d/%d", p, r)
		}
		var after billing.Invoice
		db.First(&after, inv.ID)
		if after.PaidAmount != 0 || after.BalanceAmount != 4000 || after.Status != billing.InvoiceIssued {
			t.Fatalf("invoice mutated: %#v", after)
		}
		sum, _ := s.Get(sess.Session.ID)
		if sum.CashCollected != 0 || sum.ExpectedCash != 500 {
			t.Fatalf("drawer mutated: cash=%d expected=%d", sum.CashCollected, sum.ExpectedCash)
		}
	})

	t.Run("CASH07_legacy_sessionless_cash_readable", func(t *testing.T) {
		inv := seedCashInvoice(t, db, "INV-30AP-07", 7, 2500, 31)
		legacy := billing.Payment{InvoiceID: inv.ID, Amount: 2500, PaymentMethod: "CASH", IdempotencyKey: "30ap-07-legacy", ReceivedBy: 31, PayerProvenance: billing.PayerProvenanceLegacyUnconfirmed}
		legacy.PaidAt = inv.CreatedAt
		if e := db.Create(&legacy).Error; e != nil {
			t.Fatal(e)
		}
		got, e := bill.GetInvoice(inv.ID)
		if e != nil || len(got.Payments) != 1 || got.Payments[0].PaymentMethod != "CASH" || got.Payments[0].CashSessionID != nil {
			t.Fatalf("legacy sessionless CASH must stay readable: %v %#v", e, got)
		}
	})
}
