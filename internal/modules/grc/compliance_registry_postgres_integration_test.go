package grc

import (
	"net/http"
	"testing"
)

// TestPostgresGRC05Compliance runs compliance exploration on PostgreSQL:
// persistence, UNIQUE/CHECK/FK/RESTRICT on frameworks, requirements and
// requirement-control links, mapped/unmapped and control filters, pagination,
// and the single control mapping coverage (per framework, overall, empty
// denominator) compared with the dashboard.
func TestPostgresGRC05Compliance(t *testing.T) {
	f := newPGAssetFixture(t)
	db := f.db
	if err := db.AutoMigrate(dashboardModels()...); err != nil {
		t.Fatal(err)
	}
	svc := NewService(db)

	// Empty schema: 0/0 coverage is 0, never NaN.
	empty, err := svc.ComplianceCoverageReport(nil)
	if err != nil {
		t.Fatal(err)
	}
	requireCoverage(t, "empty", empty.Overall, 0, 0, 0, 0)

	reg := seedComplianceRegistry(t, f, svc)

	// Persistence and read.
	framework, err := svc.FindFramework(reg.iso.ID)
	if err != nil || framework.Code != "ISO-27001" || framework.Status != FrameworkStatusActive {
		t.Fatalf("framework: %v %+v", err, framework)
	}
	requirement, err := svc.FindRequirement(reg.r1.ID)
	if err != nil || requirement.Framework == nil || requirement.Framework.ID != reg.iso.ID || requirement.MappedControls != 2 {
		t.Fatalf("requirement: %v %+v", err, requirement)
	}

	// UNIQUE, through the services and directly.
	duplicate := validFrameworkRequest()
	duplicate.Code, duplicate.Version = reg.iso.Code, reg.iso.Version
	_, err = svc.SaveFramework(0, 1, duplicate)
	requireServiceError(t, err, http.StatusConflict)
	_, err = svc.SaveRequirement(0, 1, RequirementRequest{FrameworkID: reg.iso.ID, Code: "A.5.1", Title: "Doublon", Status: "ACTIVE"})
	requireServiceError(t, err, http.StatusConflict)
	_, err = svc.LinkRequirementControl(1, RequirementControlRequest{RequirementID: reg.r1.ID, ControlID: reg.c1.ID})
	requireServiceError(t, err, http.StatusConflict)
	requirePostgresError(t, db.Create(&RequirementControl{RequirementID: reg.r1.ID, ControlID: reg.c1.ID, CreatedBy: 1}).Error,
		pgUniqueViolation, "idx_grc_requirement_controls_requirement_control")

	// CHECK and FK.
	requirePostgresError(t, db.Create(&Framework{Code: "BAD", Name: "Bad", Version: "1", Status: "PUBLISHED", CreatedBy: 1, UpdatedBy: 1}).Error,
		pgCheckViolation, "grc_framework_status_valid")
	requirePostgresError(t, db.Create(&Requirement{FrameworkID: reg.iso.ID, Code: "BAD", Title: "Bad", Status: "DRAFT", CreatedBy: 1, UpdatedBy: 1}).Error,
		pgCheckViolation, "grc_requirement_status_valid")
	requirePostgresError(t, db.Create(&Requirement{FrameworkID: 999999, Code: "FK", Title: "FK", Status: "ACTIVE", CreatedBy: 1, UpdatedBy: 1}).Error,
		pgForeignKeyViolation, "")
	requirePostgresError(t, db.Create(&RequirementControl{RequirementID: reg.r2.ID, ControlID: 999999, CreatedBy: 1}).Error,
		pgForeignKeyViolation, "")

	// RESTRICT: mapped requirements, mapped controls and used frameworks stay.
	requirePostgresError(t, db.Delete(&Framework{}, reg.iso.ID).Error, pgForeignKeyViolation, "")
	requirePostgresError(t, db.Delete(&Requirement{}, reg.r1.ID).Error, pgForeignKeyViolation, "")
	requirePostgresError(t, db.Delete(&Control{}, reg.c1.ID).Error, pgForeignKeyViolation, "")
	requireRowExists(t, db, &Requirement{}, reg.r1.ID)

	// Exploration filters and pagination.
	yes, no := true, false
	page, err := svc.ListRequirements(RequirementFilter{FrameworkID: &reg.iso.ID, Mapped: &no})
	if err != nil || page.Total != 1 || page.Items[0].ID != reg.r2.ID {
		t.Fatalf("unmapped: %v %+v", err, page)
	}
	page, err = svc.ListRequirements(RequirementFilter{Mapped: &yes, ControlID: &reg.c1.ID, Search: "a.", Page: 2, Limit: 1})
	if err != nil || page.Total != 2 || page.TotalPages != 2 || len(page.Items) != 1 || page.Items[0].ID != reg.r3.ID {
		t.Fatalf("mapped to C1, page 2: %v %+v", err, page)
	}
	controls, err := svc.ListControls(ControlFilter{RequirementID: &reg.r1.ID})
	if err != nil || controls.Total != 2 {
		t.Fatalf("controls of R1: %v %+v", err, controls)
	}

	// Coverage: per framework, empty denominator, overall = dashboard.
	report, err := svc.ComplianceCoverageReport([]uint{reg.iso.ID, reg.hds.ID, reg.old.ID})
	if err != nil {
		t.Fatal(err)
	}
	requireCoverage(t, "overall", report.Overall, 5, 3, 2, 66.67)
	requireCoverage(t, "ISO", report.Frameworks[0], 4, 3, 2, 66.67)
	requireCoverage(t, "HDS (draft)", report.Frameworks[1], 1, 0, 0, 0)
	requireCoverage(t, "OLD", report.Frameworks[2], 0, 0, 0, 0)
	dashboard, err := svc.Dashboard(defaultDashboardFilter())
	if err != nil {
		t.Fatal(err)
	}
	if dashboard.Compliance.ControlMappingCoverage != report.Overall.ControlMappingCoverage ||
		dashboard.Compliance.MappedRequirements != report.Overall.MappedRequirements ||
		dashboard.Compliance.ApplicableRequirements != report.Overall.ApplicableRequirements {
		t.Fatalf("dashboard %+v diverges from %+v", dashboard.Compliance, report.Overall)
	}

	// Unlink, then the framework coverage follows.
	if err := svc.UnlinkRequirementControl(1, reg.r4.ID, reg.c2.ID); err != nil {
		t.Fatal(err)
	}
	report, err = svc.ComplianceCoverageReport([]uint{reg.iso.ID})
	if err != nil {
		t.Fatal(err)
	}
	requireCoverage(t, "ISO after unlink", report.Frameworks[0], 4, 3, 1, 33.33)
}
