package grc

import (
	"net/http"
	"testing"
	"time"
)

func TestSecurityIncidentServiceLifecycleAndRelations(t *testing.T) {
	f := newGRC01DServiceFixture(t)
	detected := evidenceTestNow.Add(-2 * time.Hour)
	asset := createComplianceAsset(t, f.svc, f.assetFixture, "AST-INC", AssetStatusActive)
	other := createComplianceAsset(t, f.svc, f.assetFixture, "AST-INC-2", AssetStatusActive)

	created, err := f.svc.CreateSecurityIncident(11, SecurityIncidentRequest{
		Reference:       " inc-svc ",
		Title:           " Incident service ",
		Description:     "description absente de l'audit",
		Severity:        " critical ",
		Status:          SecurityIncidentStatusClosed,
		DetectedAt:      detected,
		OccurredAt:      timePtr(detected.Add(-time.Minute)),
		RiskID:          &f.risk.ID,
		AssetID:         &asset.ID,
		OwningServiceID: &f.service.ID,
		OwnerProfileID:  &f.profile.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != SecurityIncidentStatusOpen || created.ResolvedAt != nil || created.ClosedAt != nil ||
		created.Reference != "INC-SVC" || created.Severity != SecurityIncidentSeverityCritical ||
		created.CreatedBy != 11 || created.RiskID == nil || created.AssetID == nil {
		t.Fatalf("create did not force the server-owned state: %+v", created)
	}

	next := incidentUpdate(*created, SecurityIncidentStatusInvestigating)
	investigating, err := f.svc.UpdateSecurityIncident(created.ID, 12, next)
	if err != nil {
		t.Fatal(err)
	}
	next = incidentUpdate(*investigating, SecurityIncidentStatusContained)
	if _, err = f.svc.UpdateSecurityIncident(created.ID, 12, next); err != nil {
		t.Fatal(err)
	}
	f.setClock(evidenceTestNow.Add(time.Minute))
	next = incidentUpdate(*investigating, SecurityIncidentStatusResolved)
	next.Status = SecurityIncidentStatusResolved
	resolved, err := f.svc.UpdateSecurityIncident(created.ID, 12, next)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ResolvedAt == nil || !resolved.ResolvedAt.Equal(evidenceTestNow.Add(time.Minute)) || resolved.ClosedAt != nil {
		t.Fatalf("resolvedAt = %v", resolved.ResolvedAt)
	}

	f.setClock(evidenceTestNow.Add(2 * time.Minute))
	next = incidentUpdate(*resolved, SecurityIncidentStatusInvestigating)
	reopened, err := f.svc.UpdateSecurityIncident(created.ID, 12, next)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Status != SecurityIncidentStatusInvestigating || reopened.ResolvedAt != nil || reopened.ClosedAt != nil {
		t.Fatalf("reopen did not clear resolvedAt: %+v", reopened)
	}

	f.setClock(evidenceTestNow.Add(3 * time.Minute))
	next = incidentUpdate(*reopened, SecurityIncidentStatusResolved)
	resolved, err = f.svc.UpdateSecurityIncident(created.ID, 12, next)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ResolvedAt == nil || !resolved.ResolvedAt.Equal(evidenceTestNow.Add(3*time.Minute)) {
		t.Fatalf("second resolution = %v", resolved.ResolvedAt)
	}

	closedReq := incidentUpdate(*resolved, SecurityIncidentStatusClosed)
	requireServiceError(t, errCall(f.svc.UpdateSecurityIncident(created.ID, 12, closedReq)), http.StatusForbidden)

	closed, err := f.svc.CloseSecurityIncident(created.ID, 13)
	if err != nil {
		t.Fatal(err)
	}
	if closed.Status != SecurityIncidentStatusClosed || closed.ClosedAt == nil ||
		!closed.ClosedAt.Equal(evidenceTestNow.Add(3*time.Minute)) ||
		closed.ResolvedAt == nil || !closed.ResolvedAt.Equal(evidenceTestNow.Add(3*time.Minute)) {
		t.Fatalf("close: status=%s resolved=%v closed=%v", closed.Status, closed.ResolvedAt, closed.ClosedAt)
	}
	requireServiceError(t, errCall(f.svc.CloseSecurityIncident(created.ID, 13)), http.StatusConflict)
	requireServiceError(t, errCall(f.svc.UpdateSecurityIncident(created.ID, 12, incidentUpdate(*closed, SecurityIncidentStatusOpen))), http.StatusConflict)

	// Relations: change, clear, unknown, and historical inactive values.
	open, err := f.svc.CreateSecurityIncident(11, SecurityIncidentRequest{
		Reference: "INC-REL", Title: "Relations", Severity: SecurityIncidentSeverityLow,
		DetectedAt: detected, RiskID: &f.risk.ID, AssetID: &asset.ID,
		OwningServiceID: &f.service.ID, OwnerProfileID: &f.profile.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	changed := incidentUpdate(*open, SecurityIncidentStatusOpen)
	changed.RiskID = &f.otherRisk.ID
	changed.AssetID = &other.ID
	changed.Title = "Relations modifiées"
	updated, err := f.svc.UpdateSecurityIncident(open.ID, 12, changed)
	if err != nil || updated.RiskID == nil || *updated.RiskID != f.otherRisk.ID || updated.AssetID == nil || *updated.AssetID != other.ID {
		t.Fatalf("relation change: %v %+v", err, updated)
	}
	changed = incidentUpdate(*updated, SecurityIncidentStatusOpen)
	changed.RiskID, changed.AssetID = nil, nil
	changed.OwningServiceID, changed.OwnerProfileID = nil, nil
	cleared, err := f.svc.UpdateSecurityIncident(open.ID, 12, changed)
	if err != nil || cleared.RiskID != nil || cleared.AssetID != nil || cleared.OwningServiceID != nil || cleared.OwnerProfileID != nil {
		t.Fatalf("relation removal: %v %+v", err, cleared)
	}

	missing := uint(999999)
	unknown := incidentUpdate(*cleared, SecurityIncidentStatusOpen)
	unknown.RiskID = &missing
	requireServiceError(t, errCall(f.svc.UpdateSecurityIncident(open.ID, 12, unknown)), http.StatusNotFound)
	unknown.RiskID = nil
	unknown.AssetID = &missing
	requireServiceError(t, errCall(f.svc.UpdateSecurityIncident(open.ID, 12, unknown)), http.StatusNotFound)
	unknown.AssetID = nil
	unknown.OwningServiceID = &missing
	requireServiceError(t, errCall(f.svc.UpdateSecurityIncident(open.ID, 12, unknown)), http.StatusNotFound)
	unknown.OwningServiceID = nil
	unknown.OwnerProfileID = &missing
	requireServiceError(t, errCall(f.svc.UpdateSecurityIncident(open.ID, 12, unknown)), http.StatusNotFound)

	assigned := incidentUpdate(*cleared, SecurityIncidentStatusOpen)
	assigned.AssetID = &asset.ID
	assigned.OwningServiceID = &f.service.ID
	assigned.OwnerProfileID = &f.profile.ID
	if _, err = f.svc.UpdateSecurityIncident(open.ID, 12, assigned); err != nil {
		t.Fatal(err)
	}
	if err = f.db.Model(&Asset{}).Where("id = ?", asset.ID).Update("status", AssetStatusRetired).Error; err != nil {
		t.Fatal(err)
	}
	if err = f.db.Model(&f.service).Update("active", false).Error; err != nil {
		t.Fatal(err)
	}
	if err = f.db.Model(&f.profile).Update("active", false).Error; err != nil {
		t.Fatal(err)
	}
	historical := incidentUpdate(*cleared, SecurityIncidentStatusOpen)
	historical.Title = "Toujours modifiable"
	historical.AssetID = &asset.ID
	historical.OwningServiceID = &f.service.ID
	historical.OwnerProfileID = &f.profile.ID
	if _, err = f.svc.UpdateSecurityIncident(open.ID, 12, historical); err != nil {
		t.Fatalf("historical inactive relations must stay editable: %v", err)
	}
	historical.AssetID = &other.ID
	if err = f.db.Model(&Asset{}).Where("id = ?", other.ID).Update("status", AssetStatusRetired).Error; err != nil {
		t.Fatal(err)
	}
	requireServiceError(t, errCall(f.svc.UpdateSecurityIncident(open.ID, 12, historical)), http.StatusConflict)

	fresh := incidentUpdate(*cleared, SecurityIncidentStatusOpen)
	fresh.OwningServiceID = &f.inactiveService.ID
	requireServiceError(t, errCall(f.svc.UpdateSecurityIncident(open.ID, 12, fresh)), http.StatusConflict)
	fresh.OwningServiceID = nil
	fresh.OwnerProfileID = &f.inactiveProfile.ID
	requireServiceError(t, errCall(f.svc.UpdateSecurityIncident(open.ID, 12, fresh)), http.StatusConflict)

	_, err = f.svc.FindSecurityIncident(open.ID)
	if err != nil {
		t.Fatal(err)
	}
	requireServiceError(t, errCall(f.svc.FindSecurityIncident(missing)), http.StatusNotFound)
	requireServiceError(t, errCall(f.svc.CloseSecurityIncident(open.ID, 13)), http.StatusConflict)
}

func TestSecurityIncidentListFilters(t *testing.T) {
	f := newGRC01DServiceFixture(t)
	detected := evidenceTestNow.Add(-2 * time.Hour)
	asset := createComplianceAsset(t, f.svc, f.assetFixture, "AST-LIST", AssetStatusActive)

	mk := func(reference, title, severity, description string, at time.Time, riskID, assetID, serviceID, ownerID *uint) SecurityIncident {
		t.Helper()
		item, err := f.svc.CreateSecurityIncident(11, SecurityIncidentRequest{
			Reference: reference, Title: title, Description: description, Severity: severity,
			DetectedAt: at, RiskID: riskID, AssetID: assetID, OwningServiceID: serviceID, OwnerProfileID: ownerID,
		})
		if err != nil {
			t.Fatal(err)
		}
		return *item
	}

	alpha := mk("INC-ALPHA", "Alpha visible", SecurityIncidentSeverityLow, "secret-uniquement-dans-la-description", detected, &f.risk.ID, &asset.ID, &f.service.ID, &f.profile.ID)
	beta := mk("INC-BETA", "Alpha aussi", SecurityIncidentSeverityCritical, "autre", detected.Add(-time.Hour), &f.otherRisk.ID, nil, nil, nil)
	investigating := incidentUpdate(beta, SecurityIncidentStatusInvestigating)
	if _, err := f.svc.UpdateSecurityIncident(beta.ID, 12, investigating); err != nil {
		t.Fatal(err)
	}

	refs := func(filter SecurityIncidentFilter) []string {
		t.Helper()
		page, err := f.svc.ListSecurityIncidents(filter)
		if err != nil {
			t.Fatal(err)
		}
		out := make([]string, 0, len(page.Items))
		for _, item := range page.Items {
			out = append(out, item.Reference)
		}
		return out
	}

	if got := refs(SecurityIncidentFilter{Search: "alpha"}); len(got) != 2 {
		t.Fatalf("search = %v", got)
	}
	if got := refs(SecurityIncidentFilter{Search: "secret-uniquement"}); len(got) != 0 {
		t.Fatalf("description must not be searched: %v", got)
	}
	if got := refs(SecurityIncidentFilter{Search: "alpha", Status: SecurityIncidentStatusOpen}); len(got) != 1 || got[0] != alpha.Reference {
		t.Fatalf("search+status parentheses: %v", got)
	}
	if got := refs(SecurityIncidentFilter{Search: "alpha", Severity: SecurityIncidentSeverityCritical}); len(got) != 1 || got[0] != "INC-BETA" {
		t.Fatalf("search+severity: %v", got)
	}
	if got := refs(SecurityIncidentFilter{Search: "alpha", RiskID: &f.risk.ID}); len(got) != 1 || got[0] != alpha.Reference {
		t.Fatalf("search+risk: %v", got)
	}
	if got := refs(SecurityIncidentFilter{AssetID: &asset.ID}); len(got) != 1 || got[0] != alpha.Reference {
		t.Fatalf("asset: %v", got)
	}
	if got := refs(SecurityIncidentFilter{OwningServiceID: &f.service.ID, OwnerProfileID: &f.profile.ID}); len(got) != 1 {
		t.Fatalf("service+owner: %v", got)
	}

	page, err := f.svc.ListSecurityIncidents(SecurityIncidentFilter{Page: 2, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.Items) != 1 || page.Items[0].Reference != "INC-BETA" || page.Page != 2 {
		t.Fatalf("page = %+v items=%v", page, refs(SecurityIncidentFilter{Page: 2, Limit: 1}))
	}
	empty, err := f.svc.ListSecurityIncidents(SecurityIncidentFilter{Search: "absent"})
	if err != nil || empty.Items == nil || len(empty.Items) != 0 {
		t.Fatalf("empty page must be an empty slice: %+v", empty)
	}
	requireServiceError(t, errCall(f.svc.ListSecurityIncidents(SecurityIncidentFilter{Status: "REOPENED"})), http.StatusBadRequest)
	requireServiceError(t, errCall(f.svc.ListSecurityIncidents(SecurityIncidentFilter{Severity: "URGENT"})), http.StatusBadRequest)
}

func incidentUpdate(item SecurityIncident, status string) SecurityIncidentRequest {
	return SecurityIncidentRequest{
		Reference:       item.Reference,
		Title:           item.Title,
		Description:     item.Description,
		Severity:        item.Severity,
		Status:          status,
		DetectedAt:      item.DetectedAt,
		OccurredAt:      item.OccurredAt,
		RiskID:          item.RiskID,
		AssetID:         item.AssetID,
		OwningServiceID: item.OwningServiceID,
		OwnerProfileID:  item.OwnerProfileID,
	}
}

func errCall[T any](_ T, err error) error {
	return err
}
