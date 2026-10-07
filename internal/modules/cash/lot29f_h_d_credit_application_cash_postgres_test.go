package cash

import (
	"testing"

	"github.com/lallene/medcore-his/backend/internal/modules/billing"
)

func TestLOT29F_HD_CashIsolation(t *testing.T) {
	t.Run("H_D06_H_D07_credit_apply_leaves_session_unchanged", func(t *testing.T) {
		db := cashDB(t)
		db.Create(&cashUser{ID: 501, Name: "Caisse HD"})
		db.Create(&cashPatient{ID: 501, Nom: "Pat", Prenoms: "HD", CodePatient: "P-HD07", Telephone: "+2250700000501"})
		s := NewService(db)
		bill := billing.NewService(db)
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "HD07", Name: "C"}, 501)
		open, e := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 12000, IdempotencyKey: "hd07-o"}, 501)
		if e != nil {
			t.Fatal(e)
		}
		// Earn credit via session payment + CN
		earn := seedCashInvoice(t, db, "INV-HD07-E", 501, 50000, 501)
		if _, e := s.Pay(open.Session.ID, PaymentRequest{
			InvoiceID: earn.ID, Amount: 50000, PaymentMethod: "CASH", IdempotencyKey: "hd07-earn",
			Payer: &PayerRequest{Mode: "PATIENT"},
		}, 501); e != nil {
			t.Fatal(e)
		}
		cn, e := bill.IssueCreditNote(earn.ID, billing.CreditNoteRequest{Amount: 15000, Reason: "Crédit HD07", IdempotencyKey: "hd07-cn"}, 501)
		if e != nil || cn.CreditHolderPartyID == nil {
			t.Fatalf("earn credit %+v %v", cn, e)
		}
		before, e := s.Get(open.Session.ID)
		if e != nil {
			t.Fatal(e)
		}
		var movBefore, opsBefore int64
		db.Model(&CashMovement{}).Where("cash_session_id=?", open.Session.ID).Count(&movBefore)
		opsBefore = before.OperationCount

		target := seedCashInvoice(t, db, "INV-HD07-T", 501, 20000, 501)
		if _, e := bill.ApplyCredit(target.ID, billing.CreditApplicationRequest{
			HolderPartyID: *cn.CreditHolderPartyID, Amount: 10000, IdempotencyKey: "hd07-app",
		}, 501); e != nil {
			t.Fatal(e)
		}

		after, e := s.Get(open.Session.ID)
		if e != nil {
			t.Fatal(e)
		}
		var movAfter int64
		db.Model(&CashMovement{}).Where("cash_session_id=?", open.Session.ID).Count(&movAfter)
		if after.CashCollected != before.CashCollected || after.NonCashCollected != before.NonCashCollected ||
			after.TotalCollected != before.TotalCollected || after.ExpectedCash != before.ExpectedCash ||
			after.OperationCount != opsBefore || movAfter != movBefore {
			t.Fatalf("H-D06/07 before=%+v after=%+v mov %d->%d", before, after, movBefore, movAfter)
		}
		if after.CashCollected != 50000 || after.ExpectedCash != 62000 {
			t.Fatalf("cash authority drifted cash=%d expected=%d", after.CashCollected, after.ExpectedCash)
		}
	})

	t.Run("H_D10_apply_without_open_session", func(t *testing.T) {
		db := cashDB(t)
		db.Create(&cashUser{ID: 502, Name: "Fact HD"})
		db.Create(&cashPatient{ID: 502, Nom: "Pat", Prenoms: "NoSess", CodePatient: "P-HD10", Telephone: "+2250700000502"})
		bill := billing.NewService(db)
		earn := seedCashInvoice(t, db, "INV-HD10-E", 502, 40000, 502)
		if _, e := bill.Pay(earn.ID, billing.PaymentRequest{
			Amount: 40000, PaymentMethod: "CASH", IdempotencyKey: "hd10-earn",
			Payer: billing.PatientPayerRequest(),
		}, 502); e != nil {
			t.Fatal(e)
		}
		cn, e := bill.IssueCreditNote(earn.ID, billing.CreditNoteRequest{Amount: 8000, Reason: "Sans session", IdempotencyKey: "hd10-cn"}, 502)
		if e != nil || cn.CreditHolderPartyID == nil {
			t.Fatal(e)
		}
		target := seedCashInvoice(t, db, "INV-HD10-T", 502, 8000, 502)
		if _, e := bill.ApplyCredit(target.ID, billing.CreditApplicationRequest{
			HolderPartyID: *cn.CreditHolderPartyID, Amount: 8000, IdempotencyKey: "hd10-app",
		}, 502); e != nil {
			t.Fatalf("H-D10 %v", e)
		}
		got, _ := bill.GetInvoice(target.ID)
		if got.Status != billing.InvoicePaid || got.PaidAmount != 0 {
			t.Fatalf("H-D10 %+v", got)
		}
	})
}
