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
			name == metricDeliveryAttemptsTotal || name == metricProviderDurationSeconds ||
			name == metricQueuePending || name == metricQueueDue ||
			name == metricQueueProcessing || name == metricQueueStaleProcessing ||
			name == metricQueueOldestDueAgeSecs {
			t.Fatalf("worker metric %s leaked into DefaultGatherer", name)
		}
	}
	_ = before
}

func TestApplyQueueSnapshotZeroFillAndChannels(t *testing.T) {
	t.Parallel()
	reg := NewWorkerMetricsRegistry()
	wm, err := NewWorkerMetrics(reg)
	if err != nil {
		t.Fatal(err)
	}

	wm.ApplyQueueSnapshot(patient_queue.NotificationQueueSnapshot{
		Channels: []patient_queue.NotificationQueueChannelSnapshot{
			{
				Channel: patient_queue.NotifChannelLog,
				Pending: 3, Due: 2, Processing: 1, StaleProcessing: 1,
				OldestDueAge: 90 * time.Second,
			},
			{
				Channel: patient_queue.NotifChannelEmail,
				Pending: 5, Due: 4, Processing: 0, StaleProcessing: 0,
				OldestDueAge: 30 * time.Second,
			},
			{
				Channel: patient_queue.NotifChannelSMS,
				Pending: 1, Due: 1, Processing: 0, StaleProcessing: 0,
				OldestDueAge: 10 * time.Second,
			},
			{Channel: "FAX", Pending: 99, Due: 99}, // dropped
			{Channel: "other", Pending: 7},         // dropped — no "other" label
		},
	})

	assertGauge := func(name string, g *prometheus.GaugeVec, ch string, want float64) {
		t.Helper()
		if got := testutil.ToFloat64(g.WithLabelValues(ch)); got != want {
			t.Fatalf("%s{%s}=%v want %v", name, ch, got, want)
		}
	}
	assertGauge(metricQueuePending, wm.queuePending, "log", 3)
	assertGauge(metricQueueDue, wm.queueDue, "log", 2)
	assertGauge(metricQueueProcessing, wm.queueProcessing, "log", 1)
	assertGauge(metricQueueStaleProcessing, wm.queueStale, "log", 1)
	assertGauge(metricQueueOldestDueAgeSecs, wm.queueOldestAge, "log", 90)

	assertGauge(metricQueuePending, wm.queuePending, "email", 5)
	assertGauge(metricQueueDue, wm.queueDue, "email", 4)
	assertGauge(metricQueueOldestDueAgeSecs, wm.queueOldestAge, "email", 30)

	assertGauge(metricQueuePending, wm.queuePending, "sms", 1)
	assertGauge(metricQueueDue, wm.queueDue, "sms", 1)
	assertGauge(metricQueueOldestDueAgeSecs, wm.queueOldestAge, "sms", 10)

	// Empty successful snapshot → all channels zero (drain).
	wm.ApplyQueueSnapshot(patient_queue.NotificationQueueSnapshot{})
	for _, ch := range []string{"log", "email", "sms"} {
		assertGauge(metricQueuePending, wm.queuePending, ch, 0)
		assertGauge(metricQueueDue, wm.queueDue, ch, 0)
		assertGauge(metricQueueProcessing, wm.queueProcessing, ch, 0)
		assertGauge(metricQueueStaleProcessing, wm.queueStale, ch, 0)
		assertGauge(metricQueueOldestDueAgeSecs, wm.queueOldestAge, ch, 0)
	}

	body := gatherMetricsBody(t, reg)
	if strings.Contains(body, `channel="other"`) || strings.Contains(body, `channel="FAX"`) {
		t.Fatal("unknown channels must not appear as labels")
	}
	for _, name := range []string{
		metricQueuePending, metricQueueDue, metricQueueProcessing,
		metricQueueStaleProcessing, metricQueueOldestDueAgeSecs,
	} {
		if !strings.Contains(body, name) {
			t.Fatalf("missing family %s", name)
		}
	}
	// 5B/5C untouched by ApplyQueueSnapshot.
	if strings.Contains(body, `result="success"`) {
		t.Fatal("ApplyQueueSnapshot must not mutate tick counters")
	}
}

func TestQueueSnapshotRefreshFailureRetainsGauges(t *testing.T) {
	t.Parallel()
	reg := NewWorkerMetricsRegistry()
	wm, err := NewWorkerMetrics(reg)
	if err != nil {
		t.Fatal(err)
	}
	wm.ApplyQueueSnapshot(patient_queue.NotificationQueueSnapshot{
		Channels: []patient_queue.NotificationQueueChannelSnapshot{
			{Channel: patient_queue.NotifChannelLog, Pending: 11, Due: 7, OldestDueAge: 42 * time.Second},
		},
	})
	if got := testutil.ToFloat64(wm.queuePending.WithLabelValues("log")); got != 11 {
		t.Fatalf("pending=%v", got)
	}

	// Simulate worker best-effort: snapshot fails → ObserveQueueSnapshot / Apply not called.
	// Gauges must retain previous values (not zeroed).
	if got := testutil.ToFloat64(wm.queuePending.WithLabelValues("log")); got != 11 {
		t.Fatalf("retained pending=%v want 11", got)
	}
	if got := testutil.ToFloat64(wm.queueDue.WithLabelValues("log")); got != 7 {
		t.Fatalf("retained due=%v want 7", got)
	}
	if got := testutil.ToFloat64(wm.queueOldestAge.WithLabelValues("log")); got != 42 {
		t.Fatalf("retained age=%v want 42", got)
	}
}

func TestMetricsScrapeDoesNotCallQueueSnapshot(t *testing.T) {
	t.Parallel()
	state := &HealthState{}
	state.MarkStarted()
	var pings atomic.Int32
	ping := func(context.Context) error {
		pings.Add(1)
		return nil
	}
	reg := NewWorkerMetricsRegistry()
	wm, err := NewWorkerMetrics(reg)
	if err != nil {
		t.Fatal(err)
	}
	wm.ApplyQueueSnapshot(patient_queue.NotificationQueueSnapshot{
		Channels: []patient_queue.NotificationQueueChannelSnapshot{
			{Channel: patient_queue.NotifChannelEmail, Pending: 2},
		},
	})
	hs := NewHealthServer("127.0.0.1:0", state, ping, NewMetricsHandler(reg))

	rec := httptest.NewRecorder()
	hs.server.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d", rec.Code)
	}
	if pings.Load() != 0 {
		t.Fatal("GET /metrics must not ping DB")
	}
	body := rec.Body.String()
	if !strings.Contains(body, metricQueuePending) {
		t.Fatal("expected queue gauge in scrape")
	}
	if !strings.Contains(body, `channel="email"`) {
		t.Fatal("expected cached email series")
	}
	// No PHI / IDs / error strings.
	for _, m := range []string{"patient_id", "appointment_id", "intent_id", "FAX", `channel="other"`} {
		if strings.Contains(body, m) {
			t.Fatalf("leaked %q", m)
		}
	}
}

func gatherMetricsBody(t *testing.T, reg *prometheus.Registry) string {
	t.Helper()
	hs := NewHealthServer("127.0.0.1:0", &HealthState{}, nil, NewMetricsHandler(reg))
	rec := httptest.NewRecorder()
	hs.server.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d", rec.Code)
	}
	return rec.Body.String()
}
