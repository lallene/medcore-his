package billing

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/core/rbac"
)

func TestLOT29F_IA_RefundWorkflowMatrix(t *testing.T) {
	db := billingDB(t)
	s := receiptBilling(t, db)
	iaPatient := func(t *testing.T) uint {
		t.Helper()
		id, _ := seedPatient(t, db, fmt.Sprintf("IA-%d", time.Now().UnixNano()))
		return id
	}

	t.Run("I_A01_request_reserves_spendable", func(t *testing.T) {
		p := iaPatient(t)
		holder := earnCredit(t, s, db, p, 20000, "ia01", 701)
		ref, e := s.RequestRefund(RefundRequest{
			PatientID: p, HolderPartyID: holder, Amount: 15000,
			ReasonCode: RefundReasonDuplicateOrOverpayment, IdempotencyKey: "ia01-req",
		}, 701)
		if e != nil || ref.Status != RefundStatusRequested {
			t.Fatalf("I-A01 %+v %v", ref, e)
		}
		sum, e := s.GetCreditSummary(holder, p)
		if e != nil || sum.LedgerAvailable != 20000 || sum.ReservedForRefund != 15000 || sum.SpendableCredit != 5000 {
			t.Fatalf("I-A01 summary %+v %v", sum, e)
		}
		var ledN int64
		db.Model(&CreditLedgerEntry{}).Where("entry_type=?", CreditEntryRefund).Count(&ledN)
		if ledN != 0 {
			t.Fatal("I-A01 must not write REFUND ledger debit")
		}
	})

	t.Run("I_A02_partial_and_multi_request_cap", func(t *testing.T) {
		p := iaPatient(t)
		holder := earnCredit(t, s, db, p, 50000, "ia02", 702)
		if _, e := s.RequestRefund(RefundRequest{
			PatientID: p, HolderPartyID: holder, Amount: 20000,
			ReasonCode: RefundReasonInvoiceCorrection, IdempotencyKey: "ia02-a",
		}, 702); e != nil {
			t.Fatal(e)
		}
		if _, e := s.RequestRefund(RefundRequest{
			PatientID: p, HolderPartyID: holder, Amount: 20000,
			ReasonCode: RefundReasonInvoiceCorrection, IdempotencyKey: "ia02-b",
		}, 702); e != nil {
			t.Fatal(e)
		}
		if _, e := s.RequestRefund(RefundRequest{
			PatientID: p, HolderPartyID: holder, Amount: 20000,
			ReasonCode: RefundReasonInvoiceCorrection, IdempotencyKey: "ia02-c",
		}, 702); e == nil {
			t.Fatal("I-A02 third request must fail")
		}
		sum, _ := s.GetCreditSummary(holder, p)
		if sum.ReservedForRefund != 40000 || sum.SpendableCredit != 10000 {
			t.Fatalf("I-A02 %+v", sum)
		}
	})

	t.Run("I_A03_apply_uses_spendable_not_ledger", func(t *testing.T) {
		p := iaPatient(t)
		holder := earnCredit(t, s, db, p, 50000, "ia03", 703)
		if _, e := s.RequestRefund(RefundRequest{
			PatientID: p, HolderPartyID: holder, Amount: 30000,
			ReasonCode: RefundReasonDuplicateOrOverpayment, IdempotencyKey: "ia03-req",
		}, 703); e != nil {
			t.Fatal(e)
		}
		target := issuedPayReady(t, s, p, consultation(t, db, p, "IA03t"), tariff(t, db, "CONSULTATION", "IA03t", 50000))
		if _, e := s.ApplyCredit(target.ID, CreditApplicationRequest{HolderPartyID: holder, Amount: 30000, IdempotencyKey: "ia03-app"}, 703); e == nil {
			t.Fatal("I-A03 apply 30k must fail when spendable=20k")
		}
		res, e := s.ApplyCredit(target.ID, CreditApplicationRequest{HolderPartyID: holder, Amount: 20000, IdempotencyKey: "ia03-app2"}, 703)
		if e != nil || res.AmountApplied != 20000 {
			t.Fatalf("I-A03 apply 20k %+v %v", res, e)
		}
		sum, _ := s.GetCreditSummary(holder, p)
		if sum.SpendableCredit != 0 || sum.ReservedForRefund != 30000 {
			t.Fatalf("I-A03 %+v", sum)
		}
	})

	t.Run("I_A04_approve_sod_and_no_ledger", func(t *testing.T) {
		p := iaPatient(t)
		holder := earnCredit(t, s, db, p, 10000, "ia04", 704)
		ref, e := s.RequestRefund(RefundRequest{
			PatientID: p, HolderPartyID: holder, Amount: 10000,
			ReasonCode: RefundReasonDuplicateOrOverpayment, IdempotencyKey: "ia04-req",
		}, 704)
		if e != nil {
			t.Fatal(e)
		}
		if _, e := s.ApproveRefund(ref.ID, RefundDecisionRequest{}, 704); e == nil {
			t.Fatal("I-A04 self-approve must fail")
		}
		ok, e := s.ApproveRefund(ref.ID, RefundDecisionRequest{}, 804)
		if e != nil || ok.Status != RefundStatusApproved {
			t.Fatalf("I-A04 %+v %v", ok, e)
		}
		sum, _ := s.GetCreditSummary(holder, p)
		if sum.ReservedForRefund != 10000 || sum.SpendableCredit != 0 {
			t.Fatalf("I-A04 reserved after approve %+v", sum)
		}
		var ledN int64
		db.Model(&CreditLedgerEntry{}).Where("entry_type=?", CreditEntryRefund).Count(&ledN)
		if ledN != 0 {
			t.Fatal("I-A04 approve must not debit REFUND ledger")
		}
	})

	t.Run("I_A05_reject_releases_reservation", func(t *testing.T) {
		p := iaPatient(t)
		holder := earnCredit(t, s, db, p, 12000, "ia05", 705)
		ref, e := s.RequestRefund(RefundRequest{
			PatientID: p, HolderPartyID: holder, Amount: 12000,
			ReasonCode: RefundReasonInvoiceCorrection, IdempotencyKey: "ia05-req",
		}, 705)
		if e != nil {
			t.Fatal(e)
		}
		out, e := s.RejectRefund(ref.ID, RefundDecisionRequest{Reason: "Dossier incomplet"}, 805)
		if e != nil || out.Status != RefundStatusRejected {
			t.Fatalf("I-A05 %+v %v", out, e)
		}
		sum, _ := s.GetCreditSummary(holder, p)
		if sum.ReservedForRefund != 0 || sum.SpendableCredit != 12000 {
			t.Fatalf("I-A05 %+v", sum)
		}
	})

	t.Run("I_A06_cancel_requested_and_approved", func(t *testing.T) {
		p := iaPatient(t)
		holder := earnCredit(t, s, db, p, 20000, "ia06", 706)
		r1, e := s.RequestRefund(RefundRequest{
			PatientID: p, HolderPartyID: holder, Amount: 5000,
			ReasonCode: RefundReasonDuplicateOrOverpayment, IdempotencyKey: "ia06-a",
		}, 706)
		if e != nil {
			t.Fatal(e)
		}
		if _, e := s.CancelRefund(r1.ID, RefundDecisionRequest{Reason: "Erreur de saisie"}, 706); e != nil {
			t.Fatal(e)
		}
		r2, e := s.RequestRefund(RefundRequest{
			PatientID: p, HolderPartyID: holder, Amount: 8000,
			ReasonCode: RefundReasonDuplicateOrOverpayment, IdempotencyKey: "ia06-b",
		}, 706)
		if e != nil {
			t.Fatal(e)
		}
		if _, e := s.ApproveRefund(r2.ID, RefundDecisionRequest{}, 806); e != nil {
			t.Fatal(e)
		}
		if _, e := s.CancelRefund(r2.ID, RefundDecisionRequest{Reason: "Patient a changé d'avis"}, 806); e != nil {
			t.Fatal(e)
		}
		sum, _ := s.GetCreditSummary(holder, p)
		if sum.ReservedForRefund != 0 || sum.SpendableCredit != 20000 {
			t.Fatalf("I-A06 %+v", sum)
		}
	})

	t.Run("I_A07_other_requires_comment_and_managerial", func(t *testing.T) {
		p := iaPatient(t)
		holder := earnCredit(t, s, db, p, 5000, "ia07", 707)
		if _, e := s.RequestRefund(RefundRequest{
			PatientID: p, HolderPartyID: holder, Amount: 1000,
			ReasonCode: RefundReasonOther, IdempotencyKey: "ia07-bad",
		}, 707); e == nil {
			t.Fatal("I-A07 OTHER without comment")
		}
		ref, e := s.RequestRefund(RefundRequest{
			PatientID: p, HolderPartyID: holder, Amount: 1000,
			ReasonCode: RefundReasonOther, ReasonComment: "Cas exceptionnel documenté",
			IdempotencyKey: "ia07-ok",
		}, 707)
		if e != nil {
			t.Fatal(e)
		}
		if _, e := s.ApproveRefund(ref.ID, RefundDecisionRequest{}, 807); e == nil {
			t.Fatal("I-A07 OTHER without managerialApproval")
		}
		if _, e := s.ApproveRefund(ref.ID, RefundDecisionRequest{ManagerialApproval: true}, 807); e != nil {
			t.Fatal(e)
		}
	})

	t.Run("I_A08_service_cancelled_requires_attestation", func(t *testing.T) {
		p := iaPatient(t)
		holder := earnCredit(t, s, db, p, 5000, "ia08", 708)
		if _, e := s.RequestRefund(RefundRequest{
			PatientID: p, HolderPartyID: holder, Amount: 1000,
			ReasonCode: RefundReasonServiceCancelledOrNotPerf, IdempotencyKey: "ia08-bad",
		}, 708); e == nil {
			t.Fatal("I-A08 attestation required")
		}
		if _, e := s.RequestRefund(RefundRequest{
			PatientID: p, HolderPartyID: holder, Amount: 1000,
			ReasonCode: RefundReasonServiceCancelledOrNotPerf,
			ClinicalAttestationRef: "ATT-CLIN-IA08", IdempotencyKey: "ia08-ok",
		}, 708); e != nil {
			t.Fatal(e)
		}
	})

	t.Run("I_A09_alternate_beneficiary_needs_consent", func(t *testing.T) {
		p := iaPatient(t)
		holder := earnCredit(t, s, db, p, 5000, "ia09", 709)
		if _, e := s.RequestRefund(RefundRequest{
			PatientID: p, HolderPartyID: holder, Amount: 1000,
			ReasonCode: RefundReasonDuplicateOrOverpayment,
			BeneficiaryMode: RefundBeneficiaryAlternate, BeneficiaryDisplayName: "Enfant",
			IdempotencyKey: "ia09-bad",
		}, 709); e == nil {
			t.Fatal("I-A09 consent required")
		}
		ref, e := s.RequestRefund(RefundRequest{
			PatientID: p, HolderPartyID: holder, Amount: 1000,
			ReasonCode: RefundReasonDuplicateOrOverpayment,
			BeneficiaryMode: RefundBeneficiaryAlternate, BeneficiaryDisplayName: "Enfant",
			BeneficiaryRelationship: "Enfant", HolderConsentRef: "CONSENT-IA09",
			IdempotencyKey: "ia09-ok",
		}, 709)
		if e != nil || ref.BeneficiaryMode != RefundBeneficiaryAlternate {
			t.Fatalf("I-A09 %+v %v", ref, e)
		}
	})

	t.Run("I_A10_zero_cash_effect", func(t *testing.T) {
		p := iaPatient(t)
		holder := earnCredit(t, s, db, p, 8000, "ia10", 710)
		var mov0 int64
		db.Table("cash_movements").Count(&mov0)
		ref, e := s.RequestRefund(RefundRequest{
			PatientID: p, HolderPartyID: holder, Amount: 3000,
			ReasonCode: RefundReasonDuplicateOrOverpayment, IdempotencyKey: "ia10-req",
		}, 710)
		if e != nil {
			t.Fatal(e)
		}
		if _, e := s.ApproveRefund(ref.ID, RefundDecisionRequest{}, 810); e != nil {
			t.Fatal(e)
		}
		var mov1 int64
		db.Table("cash_movements").Count(&mov1)
		if mov1 != mov0 {
			t.Fatal("I-A10 cash movement write")
		}
	})

	t.Run("I_A11_statement_shows_reservation", func(t *testing.T) {
		p := iaPatient(t)
		holder := earnCredit(t, s, db, p, 25000, "ia11", 711)
		if _, e := s.RequestRefund(RefundRequest{
			PatientID: p, HolderPartyID: holder, Amount: 10000,
			ReasonCode: RefundReasonDuplicateOrOverpayment, IdempotencyKey: "ia11-req",
		}, 711); e != nil {
			t.Fatal(e)
		}
		st, e := s.GetFinancialStatement(p, true)
		if e != nil || st.Summary.ReservedForRefund != 10000 || st.Summary.SpendableCredit != 15000 {
			t.Fatalf("I-A11 %+v %v", st.Summary, e)
		}
		hist, e := s.ListFinancialHistory(FinancialHistoryFilter{PatientID: p, EventType: FinEventRefundRequested, Limit: 10})
		if e != nil || hist.Total < 1 {
			t.Fatalf("I-A11 history %v", e)
		}
	})

	t.Run("I_A12_rbac_matrix", func(t *testing.T) {
		if hasPerm(rbac.EffectiveStaffPermissions("staff", []string{"CAISSIER"}, nil), "billing.refund.approve") {
			t.Fatal("I-A12 caissier approve")
		}
		if !hasPerm(rbac.EffectiveStaffPermissions("staff", []string{"CAISSIER"}, nil), "billing.refund.request") {
			t.Fatal("I-A12 caissier request")
		}
		if hasPerm(rbac.EffectiveStaffPermissions("staff", []string{"FACTURATION"}, nil), "billing.refund.approve") {
			t.Fatal("I-A12 facturation approve")
		}
		if !hasPerm(rbac.EffectiveStaffPermissions("staff", []string{"COMPTABLE"}, nil), "billing.refund.approve") {
			t.Fatal("I-A12 comptable approve")
		}
	})

	t.Run("I_A13_idempotent_request", func(t *testing.T) {
		p := iaPatient(t)
		holder := earnCredit(t, s, db, p, 5000, "ia13", 713)
		a, e := s.RequestRefund(RefundRequest{
			PatientID: p, HolderPartyID: holder, Amount: 2000,
			ReasonCode: RefundReasonDuplicateOrOverpayment, IdempotencyKey: "ia13-same",
		}, 713)
		if e != nil {
			t.Fatal(e)
		}
		b, e := s.RequestRefund(RefundRequest{
			PatientID: p, HolderPartyID: holder, Amount: 2000,
			ReasonCode: RefundReasonDuplicateOrOverpayment, IdempotencyKey: "ia13-same",
		}, 713)
		if e != nil || a.ID != b.ID {
			t.Fatalf("I-A13 %+v %+v %v", a, b, e)
		}
		sum, _ := s.GetCreditSummary(holder, p)
		if sum.ReservedForRefund != 2000 {
			t.Fatalf("I-A13 reserved %+v", sum)
		}
	})
}

