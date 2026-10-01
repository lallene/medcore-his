package main

import (
	"fmt"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/billing"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestResolveDemoPaymentIdempotencyKey_RestoresWhenBaseKeyStolen(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:demo_pay_idem?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&billing.Invoice{}, &billing.Payment{}); err != nil {
		t.Fatal(err)
	}

	foreign := billing.Invoice{Number: "INV-FOREIGN", PatientID: 1, Status: billing.InvoicePaid, PatientAmount: 20000, PaidAmount: 20000, BalanceAmount: 0, CreatedBy: 1, UpdatedBy: 1}
	if err := db.Create(&foreign).Error; err != nil {
		t.Fatal(err)
	}
	target := billing.Invoice{Number: "INV-TARGET", PatientID: 10, Status: billing.InvoiceIssued, PatientAmount: 20000, PaidAmount: 0, BalanceAmount: 20000, CreatedBy: 1, UpdatedBy: 1}
	if err := db.Create(&target).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&billing.Payment{
		InvoiceID: foreign.ID, Amount: 20000, PaymentMethod: "CASH",
		IdempotencyKey: "DEMO-RECEIVABLE-D-PAY-20K", ReceivedBy: 1, PaidAt: time.Now(),
	}).Error; err != nil {
		t.Fatal(err)
	}

	key, skip, err := resolveDemoPaymentIdempotencyKey(db, target, "DEMO-RECEIVABLE-D-PAY-20K")
	if err != nil {
		t.Fatal(err)
	}
	if skip {
		t.Fatal("must not skip unpaid target when base key belongs to another invoice")
	}
	want := fmt.Sprintf("DEMO-RECEIVABLE-D-PAY-20K:invoice:%d", target.ID)
	if key != want {
		t.Fatalf("key=%q want %q", key, want)
	}

	// After scoped payment exists, subsequent resolve must skip.
	if err := db.Create(&billing.Payment{
		InvoiceID: target.ID, Amount: 20000, PaymentMethod: "CASH",
		IdempotencyKey: key, ReceivedBy: 1, PaidAt: time.Now(),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&target).Updates(map[string]any{
		"status": billing.InvoicePaid, "paid_amount": 20000, "balance_amount": 0,
	}).Error; err != nil {
		t.Fatal(err)
	}
	_, skip, err = resolveDemoPaymentIdempotencyKey(db, target, "DEMO-RECEIVABLE-D-PAY-20K")
	if err != nil {
		t.Fatal(err)
	}
	if !skip {
		t.Fatal("paid target must skip")
	}
}

func TestResolveDemoPaymentIdempotencyKey_SameInvoiceSkips(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:demo_pay_same?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&billing.Invoice{}, &billing.Payment{}); err != nil {
		t.Fatal(err)
	}
	inv := billing.Invoice{Number: "INV-OK", PatientID: 10, Status: billing.InvoiceIssued, PatientAmount: 20000, PaidAmount: 0, BalanceAmount: 20000, CreatedBy: 1, UpdatedBy: 1}
	if err := db.Create(&inv).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&billing.Payment{
		InvoiceID: inv.ID, Amount: 20000, PaymentMethod: "CASH",
		IdempotencyKey: "DEMO-KEY", ReceivedBy: 1, PaidAt: time.Now(),
	}).Error; err != nil {
		t.Fatal(err)
	}
	_, skip, err := resolveDemoPaymentIdempotencyKey(db, inv, "DEMO-KEY")
	if err != nil {
		t.Fatal(err)
	}
	if !skip {
		t.Fatal("same-invoice base key must skip")
	}
}
