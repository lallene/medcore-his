package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseNotificationWorkerHealthPort(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		raw     string
		want    int
		wantErr bool
	}{
		{"unset", "", DefaultNotificationWorkerHealthPort, false},
		{"blank", "   ", DefaultNotificationWorkerHealthPort, false},
		{"default_explicit", "8081", 8081, false},
		{"padded", "  9090  ", 9090, false},
		{"malformed", "abc", 0, true},
		{"zero", "0", 0, true},
		{"negative", "-1", 0, true},
		{"too_high", "65536", 0, true},
		{"max", "65535", 65535, false},
		{"one", "1", 1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseNotificationWorkerHealthPort(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				if !strings.Contains(err.Error(), EnvNotificationWorkerHealthPort) {
					t.Fatalf("error should name key: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("got %d want %d", got, tc.want)
			}
		})
	}
}

func TestHealthzIndependentOfDB(t *testing.T) {
	t.Parallel()
	state := &HealthState{}
	var pings atomic.Int32
	ping := func(context.Context) error {
		pings.Add(1)
		return errors.New("sekrit-LEAK-TEST-XYZ-26I4 db down")
	}
	hs := NewHealthServer("127.0.0.1:0", state, ping)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	hs.handleHealthz(rec, req)
	if rec.Code != http.StatusServiceUnavailable || rec.Body.String() != "unavailable" {
		t.Fatalf("before start: code=%d body=%q", rec.Code, rec.Body.String())
	}

	state.MarkStarted()
	rec = httptest.NewRecorder()
	hs.handleHealthz(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Fatalf("after start: code=%d body=%q", rec.Code, rec.Body.String())
	}
	if pings.Load() != 0 {
		t.Fatal("healthz must not call DB ping")
	}
}

func TestReadyzRequiresStartAndDBPing(t *testing.T) {
	t.Parallel()
	state := &HealthState{}
	var fail atomic.Bool
	fail.Store(true)
	const marker = "sekrit-LEAK-TEST-XYZ-26I4"
	ping := func(context.Context) error {
		if fail.Load() {
			return fmt.Errorf("%s postgres://user:%s@host/db", marker, marker)
		}
		return nil
	}
	hs := NewHealthServer("127.0.0.1:0", state, ping)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	hs.handleReadyz(rec, req)
	if rec.Code != http.StatusServiceUnavailable || rec.Body.String() != "unavailable" {
		t.Fatalf("not started: code=%d body=%q", rec.Code, rec.Body.String())
	}

	state.MarkStarted()
	rec = httptest.NewRecorder()
	hs.handleReadyz(rec, req)
	if rec.Code != http.StatusServiceUnavailable || rec.Body.String() != "unavailable" {
		t.Fatalf("ping fail: code=%d body=%q", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, marker) || strings.Contains(body, "postgres://") {
		t.Fatalf("leaked DB/secret text: %q", body)
	}

	fail.Store(false)
	rec = httptest.NewRecorder()
	hs.handleReadyz(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "ready" {
		t.Fatalf("ping ok: code=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestShutdownFailsLiveAndReady(t *testing.T) {
	t.Parallel()
	state := &HealthState{}
	state.MarkStarted()
	hs := NewHealthServer("127.0.0.1:0", state, func(context.Context) error { return nil })

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	hs.handleReadyz(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("pre-shutdown ready: %d", rec.Code)
	}

	state.MarkShuttingDown()
	rec = httptest.NewRecorder()
	hs.handleHealthz(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("healthz after shutdown: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	hs.handleReadyz(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz after shutdown: %d", rec.Code)
	}
}

func TestStoppedFailsLive(t *testing.T) {
	t.Parallel()
	state := &HealthState{}
	state.MarkStarted()
	state.MarkStopped()
	hs := NewHealthServer("127.0.0.1:0", state, nil)
	rec := httptest.NewRecorder()
	hs.handleHealthz(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("code=%d", rec.Code)
	}
}

func TestHealthServerListenServeShutdown(t *testing.T) {
	t.Parallel()
	state := &HealthState{}
	hs := NewHealthServer("127.0.0.1:0", state, func(context.Context) error { return nil })
	ln, err := hs.Listen()
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	errCh := make(chan error, 1)
	go func() { errCh <- hs.Serve(ln) }()

	state.MarkStarted()

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + addr + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "ok" {
		t.Fatalf("healthz=%d %q", resp.StatusCode, body)
	}

	resp, err = client.Get("http://" + addr + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "ready" {
		t.Fatalf("readyz=%d %q", resp.StatusCode, body)
	}

	state.MarkShuttingDown()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := hs.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Serve did not return after Shutdown")
	}

	resp, err = client.Get("http://" + addr + "/readyz")
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("expected connection error after shutdown")
	}
}

func TestUnsupportedPath(t *testing.T) {
	t.Parallel()
	hs := NewHealthServer("127.0.0.1:0", &HealthState{}, nil)
	rec := httptest.NewRecorder()
	hs.server.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("code=%d", rec.Code)
	}
}

func TestNilPingReadyUnavailable(t *testing.T) {
	t.Parallel()
	state := &HealthState{}
	state.MarkStarted()
	hs := NewHealthServer("127.0.0.1:0", state, nil)
	rec := httptest.NewRecorder()
	hs.handleReadyz(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("code=%d", rec.Code)
	}
}
