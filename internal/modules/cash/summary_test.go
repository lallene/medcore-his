package cash

import "testing"

func TestAssembleSessionSummary_OpenVsClosed(t *testing.T) {
	open := Session{ID: 1, OpeningFloat: 10000, Status: SessionOpen}
	totals := sessionPaymentTotals{Cash: 20000, Card: 30000, Total: 50000, Count: 2}
	got := assembleSessionSummary(open, totals)
	if got.ExpectedCash != 30000 || got.CashCollected != 20000 || got.NonCashCollected != 30000 || got.TotalCollected != 50000 {
		t.Fatalf("open summary %+v", got)
	}
	if got.TotalPayments != got.TotalCollected || got.CashPayments != got.CashCollected {
		t.Fatal("compat mirrors")
	}

	exp, counted, diff := int64(30000), int64(29000), int64(-1000)
	closed := Session{
		ID: 2, OpeningFloat: 10000, Status: SessionClosed,
		ExpectedCashAmount: &exp, CountedCashAmount: &counted, CashDifference: &diff,
	}
	// Even if payment totals were somehow different, CLOSED expected uses snapshot.
	stale := sessionPaymentTotals{Cash: 99999, Total: 99999, Count: 9}
	gotClosed := assembleSessionSummary(closed, stale)
	if gotClosed.ExpectedCash != 30000 {
		t.Fatalf("closed expected snapshot got %d", gotClosed.ExpectedCash)
	}
}

func TestLiveExpectedCash(t *testing.T) {
	if liveExpectedCash(10000, 20000) != 30000 {
		t.Fatal()
	}
	if liveExpectedCash(0, 0) != 0 {
		t.Fatal()
	}
}
