package grc

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

var evidenceActionPermissions = []string{
	"grc.evidence.read",
	"grc.evidence.manage",
	"grc.action.read",
	"grc.action.create",
	"grc.action.update",
}

// evidenceActionRoutesFailingBeforeService lists one request per route that
// the handler rejects with 400 before reaching the (nil) service.
func evidenceActionRoutesFailingBeforeService() []complianceRouteCase {
	return []complianceRouteCase{
		{http.MethodGet, "/api/grc/evidence?expired=maybe", "", "grc.evidence.read"},
		{http.MethodGet, "/api/grc/evidence/bad", "", "grc.evidence.read"},
		{http.MethodPost, "/api/grc/evidence", malformedJSON, "grc.evidence.manage"},
		{http.MethodPut, "/api/grc/evidence/bad", "{}", "grc.evidence.manage"},

		{http.MethodGet, "/api/grc/actions?overdue=maybe", "", "grc.action.read"},
		{http.MethodGet, "/api/grc/actions/bad", "", "grc.action.read"},
		{http.MethodPost, "/api/grc/actions", malformedJSON, "grc.action.create"},
		{http.MethodPut, "/api/grc/actions/bad", "{}", "grc.action.update"},
	}
}

func TestGRCEvidenceActionRoutesRegistered(t *testing.T) {
	r := grcTestRouter(nil)

	want := map[string]bool{
		"GET /api/grc/evidence":     false,
		"GET /api/grc/evidence/:id": false,
		"POST /api/grc/evidence":    false,
		"PUT /api/grc/evidence/:id": false,
		"GET /api/grc/actions":      false,
		"GET /api/grc/actions/:id":  false,
		"POST /api/grc/actions":     false,
		"PUT /api/grc/actions/:id":  false,
	}

	for _, route := range r.Routes() {
		key := route.Method + " " + route.Path
		if _, ok := want[key]; ok {
			want[key] = true
		}
		isEvidenceOrAction := strings.HasPrefix(route.Path, "/api/grc/evidence") ||
			strings.HasPrefix(route.Path, "/api/grc/actions")
		if isEvidenceOrAction && route.Method != http.MethodGet &&
			route.Method != http.MethodPost && route.Method != http.MethodPut {
			t.Errorf("unexpected route %s %s", route.Method, route.Path)
		}
	}

	for route, found := range want {
		if !found {
			t.Errorf("route absente: %s", route)
		}
	}

	wildcard := grcTestRouter([]string{"*"})
	for _, path := range []string{"/api/grc/evidence/1", "/api/grc/actions/1"} {
		if w := grcRequest(wildcard, http.MethodDelete, path); w.Code != http.StatusNotFound {
			t.Errorf("DELETE %s status=%d, want 404", path, w.Code)
		}
	}
}

func TestGRCEvidenceActionRoutesRequireAuthentication(t *testing.T) {
	r := grcTestRouter(nil)

	for _, tc := range evidenceActionRoutesFailingBeforeService() {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			w := grcJSONRequest(r, tc.method, tc.path, tc.body)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

// Every route accepts exactly its permission (or the wildcard) and refuses
// each of the other Evidence/Action permissions: no implication between them.
func TestGRCEvidenceActionRoutesEnforceExactPermissions(t *testing.T) {
	for _, tc := range evidenceActionRoutesFailingBeforeService() {
		for _, permission := range append(evidenceActionPermissions, "*") {
			t.Run(permission+" "+tc.method+" "+tc.path, func(t *testing.T) {
				r := grcTestRouter([]string{permission})
				w := grcJSONRequest(r, tc.method, tc.path, tc.body)

				if permission == tc.permission || permission == "*" {
					if w.Code != http.StatusBadRequest {
						t.Fatalf("expected handler 400 after RBAC, got %d body=%s", w.Code, w.Body.String())
					}
					return
				}
				if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), tc.permission) {
					t.Fatalf("expected 403 requiring %q, got %d body=%s", tc.permission, w.Code, w.Body.String())
				}
			})
		}
	}
}

