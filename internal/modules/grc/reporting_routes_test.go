package grc

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestGRCReportingRouteRBAC(t *testing.T) {
	const path = "/api/grc/reporting?evidenceExpiringSoonDays=abc"
	cases := []struct {
		name        string
		permissions []string
		want        int
	}{
		{"unauthenticated", nil, http.StatusUnauthorized},
		{"no permission", []string{}, http.StatusForbidden},
		{"unrelated", []string{"grc.dashboard.read", "grc.risk.read"}, http.StatusForbidden},
		{"reporting permission", []string{"grc.reporting.read"}, http.StatusBadRequest},
		{"wildcard", []string{"*"}, http.StatusBadRequest},
	}
	for _, tc := range cases {
		if w := grcRequest(grcTestRouter(tc.permissions), http.MethodGet, path); w.Code != tc.want {
			t.Errorf("%s: status=%d, want %d", tc.name, w.Code, tc.want)
		}
	}
}

func TestGRCReportingRouteValidation(t *testing.T) {
	_, svc := newReportingService(t)
	r := grcServiceRouter(svc, []string{"grc.reporting.read"})
	for _, query := range []string{
		"evidenceExpiringSoonDays=0", "evidenceExpiringSoonDays=366", "evidenceExpiringSoonDays=abc",
		"actionDueSoonDays=0", "actionDueSoonDays=366", "actionDueSoonDays=-3",
		"frameworkIds=0", "frameworkIds=-1", "frameworkIds=1.5", "frameworkIds=abc", "frameworkIds=1,,2",
	} {
		if w := grcRequest(r, http.MethodGet, "/api/grc/reporting?"+query); w.Code != http.StatusBadRequest {
			t.Errorf("%s: status=%d body=%s", query, w.Code, w.Body.String())
		}
	}
	// 101 distinct positive ids exceed the bound even before they exist.
	query := "frameworkIds=" + strings.Join(numberedIDs(101), ",")
	if w := grcRequest(r, http.MethodGet, "/api/grc/reporting?"+query); w.Code != http.StatusBadRequest {
		t.Fatalf("101 ids: status=%d", w.Code)
	}
	if w := grcRequest(r, http.MethodGet, "/api/grc/reporting?frameworkIds=7,7,8"); w.Code != http.StatusOK {
		t.Fatalf("deduped ids: status=%d body=%s", w.Code, w.Body.String())
	}
}

func numberedIDs(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = itoa(uint(i + 1))
	}
	return out
}

func TestGRCReportingRouteReturnsAggregatesWithoutAudit(t *testing.T) {
	db, svc := newReportingService(t)
	seedReportingDataset(t, db, dashboardTestNow)
	bus := withCaptureBus(t)
	r := grcServiceRouter(svc, []string{"grc.reporting.read"})
	w := grcRequest(r, http.MethodGet, "/api/grc/reporting")
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), reportingSecret) || strings.Contains(w.Body.String(), "externalReference") {
		t.Fatal("reporting response exposes free text")
	}
	var got GRCReporting
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	requireReportingDataset(t, &got, 30)
	if len(bus.events) != 0 {
		t.Fatalf("a reporting read must not publish audit events: %d", len(bus.events))
	}

	wildcard := grcRequest(grcServiceRouter(svc, []string{"*"}), http.MethodGet, "/api/grc/reporting?evidenceExpiringSoonDays=1&actionDueSoonDays=1")
	if wildcard.Code != http.StatusOK {
		t.Fatalf("wildcard status=%d", wildcard.Code)
	}
}
