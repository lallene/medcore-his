package cash

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/billing"
	"github.com/lallene/medcore-his/backend/internal/modules/medical_records"
	"gorm.io/gorm"
)

func ibBilling(t *testing.T, db *gorm.DB) *billing.Service {
	t.Helper()
	_ = db.AutoMigrate(&medical_records.MedicalRecord{}, &medical_records.MedicalTimelineEvent{}, &billing.DocumentNumberSeries{})
	_ = db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS ux_billing_refund_exec_external_ref ON billing_refund_executions (method, external_reference) WHERE method <> 'CASH' AND external_reference <> ''")
	_ = db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS ux_billing_refunds_refund_number ON billing_refunds (refund_number) WHERE refund_number IS NOT NULL AND refund_number <> ''")
	return billing.NewService(db).WithReceiptIssuer(func(tx *gorm.DB, payment *billing.Payment, invoice *billing.Invoice, paidBefore, balanceAfter int64, user uint) error {
		return nil
	})
}

func ibSeedPatient(t *testing.T, db *gorm.DB, tag string) uint {
	t.Helper()
	p := cashPatient{Nom: "IB", Prenoms: tag, CodePatient: fmt.Sprintf("IB-%s-%d", tag, time.Now().UnixNano())}
	if e := db.Create(&p).Error; e != nil {
		t.Fatal(e)
	}
	_ = db.Create(&medical_records.MedicalRecord{
		PatientID: p.ID, RecordNumber: fmt.Sprintf("MR-IB-%d", p.ID), Status: "active",
	}).Error
	return p.ID
}

func ibSeedCredit(t *testing.T, db *gorm.DB, patientID uint, amount int64, tag string, user uint) uint {
	t.Helper()
	party := billing.FinancialParty{
		Kind: billing.PartyKindIndividual, DisplayName: "Holder " + tag, PatientID: &patientID,
	}
	if e := db.Create(&party).Error; e != nil {
		t.Fatal(e)
	}
	src := uint(time.Now().UnixNano()%1_000_000_000) + uint(len(tag)*1000)
	entry := billing.CreditLedgerEntry{
		HolderPartyID: party.ID, PatientID: patientID,
		EntryType: billing.CreditEntryCredit, Amount: amount,
		SourceType: "TEST_SEED", SourceID: src,
		Reason: "IB seed " + tag, CreatedBy: user, CreatedAt: time.Now().UTC(),
		IdempotencyKey: "ib-seed-" + tag + "-" + fmt.Sprint(src),
	}
	if e := db.Create(&entry).Error; e != nil {
		t.Fatal(e)
	}
	return party.ID
}

func ibApproveCashRefund(t *testing.T, s *billing.Service, p, holder uint, amount int64, tag string, requester, approver uint) *billing.Refund {
	t.Helper()
	ref, e := s.RequestRefund(billing.RefundRequest{
		PatientID: p, HolderPartyID: holder, Amount: amount,
		ReasonCode: billing.RefundReasonDuplicateOrOverpayment, IntendedMethod: billing.RefundMethodCash,
		IdempotencyKey: "ib-req-" + tag,
	}, requester)
	if e != nil {
		t.Fatal(e)
	}
	ok, e := s.ApproveRefund(ref.ID, billing.RefundDecisionRequest{}, approver)
	if e != nil {
		t.Fatal(e)
	}
	return ok
}

func ibOpenSession(t *testing.T, db *gorm.DB, cs *Service, opener uint, float int64, tag string) uint {
	t.Helper()
	var n int64
	db.Model(&cashUser{}).Where("id=?", opener).Count(&n)
	if n == 0 {
		db.Create(&cashUser{ID: opener, Name: fmt.Sprintf("U%d", opener)})
	}
	reg, e := cs.SaveRegister(0, RegisterRequest{Code: "IBR-" + tag, Name: "IB " + tag}, opener)
	if e != nil {
		t.Fatal(e)
	}
	out, e := cs.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: float, Note: "ib", IdempotencyKey: "ib-open-" + tag}, opener)
	if e != nil {
		t.Fatal(e)
	}
	return out.Session.ID
}