func TestGRCEvidenceActionQueryParsingRejectsInvalidValues(t *testing.T) {
	cases := []struct {
		path string
		perm string
	}{
		{"/api/grc/evidence?controlImplementationId=abc", "grc.evidence.read"},
		{"/api/grc/evidence?controlImplementationId=0", "grc.evidence.read"},
		{"/api/grc/evidence?controlImplementationId=-1", "grc.evidence.read"},
		{"/api/grc/evidence?ownerProfileId=x1", "grc.evidence.read"},
		{"/api/grc/evidence?owningServiceId=0", "grc.evidence.read"},
		{"/api/grc/evidence?expired=yes", "grc.evidence.read"},
		{"/api/grc/evidence?expired=TRUE", "grc.evidence.read"},
		{"/api/grc/evidence?expired=1", "grc.evidence.read"},
		{"/api/grc/evidence?expiresBefore=2026-06-01", "grc.evidence.read"},
		{"/api/grc/evidence?expiresBefore=2026-06-01T12:00:00", "grc.evidence.read"},
		{"/api/grc/evidence?expiresBefore=demain", "grc.evidence.read"},
		{"/api/grc/evidence?page=abc", "grc.evidence.read"},
		{"/api/grc/evidence?limit=1.5", "grc.evidence.read"},
		{"/api/grc/evidence/0", "grc.evidence.read"},
		{"/api/grc/evidence/-1", "grc.evidence.read"},
		{"/api/grc/evidence/18446744073709551616", "grc.evidence.read"},

		{"/api/grc/actions?controlImplementationId=abc", "grc.action.read"},
		{"/api/grc/actions?riskId=0", "grc.action.read"},
		{"/api/grc/actions?riskId=-1", "grc.action.read"},
		{"/api/grc/actions?assigneeProfileId=x", "grc.action.read"},
		{"/api/grc/actions?owningServiceId=-5", "grc.action.read"},
		{"/api/grc/actions?overdue=no", "grc.action.read"},
		{"/api/grc/actions?overdue=False", "grc.action.read"},
		{"/api/grc/actions?dueBefore=2026-06-01", "grc.action.read"},
		{"/api/grc/actions?dueBefore=not-a-date", "grc.action.read"},
		{"/api/grc/actions?page=x", "grc.action.read"},
		{"/api/grc/actions?limit=ten", "grc.action.read"},
		{"/api/grc/actions/0", "grc.action.read"},
		{"/api/grc/actions/-1", "grc.action.read"},
	}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			r := grcTestRouter([]string{tc.perm})
			w := grcRequest(r, http.MethodGet, tc.path)
			if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "invalide") {
				t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func decodeJSON[T any](t *testing.T, body []byte) T {
	t.Helper()

	var value T
	if err := json.Unmarshal(body, &value); err != nil {
		t.Fatalf("invalid JSON: %v body=%s", err, body)
	}
	return value
}

func TestGRCEvidenceAPILifecycle(t *testing.T) {
	f := newRemediationActionFixture(t)
	r := grcServiceRouter(f.svc, []string{"grc.evidence.read", "grc.evidence.manage"})

	body := `{
		"reference": "evd-api",
		"title": "Rapport API",
		"controlImplementationId": ` + jsonUint(f.implementation.ID) + `,
		"type": "report",
		"status": "valid",
		"expiresAt": "2026-06-01T11:00:00Z",
		"id": 999, "createdBy": 99, "updatedBy": 99, "expired": false
	}`
	w := grcJSONRequest(r, http.MethodPost, "/api/grc/evidence", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST status=%d body=%s", w.Code, w.Body.String())
	}
	created := decodeJSON[Evidence](t, w.Body.Bytes())
	if created.ID == 999 || created.CreatedBy != 7 || created.UpdatedBy != 7 || created.Reference != "EVD-API" {
		t.Fatalf("server fields must come from the server: %+v", created)
	}
	if !created.Expired || created.ControlImplementation == nil {
		t.Fatalf("expected expired evidence with relations: %+v", created)
	}

	itemPath := "/api/grc/evidence/" + jsonUint(created.ID)
	if w = grcRequest(r, http.MethodGet, itemPath); w.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", w.Code, w.Body.String())
	}

	update := `{"reference":"EVD-API","title":"Rapport API modifié","controlImplementationId":` +
		jsonUint(f.implementation.ID) + `,"type":"REPORT","status":"VALID"}`
	w = grcJSONRequest(r, http.MethodPut, itemPath, update)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", w.Code, w.Body.String())
	}
	if updated := decodeJSON[Evidence](t, w.Body.Bytes()); updated.Title != "Rapport API modifié" || updated.UpdatedBy != 7 {
		t.Fatalf("unexpected update: %+v", updated)
	}

	w = grcRequest(r, http.MethodGet, "/api/grc/evidence")
	if w.Code != http.StatusOK {
		t.Fatalf("GET list status=%d body=%s", w.Code, w.Body.String())
	}
	if page := decodeJSON[EvidencePage](t, w.Body.Bytes()); page.Total != 1 || page.Items[0].ID != created.ID {
		t.Fatalf("unexpected list: %+v", page)
	}

	if w = grcJSONRequest(r, http.MethodPost, "/api/grc/evidence", body); w.Code != http.StatusConflict {
		t.Fatalf("duplicate POST status=%d body=%s", w.Code, w.Body.String())
	}

	revoke := strings.Replace(update, `"VALID"`, `"REVOKED"`, 1)
	if w = grcJSONRequest(r, http.MethodPut, itemPath, revoke); w.Code != http.StatusOK {
		t.Fatalf("revoke status=%d body=%s", w.Code, w.Body.String())
	}
	if w = grcJSONRequest(r, http.MethodPut, itemPath, revoke); w.Code != http.StatusConflict {
		t.Fatalf("revoked update status=%d body=%s", w.Code, w.Body.String())
	}

	if w = grcJSONRequest(r, http.MethodPost, "/api/grc/evidence", malformedJSON); w.Code != http.StatusBadRequest {
		t.Fatalf("malformed JSON status=%d", w.Code)
	}
	if w = grcJSONRequest(r, http.MethodPut, "/api/grc/evidence/abc", update); w.Code != http.StatusBadRequest {
		t.Fatalf("invalid id status=%d", w.Code)
	}
	if w = grcRequest(r, http.MethodGet, "/api/grc/evidence/999999"); w.Code != http.StatusNotFound {
		t.Fatalf("unknown id status=%d", w.Code)
	}
}

