package grc

import (
	"net/http"
	"sort"
	"strings"
	"testing"
)

// GRC-03: pure validation, relation rules on update, list search/filters/
// pagination and the audit allowlist of the asset registry.

func TestValidateAssetRequest(t *testing.T) {
	f := newAssetFixture(t)
	zero := uint(0)

	valid := validAssetRequest(f)
	normalizeAssetRequest(&valid)
	if err := validateAssetRequest(valid); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	if valid.Code != "APP-MEDCORE" || valid.Name != "MedCore HIS" || valid.Type != AssetTypeApplication ||
		valid.Criticality != AssetCriticalityCritical || valid.Status != AssetStatusActive ||
		valid.Description != "Système d'information hospitalier" {
		t.Fatalf("unexpected normalization: %+v", valid)
	}

	atLimit := valid
	atLimit.Code = strings.Repeat("É", assetCodeMaxLength)
	atLimit.Name = strings.Repeat("é", assetNameMaxLength)
	if err := validateAssetRequest(atLimit); err != nil {
		t.Fatalf("lengths at the column limit rejected: %v", err)
	}

	cases := map[string]func(*AssetRequest){
		"whitespace code":                 func(r *AssetRequest) { r.Code = "   " },
		"whitespace name":                 func(r *AssetRequest) { r.Name = "\t " },
		"code too long":                   func(r *AssetRequest) { r.Code = strings.Repeat("É", assetCodeMaxLength+1) },
		"name too long":                   func(r *AssetRequest) { r.Name = strings.Repeat("é", assetNameMaxLength+1) },
		"empty type":                      func(r *AssetRequest) { r.Type = "" },
		"invalid type":                    func(r *AssetRequest) { r.Type = "LAPTOP" },
		"empty criticality":               func(r *AssetRequest) { r.Criticality = "" },
		"risk level is not a criticality": func(r *AssetRequest) { r.Criticality = "VERY_HIGH" },
		"empty status":                    func(r *AssetRequest) { r.Status = "" },
		"invalid status":                  func(r *AssetRequest) { r.Status = "DELETED" },
		"zero service id":                 func(r *AssetRequest) { r.OwningServiceID = &zero },
		"zero owner id":                   func(r *AssetRequest) { r.OwnerProfileID = &zero },
	}
	for name, mutate := range cases {
		r := valid
		mutate(&r)
		normalizeAssetRequest(&r)
		err := validateAssetRequest(r)
		requireServiceError(t, err, http.StatusBadRequest)
		if err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
}

func TestSaveAssetRelationErrors(t *testing.T) {
	f := newAssetFixture(t)
	svc := NewService(f.db)
	unknown := uint(999999)

	cases := []struct {
		name   string
		status int
		mutate func(*AssetRequest)
	}{
		{"unknown service", http.StatusNotFound, func(r *AssetRequest) { r.OwningServiceID = &unknown }},
		{"inactive service", http.StatusConflict, func(r *AssetRequest) { r.OwningServiceID = &f.inactiveService.ID }},
		{"unknown owner", http.StatusNotFound, func(r *AssetRequest) { r.OwnerProfileID = &unknown }},
		{"inactive owner", http.StatusConflict, func(r *AssetRequest) { r.OwnerProfileID = &f.inactiveProfile.ID }},
	}
	for _, tc := range cases {
		r := validAssetRequest(f)
		tc.mutate(&r)
		_, err := svc.SaveAsset(0, 1, r)
		requireServiceError(t, err, tc.status)
	}

	// Unowned asset: both relations are optional.
	r := validAssetRequest(f)
	r.Code, r.OwningServiceID, r.OwnerProfileID = "AST-UNOWNED", nil, nil
	asset, err := svc.SaveAsset(0, 1, r)
	if err != nil || asset.OwningService != nil || asset.OwnerProfile != nil {
		t.Fatalf("unowned asset: %v %+v", err, asset)
	}
}

func TestSaveAssetKeepsHistoricalRelationsEditable(t *testing.T) {
	f := newAssetFixture(t)
	svc := NewService(f.db)

	r := validAssetRequest(f)
	asset, err := svc.SaveAsset(0, 1, r)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.db.Model(&f.service).Update("active", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Model(&f.profile).Update("active", false).Error; err != nil {
		t.Fatal(err)
	}

	// Unchanged relations are not re-validated: the asset can be retired.
	r.Status = AssetStatusRetired
	retired, err := svc.SaveAsset(asset.ID, 2, r)
	if err != nil || retired.Status != AssetStatusRetired || retired.UpdatedBy != 2 || retired.CreatedBy != 1 {
		t.Fatalf("retire with deactivated relations: %v %+v", err, retired)
	}

	// A newly assigned relation must still be active.
	r.OwningServiceID = &f.inactiveService.ID
	_, err = svc.SaveAsset(asset.ID, 2, r)
	requireServiceError(t, err, http.StatusConflict)
	r.OwningServiceID = &f.service.ID
	r.OwnerProfileID = &f.inactiveProfile.ID
	_, err = svc.SaveAsset(asset.ID, 2, r)
	requireServiceError(t, err, http.StatusConflict)

	// Removing a relation is always allowed.
	r.OwnerProfileID, r.OwningServiceID = nil, nil
	cleared, err := svc.SaveAsset(asset.ID, 3, r)
	if err != nil || cleared.OwnerProfileID != nil || cleared.OwningServiceID != nil {
		t.Fatalf("clear relations: %v %+v", err, cleared)
	}
}

func TestSaveAssetNotFound(t *testing.T) {
	f := newAssetFixture(t)
	svc := NewService(f.db)
	bus := withCaptureBus(t)

	_, err := svc.SaveAsset(999999, 1, validAssetRequest(f))
	requireServiceError(t, err, http.StatusNotFound)
	_, err = svc.FindAsset(999999)
	requireServiceError(t, err, http.StatusNotFound)
	if len(bus.events) != 0 {
		t.Fatalf("not found update published %d events", len(bus.events))
	}
}

func seedAssetRegistry(t *testing.T, f assetFixture, svc *GRCService) map[string]Asset {
	t.Helper()

	rows := []AssetRequest{
		{Code: "APP-DPI", Name: "Dossier patient", Type: AssetTypeApplication, Criticality: AssetCriticalityCritical, Status: AssetStatusActive, Description: "Base clinique", OwningServiceID: &f.service.ID, OwnerProfileID: &f.profile.ID},
		{Code: "SRV-01", Name: "Serveur base", Type: AssetTypeServer, Criticality: AssetCriticalityHigh, Status: AssetStatusActive, OwningServiceID: &f.service.ID},
		{Code: "DB-LAB", Name: "Base laboratoire", Type: AssetTypeDatabase, Criticality: AssetCriticalityHigh, Status: AssetStatusInactive, OwnerProfileID: &f.profile.ID},
		{Code: "NET-CORE", Name: "Réseau cœur", Type: AssetTypeNetwork, Criticality: AssetCriticalityMedium, Status: AssetStatusRetired},
		// Same name as APP-DPI: the id breaks the tie.
		{Code: "APP-DPI-2", Name: "Dossier patient", Type: AssetTypeApplication, Criticality: AssetCriticalityLow, Status: AssetStatusActive},
	}
	out := map[string]Asset{}
	for _, r := range rows {
		asset, err := svc.SaveAsset(0, 1, r)
		if err != nil {
			t.Fatal(err)
		}
		out[asset.Code] = *asset
	}
	return out
}

func assetCodes(page *AssetPage) []string {
	codes := make([]string, 0, len(page.Items))
	for _, a := range page.Items {
		codes = append(codes, a.Code)
	}
	return codes
}

func TestListAssetsRegistry(t *testing.T) {
	f := newAssetFixture(t)
	svc := NewService(f.db)
	seedAssetRegistry(t, f, svc)

	list := func(filter AssetFilter) *AssetPage {
		t.Helper()
		page, err := svc.ListAssets(filter)
		if err != nil {
			t.Fatal(err)
		}
		return page
	}
	same := func(name string, got []string, want ...string) {
		t.Helper()
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("%s = %v, want %v", name, got, want)
		}
	}

	// Ordered by name then id.
	all := list(AssetFilter{})
	same("all", assetCodes(all), "DB-LAB", "APP-DPI", "APP-DPI-2", "NET-CORE", "SRV-01")
	if all.Total != 5 || all.Page != 1 || all.Limit != 20 || all.TotalPages != 1 {
		t.Fatalf("unexpected page meta: %+v", all)
	}

	// Case-insensitive search over code, name and description.
	same("search code", assetCodes(list(AssetFilter{Search: "srv"})), "SRV-01")
	same("search name or description", assetCodes(list(AssetFilter{Search: "  BASE "})), "DB-LAB", "APP-DPI", "SRV-01")
	same("search description", assetCodes(list(AssetFilter{Search: "clinique"})), "APP-DPI")
	same("search none", assetCodes(list(AssetFilter{Search: "absent"})))

	same("type", assetCodes(list(AssetFilter{Type: "application"})), "APP-DPI", "APP-DPI-2")
	same("criticality", assetCodes(list(AssetFilter{Criticality: "HIGH"})), "DB-LAB", "SRV-01")
	same("status", assetCodes(list(AssetFilter{Status: "retired"})), "NET-CORE")
	same("service", assetCodes(list(AssetFilter{ServiceID: &f.service.ID})), "APP-DPI", "SRV-01")
	same("owner", assetCodes(list(AssetFilter{OwnerProfileID: &f.profile.ID})), "DB-LAB", "APP-DPI")
	same("combined", assetCodes(list(AssetFilter{Search: "base", Criticality: "HIGH", Status: "ACTIVE", ServiceID: &f.service.ID})), "SRV-01")

	// Pagination is applied after the filters, in SQL.
	page2 := list(AssetFilter{Page: 2, Limit: 2})
	same("page 2", assetCodes(page2), "APP-DPI-2", "NET-CORE")
	if page2.Total != 5 || page2.TotalPages != 3 || page2.Page != 2 || page2.Limit != 2 {
		t.Fatalf("unexpected page 2 meta: %+v", page2)
	}
	beyond := list(AssetFilter{Page: 9, Limit: 2})
	if len(beyond.Items) != 0 || beyond.Total != 5 {
		t.Fatalf("page beyond the end: %+v", beyond)
	}
	empty := list(AssetFilter{Search: "absent"})
	if empty.Total != 0 || empty.TotalPages != 0 || empty.Items == nil {
		t.Fatalf("empty page must have zero pages and a non-nil list: %+v", empty)
	}
	// Out-of-range bounds use the GRC normalization.
	if p := list(AssetFilter{Page: -1, Limit: 500}); p.Page != 1 || p.Limit != 20 {
		t.Fatalf("normalized bounds: %+v", p)
	}

	// Relations are preloaded for the registry table.
	first := list(AssetFilter{Search: "APP-DPI", Limit: 1}).Items[0]
	if first.OwningService == nil || first.OwnerProfile == nil {
		t.Fatalf("relations not preloaded: %+v", first)
	}
}

func TestAssetAuditSnapshotAllowlist(t *testing.T) {
	f := newAssetFixture(t)
	svc := NewService(f.db)
	bus := withCaptureBus(t)

	if _, err := svc.SaveAsset(0, 9, validAssetRequest(f)); err != nil {
		t.Fatal(err)
	}
	snapshot := decodeAuditJSON(t, requireAuditEvent(t, bus, 0).NewValue)
	keys := make([]string, 0, len(snapshot))
	for key := range snapshot {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	want := "code,criticality,name,ownerProfileId,owningServiceId,status,type"
	if strings.Join(keys, ",") != want {
		t.Fatalf("asset audit keys = %v, want %s", keys, want)
	}
}
