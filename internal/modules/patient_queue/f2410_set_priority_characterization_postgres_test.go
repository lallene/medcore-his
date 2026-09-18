package patient_queue

import (
	"fmt"
	"sync"
	"testing"
)

// LOT 24G F24-10A — DESIGN GAP (deferred): no ExpectedVersion / OCC on SetPriority.
// F24-10B — GREEN: Priority + Version + PRIORITY history commit in one transaction.

// TestPostgresF2410A_SetPriorityConcurrentObservableBehavior characterizes
// concurrent SetPriority without inventing an OCC contract.
//
// Observable under current non-OCC semantics: both callers can succeed;
// final Priority is last-writer-wins; Version typically advances by 1 from
// the shared pre-image (not by 2).
func TestPostgresF2410A_SetPriorityConcurrentObservableBehavior(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	admin := adminAccess(100)

	tk, err := svc.CheckInWalkIn(WalkInCheckInRequest{
		PatientID: 1, ServiceID: 10, IdentityConfirmed: true, Priority: PriorityNormal,
		Reason: "f2410a",
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	startVersion := tk.Version
	if startVersion < 1 {
		t.Fatalf("start Version=%d", startVersion)
	}

	actorA := Access{
		UserID: 24101,
		Permissions: map[string]bool{
			"queue.priority.update": true,
			"queue.read.all":        true,
		},
	}
	actorB := Access{
		UserID: 24102,
		Permissions: map[string]bool{
			"queue.priority.update": true,
			"queue.read.all":        true,
		},
	}
	_ = db.Exec(`INSERT INTO users(id, name) VALUES (24101,'F2410A-A'),(24102,'F2410A-B') ON CONFLICT DO NOTHING`)

	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, e := svc.SetPriority(tk.ID, PriorityRequest{Priority: PriorityUrgent, Reason: "A"}, actorA)
		errs <- e
	}()
	go func() {
		defer wg.Done()
		<-start
		_, e := svc.SetPriority(tk.ID, PriorityRequest{Priority: PriorityHigh, Reason: "B"}, actorB)
		errs <- e
	}()
	close(start)
	wg.Wait()
	close(errs)

	success := 0
	for e := range errs {
		if e == nil {
			success++
		} else {
			t.Logf("SetPriority error (characterization): %v", e)
		}
	}

	var after Ticket
	if err := db.First(&after, tk.ID).Error; err != nil {
		t.Fatal(err)
	}

	t.Logf("F24-10A observed: success=%d startVersion=%d finalVersion=%d finalPriority=%s",
		success, startVersion, after.Version, after.Priority)

	// Characterization of deferred non-OCC contract — not a GREEN OCC expectation.
	if success != 2 {
		t.Fatalf("current SetPriority has no OCC gate; expected both concurrent calls to succeed, got success=%d", success)
	}
	if after.Priority != PriorityUrgent && after.Priority != PriorityHigh {
		t.Fatalf("final Priority=%s want one of URGENT|HIGH (last-writer-wins)", after.Priority)
	}
	if after.Version != startVersion+1 {
		t.Logf("NOTE: Version delta=%d (start=%d final=%d); classic lost-update stamp is +1",
			after.Version-startVersion, startVersion, after.Version)
	}
	if after.Version < startVersion+1 {
		t.Fatalf("Version must not decrease: start=%d final=%d", startVersion, after.Version)
	}

	var priorityEvents int64
	if err := db.Model(&History{}).
		Where("ticket_id=? AND event_type=?", tk.ID, "PRIORITY").
		Count(&priorityEvents).Error; err != nil {
		t.Fatal(err)
	}
	t.Logf("F24-10A PRIORITY history rows=%d", priorityEvents)
	if priorityEvents < 1 {
		t.Fatal("successful SetPriority must leave at least one PRIORITY history row")
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

	actor := Access{
		UserID: 24103,
		Permissions: map[string]bool{
			"queue.priority.update": true,
			"queue.read.all":        true,
		},
	}
	_ = db.Exec(`INSERT INTO users(id, name) VALUES (24103,'F2410B') ON CONFLICT DO NOTHING`)

	_, err = svc.SetPriority(tk.ID, PriorityRequest{Priority: PriorityUrgent, Reason: "audit-fail"}, actor)
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

	actor := Access{
		UserID: 24104,
		Permissions: map[string]bool{
			"queue.priority.update": true,
			"queue.read.all":        true,
		},
	}
	_ = db.Exec(`INSERT INTO users(id, name) VALUES (24104,'F2410B-OK') ON CONFLICT DO NOTHING`)

	out, err := svc.SetPriority(tk.ID, PriorityRequest{Priority: PriorityUrgent, Reason: "escalate"}, actor)
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
