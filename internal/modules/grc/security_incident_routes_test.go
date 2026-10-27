package grc

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

const securityIncidentSnapshotKeys = "assetId,closedAt,detectedAt,id,occurredAt,ownerProfileId,owningServiceId,reference,resolvedAt,riskId,severity,status,title"

func TestSecurityIncidentRoutes(t *testing.T) {
	f := newGRC01DServiceFixture(t)
	bus := withCaptureBus(t)
	perms := []string{"grc.incident.read", "grc.incident.create", "grc.incident.update", "grc.incident.close"}
	r := grcServiceRouter(f.svc, perms)

	body := `{"reference":" inc-http ","title":"Incident HTTP","description":"Texte libre secret","severity":"high",
		"detectedAt":"2026-05-01T10:00:00Z","occurredAt":"2026-05-01T09:00:00Z",
		"status":"CLOSED","resolvedAt":"2000-01-01T00:00:00Z","closedAt":"2000-01-01T00:00:00Z","createdBy":1,"id":4242}`
	w := grcJSONRequest(r, http.MethodPost, "/api/grc/incidents", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST status=%d body=%s", w.Code, w.Body.String())
	}
	var created SecurityIncident
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == 4242 || created.Reference != "INC-HTTP" || created.Status != SecurityIncidentStatusOpen ||
		created.CreatedBy != 7 || created.ResolvedAt != nil || created.ClosedAt != nil || created.Severity != SecurityIncidentSeverityHigh {
		t.Fatalf("server-managed fields not enforced: %+v", created)
	}
	event := requireAuditEvent(t, bus, 0)
	if event.Module != auditModuleGRCSecurityIncident || event.Action != auditActionCreate || event.UserID == nil || *event.UserID != 7 || event.RecordID != created.ID {
		t.Fatalf("create event: %+v", event)
	}
	if keys := snapshotKeys(t, event.NewValue); keys != securityIncidentSnapshotKeys || strings.Contains(event.NewValue, "Texte libre secret") {
		t.Fatalf("snapshot keys=%s value=%s", keys, event.NewValue)
	}

	before := len(bus.events)
	if w = grcRequest(r, http.MethodGet, "/api/grc/incidents/"+itoa(created.ID)); w.Code != http.StatusOK || len(bus.events) != before {
		t.Fatalf("GET status=%d events=%d", w.Code, len(bus.events))
	}

	update := func(status string) *http.Response {
		payload := `{"reference":"INC-HTTP","title":"Incident HTTP","description":"Texte libre secret","severity":"HIGH",
			"detectedAt":"2026-05-01T10:00:00Z","occurredAt":"2026-05-01T09:00:00Z","status":"` + status + `",
			"resolvedAt":"1999-01-01T00:00:00Z","closedAt":"1999-01-01T00:00:00Z"}`
		return grcJSONRequest(r, http.MethodPut, "/api/grc/incidents/"+itoa(created.ID), payload).Result()
	}
	for _, status := range []string{SecurityIncidentStatusInvestigating, SecurityIncidentStatusContained, SecurityIncidentStatusResolved} {
		resp := update(status)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("PUT %s status=%d", status, resp.StatusCode)
		}
		resp.Body.Close()
	}
	resolveEvent := requireAuditEvent(t, bus, 3)
	if resolveEvent.Action != auditActionUpdate || !strings.Contains(resolveEvent.NewValue, `"status":"RESOLVED"`) || strings.Contains(resolveEvent.NewValue, "Texte libre secret") {
		t.Fatalf("resolve event: %+v", resolveEvent)
	}

	reopen := update(SecurityIncidentStatusInvestigating)
	if reopen.StatusCode != http.StatusOK {
		t.Fatalf("reopen status=%d", reopen.StatusCode)
	}
	var reopened SecurityIncident
	if err := json.NewDecoder(reopen.Body).Decode(&reopened); err != nil {
		t.Fatal(err)
	}
	reopen.Body.Close()
	if reopened.ResolvedAt != nil || reopened.Status != SecurityIncidentStatusInvestigating {
		t.Fatalf("reopen response: %+v", reopened)
	}

	again := update(SecurityIncidentStatusResolved)
	if again.StatusCode != http.StatusOK {
		t.Fatalf("second resolve status=%d", again.StatusCode)
	}
	again.Body.Close()

	eventsBeforeBypass := len(bus.events)
	for _, permissions := range [][]string{{"grc.incident.update"}, {"*"}} {
		router := grcServiceRouter(f.svc, permissions)
		w = grcJSONRequest(router, http.MethodPut, "/api/grc/incidents/"+itoa(created.ID), `{"reference":"INC-HTTP","title":"Incident HTTP","severity":"HIGH","detectedAt":"2026-05-01T10:00:00Z","status":"CLOSED"}`)
		if w.Code != http.StatusForbidden {
			t.Fatalf("PUT CLOSED with %v status=%d body=%s", permissions, w.Code, w.Body.String())
		}
	}
	if len(bus.events) != eventsBeforeBypass {
		t.Fatalf("refused close published an event: %d -> %d", eventsBeforeBypass, len(bus.events))
	}

	denied := grcServiceRouter(f.svc, []string{"grc.incident.update"})
	if w = grcRequest(denied, http.MethodPost, "/api/grc/incidents/"+itoa(created.ID)+"/close"); w.Code != http.StatusForbidden {
		t.Fatalf("close without permission status=%d", w.Code)
	}
	if w = grcRequest(r, http.MethodPost, "/api/grc/incidents/"+itoa(created.ID)+"/close"); w.Code != http.StatusOK {
		t.Fatalf("close status=%d body=%s", w.Code, w.Body.String())
	}
	var closed SecurityIncident
	if err := json.Unmarshal(w.Body.Bytes(), &closed); err != nil {
		t.Fatal(err)
	}
	if closed.Status != SecurityIncidentStatusClosed || closed.ClosedAt == nil || closed.ResolvedAt == nil {
		t.Fatalf("closed response: %+v", closed)
	}
	closeEvent := requireAuditEvent(t, bus, len(bus.events)-1)
	if closeEvent.Action != auditActionUpdate || closeEvent.RecordID != created.ID || !strings.Contains(closeEvent.NewValue, `"status":"CLOSED"`) || strings.Contains(closeEvent.NewValue, "description") {
		t.Fatalf("close event: %+v", closeEvent)
	}
	if w = grcRequest(r, http.MethodPost, "/api/grc/incidents/"+itoa(created.ID)+"/close"); w.Code != http.StatusConflict {
		t.Fatalf("second close status=%d", w.Code)
	}

	if w = grcJSONRequest(r, http.MethodPut, "/api/grc/incidents/"+itoa(created.ID), `{"reference":"INC-HTTP","title":"Incident HTTP","severity":"HIGH","detectedAt":"2026-05-01T10:00:00Z","status":"RESOLVED"}`); w.Code != http.StatusConflict {
		t.Fatalf("closed -> resolved status=%d body=%s", w.Code, w.Body.String())
	}

	open, err := f.svc.CreateSecurityIncident(7, SecurityIncidentRequest{
		Reference: "INC-OPEN", Title: "Ouvert", Severity: SecurityIncidentSeverityLow, DetectedAt: created.DetectedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if w = grcRequest(r, http.MethodPost, "/api/grc/incidents/"+itoa(open.ID)+"/close"); w.Code != http.StatusConflict {
		t.Fatalf("close from open status=%d", w.Code)
	}
	if w = grcJSONRequest(r, http.MethodPut, "/api/grc/incidents/"+itoa(open.ID), `{"reference":"INC-OPEN","title":"Ouvert","severity":"LOW","detectedAt":"2026-05-01T10:00:00Z","status":"RESOLVED"}`); w.Code != http.StatusConflict {
		t.Fatalf("open -> resolved status=%d", w.Code)
	}
}

func TestSecurityIncidentRouteGuards(t *testing.T) {
	f := newGRC01DServiceFixture(t)
	unauth := grcTestRouter(nil)
	for _, call := range []struct{ method, path string }{
		{http.MethodGet, "/api/grc/incidents"},
		{http.MethodGet, "/api/grc/incidents/1"},
		{http.MethodPost, "/api/grc/incidents"},
		{http.MethodPut, "/api/grc/incidents/1"},
		{http.MethodPost, "/api/grc/incidents/1/close"},
	} {
		if w := grcRequest(unauth, call.method, call.path); w.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s status=%d, want 401", call.method, call.path, w.Code)
		}
	}

	unrelated := grcServiceRouter(f.svc, []string{"grc.risk.read", "ticket.read.own"})
	for _, call := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/grc/incidents", ""},
		{http.MethodGet, "/api/grc/incidents/1", ""},
		{http.MethodPost, "/api/grc/incidents", `{}`},
		{http.MethodPut, "/api/grc/incidents/1", `{}`},
		{http.MethodPost, "/api/grc/incidents/1/close", ""},
	} {
		w := grcJSONRequest(unrelated, call.method, call.path, call.body)
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s %s status=%d, want 403", call.method, call.path, w.Code)
		}
	}

	reader := grcServiceRouter(f.svc, []string{"grc.incident.read"})
	if w := grcRequest(reader, http.MethodGet, "/api/grc/incidents?page=abc"); w.Code != http.StatusBadRequest {
		t.Fatalf("malformed page status=%d", w.Code)
	}
	if w := grcRequest(reader, http.MethodGet, "/api/grc/incidents?riskId=0"); w.Code != http.StatusBadRequest {
		t.Fatalf("malformed riskId status=%d", w.Code)
	}
	if w := grcRequest(reader, http.MethodGet, "/api/grc/incidents?status=REOPENED"); w.Code != http.StatusBadRequest {
		t.Fatalf("invalid status status=%d", w.Code)
	}
	w := grcRequest(reader, http.MethodGet, "/api/grc/incidents?search=absent")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Fatalf("empty list status=%d body=%s", w.Code, w.Body.String())
	}
	if w = grcRequest(reader, http.MethodGet, "/api/grc/incidents/999999"); w.Code != http.StatusNotFound {
		t.Fatalf("missing incident status=%d", w.Code)
	}
}

func itoa(id uint) string {
	return strings.TrimPrefix(strings.Replace(jsonNumber(id), "", "", 1), "")
}

func jsonNumber(id uint) string {
	return strings.Trim(strings.ReplaceAll(
		func() string {
			b, _ := json.Marshal(id)
			return string(b)
		}(),
		`"`, "",
	), " ")
}
