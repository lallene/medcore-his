package main

import (
	"errors"
	"fmt"

	"github.com/lallene/medcore-his/backend/internal/modules/billing"
	"gorm.io/gorm"
)

// resolveDemoPaymentIdempotencyKey chooses a cash idempotency key for a demo
// fixture payment so reseeding remains state-restoring for named invoices.
//
// Historical payOnce treated the base key as globally unique and returned early
// even when the key had been consumed by a different invoice — leaving fixtures
// like P-DEMO-010 (DEMO-RECEIVABLE-D-PAID) ISSUED/unpaid after --demo-full.
func resolveDemoPaymentIdempotencyKey(db *gorm.DB, invoice billing.Invoice, baseKey string) (key string, skip bool, err error) {
	if invoice.ID == 0 || baseKey == "" {
		return "", true, nil
	}
	var current billing.Invoice
	if err := db.First(&current, invoice.ID).Error; err != nil {
		return "", false, err
	}
	if current.Status == billing.InvoicePaid || current.Status == billing.InvoiceCancelled || current.BalanceAmount <= 0 {
		return "", true, nil
	}

	var prior billing.Payment
	err = db.Where("idempotency_key = ?", baseKey).First(&prior).Error
	if err == nil {
		if prior.InvoiceID == current.ID {
			return "", true, nil
		}
		scoped := fmt.Sprintf("%s:invoice:%d", baseKey, current.ID)
		var scopedPrior billing.Payment
		scopedErr := db.Where("idempotency_key = ?", scoped).First(&scopedPrior).Error
		if scopedErr == nil {
			return "", true, nil
		}
		if scopedErr != nil && !errors.Is(scopedErr, gorm.ErrRecordNotFound) {
			return "", false, scopedErr
		}
		return scoped, false, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", false, err
	}
	return baseKey, false, nil
}
