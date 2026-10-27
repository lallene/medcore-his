package grc

import (
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/auth"
	"github.com/lallene/medcore-his/backend/internal/modules/organization"
	"github.com/lallene/medcore-his/backend/internal/modules/staff"
)

func TestPostgresGRC10Incidents(t *testing.T) {
	f := newPGEvidenceActionFixture(t)
	if err := f.db.AutoMigrate(&SecurityIncident{}); err != nil {
		t.Fatal(err)
	}
	db := f.db
	svc := f.svc

	asset, err := svc.SaveAsset(0, 1, AssetRequest{
		Code: "AST-INC-PG", Name: "Actif incident", Type: "APPLICATION", Criticality: "HIGH", Status: "ACTIVE",
	})
	if err != nil {
		t.Fatal(err)
	}
	detected := pgNow.Add(-2 * time.Hour)
	created, err := svc.CreateSecurityIncident(4, SecurityIncidentRequest{
		Reference: " inc-pg ", Title: "Incident PG", Description: "hors snapshot",
		Severity: "high", DetectedAt: detected, OccurredAt: timePtr(detected.Add(-time.Minute)),
		RiskID: &f.risk.ID, AssetID: &asset.ID, OwningServiceID: &f.service.ID, OwnerProfileID: &f.profile.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != SecurityIncidentStatusOpen || created.Reference != "INC-PG" || created.ResolvedAt != nil || created.ClosedAt != nil {
		t.Fatalf("insert: %+v", created)
	}

	updated, err := svc.UpdateSecurityIncident(created.ID, 5, SecurityIncidentRequest{
		Reference: created.Reference, Title: "Incident PG modifié", Description: created.Description,
		Severity: SecurityIncidentSeverityCritical, Status: SecurityIncidentStatusInvestigating,
		DetectedAt: created.DetectedAt, OccurredAt: created.OccurredAt,
		RiskID: created.RiskID, AssetID: created.AssetID, OwningServiceID: created.OwningServiceID, OwnerProfileID: created.OwnerProfileID,
	})
	if err != nil || updated.Title != "Incident PG modifié" || updated.Status != SecurityIncidentStatusInvestigating {
		t.Fatalf("update: %v %+v", err, updated)
	}

	svc.now = func() time.Time { return pgNow }
	resolved, err := svc.UpdateSecurityIncident(created.ID, 5, SecurityIncidentRequest{
		Reference: updated.Reference, Title: updated.Title, Severity: updated.Severity,
		Status: SecurityIncidentStatusResolved, DetectedAt: updated.DetectedAt, OccurredAt: updated.OccurredAt,
		RiskID: updated.RiskID, AssetID: updated.AssetID, OwningServiceID: updated.OwningServiceID, OwnerProfileID: updated.OwnerProfileID,
	})
	if err != nil || resolved.ResolvedAt == nil || !resolved.ResolvedAt.Equal(pgNow) || resolved.ClosedAt != nil {
		t.Fatalf("resolve: %v %+v", err, resolved)
	}
	reopened, err := svc.UpdateSecurityIncident(created.ID, 5, SecurityIncidentRequest{
		Reference: resolved.Reference, Title: resolved.Title, Severity: resolved.Severity,
		Status: SecurityIncidentStatusInvestigating, DetectedAt: resolved.DetectedAt,
		RiskID: resolved.RiskID, AssetID: resolved.AssetID, OwningServiceID: resolved.OwningServiceID, OwnerProfileID: resolved.OwnerProfileID,
	})
	if err != nil || reopened.Status != SecurityIncidentStatusInvestigating || reopened.ResolvedAt != nil || reopened.ClosedAt != nil {
		t.Fatalf("reopen: %v %+v", err, reopened)
	}
	svc.now = func() time.Time { return pgNow.Add(time.Minute) }
	resolved, err = svc.UpdateSecurityIncident(created.ID, 5, SecurityIncidentRequest{
		Reference: reopened.Reference, Title: reopened.Title, Severity: reopened.Severity,
		Status: SecurityIncidentStatusResolved, DetectedAt: reopened.DetectedAt,
		RiskID: reopened.RiskID, AssetID: reopened.AssetID, OwningServiceID: reopened.OwningServiceID, OwnerProfileID: reopened.OwnerProfileID,
	})
	if err != nil || resolved.ResolvedAt == nil || !resolved.ResolvedAt.Equal(pgNow.Add(time.Minute)) {
		t.Fatalf("second resolve: %v %+v", err, resolved)
	}
	closed, err := svc.CloseSecurityIncident(created.ID, 6)
	if err != nil || closed.Status != SecurityIncidentStatusClosed || closed.ClosedAt == nil ||
		!closed.ClosedAt.Equal(pgNow.Add(time.Minute)) || closed.ResolvedAt == nil || !closed.ResolvedAt.Equal(pgNow.Add(time.Minute)) {
		t.Fatalf("close: %v %+v", err, closed)
	}

	other, err := svc.CreateSecurityIncident(4, SecurityIncidentRequest{
		Reference: "INC-PG-OTHER", Title: "Autre alpha", Severity: SecurityIncidentSeverityLow,
		DetectedAt: detected.Add(-time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	page, err := svc.ListSecurityIncidents(SecurityIncidentFilter{Search: "alpha", Status: SecurityIncidentStatusOpen, Page: 1, Limit: 1})
	if err != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != other.ID {
		t.Fatalf("search+status page: %v %+v", err, page)
	}
	byRisk, err := svc.ListSecurityIncidents(SecurityIncidentFilter{RiskID: &f.risk.ID, Severity: SecurityIncidentSeverityCritical})
	if err != nil || byRisk.Total != 1 || byRisk.Items[0].ID != created.ID {
		t.Fatalf("risk+severity: %v %+v", err, byRisk)
	}
	byAsset, err := svc.ListSecurityIncidents(SecurityIncidentFilter{AssetID: &asset.ID, OwningServiceID: &f.service.ID, OwnerProfileID: &f.profile.ID})
	if err != nil || byAsset.Total != 1 {
		t.Fatalf("asset+service+owner: %v %+v", err, byAsset)
	}

	var stored SecurityIncident
	if err = db.First(&stored, created.ID).Error; err != nil {
		t.Fatal(err)
	}
	duplicate := stored
	duplicate.ID = 0
	duplicate.Reference = stored.Reference
	requirePostgresError(t, db.Create(&duplicate).Error, pgUniqueViolation, "uidx_grc_security_incidents_reference")

	bad := stored
	bad.ID, bad.Reference = 0, "INC-PG-BAD-SEV"
	bad.Severity = "URGENT"
	requirePostgresError(t, db.Create(&bad).Error, pgCheckViolation, "grc_security_incident_severity_valid")
	bad = stored
	bad.ID, bad.Reference = 0, "INC-PG-BAD-STATUS"
	bad.Status = "REOPENED"
	requirePostgresError(t, db.Create(&bad).Error, pgCheckViolation, "grc_security_incident_status_valid")
	bad = stored
	bad.ID, bad.Reference = 0, "INC-PG-BAD-OCCURRED"
	bad.Status = SecurityIncidentStatusOpen
	bad.ResolvedAt, bad.ClosedAt = nil, nil
	later := stored.DetectedAt.Add(time.Minute)
	bad.OccurredAt = &later
	requirePostgresError(t, db.Create(&bad).Error, pgCheckViolation, "grc_security_incident_occurred")
	bad = stored
	bad.ID, bad.Reference = 0, "INC-PG-BAD-RESOLVED"
	bad.Status = SecurityIncidentStatusResolved
	bad.ResolvedAt, bad.ClosedAt = nil, nil
	requirePostgresError(t, db.Create(&bad).Error, pgCheckViolation, "grc_security_incident_timestamps")
	bad = stored
	bad.ID, bad.Reference = 0, "INC-PG-BAD-OPEN-STAMP"
	bad.Status = SecurityIncidentStatusOpen
	stamp := pgNow
	bad.ResolvedAt, bad.ClosedAt = &stamp, nil
	requirePostgresError(t, db.Create(&bad).Error, pgCheckViolation, "grc_security_incident_timestamps")
	bad = stored
	bad.ID, bad.Reference = 0, "INC-PG-BAD-CLOSED"
	bad.Status = SecurityIncidentStatusClosed
	bad.ClosedAt = nil
	requirePostgresError(t, db.Create(&bad).Error, pgCheckViolation, "grc_security_incident_timestamps")

	missing := uint(999999)
	for _, mutate := range []func(*SecurityIncident){
		func(item *SecurityIncident) { item.RiskID = &missing },
		func(item *SecurityIncident) { item.AssetID = &missing },
		func(item *SecurityIncident) { item.OwningServiceID = &missing },
		func(item *SecurityIncident) { item.OwnerProfileID = &missing },
	} {
		row := stored
		row.ID = 0
		row.Reference = "INC-PG-FK"
		row.Status = SecurityIncidentStatusOpen
		row.ResolvedAt, row.ClosedAt = nil, nil
		mutate(&row)
		requirePostgresError(t, db.Create(&row).Error, pgForeignKeyViolation, "")
	}

	ownedRisk, err := svc.SaveRisk(0, 1, RiskRequest{
		Reference: "RISK-INC-PG", Title: "Risque incident", Likelihood: 2, Impact: 2,
		ResidualLikelihood: 1, ResidualImpact: 1, Treatment: "REDUCE", Status: "OPEN",
	})
	if err != nil {
		t.Fatal(err)
	}
	ownedAsset, err := svc.SaveAsset(0, 1, AssetRequest{
		Code: "AST-INC-PG-OWN", Name: "Actif dédié", Type: "SERVER", Criticality: "LOW", Status: "ACTIVE",
	})
	if err != nil {
		t.Fatal(err)
	}
	department := organization.Department{Code: "INC", Name: "Incidents", Active: true, CreatedBy: 1, UpdatedBy: 1}
	if err = db.Create(&department).Error; err != nil {
		t.Fatal(err)
	}
	ownedService := organization.Service{
		DepartmentID: department.ID, Code: "INC-SVC", Name: "Service incident",
		ServiceType: organization.TypeAdministrative, Active: true, CreatedBy: 1, UpdatedBy: 1,
	}
	if err = db.Create(&ownedService).Error; err != nil {
		t.Fatal(err)
	}
	user := auth.User{Name: "Incident owner", Email: "grc.incident.pg@test.local", PasswordHash: "x", Role: "staff", IsActive: true}
	if err = db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	ownedProfile := staff.Profile{UserID: user.ID, EmployeeCode: "INC-PG", Active: true}
	if err = db.Create(&ownedProfile).Error; err != nil {
		t.Fatal(err)
	}
	linked, err := svc.CreateSecurityIncident(4, SecurityIncidentRequest{
		Reference: "INC-PG-RESTRICT", Title: "Restrictions", Severity: SecurityIncidentSeverityMedium,
		DetectedAt: detected, RiskID: &ownedRisk.ID, AssetID: &ownedAsset.ID,
		OwningServiceID: &ownedService.ID, OwnerProfileID: &ownedProfile.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if linked.ID == 0 {
		t.Fatal("restricted incident was not stored")
	}
	// PostgreSQL reports ON DELETE RESTRICT as 23001 (restrict_violation).
	// Inserting an unknown foreign key remains 23503 (foreign_key_violation).
	const pgRestrictViolation = "23001"
	requirePostgresError(t, db.Delete(&Risk{}, ownedRisk.ID).Error, pgRestrictViolation, "fk_grc_security_incidents_risk")
	requirePostgresError(t, db.Delete(&Asset{}, ownedAsset.ID).Error, pgRestrictViolation, "fk_grc_security_incidents_asset")
	requirePostgresError(t, db.Delete(&organization.Service{}, ownedService.ID).Error, pgRestrictViolation, "fk_grc_security_incidents_owning_service")
	requirePostgresError(t, db.Delete(&staff.Profile{}, ownedProfile.ID).Error, pgRestrictViolation, "fk_grc_security_incidents_owner_profile")
}
