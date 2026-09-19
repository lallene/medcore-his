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

func TestValidateNotificationAdapters(t *testing.T) {
	t.Parallel()

	t.Run("empty_rejected", func(t *testing.T) {
		t.Parallel()
		_, _, err := validateNotificationAdapters(nil)
		if err == nil {
			t.Fatal("expected error")
		}
		_, _, err = validateNotificationAdapters(map[string]NotificationDeliveryAdapter{})
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("nil_adapter_rejected", func(t *testing.T) {
		t.Parallel()
		_, _, err := validateNotificationAdapters(map[string]NotificationDeliveryAdapter{
			NotifChannelLog: nil,
		})
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("empty_key_rejected", func(t *testing.T) {
		t.Parallel()
		_, _, err := validateNotificationAdapters(map[string]NotificationDeliveryAdapter{
			"": NewLogDeliveryAdapter(NotifChannelLog, nil),
		})
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("empty_channel_rejected", func(t *testing.T) {
		t.Parallel()
		_, _, err := validateNotificationAdapters(map[string]NotificationDeliveryAdapter{
			NotifChannelLog: &emptyChannelAdapter{},
		})
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("key_channel_mismatch_rejected", func(t *testing.T) {
		t.Parallel()
		_, _, err := validateNotificationAdapters(map[string]NotificationDeliveryAdapter{
			NotifChannelEmail: NewLogDeliveryAdapter(NotifChannelLog, nil),
		})
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("sorted_channels", func(t *testing.T) {
		t.Parallel()
		_, channels, err := validateNotificationAdapters(map[string]NotificationDeliveryAdapter{
			NotifChannelEmail: NewNoopDeliveryAdapter(NotifChannelEmail),
			NotifChannelLog:   NewLogDeliveryAdapter(NotifChannelLog, nil),
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(channels) != 2 || channels[0] != NotifChannelEmail || channels[1] != NotifChannelLog {
			t.Fatalf("channels=%v want [EMAIL LOG]", channels)
		}
		if strings.Contains(strings.Join(channels, ","), NotifChannelSMS) {
			t.Fatal("SMS must be absent when not registered")
		}
	})
}

type emptyChannelAdapter struct{}

func (emptyChannelAdapter) Channel() string { return "" }
func (emptyChannelAdapter) ProviderName() string {
	return "empty"
}
func (emptyChannelAdapter) Send(context.Context, *AppointmentNotificationIntent, NotificationPayload) (DeliveryResult, error) {
	return DeliveryResult{}, nil
}

type scriptedAdapter struct {
	channel  string
	provider string
	result   DeliveryResult
	err      error
	sends    int
}

func (s *scriptedAdapter) Channel() string      { return s.channel }
func (s *scriptedAdapter) ProviderName() string { return s.provider }
func (s *scriptedAdapter) Send(context.Context, *AppointmentNotificationIntent, NotificationPayload) (DeliveryResult, error) {
	s.sends++
	return s.result, s.err
}

func TestPostgresMultiChannelClaim26F3(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	if err := EnsureNotificationIndexes(db); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2027, 1, 5, 9, 0, 0, 0, time.UTC)
	now := time.Now().UTC()

	logRow, err := svc.EnqueueNotificationIntent(EnqueueNotificationIntentInput{
		AppointmentID: 4101, PatientID: 1, Kind: NotifKindBooked, Channel: NotifChannelLog,
		OccurrenceKey: "c-log", SendAfter: now.Add(-time.Minute), PayloadJSON: mustPayload(t, 4101, start),
	})
	if err != nil {
		t.Fatal(err)
	}
	emailRow, err := svc.EnqueueNotificationIntent(EnqueueNotificationIntentInput{
		AppointmentID: 4102, PatientID: 1, Kind: NotifKindBooked, Channel: NotifChannelEmail,
		OccurrenceKey: "c-email", SendAfter: now.Add(-time.Minute), PayloadJSON: mustPayload(t, 4102, start),
	})
	if err != nil {
		t.Fatal(err)
	}
	smsRow, err := svc.EnqueueNotificationIntent(EnqueueNotificationIntentInput{
		AppointmentID: 4103, PatientID: 1, Kind: NotifKindBooked, Channel: NotifChannelSMS,
		OccurrenceKey: "c-sms", SendAfter: now.Add(-time.Minute), PayloadJSON: mustPayload(t, 4103, start),
	})
	if err != nil {
		t.Fatal(err)
	}

	empty, err := svc.ClaimDueNotificationIntents(now, 50, nil)
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty channels must claim nothing: n=%d err=%v", len(empty), err)
	}

	logOnly, err := svc.ClaimDueNotificationIntents(now, 50, []string{NotifChannelLog})
	if err != nil {
		t.Fatal(err)
	}
	ids := map[uint]bool{}
	for _, c := range logOnly {
		ids[c.ID] = true
		if c.Channel != NotifChannelLog {
			t.Fatalf("LOG-only claimed %s", c.Channel)
		}
	}
	if !ids[logRow.ID] || ids[emailRow.ID] || ids[smsRow.ID] {
		t.Fatalf("LOG-only claim set=%v", ids)
	}

	db.Model(&AppointmentNotificationIntent{}).Where("id=?", logRow.ID).Updates(map[string]any{
		"status": NotifStatusPending, "processing_started_at": nil, "send_after": now.Add(-time.Minute),
	})

	both, err := svc.ClaimDueNotificationIntents(now, 50, []string{NotifChannelEmail, NotifChannelLog})
	if err != nil {
		t.Fatal(err)
	}
	ids = map[uint]bool{}
	for _, c := range both {
		ids[c.ID] = true
	}
	if !ids[logRow.ID] || !ids[emailRow.ID] || ids[smsRow.ID] {
		t.Fatalf("LOG+EMAIL claim set=%v", ids)
	}
	smsRel, _ := svc.FindNotificationIntent(smsRow.ID)
	if smsRel.Status != NotifStatusPending {
		t.Fatalf("SMS must stay PENDING, got %s", smsRel.Status)
	}
}

func TestPostgresWorkerSkipReasonAndClassification26F3(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	if err := EnsureNotificationIndexes(db); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2027, 2, 1, 10, 0, 0, 0, time.UTC)
	asOf := time.Now().UTC()

	enqueue := func(apptID uint, channel, key string) *AppointmentNotificationIntent {
		t.Helper()
		row, err := svc.EnqueueNotificationIntent(EnqueueNotificationIntentInput{
			AppointmentID: apptID, PatientID: 1, Kind: NotifKindBooked, Channel: channel,
			OccurrenceKey: key, SendAfter: asOf.Add(-time.Minute), PayloadJSON: mustPayload(t, apptID, start),
		})
		if err != nil {
			t.Fatal(err)
		}
		return row
	}
	claimOne := func(id uint, channels []string) *AppointmentNotificationIntent {
		t.Helper()
		db.Model(&AppointmentNotificationIntent{}).Where("id=?", id).Updates(map[string]any{
			"status": NotifStatusPending, "processing_started_at": nil, "send_after": asOf.Add(-time.Minute),
		})
		rows, err := svc.ClaimDueNotificationIntents(asOf, 20, channels)
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

	t.Run("skip_reason_persisted", func(t *testing.T) {
		row := enqueue(4201, NotifChannelEmail, "skip-reason")
		ad := &scriptedAdapter{
			channel: NotifChannelEmail, provider: "email-fake",
			result: DeliveryResult{Skipped: true, SkipReason: NotifSkipReasonRecipientUnavailable},
		}
		w := mustNotificationWorker(t, svc, NotificationWorkerConfig{
			Adapters: notificationAdapters(ad),
		})
		w.processClaimed(context.Background(), claimOne(row.ID, []string{NotifChannelEmail}), asOf)
		got, _ := svc.FindNotificationIntent(row.ID)
		if got.Status != NotifStatusSkipped {
			t.Fatalf("status=%s", got.Status)
		}
		var att AppointmentNotificationAttempt
		if err := db.Where("intent_id=?", row.ID).First(&att).Error; err != nil {
			t.Fatal(err)
		}
		if att.Error == nil || *att.Error != NotifSkipReasonRecipientUnavailable {
			t.Fatalf("error=%v", att.Error)
		}
		if att.Provider != "email-fake" {
			t.Fatalf("provider=%s", att.Provider)
		}
	})

	t.Run("empty_skip_reason_fallback", func(t *testing.T) {
		row := enqueue(4202, NotifChannelLog, "skip-empty")
		ad := &scriptedAdapter{
			channel: NotifChannelLog, provider: "noopish",
			result: DeliveryResult{Skipped: true},
		}
		w := mustNotificationWorker(t, svc, NotificationWorkerConfig{Adapters: notificationAdapters(ad)})
		w.processClaimed(context.Background(), claimOne(row.ID, []string{NotifChannelLog}), asOf)
		var att AppointmentNotificationAttempt
		if err := db.Where("intent_id=?", row.ID).First(&att).Error; err != nil {
			t.Fatal(err)
		}
		if att.Error == nil || *att.Error != "adapter skipped" {
			t.Fatalf("error=%v", att.Error)
		}
	})

	t.Run("transient_retries", func(t *testing.T) {
		row := enqueue(4203, NotifChannelEmail, "transient")
		ad := &scriptedAdapter{
			channel: NotifChannelEmail, provider: "m365",
			err: email.Transient(errors.New("throttle")),
		}
		w := mustNotificationWorker(t, svc, NotificationWorkerConfig{Adapters: notificationAdapters(ad)})
		w.processClaimed(context.Background(), claimOne(row.ID, []string{NotifChannelEmail}), asOf)
		got, _ := svc.FindNotificationIntent(row.ID)
		if got.Status != NotifStatusPending {
			t.Fatalf("status=%s want PENDING", got.Status)
		}
		if !got.SendAfter.After(asOf) {
			t.Fatalf("send_after=%s", got.SendAfter)
		}
		n, _ := svc.CountNotificationAttempts(row.ID)
		if n != 1 {
			t.Fatalf("attempts=%d", n)
		}
	})

	t.Run("unknown_retries", func(t *testing.T) {
		row := enqueue(4204, NotifChannelLog, "unknown")
		ad := &scriptedAdapter{channel: NotifChannelLog, provider: "x", err: errors.New("boom")}
		w := mustNotificationWorker(t, svc, NotificationWorkerConfig{Adapters: notificationAdapters(ad)})
		w.processClaimed(context.Background(), claimOne(row.ID, []string{NotifChannelLog}), asOf)
		got, _ := svc.FindNotificationIntent(row.ID)
		if got.Status != NotifStatusPending {
			t.Fatalf("status=%s", got.Status)
		}
	})

	t.Run("permanent_terminal", func(t *testing.T) {
		row := enqueue(4205, NotifChannelEmail, "permanent")
		ad := &scriptedAdapter{
			channel: NotifChannelEmail, provider: "m365",
			err: fmt.Errorf("wrap: %w", email.Permanent(errors.New("policy"))),
		}
		w := mustNotificationWorker(t, svc, NotificationWorkerConfig{Adapters: notificationAdapters(ad)})
		w.processClaimed(context.Background(), claimOne(row.ID, []string{NotifChannelEmail}), asOf)
		got, _ := svc.FindNotificationIntent(row.ID)
		if got.Status != NotifStatusFailed {
			t.Fatalf("status=%s want FAILED", got.Status)
		}
		n, _ := svc.CountNotificationAttempts(row.ID)
		if n != 1 {
			t.Fatalf("attempts=%d", n)
		}
		var att AppointmentNotificationAttempt
		_ = db.Where("intent_id=?", row.ID).First(&att)
		if att.AttemptNo != 1 {
			t.Fatalf("attempt_no=%d", att.AttemptNo)
		}
	})

	t.Run("invalid_message_terminal", func(t *testing.T) {
		row := enqueue(4206, NotifChannelEmail, "invalid")
		ad := &scriptedAdapter{channel: NotifChannelEmail, provider: "m365", err: email.ErrInvalidMessage}
		w := mustNotificationWorker(t, svc, NotificationWorkerConfig{Adapters: notificationAdapters(ad)})
		w.processClaimed(context.Background(), claimOne(row.ID, []string{NotifChannelEmail}), asOf)
		got, _ := svc.FindNotificationIntent(row.ID)
		if got.Status != NotifStatusFailed {
			t.Fatalf("status=%s", got.Status)
		}
	})

	t.Run("not_configured_terminal", func(t *testing.T) {
		row := enqueue(4207, NotifChannelEmail, "noconfig")
		ad := &scriptedAdapter{channel: NotifChannelEmail, provider: "m365", err: email.NotConfigured(nil)}
		w := mustNotificationWorker(t, svc, NotificationWorkerConfig{Adapters: notificationAdapters(ad)})
		w.processClaimed(context.Background(), claimOne(row.ID, []string{NotifChannelEmail}), asOf)
		got, _ := svc.FindNotificationIntent(row.ID)
		if got.Status != NotifStatusFailed {
			t.Fatalf("status=%s", got.Status)
		}
	})

	t.Run("context_canceled_leaves_processing", func(t *testing.T) {
		row := enqueue(4208, NotifChannelEmail, "canceled")
		ad := &scriptedAdapter{channel: NotifChannelEmail, provider: "m365", err: context.Canceled}
		w := mustNotificationWorker(t, svc, NotificationWorkerConfig{Adapters: notificationAdapters(ad)})
		claimed := claimOne(row.ID, []string{NotifChannelEmail})
		w.processClaimed(context.Background(), claimed, asOf)
		got, _ := svc.FindNotificationIntent(row.ID)
		if got.Status != NotifStatusProcessing {
			t.Fatalf("status=%s want PROCESSING", got.Status)
		}
		if got.ProcessingStartedAt == nil {
			t.Fatal("processing_started_at must remain set")
		}
		n, _ := svc.CountNotificationAttempts(row.ID)
		if n != 0 {
			t.Fatalf("attempts=%d want 0", n)
		}
	})

	t.Run("deadline_exceeded_leaves_processing", func(t *testing.T) {
		row := enqueue(4209, NotifChannelEmail, "deadline")
		ad := &scriptedAdapter{channel: NotifChannelEmail, provider: "m365", err: context.DeadlineExceeded}
		w := mustNotificationWorker(t, svc, NotificationWorkerConfig{Adapters: notificationAdapters(ad)})
		claimed := claimOne(row.ID, []string{NotifChannelEmail})
		w.processClaimed(context.Background(), claimed, asOf)
		got, _ := svc.FindNotificationIntent(row.ID)
		if got.Status != NotifStatusProcessing || got.ProcessingStartedAt == nil {
			t.Fatalf("got status=%s lease=%v", got.Status, got.ProcessingStartedAt)
		}
		n, _ := svc.CountNotificationAttempts(row.ID)
		if n != 0 {
			t.Fatalf("attempts=%d", n)
		}
	})

	t.Run("provider_selection_per_channel", func(t *testing.T) {
		logRow := enqueue(4210, NotifChannelLog, "prov-log")
		emailRow := enqueue(4211, NotifChannelEmail, "prov-email")
		logAd := &scriptedAdapter{channel: NotifChannelLog, provider: "log-provider", result: DeliveryResult{ProviderMessageID: "L"}}
		emailAd := &scriptedAdapter{channel: NotifChannelEmail, provider: "email-provider", result: DeliveryResult{ProviderMessageID: "E"}}
		w := mustNotificationWorker(t, svc, NotificationWorkerConfig{
			Adapters: notificationAdapters(logAd, emailAd),
		})
		w.processClaimed(context.Background(), claimOne(logRow.ID, []string{NotifChannelLog, NotifChannelEmail}), asOf)
		w.processClaimed(context.Background(), claimOne(emailRow.ID, []string{NotifChannelLog, NotifChannelEmail}), asOf)
		var logAtt, emailAtt AppointmentNotificationAttempt
		if err := db.Where("intent_id=?", logRow.ID).First(&logAtt).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Where("intent_id=?", emailRow.ID).First(&emailAtt).Error; err != nil {
			t.Fatal(err)
		}
		if logAtt.Provider != "log-provider" || emailAtt.Provider != "email-provider" {
			t.Fatalf("providers log=%s email=%s", logAtt.Provider, emailAtt.Provider)
		}
	})
}

func TestPostgresFinalizeNotificationFailedTerminal26F3(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	if err := EnsureNotificationIndexes(db); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2027, 3, 1, 8, 0, 0, 0, time.UTC)
	asOf := time.Now().UTC()
	row, err := svc.EnqueueNotificationIntent(EnqueueNotificationIntentInput{
		AppointmentID: 4301, PatientID: 1, Kind: NotifKindBooked, Channel: NotifChannelLog,
		OccurrenceKey: "term", SendAfter: asOf.Add(-time.Minute), PayloadJSON: mustPayload(t, 4301, start),
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := svc.ClaimDueNotificationIntents(asOf, 5, []string{NotifChannelLog})
	if err != nil || len(claimed) == 0 {
		t.Fatalf("claim err=%v n=%d", err, len(claimed))
	}
	msg := "permanent failure"
	att, err := svc.FinalizeNotificationFailedTerminal(row.ID, "prov", nil, &msg)
	if err != nil {
		t.Fatal(err)
	}
	if att.AttemptNo != 1 || att.Provider != "prov" || att.Error == nil || *att.Error != msg {
		t.Fatalf("attempt=%+v", att)
	}
	got, _ := svc.FindNotificationIntent(row.ID)
	if got.Status != NotifStatusFailed || got.ProcessingStartedAt != nil {
		t.Fatalf("intent=%+v", got)
	}

	_, err = svc.FinalizeNotificationFailedTerminal(row.ID, "prov", nil, &msg)
	if err == nil {
		t.Fatal("expected conflict on non-PROCESSING")
	}
	n, _ := svc.CountNotificationAttempts(row.ID)
	if n != 1 {
		t.Fatalf("no partial second attempt; attempts=%d", n)
	}
}
