package grc

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// GRC-08: remediation action routes on the real router with a real service.

func TestRemediationActionRegistryRoutesCRUD(t *testing.T) {
	f := newActionFindingFixture(t)
	bus := withCaptureBus(t)
	r := grcServiceRouter(f.svc, []string{"grc.action.read", "grc.action.create", "grc.action.update"})

	due := evidenceTestNow.AddDate(0, 0, 5).Format("2006-01-02T15:04:05Z07:00")
	body := fmt.Sprintf(`{"reference":" act-http ","title":"Action HTTP","description":"Texte libre","findingId":%d,
		"priority":"high","status":"open","dueAt":"%s",
		"id":4242,"createdBy":1,"updatedBy":1,"createdAt":"2000-01-01T00:00:00Z","dueState":"CLOSED","overdue":true}`, f.plain.ID, due)
	w := grcJSONRequest(r, http.MethodPost, "/api/grc/actions", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST status=%d body=%s", w.Code, w.Body.String())
	}
	var created RemediationAction
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == 4242 || created.Reference != "ACT-HTTP" || created.CreatedBy != 7 || created.CreatedAt.Year() == 2000 ||
		created.DueState != RemediationActionDueSoon || created.Overdue || created.Finding == nil {
		t.Fatalf("server-managed fields not enforced: %+v", created)
	}
	event := requireAuditEvent(t, bus, 0)
	if event.Action != auditActionCreate || event.UserID == nil || *event.UserID != 7 {
		t.Fatalf("create event: %+v", event)
	}
	if keys := snapshotKeys(t, event.NewValue); keys != "assigneeProfileId,completedAt,controlImplementationId,dueAt,findingId,owningServiceId,priority,reference,riskId,status,title" ||
		strings.Contains(event.NewValue, "Texte libre") {
		t.Fatalf("action audit keys: %s (%s)", keys, event.NewValue)
	}

	path := fmt.Sprintf("/api/grc/actions/%d", created.ID)
	update := fmt.Sprintf(`{"reference":"ACT-HTTP","title":"Action terminée","findingId":%d,"priority":"HIGH","status":"DONE","dueAt":"%s"}`, f.plain.ID, due)
	w = grcJSONRequest(r, http.MethodPut, path, update)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"dueState":"CLOSED"`) || !strings.Contains(w.Body.String(), `"completedAt":"`) {
		t.Fatalf("PUT status=%d body=%s", w.Code, w.Body.String())
	}
	w = grcRequest(r, http.MethodGet, path+"?dueSoonDays=1")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"dueState":"CLOSED"`) {
		t.Fatalf("GET detail: %d %s", w.Code, w.Body.String())
	}
	w = grcRequest(r, http.MethodGet, fmt.Sprintf("/api/grc/actions?dueState=closed&search=http&findingId=%d&page=1&limit=5", f.plain.ID))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"total":1`) {
		t.Fatalf("GET list: %d %s", w.Code, w.Body.String())
	}
	w = grcRequest(r, http.MethodGet, "/api/grc/actions?dueState=OVERDUE&search=http")
	if !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Fatalf("empty list must be []: %s", w.Body.String())
	}
	if w := grcRequest(r, http.MethodDelete, path); w.Code != http.StatusNotFound {
		t.Fatalf("DELETE status=%d, want 404", w.Code)
	}
	if len(bus.events) != 2 {
		t.Fatalf("audit events=%d, want 2", len(bus.events))
	}
}

