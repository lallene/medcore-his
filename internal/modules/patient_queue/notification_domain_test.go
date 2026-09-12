package patient_queue

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestNotificationKindChannelStatusValidation(t *testing.T) {
	if err := ValidateNotificationKind(NotifKindReminderT24H); err != nil {
		t.Fatal(err)
	}
	if err := ValidateNotificationKind("NOPE"); err == nil {
		t.Fatal("expected invalid kind")
	}
	if err := ValidateNotificationChannel(NotifChannelSMS); err != nil {
		t.Fatal(err)
	}
	if err := ValidateNotificationChannel("PUSH"); err == nil {
		t.Fatal("expected invalid channel")
	}
	if err := ValidateNotificationStatus(NotifStatusPending); err != nil {
		t.Fatal(err)
	}
	if err := ValidateNotificationStatus("QUEUED"); err == nil {
		t.Fatal("expected invalid status")
	}
}

func TestNotificationTransitionMatrix(t *testing.T) {
	allowed := [][2]string{
		{NotifStatusPending, NotifStatusProcessing},
		{NotifStatusPending, NotifStatusCancelled},
		{NotifStatusPending, NotifStatusSkipped},
		{NotifStatusProcessing, NotifStatusSent},
		{NotifStatusProcessing, NotifStatusFailed},
		{NotifStatusProcessing, NotifStatusPending},
	}
	for _, p := range allowed {
		if !CanTransitionNotificationStatus(p[0], p[1]) {
			t.Fatalf("expected allow %s → %s", p[0], p[1])
		}
		if err := AssertNotificationTransition(p[0], p[1]); err != nil {
			t.Fatal(err)
		}
	}
	denied := [][2]string{
		{NotifStatusSent, NotifStatusPending},
		{NotifStatusCancelled, NotifStatusProcessing},
		{NotifStatusSent, NotifStatusCancelled},
		{NotifStatusFailed, NotifStatusSent},
		{NotifStatusSkipped, NotifStatusPending},
	}
	for _, p := range denied {
		if CanTransitionNotificationStatus(p[0], p[1]) {
			t.Fatalf("expected deny %s → %s", p[0], p[1])
		}
		if err := AssertNotificationTransition(p[0], p[1]); err == nil {
			t.Fatalf("expected error %s → %s", p[0], p[1])
		}
	}
}

func TestOccurrenceKeyDeterministicAndUTCEquivalent(t *testing.T) {
	base := time.Date(2026, 6, 15, 10, 30, 0, 123456789, time.UTC)
	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatal(err)
	}
	sameInstant := base.In(paris)
	k1 := OccurrenceKeyFromScheduledAt(base)
	k2 := OccurrenceKeyFromScheduledAt(sameInstant)
	if k1 != k2 {
		t.Fatalf("UTC-equivalent must match: %s vs %s", k1, k2)
	}
	if k1 != OccurrenceKeyFromScheduledAt(base) {
		t.Fatal("same inputs must be stable")
	}
	rescheduled := base.Add(30 * time.Minute)
	if OccurrenceKeyFromScheduledAt(rescheduled) == k1 {
		t.Fatal("changed scheduled_at must change occurrence key")
	}
}

func TestReminderSendAfterT24HAbsoluteUTCEquivalent(t *testing.T) {
	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatal(err)
	}
	// Around EU DST spring-forward: same instant in UTC vs Paris must yield same send_after.
	startUTC := time.Date(2026, 3, 29, 14, 0, 0, 0, time.UTC)
	startParis := startUTC.In(paris)

	gotUTC := ReminderSendAfterT24H(startUTC)
	gotParis := ReminderSendAfterT24H(startParis)
	if !gotUTC.Equal(gotParis) {
		t.Fatalf("UTC-equivalent inputs must match: %s vs %s", gotUTC, gotParis)
	}
	want := startUTC.Add(-24 * time.Hour).UTC()
	if !gotUTC.Equal(want) {
		t.Fatalf("T-24h got %s want %s", gotUTC, want)
	}
	if startUTC.UTC().Sub(gotUTC) != 24*time.Hour {
		t.Fatalf("delta must be exactly 24h, got %s", startUTC.UTC().Sub(gotUTC))
	}
	if gotUTC.Location() != time.UTC {
		t.Fatalf("send_after must be UTC, got %s", gotUTC.Location())
	}
}

