package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/patient_queue"
	"github.com/lallene/medcore-his/backend/internal/shared/email"
)

type recordingObs struct {
	providers []struct {
		ch patient_queue.MetricChannel
		p  patient_queue.MetricProvider
		d  time.Duration
	}
}

func (r *recordingObs) ObserveTick(time.Duration, error) {}
func (r *recordingObs) ObserveClaimed(int)               {}
func (r *recordingObs) ObserveStaleRecovered(int)        {}
func (r *recordingObs) ObserveDeliveryAttempt(patient_queue.MetricChannel, patient_queue.DeliveryOutcome) {
}
func (r *recordingObs) ObserveProviderDuration(ch patient_queue.MetricChannel, p patient_queue.MetricProvider, d time.Duration) {
	r.providers = append(r.providers, struct {
		ch patient_queue.MetricChannel
		p  patient_queue.MetricProvider
		d  time.Duration
	}{ch, p, d})
}

type countingTransport struct {
	calls int
	err   error
}

func (c *countingTransport) ProviderName() string { return "microsoft365" }
func (c *countingTransport) Send(context.Context, email.Message) (email.Result, error) {
	c.calls++
	if c.err != nil {
		return email.Result{}, c.err
	}
	return email.Result{ProviderMessageID: "m"}, nil
}

func TestMetricsEmailTransportTimesSendOnly(t *testing.T) {
	t.Parallel()
	obs := &recordingObs{}
	inner := &countingTransport{}
	tr := newMetricsEmailTransport(inner, obs)

	msg := email.Message{
		To: email.Address{Address: "a@example.invalid"}, Subject: "s", TextBody: "b",
	}
	if _, err := tr.Send(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	if inner.calls != 1 {
		t.Fatalf("calls=%d", inner.calls)
	}
	if len(obs.providers) != 1 {
		t.Fatalf("obs=%v", obs.providers)
	}
	if obs.providers[0].ch != patient_queue.MetricChannelEmail ||
		obs.providers[0].p != patient_queue.MetricProviderMicrosoft365 {
		t.Fatalf("labels=%v", obs.providers[0])
	}
	if obs.providers[0].d < 0 {
		t.Fatal("duration")
	}

	inner.err = errors.New("boom")
	_, _ = tr.Send(context.Background(), msg)
	if len(obs.providers) != 2 {
		t.Fatalf("error path must still observe: %d", len(obs.providers))
	}
}

func TestMetricsLogAdapterTimesSend(t *testing.T) {
	t.Parallel()
	obs := &recordingObs{}
	inner := patient_queue.NewLogDeliveryAdapter(patient_queue.NotifChannelLog, nil)
	ad := newMetricsLogAdapter(inner, obs)
	intent := &patient_queue.AppointmentNotificationIntent{ID: 1, Channel: patient_queue.NotifChannelLog}
	_, err := ad.Send(context.Background(), intent, patient_queue.NotificationPayload{ScheduledAt: "2026-01-01T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if len(obs.providers) != 1 {
		t.Fatalf("obs=%v", obs.providers)
	}
	if obs.providers[0].ch != patient_queue.MetricChannelLog ||
		obs.providers[0].p != patient_queue.MetricProviderLog {
		t.Fatalf("labels=%v", obs.providers[0])
	}
}
