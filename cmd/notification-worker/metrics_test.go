package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/patient_queue"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestMetricsEndpointDoesNotPingDB(t *testing.T) {
	t.Parallel()
	state := &HealthState{}
	state.MarkStarted()
	var pings atomic.Int32
	ping := func(context.Context) error {
		pings.Add(1)
		return nil
	}
	reg := NewWorkerMetricsRegistry()
	if _, err := NewWorkerMetrics(reg); err != nil {
		t.Fatal(err)
	}
	hs := NewHealthServer("127.0.0.1:0", state, ping, NewMetricsHandler(reg))

	rec := httptest.NewRecorder()
	hs.server.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("metrics code=%d body=%q", rec.Code, rec.Body.String())
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/plain") && !strings.Contains(ct, "openmetrics") {
		t.Fatalf("unexpected Content-Type %q", ct)
	}
	if pings.Load() != 0 {
		t.Fatalf("GET /metrics must not call DB ping; got %d", pings.Load())
	}

	rec = httptest.NewRecorder()
	hs.server.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("readyz code=%d", rec.Code)
	}
	if pings.Load() != 1 {
		t.Fatalf("readyz should ping once; got %d", pings.Load())
	}
}

func TestMetricsPrivateRegistryIsolation(t *testing.T) {
	t.Parallel()
	state := &HealthState{}
	state.MarkStarted()

	regA := NewWorkerMetricsRegistry()
	wmA, err := NewWorkerMetrics(regA)
	if err != nil {
		t.Fatal(err)
	}
	wmA.ObserveTick(10*time.Millisecond, nil)
	wmA.ObserveClaimed(2)

	hsA := NewHealthServer("127.0.0.1:0", state, nil, NewMetricsHandler(regA))
	recA := httptest.NewRecorder()
	hsA.server.Handler.ServeHTTP(recA, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if recA.Code != http.StatusOK {
		t.Fatalf("A code=%d", recA.Code)
	}
	bodyA := recA.Body.String()
	if !strings.Contains(bodyA, metricTicksTotal) {
		t.Fatalf("expected %s in A exposition", metricTicksTotal)
	}
	if !strings.Contains(bodyA, `result="success"`) {
		t.Fatal("expected result=success in A")
	}

	regB := NewWorkerMetricsRegistry()
	if _, err := NewWorkerMetrics(regB); err != nil {
		t.Fatal(err)
	}
	hsB := NewHealthServer("127.0.0.1:0", state, nil, NewMetricsHandler(regB))
	recB := httptest.NewRecorder()
	hsB.server.Handler.ServeHTTP(recB, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if recB.Code != http.StatusOK {
		t.Fatalf("B code=%d", recB.Code)
	}
	bodyB := recB.Body.String()
	if strings.Contains(bodyB, `result="success"`) {
		t.Fatal("private registry B must not inherit A's tick observations")
	}
	if strings.Contains(bodyB, metricClaimedTotal+" 2") {
		t.Fatal("private registry B must not inherit A's claimed count")
	}
}

func TestMetricsFoundationDoesNotExposeSyntheticSecrets(t *testing.T) {
	t.Parallel()
	markers := []string{
		"patient-SECRET-MARKER",
		"patient@example.invalid",
		"appointment-SECRET-MARKER",
		"postgres://SECRET-MARKER",
		"Bearer SECRET-MARKER",
		"correlation-SECRET-MARKER",
		"patient_id",
		"appointment_id",
		"intent_id",
		"attempt_id",
		"recipient",
		"worker_id",
		"hostname",
		"correlation_id",
		"request_id",
	}
	reg := NewWorkerMetricsRegistry()
	wm, err := NewWorkerMetrics(reg)
	if err != nil {
		t.Fatal(err)
	}
	wm.ObserveTick(time.Millisecond, errors.New("should-not-appear-as-label"))
	wm.ObserveClaimed(1)
	wm.ObserveStaleRecovered(1)

	hs := NewHealthServer("127.0.0.1:0", &HealthState{}, nil, NewMetricsHandler(reg))
	rec := httptest.NewRecorder()
	hs.server.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d", rec.Code)
	}
	body := rec.Body.String()
	for _, m := range markers {
		if strings.Contains(body, m) {
			t.Fatalf("metrics leaked marker %q", m)
		}
	}
	if strings.Contains(body, "should-not-appear-as-label") {
		t.Fatal("error string must not appear in exposition")
	}
}

func TestMetricsWrongMethodNoBusinessWork(t *testing.T) {
	t.Parallel()
	var pings atomic.Int32
	ping := func(context.Context) error {
		pings.Add(1)
		return nil
	}
	state := &HealthState{}
	state.MarkStarted()
	hs := NewHealthServer("127.0.0.1:0", state, ping, NewMetricsHandler(NewWorkerMetricsRegistry()))
	rec := httptest.NewRecorder()
	hs.server.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/metrics", nil))
	if rec.Code == http.StatusOK {
		t.Fatalf("POST /metrics must not succeed as GET; code=%d", rec.Code)
	}
	if pings.Load() != 0 {
		t.Fatal("wrong method must not invoke readiness ping")
	}
}

func TestNilMetricsHandlerLeavesMetricsUnregistered(t *testing.T) {
	t.Parallel()
	hs := NewHealthServer("127.0.0.1:0", &HealthState{}, nil, nil)
	rec := httptest.NewRecorder()
	hs.server.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("nil metrics handler: want 404, got %d", rec.Code)
	}
}

