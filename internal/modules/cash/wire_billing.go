package cash

import "github.com/lallene/medcore-his/backend/internal/modules/billing"

func init() {
	// LOT29D-B: billing.Pay issues canonical cash_receipts without inventing a cash session.
	billing.RegisterReceiptIssuer(IssueBillingReceipt)
}
