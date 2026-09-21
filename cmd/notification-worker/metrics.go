package main

import (
	"fmt"
	"net/http"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/patient_queue"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Worker metrics privacy / cardinality contract (LOT 26I-5A / 26I-5B / 26I-5C / 26I-5D).
//
// Allowed label dimensions (bounded enums only, validated at observation):
//   5B: result ∈ {success, error}
//   5C: channel ∈ {log, email}
//       outcome ∈ {sent, skipped, permanent, invalid_message, not_configured,
//                  ambiguous, transient, canceled, invalid_payload, adapter_unavailable}
//       provider ∈ {log, microsoft365}
//   5D: channel ∈ {log, email, sms}  (queue gauges only)
//
// Forbidden metric labels (never use raw identifiers or free text):
//   patient_id, appointment_id, intent_id, attempt_id, recipient, email,
//   idempotency_key, error, error_message, request_id, correlation_id,
//   subject, notification body, template content, raw provider payload,
//   DATABASE_URL, Graph tokens, tenant/client secrets, worker_id, hostname,
//   instance, pod, replica
//
// Unknown channel/provider values: drop observation (do not invent "other").
//
// Queue gauges (5D) are GLOBAL DB-state snapshots cached in-memory after Tick.
// DO NOT SUM them across worker replicas — prefer max by (channel) or one target.
// GET /metrics never queries the database.

const (
	metricTicksTotal              = "medcore_notification_worker_ticks_total"
	metricTickDurationSeconds     = "medcore_notification_worker_tick_duration_seconds"
	metricClaimedTotal            = "medcore_notification_worker_claimed_total"
	metricStaleRecoveredTotal     = "medcore_notification_worker_stale_recovered_total"
	metricDeliveryAttemptsTotal   = "medcore_notification_delivery_attempts_total"
	metricProviderDurationSeconds = "medcore_notification_provider_duration_seconds"
	metricQueuePending            = "medcore_notification_queue_pending"
	metricQueueDue                = "medcore_notification_queue_due"
	metricQueueProcessing         = "medcore_notification_queue_processing"
	metricQueueStaleProcessing    = "medcore_notification_queue_stale_processing"
	metricQueueOldestDueAgeSecs   = "medcore_notification_queue_oldest_due_age_seconds"
	tickResultSuccess             = "success"
	tickResultError               = "error"
)

var queueMetricChannels = []patient_queue.MetricChannel{
	patient_queue.MetricChannelLog,
	patient_queue.MetricChannelEmail,
	patient_queue.MetricChannelSMS,
}

// tickDurationBuckets are locked for LOT 26I-5B (seconds).
var tickDurationBuckets = []float64{
	0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300, 600,
}

// providerDurationBuckets are locked for LOT 26I-5C (seconds).
var providerDurationBuckets = []float64{
	0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120,
}

// NewWorkerMetricsRegistry returns a Prometheus registry owned by the worker.
// It does NOT use prometheus.DefaultRegisterer / DefaultGatherer.
// Go/process collectors are intentionally omitted.
func NewWorkerMetricsRegistry() *prometheus.Registry {
	return prometheus.NewRegistry()
}

// NewMetricsHandler exposes gatherer via promhttp.HandlerFor.
// A nil gatherer returns 503 "unavailable" without DB or business work.
func NewMetricsHandler(g prometheus.Gatherer) http.Handler {
	if g == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("unavailable"))
		})
	}
	return promhttp.HandlerFor(g, promhttp.HandlerOpts{})
}

// WorkerMetrics is the Prometheus-backed WorkerLoopObserver + queue snapshot observer
// (LOT 26I-5B / 5C / 5D).
type WorkerMetrics struct {
	ticks            *prometheus.CounterVec
	tickDuration     prometheus.Histogram
	claimed          prometheus.Counter
	staleRecovered   prometheus.Counter
	deliveryAttempts *prometheus.CounterVec
	providerDuration *prometheus.HistogramVec
	queuePending     *prometheus.GaugeVec
	queueDue         *prometheus.GaugeVec
	queueProcessing  *prometheus.GaugeVec
	queueStale       *prometheus.GaugeVec
	queueOldestAge   *prometheus.GaugeVec
}

