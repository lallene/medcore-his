package cash

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/billing"
)

func TestLOT29F_IC_RefundNumberingAndVoucher(t *testing.T) {
	db := cashDB(t)
	cs := NewService(db)
	s := ibBilling(t, db)
	for _, id := range []uint{901, 902, 903} {
		db.Create(&cashUser{ID: id, Name: fmt.Sprintf("ICU%d", id)})
	}
	year := time.Now().UTC().Year()

	t.Run("I_C21_to_I_C26_number_lifecycle", func(t *testing.T) {
		p := ibSeedPatient(t, db, "c21")
		holder := ibSeedCredit(t, db, p, 100000, "c21", 901)
		ibOpenSession(t, db, cs, 903, 200000, "c21")

		req, e := s.RequestRefund(billing.RefundRequest{
			PatientID: p, HolderPartyID: holder, Amount: 5000,
			ReasonCode: billing.RefundReasonUnusedAdvance, IntendedMethod: billing.RefundMethodCash,
			IdempotencyKey: "ic21-req",
		}, 901)
		if e != nil || req.RefundNumber != "" {
			t.Fatalf("I-C23 REQUESTED has number %+v %v", req, e)
		}
		appr, e := s.ApproveRefund(req.ID, billing.RefundDecisionRequest{}, 902)
		if e != nil || appr.RefundNumber != "" {
			t.Fatalf("I-C24 APPROVED has number %+v %v", appr, e)
		}
		out, e := s.ExecuteRefund(appr.ID, billing.RefundExecuteRequest{
			Method: billing.RefundMethodCash, IdempotencyKey: "ic21-exec",
		}, 903)
		if e != nil || out.Refund.RefundNumber != billing.FormatRefundNumber(year, 1) {
			t.Fatalf("I-C21 first number got %q want %s err=%v", out.Refund.RefundNumber, billing.FormatRefundNumber(year, 1), e)
		}
		v, e := s.GetRefundVoucher(out.Refund.ID, true)
		if e != nil || v.RefundNumber != out.Refund.RefundNumber || len(v.CopyLabels) != 2 {
			t.Fatalf("I-C16/17/36 voucher %+v %v", v, e)
		}
		if !strings.Contains(strings.Join(v.CopyLabels, "|"), "BÉNÉFICIAIRE") || !strings.Contains(strings.Join(v.SignatureZones, "|"), "bénéficiaire") {
			t.Fatalf("I-C48/49 copies/signatures %+v", v)
		}

		p2 := ibSeedPatient(t, db, "c22")
		h2 := ibSeedCredit(t, db, p2, 50000, "c22", 901)
		ibOpenSession(t, db, cs, 903, 100000, "c22b")
		ref2 := ibApproveCashRefund(t, s, p2, h2, 3000, "c22", 901, 902)
		out2, e := s.ExecuteRefund(ref2.ID, billing.RefundExecuteRequest{
			Method: billing.RefundMethodCash, IdempotencyKey: "ic22-exec",
		}, 903)
		if e != nil || out2.Refund.RefundNumber != billing.FormatRefundNumber(year, 2) {
			t.Fatalf("I-C22 second %q err=%v", out2.Refund.RefundNumber, e)
		}

		rejReq, _ := s.RequestRefund(billing.RefundRequest{
			PatientID: p2, HolderPartyID: h2, Amount: 1000,
			ReasonCode: billing.RefundReasonInvoiceCorrection, IntendedMethod: billing.RefundMethodTransfer,
			IdempotencyKey: "ic25-req",
		}, 901)
		rej, _ := s.RejectRefund(rejReq.ID, billing.RefundDecisionRequest{Reason: "dossier incomplet"}, 902)
		if rej.RefundNumber != "" {
			t.Fatal("I-C25 REJECTED number")
		}
		canReq, _ := s.RequestRefund(billing.RefundRequest{
			PatientID: p2, HolderPartyID: h2, Amount: 1000,
			ReasonCode: billing.RefundReasonInvoiceCorrection, IntendedMethod: billing.RefundMethodTransfer,
			IdempotencyKey: "ic26-req",
		}, 901)
		can, _ := s.CancelRefund(canReq.ID, billing.RefundDecisionRequest{Reason: "annulation volontaire"}, 901)
		if can.RefundNumber != "" {
			t.Fatal("I-C26 CANCELLED number")
		}
	})

	t.Run("I_C27_failed_exec_consumes_no_number", func(t *testing.T) {
		p := ibSeedPatient(t, db, "c27")
		holder := ibSeedCredit(t, db, p, 40000, "c27", 901)
		ibOpenSession(t, db, cs, 903, 80000, "c27")
		ref := ibApproveCashRefund(t, s, p, holder, 4000, "c27", 901, 902)

		var seriesBefore billing.DocumentNumberSeries
		_ = db.Where("document_type=? AND year=?", billing.DocumentTypeRefund, year).First(&seriesBefore)
		before := seriesBefore.LastNumber

		billing.SetRefundExecFailAfterNumberAllocate(func() error { return errors.New("inject after number") })
		defer billing.SetRefundExecFailAfterNumberAllocate(nil)
		_, e := s.ExecuteRefund(ref.ID, billing.RefundExecuteRequest{
			Method: billing.RefundMethodCash, IdempotencyKey: "ic27-fail",
		}, 903)
		if e == nil {
			t.Fatal("I-C27 expected failure")
		}
		fresh, _ := s.GetRefund(ref.ID)
		if fresh.Status != billing.RefundStatusApproved || fresh.RefundNumber != "" {
			t.Fatalf("I-C27 state %+v", fresh)
		}
		var seriesAfter billing.DocumentNumberSeries
		_ = db.Where("document_type=? AND year=?", billing.DocumentTypeRefund, year).First(&seriesAfter)
		if seriesAfter.LastNumber != before {
			t.Fatalf("I-C27 counter consumed %d→%d", before, seriesAfter.LastNumber)
		}
		// Successful retry allocates next committed number.
		billing.SetRefundExecFailAfterNumberAllocate(nil)
		ok, e := s.ExecuteRefund(ref.ID, billing.RefundExecuteRequest{
			Method: billing.RefundMethodCash, IdempotencyKey: "ic27-ok",
		}, 903)
		if e != nil || ok.Refund.RefundNumber == "" {
			t.Fatalf("I-C27 retry %v %+v", e, ok)
		}
	})

	t.Run("I_C28_I_C29_idempotency_and_double_exec", func(t *testing.T) {
		p := ibSeedPatient(t, db, "c28")
		holder := ibSeedCredit(t, db, p, 30000, "c28", 901)
		ibOpenSession(t, db, cs, 903, 60000, "c28")
		ref := ibApproveCashRefund(t, s, p, holder, 2500, "c28", 901, 902)
		a, e := s.ExecuteRefund(ref.ID, billing.RefundExecuteRequest{
			Method: billing.RefundMethodCash, IdempotencyKey: "ic28-key",
		}, 903)
		if e != nil {
			t.Fatal(e)
		}
		b, e := s.ExecuteRefund(ref.ID, billing.RefundExecuteRequest{
			Method: billing.RefundMethodCash, IdempotencyKey: "ic28-key",
		}, 903)
		if e != nil || b.Refund.RefundNumber != a.Refund.RefundNumber {
			t.Fatalf("I-C28 replay %q vs %q %v", a.Refund.RefundNumber, b.Refund.RefundNumber, e)
		}
		if _, e := s.ExecuteRefund(ref.ID, billing.RefundExecuteRequest{
			Method: billing.RefundMethodCash, IdempotencyKey: "ic29-other",
		}, 903); e == nil {
			t.Fatal("I-C29 double execute")
		}
		v1, _ := s.GetRefundVoucher(ref.ID, false)
		v2, _ := s.GetRefundVoucher(ref.ID, false)
		if v1.RefundNumber != v2.RefundNumber || v1.Amount != v2.Amount {
			t.Fatal("I-C33/34 voucher reprint mutates")
		}
	})

	t.Run("I_C01_to_I_C13_reporting", func(t *testing.T) {
		p := ibSeedPatient(t, db, "cr")
		holder := ibSeedCredit(t, db, p, 80000, "cr", 901)
		ibOpenSession(t, db, cs, 903, 150000, "cr")

		pending, _ := s.RequestRefund(billing.RefundRequest{
			PatientID: p, HolderPartyID: holder, Amount: 1000,
			ReasonCode: billing.RefundReasonUnusedAdvance, IntendedMethod: billing.RefundMethodCash,
			IdempotencyKey: "ic-rep-pend",
		}, 901)
		_, _ = s.ApproveRefund(pending.ID, billing.RefundDecisionRequest{}, 902)

		cashRef := ibApproveCashRefund(t, s, p, holder, 7000, "cr-cash", 901, 902)
		cashOut, e := s.ExecuteRefund(cashRef.ID, billing.RefundExecuteRequest{
			Method: billing.RefundMethodCash, IdempotencyKey: "ic-rep-cash",
		}, 903)
		if e != nil {
			t.Fatal(e)
		}

		mobReq, _ := s.RequestRefund(billing.RefundRequest{
			PatientID: p, HolderPartyID: holder, Amount: 5000,
			ReasonCode: billing.RefundReasonUnusedAdvance, IntendedMethod: billing.RefundMethodMobileMoney,
			IdempotencyKey: "ic-rep-mob-req",
		}, 901)
		mobAppr, _ := s.ApproveRefund(mobReq.ID, billing.RefundDecisionRequest{}, 902)
		mobOut, e := s.ExecuteRefund(mobAppr.ID, billing.RefundExecuteRequest{
			Method: billing.RefundMethodMobileMoney, IdempotencyKey: "ic-rep-mob",
			ExternalReference: "MM-IC-1", BeneficiaryRailRef: "0700123456",
		}, 903)
		if e != nil {
			t.Fatal(e)
		}

		today := time.Now().UTC().Format("2006-01-02")
		rep, e := s.GetRefundReport(billing.RefundReportFilter{DateFrom: today, DateTo: today, Limit: 50})
		if e != nil {
			t.Fatal(e)
		}
		if rep.Summary.ExecutedRefundAmount < 12000 {
			t.Fatalf("I-C05/06/09 amount %+v", rep.Summary)
		}
		if rep.Summary.CashRefundAmount < 7000 || rep.Summary.ExternalRefundAmount < 5000 {
			t.Fatalf("I-C07/08 cash/external split %+v", rep.Summary)
		}
		if rep.Summary.CashRefundAmount+rep.Summary.ExternalRefundAmount != rep.Summary.ExecutedRefundAmount {
			t.Fatalf("I-C28 consistency %+v", rep.Summary)
		}

		cashOnly, _ := s.GetRefundReport(billing.RefundReportFilter{
			DateFrom: today, DateTo: today, Method: billing.RefundMethodCash, Limit: 50,
		})
		for _, row := range cashOnly.Data {
			if row.Method != billing.RefundMethodCash {
				t.Fatal("I-C08 method filter")
			}
		}

		found, e := s.ListRefunds(billing.RefundListFilter{RefundNumber: cashOut.Refund.RefundNumber})
		if e != nil || found.Total != 1 || found.Data[0].ID != cashOut.Refund.ID {
			t.Fatalf("I-C13 RMB search %+v %v", found, e)
		}

		sess, _ := cs.Get(*cashOut.Execution.CashSessionID)
		if sess.CashMovementRefundOut < 7000 || sess.CashRefundCount < 1 {
			t.Fatalf("I-C18 cash session refund %+v", sess)
		}
		_ = mobOut
	})

	t.Run("I_C16_voucher_non_executed", func(t *testing.T) {
		p := ibSeedPatient(t, db, "cv")
		holder := ibSeedCredit(t, db, p, 10000, "cv", 901)
		req, _ := s.RequestRefund(billing.RefundRequest{
			PatientID: p, HolderPartyID: holder, Amount: 2000,
			ReasonCode: billing.RefundReasonUnusedAdvance, IntendedMethod: billing.RefundMethodTransfer,
			IdempotencyKey: "ic-voucher-req",
		}, 901)
		if _, e := s.GetRefundVoucher(req.ID, false); e == nil {
			t.Fatal("I-C16 voucher on REQUESTED")
		}
	})
}

