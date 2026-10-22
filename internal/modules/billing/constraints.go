package billing

import (
	"fmt"

	"gorm.io/gorm"
)

// ConstraintCashPaymentRequiresSession enforces at the database level that a new CASH
// payment carries a cash session (LOT30-A′).
const ConstraintCashPaymentRequiresSession = "billing_payment_cash_requires_session"

// EnsureCashPaymentSessionConstraint adds the CASH ⇒ cash_session_id CHECK as NOT VALID:
// historical sessionless CASH rows are kept untouched (never backfilled) while every new
// or updated row is enforced. Idempotent; schema-relative (search_path).
func EnsureCashPaymentSessionConstraint(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("EnsureCashPaymentSessionConstraint: nil db")
	}
	var n int64
	if err := db.Raw(`SELECT COUNT(*) FROM pg_constraint
		WHERE conname = ? AND conrelid = 'billing_payments'::regclass`, ConstraintCashPaymentRequiresSession).Scan(&n).Error; err != nil {
		return fmt.Errorf("%s lookup: %w", ConstraintCashPaymentRequiresSession, err)
	}
	if n > 0 {
		return nil
	}
	stmt := `ALTER TABLE billing_payments ADD CONSTRAINT ` + ConstraintCashPaymentRequiresSession +
		` CHECK (payment_method <> 'CASH' OR cash_session_id IS NOT NULL) NOT VALID`
	if err := db.Exec(stmt).Error; err != nil {
		return fmt.Errorf("%s: %w", ConstraintCashPaymentRequiresSession, err)
	}
	return nil
}
