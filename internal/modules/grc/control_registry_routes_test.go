package grc

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"testing"
)

// GRC-06: control, implementation and control-risk routes on the real router.

func snapshotKeys(t *testing.T, raw string) string {
	t.Helper()
	keys := []string{}
	for key := range decodeAuditJSON(t, raw) {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

func TestControlRegistryRoutesCRUD(t *testing.T) {
	f, svc := newComplianceServiceFixture(t)
	asset := createComplianceAsset(t, svc, f, "AST-HTTP", AssetStatusActive)
	bus := withCaptureBus(t)
	r := grcServiceRouter(svc, []string{"grc.control.read", "grc.control.manage", "grc.control.implementation.read", "grc.control.implementation.manage"})

	body := fmt.Sprintf(`{"code":" ctl-http ","name":"Contrôle HTTP","category":"Accès","status":"active","ownerProfileId":%d,"owningServiceId":%d,
		"id":4242,"createdBy":1,"updatedBy":1,"createdAt":"2000-01-01T00:00:00Z","requirementCount":9,"riskCount":9,"implementationCount":9}`,
		f.profile.ID, f.service.ID)
	w := grcJSONRequest(r, http.MethodPost, "/api/grc/controls", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST control status=%d body=%s", w.Code, w.Body.String())
	}
	var control Control
	if err := json.Unmarshal(w.Body.Bytes(), &control); err != nil {
		t.Fatal(err)
	}
	if control.ID == 4242 || control.Code != "CTL-HTTP" || control.CreatedBy != 7 || control.CreatedAt.Year() == 2000 ||
		control.RequirementCount != 0 || control.RiskCount != 0 || control.ImplementationCount != 0 {
		t.Fatalf("server-managed fields not enforced: %+v", control)
	}
	created := requireAuditEvent(t, bus, 0)
	if created.Action != auditActionCreate || created.UserID == nil || *created.UserID != 7 {
		t.Fatalf("control create event: %+v", created)
	}
	if keys := snapshotKeys(t, created.NewValue); keys != "category,code,id,name,ownerProfileId,owningServiceId,status" {
		t.Fatalf("control audit keys: %s", keys)
	}

	path := fmt.Sprintf("/api/grc/controls/%d", control.ID)
	w = grcJSONRequest(r, http.MethodPut, path, `{"code":"CTL-HTTP","name":"Contrôle renommé","status":"inactive"}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"status":"INACTIVE"`) {
		t.Fatalf("PUT control status=%d body=%s", w.Code, w.Body.String())
	}
	w = grcJSONRequest(r, http.MethodPut, path, `{"code":"CTL-HTTP","name":"Contrôle réactivé","status":"active"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT control back to active: %d", w.Code)
	}

	implBody := fmt.Sprintf(`{"controlId":%d,"assetId":%d,"status":"partial","implementationDetails":"MFA sur le VPN","id":99,"createdBy":1}`, control.ID, asset.ID)
	w = grcJSONRequest(r, http.MethodPost, "/api/grc/control-implementations", implBody)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST implementation status=%d body=%s", w.Code, w.Body.String())
	}
	var implementation ControlImplementation
	if err := json.Unmarshal(w.Body.Bytes(), &implementation); err != nil {
		t.Fatal(err)
	}
	if implementation.ID == 99 || implementation.CreatedBy != 7 || implementation.Status != ControlImplementationStatusPartial ||
		implementation.Control == nil || implementation.Asset == nil {
		t.Fatalf("unexpected implementation: %+v", implementation)
	}
	implEvent := requireAuditEvent(t, bus, 3)
	if implEvent.Module != auditModuleGRCControlImplementation || implEvent.Action != auditActionCreate {
		t.Fatalf("implementation event: %+v", implEvent)
	}
	if keys := snapshotKeys(t, implEvent.NewValue); keys != "assetId,controlId,id,status" {
		t.Fatalf("implementation audit keys: %s", keys)
	}

	implPath := fmt.Sprintf("/api/grc/control-implementations/%d", implementation.ID)
	w = grcJSONRequest(r, http.MethodPut, implPath, fmt.Sprintf(`{"controlId":%d,"assetId":%d,"status":"implemented","implementedAt":"2026-05-01T00:00:00Z"}`, control.ID, asset.ID))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"status":"IMPLEMENTED"`) {
		t.Fatalf("PUT implementation status=%d body=%s", w.Code, w.Body.String())
	}

	w = grcRequest(r, http.MethodGet, path)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"implementationCount":1`) {
		t.Fatalf("GET control detail: %d %s", w.Code, w.Body.String())
	}
	w = grcRequest(r, http.MethodGet, fmt.Sprintf("/api/grc/control-implementations?controlId=%d&status=IMPLEMENTED&page=1&limit=5", control.ID))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"total":1`) {
		t.Fatalf("GET implementations: %d %s", w.Code, w.Body.String())
	}
	w = grcRequest(r, http.MethodGet, "/api/grc/controls?search=renomm&status=active&limit=5")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Fatalf("search must not widen the status filter: %s", w.Body.String())
	}
	for _, p := range []string{path, implPath} {
		if w := grcRequest(r, http.MethodDelete, p); w.Code != http.StatusNotFound {
			t.Fatalf("DELETE %s status=%d, want 404", p, w.Code)
		}
	}
	if len(bus.events) != 5 {
		t.Fatalf("audit events=%d, want 5", len(bus.events))
	}
}

