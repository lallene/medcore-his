package cash

import (
	"fmt"
	"sync"
	"testing"

	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"github.com/lallene/medcore-his/backend/internal/modules/billing"
)

func TestLOT29F_D_OpenCashSessionReversal(t *testing.T) {
	t.Run("RV_C01_C17_open_cash_reversal", func(t *testing.T) {
		db := cashDB(t)
		db.Create(&cashUser{ID: 61, Name: "Dir"})
		db.Create(&cashPatient{ID: 61, Nom: "P", Prenoms: "R", CodePatient: "P-RVC01"})
		s := NewService(db)
		bill := billing.NewService(db)
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "RVC01", Name: "C"}, 61)
		open, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 10000, IdempotencyKey: "rvc01-o"}, 61)
		inv := seedCashInvoice(t, db, "INV-RVC01", 61, 20000, 61)
		rec, e := s.Pay(open.Session.ID, PaymentRequest{
			InvoiceID: inv.ID, Amount: 20000, PaymentMethod: "CASH", IdempotencyKey: "rvc01-p",
		}, 61)
		if e != nil {
			t.Fatal(e)
		}
		beforePay := billing.Payment{}
		if e := db.First(&beforePay, rec.PaymentID).Error; e != nil {
			t.Fatal(e)
		}
		out, e := bill.ReversePayment(rec.PaymentID, billing.ReversePaymentRequest{
			Reason: "Erreur de saisie caisse", IdempotencyKey: "rvc01-rev",
		}, 61)
		if e != nil {
			t.Fatalf("RV-C01 %v", e)
		}
		if out.Status != billing.InvoiceIssued || out.PaidAmount != 0 || out.BalanceAmount != 20000 {
			t.Fatalf("RV-C11 invoice %+v", out)
		}
		afterPay := billing.Payment{}
		db.First(&afterPay, rec.PaymentID)
		if afterPay.Amount != beforePay.Amount || afterPay.PaymentMethod != beforePay.PaymentMethod ||
			afterPay.PaidAt != beforePay.PaidAt || afterPay.CashSessionID == nil {
			t.Fatal("RV-C02 payment mutated")
		}
		var revs int64
		db.Model(&billing.PaymentReversal{}).Count(&revs)
		if revs != 1 {
			t.Fatal("RV-C03")
		}
		var movs []CashMovement
		db.Where("cash_session_id=?", open.Session.ID).Find(&movs)
		if len(movs) != 1 {
			t.Fatalf("RV-C04 movs=%d", len(movs))
		}
		m := movs[0]
		if m.Direction != MovementOut || m.Type != MovementPaymentReversal || m.Amount != 20000 {
			t.Fatalf("RV-C05/09 %+v", m)
		}
		if m.CreatedBy != 61 || m.CashSessionID != open.Session.ID {
			t.Fatalf("RV-C06/07 %+v", m)
		}
		if m.ReferenceType != MovementRefPaymentReversal || m.ReferenceID == nil {
			t.Fatal("RV-C08 reference")
		}
		var rev billing.PaymentReversal
		db.First(&rev, *m.ReferenceID)
		if rev.OriginalPaymentID != rec.PaymentID {
			t.Fatal("RV-C08 link")
		}
		if m.Reason == "" || m.Reason == "Erreur de saisie caisse" {
			// server-derived, not raw client reason dump required — must be non-empty derived
			if m.Reason == "" {
				t.Fatal("RV-C10 empty reason")
			}
		}
		var manual int64
		db.Model(&CashMovement{}).Where("type=?", MovementManualOut).Count(&manual)
		if manual != 0 {
			t.Fatal("RV-C17 manual")
		}
		var cn int64
		db.Model(&billing.CreditNote{}).Count(&cn)
		if cn != 0 {
			t.Fatal("RV-C16 credit note")
		}
		sum, _ := s.Get(open.Session.ID)
		if sum.CashCollected != 20000 || sum.CashMovementReversalOut != 20000 || sum.ExpectedCash != 10000 {
			t.Fatalf("RV-C37/38/39 %+v", sum)
		}
		if sum.OperationCount != 1 {
			t.Fatalf("RV-C41 ops=%d", sum.OperationCount)
		}
		if !out.Payments[0].Reversed || out.Payments[0].ReceiptID == nil {
			t.Fatal("RV-C13/14 receipt/reversed")
		}
	})

	t.Run("RV_C18_C23_idempotency_uniqueness", func(t *testing.T) {
		db := cashDB(t)
		db.Create(&cashUser{ID: 62, Name: "Dir"})
		db.Create(&cashPatient{ID: 62, Nom: "P", Prenoms: "R", CodePatient: "P-RVC18"})
		s := NewService(db)
		bill := billing.NewService(db)
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "RVC18", Name: "C"}, 62)
		open, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 0, IdempotencyKey: "rvc18-o"}, 62)
		inv := seedCashInvoice(t, db, "INV-RVC18", 62, 8000, 62)
		rec, _ := s.Pay(open.Session.ID, PaymentRequest{
			InvoiceID: inv.ID, Amount: 8000, PaymentMethod: "CASH", IdempotencyKey: "rvc18-p",
		}, 62)
		req := billing.ReversePaymentRequest{Reason: "Replay motif", IdempotencyKey: "rvc18-k"}
		a, e := bill.ReversePayment(rec.PaymentID, req, 62)
		if e != nil {
			t.Fatal(e)
		}
		b, e := bill.ReversePayment(rec.PaymentID, req, 62)
		if e != nil || b.ID != a.ID {
			t.Fatalf("RV-C18/19 %v", e)
		}
		var nMov int64
		db.Model(&CashMovement{}).Where("cash_session_id=? AND type=?", open.Session.ID, MovementPaymentReversal).Count(&nMov)
		if nMov != 1 {
			t.Fatalf("RV-C20 duplicate movement n=%d", nMov)
		}
		if _, e := bill.ReversePayment(rec.PaymentID, billing.ReversePaymentRequest{
			Reason: "Autre motif", IdempotencyKey: "rvc18-k",
		}, 62); cashErrCode(e) != "CONFLICT" {
			t.Fatalf("RV-C21 %v", e)
		}
		if _, e := bill.ReversePayment(rec.PaymentID, billing.ReversePaymentRequest{
			Reason: "Second reverse", IdempotencyKey: "rvc18-k2",
		}, 62); cashErrCode(e) != "CONFLICT" {
			t.Fatalf("RV-C22 %v", e)
		}
		// DB uniqueness on reference
		var rev billing.PaymentReversal
		db.Where("original_payment_id=?", rec.PaymentID).First(&rev)
		dup := CashMovement{
			CashSessionID: open.Session.ID, Direction: MovementOut, Type: MovementPaymentReversal,
			Amount: 1, Reason: "dup", ReferenceType: MovementRefPaymentReversal, ReferenceID: &rev.ID,
			CreatedBy: 62, OccurredAt: open.Session.OpenedAt, IdempotencyKey: "rvc23-dup",
		}
		if e := db.Create(&dup).Error; e == nil {
			t.Fatal("RV-C23 unique ref must reject")
		}
	})

	t.Run("RV_C24_C25_sessionless_unchanged", func(t *testing.T) {
		db := cashDB(t)
		db.Create(&cashUser{ID: 63, Name: "Dir"})
		db.Create(&cashPatient{ID: 63, Nom: "P", Prenoms: "R", CodePatient: "P-RVC24"})
		bill := billing.NewService(db)
		inv := seedCashInvoice(t, db, "INV-RVC24", 63, 4000, 63)
		paid, e := bill.Pay(inv.ID, billing.PaymentRequest{
			Amount: 4000, PaymentMethod: "CASH", IdempotencyKey: "rvc24-p",
		}, 63)
		if e != nil {
			t.Fatal(e)
		}
		if _, e := bill.ReversePayment(paid.Payments[0].ID, billing.ReversePaymentRequest{
			Reason: "Sessionless ok", IdempotencyKey: "rvc24-r",
		}, 63); e != nil {
			t.Fatalf("RV-C24 %v", e)
		}
		var n int64
		db.Model(&CashMovement{}).Count(&n)
		if n != 0 {
			t.Fatal("RV-C25 movement created")
		}
	})

	t.Run("RV_C26_C31_closed_cash_accounting_noncash_blocked", func(t *testing.T) {
		db := cashDB(t)
		db.Create(&cashUser{ID: 64, Name: "Dir"})
		db.Create(&cashPatient{ID: 64, Nom: "P", Prenoms: "R", CodePatient: "P-RVC26"})
		s := NewService(db)
		bill := billing.NewService(db)
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "RVC26", Name: "C"}, 64)
		open, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 5000, IdempotencyKey: "rvc26-o"}, 64)
		invCash := seedCashInvoice(t, db, "INV-RVC26C", 64, 2000, 64)
		recCash, _ := s.Pay(open.Session.ID, PaymentRequest{
			InvoiceID: invCash.ID, Amount: 2000, PaymentMethod: "CASH", IdempotencyKey: "rvc26-cash",
		}, 64)
		closed, e := s.Close(open.Session.ID, CloseRequest{CountedCashAmount: 7000, IdempotencyKey: "rvc26-c"}, 64, false)
		if e != nil {
			t.Fatal(e)
		}
		snapExp := *closed.Session.ExpectedCashAmount
		snapCnt := *closed.Session.CountedCashAmount
		snapDiff := *closed.Session.CashDifference
		beforeMov := movementCount(t, db)
		if _, e := bill.ReversePayment(recCash.PaymentID, billing.ReversePaymentRequest{
			Reason: "Apres close", IdempotencyKey: "rvc26-r",
		}, 64); e != nil {
			t.Fatalf("RV-C26 CLOSED CASH accounting reverse %v", e)
		}
		if movementCount(t, db) != beforeMov {
			t.Fatal("RV-C26 no movement on CLOSED")
		}
		again, _ := s.Get(open.Session.ID)
		if again.ExpectedCash != snapExp || *again.Session.CountedCashAmount != snapCnt || *again.Session.CashDifference != snapDiff {
			t.Fatal("RV-C27 snapshot immutable")
		}
		// Non-cash on a fresh open session still blocked
		open2, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 0, IdempotencyKey: "rvc28-o"}, 64)
		revBefore := int64(0)
		db.Model(&billing.PaymentReversal{}).Count(&revBefore)
		for i, method := range []string{"CARD", "MOBILE_MONEY", "BANK_TRANSFER", "CHECK"} {
			inv := seedCashInvoice(t, db, fmt.Sprintf("INV-RVC28-%d", i), 64, 1000, 64)
			req := PaymentRequest{
				InvoiceID: inv.ID, Amount: 1000, PaymentMethod: method, IdempotencyKey: fmt.Sprintf("rvc28-p-%d", i),
			}
			if method == "BANK_TRANSFER" || method == "CHECK" {
				req.ExternalReference = "QA-REF"
			}
			if method == "MOBILE_MONEY" {
				req.MobileOperator = "Orange Money"
			}
			rec, e := s.Pay(open2.Session.ID, req, 64)
			if e != nil {
				t.Fatal(e)
			}
			if _, e := bill.ReversePayment(rec.PaymentID, billing.ReversePaymentRequest{
				Reason: "Non cash", IdempotencyKey: fmt.Sprintf("rvc28-r-%d", i),
			}, 64); cashErrCode(e) != "CONFLICT" {
				t.Fatalf("RV-C28-31 %s %v", method, e)
			}
		}
		var nRev int64
		db.Model(&billing.PaymentReversal{}).Count(&nRev)
		if nRev != revBefore {
			t.Fatalf("non-cash must not create reversals; want %d got %d", revBefore, nRev)
		}
	})

	t.Run("RV_C32_C34_reversal_vs_close", func(t *testing.T) {
		db := cashDB(t)
		db.Create(&cashUser{ID: 65, Name: "Dir"})
		db.Create(&cashPatient{ID: 65, Nom: "P", Prenoms: "R", CodePatient: "P-RVC32"})
		s := NewService(db)
		bill := billing.NewService(db)
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "RVC32", Name: "C"}, 65)
		open, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 1000, IdempotencyKey: "rvc32-o"}, 65)
		inv := seedCashInvoice(t, db, "INV-RVC32", 65, 5000, 65)
		rec, _ := s.Pay(open.Session.ID, PaymentRequest{
			InvoiceID: inv.ID, Amount: 5000, PaymentMethod: "CASH", IdempotencyKey: "rvc32-p",
		}, 65)
		if _, e := bill.ReversePayment(rec.PaymentID, billing.ReversePaymentRequest{
			Reason: "Avant close", IdempotencyKey: "rvc32-r",
		}, 65); e != nil {
			t.Fatal(e)
		}
		closed, e := s.Close(open.Session.ID, CloseRequest{CountedCashAmount: 1000, IdempotencyKey: "rvc32-c"}, 65, false)
		if e != nil || closed.ExpectedCash != 1000 || closed.CashMovementReversalOut != 5000 {
			t.Fatalf("RV-C32 %+v %v", closed, e)
		}

		// Concurrency: close vs reverse on another session
		openB, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 2000, IdempotencyKey: "rvc34-o"}, 65)
		invB := seedCashInvoice(t, db, "INV-RVC34", 65, 2000, 65)
		recB, _ := s.Pay(openB.Session.ID, PaymentRequest{
			InvoiceID: invB.ID, Amount: 2000, PaymentMethod: "CASH", IdempotencyKey: "rvc34-p",
		}, 65)
		var closeErr, revErr error
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			// Note covers both outcomes: close-first (expected 4000) or reverse-first (expected 2000).
			_, closeErr = s.Close(openB.Session.ID, CloseRequest{
				CountedCashAmount: 4000, Note: "race close", IdempotencyKey: "rvc34-c",
			}, 65, false)
		}()
		go func() {
			defer wg.Done()
			_, revErr = bill.ReversePayment(recB.PaymentID, billing.ReversePaymentRequest{
				Reason: "Race close", IdempotencyKey: "rvc34-r",
			}, 65)
		}()
		wg.Wait()
		if closeErr != nil && revErr != nil {
			t.Fatalf("RV-C34 both failed %v %v", closeErr, revErr)
		}
		if closeErr != nil || revErr != nil {
			// Close and reverse may serialize either way; neither may hard-fail under lock.
			t.Fatalf("RV-C34 unexpected error close=%v rev=%v", closeErr, revErr)
		}
		sum, _ := s.Get(openB.Session.ID)
		if sum.Session.Status != SessionClosed {
			t.Fatal("RV-C34 expected closed")
		}
		// Reverse-first (OPEN): OUT included → expected 2000, reversalOut 2000.
		// Close-first (CLOSED): PCR1 accounting-only → expected 4000, reversalOut 0.
		if sum.CashMovementReversalOut == 2000 {
			if *sum.Session.ExpectedCashAmount != 2000 {
				t.Fatalf("RV-C34 reverse-first %+v", sum)
			}
		} else if sum.CashMovementReversalOut == 0 {
			if *sum.Session.ExpectedCashAmount != 4000 {
				t.Fatalf("RV-C34 close-first %+v", sum)
			}
		} else {
			t.Fatalf("RV-C34 incoherent %+v", sum)
		}
	})

	t.Run("RV_C35_C40_manual_and_breakdown", func(t *testing.T) {
		db := cashDB(t)
		db.Create(&cashUser{ID: 66, Name: "Dir"})
		db.Create(&cashPatient{ID: 66, Nom: "P", Prenoms: "R", CodePatient: "P-RVC35"})
		s := NewService(db)
		bill := billing.NewService(db)
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "RVC35", Name: "C"}, 66)
		open, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 10000, IdempotencyKey: "rvc35-o"}, 66)
		if _, e := s.CreateMovement(open.Session.ID, MovementRequest{
			Direction: MovementOut, Type: MovementManualOut, Amount: 1000, Reason: "Retrait manuel", IdempotencyKey: "rvc35-m",
		}, 66); e != nil {
			t.Fatal(e)
		}
		inv := seedCashInvoice(t, db, "INV-RVC35", 66, 3000, 66)
		rec, _ := s.Pay(open.Session.ID, PaymentRequest{
			InvoiceID: inv.ID, Amount: 3000, PaymentMethod: "CASH", IdempotencyKey: "rvc35-p",
		}, 66)
		if _, e := bill.ReversePayment(rec.PaymentID, billing.ReversePaymentRequest{
			Reason: "Avec manuel", IdempotencyKey: "rvc35-r",
		}, 66); e != nil {
			t.Fatal(e)
		}
		sum, _ := s.Get(open.Session.ID)
		// expected = 10000 + 3000 - 1000 - 3000 = 9000
		if sum.ExpectedCash != 9000 || sum.CashMovementManualOut != 1000 || sum.CashMovementReversalOut != 3000 {
			t.Fatalf("RV-C35/40 %+v", sum)
		}
		if sum.CashMovementOut != 4000 {
			t.Fatalf("total out %+v", sum)
		}
	})

	t.Run("RV_C47_C50_rbac_boundary", func(t *testing.T) {
		cai := rbac.EffectiveStaffPermissions("staff", []string{"CAISSIER"}, nil)
		if rbac.HasAnyPermission(cai, "billing.payment.reverse") {
			t.Fatal("RV-C48 CAISSIER must not reverse")
		}
		comp := rbac.EffectiveStaffPermissions("staff", []string{"COMPTABLE"}, nil)
		if !rbac.HasAnyPermission(comp, "billing.payment.reverse") {
			t.Fatal("RV-C47 COMPTABLE reverse")
		}
		if rbac.HasAnyPermission(comp, "cash.movement.create") {
			t.Fatal("RV-C50 system movement must not require cash.movement.create")
		}
	})
}
