package patient_queue

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/shared/email"
)

type stubFinalizer struct{}

func (stubFinalizer) failOrRetryAfterAttempt(uint, string, *string, *string, time.Time, time.Duration) error {
	return nil
}
func (stubFinalizer) FinalizeNotificationSent(uint, string, *string) (*AppointmentNotificationAttempt, error) {
	return &AppointmentNotificationAttempt{}, nil
}
func (stubFinalizer) FinalizeNotificationSkipped(uint, string, *string) (*AppointmentNotificationAttempt, error) {
	return &AppointmentNotificationAttempt{}, nil
}
func (stubFinalizer) FinalizeNotificationFailedTerminal(uint, string, *string, *string) (*AppointmentNotificationAttempt, error) {
	return &AppointmentNotificationAttempt{}, nil
}

type failSentFinalizer struct{ stubFinalizer }

func (failSentFinalizer) FinalizeNotificationSent(uint, string, *string) (*AppointmentNotificationAttempt, error) {
	return nil, errors.New("finalize sent failed")
}

func (f *fakeWorkerLoopObserver) ObserveDeliveryAttempt(ch MetricChannel, o DeliveryOutcome) {
	f.deliveries = append(f.deliveries, deliveryObs{channel: ch, outcome: o})
}
func (f *fakeWorkerLoopObserver) ObserveProviderDuration(ch MetricChannel, p MetricProvider, d time.Duration) {
	f.providers = append(f.providers, providerObs{channel: ch, provider: p, duration: d})
}

type deliveryObs struct {
	channel MetricChannel
	outcome DeliveryOutcome
}
type providerObs struct {
	channel  MetricChannel
	provider MetricProvider
	duration time.Duration
}

func testWorkerForDelivery(t *testing.T, ad NotificationDeliveryAdapter, obs *fakeWorkerLoopObserver, fin notificationDeliveryFinalizer) *NotificationWorker {
	t.Helper()
	if fin == nil {
		fin = stubFinalizer{}
	}
	w, err := NewNotificationWorker(nil, NotificationWorkerConfig{
		Adapters: map[string]NotificationDeliveryAdapter{ad.Channel(): ad},
		Observer: obs,
	})
	if err != nil {
		t.Fatal(err)
	}
	w.finalizer = fin
	w.lease = &scriptedLease{}
	return w
}

func intentLOG(id uint) *AppointmentNotificationIntent {
	return &AppointmentNotificationIntent{
		ID: id, Channel: NotifChannelLog, Kind: NotifKindBooked,
		Status: NotifStatusProcessing, PayloadJSON: mustPayloadJSON(),
	}
}

func mustPayloadJSON() string {
	_, raw, err := BuildNotificationPayload(1, time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC), "", "", "")
	if err != nil {
		panic(err)
	}
	return raw
}

func assertOneDelivery(t *testing.T, obs *fakeWorkerLoopObserver, want DeliveryOutcome) {
	t.Helper()
	if len(obs.deliveries) != 1 {
		t.Fatalf("deliveries=%v want 1", obs.deliveries)
	}
	if obs.deliveries[0].channel != MetricChannelLog {
		t.Fatalf("channel=%q", obs.deliveries[0].channel)
	}
	if obs.deliveries[0].outcome != want {
		t.Fatalf("outcome=%q want %q", obs.deliveries[0].outcome, want)
	}
}

func TestDeliveryOutcomeSent(t *testing.T) {
	t.Parallel()
	obs := &fakeWorkerLoopObserver{}
	ad := &scriptedAdapter{channel: NotifChannelLog, provider: "log", result: DeliveryResult{ProviderMessageID: "log"}}
	w := testWorkerForDelivery(t, ad, obs, stubFinalizer{})
	w.processClaimed(context.Background(), intentLOG(1), time.Now().UTC())
	assertOneDelivery(t, obs, DeliveryOutcomeSent)
}