func TestLOT29F_IB_RefundExecutionMatrix(t *testing.T) {
	db := cashDB(t)
	cs := NewService(db)
	s := ibBilling(t, db)
	for _, id := range []uint{801, 802, 803} {
		db.Create(&cashUser{ID: id, Name: fmt.Sprintf("U%d", id)})
	}

	t.Run("I_B01_to_I_B10_cash_core", func(t *testing.T) {
		p := ibSeedPatient(t, db, "01")
		holder := ibSeedCredit(t, db, p, 50000, "01", 801)
		ref := ibApproveCashRefund(t, s, p, holder, 20000, "01", 801, 802)
		sessID := ibOpenSession(t, db, cs, 803, 100000, "01")

		beforeSum, _ := s.GetCreditSummary(holder, p)
		beforeCash, _ := cs.Get(sessID)
		out, e := s.ExecuteRefund(ref.ID, billing.RefundExecuteRequest{
			Method: billing.RefundMethodCash, IdempotencyKey: "ib01-exec",
		}, 803)
		if e != nil || out.Refund.Status != billing.RefundStatusExecuted || out.Execution == nil || out.Execution.CashMovementID == nil {
			t.Fatalf("I-B01 %+v %v", out, e)
		}
		var ledN, movN int64
		db.Model(&billing.CreditLedgerEntry{}).Where("entry_type=? AND source_id=?", billing.CreditEntryRefund, ref.ID).Count(&ledN)
		db.Model(&CashMovement{}).Where("type=?", MovementRefundOut).Count(&movN)
		if ledN != 1 || movN != 1 {
			t.Fatalf("I-B02/03 led=%d mov=%d", ledN, movN)
		}
		afterSum, _ := s.GetCreditSummary(holder, p)
		if afterSum.ReservedForRefund != 0 || afterSum.SpendableCredit != beforeSum.SpendableCredit || afterSum.LedgerAvailable != 30000 {
			t.Fatalf("I-B05/06 %+v pre=%+v", afterSum, beforeSum)
		}
		afterCash, _ := cs.Get(sessID)
		if afterCash.ExpectedCash != beforeCash.ExpectedCash-20000 {
			t.Fatalf("I-B07 expected %d→%d", beforeCash.ExpectedCash, afterCash.ExpectedCash)
		}
		if afterCash.CashCollected != beforeCash.CashCollected {
			t.Fatal("I-B08 CashCollected")
		}
		if afterCash.CashMovementRefundOut != 20000 {
			t.Fatalf("refund out %d", afterCash.CashMovementRefundOut)
		}
	})

	t.Run("I_B11_status_gates_and_sod", func(t *testing.T) {
		p := ibSeedPatient(t, db, "11")
		holder := ibSeedCredit(t, db, p, 20000, "11", 801)
		ibOpenSession(t, db, cs, 803, 50000, "11")

		req, _ := s.RequestRefund(billing.RefundRequest{
			PatientID: p, HolderPartyID: holder, Amount: 1000,
			ReasonCode: billing.RefundReasonInvoiceCorrection, IntendedMethod: billing.RefundMethodCash,
			IdempotencyKey: "ib11-req",
		}, 801)
		if _, e := s.ExecuteRefund(req.ID, billing.RefundExecuteRequest{Method: billing.RefundMethodCash, IdempotencyKey: "ib11-a"}, 803); e == nil {
			t.Fatal("I-B11")
		}
		ok := ibApproveCashRefund(t, s, p, holder, 2000, "11b", 801, 802)
		if _, e := s.ExecuteRefund(ok.ID, billing.RefundExecuteRequest{Method: billing.RefundMethodCash, IdempotencyKey: "ib15"}, 802); e == nil {
			t.Fatal("I-B15 SoD")
		}
		if _, e := s.ExecuteRefund(ok.ID, billing.RefundExecuteRequest{Method: billing.RefundMethodCash, IdempotencyKey: "ib11-ok"}, 803); e != nil {
			t.Fatal(e)
		}
		if _, e := s.ExecuteRefund(ok.ID, billing.RefundExecuteRequest{Method: billing.RefundMethodCash, IdempotencyKey: "ib14"}, 803); e == nil {
			t.Fatal("I-B14")
		}
		if _, e := s.CancelRefund(ok.ID, billing.RefundDecisionRequest{Reason: "late"}, 802); e == nil {
			t.Fatal("I-B48")
		}
	})

	t.Run("I_B18_insufficient_cash", func(t *testing.T) {
		p := ibSeedPatient(t, db, "18")
		holder := ibSeedCredit(t, db, p, 20000, "18", 801)
		ibOpenSession(t, db, cs, 803, 5000, "18")
		ok := ibApproveCashRefund(t, s, p, holder, 10000, "18", 801, 802)
		if _, e := s.ExecuteRefund(ok.ID, billing.RefundExecuteRequest{Method: billing.RefundMethodCash, IdempotencyKey: "ib18"}, 803); e == nil {
			t.Fatal("I-B18")
		}
		got, _ := s.GetRefund(ok.ID)
		if got.Status != billing.RefundStatusApproved {
			t.Fatal(got.Status)
		}
	})

	t.Run("I_B23_external", func(t *testing.T) {
		p := ibSeedPatient(t, db, "23")
		holder := ibSeedCredit(t, db, p, 15000, "23", 801)
		ref, _ := s.RequestRefund(billing.RefundRequest{
			PatientID: p, HolderPartyID: holder, Amount: 5000,
			ReasonCode: billing.RefundReasonDuplicateOrOverpayment, IntendedMethod: billing.RefundMethodTransfer,
			IdempotencyKey: "ib23-req",
		}, 801)
		_, _ = s.ApproveRefund(ref.ID, billing.RefundDecisionRequest{}, 802)
		movBefore, _ := countMovements(db)
		if _, e := s.ExecuteRefund(ref.ID, billing.RefundExecuteRequest{Method: billing.RefundMethodTransfer, IdempotencyKey: "ib23-nr"}, 803); e == nil {
			t.Fatal("I-B24")
		}
		out, e := s.ExecuteRefund(ref.ID, billing.RefundExecuteRequest{
			Method: billing.RefundMethodTransfer, ExternalReference: "VIR-23",
			BeneficiaryRailRef: "ACC-23", IdempotencyKey: "ib23-ok",
		}, 803)
		if e != nil || out.Refund.Status != billing.RefundStatusExecuted || out.Execution.CashMovementID != nil {
			t.Fatalf("I-B23/27 %+v %v", out, e)
		}
		movAfter, _ := countMovements(db)
		if movAfter != movBefore {
			t.Fatal("I-B23 movement")
		}
	})

	t.Run("I_B30_idempotency_replay", func(t *testing.T) {
		p := ibSeedPatient(t, db, "30")
		holder := ibSeedCredit(t, db, p, 10000, "30", 801)
		ok := ibApproveCashRefund(t, s, p, holder, 2500, "30", 801, 802)
		ibOpenSession(t, db, cs, 803, 50000, "30")
		a, e := s.ExecuteRefund(ok.ID, billing.RefundExecuteRequest{Method: billing.RefundMethodCash, IdempotencyKey: "ib30-k"}, 803)
		if e != nil {
			t.Fatal(e)
		}
		b, e := s.ExecuteRefund(ok.ID, billing.RefundExecuteRequest{Method: billing.RefundMethodCash, IdempotencyKey: "ib30-k"}, 803)
		if e != nil || b.Execution.ID != a.Execution.ID {
			t.Fatalf("I-B30 %+v %v", b, e)
		}
	})

	t.Run("I_B35_rollback_injection", func(t *testing.T) {
		// Use transaction wrapper force-fail after successful path pieces via hooks.
		// billing package hooks are package-level; set via exported test-only setters.
		billing.SetRefundExecFailAfterLedger(func() error { return errors.New("forced-ledger") })
		defer billing.SetRefundExecFailAfterLedger(nil)

		p := ibSeedPatient(t, db, "35")
		holder := ibSeedCredit(t, db, p, 10000, "35", 801)
		ok := ibApproveCashRefund(t, s, p, holder, 1500, "35", 801, 802)
		ibOpenSession(t, db, cs, 803, 50000, "35")
		if _, e := s.ExecuteRefund(ok.ID, billing.RefundExecuteRequest{Method: billing.RefundMethodCash, IdempotencyKey: "ib35"}, 803); e == nil {
			t.Fatal("expected fail")
		}
		got, _ := s.GetRefund(ok.ID)
		if got.Status != billing.RefundStatusApproved {
			t.Fatal(got.Status)
		}
		var led, execN, mov int64
		db.Model(&billing.CreditLedgerEntry{}).Where("source_id=? AND source_type=?", ok.ID, billing.CreditSourceRefund).Count(&led)
		db.Model(&billing.RefundExecution{}).Where("refund_id=?", ok.ID).Count(&execN)
		db.Model(&CashMovement{}).Where("idempotency_key=?", fmt.Sprintf("cash-refund-out-%d", ok.ID)).Count(&mov)
		if led != 0 || execN != 0 || mov != 0 {
			t.Fatalf("orphan led=%d exec=%d mov=%d", led, execN, mov)
		}
	})

	t.Run("I_B40_statement_executed_only", func(t *testing.T) {
		p := ibSeedPatient(t, db, "40")
		holder := ibSeedCredit(t, db, p, 20000, "40", 801)
		ok := ibApproveCashRefund(t, s, p, holder, 7000, "40", 801, 802)
		ibOpenSession(t, db, cs, 803, 50000, "40")
		if _, e := s.ExecuteRefund(ok.ID, billing.RefundExecuteRequest{Method: billing.RefundMethodCash, IdempotencyKey: "ib40"}, 803); e != nil {
			t.Fatal(e)
		}
		sum, e := s.GetCreditSummary(holder, p)
		if e != nil || sum.TotalRefunded != 7000 || sum.ReservedForRefund != 0 || sum.LedgerAvailable != 13000 {
			t.Fatalf("I-B40 summary %+v %v", sum, e)
		}
		var execN int64
		db.Model(&billing.RefundExecution{}).Where("refund_id=?", ok.ID).Count(&execN)
		if execN != 1 {
			t.Fatal("I-B39 execution missing")
		}
		got, _ := s.GetRefundWithExecution(ok.ID, false)
		if got.Refund.Status != billing.RefundStatusExecuted || got.Execution == nil {
			t.Fatalf("I-B39 detail %+v", got)
		}
	})
}

