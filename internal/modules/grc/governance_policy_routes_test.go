package grc

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

const governancePolicySnapshotKeys = "approvedAt,approverProfileId,controlIds,effectiveFrom,effectiveTo,frameworkIds,id,ownerProfileId,owningServiceId,publishedAt,reference,retiredAt,status,title,version"

func TestGovernancePolicyRoutesAuditAndBypass(t *testing.T) {
	f := newPolicyFixture(t)
	framework := createFramework(t, f.svc, "FW-HTTP", "1.0")
	bus := withCaptureBus(t)
	perms := []string{
		"grc.policy.read", "grc.policy.create", "grc.policy.update",
		"grc.policy.approve", "grc.policy.publish", "grc.policy.retire",
	}
	r := grcServiceRouter(f.svc, perms)

	body := `{"reference":" pol-http ","title":"Politique HTTP","description":"Texte libre secret","version":"1.0",
		"status":"PUBLISHED","approvedAt":"2000-01-01T00:00:00Z","publishedAt":"2000-01-01T00:00:00Z","retiredAt":"2000-01-01T00:00:00Z",
		"createdBy":1,"id":4242,"frameworkIds":[` + itoa(framework.ID) + `]}`
	w := grcJSONRequest(r, http.MethodPost, "/api/grc/policies", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST status=%d body=%s", w.Code, w.Body.String())
	}
	var created GovernancePolicy
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == 4242 || created.Reference != "POL-HTTP" || created.Status != GovernancePolicyStatusDraft ||
		created.CreatedBy != 7 || created.ApprovedAt != nil || created.FrameworkCount != 1 {
		t.Fatalf("server-managed fields: %+v", created)
	}
	event := requireAuditEvent(t, bus, 0)
	if event.Module != auditModuleGRCGovernancePolicy || event.Action != auditActionCreate || event.UserID == nil || *event.UserID != 7 || event.RecordID != created.ID {
		t.Fatalf("create event: %+v", event)
	}
	if keys := snapshotKeys(t, event.NewValue); keys != governancePolicySnapshotKeys || strings.Contains(event.NewValue, "Texte libre secret") {
		t.Fatalf("snapshot keys=%s value=%s", keys, event.NewValue)
	}

	before := len(bus.events)
	if w = grcRequest(r, http.MethodGet, "/api/grc/policies/"+itoa(created.ID)); w.Code != http.StatusOK || len(bus.events) != before {
		t.Fatalf("GET status=%d events=%d", w.Code, len(bus.events))
	}

	update := `{"reference":"POL-HTTP","title":"Politique HTTP","description":"Texte libre secret","version":"1.1","status":"IN_REVIEW","approverProfileId":` + itoa(f.profile.ID) + `}`
	w = grcJSONRequest(r, http.MethodPut, "/api/grc/policies/"+itoa(created.ID), update)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT review status=%d body=%s", w.Code, w.Body.String())
	}
	if event = requireAuditEvent(t, bus, 1); event.Action != auditActionUpdate || strings.Contains(event.NewValue, "Texte libre secret") {
		t.Fatalf("update event: %+v", event)
	}

	eventsBefore := len(bus.events)
	for _, permissions := range [][]string{{"grc.policy.update"}, {"*"}} {
		router := grcServiceRouter(f.svc, permissions)
		for _, status := range []string{"APPROVED", "PUBLISHED", "RETIRED"} {
			payload := `{"reference":"POL-HTTP","title":"Politique HTTP","version":"1.1","status":"` + status + `"}`
			w = grcJSONRequest(router, http.MethodPut, "/api/grc/policies/"+itoa(created.ID), payload)
			if w.Code != http.StatusForbidden {
				t.Fatalf("PUT %s with %v status=%d body=%s", status, permissions, w.Code, w.Body.String())
			}
		}
	}
	if len(bus.events) != eventsBefore {
		t.Fatalf("refused update published an event: %d -> %d", eventsBefore, len(bus.events))
	}

	denied := grcServiceRouter(f.svc, []string{"grc.policy.update"})
	if w = grcRequest(denied, http.MethodPost, "/api/grc/policies/"+itoa(created.ID)+"/approve"); w.Code != http.StatusForbidden {
		t.Fatalf("approve without permission status=%d", w.Code)
	}
	if w = grcRequest(r, http.MethodPost, "/api/grc/policies/"+itoa(created.ID)+"/approve"); w.Code != http.StatusOK {
		t.Fatalf("approve status=%d body=%s", w.Code, w.Body.String())
	}
	if event = requireAuditEvent(t, bus, len(bus.events)-1); event.Action != auditActionUpdate || !strings.Contains(event.NewValue, `"status":"APPROVED"`) {
		t.Fatalf("approve event: %+v", event)
	}

	if w = grcRequest(r, http.MethodPost, "/api/grc/policies/"+itoa(created.ID)+"/publish"); w.Code != http.StatusConflict {
		t.Fatalf("publish without effectiveFrom status=%d body=%s", w.Code, w.Body.String())
	}
	withDate := `{"reference":"POL-HTTP","title":"Politique HTTP","version":"1.1","approverProfileId":` + itoa(f.profile.ID) + `,"effectiveFrom":"2026-06-01T00:00:00Z"}`
	if w = grcJSONRequest(r, http.MethodPut, "/api/grc/policies/"+itoa(created.ID), withDate); w.Code != http.StatusOK {
		t.Fatalf("effective date status=%d body=%s", w.Code, w.Body.String())
	}
	if w = grcRequest(denied, http.MethodPost, "/api/grc/policies/"+itoa(created.ID)+"/publish"); w.Code != http.StatusForbidden {
		t.Fatalf("publish without permission status=%d", w.Code)
	}
	if w = grcRequest(r, http.MethodPost, "/api/grc/policies/"+itoa(created.ID)+"/publish"); w.Code != http.StatusOK {
		t.Fatalf("publish status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(requireAuditEvent(t, bus, len(bus.events)-1).NewValue, `"status":"PUBLISHED"`) {
		t.Fatal("publish event missing status")
	}

	if w = grcRequest(grcServiceRouter(f.svc, []string{"*"}), http.MethodPost, "/api/grc/policies/"+itoa(created.ID)+"/retire"); w.Code != http.StatusOK {
		t.Fatalf("wildcard retire status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(requireAuditEvent(t, bus, len(bus.events)-1).NewValue, `"status":"RETIRED"`) {
		t.Fatal("retire event missing status")
	}
	eventsBefore = len(bus.events)
	if w = grcJSONRequest(r, http.MethodPut, "/api/grc/policies/"+itoa(created.ID), `{"reference":"POL-HTTP","title":"Bloquée","version":"1.1"}`); w.Code != http.StatusConflict || len(bus.events) != eventsBefore {
		t.Fatalf("retired update status=%d events=%d body=%s", w.Code, len(bus.events), w.Body.String())
	}

	reader := grcServiceRouter(f.svc, []string{"grc.policy.read"})
	if w = grcRequest(reader, http.MethodGet, "/api/grc/policies?search=does-not-exist"); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"items":[]`) || len(bus.events) != eventsBefore {
		t.Fatalf("empty list status=%d events=%d body=%s", w.Code, len(bus.events), w.Body.String())
	}
	for _, path := range []string{"?page=abc", "?frameworkId=0", "?status=REOPENED"} {
		if w = grcRequest(reader, http.MethodGet, "/api/grc/policies"+path); w.Code != http.StatusBadRequest {
			t.Fatalf("%s status=%d", path, w.Code)
		}
	}
	if w = grcRequest(grcTestRouter(nil), http.MethodGet, "/api/grc/policies"); w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status=%d", w.Code)
	}
	if w = grcRequest(grcTestRouter([]string{"grc.asset.read"}), http.MethodPost, "/api/grc/policies"); w.Code != http.StatusForbidden {
		t.Fatalf("unrelated permission status=%d", w.Code)
	}
}