func TestDeliveryOutcomeSentSkippedWhenFinalizeFails(t *testing.T) {
	t.Parallel()
	obs := &fakeWorkerLoopObserver{}
	ad := &scriptedAdapter{channel: NotifChannelLog, provider: "log", result: DeliveryResult{ProviderMessageID: "log"}}
	w := testWorkerForDelivery(t, ad, obs, failSentFinalizer{})
	w.processClaimed(context.Background(), intentLOG(1), time.Now().UTC())
	if len(obs.deliveries) != 0 {
		t.Fatalf("finalize failure must not record sent: %v", obs.deliveries)
	}
}

func TestDeliveryOutcomePreSendSkipped(t *testing.T) {
	t.Parallel()
	obs := &fakeWorkerLoopObserver{}
	ad := &scriptedAdapter{channel: NotifChannelLog, provider: "log"}
	w := testWorkerForDelivery(t, ad, obs, stubFinalizer{})
	w.preSendCheck = func(*AppointmentNotificationIntent, NotificationPayload) (bool, string) {
		return true, "appointment cancelled"
	}
	w.processClaimed(context.Background(), intentLOG(2), time.Now().UTC())
	assertOneDelivery(t, obs, DeliveryOutcomeSkipped)
	if ad.sends != 0 {
		t.Fatal("pre-send skip must not call Send")
	}
}

func TestDeliveryOutcomeAdapterSkipped(t *testing.T) {
	t.Parallel()
	obs := &fakeWorkerLoopObserver{}
	ad := NewNoopDeliveryAdapter(NotifChannelLog)
	w := testWorkerForDelivery(t, ad, obs, stubFinalizer{})
	w.processClaimed(context.Background(), intentLOG(3), time.Now().UTC())
	assertOneDelivery(t, obs, DeliveryOutcomeSkipped)
}

func TestDeliveryOutcomeAmbiguous(t *testing.T) {
	t.Parallel()
	obs := &fakeWorkerLoopObserver{}
	ad := &scriptedAdapter{channel: NotifChannelLog, provider: "log", err: email.AmbiguousDelivery(errors.New("x"))}
	w := testWorkerForDelivery(t, ad, obs, stubFinalizer{})
	w.processClaimed(context.Background(), intentLOG(4), time.Now().UTC())
	assertOneDelivery(t, obs, DeliveryOutcomeAmbiguous)
}

func TestDeliveryOutcomePermanent(t *testing.T) {
	t.Parallel()
	obs := &fakeWorkerLoopObserver{}
	ad := &scriptedAdapter{channel: NotifChannelLog, provider: "log", err: email.Permanent(errors.New("x"))}
	w := testWorkerForDelivery(t, ad, obs, stubFinalizer{})
	w.processClaimed(context.Background(), intentLOG(5), time.Now().UTC())
	assertOneDelivery(t, obs, DeliveryOutcomePermanent)
}

func TestDeliveryOutcomeInvalidMessage(t *testing.T) {
	t.Parallel()
	obs := &fakeWorkerLoopObserver{}
	ad := &scriptedAdapter{channel: NotifChannelLog, provider: "log", err: email.ErrInvalidMessage}
	w := testWorkerForDelivery(t, ad, obs, stubFinalizer{})
	w.processClaimed(context.Background(), intentLOG(6), time.Now().UTC())
	assertOneDelivery(t, obs, DeliveryOutcomeInvalidMessage)
}

func TestDeliveryOutcomeNotConfigured(t *testing.T) {
	t.Parallel()
	obs := &fakeWorkerLoopObserver{}
	ad := &scriptedAdapter{channel: NotifChannelLog, provider: "log", err: email.NotConfigured(nil)}
	w := testWorkerForDelivery(t, ad, obs, stubFinalizer{})
	w.processClaimed(context.Background(), intentLOG(7), time.Now().UTC())
	assertOneDelivery(t, obs, DeliveryOutcomeNotConfigured)
}

