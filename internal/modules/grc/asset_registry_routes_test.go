package grc

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// GRC-03: asset routes on the real router with a real service.

func assetJSON(t *testing.T, f assetFixture, code string) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"code": code, "name": "Actif " + code, "type": "server", "criticality": "high",
		"status": "active", "description": "Hébergement", "owningServiceId": f.service.ID,
		"ownerProfileId": f.profile.ID,
		// Server-managed fields: ignored by the request DTO.
		"id": 4242, "createdBy": 1, "updatedBy": 1, "createdAt": "2000-01-01T00:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func decodeAsset(t *testing.T, body []byte) Asset {
	t.Helper()
	var asset Asset
	if err := json.Unmarshal(body, &asset); err != nil {
		t.Fatalf("decode asset: %v (%s)", err, body)
	}
	return asset
}

func TestAssetRegistryRoutesCRUD(t *testing.T) {
	f := newAssetFixture(t)
	svc := NewService(f.db)
	bus := withCaptureBus(t)
	manager := grcServiceRouter(svc, []string{"grc.asset.read", "grc.asset.manage"})

	w := grcJSONRequest(manager, http.MethodPost, "/api/grc/assets", assetJSON(t, f, "srv-http"))
	if w.Code != http.StatusCreated {
		t.Fatalf("POST status=%d body=%s", w.Code, w.Body.String())
	}
	created := decodeAsset(t, w.Body.Bytes())
	// The actor comes from the authenticated context (user 7), never the body.
	if created.ID == 4242 || created.Code != "SRV-HTTP" || created.Type != AssetTypeServer ||
		created.CreatedBy != 7 || created.UpdatedBy != 7 || created.CreatedAt.Year() == 2000 ||
		created.OwningService == nil || created.OwnerProfile == nil {
		t.Fatalf("unexpected created asset: %+v", created)
	}
	if event := requireAuditEvent(t, bus, 0); event.Action != auditActionCreate || event.UserID == nil || *event.UserID != 7 {
		t.Fatalf("unexpected create event: %+v", event)
	}

	path := fmt.Sprintf("/api/grc/assets/%d", created.ID)
	w = grcRequest(manager, http.MethodGet, path)
	if w.Code != http.StatusOK || decodeAsset(t, w.Body.Bytes()).Code != "SRV-HTTP" {
		t.Fatalf("GET detail status=%d body=%s", w.Code, w.Body.String())
	}

	update := `{"code":"SRV-HTTP","name":"Serveur renommé","type":"SERVER","criticality":"CRITICAL","status":"RETIRED","createdBy":99}`
	w = grcJSONRequest(manager, http.MethodPut, path, update)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", w.Code, w.Body.String())
	}
	updated := decodeAsset(t, w.Body.Bytes())
	if updated.ID != created.ID || updated.Name != "Serveur renommé" || updated.Status != AssetStatusRetired ||
		updated.CreatedBy != 7 || updated.OwningServiceID != nil || updated.OwnerProfileID != nil {
		t.Fatalf("unexpected updated asset: %+v", updated)
	}
	if event := requireAuditEvent(t, bus, 1); event.Action != auditActionUpdate || event.RecordID != created.ID {
		t.Fatalf("unexpected update event: %+v", event)
	}

	w = grcRequest(manager, http.MethodGet, "/api/grc/assets?search=renomm&status=retired&criticality=critical&type=server&page=1&limit=10")
	var page AssetPage
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || w.Code != http.StatusOK {
		t.Fatalf("GET list status=%d err=%v", w.Code, err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != created.ID || page.Limit != 10 || page.TotalPages != 1 {
		t.Fatalf("unexpected list page: %+v", page)
	}

	// No physical delete route.
	if w := grcRequest(manager, http.MethodDelete, path); w.Code != http.StatusNotFound {
		t.Fatalf("DELETE status=%d, want 404", w.Code)
	}
	if len(bus.events) != 2 {
		t.Fatalf("audit events=%d, want 2", len(bus.events))
	}
}

func TestAssetRegistryRoutesErrors(t *testing.T) {
	f := newAssetFixture(t)
	svc := NewService(f.db)
	manager := grcServiceRouter(svc, []string{"*"})
	w := grcJSONRequest(manager, http.MethodPost, "/api/grc/assets", assetJSON(t, f, "SRV-ERR"))
	if w.Code != http.StatusCreated {
		t.Fatalf("seed status=%d", w.Code)
	}
	bus := withCaptureBus(t)

	cases := []struct {
		method, path, body string
		want               int
	}{
		{http.MethodGet, "/api/grc/assets/abc", "", http.StatusBadRequest},
		{http.MethodGet, "/api/grc/assets/0", "", http.StatusBadRequest},
		{http.MethodGet, "/api/grc/assets/999999", "", http.StatusNotFound},
		{http.MethodPut, "/api/grc/assets/999999", assetJSON(t, f, "SRV-404"), http.StatusNotFound},
		{http.MethodPut, "/api/grc/assets/abc", "{}", http.StatusBadRequest},
		{http.MethodPost, "/api/grc/assets", malformedJSON, http.StatusBadRequest},
		{http.MethodPost, "/api/grc/assets", `{"code":"X","name":"Y","type":"LAPTOP","criticality":"LOW","status":"ACTIVE"}`, http.StatusBadRequest},
		{http.MethodPost, "/api/grc/assets", `{"code":"X","name":"Y","type":"SERVER","criticality":"LOW","status":"ACTIVE","ownerProfileId":0}`, http.StatusBadRequest},
		{http.MethodPost, "/api/grc/assets", `{"code":"X","name":"Y","type":"SERVER","criticality":"LOW","status":"ACTIVE","owningServiceId":-1}`, http.StatusBadRequest},
		{http.MethodPost, "/api/grc/assets", assetJSON(t, f, "SRV-ERR"), http.StatusConflict},
		{http.MethodGet, "/api/grc/assets?page=abc", "", http.StatusBadRequest},
		{http.MethodGet, "/api/grc/assets?limit=1.5", "", http.StatusBadRequest},
		{http.MethodGet, "/api/grc/assets?serviceId=0", "", http.StatusBadRequest},
		{http.MethodGet, "/api/grc/assets?ownerProfileId=x", "", http.StatusBadRequest},
		{http.MethodGet, "/api/grc/assets?status=DELETED", "", http.StatusBadRequest},
		{http.MethodGet, "/api/grc/assets?criticality=VERY_HIGH", "", http.StatusBadRequest},
		{http.MethodGet, "/api/grc/assets?type=LAPTOP", "", http.StatusBadRequest},
	}
	for _, tc := range cases {
		if w := grcJSONRequest(manager, tc.method, tc.path, tc.body); w.Code != tc.want {
			t.Errorf("%s %s status=%d, want %d (%s)", tc.method, tc.path, w.Code, tc.want, w.Body.String())
		}
	}
	if len(bus.events) != 0 {
		t.Fatalf("failed mutations published %d audit events", len(bus.events))
	}
}

func TestAssetRegistryRoutesRBAC(t *testing.T) {
	f := newAssetFixture(t)
	svc := NewService(f.db)
	seed, err := svc.SaveAsset(0, 1, validAssetRequest(f))
	if err != nil {
		t.Fatal(err)
	}
	detail := fmt.Sprintf("/api/grc/assets/%d", seed.ID)
	update := assetJSON(t, f, seed.Code)

	type call struct{ method, path, body string }
	read := []call{{http.MethodGet, "/api/grc/assets", ""}, {http.MethodGet, detail, ""}}
	write := []call{{http.MethodPost, "/api/grc/assets", assetJSON(t, f, "SRV-NEW")}, {http.MethodPut, detail, update}}

	check := func(name string, permissions []string, calls []call, want int) {
		t.Helper()
		var r http.Handler
		if permissions == nil {
			r = grcTestRouter(nil)
		} else {
			r = grcServiceRouter(svc, permissions)
		}
		for _, c := range calls {
			if w := grcJSONRequest(r, c.method, c.path, c.body); w.Code != want {
				t.Errorf("%s: %s %s status=%d, want %d", name, c.method, c.path, w.Code, want)
			}
		}
	}

	check("unauthenticated", nil, append(read, write...), http.StatusUnauthorized)
	check("unrelated", []string{"grc.risk.read", "grc.risk.create", "grc.dashboard.read"}, append(read, write...), http.StatusForbidden)
	check("read-only reads", []string{"grc.asset.read"}, read, http.StatusOK)
	check("read-only cannot mutate", []string{"grc.asset.read"}, write, http.StatusForbidden)
	check("manage without read cannot read", []string{"grc.asset.manage"}, read, http.StatusForbidden)
	check("wildcard reads", []string{"*"}, read, http.StatusOK)
	check("manage creates", []string{"grc.asset.manage"}, write[:1], http.StatusCreated)
	check("manage updates", []string{"grc.asset.manage"}, write[1:], http.StatusOK)
}
