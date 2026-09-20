package patient_queue

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/shared/email"
)

func TestNotificationEffectiveRetryDelay26H4(t *testing.T) {
	t.Parallel()
	if NotificationMaxAttempts != 5 {
		t.Fatalf("MaxAttempts=%d", NotificationMaxAttempts)
	}
	if NotificationStaleProcessing != 15*time.Minute {
		t.Fatalf("Stale=%s", NotificationStaleProcessing)
	}
	cases := []struct {
		attempt int
		floor   time.Duration
		want    time.Duration
		ok      bool
	}{
		{1, 0, time.Minute, true},
		{1, -time.Minute, time.Minute, true},          // negative floor ignored
		{1, 30 * time.Second, time.Minute, true},      // smaller than base → base
		{1, 2 * time.Minute, 2 * time.Minute, true},   // larger → floor
		{3, 2 * time.Minute, 15 * time.Minute, true},  // later attempt base wins
		{3, 20 * time.Minute, 20 * time.Minute, true}, // later attempt floor wins
		{5, 10 * time.Hour, 0, false},                 // terminal
		{2, 0, 5 * time.Minute, true},
		{4, 0, time.Hour, true},
	}
	for _, tc := range cases {
		got, ok := NotificationEffectiveRetryDelay(tc.attempt, tc.floor)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("attempt=%d floor=%s → (%s,%v) want (%s,%v)",
				tc.attempt, tc.floor, got, ok, tc.want, tc.ok)
		}
	}
}