func TestDeliveryOutcomeTransient(t *testing.T) {
	t.Parallel()
	obs := &fakeWorkerLoopObserver{}
	ad := &scriptedAdapter{channel: NotifChannelLog, provider: "log", err: email.Transient(errors.New("throttle"))}
	w := testWorkerForDelivery(t, ad, obs, stubFinalizer{})
	w.processClaimed(context.Background(), intentLOG(8), time.Now().UTC())
	assertOneDelivery(t, obs, DeliveryOutcomeTransient)
}

func TestDeliveryOutcomeUnknownMapsTransient(t *testing.T) {
	t.Parallel()
	obs := &fakeWorkerLoopObserver{}
	ad := &scriptedAdapter{channel: NotifChannelLog, provider: "log", err: errors.New("boom")}
	w := testWorkerForDelivery(t, ad, obs, stubFinalizer{})
	w.processClaimed(context.Background(), intentLOG(9), time.Now().UTC())
	assertOneDelivery(t, obs, DeliveryOutcomeTransient)
}

func TestDeliveryOutcomeCanceled(t *testing.T) {
	t.Parallel()
	obs := &fakeWorkerLoopObserver{}
	ad := &scriptedAdapter{channel: NotifChannelLog, provider: "log", err: context.Canceled}
	w := testWorkerForDelivery(t, ad, obs, stubFinalizer{})
	w.processClaimed(context.Background(), intentLOG(10), time.Now().UTC())
	assertOneDelivery(t, obs, DeliveryOutcomeCanceled)
}

func TestDeliveryOutcomeDeadlineCanceled(t *testing.T) {
	t.Parallel()
	obs := &fakeWorkerLoopObserver{}
	ad := &scriptedAdapter{channel: NotifChannelLog, provider: "log", err: context.DeadlineExceeded}
	w := testWorkerForDelivery(t, ad, obs, stubFinalizer{})
	w.processClaimed(context.Background(), intentLOG(11), time.Now().UTC())
	assertOneDelivery(t, obs, DeliveryOutcomeCanceled)
}

func TestDeliveryOutcomeInvalidPayload(t *testing.T) {
	t.Parallel()
	obs := &fakeWorkerLoopObserver{}
	ad := &scriptedAdapter{channel: NotifChannelLog, provider: "log"}
	w := testWorkerForDelivery(t, ad, obs, stubFinalizer{})
	intent := intentLOG(12)
	intent.PayloadJSON = "{"
	w.processClaimed(context.Background(), intent, time.Now().UTC())
	assertOneDelivery(t, obs, DeliveryOutcomeInvalidPayload)
	if ad.sends != 0 {
		t.Fatal("invalid payload must not Send")
	}
}

func TestDeliveryOutcomeAdapterUnavailable(t *testing.T) {
	t.Parallel()
	obs := &fakeWorkerLoopObserver{}
	ad := &scriptedAdapter{channel: NotifChannelLog, provider: "log"}
	w := testWorkerForDelivery(t, ad, obs, stubFinalizer{})
	intent := &AppointmentNotificationIntent{
		ID: 13, Channel: NotifChannelEmail, Kind: NotifKindBooked,
		Status: NotifStatusProcessing, PayloadJSON: mustPayloadJSON(),
	}
	w.processClaimed(context.Background(), intent, time.Now().UTC())
	if len(obs.deliveries) != 1 || obs.deliveries[0].outcome != DeliveryOutcomeAdapterUnavailable {
		t.Fatalf("deliveries=%v", obs.deliveries)
	}
	if obs.deliveries[0].channel != MetricChannelEmail {
		t.Fatalf("channel=%q", obs.deliveries[0].channel)
	}
}

func TestMetricChannelFromDomainDropsSMS(t *testing.T) {
	t.Parallel()
	if _, ok := MetricChannelFromDomain(NotifChannelSMS); ok {
		t.Fatal("SMS must not map to a metric channel")
	}
}
