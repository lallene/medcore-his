package grc

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// GRC-09: audit and finding routes on the real router: lifecycle, dedicated
// transition routes (anti-bypass), close guard, RBAC and audit trail.

func TestAuditRegistryRoutesLifecycle(t *testing.T) {
	f := newGRC01DServiceFixture(t)
	bus := withCaptureBus(t)
	r := grcServiceRouter(f.svc, []string{"grc.audit.read", "grc.audit.create", "grc.audit.update", "grc.audit.complete"})

	body := `{"reference":" aud-http ","title":"Audit HTTP","description":"Texte libre","scope":"Périmètre","type":"internal","status":"planned",
		"plannedStartAt":"2026-07-01T00:00:00Z","plannedEndAt":"2026-07-05T00:00:00Z",
		"id":4242,"createdBy":1,"completedAt":"2000-01-01T00:00:00Z","findingCount":9,"openFindingCount":9}`
	w := grcJSONRequest(r, http.MethodPost, "/api/grc/audits", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST status=%d body=%s", w.Code, w.Body.String())
	}
	var audit AuditEngagement
	if err := json.Unmarshal(w.Body.Bytes(), &audit); err != nil {
		t.Fatal(err)
	}
	if audit.ID == 4242 || audit.Reference != "AUD-HTTP" || audit.CreatedBy != 7 || audit.CompletedAt != nil ||
		audit.FindingCount != 0 || audit.Status != AuditEngagementStatusPlanned {
		t.Fatalf("server-managed fields not enforced: %+v", audit)
	}
	event := requireAuditEvent(t, bus, 0)
	if event.Module != auditModuleGRCAuditEngagement || event.Action != auditActionCreate || *event.UserID != 7 {
		t.Fatalf("create event: %+v", event)
	}
	if keys := snapshotKeys(t, event.NewValue); keys != "completedAt,leadAuditorProfileId,owningServiceId,plannedEndAt,plannedStartAt,reference,startedAt,status,title,type" ||
		strings.Contains(event.NewValue, "Texte libre") || strings.Contains(event.NewValue, "Périmètre") {
		t.Fatalf("audit snapshot: %s", event.NewValue)
	}

	path := fmt.Sprintf("/api/grc/audits/%d", audit.ID)
	update := func(status string) string {
		return fmt.Sprintf(`{"reference":"AUD-HTTP","title":"Audit HTTP","type":"INTERNAL","status":"%s","plannedStartAt":"2026-07-01T00:00:00Z","plannedEndAt":"2026-07-05T00:00:00Z"}`, status)
	}
	// Anti-bypass: COMPLETED only through /complete, whatever the permissions.
	if w := grcJSONRequest(r, http.MethodPut, path, update("COMPLETED")); w.Code != http.StatusForbidden {
		t.Fatalf("PUT COMPLETED status=%d, want 403", w.Code)
	}
	if w := grcJSONRequest(grcServiceRouter(f.svc, []string{"*"}), http.MethodPut, path, update("completed")); w.Code != http.StatusForbidden {
		t.Fatalf("wildcard PUT COMPLETED status=%d, want 403", w.Code)
	}
	// PLANNED cannot be completed directly.
	if w := grcRequest(r, http.MethodPost, path+"/complete"); w.Code != http.StatusConflict {
		t.Fatalf("complete from PLANNED status=%d, want 409", w.Code)
	}
	if w := grcJSONRequest(r, http.MethodPut, path, update("IN_PROGRESS")); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"startedAt":"`) {
		t.Fatalf("start status=%d body=%s", w.Code, w.Body.String())
	}
	w = grcRequest(r, http.MethodPost, path+"/complete")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"status":"COMPLETED"`) {
		t.Fatalf("complete status=%d body=%s", w.Code, w.Body.String())
	}
	if event := requireAuditEvent(t, bus, 2); event.Action != auditActionUpdate || !strings.Contains(event.NewValue, `"status":"COMPLETED"`) {
		t.Fatalf("complete event: %+v", event)
	}
	// Terminal: no transition back, no second completion.
	for _, status := range []string{"IN_PROGRESS", "PLANNED", "CANCELLED"} {
		if w := grcJSONRequest(r, http.MethodPut, path, update(status)); w.Code != http.StatusConflict {
			t.Errorf("COMPLETED -> %s status=%d, want 409", status, w.Code)
		}
	}
	if w := grcRequest(r, http.MethodPost, path+"/complete"); w.Code != http.StatusConflict {
		t.Fatalf("second completion status=%d, want 409", w.Code)
	}

	// Cancellation through the generic PUT (not reserved), then terminal.
	w = grcJSONRequest(r, http.MethodPost, "/api/grc/audits", `{"reference":"AUD-CANCEL","title":"Annulé","type":"EXTERNAL","status":"PLANNED"}`)
	var cancelled AuditEngagement
	_ = json.Unmarshal(w.Body.Bytes(), &cancelled)
	cancelPath := fmt.Sprintf("/api/grc/audits/%d", cancelled.ID)
	cancelBody := `{"reference":"AUD-CANCEL","title":"Annulé","type":"EXTERNAL","status":"%s"}`
	if w := grcJSONRequest(r, http.MethodPut, cancelPath, fmt.Sprintf(cancelBody, "CANCELLED")); w.Code != http.StatusOK {
		t.Fatalf("cancel status=%d", w.Code)
	}
	if w := grcJSONRequest(r, http.MethodPut, cancelPath, fmt.Sprintf(cancelBody, "PLANNED")); w.Code != http.StatusConflict {
		t.Fatalf("CANCELLED -> PLANNED status=%d, want 409", w.Code)
	}
	eventsBefore := len(bus.events)

	// Reads publish nothing; list filters and empty list.
	if w := grcRequest(r, http.MethodGet, "/api/grc/audits?search=aud-&status=completed&type=INTERNAL&page=1&limit=5"); !strings.Contains(w.Body.String(), `"total":1`) {
		t.Fatalf("list: %s", w.Body.String())
	}
	if w := grcRequest(r, http.MethodGet, "/api/grc/audits?search=absent"); !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Fatalf("empty list: %s", w.Body.String())
	}
	grcRequest(r, http.MethodGet, path)
	if len(bus.events) != eventsBefore {
		t.Fatalf("reads published %d events", len(bus.events)-eventsBefore)
	}
	if w := grcRequest(r, http.MethodDelete, path); w.Code != http.StatusNotFound {
		t.Fatalf("DELETE status=%d, want 404", w.Code)
	}
}

func TestFindingRoutesTransitionsAndCloseGuard(t *testing.T) {
	f := newGRC01DServiceFixture(t)
	audit := f.inProgressAudit(t, "AUD-FND-HTTP")
	bus := withCaptureBus(t)
	all := []string{"grc.finding.read", "grc.finding.create", "grc.finding.update", "grc.finding.close", "grc.finding.accept", "grc.action.create", "grc.action.update"}
	r := grcServiceRouter(f.svc, all)

	body := fmt.Sprintf(`{"reference":"fnd-http","title":"Constat HTTP","description":"Texte libre","auditEngagementId":%d,
		"category":"CONTROL_DEFICIENCY","severity":"high","status":"open","dueAt":"2026-05-01T00:00:00Z","id":99,"createdBy":1,"overdue":false}`, audit.ID)
	w := grcJSONRequest(r, http.MethodPost, "/api/grc/findings", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST status=%d body=%s", w.Code, w.Body.String())
	}
	var finding Finding
	_ = json.Unmarshal(w.Body.Bytes(), &finding)
	if finding.ID == 99 || finding.CreatedBy != 7 || !finding.Overdue || finding.AuditEngagement == nil {
		t.Fatalf("created finding: %+v", finding)
	}
	if event := requireAuditEvent(t, bus, 0); snapshotKeys(t, event.NewValue) !=
		"assessmentId,auditEngagementId,category,closedAt,controlImplementationId,dueAt,ownerProfileId,owningServiceId,reference,resolvedAt,riskId,severity,status,title" ||
		strings.Contains(event.NewValue, "Texte libre") {
		t.Fatalf("finding snapshot: %s", event.NewValue)
	}

	path := fmt.Sprintf("/api/grc/findings/%d", finding.ID)
	put := func(status string) string {
		return fmt.Sprintf(`{"reference":"FND-HTTP","title":"Constat HTTP","auditEngagementId":%d,"category":"CONTROL_DEFICIENCY","severity":"HIGH","status":"%s","closureNote":"Note"}`, audit.ID, status)
	}
	// Anti-bypass: CLOSED and ACCEPTED only through their routes.
	for _, status := range []string{"CLOSED", "accepted"} {
		if w := grcJSONRequest(grcServiceRouter(f.svc, []string{"*"}), http.MethodPut, path, put(status)); w.Code != http.StatusForbidden {
			t.Errorf("PUT %s status=%d, want 403", status, w.Code)
		}
	}
	// Source immutability through HTTP.
	other := f.inProgressAudit(t, "AUD-FND-OTHER")
	moved := strings.Replace(put("OPEN"), fmt.Sprintf(`"auditEngagementId":%d`, audit.ID), fmt.Sprintf(`"auditEngagementId":%d`, other.ID), 1)
	if w := grcJSONRequest(r, http.MethodPut, path, moved); w.Code != http.StatusConflict {
		t.Fatalf("source change status=%d, want 409", w.Code)
	}

	// Close guard: an open action blocks /close.
	action := fmt.Sprintf(`{"reference":"ACT-FND-HTTP","title":"Action","findingId":%d,"priority":"LOW","status":"OPEN"}`, finding.ID)
	w = grcJSONRequest(r, http.MethodPost, "/api/grc/actions", action)
	var created RemediationAction
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	if w := grcJSONRequest(r, http.MethodPut, path, put("RESOLVED")); w.Code != http.StatusOK {
		t.Fatalf("resolve status=%d body=%s", w.Code, w.Body.String())
	}
	f.setClock(evidenceTestNow.Add(time.Hour))
	if w := grcRequest(r, http.MethodPost, path+"/close"); w.Code != http.StatusConflict {
		t.Fatalf("close with an open action status=%d, want 409", w.Code)
	}
	done := strings.Replace(action, `"status":"OPEN"`, `"status":"DONE"`, 1)
	if w := grcJSONRequest(r, http.MethodPut, fmt.Sprintf("/api/grc/actions/%d", created.ID), done); w.Code != http.StatusOK {
		t.Fatalf("action done status=%d", w.Code)
	}
	w = grcRequest(r, http.MethodPost, path+"/close")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"status":"CLOSED"`) {
		t.Fatalf("close status=%d body=%s", w.Code, w.Body.String())
	}
	if w := grcRequest(r, http.MethodPost, path+"/close"); w.Code != http.StatusConflict {
		t.Fatalf("second close status=%d, want 409", w.Code)
	}

	// Accept: justification required, terminal afterwards.
	second := f.createFinding(t, f.findingRequest("FND-ACCEPT", audit.ID))
	acceptPath := fmt.Sprintf("/api/grc/findings/%d/accept", second.ID)
	if w := grcJSONRequest(r, http.MethodPost, acceptPath, `{"closureNote":"  "}`); w.Code != http.StatusBadRequest {
		t.Fatalf("accept without note status=%d, want 400", w.Code)
	}
	if w := grcJSONRequest(r, http.MethodPost, acceptPath, `{"closureNote":"Risque accepté par la direction"}`); w.Code != http.StatusOK {
		t.Fatalf("accept status=%d", w.Code)
	}
	if w := grcRequest(r, http.MethodGet, fmt.Sprintf("/api/grc/findings?auditEngagementId=%d&status=ACCEPTED&severity=high&search=fnd", audit.ID)); !strings.Contains(w.Body.String(), `"total":1`) {
		t.Fatalf("finding filters: %s", w.Body.String())
	}
}

