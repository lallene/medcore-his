package billing

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"gorm.io/gorm"
)

// localSessionlessReceiptIssuer creates canonical cash_receipts rows for billing tests
// (avoids importing cash → billing cycle).
func localSessionlessReceiptIssuer(tx *gorm.DB, payment *Payment, invoice *Invoice, paidBefore, balanceAfter int64, user uint) error {
	if payment == nil || invoice == nil {
		return fmt.Errorf("receipt issue requires payment and invoice")
	}
	var prior billingCashReceipt
	if e := tx.Where("payment_id=?", payment.ID).First(&prior).Error; e == nil {
		return nil
	} else if !errors.Is(e, gorm.ErrRecordNotFound) {
		return e
	}
	var patient struct{ Nom, Prenoms, CodePatient string }
	if e := tx.Table("patients").Select("nom,prenoms,code_patient").Where("id=?", invoice.PatientID).Scan(&patient).Error; e != nil {
		return e
	}
	var cashier struct{ Name string }
	_ = tx.Table("users").Select("name").Where("id=?", user).Scan(&cashier)
	name := strings.TrimSpace(cashier.Name)
	if name == "" {
		name = fmt.Sprintf("Utilisateur #%d", user)
	}
	rec := billingCashReceipt{
		ReceiptNumber:      fmt.Sprintf("TMP-%d", time.Now().UnixNano()),
		PaymentID:          payment.ID,
		InvoiceID:          invoice.ID,
		PatientID:          invoice.PatientID,
		CashSessionID:      nil,
		Amount:             payment.Amount,
		PaymentMethod:      payment.PaymentMethod,
		ExternalReference:  payment.Reference,
		MobileOperator:     payment.MobileOperator,
		IssuedBy:           user,
		IssuedAt:           time.Now(),
		InvoiceNumber:      invoice.Number,
		PatientName:        strings.TrimSpace(patient.Prenoms + " " + patient.Nom),
		PatientCode:        patient.CodePatient,
		CashierName:        name,
		RegisterCode:       "",
		RegisterName:       "",
		InvoiceGrossAmount: invoice.GrossAmount,
		InsuranceAmount:    invoice.InsuranceAmount,
		PatientAmount:      invoice.PatientAmount,
		PaidBefore:         paidBefore,
		BalanceAfter:       balanceAfter,
	}
	if e := tx.Exec("SAVEPOINT cash_receipt_idempotency").Error; e != nil {
		return e
	}
	if e := tx.Create(&rec).Error; e != nil {
		_ = tx.Exec("ROLLBACK TO SAVEPOINT cash_receipt_idempotency").Error
		msg := strings.ToLower(e.Error())
		if strings.Contains(msg, "duplicate") || strings.Contains(msg, "unique") || strings.Contains(msg, "23505") {
			return nil
		}
		return e
	}
	if e := tx.Exec("RELEASE SAVEPOINT cash_receipt_idempotency").Error; e != nil {
		return e
	}
	return tx.Model(&rec).Update("receipt_number", fmt.Sprintf("REC-%06d", rec.ID)).Error
}

func receiptBilling(t *testing.T, db *gorm.DB) *Service {
	t.Helper()
	return NewService(db).WithReceiptIssuer(localSessionlessReceiptIssuer)
}

func receiptCount(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var n int64
	if e := db.Table("cash_receipts").Count(&n).Error; e != nil {
		t.Fatal(e)
	}
	return n
}

func receiptByPayment(t *testing.T, db *gorm.DB, paymentID uint) billingCashReceipt {
	t.Helper()
	var r billingCashReceipt
	if e := db.Where("payment_id=?", paymentID).First(&r).Error; e != nil {
		t.Fatal(e)
	}
	return r
}

