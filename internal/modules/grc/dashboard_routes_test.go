package grc

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestGRCDashboardRouteRegistered(t *testing.T) {
	r := grcTestRouter(nil)

	var methods []string
	for _, route := range r.Routes() {
		if route.Path == "/api/grc/dashboard" {
			methods = append(methods, route.Method)
		}
	}
	if len(methods) != 1 || methods[0] != http.MethodGet {
		t.Fatalf("dashboard routes = %v, want only GET", methods)
	}

	wildcard := grcTestRouter([]string{"*"})
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		if w := grcRequest(wildcard, method, "/api/grc/dashboard"); w.Code != http.StatusNotFound {
			t.Errorf("%s /api/grc/dashboard status=%d, want 404", method, w.Code)
		}
	}
}

func TestGRCDashboardRouteRBAC(t *testing.T) {
	// Malformed window: the handler answers 400 before the (nil) service,
	// so 401/403 prove the middleware runs first.
	const path = "/api/grc/dashboard?expiringSoonDays=abc"
	cases := []struct {
		name        string
		permissions []string
		want        int
	}{
		{"unauthenticated", nil, http.StatusUnauthorized},
		{"no permission", []string{}, http.StatusForbidden},
		{"unrelated permissions", []string{"grc.risk.read", "grc.evidence.read", "dashboard.read"}, http.StatusForbidden},
		{"dashboard permission", []string{"grc.dashboard.read"}, http.StatusBadRequest},
		{"wildcard", []string{"*"}, http.StatusBadRequest},
	}
	for _, tc := range cases {
		if w := grcRequest(grcTestRouter(tc.permissions), http.MethodGet, path); w.Code != tc.want {
			t.Errorf("%s: status=%d, want %d", tc.name, w.Code, tc.want)
		}
	}
}

func TestGRCDashboardRouteRejectsInvalidWindows(t *testing.T) {
	_, svc := newDashboardService(t)
	r := grcServiceRouter(svc, []string{"grc.dashboard.read"})

	for _, query := range []string{
		"expiringSoonDays=0", "expiringSoonDays=366", "expiringSoonDays=abc", "expiringSoonDays=1.5",
		"dueSoonDays=0", "dueSoonDays=366", "dueSoonDays=-3", "dueSoonDays=x",
	} {
		if w := grcRequest(r, http.MethodGet, "/api/grc/dashboard?"+query); w.Code != http.StatusBadRequest {
			t.Errorf("%s: status=%d, want 400", query, w.Code)
		}
	}
}

func TestGRCDashboardRouteReturnsAggregates(t *testing.T) {
	db, svc := newDashboardService(t)
	seedDashboardDataset(t, db, dashboardTestNow)
	bus := withCaptureBus(t)

	for _, permissions := range [][]string{{"grc.dashboard.read"}, {"*"}} {
		w := grcRequest(grcServiceRouter(svc, permissions), http.MethodGet, "/api/grc/dashboard")
		if w.Code != http.StatusOK {
			t.Fatalf("%v: status=%d body=%s", permissions, w.Code, w.Body.String())
		}
		var d GRCDashboard
		if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
			t.Fatal(err)
		}
		requireDashboardDataset(t, &d, dashboardTestNow)
	}

	// The JSON contract uses camelCase keys and returns the windows.
	w := grcRequest(grcServiceRouter(svc, []string{"grc.dashboard.read"}), http.MethodGet,
		"/api/grc/dashboard?expiringSoonDays=1&dueSoonDays=365")
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	windows, _ := body["windows"].(map[string]any)
	if windows["expiringSoonDays"] != float64(1) || windows["dueSoonDays"] != float64(365) {
		t.Fatalf("unexpected windows: %v", body["windows"])
	}
	summary, _ := body["summary"].(map[string]any)
	if summary["controlMappingCoveragePercent"] != 33.33 {
		t.Fatalf("unexpected summary: %v", body["summary"])
	}
	if generated, _ := body["generatedAt"].(string); generated != dashboardTestNow.Format(time.RFC3339) {
		t.Fatalf("generatedAt=%v", body["generatedAt"])
	}

	if len(bus.events) != 0 {
		t.Fatalf("a dashboard read must not publish audit events: %d", len(bus.events))
	}
}
