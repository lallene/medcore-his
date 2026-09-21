package patient_queue

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// LOT 26I-5D: targeted PostgreSQL coverage for NotificationQueueSnapshot aggregate.
func TestNotificationQueueSnapshotPostgresAggregate(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	asOf := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	staleCutoff := asOf.Add(-NotificationStaleProcessing)
	start := asOf.Add(24 * time.Hour)

	enqueue := func(apptID uint, channel, status string, sendAfter time.Time, procStarted *time.Time) {
		t.Helper()
		row, err := svc.EnqueueNotificationIntent(EnqueueNotificationIntentInput{
			AppointmentID: apptID,
			PatientID:     1,
			Kind:          NotifKindBooked,
			Channel:       channel,
			OccurrenceKey: fmt.Sprintf("snap-5d-%d", apptID),
			SendAfter:     sendAfter,
			PayloadJSON:   mustPayload(t, apptID, start),
		})
		if err != nil {
			t.Fatalf("enqueue appt=%d: %v", apptID, err)
		}
		updates := map[string]any{"status": status, "send_after": sendAfter}
		if status == NotifStatusProcessing {
			updates["processing_started_at"] = procStarted // may be nil
		}
		if err := db.Model(row).Updates(updates).Error; err != nil {
			t.Fatalf("update appt=%d: %v", apptID, err)
		}
		if status == NotifStatusProcessing && procStarted == nil {
			// GORM Updates skips nil pointers; force NULL explicitly.
			if err := db.Model(row).Update("processing_started_at", nil).Error; err != nil {
				t.Fatalf("null processing_started_at: %v", err)
			}
		}
	}

	// Zero rows → empty channels.
	empty, err := svc.NotificationQueueSnapshot(context.Background(), asOf)
	if err != nil {
		t.Fatal(err)
	}
	if len(empty.Channels) != 0 {
		t.Fatalf("zero rows want empty channels, got %+v", empty.Channels)
	}
	if !empty.AsOf.Equal(asOf) {
		t.Fatalf("AsOf=%v want %v", empty.AsOf, asOf)
	}

	// Pending future (not due).
	enqueue(9101, NotifChannelLog, NotifStatusPending, asOf.Add(time.Hour), nil)
	// Pending due (exact boundary send_after == asOf is due).
	enqueue(9102, NotifChannelLog, NotifStatusPending, asOf, nil)
	// Pending due older.
	oldestDue := asOf.Add(-5 * time.Minute)
	enqueue(9103, NotifChannelLog, NotifStatusPending, oldestDue, nil)
	// EMAIL due.
	enqueue(9104, NotifChannelEmail, NotifStatusPending, asOf.Add(-time.Minute), nil)
	// SMS due.
	enqueue(9105, NotifChannelSMS, NotifStatusPending, asOf.Add(-30*time.Second), nil)
	// PROCESSING fresh.
	freshStart := asOf.Add(-time.Minute)
	enqueue(9106, NotifChannelLog, NotifStatusProcessing, asOf.Add(-time.Hour), &freshStart)
	// PROCESSING stale (started before cutoff).
	staleStart := staleCutoff.Add(-time.Second)
	enqueue(9107, NotifChannelLog, NotifStatusProcessing, asOf.Add(-2*time.Hour), &staleStart)
	// PROCESSING at exact stale boundary (== cutoff) must NOT be stale (< cutoff).
	eqCutoff := staleCutoff
	enqueue(9108, NotifChannelLog, NotifStatusProcessing, asOf.Add(-3*time.Hour), &eqCutoff)
	// PROCESSING with NULL processing_started_at → not stale.
	enqueue(9109, NotifChannelLog, NotifStatusProcessing, asOf.Add(-4*time.Hour), nil)
	// Excluded statuses.
	enqueue(9110, NotifChannelLog, NotifStatusSent, asOf.Add(-time.Hour), nil)
	enqueue(9111, NotifChannelLog, NotifStatusFailed, asOf.Add(-time.Hour), nil)
	enqueue(9112, NotifChannelLog, NotifStatusCancelled, asOf.Add(-time.Hour), nil)

	snap, err := svc.NotificationQueueSnapshot(context.Background(), asOf)
	if err != nil {
		t.Fatal(err)
	}

	byCh := map[string]NotificationQueueChannelSnapshot{}
	for _, c := range snap.Channels {
		byCh[c.Channel] = c
	}

	logCh, ok := byCh[NotifChannelLog]
	if !ok {
		t.Fatalf("missing LOG: %+v", snap.Channels)
	}
	// pending: future + due@asOf + oldestDue = 3
	if logCh.Pending != 3 {
		t.Fatalf("LOG pending=%d want 3", logCh.Pending)
	}
	// due: asOf + oldestDue = 2 (future excluded)
	if logCh.Due != 2 {
		t.Fatalf("LOG due=%d want 2", logCh.Due)
	}
	// processing: fresh + stale + eqCutoff + nil-started = 4
	if logCh.Processing != 4 {
		t.Fatalf("LOG processing=%d want 4", logCh.Processing)
	}
	// stale: only staleStart < cutoff
	if logCh.StaleProcessing != 1 {
		t.Fatalf("LOG stale=%d want 1", logCh.StaleProcessing)
	}
	if logCh.OldestDueAge != 5*time.Minute {
		t.Fatalf("LOG oldest age=%s want 5m", logCh.OldestDueAge)
	}

	emailCh, ok := byCh[NotifChannelEmail]
	if !ok {
		t.Fatal("missing EMAIL")
	}
	if emailCh.Pending != 1 || emailCh.Due != 1 || emailCh.Processing != 0 || emailCh.StaleProcessing != 0 {
		t.Fatalf("EMAIL=%+v", emailCh)
	}
	if emailCh.OldestDueAge != time.Minute {
		t.Fatalf("EMAIL age=%s", emailCh.OldestDueAge)
	}

	smsCh, ok := byCh[NotifChannelSMS]
	if !ok {
		t.Fatal("missing SMS")
	}
	if smsCh.Pending != 1 || smsCh.Due != 1 {
		t.Fatalf("SMS=%+v", smsCh)
	}
	if smsCh.OldestDueAge != 30*time.Second {
		t.Fatalf("SMS age=%s", smsCh.OldestDueAge)
	}

	for _, c := range snap.Channels {
		if c.OldestDueAge < 0 {
			t.Fatalf("negative age for %s", c.Channel)
		}
	}
}
