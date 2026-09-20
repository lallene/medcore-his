package patient_queue

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/shared/email"
)

// LOT 26H-2 — ambiguous delivery must be terminal FAILED (never PENDING retry).
func TestPostgresWorkerAmbiguousDelivery26H2(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	if err := EnsureNotificationIndexes(db); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2027, 4, 1, 10, 0, 0, 0, time.UTC)
	asOf := time.Now().UTC()

	enqueue := func(apptID uint, key string) *AppointmentNotificationIntent {
		t.Helper()
		row, err := svc.EnqueueNotificationIntent(EnqueueNotificationIntentInput{
			AppointmentID: apptID, PatientID: 1, Kind: NotifKindBooked, Channel: NotifChannelEmail,
			OccurrenceKey: key, SendAfter: asOf.Add(-time.Minute), PayloadJSON: mustPayload(t, apptID, start),
		})
		if err != nil {
			t.Fatal(err)
		}
		return row
	}
	claimOne := func(id uint) *AppointmentNotificationIntent {
		t.Helper()
		db.Model(&AppointmentNotificationIntent{}).Where("id=?", id).Updates(map[string]any{
			"status": NotifStatusPending, "processing_started_at": nil, "send_after": asOf.Add(-time.Minute),
		})
		rows, err := svc.ClaimDueNotificationIntents(asOf, 20, []string{NotifChannelEmail})
		if err != nil {
			t.Fatal(err)
		}
		for i := range rows {
			if rows[i].ID == id {
				return &rows[i]
			}
		}
		t.Fatalf("intent %d not claimed", id)
		return nil
	}

	t.Run("ambiguous_terminal_failed", func(t *testing.T) {
		row := enqueue(5201, "amb-term")
		ad := &scriptedAdapter{
			channel: NotifChannelEmail, provider: "m365",
			err: email.AmbiguousDelivery(errors.New("timeout after dispatch")),
		}
		w := mustNotificationWorker(t, svc, NotificationWorkerConfig{Adapters: notificationAdapters(ad)})
		w.processClaimed(context.Background(), claimOne(row.ID), asOf)

		got, _ := svc.FindNotificationIntent(row.ID)
		if got.Status != NotifStatusFailed {
			t.Fatalf("status=%s want FAILED", got.Status)
		}
		if got.ProcessingStartedAt != nil {
			t.Fatal("processing_started_at must be cleared")
		}
		n, _ := svc.CountNotificationAttempts(row.ID)
		if n != 1 {
			t.Fatalf("attempts=%d want 1", n)
		}
		var att AppointmentNotificationAttempt
		if err := db.Where("intent_id=?", row.ID).First(&att).Error; err != nil {
			t.Fatal(err)
		}
		if att.AttemptNo != 1 || att.Provider != "m365" {
			t.Fatalf("attempt=%+v", att)
		}
		if att.Error == nil || *att.Error != notificationAmbiguousDeliveryReason {
			t.Fatalf("error=%v want %q", att.Error, notificationAmbiguousDeliveryReason)
		}
		if strings.Contains(*att.Error, "@") || strings.Contains(strings.ToLower(*att.Error), "patient") {
			t.Fatalf("persisted error not privacy-safe: %q", *att.Error)
		}
		// Must not be claimable again.
		db.Model(&AppointmentNotificationIntent{}).Where("id=?", row.ID).
			Update("send_after", asOf.Add(-time.Minute))
		again, err := svc.ClaimDueNotificationIntents(asOf, 20, []string{NotifChannelEmail})
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range again {
			if c.ID == row.ID {
				t.Fatal("FAILED ambiguous intent must not be claimable")
			}
		}
		if ad.sends != 1 {
			t.Fatalf("sends=%d want 1 (no automatic re-Send)", ad.sends)
		}
	})

	t.Run("ambiguous_vs_transient", func(t *testing.T) {
		ambRow := enqueue(5202, "amb-vs")
		trRow := enqueue(5203, "tr-vs")
		ambAd := &scriptedAdapter{
			channel: NotifChannelEmail, provider: "m365",
			err: email.ErrAmbiguousDelivery,
		}
		trAd := &scriptedAdapter{
			channel: NotifChannelEmail, provider: "m365",
			err: email.Transient(errors.New("429")),
		}
		// Separate workers so each adapter is used once; same channel registry pattern.
		wAmb := mustNotificationWorker(t, svc, NotificationWorkerConfig{Adapters: notificationAdapters(ambAd)})
		wAmb.processClaimed(context.Background(), claimOne(ambRow.ID), asOf)
		// Re-register transient adapter for second intent (same channel key).
		wTr := mustNotificationWorker(t, svc, NotificationWorkerConfig{Adapters: notificationAdapters(trAd)})
		wTr.processClaimed(context.Background(), claimOne(trRow.ID), asOf)

		ambGot, _ := svc.FindNotificationIntent(ambRow.ID)
		trGot, _ := svc.FindNotificationIntent(trRow.ID)
		if ambGot.Status != NotifStatusFailed {
			t.Fatalf("ambiguous status=%s want FAILED", ambGot.Status)
		}
		if trGot.Status != NotifStatusPending {
			t.Fatalf("transient status=%s want PENDING", trGot.Status)
		}
		backoff, ok := NotificationRetryBackoff(1)
		if !ok {
			t.Fatal("attempt 1 must have backoff")
		}
		wantAfter := asOf.Add(backoff)
		if trGot.SendAfter.Before(wantAfter.Add(-time.Second)) || trGot.SendAfter.After(wantAfter.Add(time.Second)) {
			t.Fatalf("transient send_after=%s want ~%s", trGot.SendAfter, wantAfter)
		}
		if ambGot.ProcessingStartedAt != nil {
			t.Fatal("ambiguous lease must be cleared")
		}
		if trGot.ProcessingStartedAt != nil {
			t.Fatal("transient lease must be cleared on PENDING reclaim")
		}
	})

	t.Run("ambiguous_terminal_before_max_attempts", func(t *testing.T) {
		row := enqueue(5204, "amb-early")
		claimed := claimOne(row.ID)
		// Simulate two prior failed attempts while still PROCESSING.
		for i := 0; i < 2; i++ {
			msg := "prior transient"
			if err := svc.failOrRetryAfterAttempt(row.ID, "m365", nil, &msg, asOf, 0); err != nil {
				t.Fatal(err)
			}
			// Re-claim into PROCESSING for the next simulated attempt.
			db.Model(&AppointmentNotificationIntent{}).Where("id=?", row.ID).Updates(map[string]any{
				"status": NotifStatusPending, "processing_started_at": nil, "send_after": asOf.Add(-time.Minute),
			})
			claimed = claimOne(row.ID)
		}
		nBefore, _ := svc.CountNotificationAttempts(row.ID)
		if nBefore != 2 {
			t.Fatalf("prior attempts=%d want 2", nBefore)
		}
		ad := &scriptedAdapter{
			channel: NotifChannelEmail, provider: "m365",
			err: fmt.Errorf("wrap: %w", email.AmbiguousDelivery(nil)),
		}
		w := mustNotificationWorker(t, svc, NotificationWorkerConfig{Adapters: notificationAdapters(ad)})
		w.processClaimed(context.Background(), claimed, asOf)
		got, _ := svc.FindNotificationIntent(row.ID)
		if got.Status != NotifStatusFailed {
			t.Fatalf("status=%s want FAILED (terminal before max attempts)", got.Status)
		}
		n, _ := svc.CountNotificationAttempts(row.ID)
		if n != 3 {
			t.Fatalf("attempts=%d want 3", n)
		}
		var att AppointmentNotificationAttempt
		_ = db.Where("intent_id=? AND attempt_no=3", row.ID).First(&att)
		if att.Error == nil || *att.Error != notificationAmbiguousDeliveryReason {
			t.Fatalf("attempt3 error=%v", att.Error)
		}
	})

	t.Run("ambiguous_wrapped_deadline_is_terminal", func(t *testing.T) {
		row := enqueue(5205, "amb-deadline")
		ad := &scriptedAdapter{
			channel: NotifChannelEmail, provider: "m365",
			err: email.AmbiguousDelivery(context.DeadlineExceeded),
		}
		w := mustNotificationWorker(t, svc, NotificationWorkerConfig{Adapters: notificationAdapters(ad)})
		w.processClaimed(context.Background(), claimOne(row.ID), asOf)
		got, _ := svc.FindNotificationIntent(row.ID)
		if got.Status != NotifStatusFailed {
			t.Fatalf("status=%s want FAILED (ambiguous wins over deadline)", got.Status)
		}
		n, _ := svc.CountNotificationAttempts(row.ID)
		if n != 1 {
			t.Fatalf("attempts=%d", n)
		}
	})

	t.Run("plain_deadline_still_leaves_processing", func(t *testing.T) {
		row := enqueue(5206, "plain-deadline")
		ad := &scriptedAdapter{
			channel: NotifChannelEmail, provider: "m365",
			err: context.DeadlineExceeded,
		}
		w := mustNotificationWorker(t, svc, NotificationWorkerConfig{Adapters: notificationAdapters(ad)})
		claimed := claimOne(row.ID)
		w.processClaimed(context.Background(), claimed, asOf)
		got, _ := svc.FindNotificationIntent(row.ID)
		if got.Status != NotifStatusProcessing || got.ProcessingStartedAt == nil {
			t.Fatalf("status=%s lease=%v want PROCESSING with lease", got.Status, got.ProcessingStartedAt)
		}
		n, _ := svc.CountNotificationAttempts(row.ID)
		if n != 0 {
			t.Fatalf("attempts=%d want 0", n)
		}
	})
}

func TestAmbiguousDeliveryReasonPrivacy26H2(t *testing.T) {
	t.Parallel()
	r := strings.ToLower(notificationAmbiguousDeliveryReason)
	for _, bad := range []string{"@", "patient", "email", "reason", "token", "bearer"} {
		if strings.Contains(r, bad) {
			t.Fatalf("reason %q contains %q", notificationAmbiguousDeliveryReason, bad)
		}
	}
	if notificationAmbiguousDeliveryReason != "delivery outcome ambiguous" {
		t.Fatalf("stable ops reason changed: %q", notificationAmbiguousDeliveryReason)
	}
}
