package main

import (
	"fmt"
	"net/http"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/patient_queue"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Worker metrics privacy / cardinality contract (LOT 26I-5A / 26I-5B).
//
// Allowed FUTURE label dimensions (bounded enums only, validated at registration):
//   channel, kind, outcome_class, provider, operation
//
// 26I-5B uses only:
//   result ∈ {success, error} on ticks_total
//
// Forbidden metric labels (never use raw identifiers or free text):
//   patient_id, appointment_id, intent_id, attempt_id, recipient, email,
//   idempotency_key, error, error_message, request_id, correlation_id,
//   subject, notification body, template content, raw provider payload,
//   DATABASE_URL, Graph tokens, tenant/client secrets, worker_id, hostname
//
// Unknown future enum values must collapse to "other" or "unknown".

const (
	metricTicksTotal          = "medcore_notification_worker_ticks_total"
	metricTickDurationSeconds = "medcore_notification_worker_tick_duration_seconds"
	metricClaimedTotal        = "medcore_notification_worker_claimed_total"
	metricStaleRecoveredTotal = "medcore_notification_worker_stale_recovered_total"
	tickResultSuccess         = "success"
	tickResultError           = "error"
)

// tickDurationBuckets are locked for LOT 26I-5B (seconds).
var tickDurationBuckets = []float64{
	0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300, 600,
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

// WorkerMetrics is the Prometheus-backed WorkerLoopObserver (LOT 26I-5B).
type WorkerMetrics struct {
	ticks          *prometheus.CounterVec
	tickDuration   prometheus.Histogram
	claimed        prometheus.Counter
	staleRecovered prometheus.Counter
}

// NewWorkerMetrics registers 5B collectors on the given private Registerer.
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
	}
	collectors := []prometheus.Collector{m.ticks, m.tickDuration, m.claimed, m.staleRecovered}
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

// Ensure WorkerMetrics satisfies the observer interface at compile time.
var _ patient_queue.WorkerLoopObserver = (*WorkerMetrics)(nil)