func TestAuditAndFindingRoutesRBAC(t *testing.T) {
	f := newGRC01DServiceFixture(t)
	audit := f.inProgressAudit(t, "AUD-RBAC-09")
	finding := f.createFinding(t, f.findingRequest("FND-RBAC-09", audit.ID))
	auditPath := fmt.Sprintf("/api/grc/audits/%d", audit.ID)
	findingPath := fmt.Sprintf("/api/grc/findings/%d", finding.ID)
	auditBody := `{"reference":"AUD-NEW-09","title":"Audit","type":"INTERNAL","status":"PLANNED"}`
	auditUpdate := `{"reference":"AUD-RBAC-09","title":"Audit modifié","type":"INTERNAL","status":"IN_PROGRESS"}`
	findingBody := fmt.Sprintf(`{"reference":"FND-NEW-09","title":"Constat","auditEngagementId":%d,"category":"OBSERVATION","severity":"LOW","status":"OPEN"}`, audit.ID)
	findingUpdate := fmt.Sprintf(`{"reference":"FND-RBAC-09","title":"Constat modifié","auditEngagementId":%d,"category":"CONTROL_DEFICIENCY","severity":"HIGH","status":"IN_REMEDIATION"}`, audit.ID)

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
		{"unauthenticated audits", nil, http.MethodGet, "/api/grc/audits", "", http.StatusUnauthorized},
		{"unauthenticated findings", nil, http.MethodGet, "/api/grc/findings", "", http.StatusUnauthorized},
		{"unrelated", []string{"grc.risk.read", "grc.action.read"}, http.MethodGet, auditPath, "", http.StatusForbidden},
		{"audit read", []string{"grc.audit.read"}, http.MethodGet, auditPath, "", http.StatusOK},
		{"audit read cannot create", []string{"grc.audit.read"}, http.MethodPost, "/api/grc/audits", auditBody, http.StatusForbidden},
		{"audit read cannot update", []string{"grc.audit.read"}, http.MethodPut, auditPath, auditUpdate, http.StatusForbidden},
		{"audit update cannot complete", []string{"grc.audit.update"}, http.MethodPost, auditPath + "/complete", "", http.StatusForbidden},
		{"audit read cannot read findings", []string{"grc.audit.read"}, http.MethodGet, findingPath, "", http.StatusForbidden},
		{"audit create", []string{"grc.audit.create"}, http.MethodPost, "/api/grc/audits", auditBody, http.StatusCreated},
		{"audit update", []string{"grc.audit.update"}, http.MethodPut, auditPath, auditUpdate, http.StatusOK},
		{"finding read", []string{"grc.finding.read"}, http.MethodGet, findingPath, "", http.StatusOK},
		{"finding read cannot create", []string{"grc.finding.read"}, http.MethodPost, "/api/grc/findings", findingBody, http.StatusForbidden},
		{"finding update cannot close", []string{"grc.finding.update"}, http.MethodPost, findingPath + "/close", "", http.StatusForbidden},
		{"finding close cannot accept", []string{"grc.finding.close"}, http.MethodPost, findingPath + "/accept", `{"closureNote":"x"}`, http.StatusForbidden},
		{"finding create", []string{"grc.finding.create"}, http.MethodPost, "/api/grc/findings", findingBody, http.StatusCreated},
		{"finding update", []string{"grc.finding.update"}, http.MethodPut, findingPath, findingUpdate, http.StatusOK},
		{"wildcard", []string{"*"}, http.MethodGet, "/api/grc/findings", "", http.StatusOK},
		{"audit complete", []string{"grc.audit.complete"}, http.MethodPost, auditPath + "/complete", "", http.StatusOK},
	} {
		if got := status(tc.permissions, tc.method, tc.path, tc.body); got != tc.want {
			t.Errorf("%s: status=%d, want %d", tc.name, got, tc.want)
		}
	}

	// Errors.
	admin := grcServiceRouter(f.svc, []string{"*"})
	for _, tc := range []struct {
		method, path, body string
		want               int
	}{
		{http.MethodGet, "/api/grc/audits/abc", "", http.StatusBadRequest},
		{http.MethodGet, "/api/grc/audits/999999", "", http.StatusNotFound},
		{http.MethodPost, "/api/grc/audits/999999/complete", "", http.StatusNotFound},
		{http.MethodGet, "/api/grc/audits?page=x", "", http.StatusBadRequest},
		{http.MethodGet, "/api/grc/audits?status=APPROVED", "", http.StatusBadRequest},
		{http.MethodGet, "/api/grc/audits?plannedStartAfter=2026-13-01", "", http.StatusBadRequest},
		{http.MethodPost, "/api/grc/audits", `{"reference":"AUD-X","title":"X","type":"INTERNAL","status":"COMPLETED"}`, http.StatusBadRequest},
		{http.MethodPost, "/api/grc/audits", `{"reference":"AUD-X","title":"X","type":"INTERNAL","status":"PLANNED","plannedStartAt":"2026-07-05T00:00:00Z","plannedEndAt":"2026-07-01T00:00:00Z"}`, http.StatusBadRequest},
		{http.MethodPost, "/api/grc/audits", `{"reference":"AUD-RBAC-09","title":"Doublon","type":"INTERNAL","status":"PLANNED"}`, http.StatusConflict},
		{http.MethodGet, "/api/grc/findings/999999", "", http.StatusNotFound},
		{http.MethodGet, "/api/grc/findings?auditEngagementId=0", "", http.StatusBadRequest},
		{http.MethodGet, "/api/grc/findings?severity=EXTREME", "", http.StatusBadRequest},
		{http.MethodPost, "/api/grc/findings", `{"reference":"FND-X","title":"X","category":"OBSERVATION","severity":"LOW","status":"OPEN"}`, http.StatusBadRequest},
	} {
		if w := grcJSONRequest(admin, tc.method, tc.path, tc.body); w.Code != tc.want {
			t.Errorf("%s %s status=%d, want %d (%s)", tc.method, tc.path, w.Code, tc.want, w.Body.String())
		}
	}
}