func TestGRCRemediationActionAPILifecycle(t *testing.T) {
	f := newRemediationActionFixture(t)
	r := grcServiceRouter(f.svc, []string{"grc.action.read", "grc.action.create", "grc.action.update"})

	body := `{
		"reference": "act-api",
		"title": "Action API",
		"controlImplementationId": ` + jsonUint(f.implementation.ID) + `,
		"riskId": ` + jsonUint(f.risk.ID) + `,
		"priority": "high",
		"status": "open",
		"dueAt": "2026-06-01T11:00:00Z",
		"id": 999, "createdBy": 99, "overdue": false
	}`
	w := grcJSONRequest(r, http.MethodPost, "/api/grc/actions", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST status=%d body=%s", w.Code, w.Body.String())
	}
	created := decodeJSON[RemediationAction](t, w.Body.Bytes())
	if created.ID == 999 || created.CreatedBy != 7 || created.Reference != "ACT-API" || !created.Overdue {
		t.Fatalf("unexpected created action: %+v", created)
	}

	itemPath := "/api/grc/actions/" + jsonUint(created.ID)
	if w = grcRequest(r, http.MethodGet, itemPath); w.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", w.Code, w.Body.String())
	}

	done := `{"reference":"ACT-API","title":"Action API","controlImplementationId":` + jsonUint(f.implementation.ID) +
		`,"riskId":` + jsonUint(f.risk.ID) + `,"priority":"HIGH","status":"DONE"}`
	w = grcJSONRequest(r, http.MethodPut, itemPath, done)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", w.Code, w.Body.String())
	}
	updated := decodeJSON[RemediationAction](t, w.Body.Bytes())
	if updated.Status != RemediationActionStatusDone || updated.CompletedAt == nil || updated.Overdue || updated.UpdatedBy != 7 {
		t.Fatalf("unexpected completed action: %+v", updated)
	}

	w = grcRequest(r, http.MethodGet, "/api/grc/actions")
	if w.Code != http.StatusOK {
		t.Fatalf("GET list status=%d body=%s", w.Code, w.Body.String())
	}
	if page := decodeJSON[RemediationActionPage](t, w.Body.Bytes()); page.Total != 1 || page.Items[0].ID != created.ID {
		t.Fatalf("unexpected list: %+v", page)
	}

	incoherent := strings.Replace(strings.Replace(body, `"act-api"`, `"act-incoherent"`, 1),
		`"riskId": `+jsonUint(f.risk.ID), `"riskId": `+jsonUint(f.otherRisk.ID), 1)
	if w = grcJSONRequest(r, http.MethodPost, "/api/grc/actions", incoherent); w.Code != http.StatusConflict {
		t.Fatalf("incoherent control risk status=%d body=%s", w.Code, w.Body.String())
	}

	future := `{"reference":"ACT-FUTURE","title":"Futur","riskId":` + jsonUint(f.risk.ID) +
		`,"priority":"LOW","status":"DONE","completedAt":"2026-06-02T00:00:00Z"}`
	if w = grcJSONRequest(r, http.MethodPost, "/api/grc/actions", future); w.Code != http.StatusBadRequest {
		t.Fatalf("future completion status=%d body=%s", w.Code, w.Body.String())
	}

	if w = grcJSONRequest(r, http.MethodPost, "/api/grc/actions", malformedJSON); w.Code != http.StatusBadRequest {
		t.Fatalf("malformed JSON status=%d", w.Code)
	}
	if w = grcJSONRequest(r, http.MethodPut, "/api/grc/actions/abc", done); w.Code != http.StatusBadRequest {
		t.Fatalf("invalid id status=%d", w.Code)
	}
	if w = grcRequest(r, http.MethodGet, "/api/grc/actions/999999"); w.Code != http.StatusNotFound {
		t.Fatalf("unknown id status=%d", w.Code)
	}
}