func TestRemediationActionRegistryRoutesErrorsAndRBAC(t *testing.T) {
	f := newActionFindingFixture(t)
	seed, err := f.svc.SaveRemediationAction(0, 1, f.findingAction("ACT-RBAC", f.plain.ID))
	if err != nil {
		t.Fatal(err)
	}
	bus := withCaptureBus(t)
	admin := grcServiceRouter(f.svc, []string{"*"})
	action := func(reference string, findingID uint, extra string) string {
		return fmt.Sprintf(`{"reference":"%s","title":"Action","findingId":%d,"priority":"LOW","status":"OPEN"%s}`, reference, findingID, extra)
	}

	for _, tc := range []struct {
		method, path, body string
		want               int
	}{
		{http.MethodGet, "/api/grc/actions?dueState=LATE", "", http.StatusBadRequest},
		{http.MethodGet, "/api/grc/actions?dueSoonDays=0", "", http.StatusBadRequest},
		{http.MethodGet, "/api/grc/actions?dueSoonDays=366", "", http.StatusBadRequest},
		{http.MethodGet, "/api/grc/actions?dueSoonDays=x", "", http.StatusBadRequest},
		{http.MethodGet, "/api/grc/actions?page=x", "", http.StatusBadRequest},
		{http.MethodGet, "/api/grc/actions?findingId=0", "", http.StatusBadRequest},
		{http.MethodGet, "/api/grc/actions?status=ARCHIVED", "", http.StatusBadRequest},
		{http.MethodGet, fmt.Sprintf("/api/grc/actions/%d?dueSoonDays=400", seed.ID), "", http.StatusBadRequest},
		{http.MethodGet, "/api/grc/actions/abc", "", http.StatusBadRequest},
		{http.MethodGet, "/api/grc/actions/999999", "", http.StatusNotFound},
		{http.MethodPut, "/api/grc/actions/999999", action("ACT-404", f.plain.ID, ""), http.StatusNotFound},
		{http.MethodPost, "/api/grc/actions", malformedJSON, http.StatusBadRequest},
		{http.MethodPost, "/api/grc/actions", `{"reference":"ACT-NOTARGET","title":"Sans cible","priority":"LOW","status":"OPEN"}`, http.StatusBadRequest},
		{http.MethodPost, "/api/grc/actions", action("ACT-UNKNOWN", 999999, ""), http.StatusNotFound},
		{http.MethodPost, "/api/grc/actions", action("ACT-CLOSED", f.closed.ID, ""), http.StatusConflict},
		{http.MethodPost, "/api/grc/actions", action("ACT-ACCEPTED", f.accepted.ID, ""), http.StatusConflict},
		{http.MethodPost, "/api/grc/actions", action("ACT-RBAC", f.plain.ID, ""), http.StatusConflict},
		{http.MethodPost, "/api/grc/actions", action("ACT-PRIO", f.plain.ID, `,"priority":"URGENT"`), http.StatusBadRequest},
		{http.MethodPost, "/api/grc/actions", action(strings.Repeat("X", 61), f.plain.ID, ""), http.StatusBadRequest},
		{http.MethodPut, fmt.Sprintf("/api/grc/actions/%d", seed.ID), action("ACT-RBAC", f.closed.ID, ""), http.StatusConflict},
	} {
		if w := grcJSONRequest(admin, tc.method, tc.path, tc.body); w.Code != tc.want {
			t.Errorf("%s %s status=%d, want %d (%s)", tc.method, tc.path, w.Code, tc.want, w.Body.String())
		}
	}
	if len(bus.events) != 0 {
		t.Fatalf("failed requests published %d events", len(bus.events))
	}

	detail := fmt.Sprintf("/api/grc/actions/%d", seed.ID)
	update := action("ACT-RBAC", f.plain.ID, `,"status":"IN_PROGRESS"`)
	status := func(permissions []string, method, path, body string) int {
		router := grcTestRouter(nil)
		if permissions != nil {
			router = grcServiceRouter(f.svc, permissions)
		}
		return grcJSONRequest(router, method, path, body).Code
	}
	for _, tc := range []struct {
		name        string
		permissions []string
		method      string
		path, body  string
		want        int
	}{
		{"unauthenticated", nil, http.MethodGet, "/api/grc/actions", "", http.StatusUnauthorized},
		{"unrelated", []string{"grc.finding.read", "grc.risk.read"}, http.MethodGet, detail, "", http.StatusForbidden},
		{"read", []string{"grc.action.read"}, http.MethodGet, detail, "", http.StatusOK},
		{"read-only cannot create", []string{"grc.action.read"}, http.MethodPost, "/api/grc/actions", action("ACT-RO", f.plain.ID, ""), http.StatusForbidden},
		{"read-only cannot update", []string{"grc.action.read"}, http.MethodPut, detail, update, http.StatusForbidden},
		{"create cannot update", []string{"grc.action.create"}, http.MethodPut, detail, update, http.StatusForbidden},
		{"update cannot create", []string{"grc.action.update"}, http.MethodPost, "/api/grc/actions", action("ACT-UP", f.plain.ID, ""), http.StatusForbidden},
		{"create", []string{"grc.action.create"}, http.MethodPost, "/api/grc/actions", action("ACT-NEW", f.plain.ID, ""), http.StatusCreated},
		{"update", []string{"grc.action.update"}, http.MethodPut, detail, update, http.StatusOK},
		{"wildcard", []string{"*"}, http.MethodGet, "/api/grc/actions", "", http.StatusOK},
	} {
		if got := status(tc.permissions, tc.method, tc.path, tc.body); got != tc.want {
			t.Errorf("%s: status=%d, want %d", tc.name, got, tc.want)
		}
	}
}