func TestLOT29D_B_BillingReceiptMatrix(t *testing.T) {
	t.Run("RB01_RB05_billing_payment_creates_canonical_receipt", func(t *testing.T) {
		db := billingDB(t)
		db.Create(&billingUser{ID: 41, Name: "Caissier Billing"})
		s := receiptBilling(t, db)
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		out, e := s.Pay(inv.ID, PaymentRequest{Amount: 5000, PaymentMethod: "CARD", Reference: "REF-RB01", IdempotencyKey: "rb01", Payer: PatientPayerRequest()}, 41)
		if e != nil {
			t.Fatal(e)
		}
		if len(out.Payments) != 1 || out.Payments[0].ReceiptID == nil || out.Payments[0].ReceiptNumber == "" {
			t.Fatalf("RB01 missing receipt on payment %+v", out.Payments)
		}
		pay := out.Payments[0]
		rec := receiptByPayment(t, db, pay.ID)
		if rec.PaymentID != pay.ID {
			t.Fatalf("RB02 payment_id=%d want %d", rec.PaymentID, pay.ID)
		}
		if rec.Amount != pay.Amount || rec.Amount != 5000 {
			t.Fatalf("RB03 amount=%d", rec.Amount)
		}
		if rec.PaymentMethod != "CARD" || rec.PaymentMethod != pay.PaymentMethod {
			t.Fatalf("RB04 method=%s", rec.PaymentMethod)
		}
		if rec.CashSessionID != nil || rec.RegisterCode != "" || rec.RegisterName != "" {
			t.Fatalf("RB05 sessionless violated %+v", rec)
		}
		if rec.IssuedBy != 41 || rec.CashierName != "Caissier Billing" || rec.ExternalReference != "REF-RB01" {
			t.Fatalf("RB05 actor/snapshot %+v", rec)
		}
		if !strings.HasPrefix(rec.ReceiptNumber, "REC-") {
			t.Fatalf("RB01 number=%s", rec.ReceiptNumber)
		}
	})

	t.Run("RB06_RB07_payment_replay_same_receipt", func(t *testing.T) {
		db := billingDB(t)
		db.Create(&billingUser{ID: 42, Name: "Replay"})
		s := receiptBilling(t, db)
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		req := PaymentRequest{Amount: 3000, PaymentMethod: "CASH", IdempotencyKey: "rb06", Payer: PatientPayerRequest()}
		first, e := s.Pay(inv.ID, req, 42)
		if e != nil {
			t.Fatal(e)
		}
		second, e := s.Pay(inv.ID, req, 42)
		if e != nil {
			t.Fatal(e)
		}
		if first.Payments[0].ID != second.Payments[0].ID {
			t.Fatal("RB06 different payment")
		}
		if *first.Payments[0].ReceiptID != *second.Payments[0].ReceiptID {
			t.Fatal("RB06 different receipt id")
		}
		if first.Payments[0].ReceiptNumber != second.Payments[0].ReceiptNumber {
			t.Fatal("RB06 different receipt number")
		}
		if receiptCount(t, db) != 1 || paymentCount(t, db, inv.ID) != 1 {
			t.Fatalf("RB07 payments/receipts duplicated")
		}
	})

	t.Run("RB08_concurrent_ensure_same_payment_one_receipt", func(t *testing.T) {
		db := billingDB(t)
		db.Create(&billingUser{ID: 43, Name: "Conc"})
		bare := NewService(db).WithReceiptIssuer(nil)
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, bare, p, c, tariffID)
		out, e := bare.Pay(inv.ID, PaymentRequest{Amount: 2000, PaymentMethod: "CASH", IdempotencyKey: "rb08-pay", Payer: PatientPayerRequest()}, 43)
		if e != nil {
			t.Fatal(e)
		}
		payID := out.Payments[0].ID
		var pay Payment
		if e := db.First(&pay, payID).Error; e != nil {
			t.Fatal(e)
		}
		var invRow Invoice
		if e := db.First(&invRow, inv.ID).Error; e != nil {
			t.Fatal(e)
		}
		start := make(chan struct{})
		errs := make(chan error, 8)
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				errs <- db.Transaction(func(tx *gorm.DB) error {
					return localSessionlessReceiptIssuer(tx, &pay, &invRow, 0, invRow.BalanceAmount, 43)
				})
			}()
		}
		close(start)
		wg.Wait()
		close(errs)
		for e := range errs {
			if e != nil {
				t.Fatal(e)
			}
		}
		if receiptCount(t, db) != 1 {
			t.Fatalf("RB08 receipts=%d", receiptCount(t, db))
		}
		rec := receiptByPayment(t, db, payID)
		if rec.PaymentID != payID {
			t.Fatal("RB08 payment link")
		}
	})

	t.Run("RB09_two_payments_two_receipts", func(t *testing.T) {
		db := billingDB(t)
		db.Create(&billingUser{ID: 44, Name: "Two"})
		s := receiptBilling(t, db)
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		a, e := s.Pay(inv.ID, PaymentRequest{Amount: 4000, PaymentMethod: "CASH", IdempotencyKey: "rb09a", Payer: PatientPayerRequest()}, 44)
		if e != nil {
			t.Fatal(e)
		}
		b, e := s.Pay(inv.ID, PaymentRequest{Amount: 6000, PaymentMethod: "CARD", IdempotencyKey: "rb09b", Payer: PatientPayerRequest()}, 44)
		if e != nil {
			t.Fatal(e)
		}
		if receiptCount(t, db) != 2 {
			t.Fatalf("RB09 receipts=%d", receiptCount(t, db))
		}
		if len(b.Payments) < 2 {
			t.Fatalf("RB09 payments=%d", len(b.Payments))
		}
		idA, idB := *a.Payments[0].ReceiptID, *b.Payments[1].ReceiptID
		if idA == idB {
			t.Fatal("RB09 same receipt for different payments")
		}
		if a.Payments[0].ReceiptNumber == b.Payments[1].ReceiptNumber {
			t.Fatal("RB09 same receipt number")
		}
	})

	t.Run("RB10_partial_payment_receipt_amount", func(t *testing.T) {
		db := billingDB(t)
		db.Create(&billingUser{ID: 45, Name: "Partial"})
		s := receiptBilling(t, db)
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		out, e := s.Pay(inv.ID, PaymentRequest{Amount: 7000, PaymentMethod: "CASH", IdempotencyKey: "rb10", Payer: PatientPayerRequest()}, 45)
		if e != nil || out.Status != InvoicePartiallyPaid {
			t.Fatalf("RB10 %+v %v", out, e)
		}
		rec := receiptByPayment(t, db, out.Payments[0].ID)
		if rec.Amount != 7000 || rec.BalanceAfter != out.BalanceAmount || rec.PaidBefore != 0 {
			t.Fatalf("RB10 receipt %+v outBal=%d", rec, out.BalanceAmount)
		}
	})

	t.Run("RB11_final_payment_receipt_amount", func(t *testing.T) {
		db := billingDB(t)
		db.Create(&billingUser{ID: 46, Name: "Final"})
		s := receiptBilling(t, db)
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		if _, e := s.Pay(inv.ID, PaymentRequest{Amount: 5000, PaymentMethod: "CASH", IdempotencyKey: "rb11a", Payer: PatientPayerRequest()}, 46); e != nil {
			t.Fatal(e)
		}
		out, e := s.Pay(inv.ID, PaymentRequest{Amount: 15000, PaymentMethod: "CASH", IdempotencyKey: "rb11b", Payer: PatientPayerRequest()}, 46)
		if e != nil || out.Status != InvoicePaid || out.BalanceAmount != 0 {
			t.Fatalf("RB11 %+v %v", out, e)
		}
		final := out.Payments[len(out.Payments)-1]
		rec := receiptByPayment(t, db, final.ID)
		if rec.Amount != 15000 || rec.BalanceAfter != 0 || rec.PaidBefore != 5000 {
			t.Fatalf("RB11 receipt %+v", rec)
		}
		if rec.InvoiceGrossAmount != inv.GrossAmount {
			t.Fatalf("RB11 must not confuse receipt total with invoice gross")
		}
	})

	t.Run("RB12_receipt_failure_rolls_back_payment", func(t *testing.T) {
		db := billingDB(t)
		s := NewService(db).WithReceiptIssuer(func(tx *gorm.DB, payment *Payment, invoice *Invoice, paidBefore, balanceAfter int64, user uint) error {
			return errors.New("injected-receipt-failure")
		})
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		beforePaid, beforeBal := inv.PaidAmount, inv.BalanceAmount
		_, e := s.Pay(inv.ID, PaymentRequest{Amount: 4000, PaymentMethod: "CASH", IdempotencyKey: "rb12", Payer: PatientPayerRequest()}, 47)
		if e == nil {
			t.Fatal("RB12 expected failure")
		}
		var fresh Invoice
		if e := db.First(&fresh, inv.ID).Error; e != nil {
			t.Fatal(e)
		}
		if fresh.PaidAmount != beforePaid || fresh.BalanceAmount != beforeBal {
			t.Fatalf("RB12 not rolled back %+v", fresh)
		}
		if paymentCount(t, db, inv.ID) != 0 || receiptCount(t, db) != 0 {
			t.Fatal("RB12 leaked payment/receipt")
		}
	})

	t.Run("RB14_reprint_does_not_create_receipt", func(t *testing.T) {
		db := billingDB(t)
		db.Create(&billingUser{ID: 48, Name: "Print"})
		s := receiptBilling(t, db)
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		out, e := s.Pay(inv.ID, PaymentRequest{Amount: 1000, PaymentMethod: "CASH", IdempotencyKey: "rb14", Payer: PatientPayerRequest()}, 48)
		if e != nil {
			t.Fatal(e)
		}
		id := *out.Payments[0].ReceiptID
		again, e := s.GetInvoice(inv.ID)
		if e != nil || again.Payments[0].ReceiptID == nil || *again.Payments[0].ReceiptID != id {
			t.Fatalf("RB14 read %+v %v", again, e)
		}
		if receiptCount(t, db) != 1 {
			t.Fatal("RB14 created receipt on read")
		}
	})

	t.Run("RB15_receipt_read_requires_cash_receipt_read", func(t *testing.T) {
		// Contract: CAISSIER has cash.receipt.read; billing.payment.create alone is not receipt-read.
		caissier := rbacHas("CAISSIER", "cash.receipt.read")
		payOnly := rbacHas("FACTURATION", "cash.receipt.read")
		if !caissier {
			t.Fatal("RB15 CAISSIER must retain cash.receipt.read")
		}
		if payOnly {
			t.Fatal("RB15 FACTURATION must not gain cash.receipt.read implicitly")
		}
	})

	t.Run("RB16_receipt_no_clinical_fields", func(t *testing.T) {
		db := billingDB(t)
		db.Create(&billingUser{ID: 50, Name: "PHI"})
		s := receiptBilling(t, db)
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		out, e := s.Pay(inv.ID, PaymentRequest{Amount: 1000, PaymentMethod: "CASH", IdempotencyKey: "rb16", Payer: PatientPayerRequest()}, 50)
		if e != nil {
			t.Fatal(e)
		}
		rec := receiptByPayment(t, db, out.Payments[0].ID)
		rows, e := db.Raw("SELECT * FROM cash_receipts WHERE id=?", rec.ID).Rows()
		if e != nil {
			t.Fatal(e)
		}
		defer rows.Close()
		cols, _ := rows.Columns()
		joined := strings.ToLower(strings.Join(cols, ","))
		for _, bad := range []string{"diagnosis", "clinical", "motif", "timeline"} {
			if strings.Contains(joined, bad) {
				t.Fatalf("RB16 clinical column %s in %s", bad, joined)
			}
		}
	})

	t.Run("RB17_legacy_no_receipt_untouched", func(t *testing.T) {
		db := billingDB(t)
		s := NewService(db).WithReceiptIssuer(nil)
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		out, e := s.Pay(inv.ID, PaymentRequest{Amount: 2500, PaymentMethod: "CASH", IdempotencyKey: "rb17", Payer: PatientPayerRequest()}, 51)
		if e != nil {
			t.Fatal(e)
		}
		if receiptCount(t, db) != 0 {
			t.Fatal("RB17 unexpected backfill on pay without issuer")
		}
		got, e := s.GetInvoice(inv.ID)
		if e != nil {
			t.Fatal(e)
		}
		if got.Payments[0].ReceiptID != nil || got.Payments[0].ReceiptNumber != "" {
			t.Fatalf("RB17 GetInvoice fabricated receipt %+v", got.Payments[0])
		}
		_ = out
	})
}

func rbacHas(function, perm string) bool {
	for _, p := range rbac.EffectiveStaffPermissions("staff", []string{function}, nil) {
		if p == perm {
			return true
		}
	}
	return false
}