func requireListReferences[T any](t *testing.T, r http.Handler, path string, references func(T) []string, want ...string) {
	t.Helper()

	w := grcRequest(r, http.MethodGet, path)
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s status=%d body=%s", path, w.Code, w.Body.String())
	}
	got := references(decodeJSON[T](t, w.Body.Bytes()))
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("GET %s references=%v want %v", path, got, want)
	}
}

func TestGRCEvidenceAPIQueryFilters(t *testing.T) {
	f := newRemediationActionFixture(t)

	a := f.request("EVD-A")
	a.Title = "Rapport annuel"
	a.Status = EvidenceStatusValid
	a.ExpiresAt = timePtr(evidenceTestNow.Add(-time.Hour))
	a.OwnerProfileID = &f.profile.ID
	f.create(t, a)

	b := f.request("EVD-B")
	b.Title = "Journal pare-feu"
	b.Type = EvidenceTypeLog
	b.ControlImplementationID = f.other.ID
	b.OwningServiceID = &f.service.ID
	b.ExpiresAt = timePtr(evidenceTestNow.AddDate(0, 0, 10))
	f.create(t, b)

	c := f.request("EVD-C")
	c.Type = EvidenceTypeDocument
	c.Status = EvidenceStatusValid
	f.create(t, c)

	r := grcServiceRouter(f.svc, []string{"grc.evidence.read"})
	refs := func(page EvidencePage) []string {
		out := []string{}
		for _, item := range page.Items {
			out = append(out, item.Reference)
		}
		return out
	}
	tomorrow := url.QueryEscape(evidenceTestNow.Add(24 * time.Hour).In(time.FixedZone("UTC+2", 2*3600)).Format(time.RFC3339))

	requireListReferences(t, r, "/api/grc/evidence?search=PARE-FEU", refs, "EVD-B")
	requireListReferences(t, r, "/api/grc/evidence?controlImplementationId="+jsonUint(f.other.ID), refs, "EVD-B")
	requireListReferences(t, r, "/api/grc/evidence?ownerProfileId="+jsonUint(f.profile.ID), refs, "EVD-A")
	requireListReferences(t, r, "/api/grc/evidence?owningServiceId="+jsonUint(f.service.ID), refs, "EVD-B")
	requireListReferences(t, r, "/api/grc/evidence?type=log", refs, "EVD-B")
	requireListReferences(t, r, "/api/grc/evidence?status=draft", refs, "EVD-B")
	requireListReferences(t, r, "/api/grc/evidence?expired=true", refs, "EVD-A")
	requireListReferences(t, r, "/api/grc/evidence?expired=false", refs, "EVD-B", "EVD-C")
	requireListReferences(t, r, "/api/grc/evidence?expiresBefore="+tomorrow, refs, "EVD-A")
	requireListReferences(t, r, "/api/grc/evidence?expiresBefore=2026-06-01T12:00:00Z", refs, "EVD-A")
	requireListReferences(t, r, "/api/grc/evidence?page=2&limit=2", refs, "EVD-C")
	requireListReferences(t, r, "/api/grc/evidence?limit=1", refs, "EVD-A")

	w := grcRequest(r, http.MethodGet, "/api/grc/evidence?expired=true")
	page := decodeJSON[EvidencePage](t, w.Body.Bytes())
	if !page.Items[0].Expired || page.Total != 1 {
		t.Fatalf("expired item must be marked expired: %+v", page)
	}

	// Enum values are validated by the service.
	if w = grcRequest(r, http.MethodGet, "/api/grc/evidence?status=EXPIRED"); w.Code != http.StatusBadRequest {
		t.Fatalf("invalid status status=%d", w.Code)
	}
}

