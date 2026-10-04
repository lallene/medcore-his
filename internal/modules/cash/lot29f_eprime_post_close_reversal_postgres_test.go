package cash

import (
	"sync"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"github.com/lallene/medcore-his/backend/internal/modules/billing"
)

func TestLOT29F_EPrime_PostCloseCashReversal(t *testing.T) {
	t.Run("PCR_C01_C22_closed_cash_accounting", func(t *testing.T) {
		db := cashDB(t)
		db.Create(&cashUser{ID: 71, Name: "Dir"})
		db.Create(&cashPatient{ID: 71, Nom: "P", Prenoms: "E", CodePatient: "P-PCR01"})
		s := NewService(db)
		bill := billing.NewService(db)
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "PCR01", Name: "C"}, 71)
		open, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 10000, IdempotencyKey: "pcr01-o"}, 71)
		inv := seedCashInvoice(t, db, "INV-PCR01", 71, 20000, 71)
		rec, e := s.Pay(open.Session.ID, PaymentRequest{
			InvoiceID: inv.ID, Amount: 20000, PaymentMethod: "CASH", IdempotencyKey: "pcr01-p",
		}, 71)
		if e != nil {
			t.Fatal(e)
		}
		closed, e := s.Close(open.Session.ID, CloseRequest{
			CountedCashAmount: 30000, Note: "cloture QA", IdempotencyKey: "pcr01-c",
		}, 71, false)
		if e != nil {
			t.Fatal(e)
		}
		exp, cnt, diff := *closed.Session.ExpectedCashAmount, *closed.Session.CountedCashAmount, *closed.Session.CashDifference
		closedAt, closedBy, note := *closed.Session.ClosedAt, *closed.Session.ClosedBy, closed.Session.ClosingNote
		beforeMov := movementCount(t, db)
		beforePay := int64(0)
		db.Model(&billing.Payment{}).Count(&beforePay)

		out, e := bill.ReversePayment(rec.PaymentID, billing.ReversePaymentRequest{
			Reason: "Erreur apres cloture", IdempotencyKey: "pcr01-r",
		}, 71)
		if e != nil {
			t.Fatalf("PCR-C01 %v", e)
		}
		if movementCount(t, db) != beforeMov {
			t.Fatal("PCR-C04/C05 movement")
		}
		var nPay int64
		db.Model(&billing.Payment{}).Count(&nPay)
		if nPay != beforePay {
			t.Fatal("PCR-C02 payment count")
		}
		var rev billing.PaymentReversal
		if e := db.Where("original_payment_id=?", rec.PaymentID).First(&rev).Error; e != nil || rev.Amount != 20000 || rev.ReversedBy != 71 {
			t.Fatalf("PCR-C03 %+v %v", rev, e)
		}
		if !rev.ReversedAt.After(closedAt) && !rev.ReversedAt.Equal(closedAt) {
			// Allow equal only if clock granularity collapses; prefer After.
			if rev.CreatedAt.Before(closedAt) {
				t.Fatal("PCR-C21 timestamp")
			}
		}
		again, _ := s.Get(open.Session.ID)
		if *again.Session.ExpectedCashAmount != exp || *again.Session.CountedCashAmount != cnt ||
			*again.Session.CashDifference != diff || *again.Session.ClosedBy != closedBy ||
			again.Session.ClosingNote != note || !again.Session.ClosedAt.Equal(closedAt) {
			t.Fatalf("PCR-C06–C11 snapshot %+v", again.Session)
		}
		if again.CashCollected != 20000 || again.CashMovementReversalOut != 0 {
			t.Fatalf("PCR-C13/C50 historical gross %+v", again)
		}
		if out.Status != billing.InvoiceIssued || out.PaidAmount != 0 {
			t.Fatalf("PCR-C12 invoice %+v", out)
		}
		got, _ := bill.GetInvoice(out.ID)
		var pay *billing.Payment
		for i := range got.Payments {
			if got.Payments[i].ID == rec.PaymentID {
				pay = &got.Payments[i]
				break
			}
		}
		if pay == nil || !pay.Reversed || !pay.PostCloseCorrection {
			t.Fatalf("PCR-C15/C20 decoration %+v", pay)
		}
		rcpt, e := s.Receipt(rec.ID)
		if e != nil || rcpt == nil || !rcpt.PaymentReversed {
			t.Fatalf("PCR-C14/C15 receipt %+v %v", rcpt, e)
		}
		var cn, refund int64
		db.Model(&billing.CreditNote{}).Count(&cn)
		db.Raw("SELECT COUNT(*) FROM information_schema.tables WHERE table_name='billing_refunds'").Scan(&refund)
		if cn != 0 {
			t.Fatal("PCR-C17 CreditNote")
		}
	})

	t.Run("PCR_C23_C27_idempotency", func(t *testing.T) {
		db := cashDB(t)
		db.Create(&cashUser{ID: 72, Name: "Dir"})
		db.Create(&cashPatient{ID: 72, Nom: "P", Prenoms: "E", CodePatient: "P-PCR23"})
		s := NewService(db)
		bill := billing.NewService(db)
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "PCR23", Name: "C"}, 72)
		open, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 1000, IdempotencyKey: "pcr23-o"}, 72)
		inv := seedCashInvoice(t, db, "INV-PCR23", 72, 4000, 72)
		rec, _ := s.Pay(open.Session.ID, PaymentRequest{
			InvoiceID: inv.ID, Amount: 4000, PaymentMethod: "CASH", IdempotencyKey: "pcr23-p",
		}, 72)
		_, _ = s.Close(open.Session.ID, CloseRequest{CountedCashAmount: 5000, IdempotencyKey: "pcr23-c"}, 72, false)
		req := billing.ReversePaymentRequest{Reason: "Replay post close", IdempotencyKey: "pcr23-r"}
		a, e := bill.ReversePayment(rec.PaymentID, req, 72)
		if e != nil {
			t.Fatal(e)
		}
		before := movementCount(t, db)
		b, e := bill.ReversePayment(rec.PaymentID, req, 72)
		if e != nil || a.ID != b.ID {
			t.Fatalf("PCR-C23/24 %v", e)
		}
		if movementCount(t, db) != before {
			t.Fatal("PCR-C25")
		}
		if _, e := bill.ReversePayment(rec.PaymentID, billing.ReversePaymentRequest{
			Reason: "Autre motif", IdempotencyKey: "pcr23-r",
		}, 72); cashErrCode(e) != "CONFLICT" {
			t.Fatalf("PCR-C26 %v", e)
		}
		if _, e := bill.ReversePayment(rec.PaymentID, billing.ReversePaymentRequest{
			Reason: "Second reverse", IdempotencyKey: "pcr23-r2",
		}, 72); cashErrCode(e) != "CONFLICT" {
			t.Fatalf("PCR-C27 %v", e)
		}
	})

	t.Run("PCR_C28_C32_open_and_sessionless", func(t *testing.T) {
		db := cashDB(t)
		db.Create(&cashUser{ID: 73, Name: "Dir"})
		db.Create(&cashPatient{ID: 73, Nom: "P", Prenoms: "E", CodePatient: "P-PCR28"})
		s := NewService(db)
		bill := billing.NewService(db)
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "PCR28", Name: "C"}, 73)
		open, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 5000, IdempotencyKey: "pcr28-o"}, 73)
		inv := seedCashInvoice(t, db, "INV-PCR28", 73, 3000, 73)
		rec, _ := s.Pay(open.Session.ID, PaymentRequest{
			InvoiceID: inv.ID, Amount: 3000, PaymentMethod: "CASH", IdempotencyKey: "pcr28-p",
		}, 73)
		if _, e := bill.ReversePayment(rec.PaymentID, billing.ReversePaymentRequest{
			Reason: "Open still moves", IdempotencyKey: "pcr28-r",
		}, 73); e != nil {
			t.Fatal(e)
		}
		sum, _ := s.Get(open.Session.ID)
		if sum.CashMovementReversalOut != 3000 || sum.ExpectedCash != 5000 {
			t.Fatalf("PCR-C28/C30 %+v", sum)
		}
		_, _ = s.Close(open.Session.ID, CloseRequest{CountedCashAmount: 5000, IdempotencyKey: "pcr28-c"}, 73, false)

		inv2 := seedCashInvoice(t, db, "INV-PCR31", 73, 1500, 73)
		paid2, e := bill.Pay(inv2.ID, billing.PaymentRequest{
			Amount: 1500, PaymentMethod: "CASH", IdempotencyKey: "pcr31-p",
		}, 73)
		if e != nil {
			t.Fatal(e)
		}
		before := movementCount(t, db)
		if _, e := bill.ReversePayment(paid2.Payments[0].ID, billing.ReversePaymentRequest{
			Reason: "Sessionless", IdempotencyKey: "pcr31-r",
		}, 73); e != nil {
			t.Fatal(e)
		}
		if movementCount(t, db) != before {
			t.Fatal("PCR-C32")
		}
	})

	t.Run("PCR_C33_C36_closed_noncash_blocked", func(t *testing.T) {
		db := cashDB(t)
		db.Create(&cashUser{ID: 74, Name: "Dir"})
		db.Create(&cashPatient{ID: 74, Nom: "P", Prenoms: "E", CodePatient: "P-PCR33"})
		s := NewService(db)
		bill := billing.NewService(db)
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "PCR33", Name: "C"}, 74)
		open, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 0, IdempotencyKey: "pcr33-o"}, 74)
		for i, method := range []string{"CARD", "MOBILE_MONEY", "BANK_TRANSFER", "CHECK"} {
			inv := seedCashInvoice(t, db, "INV-PCR33-"+method, 74, 1000, 74)
			req := PaymentRequest{
				InvoiceID: inv.ID, Amount: 1000, PaymentMethod: method, IdempotencyKey: "pcr33-p-" + method,
			}
			if method == "BANK_TRANSFER" || method == "CHECK" {
				req.ExternalReference = "QA-REF"
			}
			if method == "MOBILE_MONEY" {
				req.MobileOperator = "Orange Money"
			}
			rec, e := s.Pay(open.Session.ID, req, 74)
			if e != nil {
				t.Fatalf("%s pay %v", method, e)
			}
			_ = i
			_ = rec
		}
		_, _ = s.Close(open.Session.ID, CloseRequest{CountedCashAmount: 0, IdempotencyKey: "pcr33-c"}, 74, false)
		var pays []billing.Payment
		db.Where("cash_session_id=?", open.Session.ID).Find(&pays)
		if len(pays) != 4 {
			t.Fatalf("want 4 payments got %d", len(pays))
		}
		for _, p := range pays {
			if _, e := bill.ReversePayment(p.ID, billing.ReversePaymentRequest{
				Reason: "Non cash closed", IdempotencyKey: "pcr33-r-" + p.PaymentMethod,
			}, 74); cashErrCode(e) != "CONFLICT" {
				t.Fatalf("PCR-C33–C36 %s %v", p.PaymentMethod, e)
			}
		}
	})

	t.Run("PCR_C37_C40_close_vs_reversal_race", func(t *testing.T) {
		db := cashDB(t)
		db.Create(&cashUser{ID: 75, Name: "Dir"})
		db.Create(&cashPatient{ID: 75, Nom: "P", Prenoms: "E", CodePatient: "P-PCR37"})
		s := NewService(db)
		bill := billing.NewService(db)
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "PCR37", Name: "C"}, 75)
		open, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 2000, IdempotencyKey: "pcr37-o"}, 75)
		inv := seedCashInvoice(t, db, "INV-PCR37", 75, 2000, 75)
		rec, _ := s.Pay(open.Session.ID, PaymentRequest{
			InvoiceID: inv.ID, Amount: 2000, PaymentMethod: "CASH", IdempotencyKey: "pcr37-p",
		}, 75)
		var closeErr, revErr error
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, closeErr = s.Close(open.Session.ID, CloseRequest{
				CountedCashAmount: 4000, Note: "race", IdempotencyKey: "pcr37-c",
			}, 75, false)
		}()
		go func() {
			defer wg.Done()
			_, revErr = bill.ReversePayment(rec.PaymentID, billing.ReversePaymentRequest{
				Reason: "Race", IdempotencyKey: "pcr37-r",
			}, 75)
		}()
		wg.Wait()
		if closeErr != nil || revErr != nil {
			t.Fatalf("PCR-C39 %v %v", closeErr, revErr)
		}
		sum, _ := s.Get(open.Session.ID)
		if sum.Session.Status != SessionClosed {
			t.Fatal("PCR-C39 closed")
		}
		if sum.CashMovementReversalOut == 2000 {
			if *sum.Session.ExpectedCashAmount != 2000 {
				t.Fatalf("PCR-C37 open-win %+v", sum)
			}
		} else if sum.CashMovementReversalOut == 0 {
			if *sum.Session.ExpectedCashAmount != 4000 {
				t.Fatalf("PCR-C38 close-win %+v", sum)
			}
		} else {
			t.Fatalf("PCR-C40 half-state %+v", sum)
		}
	})

	t.Run("PCR_C45_C47_rbac", func(t *testing.T) {
		cai := rbac.EffectiveStaffPermissions("staff", []string{"CAISSIER"}, nil)
		if rbac.HasAnyPermission(cai, "billing.payment.reverse") {
			t.Fatal("PCR-C46")
		}
		comp := rbac.EffectiveStaffPermissions("staff", []string{"COMPTABLE"}, nil)
		dir := rbac.EffectiveStaffPermissions("staff", []string{"DIRECTEUR_ADMINISTRATIF"}, nil)
		if !rbac.HasAnyPermission(comp, "billing.payment.reverse") || !rbac.HasAnyPermission(dir, "billing.payment.reverse") {
			t.Fatal("PCR-C45 roles")
		}
		_ = time.Now()
	})
}