func countMovements(db *gorm.DB) (int64, error) {
	var n int64
	e := db.Model(&CashMovement{}).Count(&n).Error
	return n, e
}

func TestLOT29F_IB_Concurrency(t *testing.T) {
	db := cashDB(t)
	cs := NewService(db)
	s := ibBilling(t, db)
	for _, id := range []uint{801, 802, 803} {
		db.Create(&cashUser{ID: id, Name: fmt.Sprintf("U%d", id)})
	}

	t.Run("I_BX01_double_execute", func(t *testing.T) {
		p := ibSeedPatient(t, db, "x01")
		holder := ibSeedCredit(t, db, p, 20000, "x01", 801)
		ok := ibApproveCashRefund(t, s, p, holder, 8000, "x01", 801, 802)
		ibOpenSession(t, db, cs, 803, 100000, "x01")
		var okN int32
		var wg sync.WaitGroup
		wg.Add(2)
		for i := 0; i < 2; i++ {
			i := i
			go func() {
				defer wg.Done()
				if _, e := s.ExecuteRefund(ok.ID, billing.RefundExecuteRequest{
					Method: billing.RefundMethodCash, IdempotencyKey: fmt.Sprintf("ibx01-%d", i),
				}, 803); e == nil {
					atomic.AddInt32(&okN, 1)
				}
			}()
		}
		wg.Wait()
		if okN != 1 {
			t.Fatalf("ok=%d", okN)
		}
		var led, execN, mov int64
		db.Model(&billing.CreditLedgerEntry{}).Where("source_type=? AND source_id=?", billing.CreditSourceRefund, ok.ID).Count(&led)
		db.Model(&billing.RefundExecution{}).Where("refund_id=?", ok.ID).Count(&execN)
		db.Model(&CashMovement{}).Where("type=? AND amount=?", MovementRefundOut, 8000).Count(&mov)
		if led != 1 || execN != 1 || mov != 1 {
			t.Fatalf("led=%d exec=%d mov=%d", led, execN, mov)
		}
	})

	t.Run("I_BX10_cancel_vs_execute", func(t *testing.T) {
		p := ibSeedPatient(t, db, "x10")
		holder := ibSeedCredit(t, db, p, 10000, "x10", 801)
		ok := ibApproveCashRefund(t, s, p, holder, 4000, "x10", 801, 802)
		ibOpenSession(t, db, cs, 803, 100000, "x10")
		var cOK, eOK int32
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, e := s.CancelRefund(ok.ID, billing.RefundDecisionRequest{Reason: "race"}, 802); e == nil {
				atomic.AddInt32(&cOK, 1)
			}
		}()
		go func() {
			defer wg.Done()
			if _, e := s.ExecuteRefund(ok.ID, billing.RefundExecuteRequest{Method: billing.RefundMethodCash, IdempotencyKey: "ibx10"}, 803); e == nil {
				atomic.AddInt32(&eOK, 1)
			}
		}()
		wg.Wait()
		if cOK+eOK != 1 {
			t.Fatalf("cancel=%d exec=%d", cOK, eOK)
		}
	})
}
