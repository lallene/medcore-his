package billing

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/authorization"
	"github.com/lallene/medcore-his/backend/internal/modules/medical_records"
	"gorm.io/gorm"
)

func isBadRequest(e error) bool {
	var app *coreerrors.AppError
	return e != nil && errors.As(e, &app) && app.Status == 400
}

func issuedPayReady(t *testing.T, s *Service, p, c, tariffID uint) *Invoice {
	t.Helper()
	x := invoiceFor(t, s, p, c, tariffID)
	out, e := s.Issue(x.ID, 2)
	if e != nil {
		t.Fatal(e)
	}
	return out
}

func paymentCount(t *testing.T, db *gorm.DB, invoiceID uint) int64 {
	t.Helper()
	var n int64
	if e := db.Model(&Payment{}).Where("invoice_id=?", invoiceID).Count(&n).Error; e != nil {
		t.Fatal(e)
	}
	return n
}

func timelineCount(t *testing.T, db *gorm.DB, patientID uint, types []string) int64 {
	t.Helper()
	var n int64
	if e := db.Model(&medical_records.MedicalTimelineEvent{}).
		Where("patient_id=? AND event_type IN ?", patientID, types).Count(&n).Error; e != nil {
		t.Fatal(e)
	}
	return n
}

func TestLOT29B_PaymentIntegrityMatrix(t *testing.T) {
	t.Run("B01_normal_payment", func(t *testing.T) {
		db := billingDB(t)
		s, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		out, e := s.Pay(inv.ID, PaymentRequest{Amount: 5000, PaymentMethod: "CASH", IdempotencyKey: "b01", Payer: PatientPayerRequest()}, 11)
		if e != nil || out.PaidAmount != 5000 || out.BalanceAmount != inv.PatientAmount-5000 {
			t.Fatalf("B01 %+v %v", out, e)
		}
		if paymentCount(t, db, inv.ID) != 1 {
			t.Fatal("B01 payment rows")
		}
	})

	t.Run("B02_partial_payment", func(t *testing.T) {
		db := billingDB(t)
		s, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		out, e := s.Pay(inv.ID, PaymentRequest{Amount: 7000, PaymentMethod: "CARD", IdempotencyKey: "b02", Payer: PatientPayerRequest()}, 12)
		if e != nil || out.Status != InvoicePartiallyPaid || out.BalanceAmount != 13000 {
			t.Fatalf("B02 %+v %v", out, e)
		}
	})

	t.Run("B03_exact_final_payment", func(t *testing.T) {
		db := billingDB(t)
		s, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		out, e := s.Pay(inv.ID, PaymentRequest{Amount: inv.BalanceAmount, PaymentMethod: "CASH", IdempotencyKey: "b03", Payer: PatientPayerRequest()}, 13)
		if e != nil || out.Status != InvoicePaid || out.BalanceAmount != 0 {
			t.Fatalf("B03 %+v %v", out, e)
		}
	})

	t.Run("B04_overpayment_rejected", func(t *testing.T) {
		db := billingDB(t)
		s, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		if _, e := s.Pay(inv.ID, PaymentRequest{Amount: inv.BalanceAmount + 1, PaymentMethod: "CASH", IdempotencyKey: "b04", Payer: PatientPayerRequest()}, 14); !isConflict(e) {
			t.Fatalf("B04 want conflict got %v", e)
		}
		if paymentCount(t, db, inv.ID) != 0 {
			t.Fatal("B04 mutated")
		}
	})

	t.Run("B05_zero_rejected", func(t *testing.T) {
		db := billingDB(t)
		s, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		if _, e := s.Pay(inv.ID, PaymentRequest{Amount: 0, PaymentMethod: "CASH", IdempotencyKey: "b05", Payer: PatientPayerRequest()}, 15); !isBadRequest(e) {
			t.Fatalf("B05 want bad request got %v", e)
		}
	})

	t.Run("B06_negative_rejected", func(t *testing.T) {
		db := billingDB(t)
		s, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		if _, e := s.Pay(inv.ID, PaymentRequest{Amount: -1, PaymentMethod: "CASH", IdempotencyKey: "b06", Payer: PatientPayerRequest()}, 16); !isBadRequest(e) {
			t.Fatalf("B06 want bad request got %v", e)
		}
	})

	t.Run("B07_settled_rejects", func(t *testing.T) {
		db := billingDB(t)
		s, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		if _, e := s.Pay(inv.ID, PaymentRequest{Amount: inv.BalanceAmount, PaymentMethod: "CASH", IdempotencyKey: "b07a", Payer: PatientPayerRequest()}, 17); e != nil {
			t.Fatal(e)
		}
		if _, e := s.Pay(inv.ID, PaymentRequest{Amount: 1, PaymentMethod: "CASH", IdempotencyKey: "b07b", Payer: PatientPayerRequest()}, 17); !isConflict(e) {
			t.Fatalf("B07 want conflict got %v", e)
		}
	})

	t.Run("B08_same_key_replay", func(t *testing.T) {
		db := billingDB(t)
		s, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		req := PaymentRequest{Amount: 4000, PaymentMethod: "CASH", IdempotencyKey: "b08-replay", Payer: PatientPayerRequest()}
		a, e := s.Pay(inv.ID, req, 18)
		if e != nil {
			t.Fatal(e)
		}
		b, e := s.Pay(inv.ID, req, 18)
		if e != nil {
			t.Fatal(e)
		}
		if a.PaidAmount != b.PaidAmount || paymentCount(t, db, inv.ID) != 1 {
			t.Fatalf("B08 replay mutated paid=%d/%d count=%d", a.PaidAmount, b.PaidAmount, paymentCount(t, db, inv.ID))
		}
	})

	t.Run("B09_same_key_different_payload", func(t *testing.T) {
		db := billingDB(t)
		s, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		if _, e := s.Pay(inv.ID, PaymentRequest{Amount: 3000, PaymentMethod: "CASH", IdempotencyKey: "b09", Payer: PatientPayerRequest()}, 19); e != nil {
			t.Fatal(e)
		}
		if _, e := s.Pay(inv.ID, PaymentRequest{Amount: 5000, PaymentMethod: "CASH", IdempotencyKey: "b09", Payer: PatientPayerRequest()}, 19); !isConflict(e) {
			t.Fatalf("B09 want conflict got %v", e)
		}
		if paymentCount(t, db, inv.ID) != 1 {
			t.Fatal("B09 second payment created")
		}
	})

	t.Run("B10_concurrent_same_key", func(t *testing.T) {
		db := billingDB(t)
		s, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		start := make(chan struct{})
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				_, e := s.Pay(inv.ID, PaymentRequest{Amount: 8000, PaymentMethod: "CASH", IdempotencyKey: "b10-same", Payer: PatientPayerRequest()}, 20)
				errs <- e
			}()
		}
		close(start)
		wg.Wait()
		close(errs)
		for e := range errs {
			if e != nil {
				t.Fatalf("B10 same-key must recover success, got %v", e)
			}
		}
		if paymentCount(t, db, inv.ID) != 1 {
			t.Fatalf("B10 payments=%d", paymentCount(t, db, inv.ID))
		}
		var sum int64
		db.Model(&Payment{}).Where("invoice_id=?", inv.ID).Select("COALESCE(SUM(amount),0)").Scan(&sum)
		if sum != 8000 {
			t.Fatalf("B10 sum=%d", sum)
		}
	})

	t.Run("B11_concurrent_different_key", func(t *testing.T) {
		db := billingDB(t)
		s, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		start := make(chan struct{})
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				<-start
				_, e := s.Pay(inv.ID, PaymentRequest{Amount: inv.BalanceAmount, PaymentMethod: "CASH", IdempotencyKey: fmt.Sprintf("b11-%d", n), Payer: PatientPayerRequest()}, 21)
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
		if ok != 1 || conflict != 1 || paymentCount(t, db, inv.ID) != 1 {
			t.Fatalf("B11 ok=%d conflict=%d payments=%d", ok, conflict, paymentCount(t, db, inv.ID))
		}
	})

	t.Run("B12_B13_forced_rollback", func(t *testing.T) {
		db := billingDB(t)
		s, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		beforePaid, beforeBal, beforeStatus := inv.PaidAmount, inv.BalanceAmount, inv.Status
		e := db.Transaction(func(tx *gorm.DB) error {
			_, payErr := s.PayInTransaction(tx, inv.ID, PaymentRequest{Amount: 6000, PaymentMethod: "CASH", IdempotencyKey: "b12-rollback", Payer: PatientPayerRequest()}, 22, nil)
			if payErr != nil {
				return payErr
			}
			return errors.New("forced-rollback")
		})
		if e == nil {
			t.Fatal("B12 expected forced error")
		}
		var fresh Invoice
		if e := db.First(&fresh, inv.ID).Error; e != nil {
			t.Fatal(e)
		}
		if fresh.PaidAmount != beforePaid || fresh.BalanceAmount != beforeBal || fresh.Status != beforeStatus {
			t.Fatalf("B13 balance not rolled back: %+v", fresh)
		}
		if paymentCount(t, db, inv.ID) != 0 {
			t.Fatal("B12 payment not rolled back")
		}
	})

	t.Run("B14_insurance_share_unaffected", func(t *testing.T) {
		db := billingDB(t)
		p, m := seedPatient(t, db, "B14")
		c := consultation(t, db, p, "A")
		rate := 50.0
		seedAuthorization(t, db, p, m, c, authorization.StatusApproved, &rate, 10000)
		s := NewService(db)
		tariffID := tariff(t, db, "CONSULTATION", "B14", 20000)
		inv := issuedPayReady(t, s, p, c, tariffID)
		if inv.InsuranceAmount != 10000 || inv.PatientAmount != 10000 {
			t.Fatalf("B14 split %+v", inv)
		}
		insBefore := inv.InsuranceAmount
		out, e := s.Pay(inv.ID, PaymentRequest{Amount: 4000, PaymentMethod: "CASH", IdempotencyKey: "b14", Payer: PatientPayerRequest()}, 23)
		if e != nil {
			t.Fatal(e)
		}
		if out.InsuranceAmount != insBefore || out.PatientAmount != 10000 || out.PaidAmount != 4000 || out.BalanceAmount != 6000 {
			t.Fatalf("B14 insurance mutated %+v", out)
		}
	})

	t.Run("B15_authoritative_actor", func(t *testing.T) {
		db := billingDB(t)
		s, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		if _, e := s.Pay(inv.ID, PaymentRequest{Amount: 2500, PaymentMethod: "CASH", IdempotencyKey: "b15", Payer: PatientPayerRequest()}, 99); e != nil {
			t.Fatal(e)
		}
		var pay Payment
		if e := db.Where("idempotency_key=?", "b15").First(&pay).Error; e != nil || pay.ReceivedBy != 99 {
			t.Fatalf("B15 actor=%+v %v", pay, e)
		}
	})

	t.Run("B16_permission_enforcement", func(t *testing.T) {
		db := billingDB(t)
		s, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		gin.SetMode(gin.TestMode)
		h := NewHandler(s)
		deny := gin.New()
		deny.Use(func(c *gin.Context) {
			rbac.SetUser(c, 7, "staff", []string{"billing.read"})
			c.Next()
		})
		RegisterRoutes(deny.Group("/api"), h)
		body, _ := json.Marshal(PaymentRequest{Amount: 1000, PaymentMethod: "CASH", IdempotencyKey: "b16", Payer: PatientPayerRequest()})
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/billing/invoices/%d/payments", inv.ID), bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		deny.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("B16 want 403 got %d body=%s", w.Code, w.Body.String())
		}
		if paymentCount(t, db, inv.ID) != 0 {
			t.Fatal("B16 payment created without permission")
		}
	})

	t.Run("B18_replay_no_duplicate_timeline", func(t *testing.T) {
		db := billingDB(t)
		s, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		req := PaymentRequest{Amount: 3500, PaymentMethod: "CASH", IdempotencyKey: "b18-timeline", Payer: PatientPayerRequest()}
		if _, e := s.Pay(inv.ID, req, 24); e != nil {
			t.Fatal(e)
		}
		before := timelineCount(t, db, p, []string{"payment_received", "invoice_paid"})
		if _, e := s.Pay(inv.ID, req, 24); e != nil {
			t.Fatal(e)
		}
		after := timelineCount(t, db, p, []string{"payment_received", "invoice_paid"})
		if after != before {
			t.Fatalf("B18 timeline before=%d after=%d", before, after)
		}
		if paymentCount(t, db, inv.ID) != 1 {
			t.Fatal("B18 duplicate payment")
		}
	})
}

func TestLOT29B_HeaderIdempotencyKey(t *testing.T) {
	db := billingDB(t)
	s, p, c, tariffID := seedBilling(t, db)
	inv := issuedPayReady(t, s, p, c, tariffID)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		rbac.SetUser(c, 31, "staff", []string{"billing.payment.create"})
		c.Next()
	})
	RegisterRoutes(r.Group("/api"), NewHandler(s))
	body, _ := json.Marshal(map[string]any{
		"amount": 1500, "paymentMethod": "CASH",
		"payer": map[string]any{"mode": "PATIENT"},
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/billing/invoices/%d/payments", inv.ID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "b29-header-only")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("header key want 200 got %d %s", w.Code, w.Body.String())
	}
	var pay Payment
	if e := db.Where("idempotency_key=?", "b29-header-only").First(&pay).Error; e != nil || pay.ReceivedBy != 31 {
		t.Fatalf("header payment %+v %v", pay, e)
	}
}
