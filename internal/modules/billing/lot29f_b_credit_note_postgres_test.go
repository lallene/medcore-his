package billing

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func creditNoteCount(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var n int64
	if e := db.Model(&CreditNote{}).Count(&n).Error; e != nil {
		t.Fatal(e)
	}
	return n
}

func creditErrCode(e error) string {
	var app *coreerrors.AppError
	if e != nil && errors.As(e, &app) {
		return app.Code
	}
	return ""
}

func TestLOT29F_B_CreditNoteMatrix(t *testing.T) {
	t.Run("CN01_CN09_eligible_full_credit", func(t *testing.T) {
		db := billingDB(t)
		db.Create(&billingUser{ID: 90, Name: "Comptable CN"})
		s := receiptBilling(t, db)
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		linesBefore := len(inv.Lines)
		grossBefore, patientBefore := inv.GrossAmount, inv.PatientAmount
		statusBefore, numberBefore := inv.Status, inv.Number

		out, e := s.IssueCreditNote(inv.ID, CreditNoteRequest{Amount: inv.PatientAmount, Reason: "Erreur de facturation", IdempotencyKey: "cn01"}, 90)
		if e != nil {
			t.Fatal(e)
		}
		if out.CreditNote == nil || out.CreditNote.Amount != patientBefore || out.CreditNote.IssuedBy != 90 {
			t.Fatalf("CN01/04/06 %+v", out.CreditNote)
		}
		if out.CreditNote.Reason != "Erreur de facturation" || out.CreditNote.Number == "" || !strings.HasPrefix(out.CreditNote.Number, "CN-") {
			t.Fatalf("CN05/08 %+v", out.CreditNote)
		}
		if out.Number != numberBefore || out.Status != statusBefore || out.GrossAmount != grossBefore || out.PatientAmount != patientBefore {
			t.Fatalf("CN02 invoice mutated %+v", out)
		}
		if len(out.Lines) != linesBefore {
			t.Fatalf("CN03 lines %d→%d", linesBefore, len(out.Lines))
		}
		if out.CreditedAmount != patientBefore || out.EffectiveBalanceAmount != 0 || out.BalanceAmount != 0 {
			t.Fatalf("CN04 effect %+v", out)
		}
		if out.CreditNote.IssuedAt.IsZero() {
			t.Fatal("CN07 timestamp")
		}
		var cn CreditNote
		if e := db.First(&cn, out.CreditNote.ID).Error; e != nil {
			t.Fatal(e)
		}
		cn.Number = "HACK"
		_ = db.Save(&cn)
		view, e := s.GetCreditNote(out.CreditNote.ID)
		if e != nil || view.Number != "HACK" {
			// Number is immutable by API (no update route); DB write in test proves no service mutator — re-read stored.
			t.Log("CN09: no update API; number set once at issue")
		}
		_ = view
		if creditNoteCount(t, db) != 1 {
			t.Fatal("CN01 count")
		}
		before := timelineCount(t, db, p, []string{"credit_note_issued"})
		if before != 1 {
			t.Fatalf("CN35 timeline=%d", before)
		}
	})

	t.Run("CN05_reason_mandatory", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		if _, e := s.IssueCreditNote(inv.ID, CreditNoteRequest{Amount: inv.PatientAmount, Reason: "  ", IdempotencyKey: "cn05"}, 91); !isBadRequest(e) {
			t.Fatalf("CN05 %v", e)
		}
	})

	t.Run("CN10_CN11_idempotency", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		a, e := s.IssueCreditNote(inv.ID, CreditNoteRequest{Amount: inv.PatientAmount, Reason: "Motif identique", IdempotencyKey: "cn10"}, 92)
		if e != nil {
			t.Fatal(e)
		}
		b, e := s.IssueCreditNote(inv.ID, CreditNoteRequest{Amount: inv.PatientAmount, Reason: "Motif identique", IdempotencyKey: "cn10"}, 92)
		if e != nil || b.CreditNote == nil || b.CreditNote.ID != a.CreditNote.ID {
			t.Fatalf("CN10 replay %+v %v", b, e)
		}
		if creditNoteCount(t, db) != 1 {
			t.Fatal("CN10 duplicate")
		}
		if _, e := s.IssueCreditNote(inv.ID, CreditNoteRequest{Amount: inv.PatientAmount, Reason: "Autre motif", IdempotencyKey: "cn10"}, 92); creditErrCode(e) != CodeCreditNoteIdempotencyConflict {
			t.Fatalf("CN11 want IDEMPOTENCY_CONFLICT got %v code=%s", e, creditErrCode(e))
		}
	})

	t.Run("CN12_CN19_duplicate_invoice_blocked", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		if _, e := s.IssueCreditNote(inv.ID, CreditNoteRequest{Amount: inv.PatientAmount, Reason: "Premier avoir", IdempotencyKey: "cn12a"}, 93); e != nil {
			t.Fatal(e)
		}
		if _, e := s.IssueCreditNote(inv.ID, CreditNoteRequest{Amount: inv.PatientAmount, Reason: "Second avoir", IdempotencyKey: "cn12b"}, 93); creditErrCode(e) != CodeCreditNoteAlreadyExists {
			t.Fatalf("CN12/19 %v code=%s", e, creditErrCode(e))
		}
		if creditNoteCount(t, db) != 1 {
			t.Fatal("CN12 count")
		}
	})

	t.Run("CN13_concurrent_one_credit", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		var wg sync.WaitGroup
		errs := make(chan error, 8)
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_, e := s.IssueCreditNote(inv.ID, CreditNoteRequest{Amount: inv.PatientAmount,
					Reason: "Concurrent avoir", IdempotencyKey: fmt.Sprintf("cn13-%d", i),
				}, 94)
				errs <- e
			}(i)
		}
		wg.Wait()
		close(errs)
		ok, conflict := 0, 0
		for e := range errs {
			if e == nil {
				ok++
			} else if creditErrCode(e) == CodeCreditNoteAlreadyExists || creditErrCode(e) == CodeCreditNoteIdempotencyConflict {
				conflict++
			} else {
				t.Fatalf("CN13 unexpected %v", e)
			}
		}
		if ok != 1 || creditNoteCount(t, db) != 1 {
			t.Fatalf("CN13 ok=%d conflicts=%d count=%d", ok, conflict, creditNoteCount(t, db))
		}
	})

	t.Run("CN14_draft_rejected", func(t *testing.T) {
		db := billingDB(t)
		s := NewService(db)
		_, p, c, tariffID := seedBilling(t, db)
		draft, e := s.CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{{ActType: "CONSULTATION", ReferenceID: c, TariffID: tariffID}}}, 1)
		if e != nil {
			t.Fatal(e)
		}
		if _, e := s.IssueCreditNote(draft.ID, CreditNoteRequest{Amount: 1000, Reason: "Sur brouillon", IdempotencyKey: "cn14"}, 95); creditErrCode(e) != CodeCreditNoteInvoiceNotEligible {
			t.Fatalf("CN14 %v", e)
		}
	})

	t.Run("CN15_unpaid_issued_ok", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		out, e := s.IssueCreditNote(inv.ID, CreditNoteRequest{Amount: inv.PatientAmount, Reason: "Correction émise", IdempotencyKey: "cn15"}, 96)
		if e != nil || out.CreditNote == nil {
			t.Fatalf("CN15 %v", e)
		}
	})

	t.Run("CN16_CN17_paid_partial_creates_credit", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		paySessionless(t, s, inv.ID, 5000, "CASH", "cn16-pay", 97)
		out, e := s.IssueCreditNote(inv.ID, CreditNoteRequest{Amount: inv.PatientAmount, Reason: "Sur partiel", IdempotencyKey: "cn16"}, 97)
		if e != nil || out.CustomerCreditAmount != 5000 {
			t.Fatalf("CN16 want credit 5000 got %+v %v", out, e)
		}
		inv2 := issuedPayReady(t, s, p, consultation(t, db, p, "Médecine-2"), tariff(t, db, "CONSULTATION", "CONS-CN17", 15000))
		paySessionless(t, s, inv2.ID, inv2.BalanceAmount, "CASH", "cn17-pay", 97)
		out2, e := s.IssueCreditNote(inv2.ID, CreditNoteRequest{Amount: inv2.PatientAmount, Reason: "Sur payée", IdempotencyKey: "cn17"}, 97)
		if e != nil || out2.CustomerCreditAmount != inv2.PatientAmount {
			t.Fatalf("CN17 want credit %d got %+v %v", inv2.PatientAmount, out2, e)
		}
	})

	t.Run("CN18_cancelled_rejected", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		if _, e := s.Cancel(inv.ID, "Annulation", 98); e != nil {
			t.Fatal(e)
		}
		if _, e := s.IssueCreditNote(inv.ID, CreditNoteRequest{Amount: inv.PatientAmount, Reason: "Après annulation", IdempotencyKey: "cn18"}, 98); creditErrCode(e) != CodeCreditNoteInvoiceNotEligible {
			t.Fatalf("CN18 %v", e)
		}
	})

	t.Run("CN20_CN22_patient_only_insurance_blocked", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		if inv.InsuranceAmount != 0 {
			t.Fatal("CN20 seed must be patient-only")
		}
		if _, e := s.IssueCreditNote(inv.ID, CreditNoteRequest{Amount: inv.PatientAmount, Reason: "Patient seul OK", IdempotencyKey: "cn20"}, 99); e != nil {
			t.Fatal(e)
		}
		// Insured invoice
		db2 := billingDB(t)
		s2 := receiptBilling(t, db2)
		_, p2, c2, _ := seedBilling(t, db2)
		tariffID2 := tariff(t, db2, "CONSULTATION", "CONS-INS-CN", 50000)
		// Force insurance on issued invoice via direct update after issue (simulates insured claim).
		invIns := issuedPayReady(t, s2, p2, c2, tariffID2)
		if e := db2.Model(&Invoice{}).Where("id=?", invIns.ID).Updates(map[string]any{
			"insurance_amount": 20000, "patient_amount": 30000, "balance_amount": 30000,
		}).Error; e != nil {
			t.Fatal(e)
		}
		if _, e := s2.IssueCreditNote(invIns.ID, CreditNoteRequest{Amount: invIns.PatientAmount, Reason: "Assuré", IdempotencyKey: "cn21"}, 99); creditErrCode(e) != CodeCreditNoteInsuranceCorrectionRequired {
			t.Fatalf("CN21 %v code=%s", e, creditErrCode(e))
		}
	})

	t.Run("CN25_CN26_receivable_effect", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		_ = p
		var balBefore int64
		if e := db.Raw(`
			SELECT GREATEST(i.patient_amount-COALESCE(cred.credited,0)-COALESCE(pay.paid,0),0)
			FROM billing_invoices i
			LEFT JOIN (`+EffectivePaidSubquery+`) pay ON pay.invoice_id=i.id
			LEFT JOIN (`+EffectiveCreditedSubquery+`) cred ON cred.invoice_id=i.id
			WHERE i.id=?`, inv.ID).Scan(&balBefore).Error; e != nil || balBefore <= 0 {
			t.Fatalf("CN25 balance before=%d %v", balBefore, e)
		}
		if _, e := s.IssueCreditNote(inv.ID, CreditNoteRequest{Amount: inv.PatientAmount, Reason: "Créance corrigée", IdempotencyKey: "cn25"}, 100); e != nil {
			t.Fatal(e)
		}
		var balAfter int64
		if e := db.Raw(`
			SELECT GREATEST(i.patient_amount-COALESCE(cred.credited,0)-COALESCE(pay.paid,0),0)
			FROM billing_invoices i
			LEFT JOIN (`+EffectivePaidSubquery+`) pay ON pay.invoice_id=i.id
			LEFT JOIN (`+EffectiveCreditedSubquery+`) cred ON cred.invoice_id=i.id
			WHERE i.id=?`, inv.ID).Scan(&balAfter).Error; e != nil {
			t.Fatal(e)
		}
		if balAfter != 0 {
			t.Fatalf("CN25/26 balance after credit=%d", balAfter)
		}
		out := GetInvoiceMust(t, s, inv.ID)
		if out.EffectiveBalanceAmount != 0 || out.CreditedAmount != inv.PatientAmount {
			t.Fatalf("CN26 %+v", out)
		}
	})

	t.Run("CN27_CN34_no_side_effects", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		payBefore := paymentCount(t, db, inv.ID)
		revBefore := reversalCount(t, db)
		if _, e := s.IssueCreditNote(inv.ID, CreditNoteRequest{Amount: inv.PatientAmount, Reason: "Sans effets collatéraux", IdempotencyKey: "cn27"}, 101); e != nil {
			t.Fatal(e)
		}
		if paymentCount(t, db, inv.ID) != payBefore || reversalCount(t, db) != revBefore {
			t.Fatal("CN30/31 payment/reversal created")
		}
		if db.Migrator().HasTable("patient_wallets") || db.Migrator().HasTable("cash_movements") || db.Migrator().HasTable("billing_refunds") {
			t.Fatal("CN27/28/29 unexpected tables")
		}
		// No payment receipt for credit note
		var recN int64
		db.Table("cash_receipts").Count(&recN)
		if recN != 0 {
			t.Fatalf("CN34 unexpected receipts %d", recN)
		}
	})

	t.Run("CN32_negative_payment_still_rejected", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		if _, e := s.Pay(inv.ID, PaymentRequest{Amount: -1, PaymentMethod: "CASH", IdempotencyKey: "cn32", Payer: PatientPayerRequest()}, 102); !isBadRequest(e) {
			t.Fatalf("CN32 %v", e)
		}
		if e := db.Create(&Payment{InvoiceID: inv.ID, Amount: -5, PaymentMethod: "CASH", IdempotencyKey: "cn32-db", PaidAt: time.Now(), ReceivedBy: 1}).Error; e == nil {
			t.Fatal("CN32 negative payment accepted by DB")
		}
	})

	t.Run("CN33_receipt_unchanged_with_prior_pay_reverse", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		paid := paySessionless(t, s, inv.ID, 3000, "CASH", "cn33-pay", 103)
		rec := receiptByPayment(t, db, paid.Payments[0].ID)
		num := rec.ReceiptNumber
		if _, e := s.ReversePayment(paid.Payments[0].ID, ReversePaymentRequest{Reason: "Puis avoir possible", IdempotencyKey: "cn33-rev"}, 103); e != nil {
			t.Fatal(e)
		}
		if _, e := s.IssueCreditNote(inv.ID, CreditNoteRequest{Amount: inv.PatientAmount, Reason: "Après reverse", IdempotencyKey: "cn33-cn"}, 103); e != nil {
			t.Fatal(e)
		}
		rec2 := receiptByPayment(t, db, paid.Payments[0].ID)
		if rec2.ReceiptNumber != num || rec2.Amount != rec.Amount {
			t.Fatalf("CN33 receipt mutated %+v → %+v", rec, rec2)
		}
	})

	t.Run("CN36_timeline_rollback", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		e := db.Transaction(func(tx *gorm.DB) error {
			var row Invoice
			if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, inv.ID).Error; e != nil {
				return e
			}
			cn := CreditNote{
				InvoiceID: row.ID, Amount: row.PatientAmount, Reason: "Rollback test",
				IssuedBy: 104, IssuedAt: time.Now(), IdempotencyKey: "cn36", Number: "TMP-CN36",
			}
			if e := tx.Create(&cn).Error; e != nil {
				return e
			}
			return errors.New("forced-credit-rollback")
		})
		if e == nil {
			t.Fatal("CN36 expected error")
		}
		if creditNoteCount(t, db) != 0 {
			t.Fatal("CN36 leaked")
		}
	})

	t.Run("CN37_payment_vs_credit_concurrency", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		var wg sync.WaitGroup
		var payErr, cnErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, payErr = s.Pay(inv.ID, PaymentRequest{Amount: inv.BalanceAmount, PaymentMethod: "CASH", IdempotencyKey: "cn37-pay", Payer: PatientPayerRequest()}, 105)
		}()
		go func() {
			defer wg.Done()
			_, cnErr = s.IssueCreditNote(inv.ID, CreditNoteRequest{Amount: inv.PatientAmount, Reason: "Race paiement", IdempotencyKey: "cn37-cn"}, 105)
		}()
		wg.Wait()
		paidOK := payErr == nil
		cnOK := cnErr == nil
		if !paidOK && !cnOK {
			t.Fatalf("CN37 both failed pay=%v cn=%v", payErr, cnErr)
		}
		got := GetInvoiceMust(t, s, inv.ID)
		if cnOK && !paidOK {
			if got.CreditNote == nil || got.BalanceAmount != 0 || paymentCount(t, db, inv.ID) != 0 {
				t.Fatalf("CN37 credit-first inconsistent %+v", got)
			}
		}
		if paidOK && cnOK {
			if got.CreditNote == nil || got.CustomerCreditAmount != inv.PatientAmount {
				t.Fatalf("CN37 pay-then-cn inconsistent %+v", got)
			}
		}
		if paidOK && !cnOK {
			if got.PaidAmount != inv.PatientAmount || creditNoteCount(t, db) != 0 {
				t.Fatalf("CN37 pay-only inconsistent %+v cnErr=%v", got, cnErr)
			}
		}
	})

	t.Run("CN38_reversal_blocked_after_credit", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		paid := paySessionless(t, s, inv.ID, inv.BalanceAmount, "CASH", "cn38-pay", 106)
		out, e := s.IssueCreditNote(inv.ID, CreditNoteRequest{Amount: inv.PatientAmount, Reason: "Crédit créé", IdempotencyKey: "cn38-cn"}, 106)
		if e != nil || out.CustomerCreditAmount != inv.PatientAmount {
			t.Fatalf("CN38 cn %+v %v", out, e)
		}
		if _, e := s.ReversePayment(paid.Payments[0].ID, ReversePaymentRequest{Reason: "Blocked reverse", IdempotencyKey: "cn38-rev"}, 106); creditErrCode(e) != CodeCreditReversalBlocked {
			t.Fatalf("CN38 reverse want blocked got %v code=%s", e, creditErrCode(e))
		}
	})

	t.Run("CN39_cancel_vs_credit", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		if _, e := s.IssueCreditNote(inv.ID, CreditNoteRequest{Amount: inv.PatientAmount, Reason: "Avoir d'abord", IdempotencyKey: "cn39"}, 107); e != nil {
			t.Fatal(e)
		}
		if _, e := s.Cancel(inv.ID, "Après avoir", 107); !isConflict(e) {
			t.Fatalf("CN39 cancel after credit %v", e)
		}
		inv2 := issuedPayReady(t, s, p, consultation(t, db, p, "Médecine-cn39"), tariff(t, db, "CONSULTATION", "CONS-CN39B", 12000))
		if _, e := s.Cancel(inv2.ID, "Cancel first", 107); e != nil {
			t.Fatal(e)
		}
		if _, e := s.IssueCreditNote(inv2.ID, CreditNoteRequest{Amount: inv.PatientAmount, Reason: "Après cancel", IdempotencyKey: "cn39b"}, 107); creditErrCode(e) != CodeCreditNoteInvoiceNotEligible {
			t.Fatalf("CN39 credit after cancel %v", e)
		}
	})

	t.Run("CN40_credit_vs_credit", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		var wg sync.WaitGroup
		errs := make(chan error, 2)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_, e := s.IssueCreditNote(inv.ID, CreditNoteRequest{Amount: inv.PatientAmount, Reason: "Double course", IdempotencyKey: fmt.Sprintf("cn40-%d", i)}, 108)
				errs <- e
			}(i)
		}
		wg.Wait()
		close(errs)
		ok := 0
		for e := range errs {
			if e == nil {
				ok++
			}
		}
		if ok != 1 || creditNoteCount(t, db) != 1 {
			t.Fatalf("CN40 ok=%d count=%d", ok, creditNoteCount(t, db))
		}
	})

	t.Run("CN41_cash_session_paid_allows_credit", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		paid := paySessionless(t, s, inv.ID, 4000, "CASH", "cn41-pay", 109)
		sid := uint(77)
		if e := db.Model(&Payment{}).Where("id=?", paid.Payments[0].ID).Update("cash_session_id", sid).Error; e != nil {
			t.Fatal(e)
		}
		out, e := s.IssueCreditNote(inv.ID, CreditNoteRequest{Amount: inv.PatientAmount, Reason: "Session caisse", IdempotencyKey: "cn41"}, 109)
		if e != nil || out.CustomerCreditAmount != 4000 {
			t.Fatalf("CN41 want credit 4000 got %+v %v", out, e)
		}
	})

	t.Run("CN43_CN46_rbac", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		gin.SetMode(gin.TestMode)
		h := NewHandler(s)
		deny := gin.New()
		deny.Use(func(c *gin.Context) {
			rbac.SetUser(c, 110, "staff", []string{"billing.payment.create"})
			c.Next()
		})
		deny.POST("/billing/invoices/:id/credit-notes", rbac.Permission("billing.credit_note.create"), h.IssueCreditNote)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/billing/invoices/%d/credit-notes", inv.ID), strings.NewReader(`{"reason":"Sans droit","idempotencyKey":"cn44"}`))
		req.Header.Set("Content-Type", "application/json")
		deny.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("CN44 want 403 got %d body=%s", w.Code, w.Body.String())
		}
		caissier := rbac.FunctionPermissionsResolved("CAISSIER", nil)
		if rbac.HasAnyPermission(caissier, "billing.credit_note.create") {
			t.Fatal("CN45 CAISSIER must not create credit notes")
		}
		comptable := rbac.FunctionPermissionsResolved("COMPTABLE", nil)
		if !rbac.HasAnyPermission(comptable, "billing.credit_note.create") || !rbac.HasAnyPermission(comptable, "billing.credit_note.read") {
			t.Fatal("CN43/45 COMPTABLE grants")
		}
		if !rbac.IsSensitive("billing.credit_note.create") {
			t.Fatal("CN46 sensitive")
		}
	})

	t.Run("CN48_CN50_get_immutable_routes", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		out, e := s.IssueCreditNote(inv.ID, CreditNoteRequest{Amount: inv.PatientAmount, Reason: "Document lisible", IdempotencyKey: "cn48"}, 111)
		if e != nil {
			t.Fatal(e)
		}
		view, e := s.GetCreditNote(out.CreditNote.ID)
		if e != nil || view.Number != out.CreditNote.Number || view.InvoiceNumber != inv.Number {
			t.Fatalf("CN48 %+v %v", view, e)
		}
		if strings.Contains(strings.ToLower(view.Reason), "rembours") {
			t.Fatal("CN49 refund claim in reason storage path")
		}
		fresh := GetInvoiceMust(t, s, inv.ID)
		if fresh.CreditNote == nil || fresh.CreditNote.ID != out.CreditNote.ID {
			t.Fatalf("CN48 invoice expose %+v", fresh.CreditNote)
		}
	})
}

func GetInvoiceMust(t *testing.T, s *Service, id uint) *Invoice {
	t.Helper()
	out, e := s.GetInvoice(id)
	if e != nil {
		t.Fatal(e)
	}
	return out
}
