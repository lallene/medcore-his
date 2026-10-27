package grc

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// GRC-05: compliance exploration and mapping routes on the real router.

func TestComplianceRoutesExploration(t *testing.T) {
	f, svc := newComplianceServiceFixture(t)
	reg := seedComplianceRegistry(t, f, svc)
	bus := withCaptureBus(t)
	reader := grcServiceRouter(svc, []string{"grc.framework.read", "grc.requirement.read", "grc.control.read"})

	get := func(path string, want int) string {
		t.Helper()
		w := grcRequest(reader, http.MethodGet, path)
		if w.Code != want {
			t.Fatalf("GET %s status=%d, want %d (%s)", path, w.Code, want, w.Body.String())
		}
		return w.Body.String()
	}

	var report ComplianceCoverageReport
	body := get(fmt.Sprintf("/api/grc/compliance/coverage?frameworkIds=%d,%d", reg.iso.ID, reg.hds.ID), http.StatusOK)
	if err := json.Unmarshal([]byte(body), &report); err != nil {
		t.Fatal(err)
	}
	requireCoverage(t, "overall", report.Overall, 5, 3, 2, 66.67)
	requireCoverage(t, "ISO", report.Frameworks[0], 4, 3, 2, 66.67)
	if !strings.Contains(get("/api/grc/compliance/coverage", http.StatusOK), `"frameworks":[]`) {
		t.Fatal("coverage without frameworkIds must return an empty breakdown")
	}

	if !strings.Contains(get("/api/grc/frameworks?status=active&search=iso&page=1&limit=5", http.StatusOK), `"total":1`) {
		t.Fatal("framework search + status")
	}
	get(fmt.Sprintf("/api/grc/frameworks/%d", reg.iso.ID), http.StatusOK)

	var page RequirementPage
	body = get(fmt.Sprintf("/api/grc/requirements?frameworkId=%d&mapped=false", reg.iso.ID), http.StatusOK)
	if err := json.Unmarshal([]byte(body), &page); err != nil || page.Total != 1 || page.Items[0].ID != reg.r2.ID {
		t.Fatalf("unmapped requirements: %v %s", err, body)
	}
	body = get(fmt.Sprintf("/api/grc/requirements/%d", reg.r1.ID), http.StatusOK)
	if !strings.Contains(body, `"mappedControls":2`) {
		t.Fatalf("requirement detail: %s", body)
	}
	if !strings.Contains(get(fmt.Sprintf("/api/grc/requirements?frameworkId=%d", reg.old.ID), http.StatusOK), `"items":[]`) {
		t.Fatal("empty requirement list must be []")
	}
	if !strings.Contains(get(fmt.Sprintf("/api/grc/controls?requirementId=%d", reg.r1.ID), http.StatusOK), `"total":2`) {
		t.Fatal("controls of a requirement")
	}

	for _, path := range []string{
		"/api/grc/compliance/coverage?frameworkIds=abc",
		"/api/grc/compliance/coverage?frameworkIds=1,0",
		"/api/grc/compliance/coverage?frameworkIds=1,,2",
		"/api/grc/compliance/coverage?frameworkIds=" + strings.TrimSuffix(strings.Repeat("1,", ComplianceCoverageMaxFrameworks+1), ","),
		"/api/grc/requirements?mapped=maybe",
		"/api/grc/requirements?controlId=0",
		"/api/grc/requirements?page=x",
		"/api/grc/frameworks?limit=1.5",
		"/api/grc/frameworks?status=PUBLISHED",
		"/api/grc/controls?requirementId=-1",
		"/api/grc/frameworks/abc",
		"/api/grc/requirements/abc",
	} {
		get(path, http.StatusBadRequest)
	}
	get("/api/grc/frameworks/999999", http.StatusNotFound)
	get("/api/grc/requirements/999999", http.StatusNotFound)

	if len(bus.events) != 0 {
		t.Fatalf("reads published %d audit events", len(bus.events))
	}
}

