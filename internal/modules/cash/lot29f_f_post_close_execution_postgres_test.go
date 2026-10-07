package cash

import (
	"sync"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"github.com/lallene/medcore-his/backend/internal/modules/billing"
)

type ExpectedSnap struct {
	Expected, Counted, Diff int64
	ClosedBy                uint
	ClosedAt                time.Time
	Note                    string
}

func TestLOT29F_F_PostCloseCashExecution(t *testing.T) {
	t.Run("PCE_C01_C29_happy_path", func(t *testing.T) {
		db := cashDB(t)
		db.Create(&cashUser{ID: 81, Name: "Dir"})
		db.Create(&cashPatient{ID: 81, Nom: "P", Prenoms: "F", CodePatient: "P-PCE01"})
		s := NewService(db)
		bill := billing.NewService(db)
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "PCE01", Name: "C"}, 81)
		open, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 10000, IdempotencyKey: "pce01-o"}, 81)
		inv := seedCashInvoice(t, db, "INV-PCE01", 81, 5000, 81)
		rec, e := s.Pay(open.Session.ID, PaymentRequest{
			InvoiceID: inv.ID, Amount: 5000, PaymentMethod: "CASH", IdempotencyKey: "pce01-p", Payer: &PayerRequest{Mode: "PATIENT"}}, 81)
		if e != nil {
			t.Fatal(e)
		}
		closed, e := s.Close(open.Session.ID, CloseRequest{
			CountedCashAmount: 15000, Note: "s1", IdempotencyKey: "pce01-c",
		}, 81, false)
		if e != nil {
			t.Fatal(e)
		}
		snap := ExpectedSnap{
			Expected: *closed.Session.ExpectedCashAmount,
			Counted:  *closed.Session.CountedCashAmount,
			Diff:     *closed.Session.CashDifference,
			ClosedBy: *closed.Session.ClosedBy,
			ClosedAt: *closed.Session.ClosedAt,
			Note:     closed.Session.ClosingNote,
		}
		beforeMov := movementCount(t, db)
		revInv, e := bill.ReversePayment(rec.PaymentID, billing.ReversePaymentRequest{
			Reason: "Post close accounting", IdempotencyKey: "pce01-r",
		}, 81)
		if e != nil {
			t.Fatal(e)
		}
		if movementCount(t, db) != beforeMov {
			t.Fatal("PCE-C52 reversal alone zero movement")
		}
		var rev billing.PaymentReversal
		if e := db.Where("original_payment_id=?", rec.PaymentID).First(&rev).Error; e != nil {
			t.Fatal(e)
		}
		invAfterT3 := *revInv

		open2, e := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 20000, IdempotencyKey: "pce01-o2"}, 81)
		if e != nil {
			t.Fatal(e)
		}
		elig, e := s.CorrectionEligibilityForPayment(rec.PaymentID)
		if e != nil || !elig.Eligible || elig.HostSession == nil || elig.HostSession.Session.ID != open2.Session.ID {
			t.Fatalf("PCE-C01 eligibility %+v %v", elig, e)
		}
		beforeHostMov := movementCount(t, db)
		exec, e := s.ExecutePostCloseCorrection(ExecuteCorrectionRequest{
			PaymentReversalID: rev.ID,
			HostSessionID:     &open2.Session.ID,
			Note:              "sortie physique",
			IdempotencyKey:    "pce01-x",
		}, 81)
		if e != nil {
			t.Fatalf("PCE-C01 %v", e)
		}
		if exec.Amount != 5000 || exec.HostCashSessionID != open2.Session.ID || exec.OriginalCashSessionID != open.Session.ID {
			t.Fatalf("PCE-C12/C17 %+v", exec)
		}
		if exec.CashMovementID == nil || *exec.CashMovementID == 0 || exec.ExecutedBy != 81 {
			t.Fatal("PCE-C13/C20")
		}
		if movementCount(t, db) != beforeHostMov+1 {
			t.Fatal("PCE-C13 one movement")
		}
		var m CashMovement
		if e := db.First(&m, *exec.CashMovementID).Error; e != nil {
			t.Fatal(e)
		}
		if m.Direction != MovementOut || m.Type != MovementPostCloseCorrectionOut || m.Amount != 5000 {
			t.Fatalf("PCE-C14/C15/C16 %+v", m)
		}
		if m.CashSessionID != open2.Session.ID || m.CashSessionID == open.Session.ID {
			t.Fatal("PCE-C17/C18")
		}
		if m.ReferenceType != MovementRefCashCorrectionExec || m.ReferenceID == nil || *m.ReferenceID != exec.ID {
			t.Fatalf("PCE-C19 %+v", m)
		}
		again1, _ := s.Get(open.Session.ID)
		if *again1.Session.ExpectedCashAmount != snap.Expected ||
			*again1.Session.CountedCashAmount != snap.Counted ||
			*again1.Session.CashDifference != snap.Diff ||
			*again1.Session.ClosedBy != snap.ClosedBy ||
			again1.Session.ClosingNote != snap.Note ||
			!again1.Session.ClosedAt.Equal(snap.ClosedAt) {
			t.Fatalf("PCE-C22–C25 %+v", again1.Session)
		}
		host, _ := s.Get(open2.Session.ID)
		if host.ExpectedCash != 15000 || host.CashCollected != 0 || host.TotalCollected != 0 {
			t.Fatalf("PCE-C26–C28 %+v", host)
		}
		if host.CashMovementPostCloseCorrectionOut != 5000 || host.CashMovementReversalOut != 0 || host.CashMovementManualOut != 0 {
			t.Fatalf("PCE-C29 %+v", host)
		}
		got, _ := bill.GetInvoice(invAfterT3.ID)
		if got.PaidAmount != invAfterT3.PaidAmount || got.BalanceAmount != invAfterT3.BalanceAmount || got.Status != invAfterT3.Status {
			t.Fatalf("PCE-C43 invoice mutated %+v", got)
		}
		var pay billing.Payment
		db.First(&pay, rec.PaymentID)
		if pay.Amount != 5000 {
			t.Fatal("PCE-C45")
		}
		var rev2 billing.PaymentReversal
		db.First(&rev2, rev.ID)
		if rev2.Amount != rev.Amount || rev2.Reason != rev.Reason {
			t.Fatal("PCE-C46")
		}
		rcpt, _ := s.Receipt(rec.ID)
		if rcpt == nil || !rcpt.PaymentReversed || !rcpt.CashCorrectionExecuted {
			t.Fatalf("PCE-C47 %+v", rcpt)
		}
		var cn int64
		db.Model(&billing.CreditNote{}).Count(&cn)
		if cn != 0 {
			t.Fatal("PCE-C48")
		}
	})

	t.Run("PCE_C02_C11_eligibility_guards", func(t *testing.T) {
		db := cashDB(t)
		db.Create(&cashUser{ID: 82, Name: "Dir"})
		db.Create(&cashPatient{ID: 82, Nom: "P", Prenoms: "F", CodePatient: "P-PCE02"})
		s := NewService(db)
		bill := billing.NewService(db)
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "PCE02", Name: "C"}, 82)
		open, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 5000, IdempotencyKey: "pce02-o"}, 82)
		inv := seedCashInvoice(t, db, "INV-PCE02", 82, 2000, 82)
		rec, _ := s.Pay(open.Session.ID, PaymentRequest{
			InvoiceID: inv.ID, Amount: 2000, PaymentMethod: "CASH", IdempotencyKey: "pce02-p", Payer: &PayerRequest{Mode: "PATIENT"}}, 82)
		if _, e := s.ExecutePostCloseCorrection(ExecuteCorrectionRequest{
			PaymentReversalID: 99999, IdempotencyKey: "pce02-x0",
		}, 82); e == nil || cashErrCode(e) != "PAYMENT_REVERSAL_NOT_FOUND" {
			t.Fatalf("PCE-C02 %v code=%s", e, cashErrCode(e))
		}
		_, _ = s.Close(open.Session.ID, CloseRequest{CountedCashAmount: 7000, IdempotencyKey: "pce02-c"}, 82, false)
		// OPEN reverse path must not become post-close execution target without close+reverse after.
		openB, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 10000, IdempotencyKey: "pce02-ob"}, 82)
		invB := seedCashInvoice(t, db, "INV-PCE02B", 82, 1000, 82)
		recB, _ := s.Pay(openB.Session.ID, PaymentRequest{
			InvoiceID: invB.ID, Amount: 1000, PaymentMethod: "CASH", IdempotencyKey: "pce02-pb", Payer: &PayerRequest{Mode: "PATIENT"}}, 82)
		if _, e := bill.ReversePayment(recB.PaymentID, billing.ReversePaymentRequest{
			Reason: "Open reverse", IdempotencyKey: "pce02-rb",
		}, 82); e != nil {
			t.Fatal(e)
		}
		var revOpen billing.PaymentReversal
		db.Where("original_payment_id=?", recB.PaymentID).First(&revOpen)
		if _, e := s.ExecutePostCloseCorrection(ExecuteCorrectionRequest{
			PaymentReversalID: revOpen.ID, IdempotencyKey: "pce02-xopen",
		}, 82); cashErrCode(e) != "CONFLICT" {
			t.Fatalf("PCE-C03/C05 open reverse not post-close %v", e)
		}
		sumB, _ := s.Get(openB.Session.ID)
		if _, e := s.Close(openB.Session.ID, CloseRequest{
			CountedCashAmount: sumB.ExpectedCash, IdempotencyKey: "pce02-cb",
		}, 82, false); e != nil {
			t.Fatalf("close openB %v", e)
		}

		if _, e := bill.ReversePayment(rec.PaymentID, billing.ReversePaymentRequest{
			Reason: "Closed reverse", IdempotencyKey: "pce02-r",
		}, 82); e != nil {
			t.Fatal(e)
		}
		var rev billing.PaymentReversal
		db.Where("original_payment_id=?", rec.PaymentID).First(&rev)
		if _, e := s.ExecutePostCloseCorrection(ExecuteCorrectionRequest{
			PaymentReversalID: rev.ID, IdempotencyKey: "pce02-xnone",
		}, 82); cashErrCode(e) != "CONFLICT" {
			t.Fatalf("PCE-C09 no host %v", e)
		}
		reg2, _ := s.SaveRegister(0, RegisterRequest{Code: "PCE02B", Name: "Other"}, 82)
		openOther, _ := s.Open(OpenRequest{CashRegisterID: reg2.ID, OpeningFloat: 50000, IdempotencyKey: "pce02-other"}, 82)
		if _, e := s.ExecutePostCloseCorrection(ExecuteCorrectionRequest{
			PaymentReversalID: rev.ID, HostSessionID: &openOther.Session.ID, IdempotencyKey: "pce02-xreg",
		}, 82); cashErrCode(e) != "CONFLICT" {
			t.Fatalf("PCE-C08 different register %v", e)
		}
		_, _ = s.Close(openOther.Session.ID, CloseRequest{CountedCashAmount: 50000, IdempotencyKey: "pce02-cother"}, 82, false)
		openSame, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 100, IdempotencyKey: "pce02-same"}, 82)
		if _, e := s.ExecutePostCloseCorrection(ExecuteCorrectionRequest{
			PaymentReversalID: rev.ID, HostSessionID: &openSame.Session.ID, IdempotencyKey: "pce02-xcash",
		}, 82); cashErrCode(e) != "CONFLICT" {
			t.Fatalf("PCE-C30 insufficient %v", e)
		}
		var nPost int64
		db.Model(&CashMovement{}).Where("type=?", MovementPostCloseCorrectionOut).Count(&nPost)
		if nPost != 0 {
			t.Fatal("PCE-C31 side effects")
		}
		_ = openSame
	})

	t.Run("PCE_C33_C38_idempotency", func(t *testing.T) {
		db := cashDB(t)
		db.Create(&cashUser{ID: 83, Name: "Dir"})
		db.Create(&cashPatient{ID: 83, Nom: "P", Prenoms: "F", CodePatient: "P-PCE33"})
		s := NewService(db)
		bill := billing.NewService(db)
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "PCE33", Name: "C"}, 83)
		open, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 0, IdempotencyKey: "pce33-o"}, 83)
		inv := seedCashInvoice(t, db, "INV-PCE33", 83, 3000, 83)
		rec, _ := s.Pay(open.Session.ID, PaymentRequest{
			InvoiceID: inv.ID, Amount: 3000, PaymentMethod: "CASH", IdempotencyKey: "pce33-p", Payer: &PayerRequest{Mode: "PATIENT"}}, 83)
		_, _ = s.Close(open.Session.ID, CloseRequest{CountedCashAmount: 3000, IdempotencyKey: "pce33-c"}, 83, false)
		_, _ = bill.ReversePayment(rec.PaymentID, billing.ReversePaymentRequest{
			Reason: "Rev", IdempotencyKey: "pce33-r",
		}, 83)
		var rev billing.PaymentReversal
		db.Where("original_payment_id=?", rec.PaymentID).First(&rev)
		open2, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 10000, IdempotencyKey: "pce33-o2"}, 83)
		req := ExecuteCorrectionRequest{
			PaymentReversalID: rev.ID, HostSessionID: &open2.Session.ID, IdempotencyKey: "pce33-x",
		}
		a, e := s.ExecutePostCloseCorrection(req, 83)
		if e != nil {
			t.Fatal(e)
		}
		b, e := s.ExecutePostCloseCorrection(req, 83)
		if e != nil || a.ID != b.ID || *a.CashMovementID != *b.CashMovementID {
			t.Fatalf("PCE-C33–C35 %+v %+v %v", a, b, e)
		}
		var nExec, nMov int64
		db.Model(&CashCorrectionExecution{}).Count(&nExec)
		db.Model(&CashMovement{}).Where("type=?", MovementPostCloseCorrectionOut).Count(&nMov)
		if nExec != 1 || nMov != 1 {
			t.Fatalf("PCE-C38 uniq exec=%d mov=%d", nExec, nMov)
		}
		if _, e := s.ExecutePostCloseCorrection(ExecuteCorrectionRequest{
			PaymentReversalID: rev.ID, HostSessionID: &open2.Session.ID, Note: "other", IdempotencyKey: "pce33-x",
		}, 83); cashErrCode(e) != "CONFLICT" {
			t.Fatalf("PCE-C36 %v", e)
		}
		if _, e := s.ExecutePostCloseCorrection(ExecuteCorrectionRequest{
			PaymentReversalID: rev.ID, HostSessionID: &open2.Session.ID, IdempotencyKey: "pce33-x2",
		}, 83); cashErrCode(e) != "CONFLICT" {
			t.Fatalf("PCE-C37 %v", e)
		}
	})

	t.Run("PCE_C39_C42_exec_vs_close_and_race", func(t *testing.T) {
		db := cashDB(t)
		db.Create(&cashUser{ID: 84, Name: "Dir"})
		db.Create(&cashPatient{ID: 84, Nom: "P", Prenoms: "F", CodePatient: "P-PCE39"})
		s := NewService(db)
		bill := billing.NewService(db)
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "PCE39", Name: "C"}, 84)
		open, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 0, IdempotencyKey: "pce39-o"}, 84)
		inv := seedCashInvoice(t, db, "INV-PCE39", 84, 2000, 84)
		rec, _ := s.Pay(open.Session.ID, PaymentRequest{
			InvoiceID: inv.ID, Amount: 2000, PaymentMethod: "CASH", IdempotencyKey: "pce39-p", Payer: &PayerRequest{Mode: "PATIENT"}}, 84)
		_, _ = s.Close(open.Session.ID, CloseRequest{CountedCashAmount: 2000, IdempotencyKey: "pce39-c"}, 84, false)
		_, _ = bill.ReversePayment(rec.PaymentID, billing.ReversePaymentRequest{
			Reason: "Rev", IdempotencyKey: "pce39-r",
		}, 84)
		var rev billing.PaymentReversal
		db.Where("original_payment_id=?", rec.PaymentID).First(&rev)

		// execution wins then close
		open2, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 5000, IdempotencyKey: "pce39-o2"}, 84)
		exec, e := s.ExecutePostCloseCorrection(ExecuteCorrectionRequest{
			PaymentReversalID: rev.ID, HostSessionID: &open2.Session.ID, IdempotencyKey: "pce39-x",
		}, 84)
		if e != nil {
			t.Fatal(e)
		}
		closed, e := s.Close(open2.Session.ID, CloseRequest{CountedCashAmount: 3000, IdempotencyKey: "pce39-c2"}, 84, false)
		if e != nil || closed.ExpectedCash != 3000 || closed.CashMovementPostCloseCorrectionOut != 2000 {
			t.Fatalf("PCE-C39 %+v %v", closed, e)
		}
		_ = exec

		// close wins: prepare second payment/reversal
		open3, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 0, IdempotencyKey: "pce39-o3"}, 84)
		inv3 := seedCashInvoice(t, db, "INV-PCE39B", 84, 1500, 84)
		rec3, _ := s.Pay(open3.Session.ID, PaymentRequest{
			InvoiceID: inv3.ID, Amount: 1500, PaymentMethod: "CASH", IdempotencyKey: "pce39-p3", Payer: &PayerRequest{Mode: "PATIENT"}}, 84)
		_, _ = s.Close(open3.Session.ID, CloseRequest{CountedCashAmount: 1500, IdempotencyKey: "pce39-c3"}, 84, false)
		_, _ = bill.ReversePayment(rec3.PaymentID, billing.ReversePaymentRequest{
			Reason: "Rev2", IdempotencyKey: "pce39-r3",
		}, 84)
		var rev3 billing.PaymentReversal
		db.Where("original_payment_id=?", rec3.PaymentID).First(&rev3)
		open4, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 8000, IdempotencyKey: "pce39-o4"}, 84)
		_, _ = s.Close(open4.Session.ID, CloseRequest{CountedCashAmount: 8000, IdempotencyKey: "pce39-c4"}, 84, false)
		if _, e := s.ExecutePostCloseCorrection(ExecuteCorrectionRequest{
			PaymentReversalID: rev3.ID, HostSessionID: &open4.Session.ID, IdempotencyKey: "pce39-x4",
		}, 84); cashErrCode(e) != "CONFLICT" {
			t.Fatalf("PCE-C40 close wins %v", e)
		}

		// concurrent duplicate execution
		open5, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 10000, IdempotencyKey: "pce39-o5"}, 84)
		var errA, errB error
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, errA = s.ExecutePostCloseCorrection(ExecuteCorrectionRequest{
				PaymentReversalID: rev3.ID, HostSessionID: &open5.Session.ID, IdempotencyKey: "pce39-race-a",
			}, 84)
		}()
		go func() {
			defer wg.Done()
			_, errB = s.ExecutePostCloseCorrection(ExecuteCorrectionRequest{
				PaymentReversalID: rev3.ID, HostSessionID: &open5.Session.ID, IdempotencyKey: "pce39-race-b",
			}, 84)
		}()
		wg.Wait()
		okN := 0
		if errA == nil {
			okN++
		}
		if errB == nil {
			okN++
		}
		if okN != 1 {
			t.Fatalf("PCE-C41/C42 ok=%d a=%v b=%v", okN, errA, errB)
		}
		var nExec int64
		db.Model(&CashCorrectionExecution{}).Where("payment_reversal_id=?", rev3.ID).Count(&nExec)
		if nExec != 1 {
			t.Fatal("PCE-C42")
		}
	})

	t.Run("PCE_C53_C55_regressions", func(t *testing.T) {
		db := cashDB(t)
		db.Create(&cashUser{ID: 85, Name: "Dir"})
		db.Create(&cashPatient{ID: 85, Nom: "P", Prenoms: "F", CodePatient: "P-PCE53"})
		s := NewService(db)
		bill := billing.NewService(db)
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "PCE53", Name: "C"}, 85)
		open, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 1000, IdempotencyKey: "pce53-o"}, 85)
		inv := seedCashInvoice(t, db, "INV-PCE53", 85, 1000, 85)
		rec, _ := s.Pay(open.Session.ID, PaymentRequest{
			InvoiceID: inv.ID, Amount: 1000, PaymentMethod: "CASH", IdempotencyKey: "pce53-p", Payer: &PayerRequest{Mode: "PATIENT"}}, 85)
		if _, e := bill.ReversePayment(rec.PaymentID, billing.ReversePaymentRequest{
			Reason: "Open still", IdempotencyKey: "pce53-r",
		}, 85); e != nil {
			t.Fatal(e)
		}
		var m CashMovement
		if e := db.Where("type=? AND cash_session_id=?", MovementPaymentReversal, open.Session.ID).First(&m).Error; e != nil {
			t.Fatal("PCE-C53/C54")
		}
		_, _ = s.Close(open.Session.ID, CloseRequest{CountedCashAmount: 1000, IdempotencyKey: "pce53-c"}, 85, false)
		inv2 := seedCashInvoice(t, db, "INV-PCE55", 85, 500, 85)
		paid, _ := bill.Pay(inv2.ID, billing.PaymentRequest{
			Amount: 500, PaymentMethod: "CASH", IdempotencyKey: "pce55-p", Payer: billing.PatientPayerRequest()}, 85)
		before := movementCount(t, db)
		if _, e := bill.ReversePayment(paid.Payments[0].ID, billing.ReversePaymentRequest{
			Reason: "Sessionless", IdempotencyKey: "pce55-r",
		}, 85); e != nil {
			t.Fatal(e)
		}
		if movementCount(t, db) != before {
			t.Fatal("PCE-C55")
		}
	})

	t.Run("PCE_C56_C58_rbac", func(t *testing.T) {
		cai := rbac.EffectiveStaffPermissions("staff", []string{"CAISSIER"}, nil)
		if rbac.HasAnyPermission(cai, "cash.correction.execute") {
			t.Fatal("PCE-C57")
		}
		comp := rbac.EffectiveStaffPermissions("staff", []string{"COMPTABLE"}, nil)
		if rbac.HasAnyPermission(comp, "cash.correction.execute") {
			t.Fatal("PCE-C58 comptable must not execute")
		}
		if !rbac.HasAnyPermission(comp, "cash.correction.read") {
			t.Fatal("comptable read")
		}
		dir := rbac.EffectiveStaffPermissions("staff", []string{"DIRECTEUR_ADMINISTRATIF"}, nil)
		if !rbac.HasAnyPermission(dir, "cash.correction.execute") || !rbac.HasAnyPermission(dir, "cash.correction.read") {
			t.Fatal("PCE-C56 director")
		}
	})
}
