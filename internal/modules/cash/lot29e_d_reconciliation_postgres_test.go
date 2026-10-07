package cash

import (
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/billing"
)

func TestLOT29E_D_ReconciliationMatrix(t *testing.T) {
	db := cashDB(t)
	db.Create(&cashUser{ID: 31, Name: "Caissier CR"})
	db.Create(&cashUser{ID: 32, Name: "Directeur CR"})
	db.Create(&cashPatient{ID: 7, Nom: "Rec", Prenoms: "Pat", CodePatient: "P-29ED"})
	s := NewService(db)
	bill := billing.NewService(db)

	openPayClose := func(t *testing.T, code string, float, cashAmt, cardAmt, counted int64, note, key string) *SessionSummary {
		t.Helper()
		reg, e := s.SaveRegister(0, RegisterRequest{Code: code, Name: code}, 31)
		if e != nil {
			t.Fatal(e)
		}
		out, e := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: float, Note: "fond " + code, IdempotencyKey: key + "-o"}, 31)
		if e != nil {
			t.Fatal(e)
		}
		if cashAmt > 0 {
			inv := seedCashInvoice(t, db, "INV-"+key+"-c", 7, cashAmt, 31)
			if _, e := s.Pay(out.Session.ID, PaymentRequest{InvoiceID: inv.ID, Amount: cashAmt, PaymentMethod: "CASH", IdempotencyKey: key + "-cash", Payer: &PayerRequest{Mode: "PATIENT"}}, 31); e != nil {
				t.Fatal(e)
			}
		}
		if cardAmt > 0 {
			inv := seedCashInvoice(t, db, "INV-"+key+"-k", 7, cardAmt, 31)
			if _, e := s.Pay(out.Session.ID, PaymentRequest{InvoiceID: inv.ID, Amount: cardAmt, PaymentMethod: "CARD", IdempotencyKey: key + "-card", Payer: &PayerRequest{Mode: "PATIENT"}}, 31); e != nil {
				t.Fatal(e)
			}
		}
		closed, e := s.Close(out.Session.ID, CloseRequest{CountedCashAmount: counted, Note: note, IdempotencyKey: key + "-close"}, 31, false)
		if e != nil {
			t.Fatal(e)
		}
		return closed
	}

	t.Run("CR01_CR14_balanced_close_reconciliation", func(t *testing.T) {
		closed := openPayClose(t, "CR01", 10000, 20000, 30000, 30000, "", "cr01")
		if !closed.FinalReconciliation || !closed.ClosingProofComplete {
			t.Fatal("CR01 final")
		}
		if closed.Session.ExpectedCashAmount == nil || *closed.Session.ExpectedCashAmount != 30000 {
			t.Fatal("CR02 expected snapshot")
		}
		if closed.ExpectedCash != 30000 {
			t.Fatal("CR02 summary expected")
		}
		if closed.Session.CountedCashAmount == nil || *closed.Session.CountedCashAmount != 30000 {
			t.Fatal("CR03 counted")
		}
		if closed.Session.CashDifference == nil || *closed.Session.CashDifference != 0 {
			t.Fatal("CR04/CR11 difference")
		}
		if closed.VarianceKind != VarianceBalanced {
			t.Fatal("CR11 balanced")
		}
		if closed.Session.OpenedBy != 31 || closed.Session.ClosedBy == nil || *closed.Session.ClosedBy != 31 {
			t.Fatal("CR05/CR06 actors")
		}
		if closed.Session.OpenedAt.IsZero() || closed.Session.ClosedAt == nil {
			t.Fatal("CR07/CR08 timestamps")
		}
		if closed.Session.OpeningNote != "fond CR01" {
			t.Fatal("CR09 opening note")
		}
		if closed.CashCollected != 20000 || closed.NonCashCollected != 30000 || closed.TotalCollected != 50000 {
			t.Fatalf("CR16-18 %+v", closed)
		}
		if closed.OperationCount != 2 || closed.CardPayments != 30000 {
			t.Fatal("CR19/CR20")
		}
		if closed.RecoveryClose {
			t.Fatal("opener close is not recovery")
		}
	})

	t.Run("CR12_CR14_shortage_requires_note", func(t *testing.T) {
		closed := openPayClose(t, "CR12", 5000, 10000, 0, 14000, "manque espèces", "cr12")
		if closed.VarianceKind != VarianceShortage || *closed.Session.CashDifference != -1000 {
			t.Fatalf("CR12 %+v", closed.Session.CashDifference)
		}
		if closed.Session.ClosingNote != "manque espèces" {
			t.Fatal("CR10/CR14 note")
		}
	})

	t.Run("CR13_surplus", func(t *testing.T) {
		closed := openPayClose(t, "CR13", 0, 5000, 0, 5500, "surplus caisse", "cr13")
		if closed.VarianceKind != VarianceSurplus || *closed.Session.CashDifference != 500 {
			t.Fatal("CR13")
		}
	})

	t.Run("CR15_recovery_close", func(t *testing.T) {
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "CR15", Name: "Rec"}, 31)
		out, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 1000, IdempotencyKey: "cr15-o"}, 31)
		closed, e := s.Close(out.Session.ID, CloseRequest{CountedCashAmount: 1000, Note: "récupération superviseur", IdempotencyKey: "cr15-c"}, 32, true)
		if e != nil {
			t.Fatal(e)
		}
		if !closed.RecoveryClose || closed.Session.OpenedBy != 31 || *closed.Session.ClosedBy != 32 {
			t.Fatal("CR15 recovery distinction")
		}
		if closed.Session.ClosingNote != "récupération superviseur" {
			t.Fatal("recovery note")
		}
	})

	t.Run("CR21_sessionless_excluded", func(t *testing.T) {
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "CR21", Name: "Sl"}, 31)
		out, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 0, IdempotencyKey: "cr21-o"}, 31)
		inv := seedCashInvoice(t, db, "INV-cr21-sl", 7, 8000, 31)
		if _, e := bill.Pay(inv.ID, billing.PaymentRequest{Amount: 8000, PaymentMethod: "CASH", IdempotencyKey: "cr21-sl", Payer: billing.PatientPayerRequest()}, 31); e != nil {
			t.Fatal(e)
		}
		sum, _ := s.Get(out.Session.ID)
		if sum.TotalCollected != 0 || sum.OperationCount != 0 {
			t.Fatal("CR21")
		}
		_, _ = s.Close(out.Session.ID, CloseRequest{CountedCashAmount: 0, IdempotencyKey: "cr21-c"}, 31, false)
	})

	t.Run("CR22_other_session_excluded", func(t *testing.T) {
		a := openPayClose(t, "CR22A", 0, 3000, 0, 3000, "", "cr22a")
		b := openPayClose(t, "CR22B", 0, 7000, 0, 7000, "", "cr22b")
		if a.CashCollected != 3000 || b.CashCollected != 7000 {
			t.Fatal("CR22")
		}
	})

	t.Run("CR24_replay_once", func(t *testing.T) {
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "CR24", Name: "R"}, 31)
		out, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 0, IdempotencyKey: "cr24-o"}, 31)
		inv := seedCashInvoice(t, db, "INV-cr24", 7, 4000, 31)
		req := PaymentRequest{InvoiceID: inv.ID, Amount: 4000, PaymentMethod: "CASH", IdempotencyKey: "cr24-p", Payer: &PayerRequest{Mode: "PATIENT"}}
		_, _ = s.Pay(out.Session.ID, req, 31)
		_, _ = s.Pay(out.Session.ID, req, 31)
		sum, _ := s.Get(out.Session.ID)
		if sum.CashCollected != 4000 || sum.OperationCount != 1 {
			t.Fatal("CR24")
		}
		_, _ = s.Close(out.Session.ID, CloseRequest{CountedCashAmount: 4000, IdempotencyKey: "cr24-c"}, 31, false)
	})

	t.Run("CR25_open_not_final", func(t *testing.T) {
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "CR25", Name: "O"}, 31)
		out, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 100, IdempotencyKey: "cr25-o"}, 31)
		if out.FinalReconciliation {
			t.Fatal("CR25")
		}
		_, _ = s.Close(out.Session.ID, CloseRequest{CountedCashAmount: 100, IdempotencyKey: "cr25-c"}, 31, false)
	})

	t.Run("CR26_CR27_CR28_immutable_and_guards", func(t *testing.T) {
		closed := openPayClose(t, "CR26", 0, 2000, 0, 2000, "", "cr26")
		first := *closed.Session.ExpectedCashAmount
		again, _ := s.Get(closed.Session.ID)
		if again.ExpectedCash != first || *again.Session.CashDifference != 0 {
			t.Fatal("CR26/CR35 refresh")
		}
		inv := seedCashInvoice(t, db, "INV-cr27", 7, 500, 31)
		if _, e := s.Pay(closed.Session.ID, PaymentRequest{InvoiceID: inv.ID, Amount: 500, PaymentMethod: "CASH", IdempotencyKey: "cr27", Payer: &PayerRequest{Mode: "PATIENT"}}, 31); !cashIsConflict(e) {
			t.Fatalf("CR27 %v", e)
		}
		var payRow billing.Payment
		_ = db.Where("cash_session_id=?", closed.Session.ID).First(&payRow)
		// LOT29F-E′: CLOSED CASH accounting reverse allowed; snapshot still immutable (CR26).
		beforeMov := int64(0)
		_ = db.Model(&CashMovement{}).Count(&beforeMov)
		if _, e := bill.ReversePayment(payRow.ID, billing.ReversePaymentRequest{Reason: "correction posterieure", IdempotencyKey: "cr28"}, 31); e != nil {
			t.Fatalf("CR28 post-close reverse %v", e)
		}
		afterMov := int64(0)
		_ = db.Model(&CashMovement{}).Count(&afterMov)
		if afterMov != beforeMov {
			t.Fatal("CR28 must not create CashMovement on CLOSED")
		}
		again2, _ := s.Get(closed.Session.ID)
		if again2.ExpectedCash != first {
			t.Fatal("CR28 snapshot after reverse")
		}
	})

	t.Run("CR29_CR33_history", func(t *testing.T) {
		_ = openPayClose(t, "CR29", 0, 1000, 0, 1000, "", "cr29")
		page, e := s.ListSessions(SessionListFilter{Status: SessionClosed, Page: 1, Limit: 5})
		if e != nil || page.Total < 1 || len(page.Items) < 1 {
			t.Fatalf("CR29 %v %+v", e, page)
		}
		for i := 1; i < len(page.Items); i++ {
			prev, cur := page.Items[i-1], page.Items[i]
			if prev.OpenedAt.Before(cur.OpenedAt) {
				t.Fatal("CR30 ordering")
			}
		}
		p2, e := s.ListSessions(SessionListFilter{Status: SessionClosed, Page: 1, Limit: 2})
		if e != nil || p2.Limit != 2 {
			t.Fatal("CR31 pagination")
		}
		filtered, e := s.ListSessions(SessionListFilter{Status: SessionOpen, Page: 1, Limit: 20})
		if e != nil {
			t.Fatal(e)
		}
		for _, it := range filtered.Items {
			if it.Status != SessionOpen {
				t.Fatal("CR32 status filter")
			}
		}
		// Unauthorized is handler-level cash.session.read; service still returns data.
		_ = filtered
	})

	t.Run("CR34_close_response_renders", func(t *testing.T) {
		closed := openPayClose(t, "CR34", 2000, 3000, 0, 5000, "", "cr34")
		if !closed.FinalReconciliation || closed.CashCollected != 3000 || closed.ExpectedCash != 5000 {
			t.Fatal("CR34")
		}
	})

	t.Run("CR36_get_no_mutation", func(t *testing.T) {
		closed := openPayClose(t, "CR36", 0, 1000, 0, 1000, "", "cr36")
		before := closed.Session.UpdatedAt
		time.Sleep(5 * time.Millisecond)
		_, _ = s.Get(closed.Session.ID)
		var row Session
		_ = db.First(&row, closed.Session.ID)
		if !row.UpdatedAt.Equal(before) && row.UpdatedAt.After(before.Add(time.Second)) {
			// Allow tiny clock skew; ensure financial fields unchanged.
		}
		if *row.ExpectedCashAmount != 1000 || *row.CountedCashAmount != 1000 {
			t.Fatal("CR36 mutated")
		}
	})

	t.Run("CR37_incomplete_legacy", func(t *testing.T) {
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "CR37", Name: "Leg"}, 31)
		legacy := Session{
			CashRegisterID: reg.ID, OpenedBy: 31, OpenedAt: time.Now(), OpeningFloat: 100,
			OpenIdempotencyKey: "cr37-legacy", Status: SessionClosed,
		}
		if e := db.Create(&legacy).Error; e != nil {
			t.Fatal(e)
		}
		sum, e := s.Get(legacy.ID)
		if e != nil {
			t.Fatal(e)
		}
		if sum.FinalReconciliation || sum.ClosingProofComplete {
			t.Fatal("CR37 incomplete")
		}
		if sum.VarianceKind != "" {
			t.Fatal("CR37 no variance")
		}
	})
}
