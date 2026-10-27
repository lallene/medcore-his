package grc

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"testing"
)

// GRC-04: risk routes on the real router with a real service.

func riskJSON(t *testing.T, f assetFixture, assetID uint, reference string, extra map[string]any) string {
	t.Helper()
	body := map[string]any{
		"reference": reference, "title": "Risque " + reference, "description": "Description",
		"assetId": assetID, "owningServiceId": f.service.ID, "ownerProfileId": f.profile.ID,
		"likelihood": 4, "impact": 5, "residualLikelihood": 2, "residualImpact": 3,
		"treatment": "reduce", "status": "open",
	}
	for k, v := range extra {
		body[k] = v
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// Server-managed and derived fields a client might try to force.
var riskInjection = map[string]any{
	"id": 4242, "createdBy": 1, "updatedBy": 1, "createdAt": "2000-01-01T00:00:00Z",
	"inherentScore": 1, "residualScore": 1, "inherentLevel": "LOW", "residualLevel": "LOW",
}

func decodeRisk(t *testing.T, body []byte) Risk {
	t.Helper()
	var risk Risk
	if err := json.Unmarshal(body, &risk); err != nil {
		t.Fatalf("decode risk: %v (%s)", err, body)
	}
	return risk
}

func TestRiskRegistryRoutesCRUD(t *testing.T) {
	f := newAssetFixture(t)
	svc := NewService(f.db)
	asset := createRiskAsset(t, f)
	bus := withCaptureBus(t)
	r := grcServiceRouter(svc, []string{"grc.risk.read", "grc.risk.create", "grc.risk.update"})

	w := grcJSONRequest(r, http.MethodPost, "/api/grc/risks", riskJSON(t, f, asset.ID, "risk-http", riskInjection))
	if w.Code != http.StatusCreated {
		t.Fatalf("POST status=%d body=%s", w.Code, w.Body.String())
	}
	created := decodeRisk(t, w.Body.Bytes())
	if created.ID == 4242 || created.Reference != "RISK-HTTP" || created.CreatedBy != 7 || created.CreatedAt.Year() == 2000 ||
		created.InherentScore != 20 || created.InherentLevel != RiskLevelCritical ||
		created.ResidualScore != 6 || created.ResidualLevel != RiskLevelMedium ||
		created.Asset == nil || created.OwnerProfile == nil || created.OwningService == nil {
		t.Fatalf("server-managed fields not enforced: %+v", created)
	}
	if event := requireAuditEvent(t, bus, 0); event.Action != auditActionCreate || event.UserID == nil || *event.UserID != 7 {
		t.Fatalf("unexpected create event: %+v", event)
	}

	path := fmt.Sprintf("/api/grc/risks/%d", created.ID)
	w = grcRequest(r, http.MethodGet, path)
	if w.Code != http.StatusOK || decodeRisk(t, w.Body.Bytes()).ResidualLevel != RiskLevelMedium {
		t.Fatalf("GET detail status=%d body=%s", w.Code, w.Body.String())
	}

	update := riskJSON(t, f, asset.ID, "RISK-HTTP", map[string]any{"residualLikelihood": 5, "residualImpact": 5, "status": "in_treatment", "residualScore": 2})
	w = grcJSONRequest(r, http.MethodPut, path, update)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", w.Code, w.Body.String())
	}
	updated := decodeRisk(t, w.Body.Bytes())
	if updated.ResidualScore != 25 || updated.ResidualLevel != RiskLevelCritical || updated.Status != RiskStatusInTreatment || updated.CreatedBy != 7 {
		t.Fatalf("unexpected updated risk: %+v", updated)
	}
	if event := requireAuditEvent(t, bus, 1); event.Action != auditActionUpdate || event.RecordID != created.ID {
		t.Fatalf("unexpected update event: %+v", event)
	}

	w = grcRequest(r, http.MethodGet, fmt.Sprintf("/api/grc/risks?search=http&status=IN_TREATMENT&residualLevel=critical&inherentLevel=CRITICAL&assetId=%d&page=1&limit=5", asset.ID))
	var page RiskPage
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || w.Code != http.StatusOK {
		t.Fatalf("GET list status=%d err=%v", w.Code, err)
	}
	if page.Total != 1 || page.Items[0].ID != created.ID || page.Items[0].InherentLevel != RiskLevelCritical || page.Limit != 5 {
		t.Fatalf("unexpected list: %+v", page)
	}
	if w := grcRequest(r, http.MethodDelete, path); w.Code != http.StatusNotFound {
		t.Fatalf("DELETE status=%d, want 404", w.Code)
	}
	if len(bus.events) != 2 {
		t.Fatalf("audit events=%d, want 2", len(bus.events))
	}

	// Empty list is an empty array, never null.
	w = grcRequest(r, http.MethodGet, "/api/grc/risks?search=absent")
	if !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Fatalf("empty list body: %s", w.Body.String())
	}
}