func TestNilGathererMetricsUnavailable(t *testing.T) {
	t.Parallel()
	hs := NewHealthServer("127.0.0.1:0", &HealthState{}, nil, NewMetricsHandler(nil))
	rec := httptest.NewRecorder()
	hs.server.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusServiceUnavailable || rec.Body.String() != "unavailable" {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestMetricsLiveServer(t *testing.T) {
	t.Parallel()
	state := &HealthState{}
	state.MarkStarted()
	reg := NewWorkerMetricsRegistry()
	if _, err := NewWorkerMetrics(reg); err != nil {
		t.Fatal(err)
	}
	hs := NewHealthServer("127.0.0.1:0", state, func(context.Context) error { return nil }, NewMetricsHandler(reg))
	ln, err := hs.Listen()
	if err != nil {
		t.Fatal(err)
	}
	errCh := make(chan error, 1)
	go func() { errCh <- hs.Serve(ln) }()
	t.Cleanup(func() {
		_ = hs.Shutdown(context.Background())
		<-errCh
	})

	resp, err := http.Get("http://" + ln.Addr().String() + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%q", resp.StatusCode, body)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/plain") && !strings.Contains(ct, "openmetrics") {
		t.Fatalf("Content-Type=%q", ct)
	}
}

func TestWorkerMetricsObserveFamilies(t *testing.T) {
	t.Parallel()
	reg := NewWorkerMetricsRegistry()
	wm, err := NewWorkerMetrics(reg)
	if err != nil {
		t.Fatal(err)
	}

	wm.ObserveTick(25*time.Millisecond, nil)
	wm.ObserveTick(40*time.Millisecond, errors.New("boom"))
	wm.ObserveClaimed(5)
	wm.ObserveStaleRecovered(3)
	wm.ObserveClaimed(0)        // no-op
	wm.ObserveStaleRecovered(0) // no-op
	wm.ObserveDeliveryAttempt(patient_queue.MetricChannelLog, patient_queue.DeliveryOutcomeSent)
	wm.ObserveDeliveryAttempt(patient_queue.MetricChannelEmail, patient_queue.DeliveryOutcomeTransient)
	wm.ObserveProviderDuration(patient_queue.MetricChannelLog, patient_queue.MetricProviderLog, 10*time.Millisecond)
	wm.ObserveProviderDuration(patient_queue.MetricChannelEmail, patient_queue.MetricProviderMicrosoft365, 20*time.Millisecond)
	// Drops
	wm.ObserveDeliveryAttempt(patient_queue.MetricChannel("sms"), patient_queue.DeliveryOutcomeSent)
	wm.ObserveProviderDuration(patient_queue.MetricChannelEmail, patient_queue.MetricProvider("fake"), time.Millisecond)

	if got := testutil.ToFloat64(wm.ticks.WithLabelValues(tickResultSuccess)); got != 1 {
		t.Fatalf("success ticks=%v want 1", got)
	}
	if got := testutil.ToFloat64(wm.ticks.WithLabelValues(tickResultError)); got != 1 {
		t.Fatalf("error ticks=%v want 1", got)
	}
	if got := testutil.CollectAndCount(wm.tickDuration); got != 1 {
		t.Fatalf("histogram metric count=%d want 1", got)
	}
	if got := testutil.ToFloat64(wm.deliveryAttempts.WithLabelValues("log", "sent")); got != 1 {
		t.Fatalf("delivery log/sent=%v", got)
	}
	if got := testutil.ToFloat64(wm.deliveryAttempts.WithLabelValues("email", "transient")); got != 1 {
		t.Fatalf("delivery email/transient=%v", got)
	}
	// Histogram observation count via Gather
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	var histCount, providerCount uint64
	var claimed, stale float64
	for _, mf := range mfs {
		switch mf.GetName() {
		case metricTickDurationSeconds:
			for _, m := range mf.GetMetric() {
				histCount += m.GetHistogram().GetSampleCount()
			}
		case metricProviderDurationSeconds:
			for _, m := range mf.GetMetric() {
				providerCount += m.GetHistogram().GetSampleCount()
			}
		case metricClaimedTotal:
			for _, m := range mf.GetMetric() {
				claimed = m.GetCounter().GetValue()
			}
		case metricStaleRecoveredTotal:
			for _, m := range mf.GetMetric() {
				stale = m.GetCounter().GetValue()
			}
		}
	}
	if histCount != 2 {
		t.Fatalf("tick_duration sample_count=%d want 2", histCount)
	}
	if providerCount != 2 {
		t.Fatalf("provider_duration sample_count=%d want 2", providerCount)
	}
	if claimed != 5 {
		t.Fatalf("claimed=%v want 5", claimed)
	}
	if stale != 3 {
		t.Fatalf("stale=%v want 3", stale)
	}
}

func TestWorkerMetricsRegistrationFailClosed(t *testing.T) {
	t.Parallel()
	reg := NewWorkerMetricsRegistry()
	if _, err := NewWorkerMetrics(reg); err != nil {
		t.Fatal(err)
	}
	_, err := NewWorkerMetrics(reg)
	if err == nil {
		t.Fatal("duplicate registration must fail closed")
	}
}

func TestWorkerMetricsNilRegisterer(t *testing.T) {
	t.Parallel()
	_, err := NewWorkerMetrics(nil)
	if err == nil {
		t.Fatal("nil registerer must error")
	}
}

func TestWorkerMetricsNoDefaultRegistryLeak(t *testing.T) {
	t.Parallel()
	before, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatal(err)
	}
	reg := NewWorkerMetricsRegistry()
	wm, err := NewWorkerMetrics(reg)
	if err != nil {
		t.Fatal(err)
	}
	wm.ObserveTick(time.Millisecond, nil)
	after, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, mf := range after {
		name := mf.GetName()
		if name == metricTicksTotal || name == metricTickDurationSeconds ||
			name == metricClaimedTotal || name == metricStaleRecoveredTotal ||
			name == metricDeliveryAttemptsTotal || name == metricProviderDurationSeconds {
			t.Fatalf("worker metric %s leaked into DefaultGatherer", name)
		}
	}
	_ = before
}