func TestPostgresWorkerRetryAfterFloor26H4(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	if err := EnsureNotificationIndexes(db); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2027, 5, 1, 10, 0, 0, 0, time.UTC)
	asOf := time.Now().UTC()

	enqueueClaim := func(apptID uint, key string) *AppointmentNotificationIntent {
		t.Helper()
		row, err := svc.EnqueueNotificationIntent(EnqueueNotificationIntentInput{
			AppointmentID: apptID, PatientID: 1, Kind: NotifKindBooked, Channel: NotifChannelEmail,
			OccurrenceKey: key, SendAfter: asOf.Add(-time.Minute), PayloadJSON: mustPayload(t, apptID, start),
		})
		if err != nil {
			t.Fatal(err)
		}
		claimed, err := svc.ClaimDueNotificationIntents(asOf, 5, []string{NotifChannelEmail})
		if err != nil {
			t.Fatal(err)
		}
		for i := range claimed {
			if claimed[i].ID == row.ID {
				return &claimed[i]
			}
		}
		t.Fatalf("not claimed %d", row.ID)
		return nil
	}

	t.Run("hint_smaller_than_base", func(t *testing.T) {
		claimed := enqueueClaim(5301, "floor-small")
		ad := &scriptedAdapter{
			channel: NotifChannelEmail, provider: "m365",
			err: email.TransientRetryAfter(errors.New("429"), 30*time.Second),
		}
		w := mustNotificationWorker(t, svc, NotificationWorkerConfig{Adapters: notificationAdapters(ad)})
		w.processClaimed(context.Background(), claimed, asOf)
		got, _ := svc.FindNotificationIntent(claimed.ID)
		if got.Status != NotifStatusPending {
			t.Fatalf("status=%s", got.Status)
		}
		want := asOf.Add(time.Minute)
		if got.SendAfter.Before(want.Add(-time.Second)) || got.SendAfter.After(want.Add(time.Second)) {
			t.Fatalf("send_after=%s want ~%s (base 1m)", got.SendAfter, want)
		}
	})

	t.Run("hint_larger_than_base", func(t *testing.T) {
		claimed := enqueueClaim(5302, "floor-large")
		ad := &scriptedAdapter{
			channel: NotifChannelEmail, provider: "m365",
			err: email.TransientRetryAfter(errors.New("429"), 2*time.Minute),
		}
		w := mustNotificationWorker(t, svc, NotificationWorkerConfig{Adapters: notificationAdapters(ad)})
		w.processClaimed(context.Background(), claimed, asOf)
		got, _ := svc.FindNotificationIntent(claimed.ID)
		if got.Status != NotifStatusPending {
			t.Fatalf("status=%s", got.Status)
		}
		want := asOf.Add(2 * time.Minute)
		if got.SendAfter.Before(want.Add(-time.Second)) || got.SendAfter.After(want.Add(time.Second)) {
			t.Fatalf("send_after=%s want ~%s (floor 2m)", got.SendAfter, want)
		}
	})

	t.Run("later_attempt_base_wins", func(t *testing.T) {
		row, err := svc.EnqueueNotificationIntent(EnqueueNotificationIntentInput{
			AppointmentID: 5303, PatientID: 1, Kind: NotifKindBooked, Channel: NotifChannelEmail,
			OccurrenceKey: "floor-later", SendAfter: asOf.Add(-time.Minute), PayloadJSON: mustPayload(t, 5303, start),
		})
		if err != nil {
			t.Fatal(err)
		}
		// Two prior failures → next attemptNo=3 → base 15m.
		for i := 0; i < 2; i++ {
			claimed, err := svc.ClaimDueNotificationIntents(asOf, 5, []string{NotifChannelEmail})
			if err != nil || len(claimed) == 0 {
				t.Fatalf("claim err=%v n=%d", err, len(claimed))
			}
			var hit *AppointmentNotificationIntent
			for j := range claimed {
				if claimed[j].ID == row.ID {
					hit = &claimed[j]
					break
				}
			}
			if hit == nil {
				t.Fatal("row not claimed")
			}
			msg := "prior"
			if err := svc.failOrRetryAfterAttempt(row.ID, "m365", nil, &msg, asOf, 0); err != nil {
				t.Fatal(err)
			}
			db.Model(&AppointmentNotificationIntent{}).Where("id=?", row.ID).Updates(map[string]any{
				"status": NotifStatusPending, "processing_started_at": nil, "send_after": asOf.Add(-time.Minute),
			})
		}
		claimed, err := svc.ClaimDueNotificationIntents(asOf, 5, []string{NotifChannelEmail})
		if err != nil {
			t.Fatal(err)
		}
		var hit *AppointmentNotificationIntent
		for i := range claimed {
			if claimed[i].ID == row.ID {
				hit = &claimed[i]
				break
			}
		}
		if hit == nil {
			t.Fatal("not claimed for attempt 3")
		}
		ad := &scriptedAdapter{
			channel: NotifChannelEmail, provider: "m365",
			err: email.TransientRetryAfter(errors.New("429"), 2*time.Minute),
		}
		w := mustNotificationWorker(t, svc, NotificationWorkerConfig{Adapters: notificationAdapters(ad)})
		w.processClaimed(context.Background(), hit, asOf)
		got, _ := svc.FindNotificationIntent(row.ID)
		if got.Status != NotifStatusPending {
			t.Fatalf("status=%s", got.Status)
		}
		want := asOf.Add(15 * time.Minute)
		if got.SendAfter.Before(want.Add(-time.Second)) || got.SendAfter.After(want.Add(time.Second)) {
			t.Fatalf("send_after=%s want ~%s (base 15m > hint 2m)", got.SendAfter, want)
		}
	})

	t.Run("max_attempts_ignores_large_hint", func(t *testing.T) {
		row, err := svc.EnqueueNotificationIntent(EnqueueNotificationIntentInput{
			AppointmentID: 5304, PatientID: 1, Kind: NotifKindBooked, Channel: NotifChannelEmail,
			OccurrenceKey: "floor-max", SendAfter: asOf.Add(-time.Minute), PayloadJSON: mustPayload(t, 5304, start),
		})
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < NotificationMaxAttempts-1; i++ {
			claimed, err := svc.ClaimDueNotificationIntents(asOf, 5, []string{NotifChannelEmail})
			if err != nil {
				t.Fatal(err)
			}
			var hit *AppointmentNotificationIntent
			for j := range claimed {
				if claimed[j].ID == row.ID {
					hit = &claimed[j]
					break
				}
			}
			if hit == nil {
				t.Fatal("not claimed")
			}
			msg := "prior"
			if err := svc.failOrRetryAfterAttempt(row.ID, "m365", nil, &msg, asOf, 0); err != nil {
				t.Fatal(err)
			}
			if i < NotificationMaxAttempts-2 {
				db.Model(&AppointmentNotificationIntent{}).Where("id=?", row.ID).Updates(map[string]any{
					"status": NotifStatusPending, "processing_started_at": nil, "send_after": asOf.Add(-time.Minute),
				})
			}
		}
		// Ensure PENDING for final claim.
		db.Model(&AppointmentNotificationIntent{}).Where("id=?", row.ID).Updates(map[string]any{
			"status": NotifStatusPending, "processing_started_at": nil, "send_after": asOf.Add(-time.Minute),
		})
		claimed, err := svc.ClaimDueNotificationIntents(asOf, 5, []string{NotifChannelEmail})
		if err != nil {
			t.Fatal(err)
		}
		var hit *AppointmentNotificationIntent
		for i := range claimed {
			if claimed[i].ID == row.ID {
				hit = &claimed[i]
				break
			}
		}
		if hit == nil {
			t.Fatal("final claim missed")
		}
		ad := &scriptedAdapter{
			channel: NotifChannelEmail, provider: "m365",
			err: email.TransientRetryAfter(errors.New("429"), 24*time.Hour),
		}
		w := mustNotificationWorker(t, svc, NotificationWorkerConfig{Adapters: notificationAdapters(ad)})
		w.processClaimed(context.Background(), hit, asOf)
		got, _ := svc.FindNotificationIntent(row.ID)
		if got.Status != NotifStatusFailed {
			t.Fatalf("status=%s want FAILED", got.Status)
		}
		n, _ := svc.CountNotificationAttempts(row.ID)
		if n != NotificationMaxAttempts {
			t.Fatalf("attempts=%d want %d", n, NotificationMaxAttempts)
		}
	})

	t.Run("ambiguous_ignores_hint", func(t *testing.T) {
		claimed := enqueueClaim(5305, "floor-amb")
		// Constructively impossible in production, but defend worker precedence.
		ad := &scriptedAdapter{
			channel: NotifChannelEmail, provider: "m365",
			err: email.AmbiguousDelivery(errors.New("uncertain")),
		}
		w := mustNotificationWorker(t, svc, NotificationWorkerConfig{Adapters: notificationAdapters(ad)})
		w.processClaimed(context.Background(), claimed, asOf)
		got, _ := svc.FindNotificationIntent(claimed.ID)
		if got.Status != NotifStatusFailed {
			t.Fatalf("status=%s want FAILED", got.Status)
		}
	})
}