func TestLOT29F_IA_Concurrency(t *testing.T) {
	db := billingDB(t)
	s := receiptBilling(t, db)

	t.Run("I_AX01_two_full_requests_one_wins", func(t *testing.T) {
		p, _ := seedPatient(t, db, fmt.Sprintf("IAX01-%d", time.Now().UnixNano()))
		holder := earnCredit(t, s, db, p, 10000, "iax01", 901)
		var okN int32
		var wg sync.WaitGroup
		wg.Add(2)
		for i := 0; i < 2; i++ {
			i := i
			go func() {
				defer wg.Done()
				_, e := s.RequestRefund(RefundRequest{
					PatientID: p, HolderPartyID: holder, Amount: 10000,
					ReasonCode: RefundReasonDuplicateOrOverpayment,
					IdempotencyKey: fmt.Sprintf("iax01-%d", i),
				}, 901)
				if e == nil {
					atomic.AddInt32(&okN, 1)
				}
			}()
		}
		wg.Wait()
		if okN != 1 {
			t.Fatalf("I-AX01 ok=%d", okN)
		}
	})

	t.Run("I_AX02_two_half_requests_both", func(t *testing.T) {
		p, _ := seedPatient(t, db, fmt.Sprintf("IAX02-%d", time.Now().UnixNano()))
		holder := earnCredit(t, s, db, p, 20000, "iax02", 902)
		var okN int32
		var wg sync.WaitGroup
		wg.Add(2)
		for i := 0; i < 2; i++ {
			i := i
			go func() {
				defer wg.Done()
				_, e := s.RequestRefund(RefundRequest{
					PatientID: p, HolderPartyID: holder, Amount: 10000,
					ReasonCode: RefundReasonDuplicateOrOverpayment,
					IdempotencyKey: fmt.Sprintf("iax02-%d", i),
				}, 902)
				if e == nil {
					atomic.AddInt32(&okN, 1)
				}
			}()
		}
		wg.Wait()
		if okN != 2 {
			t.Fatalf("I-AX02 ok=%d", okN)
		}
		sum, _ := s.GetCreditSummary(holder, p)
		if sum.ReservedForRefund != 20000 || sum.SpendableCredit != 0 {
			t.Fatalf("I-AX02 %+v", sum)
		}
	})

	t.Run("I_AX03_refund_vs_apply", func(t *testing.T) {
		p, _ := seedPatient(t, db, fmt.Sprintf("IAX03-%d", time.Now().UnixNano()))
		holder := earnCredit(t, s, db, p, 15000, "iax03", 903)
		target := issuedPayReady(t, s, p, consultation(t, db, p, "IAX03t"), tariff(t, db, "CONSULTATION", "IAX03t", 15000))
		var refundOK, applyOK int32
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, e := s.RequestRefund(RefundRequest{
				PatientID: p, HolderPartyID: holder, Amount: 10000,
				ReasonCode: RefundReasonDuplicateOrOverpayment, IdempotencyKey: "iax03-r",
			}, 903); e == nil {
				atomic.AddInt32(&refundOK, 1)
			}
		}()
		go func() {
			defer wg.Done()
			if _, e := s.ApplyCredit(target.ID, CreditApplicationRequest{
				HolderPartyID: holder, Amount: 10000, IdempotencyKey: "iax03-a",
			}, 903); e == nil {
				atomic.AddInt32(&applyOK, 1)
			}
		}()
		wg.Wait()
		if refundOK+applyOK != 1 {
			t.Fatalf("I-AX03 expected exactly one consumer refund=%d apply=%d", refundOK, applyOK)
		}
		sum, _ := s.GetCreditSummary(holder, p)
		if sum.SpendableCredit < 0 || sum.TotalApplied+sum.ReservedForRefund > 15000 {
			t.Fatalf("I-AX03 impossible %+v", sum)
		}
	})

	t.Run("I_AX04_approve_vs_cancel", func(t *testing.T) {
		p, _ := seedPatient(t, db, fmt.Sprintf("IAX04-%d", time.Now().UnixNano()))
		holder := earnCredit(t, s, db, p, 10000, "iax04", 904)
		ref, e := s.RequestRefund(RefundRequest{
			PatientID: p, HolderPartyID: holder, Amount: 10000,
			ReasonCode: RefundReasonDuplicateOrOverpayment, IdempotencyKey: "iax04-req",
		}, 904)
		if e != nil {
			t.Fatal(e)
		}
		var approveOK, cancelOK int32
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, e := s.ApproveRefund(ref.ID, RefundDecisionRequest{}, 914); e == nil {
				atomic.AddInt32(&approveOK, 1)
			}
		}()
		go func() {
			defer wg.Done()
			if _, e := s.CancelRefund(ref.ID, RefundDecisionRequest{Reason: "Annulation concurrente"}, 904); e == nil {
				atomic.AddInt32(&cancelOK, 1)
			}
		}()
		wg.Wait()
		if approveOK+cancelOK != 1 {
			t.Fatalf("I-AX04 approve=%d cancel=%d", approveOK, cancelOK)
		}
		got, _ := s.GetRefund(ref.ID)
		if got.Status != RefundStatusApproved && got.Status != RefundStatusCancelled {
			t.Fatalf("I-AX04 status %s", got.Status)
		}
	})

	t.Run("I_AX05_approve_vs_reject", func(t *testing.T) {
		p, _ := seedPatient(t, db, fmt.Sprintf("IAX05-%d", time.Now().UnixNano()))
		holder := earnCredit(t, s, db, p, 10000, "iax05", 905)
		ref, e := s.RequestRefund(RefundRequest{
			PatientID: p, HolderPartyID: holder, Amount: 10000,
			ReasonCode: RefundReasonDuplicateOrOverpayment, IdempotencyKey: "iax05-req",
		}, 905)
		if e != nil {
			t.Fatal(e)
		}
		var aOK, rOK int32
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, e := s.ApproveRefund(ref.ID, RefundDecisionRequest{}, 915); e == nil {
				atomic.AddInt32(&aOK, 1)
			}
		}()
		go func() {
			defer wg.Done()
			if _, e := s.RejectRefund(ref.ID, RefundDecisionRequest{Reason: "Rejet concurrent"}, 925); e == nil {
				atomic.AddInt32(&rOK, 1)
			}
		}()
		wg.Wait()
		if aOK+rOK != 1 {
			t.Fatalf("I-AX05 approve=%d reject=%d", aOK, rOK)
		}
	})
}