func TestControlRiskRoutesAndAudit(t *testing.T) {
	f, svc := newComplianceServiceFixture(t)
	reg := seedComplianceRegistry(t, f, svc)
	asset := createComplianceAsset(t, svc, f, "AST-CR", AssetStatusActive)
	risk, err := svc.SaveRisk(0, 1, validRiskRequest(f, asset.ID))
	if err != nil {
		t.Fatal(err)
	}
	bus := withCaptureBus(t)
	r := grcServiceRouter(svc, []string{"grc.control.manage", "grc.risk.read"})
	link := fmt.Sprintf(`{"controlId":%d,"riskId":%d}`, reg.c1.ID, risk.ID)

	if w := grcJSONRequest(r, http.MethodPost, "/api/grc/control-risks", link); w.Code != http.StatusCreated {
		t.Fatalf("link status=%d body=%s", w.Code, w.Body.String())
	}
	event := requireAuditEvent(t, bus, 0)
	if event.Module != auditModuleGRCControlRisk || event.Action != auditActionLink || *event.UserID != 7 {
		t.Fatalf("link event: %+v", event)
	}
	if keys := snapshotKeys(t, event.NewValue); keys != "controlId,id,riskId" {
		t.Fatalf("control-risk audit keys: %s", keys)
	}
	w := grcRequest(r, http.MethodGet, fmt.Sprintf("/api/grc/risks?controlId=%d", reg.c1.ID))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"total":1`) {
		t.Fatalf("risks of the control: %d %s", w.Code, w.Body.String())
	}

	for _, tc := range []struct {
		method, path, body string
		want               int
	}{
		{http.MethodPost, "/api/grc/control-risks", link, http.StatusConflict},
		{http.MethodPost, "/api/grc/control-risks", fmt.Sprintf(`{"controlId":%d,"riskId":999999}`, reg.c1.ID), http.StatusNotFound},
		{http.MethodPost, "/api/grc/control-risks", `{"controlId":0,"riskId":0}`, http.StatusBadRequest},
		{http.MethodDelete, fmt.Sprintf("/api/grc/control-risks/%d/%d", reg.c2.ID, risk.ID), "", http.StatusNotFound},
	} {
		if w := grcJSONRequest(r, tc.method, tc.path, tc.body); w.Code != tc.want {
			t.Errorf("%s %s status=%d, want %d", tc.method, tc.path, w.Code, tc.want)
		}
	}
	if len(bus.events) != 1 {
		t.Fatalf("failed mutations published events: %d", len(bus.events))
	}
	if w := grcRequest(r, http.MethodDelete, fmt.Sprintf("/api/grc/control-risks/%d/%d", reg.c1.ID, risk.ID)); w.Code != http.StatusNoContent {
		t.Fatalf("unlink status=%d", w.Code)
	}
	if event := requireAuditEvent(t, bus, 1); event.Action != auditActionUnlink || event.NewValue != "" {
		t.Fatalf("unlink event: %+v", event)
	}
}

func TestControlRegistryRoutesErrorsAndRBAC(t *testing.T) {
	f, svc := newComplianceServiceFixture(t)
	reg := seedComplianceRegistry(t, f, svc)
	asset := createComplianceAsset(t, svc, f, "AST-RBAC", AssetStatusActive)
	impl, err := svc.SaveControlImplementation(0, 1, ControlImplementationRequest{ControlID: reg.c1.ID, AssetID: asset.ID, Status: "PLANNED"})
	if err != nil {
		t.Fatal(err)
	}
	bus := withCaptureBus(t)
	admin := grcServiceRouter(svc, []string{"*"})
	for _, tc := range []struct {
		method, path, body string
		want               int
	}{
		{http.MethodGet, "/api/grc/controls?page=x", "", http.StatusBadRequest},
		{http.MethodGet, "/api/grc/controls?limit=1.5", "", http.StatusBadRequest},
		{http.MethodGet, "/api/grc/controls?requirementId=0", "", http.StatusBadRequest},
		{http.MethodGet, "/api/grc/controls?riskId=abc", "", http.StatusBadRequest},
		{http.MethodGet, "/api/grc/controls?status=ARCHIVED", "", http.StatusBadRequest},
		{http.MethodGet, "/api/grc/control-implementations?page=x", "", http.StatusBadRequest},
		{http.MethodGet, "/api/grc/control-implementations?controlId=0", "", http.StatusBadRequest},
		{http.MethodGet, "/api/grc/control-implementations?status=DONE", "", http.StatusBadRequest},
		{http.MethodGet, "/api/grc/risks?controlId=-1", "", http.StatusBadRequest},
		{http.MethodGet, "/api/grc/controls/abc", "", http.StatusBadRequest},
		{http.MethodGet, "/api/grc/controls/999999", "", http.StatusNotFound},
		{http.MethodGet, "/api/grc/control-implementations/999999", "", http.StatusNotFound},
		{http.MethodPut, "/api/grc/controls/999999", `{"code":"X","name":"Y","status":"ACTIVE"}`, http.StatusNotFound},
		{http.MethodPost, "/api/grc/controls", malformedJSON, http.StatusBadRequest},
		{http.MethodPost, "/api/grc/controls", `{"code":"X","name":"Y","status":"ACTIVE","ownerProfileId":0}`, http.StatusBadRequest},
		{http.MethodPost, "/api/grc/controls", fmt.Sprintf(`{"code":"%s","name":"Doublon","status":"ACTIVE"}`, reg.c1.Code), http.StatusConflict},
		{http.MethodPost, "/api/grc/control-implementations", fmt.Sprintf(`{"controlId":%d,"assetId":%d,"status":"PLANNED"}`, reg.c1.ID, asset.ID), http.StatusConflict},
		{http.MethodPost, "/api/grc/control-implementations", `{"controlId":0,"assetId":1,"status":"PLANNED"}`, http.StatusBadRequest},
	} {
		if w := grcJSONRequest(admin, tc.method, tc.path, tc.body); w.Code != tc.want {
			t.Errorf("%s %s status=%d, want %d (%s)", tc.method, tc.path, w.Code, tc.want, w.Body.String())
		}
	}
	if len(bus.events) != 0 {
		t.Fatalf("failed requests published %d events", len(bus.events))
	}

	controlPath := fmt.Sprintf("/api/grc/controls/%d", reg.c1.ID)
	implPath := fmt.Sprintf("/api/grc/control-implementations/%d", impl.ID)
	updateControl := fmt.Sprintf(`{"code":"%s","name":"Sauvegardes","status":"ACTIVE"}`, reg.c1.Code)
	updateImpl := fmt.Sprintf(`{"controlId":%d,"assetId":%d,"status":"PARTIAL"}`, reg.c1.ID, asset.ID)
	status := func(permissions []string, method, path, body string) int {
		router := grcTestRouter(nil)
		if permissions != nil {
			router = grcServiceRouter(svc, permissions)
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
		{"unauthenticated", nil, http.MethodGet, "/api/grc/controls", "", http.StatusUnauthorized},
		{"unauthenticated impl", nil, http.MethodGet, "/api/grc/control-implementations", "", http.StatusUnauthorized},
		{"unrelated", []string{"grc.risk.read", "grc.requirement.read"}, http.MethodGet, "/api/grc/controls", "", http.StatusForbidden},
		{"control read", []string{"grc.control.read"}, http.MethodGet, controlPath, "", http.StatusOK},
		{"control read cannot update", []string{"grc.control.read"}, http.MethodPut, controlPath, updateControl, http.StatusForbidden},
		{"control read cannot read implementations", []string{"grc.control.read"}, http.MethodGet, implPath, "", http.StatusForbidden},
		{"control manage updates", []string{"grc.control.manage"}, http.MethodPut, controlPath, updateControl, http.StatusOK},
		{"control manage cannot update implementation", []string{"grc.control.manage"}, http.MethodPut, implPath, updateImpl, http.StatusForbidden},
		{"implementation read", []string{"grc.control.implementation.read"}, http.MethodGet, implPath, "", http.StatusOK},
		{"implementation read cannot mutate", []string{"grc.control.implementation.read"}, http.MethodPut, implPath, updateImpl, http.StatusForbidden},
		{"implementation manage", []string{"grc.control.implementation.manage"}, http.MethodPut, implPath, updateImpl, http.StatusOK},
		{"implementation manage cannot link risks", []string{"grc.control.implementation.manage"}, http.MethodPost, "/api/grc/control-risks", `{"controlId":1,"riskId":1}`, http.StatusForbidden},
		{"wildcard", []string{"*"}, http.MethodGet, "/api/grc/controls", "", http.StatusOK},
	} {
		if got := status(tc.permissions, tc.method, tc.path, tc.body); got != tc.want {
			t.Errorf("%s: status=%d, want %d", tc.name, got, tc.want)
		}
	}
}
