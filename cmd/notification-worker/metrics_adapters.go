package main

import (
	"context"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/patient_queue"
	"github.com/lallene/medcore-his/backend/internal/shared/email"
)

// metricsEmailTransport times only email.Transport.Send (LOT 26I-5C).
// Lookup/render remain outside this timer in EmailDeliveryAdapter.
type metricsEmailTransport struct {
	inner email.Transport
	obs   patient_queue.WorkerLoopObserver
}

func newMetricsEmailTransport(inner email.Transport, obs patient_queue.WorkerLoopObserver) email.Transport {
	if inner == nil {
		return nil
	}
	if obs == nil {
		obs = noopProviderObserver{}
	}
	return &metricsEmailTransport{inner: inner, obs: obs}
}

func (t *metricsEmailTransport) ProviderName() string {
	return t.inner.ProviderName()
}

func (t *metricsEmailTransport) Send(ctx context.Context, msg email.Message) (email.Result, error) {
	start := time.Now()
	res, err := t.inner.Send(ctx, msg)
	t.obs.ObserveProviderDuration(
		patient_queue.MetricChannelEmail,
		patient_queue.MetricProviderMicrosoft365,
		time.Since(start),
	)
	return res, err
}

// metricsLogAdapter times LogDeliveryAdapter.Send only (LOT 26I-5C).
type metricsLogAdapter struct {
	inner patient_queue.NotificationDeliveryAdapter
	obs   patient_queue.WorkerLoopObserver
}

func newMetricsLogAdapter(inner patient_queue.NotificationDeliveryAdapter, obs patient_queue.WorkerLoopObserver) patient_queue.NotificationDeliveryAdapter {
	if inner == nil {
		return nil
	}
	if obs == nil {
		obs = noopProviderObserver{}
	}
	return &metricsLogAdapter{inner: inner, obs: obs}
}

func (a *metricsLogAdapter) Channel() string      { return a.inner.Channel() }
func (a *metricsLogAdapter) ProviderName() string { return a.inner.ProviderName() }

func (a *metricsLogAdapter) Send(ctx context.Context, intent *patient_queue.AppointmentNotificationIntent, payload patient_queue.NotificationPayload) (patient_queue.DeliveryResult, error) {
	start := time.Now()
	res, err := a.inner.Send(ctx, intent, payload)
	a.obs.ObserveProviderDuration(
		patient_queue.MetricChannelLog,
		patient_queue.MetricProviderLog,
		time.Since(start),
	)
	return res, err
}

// noopProviderObserver discards duration observations when metrics are unavailable.
type noopProviderObserver struct{}

func (noopProviderObserver) ObserveTick(time.Duration, error) {}
func (noopProviderObserver) ObserveClaimed(int)               {}
func (noopProviderObserver) ObserveStaleRecovered(int)        {}
func (noopProviderObserver) ObserveDeliveryAttempt(patient_queue.MetricChannel, patient_queue.DeliveryOutcome) {
}
func (noopProviderObserver) ObserveProviderDuration(patient_queue.MetricChannel, patient_queue.MetricProvider, time.Duration) {
}
