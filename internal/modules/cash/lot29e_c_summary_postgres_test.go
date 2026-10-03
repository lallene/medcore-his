package cash

import (
	"sync"
	"testing"

	"github.com/lallene/medcore-his/backend/internal/modules/billing"
)

func TestLOT29E_C_SessionSummaryMatrix(t *testing.T) {
	db := cashDB(t)
	db.Create(&cashUser{ID: 21, Name: "Caissier SM"})
	db.Create(&cashUser{ID: 22, Name: "Other"})
	db.Create(&cashPatient{ID: 5, Nom: "Sum", Prenoms: "Pat", CodePatient: "P-29EC"})
	s := NewService(db)
	bill := billing.NewService(db)

	openOn := func(t *testing.T, code string, float int64, key string) *SessionSummary {
		t.Helper()
		reg, e := s.SaveRegister(0, RegisterRequest{Code: code, Name: code}, 21)
		if e != nil {
			t.Fatal(e)
		}
		out, e := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: float, IdempotencyKey: key}, 21)
		if e != nil {
			t.Fatal(e)
		}
		return out
	}
	pay := func(t *testing.T, sessionID uint, amount int64, method, key string) {
		t.Helper()
		inv := seedCashInvoice(t, db, "INV-"+key, 5, amount, 21)
		req := PaymentRequest{InvoiceID: inv.ID, Amount: amount, PaymentMethod: method, IdempotencyKey: key}
		if method == "BANK_TRANSFER" || method == "CHECK" {
			req.ExternalReference = "REF-" + key
		}
		if method == "MOBILE_MONEY" {
			req.MobileOperator = "Orange Money"
		}
		if _, e := s.Pay(sessionID, req, 21); e != nil {
			t.Fatal(e)
		}
	}
	assertSummary := func(t *testing.T, sum *SessionSummary, cash, nonCash, total, expected, ops int64) {
		t.Helper()
		if sum.CashCollected != cash || sum.CashPayments != cash {
			t.Fatalf("cash=%d/%d want %d", sum.CashCollected, sum.CashPayments, cash)
		}
		if sum.NonCashCollected != nonCash {
			t.Fatalf("nonCash=%d want %d", sum.NonCashCollected, nonCash)
		}
		if sum.TotalCollected != total || sum.TotalPayments != total {
			t.Fatalf("total=%d/%d want %d", sum.TotalCollected, sum.TotalPayments, total)
		}
		if sum.ExpectedCash != expected {
			t.Fatalf("expected=%d want %d", sum.ExpectedCash, expected)
		}
		if sum.OperationCount != ops {
			t.Fatalf("ops=%d want %d", sum.OperationCount, ops)
		}
	}

	t.Run("SM01_SM02_SM11_SM12_zero_activity", func(t *testing.T) {
		out := openOn(t, "SM01", 15000, "sm01-o")
		assertSummary(t, out, 0, 0, 0, 15000, 0)
		if out.Session.OpeningFloat != 15000 {
			t.Fatal("SM02 opening float")
		}
		if out.TotalCollected != 0 {
			t.Fatal("SM11 total excludes float")
		}
	})

	t.Run("SM03_one_cash", func(t *testing.T) {
		out := openOn(t, "SM03", 10000, "sm03-o")
		pay(t, out.Session.ID, 20000, "CASH", "sm03-c")
		sum, e := s.Get(out.Session.ID)
		if e != nil {
			t.Fatal(e)
		}
		assertSummary(t, sum, 20000, 0, 20000, 30000, 1)
	})

	t.Run("SM04_one_card", func(t *testing.T) {
		out := openOn(t, "SM04", 5000, "sm04-o")
		pay(t, out.Session.ID, 8000, "CARD", "sm04-card")
		sum, _ := s.Get(out.Session.ID)
		assertSummary(t, sum, 0, 8000, 8000, 5000, 1)
		if sum.CardPayments != 8000 {
			t.Fatal("card breakdown")
		}
	})

	t.Run("SM05_cash_card", func(t *testing.T) {
		out := openOn(t, "SM05", 10000, "sm05-o")
		pay(t, out.Session.ID, 20000, "CASH", "sm05-cash")
		pay(t, out.Session.ID, 30000, "CARD", "sm05-card")
		sum, _ := s.Get(out.Session.ID)
		assertSummary(t, sum, 20000, 30000, 50000, 30000, 2)
	})

	t.Run("SM06_cash_mobile", func(t *testing.T) {
		out := openOn(t, "SM06", 1000, "sm06-o")
		pay(t, out.Session.ID, 4000, "CASH", "sm06-c")
		pay(t, out.Session.ID, 6000, "MOBILE_MONEY", "sm06-m")
		sum, _ := s.Get(out.Session.ID)
		assertSummary(t, sum, 4000, 6000, 10000, 5000, 2)
		if sum.MobileMoneyPayments != 6000 {
			t.Fatal("mobile")
		}
	})

	t.Run("SM07_cash_check", func(t *testing.T) {
		out := openOn(t, "SM07", 2000, "sm07-o")
		pay(t, out.Session.ID, 1000, "CASH", "sm07-c")
		pay(t, out.Session.ID, 3000, "CHECK", "sm07-k")
		sum, _ := s.Get(out.Session.ID)
		assertSummary(t, sum, 1000, 3000, 4000, 3000, 2)
		if sum.CheckPayments != 3000 {
			t.Fatal("check")
		}
	})

	t.Run("SM08_cash_transfer", func(t *testing.T) {
		out := openOn(t, "SM08", 0, "sm08-o")
		pay(t, out.Session.ID, 2500, "CASH", "sm08-c")
		pay(t, out.Session.ID, 7500, "BANK_TRANSFER", "sm08-t")
		sum, _ := s.Get(out.Session.ID)
		assertSummary(t, sum, 2500, 7500, 10000, 2500, 2)
		if sum.BankTransferPayments != 7500 {
			t.Fatal("transfer")
		}
	})

	t.Run("SM09_SM10_SM13_SM14_multiple_mixed", func(t *testing.T) {
		out := openOn(t, "SM09", 10000, "sm09-o")
		pay(t, out.Session.ID, 1000, "CASH", "sm09-c1")
		pay(t, out.Session.ID, 2000, "CASH", "sm09-c2")
		pay(t, out.Session.ID, 3000, "CARD", "sm09-card")
		pay(t, out.Session.ID, 4000, "MOBILE_MONEY", "sm09-m")
		sum, _ := s.Get(out.Session.ID)
		assertSummary(t, sum, 3000, 7000, 10000, 13000, 4)
		if sum.NonCashCollected == sum.CashCollected {
			t.Fatal("SM13 nonCash excludes CASH")
		}
	})

	t.Run("SM15_sessionless_excluded", func(t *testing.T) {
		out := openOn(t, "SM15", 5000, "sm15-o")
		inv := seedCashInvoice(t, db, "INV-sm15-sl", 5, 9000, 21)
		if _, e := bill.Pay(inv.ID, billing.PaymentRequest{Amount: 9000, PaymentMethod: "CASH", IdempotencyKey: "sm15-sl"}, 21); e != nil {
			t.Fatal(e)
		}
		sum, _ := s.Get(out.Session.ID)
		assertSummary(t, sum, 0, 0, 0, 5000, 0)
	})

	t.Run("SM16_other_session_excluded", func(t *testing.T) {
		a := openOn(t, "SM16A", 1000, "sm16-a")
		b := openOn(t, "SM16B", 2000, "sm16-b")
		pay(t, a.Session.ID, 5000, "CASH", "sm16-a-pay")
		pay(t, b.Session.ID, 7000, "CASH", "sm16-b-pay")
		sa, _ := s.Get(a.Session.ID)
		sb, _ := s.Get(b.Session.ID)
		assertSummary(t, sa, 5000, 0, 5000, 6000, 1)
		assertSummary(t, sb, 7000, 0, 7000, 9000, 1)
	})

	t.Run("SM17_payment_replay_once", func(t *testing.T) {
		out := openOn(t, "SM17", 0, "sm17-o")
		inv := seedCashInvoice(t, db, "INV-sm17", 5, 4000, 21)
		req := PaymentRequest{InvoiceID: inv.ID, Amount: 4000, PaymentMethod: "CASH", IdempotencyKey: "sm17-key"}
		if _, e := s.Pay(out.Session.ID, req, 21); e != nil {
			t.Fatal(e)
		}
		if _, e := s.Pay(out.Session.ID, req, 21); e != nil {
			t.Fatal(e)
		}
		sum, _ := s.Get(out.Session.ID)
		assertSummary(t, sum, 4000, 0, 4000, 4000, 1)
	})

	t.Run("SM18_concurrent_payments_both", func(t *testing.T) {
		out := openOn(t, "SM18", 0, "sm18-o")
		invA := seedCashInvoice(t, db, "INV-sm18a", 5, 3000, 21)
		invB := seedCashInvoice(t, db, "INV-sm18b", 5, 5000, 21)
		var wg sync.WaitGroup
		errs := make(chan error, 2)
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, e := s.Pay(out.Session.ID, PaymentRequest{InvoiceID: invA.ID, Amount: 3000, PaymentMethod: "CASH", IdempotencyKey: "sm18-a"}, 21)
			errs <- e
		}()
		go func() {
			defer wg.Done()
			_, e := s.Pay(out.Session.ID, PaymentRequest{InvoiceID: invB.ID, Amount: 5000, PaymentMethod: "CARD", IdempotencyKey: "sm18-b"}, 21)
			errs <- e
		}()
		wg.Wait()
		close(errs)
		for e := range errs {
			if e != nil {
				t.Fatal(e)
			}
		}
		sum, _ := s.Get(out.Session.ID)
		assertSummary(t, sum, 3000, 5000, 8000, 3000, 2)
	})

	t.Run("SM19_SM20_SM21_current_detail_same_authority", func(t *testing.T) {
		out := openOn(t, "SM19", 8000, "sm19-o")
		pay(t, out.Session.ID, 2000, "CASH", "sm19-c")
		cur, e := s.Current(21)
		if e != nil || cur == nil {
			t.Fatal(e)
		}
		detail, e := s.Get(out.Session.ID)
		if e != nil {
			t.Fatal(e)
		}
		if cur.Session.ID != detail.Session.ID {
			t.Fatal("current/detail session")
		}
		if cur.ExpectedCash != detail.ExpectedCash || cur.CashCollected != detail.CashCollected || cur.TotalCollected != detail.TotalCollected {
			t.Fatalf("SM21 drift cur=%+v detail=%+v", cur, detail)
		}
		// Pure formula authority matches assemble helper.
		want := assembleSessionSummary(detail.Session, sessionPaymentTotals{
			Cash: detail.CashCollected, Card: detail.CardPayments, MobileMoney: detail.MobileMoneyPayments,
			BankTransfer: detail.BankTransferPayments, Check: detail.CheckPayments,
			Total: detail.TotalCollected, Count: detail.OperationCount,
		})
		if want.ExpectedCash != detail.ExpectedCash {
			t.Fatal("SM21 assemble mismatch")
		}
	})

	t.Run("SM22_SM27_close_snapshot_immutable", func(t *testing.T) {
		out := openOn(t, "SM22", 10000, "sm22-o")
		pay(t, out.Session.ID, 20000, "CASH", "sm22-c")
		pay(t, out.Session.ID, 5000, "CARD", "sm22-card")
		pre, _ := s.Get(out.Session.ID)
		closed, e := s.Close(out.Session.ID, CloseRequest{CountedCashAmount: 30000, IdempotencyKey: "sm22-close"}, 21, false)
		if e != nil {
			t.Fatal(e)
		}
		if pre.ExpectedCash != 30000 || closed.ExpectedCash != 30000 {
			t.Fatalf("SM22 expected pre=%d closed=%d", pre.ExpectedCash, closed.ExpectedCash)
		}
		if closed.Session.ExpectedCashAmount == nil || *closed.Session.ExpectedCashAmount != 30000 {
			t.Fatal("SM23 persist expected")
		}
		if closed.Session.CountedCashAmount == nil || *closed.Session.CountedCashAmount != 30000 {
			t.Fatal("SM24 counted")
		}
		if closed.Session.CashDifference == nil || *closed.Session.CashDifference != 0 {
			t.Fatal("SM25 difference")
		}
		again, _ := s.Get(out.Session.ID)
		if again.ExpectedCash != 30000 {
			t.Fatal("SM26 closed uses snapshot")
		}
		if again.CashCollected != 20000 || again.NonCashCollected != 5000 {
			t.Fatal("closed collection totals")
		}
		// Pay after close blocked — no drift.
		inv := seedCashInvoice(t, db, "INV-sm27", 5, 1000, 21)
		if _, e := s.Pay(out.Session.ID, PaymentRequest{InvoiceID: inv.ID, Amount: 1000, PaymentMethod: "CASH", IdempotencyKey: "sm27-late"}, 21); !cashIsConflict(e) {
			t.Fatalf("SM27 pay closed %v", e)
		}
		final, _ := s.Get(out.Session.ID)
		if final.ExpectedCash != 30000 || final.OperationCount != 2 {
			t.Fatal("SM27 no drift")
		}
	})

	t.Run("SM28_SM29_get_unknown_denied_as_not_found", func(t *testing.T) {
		if _, e := s.Get(999999); e == nil {
			t.Fatal("SM28 missing session")
		}
		// Read semantics: Get by id remains available with cash.session.read (handler-level);
		// service returns any session summary without spoofing totals.
		out := openOn(t, "SM29", 100, "sm29-o")
		sum, e := s.Get(out.Session.ID)
		if e != nil || sum.ExpectedCash != 100 {
			t.Fatal("SM29 read")
		}
	})

	t.Run("SM30_session_reversal_blocked", func(t *testing.T) {
		out := openOn(t, "SM30", 0, "sm30-o")
		pay(t, out.Session.ID, 1500, "CASH", "sm30-p")
		sum, _ := s.Get(out.Session.ID)
		var payRow billing.Payment
		if e := db.Where("cash_session_id=?", out.Session.ID).First(&payRow).Error; e != nil {
			t.Fatal(e)
		}
		if _, e := bill.ReversePayment(payRow.ID, billing.ReversePaymentRequest{Reason: "session payment reverse blocked", IdempotencyKey: "sm30-rev"}, 21); e == nil {
			t.Fatal("SM30 reversal must stay blocked")
		}
		again, _ := s.Get(out.Session.ID)
		if again.CashCollected != sum.CashCollected || again.OperationCount != 1 {
			t.Fatal("SM30 summary unchanged")
		}
	})

	t.Run("SM36_zero_float_valid", func(t *testing.T) {
		out := openOn(t, "SM36", 0, "sm36-o")
		assertSummary(t, out, 0, 0, 0, 0, 0)
	})
}
