package cash

import (
	"testing"
	"time"
)

func TestAssembleSessionSummary_OpenVsClosed(t *testing.T) {
	open := Session{ID: 1, OpeningFloat: 10000, Status: SessionOpen}
	totals := sessionPaymentTotals{Cash: 20000, Card: 30000, Total: 50000, Count: 2}
	got := assembleSessionSummary(open, totals, sessionMovementTotals{})
	if got.ExpectedCash != 30000 || got.CashCollected != 20000 || got.NonCashCollected != 30000 || got.TotalCollected != 50000 {
		t.Fatalf("open summary %+v", got)
	}
	if got.FinalReconciliation || got.ClosingProofComplete {
		t.Fatal("open is not final reconciliation")
	}
}

func TestAssembleSessionSummary_WithMovements(t *testing.T) {
	open := Session{ID: 1, OpeningFloat: 10000, Status: SessionOpen}
	totals := sessionPaymentTotals{Cash: 20000, Total: 20000, Count: 1}
	mov := sessionMovementTotals{In: 5000, Out: 2000, ManualOut: 2000}
	got := assembleSessionSummary(open, totals, mov)
	if got.ExpectedCash != 33000 || got.CashMovementIn != 5000 || got.CashMovementOut != 2000 || got.NetCashMovement != 3000 {
		t.Fatalf("movement summary %+v", got)
	}
	if got.CashMovementManualOut != 2000 || got.CashMovementReversalOut != 0 {
		t.Fatalf("out breakdown %+v", got)
	}
	if got.CashCollected != 20000 || got.TotalCollected != 20000 {
		t.Fatalf("collections must exclude movements %+v", got)
	}
}

func TestAssembleSessionSummary_ClosedSnapshotAndRecovery(t *testing.T) {
	exp, counted, diff := int64(30000), int64(29000), int64(-1000)
	closer := uint(99)
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	closed := Session{
		ID: 2, OpeningFloat: 10000, Status: SessionClosed, OpenedBy: 11,
		ExpectedCashAmount: &exp, CountedCashAmount: &counted, CashDifference: &diff,
		ClosedBy: &closer, ClosedAt: &now, ClosingNote: "écart constaté",
	}
	stale := sessionPaymentTotals{Cash: 99999, Total: 99999, Count: 9}
	got := assembleSessionSummary(closed, stale, sessionMovementTotals{In: 1, Out: 1})
	if got.ExpectedCash != 30000 {
		t.Fatalf("closed expected snapshot got %d", got.ExpectedCash)
	}
	if !got.ClosingProofComplete || !got.FinalReconciliation {
		t.Fatal("complete closed must be final reconciliation")
	}
	if !got.RecoveryClose {
		t.Fatal("recovery close when ClosedBy != OpenedBy")
	}
	if got.VarianceKind != VarianceShortage {
		t.Fatalf("variance %s", got.VarianceKind)
	}
	// Movement totals still projected for report transparency; expected uses snapshot.
	if got.CashMovementIn != 1 || got.CashMovementOut != 1 {
		t.Fatalf("movement totals %+v", got)
	}
}

func TestAssembleSessionSummary_IncompleteClosedNoFakeAuthority(t *testing.T) {
	incomplete := Session{ID: 3, OpeningFloat: 5000, Status: SessionClosed, OpenedBy: 1}
	got := assembleSessionSummary(incomplete, sessionPaymentTotals{Cash: 1000, Total: 1000, Count: 1}, sessionMovementTotals{})
	if got.FinalReconciliation || got.ClosingProofComplete {
		t.Fatal("incomplete must not be final")
	}
	if got.ExpectedCash != 0 {
		t.Fatalf("incomplete must not present live expected as close authority, got %d", got.ExpectedCash)
	}
	if got.VarianceKind != "" {
		t.Fatal("no variance without proof")
	}
}

func TestLiveExpectedCash(t *testing.T) {
	if liveExpectedCash(10000, 20000, 0, 0) != 30000 {
		t.Fatal()
	}
	if liveExpectedCash(10000, 20000, 5000, 2000) != 33000 {
		t.Fatal()
	}
	if liveExpectedCash(0, 0, 0, 0) != 0 {
		t.Fatal()
	}
}

func TestVarianceKind(t *testing.T) {
	if varianceKindFromDiff(0) != VarianceBalanced {
		t.Fatal()
	}
	if varianceKindFromDiff(-1) != VarianceShortage {
		t.Fatal()
	}
	if varianceKindFromDiff(1) != VarianceSurplus {
		t.Fatal()
	}
}
