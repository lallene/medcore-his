package billing

import "gorm.io/gorm"

// EffectivePaidSubquery sums non-reversed patient payments per invoice.
// Used by receivables / finance projections so V1 reversals are authoritative.
const EffectivePaidSubquery = `
SELECT p.invoice_id,
       SUM(p.amount) AS paid,
       MAX(p.paid_at) AS last_payment_at
FROM billing_payments p
LEFT JOIN billing_payment_reversals r ON r.original_payment_id = p.id
WHERE r.id IS NULL
GROUP BY p.invoice_id
`

// EffectivePaidOnInvoice returns the sum of non-reversed payments for an invoice.
func EffectivePaidOnInvoice(tx *gorm.DB, invoiceID uint) (int64, error) {
	var paid int64
	e := tx.Raw(`
		SELECT COALESCE(SUM(p.amount), 0)
		FROM billing_payments p
		LEFT JOIN billing_payment_reversals r ON r.original_payment_id = p.id
		WHERE p.invoice_id = ? AND r.id IS NULL
	`, invoiceID).Scan(&paid).Error
	return paid, e
}

// PaymentHasReversal reports whether a full reversal exists for paymentID.
func PaymentHasReversal(tx *gorm.DB, paymentID uint) (bool, error) {
	var n int64
	e := tx.Model(&PaymentReversal{}).Where("original_payment_id=?", paymentID).Count(&n).Error
	return n > 0, e
}
