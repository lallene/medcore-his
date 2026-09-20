package patient_queue

import (
	"sync"
	"testing"
	"time"
)

// TestPostgresClaimDueSamePendingConcurrency26H proves two concurrent ClaimDue
// callers cannot both successfully claim the same PENDING intent under real PG
// locking (FOR UPDATE SKIP LOCKED + PENDING→PROCESSING guard).
func TestPostgresClaimDueSamePendingConcurrency26H(t *testing.T) {
	db, svc := notificationTestDB(t)

	start := time.Date(2027, 1, 15, 10, 0, 0, 0, time.UTC)
	_, payload, err := BuildNotificationPayload(26001, start, "Type", "Svc", "")
	if err != nil {
		t.Fatal(err)
	}
	intent, err := svc.EnqueueNotificationIntent(EnqueueNotificationIntentInput{
		AppointmentID: 26001,
		PatientID:     1,
		Kind:          NotifKindBooked,
		Channel:       NotifChannelLog,
		OccurrenceKey: OccurrenceKeyFromScheduledAt(start),
		SendAfter:     time.Now().UTC().Add(-time.Minute),
		PayloadJSON:   payload,
	})
	if err != nil || intent == nil {
		t.Fatalf("enqueue: %+v %v", intent, err)
	}

	const workers = 2
	startCh := make(chan struct{})
	type result struct {
		rows []AppointmentNotificationIntent
		err  error
	}
	results := make([]result, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-startCh
			rows, e := svc.ClaimDueNotificationIntents(time.Now().UTC(), 10, []string{NotifChannelLog})
			results[idx] = result{rows: rows, err: e}
		}(i)
	}
	close(startCh)
	wg.Wait()

	var claimedIDs []uint
	for i, r := range results {
		if r.err != nil {
			t.Fatalf("worker %d claim err: %v", i, r.err)
		}
		for _, row := range r.rows {
			if row.ID == intent.ID {
				claimedIDs = append(claimedIDs, row.ID)
			}
		}
	}
	if len(claimedIDs) != 1 {
		t.Fatalf("want exactly 1 successful claim of intent %d across workers, got %d (%v)", intent.ID, len(claimedIDs), claimedIDs)
	}

	cur, err := svc.FindNotificationIntent(intent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cur.Status != NotifStatusProcessing {
		t.Fatalf("final status=%s want PROCESSING", cur.Status)
	}

	var attempts int64
	if err := db.Model(&AppointmentNotificationAttempt{}).Where("intent_id=?", intent.ID).Count(&attempts).Error; err != nil {
		t.Fatal(err)
	}
	if attempts != 0 {
		t.Fatalf("claim must not create attempts, got %d", attempts)
	}
}