func TestBuildNotificationPayloadNoPHIFields(t *testing.T) {
	start := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	p, raw, err := BuildNotificationPayload(42, start, "Consult", "Urgences", "Clinique Demo")
	if err != nil {
		t.Fatal(err)
	}
	if p.AppointmentID != 42 || p.AppointmentTypeName != "Consult" {
		t.Fatalf("payload=%+v", p)
	}
	lower := strings.ToLower(raw)
	for _, bad := range []string{`"reason"`, `"telephone"`, `"email"`, `"phone"`, `"diagnosis"`} {
		if strings.Contains(lower, bad) {
			t.Fatalf("forbidden key %s in %s", bad, raw)
		}
	}
}

func TestValidatePersistedNotificationPayloadIntegrity(t *testing.T) {
	valid := `{"appointmentId":7,"scheduledAt":"2026-01-01T10:00:00Z"}`
	if _, err := ValidatePersistedNotificationPayload(valid, 7); err != nil {
		t.Fatalf("valid payload: %v", err)
	}
	if _, err := ValidatePersistedNotificationPayload("", 7); err == nil {
		t.Fatal("empty payload rejected")
	}
	if _, err := ValidatePersistedNotificationPayload(`{}`, 7); err == nil {
		t.Fatal("{} rejected")
	}
	if _, err := ValidatePersistedNotificationPayload(`{"appointmentId":0,"scheduledAt":"2026-01-01T10:00:00Z"}`, 7); err == nil {
		t.Fatal("zero appointmentId rejected")
	}
	if _, err := ValidatePersistedNotificationPayload(`{"appointmentId":8,"scheduledAt":"2026-01-01T10:00:00Z"}`, 7); err == nil {
		t.Fatal("mismatched appointmentId rejected")
	}
	if _, err := ValidatePersistedNotificationPayload(`{"appointmentId":7}`, 7); err == nil {
		t.Fatal("missing scheduledAt rejected")
	}
	if _, err := ValidatePersistedNotificationPayload(`{"appointmentId":7,"scheduledAt":"not-a-time"}`, 7); err == nil {
		t.Fatal("invalid scheduledAt rejected")
	}
	// Offset form must canonicalize to UTC without error.
	if _, err := ValidatePersistedNotificationPayload(`{"appointmentId":7,"scheduledAt":"2026-01-01T11:00:00+01:00"}`, 7); err != nil {
		t.Fatalf("offset scheduledAt: %v", err)
	}
}

func TestParseNotificationPayloadRejectsProhibitedKeys(t *testing.T) {
	if _, err := ParseNotificationPayload(`{"appointmentId":1,"reason":"douleur"}`); err == nil {
		t.Fatal("expected reject reason")
	}
	if _, err := ParseNotificationPayload(`{"appointmentId":1,"telephone":"0700000000"}`); err == nil {
		t.Fatal("expected reject telephone")
	}
	if _, err := ParseNotificationPayload(`{"appointmentId":1,"email":"a@b.c"}`); err == nil {
		t.Fatal("expected reject email")
	}
	if _, err := ParseNotificationPayload(`{"appointmentId":1,"extra":"x"}`); err == nil {
		t.Fatal("expected reject unknown key")
	}
	if _, err := ParseNotificationPayload(`{"appointmentId":1,"scheduledAt":"2026-01-01T10:00:00Z"}`); err != nil {
		t.Fatalf("safe payload: %v", err)
	}
}

func TestLogAdapterDoesNotLogContactPHI(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	ad := NewLogDeliveryAdapter(NotifChannelLog, logger)
	intent := &AppointmentNotificationIntent{ID: 9, AppointmentID: 3, Kind: NotifKindBooked, Channel: NotifChannelLog}
	payload := NotificationPayload{AppointmentID: 3, ScheduledAt: "2026-01-01T10:00:00Z"}
	if _, err := ad.Send(context.Background(), intent, payload); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, bad := range []string{"telephone", "email", "+225", "@"} {
		if strings.Contains(strings.ToLower(out), bad) {
			t.Fatalf("log leaked %q: %s", bad, out)
		}
	}
	if !strings.Contains(out, "intentId") || !strings.Contains(out, "appointmentId") {
		t.Fatalf("expected safe metadata: %s", out)
	}
}

func TestNoopAdapter(t *testing.T) {
	ad := NewNoopDeliveryAdapter(NotifChannelEmail)
	if ad.Channel() != NotifChannelEmail {
		t.Fatal(ad.Channel())
	}
	res, err := ad.Send(context.Background(), &AppointmentNotificationIntent{ID: 1}, NotificationPayload{})
	if err != nil || !res.Skipped {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}