func TestComplianceRoutesRBAC(t *testing.T) {
	f, svc := newComplianceServiceFixture(t)
	reg := seedComplianceRegistry(t, f, svc)
	coverage := "/api/grc/compliance/coverage"
	link := fmt.Sprintf(`{"requirementId":%d,"controlId":%d}`, reg.r2.ID, reg.c1.ID)
	unlink := fmt.Sprintf("/api/grc/requirement-controls/%d/%d", reg.r1.ID, reg.c1.ID)

	status := func(permissions []string, method, path, body string) int {
		router := grcTestRouter(nil)
		if permissions != nil {
			router = grcServiceRouter(svc, permissions)
		}
		return grcJSONRequest(router, method, path, body).Code
	}
	cases := []struct {
		name        string
		permissions []string
		method      string
		path, body  string
		want        int
	}{
		{"unauthenticated coverage", nil, http.MethodGet, coverage, "", http.StatusUnauthorized},
		{"unauthenticated link", nil, http.MethodPost, "/api/grc/requirement-controls", link, http.StatusUnauthorized},
		{"unrelated", []string{"grc.risk.read", "grc.dashboard.read"}, http.MethodGet, coverage, "", http.StatusForbidden},
		{"framework read only", []string{"grc.framework.read"}, http.MethodGet, coverage, "", http.StatusForbidden},
		{"requirement read only", []string{"grc.requirement.read"}, http.MethodGet, coverage, "", http.StatusForbidden},
		{"both reads", []string{"grc.framework.read", "grc.requirement.read"}, http.MethodGet, coverage, "", http.StatusOK},
		{"wildcard", []string{"*"}, http.MethodGet, coverage, "", http.StatusOK},
		{"readers cannot link", []string{"grc.framework.read", "grc.requirement.read", "grc.control.read"}, http.MethodPost, "/api/grc/requirement-controls", link, http.StatusForbidden},
		{"readers cannot unlink", []string{"grc.requirement.read", "grc.control.read"}, http.MethodDelete, unlink, "", http.StatusForbidden},
		{"requirement manage cannot link", []string{"grc.requirement.manage"}, http.MethodPost, "/api/grc/requirement-controls", link, http.StatusForbidden},
		{"control manage links", []string{"grc.control.manage"}, http.MethodPost, "/api/grc/requirement-controls", link, http.StatusCreated},
		{"control manage unlinks", []string{"grc.control.manage"}, http.MethodDelete, unlink, "", http.StatusNoContent},
	}
	for _, tc := range cases {
		if got := status(tc.permissions, tc.method, tc.path, tc.body); got != tc.want {
			t.Errorf("%s: status=%d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestComplianceMappingAudit(t *testing.T) {
	f, svc := newComplianceServiceFixture(t)
	reg := seedComplianceRegistry(t, f, svc)
	bus := withCaptureBus(t)
	r := grcServiceRouter(svc, []string{"grc.control.manage"})

	link := fmt.Sprintf(`{"requirementId":%d,"controlId":%d}`, reg.r2.ID, reg.c1.ID)
	if w := grcJSONRequest(r, http.MethodPost, "/api/grc/requirement-controls", link); w.Code != http.StatusCreated {
		t.Fatalf("link status=%d body=%s", w.Code, w.Body.String())
	}
	event := requireAuditEvent(t, bus, 0)
	if event.Module != auditModuleGRCRequirementControl || event.Action != auditActionLink || event.UserID == nil || *event.UserID != 7 {
		t.Fatalf("unexpected link event: %+v", event)
	}
	if snapshot := decodeAuditJSON(t, event.NewValue); len(snapshot) != 3 || snapshot["id"] == nil || snapshot["requirementId"] == nil || snapshot["controlId"] == nil {
		t.Fatalf("link snapshot: %v", snapshot)
	}

	// Failed mutations publish nothing.
	for _, tc := range []struct {
		method, path, body string
		want               int
	}{
		{http.MethodPost, "/api/grc/requirement-controls", link, http.StatusConflict},
		{http.MethodPost, "/api/grc/requirement-controls", fmt.Sprintf(`{"requirementId":999999,"controlId":%d}`, reg.c1.ID), http.StatusNotFound},
		{http.MethodPost, "/api/grc/requirement-controls", `{"requirementId":0,"controlId":0}`, http.StatusBadRequest},
		{http.MethodDelete, fmt.Sprintf("/api/grc/requirement-controls/%d/%d", reg.r2.ID, reg.c2.ID), "", http.StatusNotFound},
		{http.MethodDelete, "/api/grc/requirement-controls/abc/1", "", http.StatusBadRequest},
	} {
		if w := grcJSONRequest(r, tc.method, tc.path, tc.body); w.Code != tc.want {
			t.Errorf("%s %s status=%d, want %d", tc.method, tc.path, w.Code, tc.want)
		}
	}
	if len(bus.events) != 1 {
		t.Fatalf("failed mutations published events: %d", len(bus.events))
	}

	if w := grcRequest(r, http.MethodDelete, fmt.Sprintf("/api/grc/requirement-controls/%d/%d", reg.r2.ID, reg.c1.ID)); w.Code != http.StatusNoContent {
		t.Fatalf("unlink status=%d", w.Code)
	}
	if event := requireAuditEvent(t, bus, 1); event.Action != auditActionUnlink || event.OldValue == "" || event.NewValue != "" {
		t.Fatalf("unexpected unlink event: %+v", event)
	}
}
