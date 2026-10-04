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

type sessionMovementTotals struct {
	In                     int64
	Out                    int64
	ManualOut              int64
	ReversalOut            int64
	PostCloseCorrectionOut int64
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

func loadSessionMovementTotals(db *gorm.DB, sessionID uint) (sessionMovementTotals, error) {
	var m sessionMovementTotals
	rows := []struct {
		Direction string
		Type      string
		Total     int64
	}{}
	if e := db.Table("cash_movements").
		Select("direction, type, COALESCE(SUM(amount),0) AS total").
		Where("cash_session_id = ?", sessionID).
		Group("direction, type").
		Scan(&rows).Error; e != nil {
		return m, e
	}
	for _, r := range rows {
		switch r.Direction {
		case MovementIn:
			m.In += r.Total
		case MovementOut:
			m.Out += r.Total
			switch r.Type {
			case MovementManualOut:
				m.ManualOut += r.Total
			case MovementPaymentReversal:
				m.ReversalOut += r.Total
			case MovementPostCloseCorrectionOut:
				m.PostCloseCorrectionOut += r.Total
			}
		}
	}
	return m, nil
}

// liveExpectedCash is the OPEN-session / pre-close drawer formula:
// OpeningFloat + SUM(gross CASH payments, including reversed) + IN − OUT.
// LOT29F-D: reversed CASH payments remain in CashCollected; PAYMENT_REVERSAL OUT
// neutralizes drawer effect exactly once (do not exclude reversed payments from CashCollected).
func liveExpectedCash(openingFloat, cashCollected, movementIn, movementOut int64) int64 {
	return openingFloat + cashCollected + movementIn - movementOut
}

// closingProofComplete reports whether a CLOSED session has a full immutable close snapshot.
func closingProofComplete(session Session) bool {
	return session.Status == SessionClosed &&
		session.ExpectedCashAmount != nil &&
		session.CountedCashAmount != nil &&
		session.CashDifference != nil &&
		session.ClosedBy != nil &&
		session.ClosedAt != nil
}

func varianceKindFromDiff(diff int64) string {
	switch {
	case diff == 0:
		return VarianceBalanced
	case diff < 0:
		return VarianceShortage
	default:
		return VarianceSurplus
	}
}

// assembleSessionSummary builds the single authoritative SessionSummary.
// For CLOSED sessions with a complete snapshot, ExpectedCash uses persisted ExpectedCashAmount.
// Incomplete legacy CLOSED rows do not fabricate a closing expected as authority.
func assembleSessionSummary(session Session, t sessionPaymentTotals, m sessionMovementTotals) SessionSummary {
	z := SessionSummary{
		Session:                            session,
		CashCollected:                      t.Cash,
		NonCashCollected:                   t.Total - t.Cash,
		TotalCollected:                     t.Total,
		CashPayments:                       t.Cash,
		CardPayments:                       t.Card,
		MobileMoneyPayments:                t.MobileMoney,
		BankTransferPayments:               t.BankTransfer,
		CheckPayments:                      t.Check,
		TotalPayments:                      t.Total,
		OperationCount:                     t.Count,
		CashMovementIn:                     m.In,
		CashMovementOut:                    m.Out,
		NetCashMovement:                    m.In - m.Out,
		CashMovementManualOut:              m.ManualOut,
		CashMovementReversalOut:            m.ReversalOut,
		CashMovementPostCloseCorrectionOut: m.PostCloseCorrectionOut,
	}
	if session.Status == SessionClosed {
		z.ClosingProofComplete = closingProofComplete(session)
		z.FinalReconciliation = z.ClosingProofComplete
		if session.ClosedBy != nil && *session.ClosedBy != session.OpenedBy {
			z.RecoveryClose = true
		}
		if session.ExpectedCashAmount != nil {
			z.ExpectedCash = *session.ExpectedCashAmount
		}
		// Incomplete: leave ExpectedCash at 0 and FinalReconciliation=false — FE must not treat as proof.
		if z.ClosingProofComplete && session.CashDifference != nil {
			z.VarianceKind = varianceKindFromDiff(*session.CashDifference)
		}
		return z
	}
	z.ExpectedCash = liveExpectedCash(session.OpeningFloat, t.Cash, m.In, m.Out)
	return z
}