// NewWorkerMetrics registers 5B+5C+5D collectors on the given private Registerer.
// Registration failure returns an error (startup fail-closed).
func NewWorkerMetrics(reg prometheus.Registerer) (*WorkerMetrics, error) {
	if reg == nil {
		return nil, fmt.Errorf("worker metrics: registerer required")
	}
	m := &WorkerMetrics{
		ticks: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: metricTicksTotal,
			Help: "Total number of notification worker Tick executions by result.",
		}, []string{"result"}),
		tickDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    metricTickDurationSeconds,
			Help:    "Wall-clock duration of notification worker Tick executions.",
			Buckets: tickDurationBuckets,
		}),
		claimed: prometheus.NewCounter(prometheus.CounterOpts{
			Name: metricClaimedTotal,
			Help: "Total notification intents successfully claimed into PROCESSING.",
		}),
		staleRecovered: prometheus.NewCounter(prometheus.CounterOpts{
			Name: metricStaleRecoveredTotal,
			Help: "Total stale PROCESSING notification intents successfully recovered.",
		}),
		deliveryAttempts: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: metricDeliveryAttemptsTotal,
			Help: "Total notification delivery attempt handling outcomes by channel and outcome.",
		}, []string{"channel", "outcome"}),
		providerDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    metricProviderDurationSeconds,
			Help:    "Wall-clock duration of notification provider Send calls by channel and provider.",
			Buckets: providerDurationBuckets,
		}, []string{"channel", "provider"}),
		queuePending: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: metricQueuePending,
			Help: "Cached count of PENDING notification intents by channel (global DB snapshot).",
		}, []string{"channel"}),
		queueDue: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: metricQueueDue,
			Help: "Cached count of due PENDING notification intents (send_after <= asOf) by channel.",
		}, []string{"channel"}),
		queueProcessing: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: metricQueueProcessing,
			Help: "Cached count of PROCESSING notification intents by channel (global DB snapshot).",
		}, []string{"channel"}),
		queueStale: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: metricQueueStaleProcessing,
			Help: "Cached count of stale PROCESSING notification intents by channel (global DB snapshot).",
		}, []string{"channel"}),
		queueOldestAge: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: metricQueueOldestDueAgeSecs,
			Help: "Cached age in seconds of the oldest due PENDING intent by channel (global DB snapshot).",
		}, []string{"channel"}),
	}
	collectors := []prometheus.Collector{
		m.ticks, m.tickDuration, m.claimed, m.staleRecovered,
		m.deliveryAttempts, m.providerDuration,
		m.queuePending, m.queueDue, m.queueProcessing, m.queueStale, m.queueOldestAge,
	}
	for _, c := range collectors {
		if err := reg.Register(c); err != nil {
			return nil, fmt.Errorf("worker metrics register: %w", err)
		}
	}
	return m, nil
}

// ObserveTick implements patient_queue.WorkerLoopObserver.
func (m *WorkerMetrics) ObserveTick(duration time.Duration, err error) {
	if m == nil {
		return
	}
	result := tickResultSuccess
	if err != nil {
		result = tickResultError
	}
	m.ticks.WithLabelValues(result).Inc()
	m.tickDuration.Observe(duration.Seconds())
}

// ObserveClaimed implements patient_queue.WorkerLoopObserver.
func (m *WorkerMetrics) ObserveClaimed(n int) {
	if m == nil || n <= 0 {
		return
	}
	m.claimed.Add(float64(n))
}

// ObserveStaleRecovered implements patient_queue.WorkerLoopObserver.
func (m *WorkerMetrics) ObserveStaleRecovered(n int) {
	if m == nil || n <= 0 {
		return
	}
	m.staleRecovered.Add(float64(n))
}

// ObserveDeliveryAttempt implements patient_queue.WorkerLoopObserver.
func (m *WorkerMetrics) ObserveDeliveryAttempt(channel patient_queue.MetricChannel, outcome patient_queue.DeliveryOutcome) {
	if m == nil || !allowedMetricChannel(channel) || !allowedDeliveryOutcome(outcome) {
		return
	}
	m.deliveryAttempts.WithLabelValues(string(channel), string(outcome)).Inc()
}

