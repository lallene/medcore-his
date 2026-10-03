package cash

import "gorm.io/gorm"

// sessionPaymentTotals is the canonical projection of billing_payments attached to a session.
// OperationCount = count of Payment rows with cash_session_id = session (not receipts).
// Opening float is never included in collected totals.
type sessionPaymentTotals struct {
	Cash         int64
	Card         int64
	MobileMoney  int64
	BankTransfer int64
	Check        int64
	Total        int64
	Count        int64
}

// loadSessionPaymentTotals aggregates effective session payments from billing_payments only.
// Sessionless (cash_session_id IS NULL) and other-session payments are excluded by the WHERE.
func loadSessionPaymentTotals(db *gorm.DB, sessionID uint) (sessionPaymentTotals, error) {
	var t sessionPaymentTotals
	rows := []struct {
		Method string
		Total  int64
		Count  int64
	}{}
	if e := db.Table("billing_payments").
		Select("payment_method AS method, COALESCE(SUM(amount),0) AS total, COUNT(*) AS count").
		Where("cash_session_id = ?", sessionID).
		Group("payment_method").
		Scan(&rows).Error; e != nil {
		return t, e
	}
	for _, r := range rows {
		t.Total += r.Total
		t.Count += r.Count
		switch r.Method {
		case "CASH":
			t.Cash = r.Total
		case "CARD":
			t.Card = r.Total
		case "MOBILE_MONEY":
			t.MobileMoney = r.Total
		case "BANK_TRANSFER":
			t.BankTransfer = r.Total
		case "CHECK":
			t.Check = r.Total
		}
	}
	return t, nil
}

// liveExpectedCash is the OPEN-session / pre-close drawer formula (E1):
// OpeningFloat + SUM(CASH payments on session).
func liveExpectedCash(openingFloat, cashCollected int64) int64 {
	return openingFloat + cashCollected
}

// assembleSessionSummary builds the single authoritative SessionSummary.
// For CLOSED sessions, ExpectedCash uses the persisted closing snapshot (immutable proof).
// Collection totals remain derived from payment rows (post-close mutation is blocked by product rules).
func assembleSessionSummary(session Session, t sessionPaymentTotals) SessionSummary {
	z := SessionSummary{
		Session:              session,
		CashCollected:        t.Cash,
		NonCashCollected:     t.Total - t.Cash,
		TotalCollected:       t.Total,
		CashPayments:         t.Cash,
		CardPayments:         t.Card,
		MobileMoneyPayments:  t.MobileMoney,
		BankTransferPayments: t.BankTransfer,
		CheckPayments:        t.Check,
		TotalPayments:        t.Total,
		OperationCount:       t.Count,
	}
	if session.Status == SessionClosed && session.ExpectedCashAmount != nil {
		z.ExpectedCash = *session.ExpectedCashAmount
	} else {
		z.ExpectedCash = liveExpectedCash(session.OpeningFloat, t.Cash)
	}
	return z
}
