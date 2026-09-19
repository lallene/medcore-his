package patient_queue

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

// LOT 26C / F24-10A — SetPriority mandatory ExpectedVersion OCC.
// F24-10B — Priority + Version + PRIORITY history remain atomic.

func priorityActor(uid uint) Access {
	return Access{
		UserID: uid,
		Permissions: map[string]bool{
			"queue.priority.update": true,
			"queue.read.all":        true,
		},
	}
}

// TestPostgresF2410A_SetPriorityConcurrentOCC: two writers with the same ExpectedVersion —
// exactly one success, one CONFLICT; Version bumps once; one PRIORITY history row.
func TestPostgresF2410A_SetPriorityConcurrentOCC(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	admin := adminAccess(100)

	tk, err := svc.CheckInWalkIn(WalkInCheckInRequest{
		PatientID: 1, ServiceID: 10, IdentityConfirmed: true, Priority: PriorityNormal,
		Reason: "f2410a-occ",
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	startVersion := tk.Version
	if startVersion < 1 {
		t.Fatalf("start Version=%d", startVersion)
	}

	actorA := priorityActor(24101)
	actorB := priorityActor(24102)
	_ = db.Exec(`INSERT INTO users(id, name) VALUES (24101,'F2410A-A'),(24102,'F2410A-B') ON CONFLICT DO NOTHING`)

	var okN, conflictN int32
	var winnerPriority atomic.Value
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		out, e := svc.SetPriority(tk.ID, PriorityRequest{
			Priority: PriorityUrgent, Reason: "A", ExpectedVersion: startVersion,
		}, actorA)
		if e == nil {
			atomic.AddInt32(&okN, 1)
			winnerPriority.Store(out.Priority)
		} else if statusOf(e) == 409 {
			atomic.AddInt32(&conflictN, 1)
		} else {
			t.Errorf("A unexpected error: %v", e)
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		out, e := svc.SetPriority(tk.ID, PriorityRequest{
			Priority: PriorityHigh, Reason: "B", ExpectedVersion: startVersion,
		}, actorB)
		if e == nil {
			atomic.AddInt32(&okN, 1)
			winnerPriority.Store(out.Priority)
		} else if statusOf(e) == 409 {
			atomic.AddInt32(&conflictN, 1)
		} else {
			t.Errorf("B unexpected error: %v", e)
		}
	}()
	close(start)
	wg.Wait()

	if okN != 1 || conflictN != 1 {
		t.Fatalf("OCC race: ok=%d conflict=%d want 1/1", okN, conflictN)
	}

	var after Ticket
	if err := db.First(&after, tk.ID).Error; err != nil {
		t.Fatal(err)
	}
	if after.Version != startVersion+1 {
		t.Fatalf("Version=%d want %d", after.Version, startVersion+1)
	}
	wp, _ := winnerPriority.Load().(string)
	if after.Priority != wp || (after.Priority != PriorityUrgent && after.Priority != PriorityHigh) {
		t.Fatalf("final Priority=%s winner=%s", after.Priority, wp)
	}

	var priorityEvents int64
	if err := db.Model(&History{}).
		Where("ticket_id=? AND event_type=?", tk.ID, "PRIORITY").
		Count(&priorityEvents).Error; err != nil {
		t.Fatal(err)
	}
	if priorityEvents != 1 {
		t.Fatalf("PRIORITY history rows=%d want 1", priorityEvents)
	}
}

// TestPostgresF2410A_SetPriorityOCCStaleAndSamePriority covers stale conflict + same-priority mutation.
func TestPostgresF2410A_SetPriorityOCCStaleAndSamePriority(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	admin := adminAccess(100)

	tk, err := svc.CheckInWalkIn(WalkInCheckInRequest{
		PatientID: 1, ServiceID: 10, IdentityConfirmed: true, Priority: PriorityNormal,
		Reason: "f2410a-stale",
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	actor := priorityActor(24105)
	_ = db.Exec(`INSERT INTO users(id, name) VALUES (24105,'F2410A-STALE') ON CONFLICT DO NOTHING`)

	if _, err := svc.SetPriority(tk.ID, PriorityRequest{
		Priority: PriorityUrgent, Reason: "first", ExpectedVersion: 0,
	}, actor); statusOf(err) != 400 {
		t.Fatalf("expectedVersion<1 want 400 got %d (%v)", statusOf(err), err)
	}

	out, err := svc.SetPriority(tk.ID, PriorityRequest{
		Priority: PriorityUrgent, Reason: "first", ExpectedVersion: tk.Version,
	}, actor)
	if err != nil {
		t.Fatal(err)
	}
	if out.Version != tk.Version+1 || out.Priority != PriorityUrgent {
		t.Fatalf("success: %+v", out)
	}

	var histBefore int64
	db.Model(&History{}).Where("ticket_id=? AND event_type=?", tk.ID, "PRIORITY").Count(&histBefore)

	_, err = svc.SetPriority(tk.ID, PriorityRequest{
		Priority: PriorityHigh, Reason: "stale", ExpectedVersion: tk.Version,
	}, actor)
	if statusOf(err) != 409 {
		t.Fatalf("stale want 409 got %d (%v)", statusOf(err), err)
	}

	var afterStale Ticket
	if err := db.First(&afterStale, tk.ID).Error; err != nil {
		t.Fatal(err)
	}
	if afterStale.Priority != PriorityUrgent || afterStale.Version != out.Version {
		t.Fatalf("stale mutated ticket: Priority=%s Version=%d", afterStale.Priority, afterStale.Version)
	}
	var histAfterStale int64
	db.Model(&History{}).Where("ticket_id=? AND event_type=?", tk.ID, "PRIORITY").Count(&histAfterStale)
	if histAfterStale != histBefore {
		t.Fatalf("stale created history: before=%d after=%d", histBefore, histAfterStale)
	}

	same, err := svc.SetPriority(tk.ID, PriorityRequest{
		Priority: PriorityUrgent, Reason: "same", ExpectedVersion: afterStale.Version,
	}, actor)
	if err != nil {
		t.Fatal(err)
	}
	if same.Priority != PriorityUrgent || same.Version != afterStale.Version+1 {
		t.Fatalf("same-priority: Priority=%s Version=%d", same.Priority, same.Version)
	}
	var histAfterSame int64
	db.Model(&History{}).Where("ticket_id=? AND event_type=?", tk.ID, "PRIORITY").Count(&histAfterSame)
	if histAfterSame != histBefore+1 {
		t.Fatalf("same-priority history rows=%d want %d", histAfterSame, histBefore+1)
	}
}

// TestPostgresF2410B_SetPriorityHistoryFailureRollsBackPriority proves F24-10B GREEN:
// when PRIORITY history INSERT fails inside the transaction, SetPriority errors and
// Priority/Version remain unchanged with no PRIORITY history row.
//
// Failure mechanism: ephemeral-schema CHECK rejecting event_type=PRIORITY.
func TestPostgresF2410B_SetPriorityHistoryFailureRollsBackPriority(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	admin := adminAccess(100)

	tk, err := svc.CheckInWalkIn(WalkInCheckInRequest{
		PatientID: 1, ServiceID: 10, IdentityConfirmed: true, Priority: PriorityNormal,
		Reason: "f2410b",
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	startVersion := tk.Version
	startPriority := tk.Priority

	const constraint = "f2410b_block_priority_history"
	if err := db.Exec(fmt.Sprintf(
		`ALTER TABLE patient_queue_history ADD CONSTRAINT %s CHECK (event_type <> 'PRIORITY')`,
		constraint,
	)).Error; err != nil {
		t.Fatalf("install history failure constraint: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Exec(fmt.Sprintf(`ALTER TABLE patient_queue_history DROP CONSTRAINT IF EXISTS %s`, constraint))
	})

	actor := priorityActor(24103)
	_ = db.Exec(`INSERT INTO users(id, name) VALUES (24103,'F2410B') ON CONFLICT DO NOTHING`)

	_, err = svc.SetPriority(tk.ID, PriorityRequest{
		Priority: PriorityUrgent, Reason: "audit-fail", ExpectedVersion: tk.Version,
	}, actor)
	if err == nil {
		t.Fatal("SetPriority must return error when PRIORITY history insert fails")
	}

	var persisted Ticket
	if err := db.First(&persisted, tk.ID).Error; err != nil {
		t.Fatal(err)
	}
	if persisted.Priority != startPriority {
		t.Fatalf("priority must roll back: got %s want %s", persisted.Priority, startPriority)
	}
	if persisted.Version != startVersion {
		t.Fatalf("version must roll back: got %d want %d", persisted.Version, startVersion)
	}

	var priorityEvents int64
	if err := db.Model(&History{}).
		Where("ticket_id=? AND event_type=?", tk.ID, "PRIORITY").
		Count(&priorityEvents).Error; err != nil {
		t.Fatal(err)
	}
	if priorityEvents != 0 {
		t.Fatalf("expected zero PRIORITY history rows after rollback, got %d", priorityEvents)
	}
}

// TestPostgresF2410B_SetPrioritySuccessCommitsPriorityAndHistory proves the happy path:
// Priority, Version, and PRIORITY history commit together.
func TestPostgresF2410B_SetPrioritySuccessCommitsPriorityAndHistory(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	admin := adminAccess(100)

	tk, err := svc.CheckInWalkIn(WalkInCheckInRequest{
		PatientID: 1, ServiceID: 10, IdentityConfirmed: true, Priority: PriorityNormal,
		Reason: "f2410b-ok",
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	startVersion := tk.Version

	actor := priorityActor(24104)
	_ = db.Exec(`INSERT INTO users(id, name) VALUES (24104,'F2410B-OK') ON CONFLICT DO NOTHING`)

	out, err := svc.SetPriority(tk.ID, PriorityRequest{
		Priority: PriorityUrgent, Reason: "escalate", ExpectedVersion: tk.Version,
	}, actor)
	if err != nil {
		t.Fatal(err)
	}
	if out.Priority != PriorityUrgent || out.Version != startVersion+1 {
		t.Fatalf("returned ticket Priority=%s Version=%d", out.Priority, out.Version)
	}

	var persisted Ticket
	if err := db.First(&persisted, tk.ID).Error; err != nil {
		t.Fatal(err)
	}
	if persisted.Priority != PriorityUrgent || persisted.Version != startVersion+1 {
		t.Fatalf("persisted Priority=%s Version=%d", persisted.Priority, persisted.Version)
	}

	var hist History
	if err := db.Where("ticket_id=? AND event_type=?", tk.ID, "PRIORITY").First(&hist).Error; err != nil {
		t.Fatalf("PRIORITY history missing: %v", err)
	}
	wantReason := PriorityNormal + "→" + PriorityUrgent + ": escalate"
	if hist.Reason != wantReason {
		t.Fatalf("history Reason=%q want %q", hist.Reason, wantReason)
	}
	if hist.ActorUserID != actor.UserID {
		t.Fatalf("history ActorUserID=%d want %d", hist.ActorUserID, actor.UserID)
	}
}