func TestLOT29F_IC_NumberingConcurrency(t *testing.T) {
	db := cashDB(t)
	cs := NewService(db)
	s := ibBilling(t, db)
	for _, id := range []uint{911, 912, 913} {
		db.Create(&cashUser{ID: id, Name: fmt.Sprintf("ICX%d", id)})
	}
	year := time.Now().UTC().Year()

	t.Run("I_CX01_I_CX02_concurrent_distinct", func(t *testing.T) {
		const n = 10
		ids := make([]uint, n)
		for i := 0; i < n; i++ {
			p := ibSeedPatient(t, db, fmt.Sprintf("x%d", i))
			h := ibSeedCredit(t, db, p, 20000, fmt.Sprintf("x%d", i), 911)
			ibOpenSession(t, db, cs, 913, 50000, fmt.Sprintf("xopen%d", i))
			ref := ibApproveCashRefund(t, s, p, h, 1000, fmt.Sprintf("xref%d", i), 911, 912)
			ids[i] = ref.ID
		}
		var wg sync.WaitGroup
		nums := make([]string, n)
		var failCount int32
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				out, e := s.ExecuteRefund(ids[i], billing.RefundExecuteRequest{
					Method: billing.RefundMethodCash, IdempotencyKey: fmt.Sprintf("icx-%d", i),
				}, 913)
				if e != nil {
					atomic.AddInt32(&failCount, 1)
					return
				}
				nums[i] = out.Refund.RefundNumber
			}(i)
		}
		wg.Wait()
		if failCount != 0 {
			t.Fatalf("I-CX02 failures %d", failCount)
		}
		seen := map[string]bool{}
		for _, num := range nums {
			if num == "" || seen[num] {
				t.Fatalf("I-CX01/02 duplicate/empty %v", nums)
			}
			seen[num] = true
			if !strings.HasPrefix(num, fmt.Sprintf("RMB-%d-", year)) {
				t.Fatalf("year prefix %s", num)
			}
		}
		if len(seen) != n {
			t.Fatalf("unique count %d", len(seen))
		}
	})

	t.Run("I_CX06_I_CX07_same_refund", func(t *testing.T) {
		p := ibSeedPatient(t, db, "xsame")
		h := ibSeedCredit(t, db, p, 25000, "xsame", 911)
		ibOpenSession(t, db, cs, 913, 50000, "xsame")
		ref := ibApproveCashRefund(t, s, p, h, 1500, "xsame", 911, 912)
		var wg sync.WaitGroup
		results := make([]*billing.RefundWithExecution, 2)
		errs := make([]error, 2)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				results[i], errs[i] = s.ExecuteRefund(ref.ID, billing.RefundExecuteRequest{
					Method: billing.RefundMethodCash, IdempotencyKey: "icx-same-key",
				}, 913)
			}(i)
		}
		wg.Wait()
		okN := 0
		var num string
		for i := 0; i < 2; i++ {
			if errs[i] == nil && results[i] != nil && results[i].Refund.RefundNumber != "" {
				okN++
				if num == "" {
					num = results[i].Refund.RefundNumber
				} else if num != results[i].Refund.RefundNumber {
					t.Fatal("I-CX07 different numbers")
				}
			}
		}
		if okN < 1 || num == "" {
			t.Fatalf("I-CX06/07 %#v %#v", errs, results)
		}
		var execN int64
		db.Model(&billing.RefundExecution{}).Where("refund_id=?", ref.ID).Count(&execN)
		if execN != 1 {
			t.Fatalf("executions %d", execN)
		}
	})
}
