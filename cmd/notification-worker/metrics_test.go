package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
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
	c := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "medcore_test_metric",
		Help: "test-only counter for private registry isolation",
	})
	if err := regA.Register(c); err != nil {
		t.Fatal(err)
	}
	c.Inc()

	hsA := NewHealthServer("127.0.0.1:0", state, nil, NewMetricsHandler(regA))
	recA := httptest.NewRecorder()
	hsA.server.Handler.ServeHTTP(recA, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if recA.Code != http.StatusOK {
		t.Fatalf("A code=%d", recA.Code)
	}
	bodyA := recA.Body.String()
	if !strings.Contains(bodyA, "medcore_test_metric") {
		t.Fatalf("expected medcore_test_metric in A exposition")
	}

	regB := NewWorkerMetricsRegistry()
	hsB := NewHealthServer("127.0.0.1:0", state, nil, NewMetricsHandler(regB))
	recB := httptest.NewRecorder()
	hsB.server.Handler.ServeHTTP(recB, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if recB.Code != http.StatusOK {
		t.Fatalf("B code=%d", recB.Code)
	}
	if strings.Contains(recB.Body.String(), "medcore_test_metric") {
		t.Fatal("private registry B must not expose A's test metric")
	}
}

func TestMetricsFoundationDoesNotExposeSyntheticSecrets(t *testing.T) {
	t.Parallel()
	// Production foundation registry is empty of business metrics and must not
	// embed application/private markers in default exposition.
	markers := []string{
		"patient-SECRET-MARKER",
		"patient@example.invalid",
		"appointment-SECRET-MARKER",
		"postgres://SECRET-MARKER",
		"Bearer SECRET-MARKER",
		"correlation-SECRET-MARKER",
	}
	reg := NewWorkerMetricsRegistry()
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
