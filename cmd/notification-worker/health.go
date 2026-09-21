package main

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// EnvNotificationWorkerHealthPort is the worker-only health HTTP listen port.
const EnvNotificationWorkerHealthPort = "NOTIFICATION_WORKER_HEALTH_PORT"

// DefaultNotificationWorkerHealthPort is used when the env is unset/blank.
const DefaultNotificationWorkerHealthPort = 8081

// healthDBPingTimeout bounds each /readyz database probe.
const healthDBPingTimeout = time.Second

// HealthState is concurrency-safe liveness/readiness bookkeeping for the worker.
type HealthState struct {
	mu           sync.RWMutex
	started      bool
	shuttingDown bool
	stopped      bool
}

// MarkStarted records that worker.Run has begun.
func (s *HealthState) MarkStarted() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.started = true
}

// MarkShuttingDown records SIGINT/SIGTERM handling; live and ready both fail.
func (s *HealthState) MarkShuttingDown() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shuttingDown = true
}

// MarkStopped records that worker.Run returned (expected or unexpected).
func (s *HealthState) MarkStopped() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped = true
}

// Live reports process/run-loop liveness (independent of DB / Graph).
func (s *HealthState) Live() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.started && !s.shuttingDown && !s.stopped
}

// ReadyForProbe reports whether readiness may succeed aside from DB ping.
func (s *HealthState) ReadyForProbe() bool {
	return s.Live()
}

// ParseNotificationWorkerHealthPort interprets NOTIFICATION_WORKER_HEALTH_PORT.
// Unset/empty/whitespace → 8081. Explicit invalid values fail closed.
func ParseNotificationWorkerHealthPort(raw string) (int, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return DefaultNotificationWorkerHealthPort, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("%s: valeur invalide (port 1–65535 attendu)", EnvNotificationWorkerHealthPort)
	}
	if n < 1 || n > 65535 {
		return 0, fmt.Errorf("%s: port hors plage (1–65535)", EnvNotificationWorkerHealthPort)
	}
	return n, nil
}

// dbPinger is the readiness probe. Production uses PingFromSQLDB.
type dbPinger func(ctx context.Context) error

// PingFromSQLDB adapts *sql.DB for /readyz probes.
func PingFromSQLDB(db *sql.DB) dbPinger {
	return func(ctx context.Context) error {
		if db == nil {
			return fmt.Errorf("database unavailable")
		}
		return db.PingContext(ctx)
	}
}

// HealthServer serves GET /healthz, GET /readyz, and optionally GET /metrics.
type HealthServer struct {
	state  *HealthState
	ping   dbPinger
	server *http.Server
}

// NewHealthServer builds a dedicated health HTTP server (stdlib net/http).
// metricsHandler, when non-nil, is mounted at GET /metrics (LOT 26I-5A).
// A nil metricsHandler leaves /metrics unregistered (404). Metrics must not
// influence /healthz or /readyz and must not perform DB work.
func NewHealthServer(addr string, state *HealthState, ping dbPinger, metricsHandler http.Handler) *HealthServer {
	hs := &HealthServer{state: state, ping: ping}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", hs.handleHealthz)
	mux.HandleFunc("GET /readyz", hs.handleReadyz)
	if metricsHandler != nil {
		mux.Handle("GET /metrics", metricsHandler)
	}
	hs.server = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       3 * time.Second,
		WriteTimeout:      3 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	return hs
}

func (hs *HealthServer) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	if !hs.state.Live() {
		writeHealthUnavailable(w)
		return
	}
	writeHealthOK(w, "ok")
}

func (hs *HealthServer) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if !hs.state.ReadyForProbe() {
		writeHealthUnavailable(w)
		return
	}
	if hs.ping == nil {
		writeHealthUnavailable(w)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), healthDBPingTimeout)
	defer cancel()
	if err := hs.ping(ctx); err != nil {
		writeHealthUnavailable(w)
		return
	}
	writeHealthOK(w, "ready")
}

func writeHealthOK(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
}

func writeHealthUnavailable(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte("unavailable"))
}

// Listen starts listening so Serve can run in a goroutine. Fail closed on error.
func (hs *HealthServer) Listen() (net.Listener, error) {
	return net.Listen("tcp", hs.server.Addr)
}

// Serve serves until the listener closes or Shutdown is called.
func (hs *HealthServer) Serve(ln net.Listener) error {
	err := hs.server.Serve(ln)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// Shutdown gracefully stops the health HTTP server.
func (hs *HealthServer) Shutdown(ctx context.Context) error {
	return hs.server.Shutdown(ctx)
}
