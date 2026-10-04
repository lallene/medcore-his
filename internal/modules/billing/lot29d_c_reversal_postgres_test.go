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

func reversalCount(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var n int64
	if e := db.Model(&PaymentReversal{}).Count(&n).Error; e != nil {
		t.Fatal(e)
	}
	return n
}

func paySessionless(t *testing.T, s *Service, invID uint, amount int64, method, key string, user uint) *Invoice {
	t.Helper()
	out, e := s.Pay(invID, PaymentRequest{Amount: amount, PaymentMethod: method, IdempotencyKey: key}, user)
	if e != nil {
		t.Fatal(e)
	}
	return out
}

func isNotFoundErr(e error) bool {
	var app *coreerrors.AppError
	return e != nil && errors.As(e, &app) && app.Status == 404
}

func TestLOT29D_C_PaymentReversalMatrix(t *testing.T) {
	t.Run("RV01_RV09_full_reversal_only_payment", func(t *testing.T) {
		db := billingDB(t)
		db.Create(&billingUser{ID: 70, Name: "Superviseur"})
		s := receiptBilling(t, db)
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		paid := paySessionless(t, s, inv.ID, inv.BalanceAmount, "CASH", "rv01-pay", 70)
		payID := paid.Payments[0].ID
		out, e := s.ReversePayment(payID, ReversePaymentRequest{Reason: "Erreur de saisie", IdempotencyKey: "rv01-rev"}, 70)
		if e != nil {
			t.Fatal(e)
		}
		if out.Status != InvoiceIssued || out.PaidAmount != 0 || out.BalanceAmount != inv.PatientAmount {
			t.Fatalf("RV07/08/09 %+v", out)
		}
		if len(out.Payments) != 1 || out.Payments[0].ID != payID || out.Payments[0].Amount != inv.PatientAmount {
			t.Fatalf("RV02 payment mutated %+v", out.Payments)
		}
		if !out.Payments[0].Reversed || out.Payments[0].ReversalID == nil {
			t.Fatalf("RV03 not linked %+v", out.Payments[0])
		}
		rec := receiptByPayment(t, db, payID)
		if rec.Amount != inv.PatientAmount || rec.ReceiptNumber == "" {
			t.Fatalf("RV25 receipt %+v", rec)
		}
		var rev PaymentReversal
		if e := db.Where("original_payment_id=?", payID).First(&rev).Error; e != nil || rev.Amount != inv.PatientAmount || rev.Reason != "Erreur de saisie" || rev.ReversedBy != 70 {
			t.Fatalf("RV04/05/06 %+v %v", rev, e)
		}
	})

	t.Run("RV10_RV13_multiple_payments", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		a := paySessionless(t, s, inv.ID, 8000, "CASH", "rv10a", 71)
		b := paySessionless(t, s, inv.ID, 12000, "CARD", "rv10b", 71)
		idA, idB := a.Payments[0].ID, b.Payments[1].ID

		onlyA, e := s.ReversePayment(idA, ReversePaymentRequest{Reason: "Contrepassation A", IdempotencyKey: "rv11"}, 71)
		if e != nil || onlyA.PaidAmount != 12000 || onlyA.BalanceAmount != 8000 || onlyA.Status != InvoicePartiallyPaid {
			t.Fatalf("RV11 %+v %v", onlyA, e)
		}
		onlyB, e := s.ReversePayment(idB, ReversePaymentRequest{Reason: "Contrepassation B", IdempotencyKey: "rv12"}, 71)
		if e != nil || onlyB.PaidAmount != 0 || onlyB.BalanceAmount != inv.PatientAmount || onlyB.Status != InvoiceIssued {
			t.Fatalf("RV12/13 %+v %v", onlyB, e)
		}
	})

	t.Run("RV14_insurance_unchanged", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		insBefore, patientBefore := inv.InsuranceAmount, inv.PatientAmount
		paid := paySessionless(t, s, inv.ID, 5000, "CASH", "rv14-pay", 73)
		out, e := s.ReversePayment(paid.Payments[0].ID, ReversePaymentRequest{Reason: "Correction assurance", IdempotencyKey: "rv14"}, 73)
		if e != nil {
			t.Fatal(e)
		}
		if out.InsuranceAmount != insBefore || out.PatientAmount != patientBefore {
			t.Fatalf("RV14 insurance mutated %+v", out)
		}
	})

	t.Run("RV15_receivable_effective_paid", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		paid := paySessionless(t, s, inv.ID, 7000, "CASH", "rv15-pay", 74)
		if _, e := s.ReversePayment(paid.Payments[0].ID, ReversePaymentRequest{Reason: "Recouvrement", IdempotencyKey: "rv15"}, 74); e != nil {
			t.Fatal(e)
		}
		eff, e := EffectivePaidOnInvoice(db, inv.ID)
		if e != nil || eff != 0 {
			t.Fatalf("RV15 effective=%d %v", eff, e)
		}
	})

	t.Run("RV16_RV17_idempotency", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		paid := paySessionless(t, s, inv.ID, 4000, "CASH", "rv16-pay", 75)
		req := ReversePaymentRequest{Reason: "Motif stable", IdempotencyKey: "rv16-key"}
		first, e := s.ReversePayment(paid.Payments[0].ID, req, 75)
		if e != nil {
			t.Fatal(e)
		}
		second, e := s.ReversePayment(paid.Payments[0].ID, req, 75)
		if e != nil {
			t.Fatal(e)
		}
		if *first.Payments[0].ReversalID != *second.Payments[0].ReversalID || reversalCount(t, db) != 1 {
			t.Fatal("RV16 duplicate")
		}
		if _, e := s.ReversePayment(paid.Payments[0].ID, ReversePaymentRequest{Reason: "Autre motif", IdempotencyKey: "rv16-key"}, 75); !isConflict(e) {
			t.Fatalf("RV17 want conflict got %v", e)
		}
	})

	t.Run("RV18_second_key_already_reversed", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		paid := paySessionless(t, s, inv.ID, 3000, "CASH", "rv18-pay", 76)
		if _, e := s.ReversePayment(paid.Payments[0].ID, ReversePaymentRequest{Reason: "Premier", IdempotencyKey: "rv18a"}, 76); e != nil {
			t.Fatal(e)
		}
		fresh, _ := s.GetInvoice(inv.ID)
		balBefore := fresh.BalanceAmount
		if _, e := s.ReversePayment(paid.Payments[0].ID, ReversePaymentRequest{Reason: "Second", IdempotencyKey: "rv18b"}, 76); !isConflict(e) {
			t.Fatalf("RV18 want conflict %v", e)
		}
		after, _ := s.GetInvoice(inv.ID)
		if after.BalanceAmount != balBefore || reversalCount(t, db) != 1 {
			t.Fatal("RV18 second effect")
		}
	})

	t.Run("RV19_concurrent_same_payment", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		paid := paySessionless(t, s, inv.ID, 6000, "CASH", "rv19-pay", 77)
		payID := paid.Payments[0].ID
		start := make(chan struct{})
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				<-start
				_, e := s.ReversePayment(payID, ReversePaymentRequest{Reason: "Concurrent", IdempotencyKey: fmt.Sprintf("rv19-%d", n)}, 77)
				errs <- e
			}(i)
		}
		close(start)
		wg.Wait()
		close(errs)
		ok, conflict := 0, 0
		for e := range errs {
			if e == nil {
				ok++
			} else if isConflict(e) {
				conflict++
			} else {
				t.Fatal(e)
			}
		}
		if ok != 1 || conflict != 1 || reversalCount(t, db) != 1 {
			t.Fatalf("RV19 ok=%d conflict=%d revs=%d", ok, conflict, reversalCount(t, db))
		}
		out, _ := s.GetInvoice(inv.ID)
		if out.PaidAmount != 0 || out.BalanceAmount != inv.PatientAmount {
			t.Fatalf("RV19 balance %+v", out)
		}
	})

	t.Run("RV20_reversal_and_new_payment", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		paid := paySessionless(t, s, inv.ID, 5000, "CASH", "rv20-pay", 78)
		payID := paid.Payments[0].ID
		start := make(chan struct{})
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, e := s.ReversePayment(payID, ReversePaymentRequest{Reason: "Race reverse", IdempotencyKey: "rv20-rev"}, 78)
			errs <- e
		}()
		go func() {
			defer wg.Done()
			<-start
			_, e := s.Pay(inv.ID, PaymentRequest{Amount: 3000, PaymentMethod: "CARD", IdempotencyKey: "rv20-new"}, 78)
			errs <- e
		}()
		close(start)
		wg.Wait()
		close(errs)
		for e := range errs {
			if e != nil {
				t.Fatal(e)
			}
		}
		out, _ := s.GetInvoice(inv.ID)
		if out.PaidAmount != 3000 || out.BalanceAmount != inv.PatientAmount-3000 {
			t.Fatalf("RV20 %+v", out)
		}
		if reversalCount(t, db) != 1 || paymentCount(t, db, inv.ID) != 2 {
			t.Fatalf("RV20 counts rev=%d pay=%d", reversalCount(t, db), paymentCount(t, db, inv.ID))
		}
	})

	t.Run("RV21_reversal_then_cancel", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		paid := paySessionless(t, s, inv.ID, inv.BalanceAmount, "CASH", "rv21-pay", 79)
		if _, e := s.Cancel(inv.ID, "avant reverse", 79); !isConflict(e) {
			t.Fatal("RV21 cancel while paid should fail")
		}
		if _, e := s.ReversePayment(paid.Payments[0].ID, ReversePaymentRequest{Reason: "Puis reverse", IdempotencyKey: "rv21"}, 79); e != nil {
			t.Fatal(e)
		}
		cancelled, e := s.Cancel(inv.ID, "Après contrepassation", 79)
		if e != nil || cancelled.Status != InvoiceCancelled {
			t.Fatalf("RV21 cancel after reverse %+v %v", cancelled, e)
		}
	})

	t.Run("RV22_RV23_authorization", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		paid := paySessionless(t, s, inv.ID, 2000, "CASH", "rv22-pay", 80)
		gin.SetMode(gin.TestMode)
		h := NewHandler(s)
		deny := gin.New()
		deny.Use(func(c *gin.Context) {
			rbac.SetUser(c, 80, "staff", []string{"billing.payment.create"})
			c.Next()
		})
		RegisterRoutes(deny.Group("/api"), h)
		body := `{"reason":"Sans droit","idempotencyKey":"rv22"}`
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/billing/payments/%d/reverse", paid.Payments[0].ID), strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		deny.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("RV22/23 want 403 got %d %s", w.Code, w.Body.String())
		}
		if reversalCount(t, db) != 0 {
			t.Fatal("RV23 mutated")
		}
		caissier := rbac.EffectiveStaffPermissions("staff", []string{"CAISSIER"}, nil)
		comptable := rbac.EffectiveStaffPermissions("staff", []string{"COMPTABLE"}, nil)
		has := func(perms []string, key string) bool {
			for _, x := range perms {
				if x == key {
					return true
				}
			}
			return false
		}
		if has(caissier, "billing.payment.reverse") {
			t.Fatal("RV23 CAISSIER must not reverse")
		}
		if !has(comptable, "billing.payment.reverse") {
			t.Fatal("RV23 COMPTABLE should reverse")
		}
	})

	t.Run("RV24_timeline_once", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		paid := paySessionless(t, s, inv.ID, 2500, "CASH", "rv24-pay", 81)
		req := ReversePaymentRequest{Reason: "Timeline", IdempotencyKey: "rv24"}
		if _, e := s.ReversePayment(paid.Payments[0].ID, req, 81); e != nil {
			t.Fatal(e)
		}
		before := timelineCount(t, db, p, []string{"payment_reversed"})
		if _, e := s.ReversePayment(paid.Payments[0].ID, req, 81); e != nil {
			t.Fatal(e)
		}
		after := timelineCount(t, db, p, []string{"payment_reversed"})
		if before != 1 || after != 1 {
			t.Fatalf("RV24 before=%d after=%d", before, after)
		}
	})

	t.Run("RV26_receipt_retained_payment_reversed_flag", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		paid := paySessionless(t, s, inv.ID, 1500, "CARD", "rv26-pay", 82)
		rid := *paid.Payments[0].ReceiptID
		if _, e := s.ReversePayment(paid.Payments[0].ID, ReversePaymentRequest{Reason: "Marquage reçu", IdempotencyKey: "rv26"}, 82); e != nil {
			t.Fatal(e)
		}
		out, e := s.GetInvoice(inv.ID)
		if e != nil || !out.Payments[0].Reversed || out.Payments[0].ReceiptID == nil || *out.Payments[0].ReceiptID != rid {
			t.Fatalf("RV26 %+v %v", out.Payments, e)
		}
		if receiptCount(t, db) != 1 {
			t.Fatal("RV25/26 receipt deleted")
		}
	})

	t.Run("RV27_nonexistent_payment", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, e := s.ReversePayment(999999, ReversePaymentRequest{Reason: "Inexistant xx", IdempotencyKey: "rv27"}, 83)
		if !isNotFoundErr(e) {
			t.Fatalf("RV27 want not found got %v", e)
		}
	})

	t.Run("RV28_RV29_rollback", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		paid := paySessionless(t, s, inv.ID, 4500, "CASH", "rv28-pay", 84)
		payID := paid.Payments[0].ID
		beforePaid, beforeBal, beforeStatus := paid.PaidAmount, paid.BalanceAmount, paid.Status
		e := db.Transaction(func(tx *gorm.DB) error {
			var pay Payment
			if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&pay, payID).Error; e != nil {
				return e
			}
			if e := tx.Create(&PaymentReversal{
				OriginalPaymentID: pay.ID, Amount: pay.Amount, Reason: "Inject fail",
				ReversedBy: 84, ReversedAt: time.Now(), IdempotencyKey: "rv28-inject",
			}).Error; e != nil {
				return e
			}
			var invRow Invoice
			if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&invRow, inv.ID).Error; e != nil {
				return e
			}
			invRow.PaidAmount = 0
			invRow.BalanceAmount = inv.PatientAmount
			invRow.Status = InvoiceIssued
			if e := tx.Save(&invRow).Error; e != nil {
				return e
			}
			return errors.New("forced-reversal-rollback")
		})
		if e == nil {
			t.Fatal("RV28 expected error")
		}
		var fresh Invoice
		db.First(&fresh, inv.ID)
		if fresh.PaidAmount != beforePaid || fresh.BalanceAmount != beforeBal || fresh.Status != beforeStatus {
			t.Fatalf("RV28/29 not rolled back %+v", fresh)
		}
		if reversalCount(t, db) != 0 {
			t.Fatal("RV28 reversal leaked")
		}
	})

	t.Run("RV30_cash_session_without_hooks_rejected", func(t *testing.T) {
		// Billing-only test DB has no cash hooks; session-linked reverse stays blocked here.
		// OPEN CASH atomic path is covered by cash LOT29F-D postgres tests (with hooks wired).
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		paid := paySessionless(t, s, inv.ID, 5000, "CASH", "rv30-pay", 86)
		sid := uint(99)
		if e := db.Model(&Payment{}).Where("id=?", paid.Payments[0].ID).Update("cash_session_id", sid).Error; e != nil {
			t.Fatal(e)
		}
		if _, e := s.ReversePayment(paid.Payments[0].ID, ReversePaymentRequest{Reason: "Session caisse", IdempotencyKey: "rv30"}, 86); !isConflict(e) {
			t.Fatalf("RV30 want conflict got %v", e)
		}
		if reversalCount(t, db) != 0 {
			t.Fatal("RV30 reversal created")
		}
	})

	t.Run("RV05_empty_reason", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		paid := paySessionless(t, s, inv.ID, 1000, "CASH", "rv05-pay", 85)
		if _, e := s.ReversePayment(paid.Payments[0].ID, ReversePaymentRequest{Reason: "  ", IdempotencyKey: "rv05"}, 85); !isBadRequest(e) {
			t.Fatalf("RV05 want bad request %v", e)
		}
	})
}