func TestRiskRegistryRoutesErrors(t *testing.T) {
	f := newAssetFixture(t)
	svc := NewService(f.db)
	asset := createRiskAsset(t, f)
	r := grcServiceRouter(svc, []string{"*"})
	if w := grcJSONRequest(r, http.MethodPost, "/api/grc/risks", riskJSON(t, f, asset.ID, "RISK-DUP", nil)); w.Code != http.StatusCreated {
		t.Fatalf("seed status=%d", w.Code)
	}
	bus := withCaptureBus(t)

	cases := []struct {
		method, path, body string
		want               int
	}{
		{http.MethodGet, "/api/grc/risks/abc", "", http.StatusBadRequest},
		{http.MethodGet, "/api/grc/risks/999999", "", http.StatusNotFound},
		{http.MethodPut, "/api/grc/risks/999999", riskJSON(t, f, asset.ID, "RISK-404", nil), http.StatusNotFound},
		{http.MethodPost, "/api/grc/risks", malformedJSON, http.StatusBadRequest},
		{http.MethodPost, "/api/grc/risks", riskJSON(t, f, asset.ID, "RISK-BAD", map[string]any{"likelihood": 6}), http.StatusBadRequest},
		{http.MethodPost, "/api/grc/risks", riskJSON(t, f, asset.ID, "RISK-BAD", map[string]any{"status": "ARCHIVED"}), http.StatusBadRequest},
		{http.MethodPost, "/api/grc/risks", riskJSON(t, f, asset.ID, "RISK-BAD", map[string]any{"treatment": "IGNORE"}), http.StatusBadRequest},
		{http.MethodPost, "/api/grc/risks", riskJSON(t, f, asset.ID, "RISK-BAD", map[string]any{"assetId": 0}), http.StatusBadRequest},
		{http.MethodPost, "/api/grc/risks", riskJSON(t, f, 999999, "RISK-BAD", nil), http.StatusNotFound},
		{http.MethodPost, "/api/grc/risks", riskJSON(t, f, asset.ID, "RISK-DUP", nil), http.StatusConflict},
		{http.MethodGet, "/api/grc/risks?page=x", "", http.StatusBadRequest},
		{http.MethodGet, "/api/grc/risks?limit=1.5", "", http.StatusBadRequest},
		{http.MethodGet, "/api/grc/risks?assetId=0", "", http.StatusBadRequest},
		{http.MethodGet, "/api/grc/risks?residualLevel=SEVERE", "", http.StatusBadRequest},
		{http.MethodGet, "/api/grc/risks?inherentLevel=1", "", http.StatusBadRequest},
		{http.MethodGet, "/api/grc/risks?status=ARCHIVED", "", http.StatusBadRequest},
		{http.MethodGet, "/api/grc/controls?riskId=abc", "", http.StatusBadRequest},
	}
	for _, tc := range cases {
		if w := grcJSONRequest(r, tc.method, tc.path, tc.body); w.Code != tc.want {
			t.Errorf("%s %s status=%d, want %d (%s)", tc.method, tc.path, w.Code, tc.want, w.Body.String())
		}
	}
	if len(bus.events) != 0 {
		t.Fatalf("failed mutations published %d audit events", len(bus.events))
	}
}

func TestRiskRegistryRoutesRBAC(t *testing.T) {
	f := newAssetFixture(t)
	svc := NewService(f.db)
	asset := createRiskAsset(t, f)
	seed, err := svc.SaveRisk(0, 1, validRiskRequest(f, asset.ID))
	if err != nil {
		t.Fatal(err)
	}
	detail := fmt.Sprintf("/api/grc/risks/%d", seed.ID)
	read := [][3]string{{http.MethodGet, "/api/grc/risks", ""}, {http.MethodGet, detail, ""}}
	create := [3]string{http.MethodPost, "/api/grc/risks", riskJSON(t, f, asset.ID, "RISK-NEW", nil)}
	update := [3]string{http.MethodPut, detail, riskJSON(t, f, asset.ID, seed.Reference, nil)}

	check := func(name string, permissions []string, calls [][3]string, want int) {
		t.Helper()
		router := grcTestRouter(nil)
		if permissions != nil {
			router = grcServiceRouter(svc, permissions)
		}
		for _, c := range calls {
			if w := grcJSONRequest(router, c[0], c[1], c[2]); w.Code != want {
				t.Errorf("%s: %s %s status=%d, want %d", name, c[0], c[1], w.Code, want)
			}
		}
	}
	all := append(append([][3]string{}, read...), create, update)
	check("unauthenticated", nil, all, http.StatusUnauthorized)
	check("unrelated", []string{"grc.asset.read", "grc.asset.manage", "grc.dashboard.read"}, all, http.StatusForbidden)
	check("read", []string{"grc.risk.read"}, read, http.StatusOK)
	check("read-only cannot mutate", []string{"grc.risk.read"}, [][3]string{create, update}, http.StatusForbidden)
	check("approve does not grant writes", []string{"grc.risk.approve"}, [][3]string{create, update}, http.StatusForbidden)
	check("create cannot update", []string{"grc.risk.create"}, [][3]string{update}, http.StatusForbidden)
	check("update cannot create", []string{"grc.risk.update"}, [][3]string{create}, http.StatusForbidden)
	check("create", []string{"grc.risk.create"}, [][3]string{create}, http.StatusCreated)
	check("update", []string{"grc.risk.update"}, [][3]string{update}, http.StatusOK)
	check("wildcard", []string{"*"}, read, http.StatusOK)
}

func TestRiskAuditSnapshotAllowlist(t *testing.T) {
	f := newAssetFixture(t)
	svc := NewService(f.db)
	asset := createRiskAsset(t, f)
	bus := withCaptureBus(t)

	if _, err := svc.SaveRisk(0, 9, validRiskRequest(f, asset.ID)); err != nil {
		t.Fatal(err)
	}
	snapshot := decodeAuditJSON(t, requireAuditEvent(t, bus, 0).NewValue)
	keys := make([]string, 0, len(snapshot))
	for key := range snapshot {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	// Free text (description, threat, vulnerability) and derived levels stay out.
	want := "approverProfileId,assetId,impact,inherentScore,likelihood,ownerProfileId,owningServiceId,reference,residualImpact,residualLikelihood,residualScore,status,title,treatment"
	if strings.Join(keys, ",") != want {
		t.Fatalf("risk audit keys = %v, want %s", keys, want)
	}
}