// ObserveProviderDuration implements patient_queue.WorkerLoopObserver.
func (m *WorkerMetrics) ObserveProviderDuration(channel patient_queue.MetricChannel, provider patient_queue.MetricProvider, duration time.Duration) {
	if m == nil || !allowedProviderPair(channel, provider) {
		return
	}
	m.providerDuration.WithLabelValues(string(channel), string(provider)).Observe(duration.Seconds())
}

// ObserveQueueSnapshot implements patient_queue.NotificationQueueSnapshotObserver (LOT 26I-5D).
func (m *WorkerMetrics) ObserveQueueSnapshot(snap patient_queue.NotificationQueueSnapshot) {
	if m == nil {
		return
	}
	m.ApplyQueueSnapshot(snap)
}

// ApplyQueueSnapshot zero-fills allowed channels then applies validated snapshot values.
// Unknown domain channels are dropped (no "other"). Does not panic on dynamic labels.
func (m *WorkerMetrics) ApplyQueueSnapshot(snap patient_queue.NotificationQueueSnapshot) {
	if m == nil {
		return
	}
	type vals struct {
		pending, due, processing, stale float64
		oldestAge                       float64
	}
	byCh := make(map[patient_queue.MetricChannel]vals, len(queueMetricChannels))
	for _, ch := range queueMetricChannels {
		byCh[ch] = vals{} // zero-fill contract
	}
	for _, c := range snap.Channels {
		ch, ok := patient_queue.QueueMetricChannelFromDomain(c.Channel)
		if !ok || !allowedQueueMetricChannel(ch) {
			continue
		}
		if _, allowed := byCh[ch]; !allowed {
			continue
		}
		byCh[ch] = vals{
			pending:    float64(c.Pending),
			due:        float64(c.Due),
			processing: float64(c.Processing),
			stale:      float64(c.StaleProcessing),
			oldestAge:  c.OldestDueAge.Seconds(),
		}
	}
	for ch, v := range byCh {
		label := string(ch)
		m.queuePending.WithLabelValues(label).Set(v.pending)
		m.queueDue.WithLabelValues(label).Set(v.due)
		m.queueProcessing.WithLabelValues(label).Set(v.processing)
		m.queueStale.WithLabelValues(label).Set(v.stale)
		m.queueOldestAge.WithLabelValues(label).Set(v.oldestAge)
	}
}

func allowedMetricChannel(ch patient_queue.MetricChannel) bool {
	return ch == patient_queue.MetricChannelLog || ch == patient_queue.MetricChannelEmail
}

func allowedQueueMetricChannel(ch patient_queue.MetricChannel) bool {
	return ch == patient_queue.MetricChannelLog ||
		ch == patient_queue.MetricChannelEmail ||
		ch == patient_queue.MetricChannelSMS
}

func allowedDeliveryOutcome(o patient_queue.DeliveryOutcome) bool {
	switch o {
	case patient_queue.DeliveryOutcomeSent,
		patient_queue.DeliveryOutcomeSkipped,
		patient_queue.DeliveryOutcomePermanent,
		patient_queue.DeliveryOutcomeInvalidMessage,
		patient_queue.DeliveryOutcomeNotConfigured,
		patient_queue.DeliveryOutcomeAmbiguous,
		patient_queue.DeliveryOutcomeTransient,
		patient_queue.DeliveryOutcomeCanceled,
		patient_queue.DeliveryOutcomeInvalidPayload,
		patient_queue.DeliveryOutcomeAdapterUnavailable:
		return true
	default:
		return false
	}
}

func allowedProviderPair(ch patient_queue.MetricChannel, p patient_queue.MetricProvider) bool {
	switch {
	case ch == patient_queue.MetricChannelLog && p == patient_queue.MetricProviderLog:
		return true
	case ch == patient_queue.MetricChannelEmail && p == patient_queue.MetricProviderMicrosoft365:
		return true
	default:
		return false
	}
}

// Ensure WorkerMetrics satisfies observer interfaces at compile time.
var (
	_ patient_queue.WorkerLoopObserver                = (*WorkerMetrics)(nil)
	_ patient_queue.NotificationQueueSnapshotObserver = (*WorkerMetrics)(nil)
)
