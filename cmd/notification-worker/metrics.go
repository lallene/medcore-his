package main

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Worker metrics privacy / cardinality contract (LOT 26I-5A foundation).
//
// Allowed FUTURE label dimensions (bounded enums only, validated at registration):
//   channel, kind, outcome_class, provider, operation
//
// Forbidden metric labels (never use raw identifiers or free text):
//   patient_id, appointment_id, intent_id, attempt_id, recipient, email,
//   idempotency_key, error, error_message, request_id, correlation_id,
//   subject, notification body, template content, raw provider payload,
//   DATABASE_URL, Graph tokens, tenant/client secrets
//
// Unknown future enum values must collapse to "other" or "unknown".
// 26I-5A registers no business metrics — later slices own instrumentation.

// NewWorkerMetricsRegistry returns a Prometheus registry owned by the worker.
// It does NOT use prometheus.DefaultRegisterer / DefaultGatherer.
// Go/process collectors are intentionally omitted in 26I-5A.
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
	return promhttp.HandlerFor(g, promhttp.HandlerOpts{
		// Do not enable OpenMetrics negotiation extras beyond defaults.
		// Errors stay generic; no scrape logging.
	})
}
