package billing

import (
	"errors"
	"strings"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"gorm.io/gorm"
)

const (
	// MaxPaymentIdempotencyKeyLen matches billing_payments.idempotency_key column size.
	MaxPaymentIdempotencyKeyLen = 120
)

// NormalizePaymentIdempotencyKey validates a client payment-command key.
// Empty / oversized keys are rejected. Keys must not be derived from PHI.
func NormalizePaymentIdempotencyKey(raw string) (string, error) {
	key := strings.TrimSpace(raw)
	if key == "" {
		return "", coreerrors.BadRequest("Clé d'idempotence obligatoire")
	}
	if len(key) > MaxPaymentIdempotencyKeyLen {
		return "", coreerrors.BadRequest("Clé d'idempotence trop longue")
	}
	return key, nil
}

func paymentFingerprintMatch(p Payment, invoiceID uint, amount int64, method, reference, mobileOperator string) bool {
	return p.InvoiceID == invoiceID &&
		p.Amount == amount &&
		p.PaymentMethod == method &&
		p.Reference == reference &&
		p.MobileOperator == mobileOperator
}

func paymentFingerprintConflict(p Payment, invoiceID uint, amount int64, method, reference, mobileOperator string) error {
	if paymentFingerprintMatch(p, invoiceID, amount, method, reference, mobileOperator) {
		return nil
	}
	return coreerrors.Conflict("Clé d'idempotence déjà utilisée")
}

func isPaymentIdempotencyUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, fragment := range []string{
		"duplicate key",
		"unique constraint",
		"sqlstate 23505",
		"idx_billing_payments_idempotency_key",
		"billing_payments_idempotency_key",
	} {
		if strings.Contains(msg, fragment) {
			return true
		}
	}
	return false
}
