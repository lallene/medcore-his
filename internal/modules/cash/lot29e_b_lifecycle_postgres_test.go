package cash

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"github.com/lallene/medcore-his/backend/internal/modules/billing"
	"gorm.io/gorm"
)

func cashErrCode(e error) string {
	var app *coreerrors.AppError
	if errors.As(e, &app) {
		return app.Code
	}
	return ""
}
func cashIsConflict(e error) bool  { return cashErrCode(e) == "CONFLICT" }
func cashIsForbidden(e error) bool { return cashErrCode(e) == "FORBIDDEN" }
func cashIsBadRequest(e error) bool {
	return cashErrCode(e) == "BAD_REQUEST"
}

func seedCashInvoice(t *testing.T, db *gorm.DB, number string, patientID, balance int64, user uint) billing.Invoice {
	t.Helper()
	inv := billing.Invoice{
		Number: number, PatientID: uint(patientID), Status: billing.InvoiceIssued,
		GrossAmount: balance, PatientAmount: balance, BalanceAmount: balance,
		CreatedBy: user, UpdatedBy: user,
	}
	if e := db.Create(&inv).Error; e != nil {
		t.Fatal(e)
	}
	return inv
}

func TestLOT29E_B_SessionLifecycleMatrix(t *testing.T) {
	db := cashDB(t)
	db.Create(&cashUser{ID: 11, Name: "Caissier A"})
	db.Create(&cashUser{ID: 12, Name: "Caissier B"})
	db.Create(&cashUser{ID: 13, Name: "Directeur"})
	db.Create(&cashPatient{ID: 3, Nom: "Pat", Prenoms: "Cash", CodePatient: "P-29EB"})
	s := NewService(db)

	t.Run("CS01_CS03_open_persists", func(t *testing.T) {
		reg, e := s.SaveRegister(0, RegisterRequest{Code: "CS01", Name: "R1"}, 11)
		if e != nil {
			t.Fatal(e)
		}
		out, e := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 10000, Note: "fond", IdempotencyKey: "cs01-open"}, 11)
		if e != nil {
			t.Fatal(e)
		}
		if out.Session.Status != SessionOpen || out.Session.OpenedBy != 11 || out.Session.OpeningFloat != 10000 || out.Session.OpeningNote != "fond" {
			t.Fatalf("CS01/02/03 %+v", out.Session)
		}
		// Close for isolation.
		if _, e := s.Close(out.Session.ID, CloseRequest{CountedCashAmount: 10000, IdempotencyKey: "cs01-close"}, 11, false); e != nil {
			t.Fatal(e)
		}
	})

	t.Run("CS04_CS05_open_idempotency", func(t *testing.T) {
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "CS04", Name: "R4"}, 11)
		req := OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 2000, Note: "a", IdempotencyKey: "cs04-key"}
		a, e := s.Open(req, 11)
		if e != nil {
			t.Fatal(e)
		}
		b, e := s.Open(req, 11)
		if e != nil || b.Session.ID != a.Session.ID || !b.Session.OpenedAt.Equal(a.Session.OpenedAt) {
			t.Fatalf("CS04 replay %+v %+v %v", a, b, e)
		}
		if _, e := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 3000, Note: "a", IdempotencyKey: "cs04-key"}, 11); !cashIsConflict(e) {
			t.Fatalf("CS05 want conflict %v", e)
		}
		if _, e := s.Close(a.Session.ID, CloseRequest{CountedCashAmount: 2000, IdempotencyKey: "cs04-close"}, 11, false); e != nil {
			t.Fatal(e)
		}
	})

	t.Run("CS06_new_key_while_open_conflict", func(t *testing.T) {
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "CS06", Name: "R6"}, 11)
		a, e := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 0, IdempotencyKey: "cs06-a"}, 11)
		if e != nil {
			t.Fatal(e)
		}
		if _, e := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 0, IdempotencyKey: "cs06-b"}, 12); !cashIsConflict(e) {
			t.Fatalf("CS06 %v", e)
		}
		if _, e := s.Close(a.Session.ID, CloseRequest{CountedCashAmount: 0, IdempotencyKey: "cs06-close"}, 11, false); e != nil {
			t.Fatal(e)
		}
	})

	t.Run("CS07_concurrent_open_one_session", func(t *testing.T) {
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "CS07", Name: "R7"}, 11)
		var wg sync.WaitGroup
		type res struct {
			id  uint
			err error
		}
		ch := make(chan res, 2)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			n := i
			go func() {
				defer wg.Done()
				out, e := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 500, IdempotencyKey: fmt.Sprintf("cs07-%d", n)}, 11)
				id := uint(0)
				if out != nil {
					id = out.Session.ID
				}
				ch <- res{id: id, err: e}
			}()
		}
		wg.Wait()
		close(ch)
		var ok, conflict int
		var sid uint
		for r := range ch {
			if r.err == nil {
				ok++
				sid = r.id
			} else if cashIsConflict(r.err) {
				conflict++
			} else {
				t.Fatalf("CS07 unexpected %v", r.err)
			}
		}
		if ok != 1 || conflict != 1 {
			t.Fatalf("CS07 ok=%d conflict=%d", ok, conflict)
		}
		var n int64
		db.Model(&Session{}).Where("cash_register_id=? AND status=?", reg.ID, SessionOpen).Count(&n)
		if n != 1 {
			t.Fatalf("CS07 open count=%d", n)
		}
		if _, e := s.Close(sid, CloseRequest{CountedCashAmount: 500, IdempotencyKey: "cs07-close"}, 11, false); e != nil {
			t.Fatal(e)
		}
	})

	t.Run("CS08_different_registers", func(t *testing.T) {
		a, _ := s.SaveRegister(0, RegisterRequest{Code: "CS08A", Name: "A"}, 11)
		b, _ := s.SaveRegister(0, RegisterRequest{Code: "CS08B", Name: "B"}, 11)
		sa, e := s.Open(OpenRequest{CashRegisterID: a.ID, OpeningFloat: 1, IdempotencyKey: "cs08-a"}, 11)
		if e != nil {
			t.Fatal(e)
		}
		sb, e := s.Open(OpenRequest{CashRegisterID: b.ID, OpeningFloat: 2, IdempotencyKey: "cs08-b"}, 11)
		if e != nil {
			t.Fatal(e)
		}
		if sa.Session.ID == sb.Session.ID {
			t.Fatal("CS08 same session")
		}
		_, _ = s.Close(sa.Session.ID, CloseRequest{CountedCashAmount: 1, IdempotencyKey: "cs08-ca"}, 11, false)
		_, _ = s.Close(sb.Session.ID, CloseRequest{CountedCashAmount: 2, IdempotencyKey: "cs08-cb"}, 11, false)
	})

	t.Run("CS09_inactive_register_open", func(t *testing.T) {
		off := false
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "CS09", Name: "Off", Active: &off}, 11)
		if _, e := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 0, IdempotencyKey: "cs09"}, 11); !cashIsConflict(e) {
			t.Fatalf("CS09 %v", e)
		}
	})

	t.Run("CS10_CS11_current_owner_only", func(t *testing.T) {
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "CS10", Name: "Cur"}, 11)
		out, e := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 0, IdempotencyKey: "cs10"}, 11)
		if e != nil {
			t.Fatal(e)
		}
		cur, e := s.Current(11)
		if e != nil || cur == nil || cur.Session.ID != out.Session.ID {
			t.Fatalf("CS10 %+v %v", cur, e)
		}
		other, e := s.Current(12)
		if e != nil || other != nil {
			t.Fatalf("CS11 leaked %+v %v", other, e)
		}
		_, _ = s.Close(out.Session.ID, CloseRequest{CountedCashAmount: 0, IdempotencyKey: "cs10-c"}, 11, false)
	})

	t.Run("CS12_multiple_register_current_latest", func(t *testing.T) {
		r1, _ := s.SaveRegister(0, RegisterRequest{Code: "CS12A", Name: "A"}, 11)
		r2, _ := s.SaveRegister(0, RegisterRequest{Code: "CS12B", Name: "B"}, 11)
		s1, _ := s.Open(OpenRequest{CashRegisterID: r1.ID, OpeningFloat: 0, IdempotencyKey: "cs12-1"}, 11)
		s2, _ := s.Open(OpenRequest{CashRegisterID: r2.ID, OpeningFloat: 0, IdempotencyKey: "cs12-2"}, 11)
		_ = db.Model(&Session{}).Where("id=?", s2.Session.ID).Update("opened_at", s1.Session.OpenedAt.Add(time.Second)).Error
		cur, _ := s.Current(11)
		if cur == nil || cur.Session.ID != s2.Session.ID {
			t.Fatalf("CS12 current=%+v want %d", cur, s2.Session.ID)
		}
		_, _ = s.Close(s1.Session.ID, CloseRequest{CountedCashAmount: 0, IdempotencyKey: "cs12-c1"}, 11, false)
		_, _ = s.Close(s2.Session.ID, CloseRequest{CountedCashAmount: 0, IdempotencyKey: "cs12-c2"}, 11, false)
	})

	t.Run("CS13_CS14_CS35_pay_ownership_and_noncash", func(t *testing.T) {
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "CS13", Name: "Pay"}, 11)
		sess, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 1000, IdempotencyKey: "cs13-o"}, 11)
		inv := seedCashInvoice(t, db, "INV-CS13", 3, 20000, 11)
		if _, e := s.Pay(sess.Session.ID, PaymentRequest{InvoiceID: inv.ID, Amount: 5000, PaymentMethod: "CASH", IdempotencyKey: "cs13-cash", Payer: &PayerRequest{Mode: "PATIENT"}}, 11); e != nil {
			t.Fatal(e)
		}
		if _, e := s.Pay(sess.Session.ID, PaymentRequest{InvoiceID: inv.ID, Amount: 3000, PaymentMethod: "CARD", IdempotencyKey: "cs13-card", Payer: &PayerRequest{Mode: "PATIENT"}}, 12); !cashIsForbidden(e) {
			t.Fatalf("CS14 want forbidden %v", e)
		}
		if _, e := s.Pay(sess.Session.ID, PaymentRequest{InvoiceID: inv.ID, Amount: 3000, PaymentMethod: "CARD", IdempotencyKey: "cs13-card-ok", Payer: &PayerRequest{Mode: "PATIENT"}}, 11); e != nil {
			t.Fatal(e)
		}
		sum, _ := s.Get(sess.Session.ID)
		if sum.ExpectedCash != 6000 || sum.CashPayments != 5000 || sum.CardPayments != 3000 {
			t.Fatalf("CS35 %+v", sum)
		}
		_, _ = s.Close(sess.Session.ID, CloseRequest{CountedCashAmount: 6000, IdempotencyKey: "cs13-c"}, 11, false)
	})

	t.Run("CS15_pay_closed_rejected", func(t *testing.T) {
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "CS15", Name: "Closed"}, 11)
		sess, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 0, IdempotencyKey: "cs15-o"}, 11)
		_, _ = s.Close(sess.Session.ID, CloseRequest{CountedCashAmount: 0, IdempotencyKey: "cs15-c"}, 11, false)
		inv := seedCashInvoice(t, db, "INV-CS15", 3, 1000, 11)
		if _, e := s.Pay(sess.Session.ID, PaymentRequest{InvoiceID: inv.ID, Amount: 1000, PaymentMethod: "CASH", IdempotencyKey: "cs15-p", Payer: &PayerRequest{Mode: "PATIENT"}}, 11); !cashIsConflict(e) {
			t.Fatalf("CS15 %v", e)
		}
	})

	t.Run("CS16_deactivate_while_open_rejected", func(t *testing.T) {
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "CS16", Name: "Deact"}, 11)
		sess, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 0, IdempotencyKey: "cs16-o"}, 11)
		off := false
		if _, e := s.SaveRegister(reg.ID, RegisterRequest{Code: "CS16", Name: "Deact", Active: &off}, 11); !cashIsConflict(e) {
			t.Fatalf("CS16 deactivate %v", e)
		}
		_, _ = s.Close(sess.Session.ID, CloseRequest{CountedCashAmount: 0, IdempotencyKey: "cs16-c"}, 11, false)
		if _, e := s.SaveRegister(reg.ID, RegisterRequest{Code: "CS16", Name: "Deact", Active: &off}, 11); e != nil {
			t.Fatalf("CS16 after close %v", e)
		}
	})

	t.Run("CS17_CS20_close_owner_variance_note", func(t *testing.T) {
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "CS17", Name: "Close"}, 11)
		sess, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 5000, IdempotencyKey: "cs17-o"}, 11)
		inv := seedCashInvoice(t, db, "INV-CS17", 3, 2000, 11)
		_, _ = s.Pay(sess.Session.ID, PaymentRequest{InvoiceID: inv.ID, Amount: 2000, PaymentMethod: "CASH", IdempotencyKey: "cs17-p", Payer: &PayerRequest{Mode: "PATIENT"}}, 11)
		if _, e := s.Close(sess.Session.ID, CloseRequest{CountedCashAmount: 6000, IdempotencyKey: "cs17-bad"}, 11, false); !cashIsBadRequest(e) {
			t.Fatalf("CS20 %v", e)
		}
		closed, e := s.Close(sess.Session.ID, CloseRequest{CountedCashAmount: 7000, IdempotencyKey: "cs17-ok"}, 11, false)
		if e != nil {
			t.Fatal(e)
		}
		if *closed.Session.ExpectedCashAmount != 7000 || *closed.Session.CashDifference != 0 {
			t.Fatalf("CS17 %+v", closed.Session)
		}
	})

	t.Run("CS18_CS19_expected_and_difference", func(t *testing.T) {
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "CS18", Name: "Exp"}, 11)
		sess, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 10000, IdempotencyKey: "cs18-o"}, 11)
		inv := seedCashInvoice(t, db, "INV-CS18", 3, 4000, 11)
		_, _ = s.Pay(sess.Session.ID, PaymentRequest{InvoiceID: inv.ID, Amount: 4000, PaymentMethod: "CASH", IdempotencyKey: "cs18-p", Payer: &PayerRequest{Mode: "PATIENT"}}, 11)
		closed, e := s.Close(sess.Session.ID, CloseRequest{CountedCashAmount: 13000, Note: "manque", IdempotencyKey: "cs18-c"}, 11, false)
		if e != nil {
			t.Fatal(e)
		}
		if *closed.Session.ExpectedCashAmount != 14000 || *closed.Session.CashDifference != -1000 {
			t.Fatalf("CS18/19 %+v", closed.Session)
		}
	})

	t.Run("CS21_CS23_close_idempotency", func(t *testing.T) {
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "CS21", Name: "CIdem"}, 11)
		sess, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 0, IdempotencyKey: "cs21-o"}, 11)
		req := CloseRequest{CountedCashAmount: 0, IdempotencyKey: "cs21-c"}
		a, e := s.Close(sess.Session.ID, req, 11, false)
		if e != nil {
			t.Fatal(e)
		}
		b, e := s.Close(sess.Session.ID, req, 11, false)
		if e != nil || b.Session.ID != a.Session.ID || !b.Session.ClosedAt.Equal(*a.Session.ClosedAt) {
			t.Fatalf("CS21 %+v %+v %v", a, b, e)
		}
		if _, e := s.Close(sess.Session.ID, CloseRequest{CountedCashAmount: 1, Note: "x", IdempotencyKey: "cs21-c"}, 11, false); !cashIsConflict(e) {
			t.Fatalf("CS22 %v", e)
		}
		if _, e := s.Close(sess.Session.ID, CloseRequest{CountedCashAmount: 0, IdempotencyKey: "cs21-new"}, 11, false); !cashIsConflict(e) {
			t.Fatalf("CS23 %v", e)
		}
	})

	t.Run("CS24_concurrent_close", func(t *testing.T) {
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "CS24", Name: "RaceC"}, 11)
		sess, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 100, IdempotencyKey: "cs24-o"}, 11)
		var wg sync.WaitGroup
		errs := make(chan error, 2)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			n := i
			go func() {
				defer wg.Done()
				_, e := s.Close(sess.Session.ID, CloseRequest{CountedCashAmount: 100, IdempotencyKey: fmt.Sprintf("cs24-c-%d", n)}, 11, false)
				errs <- e
			}()
		}
		wg.Wait()
		close(errs)
		var ok, conflict int
		for e := range errs {
			if e == nil {
				ok++
			} else if cashIsConflict(e) {
				conflict++
			} else {
				t.Fatalf("CS24 %v", e)
			}
		}
		if ok != 1 || conflict != 1 {
			t.Fatalf("CS24 ok=%d conflict=%d", ok, conflict)
		}
		var n int64
		db.Model(&Session{}).Where("id=? AND status=?", sess.Session.ID, SessionClosed).Count(&n)
		if n != 1 {
			t.Fatal("CS24 not closed once")
		}
	})

	t.Run("CS25_pay_vs_close", func(t *testing.T) {
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "CS25", Name: "RacePC"}, 11)
		sess, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 0, IdempotencyKey: "cs25-o"}, 11)
		inv := seedCashInvoice(t, db, "INV-CS25", 3, 5000, 11)
		start := make(chan struct{})
		var wg sync.WaitGroup
		var payErr, closeErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, payErr = s.Pay(sess.Session.ID, PaymentRequest{InvoiceID: inv.ID, Amount: 5000, PaymentMethod: "CASH", IdempotencyKey: "cs25-p", Payer: &PayerRequest{Mode: "PATIENT"}}, 11)
		}()
		go func() {
			defer wg.Done()
			<-start
			// Note covers pay-first (expected 5000, counted 0) and close-first (expected 0).
			_, closeErr = s.Close(sess.Session.ID, CloseRequest{CountedCashAmount: 0, Note: "race", IdempotencyKey: "cs25-c"}, 11, false)
		}()
		close(start)
		wg.Wait()
		sum, _ := s.Get(sess.Session.ID)
		if sum.Session.Status != SessionClosed {
			t.Fatal("CS25 not closed")
		}
		// Serialized: either pay before close (expected includes 5000) or close first (pay fails).
		if payErr == nil {
			if *sum.Session.ExpectedCashAmount != 5000 {
				t.Fatalf("CS25 pay-first expected=%v", sum.Session.ExpectedCashAmount)
			}
			if closeErr != nil {
				t.Fatalf("CS25 close after pay %v", closeErr)
			}
		} else if !cashIsConflict(payErr) {
			t.Fatalf("CS25 pay err %v close %v", payErr, closeErr)
		} else if closeErr != nil {
			t.Fatalf("CS25 close failed while pay rejected: %v", closeErr)
		} else if *sum.Session.ExpectedCashAmount != 0 {
			t.Fatalf("CS25 close-first expected=%v", sum.Session.ExpectedCashAmount)
		}
	})

	t.Run("CS26_close_failure_leaves_open", func(t *testing.T) {
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "CS26", Name: "Fail"}, 11)
		sess, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 0, IdempotencyKey: "cs26-o"}, 11)
		if _, e := s.Close(sess.Session.ID, CloseRequest{CountedCashAmount: -1, IdempotencyKey: "cs26-c"}, 11, false); !cashIsBadRequest(e) {
			t.Fatalf("CS26 %v", e)
		}
		cur, _ := s.Get(sess.Session.ID)
		if cur.Session.Status != SessionOpen || cur.Session.ClosedAt != nil {
			t.Fatalf("CS26 mutated %+v", cur.Session)
		}
		_, _ = s.Close(sess.Session.ID, CloseRequest{CountedCashAmount: 0, IdempotencyKey: "cs26-ok"}, 11, false)
	})

	t.Run("CS27_CS31_recovery", func(t *testing.T) {
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "CS27", Name: "Rec"}, 11)
		sess, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 2500, IdempotencyKey: "cs27-o"}, 11)
		if _, e := s.Close(sess.Session.ID, CloseRequest{CountedCashAmount: 2500, IdempotencyKey: "cs27-deny"}, 12, false); !cashIsForbidden(e) {
			t.Fatalf("CS27 %v", e)
		}
		if _, e := s.Close(sess.Session.ID, CloseRequest{CountedCashAmount: 2500, IdempotencyKey: "cs27-nonote"}, 13, true); !cashIsBadRequest(e) {
			t.Fatalf("CS30 %v", e)
		}
		closed, e := s.Close(sess.Session.ID, CloseRequest{CountedCashAmount: 2500, Note: "récupération abandon", IdempotencyKey: "cs27-ok"}, 13, true)
		if e != nil {
			t.Fatal(e)
		}
		if closed.Session.OpenedBy != 11 || closed.Session.ClosedBy == nil || *closed.Session.ClosedBy != 13 {
			t.Fatalf("CS31 %+v", closed.Session)
		}
		if closed.Session.Status != SessionClosed {
			t.Fatal("CS32/33")
		}
		if _, e := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 0, IdempotencyKey: "cs27-reopen-attempt"}, 11); e != nil {
			// new open after close is allowed — not reopen of same session
		} else {
			// ok new session
			cur, _ := s.Current(11)
			if cur != nil {
				_, _ = s.Close(cur.Session.ID, CloseRequest{CountedCashAmount: 0, IdempotencyKey: "cs27-cleanup"}, 11, false)
			}
		}
	})

	t.Run("CS34_open_cash_reversal_then_close", func(t *testing.T) {
		// LOT29F-D: OPEN CASH reversal adjusts expected; close snapshots corrected drawer.
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "CS34", Name: "Rev"}, 11)
		sess, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 0, IdempotencyKey: "cs34-o"}, 11)
		inv := seedCashInvoice(t, db, "INV-CS34", 3, 1000, 11)
		rec, e := s.Pay(sess.Session.ID, PaymentRequest{InvoiceID: inv.ID, Amount: 1000, PaymentMethod: "CASH", IdempotencyKey: "cs34-p", Payer: &PayerRequest{Mode: "PATIENT"}}, 11)
		if e != nil {
			t.Fatal(e)
		}
		bill := billing.NewService(db)
		if _, e := bill.ReversePayment(rec.PaymentID, billing.ReversePaymentRequest{Reason: "test reverse", IdempotencyKey: "cs34-rev"}, 11); e != nil {
			t.Fatalf("CS34 open reverse %v", e)
		}
		closed, e := s.Close(sess.Session.ID, CloseRequest{CountedCashAmount: 0, IdempotencyKey: "cs34-c"}, 11, false)
		if e != nil || closed.ExpectedCash != 0 {
			t.Fatalf("CS34 close %+v %v", closed, e)
		}
	})

	t.Run("CS36_sessionless_excluded", func(t *testing.T) {
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "CS36", Name: "Sl"}, 11)
		sess, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 0, IdempotencyKey: "cs36-o"}, 11)
		inv := seedCashInvoice(t, db, "INV-CS36", 3, 8000, 11)
		bill := billing.NewService(db)
		if _, e := bill.Pay(inv.ID, billing.PaymentRequest{Amount: 3000, PaymentMethod: "CASH", IdempotencyKey: "cs36-bill", Payer: billing.PatientPayerRequest()}, 11); e != nil {
			t.Fatal(e)
		}
		sum, _ := s.Get(sess.Session.ID)
		if sum.ExpectedCash != 0 || sum.OperationCount != 0 {
			t.Fatalf("CS36 counted sessionless %+v", sum)
		}
		_, _ = s.Close(sess.Session.ID, CloseRequest{CountedCashAmount: 0, IdempotencyKey: "cs36-c"}, 11, false)
	})

	t.Run("CS29_rbac_close_any_grant", func(t *testing.T) {
		dir := rbac.EffectiveStaffPermissions("staff", []string{"DIRECTEUR_ADMINISTRATIF"}, nil)
		cai := rbac.EffectiveStaffPermissions("staff", []string{"CAISSIER"}, nil)
		comp := rbac.EffectiveStaffPermissions("staff", []string{"COMPTABLE"}, nil)
		if !rbac.HasAnyPermission(dir, "cash.session.close_any") {
			t.Fatal("CS28 DirAdmin should close_any")
		}
		if rbac.HasAnyPermission(cai, "cash.session.close_any") {
			t.Fatal("CS29 CAISSIER must not close_any")
		}
		if rbac.HasAnyPermission(comp, "cash.session.close_any") {
			t.Fatal("COMPTABLE must not close_any")
		}
		if !rbac.HasAnyPermission(cai, "cash.session.open", "cash.session.close", "cash.payment.create") {
			t.Fatal("CS AA CAISSIER pack")
		}
	})
}