func TestGRCRemediationActionAPIQueryFilters(t *testing.T) {
	f := newRemediationActionFixture(t)

	a := f.actionRequest("ACT-A")
	a.Title = "Corriger pare-feu"
	a.RiskID = &f.risk.ID
	a.AssigneeProfileID = &f.profile.ID
	a.DueAt = timePtr(evidenceTestNow.Add(-time.Hour))
	f.createAction(t, a)

	b := f.actionRequest("ACT-B")
	b.ControlImplementationID = nil
	b.RiskID = &f.otherRisk.ID
	b.OwningServiceID = &f.service.ID
	b.Priority = RemediationActionPriorityLow
	b.Status = RemediationActionStatusInProgress
	b.DueAt = timePtr(evidenceTestNow.AddDate(0, 0, 5))
	f.createAction(t, b)

	c := f.actionRequest("ACT-C")
	c.ControlImplementationID = &f.other.ID
	c.Priority = RemediationActionPriorityCritical
	c.Status = RemediationActionStatusDone
	c.DueAt = timePtr(evidenceTestNow.Add(-2 * time.Hour))
	f.createAction(t, c)

	r := grcServiceRouter(f.svc, []string{"grc.action.read"})
	refs := func(page RemediationActionPage) []string {
		out := []string{}
		for _, item := range page.Items {
			out = append(out, item.Reference)
		}
		return out
	}
	nowWithOffset := url.QueryEscape(evidenceTestNow.In(time.FixedZone("UTC+2", 2*3600)).Format(time.RFC3339))

	requireListReferences(t, r, "/api/grc/actions", refs, "ACT-C", "ACT-A", "ACT-B")
	requireListReferences(t, r, "/api/grc/actions?search=pare-feu", refs, "ACT-A")
	requireListReferences(t, r, "/api/grc/actions?controlImplementationId="+jsonUint(f.other.ID), refs, "ACT-C")
	requireListReferences(t, r, "/api/grc/actions?riskId="+jsonUint(f.otherRisk.ID), refs, "ACT-B")
	requireListReferences(t, r, "/api/grc/actions?assigneeProfileId="+jsonUint(f.profile.ID), refs, "ACT-A")
	requireListReferences(t, r, "/api/grc/actions?owningServiceId="+jsonUint(f.service.ID), refs, "ACT-B")
	requireListReferences(t, r, "/api/grc/actions?priority=low", refs, "ACT-B")
	requireListReferences(t, r, "/api/grc/actions?status=done", refs, "ACT-C")
	requireListReferences(t, r, "/api/grc/actions?overdue=true", refs, "ACT-A")
	requireListReferences(t, r, "/api/grc/actions?overdue=false", refs, "ACT-C", "ACT-B")
	requireListReferences(t, r, "/api/grc/actions?dueBefore="+nowWithOffset, refs, "ACT-C", "ACT-A")
	requireListReferences(t, r, "/api/grc/actions?page=2&limit=1", refs, "ACT-A")
	requireListReferences(t, r, "/api/grc/actions?limit=2", refs, "ACT-C", "ACT-A")

	w := grcRequest(r, http.MethodGet, "/api/grc/actions?overdue=true")
	page := decodeJSON[RemediationActionPage](t, w.Body.Bytes())
	if page.Total != 1 || !page.Items[0].Overdue {
		t.Fatalf("overdue item must be marked overdue: %+v", page)
	}

	// Enum values are validated by the service.
	for _, path := range []string{"/api/grc/actions?priority=URGENT", "/api/grc/actions?status=CLOSED"} {
		if w = grcRequest(r, http.MethodGet, path); w.Code != http.StatusBadRequest {
			t.Fatalf("GET %s status=%d", path, w.Code)
		}
	}
}
