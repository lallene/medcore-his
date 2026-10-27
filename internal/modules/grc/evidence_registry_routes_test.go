package grc

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// GRC-07: evidence routes on the real router with a real service.

func TestEvidenceRegistryRoutesCRUD(t *testing.T) {
	f := newEvidenceServiceFixture(t)
	bus := withCaptureBus(t)
	r := grcServiceRouter(f.svc, []string{"grc.evidence.read", "grc.evidence.manage"})

	body := fmt.Sprintf(`{"reference":" evd-http ","title":"Rapport HTTP","description":"Texte libre","controlImplementationId":%d,
		"type":"report","status":"valid","externalReference":"https://ged.example.org/docs/42","expiresAt":"%s",
		"id":4242,"createdBy":1,"updatedBy":1,"createdAt":"2000-01-01T00:00:00Z","validity":"VALID","expired":true}`,
		f.implementation.ID, evidenceTestNow.Add(-1).Format("2006-01-02T15:04:05.999999999Z07:00"))
	w := grcJSONRequest(r, http.MethodPost, "/api/grc/evidence", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST status=%d body=%s", w.Code, w.Body.String())
	}
	var created Evidence
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	// Derived and server-managed fields come from the server only.
	if created.ID == 4242 || created.Reference != "EVD-HTTP" || created.CreatedBy != 7 || created.CreatedAt.Year() == 2000 ||
		created.Validity != EvidenceValidityExpired || !created.Expired ||
		created.ControlImplementation == nil || created.ControlImplementation.Control == nil {
		t.Fatalf("unexpected created evidence: %+v", created)
	}
	event := requireAuditEvent(t, bus, 0)
	if event.Action != auditActionCreate || event.UserID == nil || *event.UserID != 7 {
		t.Fatalf("create event: %+v", event)
	}
	// Exact allowlist: no description, no external reference (it may be a URL).
	if keys := snapshotKeys(t, event.NewValue); keys != "collectedAt,controlImplementationId,expiresAt,ownerProfileId,owningServiceId,reference,status,title,type,validFrom" {
		t.Fatalf("evidence audit keys: %s", keys)
	}
	if strings.Contains(event.NewValue, "ged.example.org") || strings.Contains(event.NewValue, "Texte libre") {
		t.Fatalf("audit snapshot leaks free text: %s", event.NewValue)
	}

	path := fmt.Sprintf("/api/grc/evidence/%d", created.ID)
	update := fmt.Sprintf(`{"reference":"EVD-HTTP","title":"Rapport renouvelé","controlImplementationId":%d,"type":"REPORT","status":"VALID","expiresAt":"%s"}`,
		f.implementation.ID, evidenceTestNow.AddDate(0, 0, 10).Format("2006-01-02T15:04:05Z07:00"))
	w = grcJSONRequest(r, http.MethodPut, path, update)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"validity":"EXPIRING_SOON"`) {
		t.Fatalf("PUT status=%d body=%s", w.Code, w.Body.String())
	}
	// The detail window changes the derived state, never the stored data.
	w = grcRequest(r, http.MethodGet, path+"?expiringSoonDays=5")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"validity":"VALID"`) {
		t.Fatalf("GET detail with window: %d %s", w.Code, w.Body.String())
	}
	w = grcRequest(r, http.MethodGet, fmt.Sprintf("/api/grc/evidence?validity=expiring_soon&search=renouv&type=REPORT&controlImplementationId=%d&page=1&limit=5", f.implementation.ID))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"total":1`) {
		t.Fatalf("GET list with validity: %d %s", w.Code, w.Body.String())
	}
	w = grcRequest(r, http.MethodGet, "/api/grc/evidence?validity=EXPIRED&search=renouv")
	if !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Fatalf("empty list must be []: %s", w.Body.String())
	}

	// REVOKED is terminal (existing rule).
	revoke := strings.Replace(update, `"status":"VALID"`, `"status":"REVOKED"`, 1)
	if w := grcJSONRequest(r, http.MethodPut, path, revoke); w.Code != http.StatusOK {
		t.Fatalf("revoke status=%d", w.Code)
	}
	if w := grcJSONRequest(r, http.MethodPut, path, update); w.Code != http.StatusConflict {
		t.Fatalf("update after revoke status=%d, want 409", w.Code)
	}
	if w := grcRequest(r, http.MethodDelete, path); w.Code != http.StatusNotFound {
		t.Fatalf("DELETE status=%d, want 404", w.Code)
	}
	if len(bus.events) != 3 {
		t.Fatalf("audit events=%d, want 3 (create, update, revoke)", len(bus.events))
	}
}

func TestEvidenceRegistryRoutesErrorsAndRBAC(t *testing.T) {
	f := newEvidenceServiceFixture(t)
	evidence := f.create(t, f.request("EVD-RBAC"))
	bus := withCaptureBus(t)
	admin := grcServiceRouter(f.svc, []string{"*"})

	valid := func(extra string) string {
		return fmt.Sprintf(`{"reference":"EVD-NEW","title":"Nouvelle","controlImplementationId":%d,"type":"DOCUMENT","status":"DRAFT"%s}`, f.implementation.ID, extra)
	}
	for _, tc := range []struct {
		method, path, body string
		want               int
	}{
		{http.MethodGet, "/api/grc/evidence?validity=STALE", "", http.StatusBadRequest},
		{http.MethodGet, "/api/grc/evidence?expiringSoonDays=0", "", http.StatusBadRequest},
		{http.MethodGet, "/api/grc/evidence?expiringSoonDays=366", "", http.StatusBadRequest},
		{http.MethodGet, "/api/grc/evidence?expiringSoonDays=x", "", http.StatusBadRequest},
		{http.MethodGet, "/api/grc/evidence?page=x", "", http.StatusBadRequest},
		{http.MethodGet, "/api/grc/evidence?controlImplementationId=0", "", http.StatusBadRequest},
		{http.MethodGet, "/api/grc/assessments?evidenceId=abc", "", http.StatusBadRequest},
		{http.MethodGet, fmt.Sprintf("/api/grc/evidence/%d?expiringSoonDays=400", evidence.ID), "", http.StatusBadRequest},
		{http.MethodGet, "/api/grc/evidence/abc", "", http.StatusBadRequest},
		{http.MethodGet, "/api/grc/evidence/999999", "", http.StatusNotFound},
		{http.MethodPut, "/api/grc/evidence/999999", valid(""), http.StatusNotFound},
		{http.MethodPost, "/api/grc/evidence", malformedJSON, http.StatusBadRequest},
		{http.MethodPost, "/api/grc/evidence", valid(`,"externalReference":"https://user:secret@ged.example.org/x"`), http.StatusBadRequest},
		{http.MethodPost, "/api/grc/evidence", valid(`,"externalReference":"https://ged.example.org/x?token=abc"`), http.StatusBadRequest},
		{http.MethodPost, "/api/grc/evidence", valid(`,"type":"VIDEO"`), http.StatusBadRequest},
		{http.MethodPost, "/api/grc/evidence", strings.Replace(valid(""), "EVD-NEW", "EVD-RBAC", 1), http.StatusConflict},
		{http.MethodPost, "/api/grc/evidence", strings.Replace(valid(""), "EVD-NEW", strings.Repeat("X", 61), 1), http.StatusBadRequest},
	} {
		if w := grcJSONRequest(admin, tc.method, tc.path, tc.body); w.Code != tc.want {
			t.Errorf("%s %s status=%d, want %d (%s)", tc.method, tc.path, w.Code, tc.want, w.Body.String())
		}
	}
	if len(bus.events) != 0 {
		t.Fatalf("failed requests published %d events", len(bus.events))
	}

	detail := fmt.Sprintf("/api/grc/evidence/%d", evidence.ID)
	update := fmt.Sprintf(`{"reference":"EVD-RBAC","title":"Mise à jour","controlImplementationId":%d,"type":"REPORT","status":"DRAFT"}`, f.implementation.ID)
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
		{"unauthenticated", nil, http.MethodGet, "/api/grc/evidence", "", http.StatusUnauthorized},
		{"unrelated", []string{"grc.control.read", "grc.assessment.read"}, http.MethodGet, detail, "", http.StatusForbidden},
		{"read", []string{"grc.evidence.read"}, http.MethodGet, detail, "", http.StatusOK},
		{"read-only cannot create", []string{"grc.evidence.read"}, http.MethodPost, "/api/grc/evidence", valid(""), http.StatusForbidden},
		{"read-only cannot update", []string{"grc.evidence.read"}, http.MethodPut, detail, update, http.StatusForbidden},
		{"assessment update cannot edit evidence", []string{"grc.assessment.update"}, http.MethodPut, detail, update, http.StatusForbidden},
		{"manage updates", []string{"grc.evidence.manage"}, http.MethodPut, detail, update, http.StatusOK},
		{"manage creates", []string{"grc.evidence.manage"}, http.MethodPost, "/api/grc/evidence", valid(""), http.StatusCreated},
		{"wildcard", []string{"*"}, http.MethodGet, "/api/grc/evidence", "", http.StatusOK},
	} {
		if got := status(tc.permissions, tc.method, tc.path, tc.body); got != tc.want {
			t.Errorf("%s: status=%d, want %d", tc.name, got, tc.want)
		}
	}
}
