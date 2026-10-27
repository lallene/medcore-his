package grc

import (
	"net/http"
	"testing"

	"github.com/lallene/medcore-his/backend/internal/modules/organization"
)

// TestPostgresGRC06Controls runs the control registry on PostgreSQL:
// control and implementation persistence/update, UNIQUE/CHECK/FK/RESTRICT,
// ownership validated on assignment only, relation counts and filters, and
// the requirement, risk, assessment and evidence relations of a control.
func TestPostgresGRC06Controls(t *testing.T) {
	f := newPGAssetFixture(t)
	db := f.db
	if err := db.AutoMigrate(dashboardModels()...); err != nil {
		t.Fatal(err)
	}
	svc := NewService(db)
	reg := seedComplianceRegistry(t, f, svc)
	asset, err := svc.SaveAsset(0, 1, validAssetRequest(f))
	if err != nil {
		t.Fatal(err)
	}
	otherReq := validAssetRequest(f)
	otherReq.Code = "AST-PG-06"
	other, err := svc.SaveAsset(0, 1, otherReq)
	if err != nil {
		t.Fatal(err)
	}

	// Control persistence and update.
	request := validControlRequest(f)
	request.Code, request.Name = "CTL-PG-06", "Contrôle PostgreSQL"
	control, err := svc.SaveControl(0, 31, request)
	if err != nil || control.Code != "CTL-PG-06" || control.CreatedBy != 31 {
		t.Fatalf("control: %v %+v", err, control)
	}
	request.Status = "inactive"
	updated, err := svc.SaveControl(control.ID, 32, request)
	if err != nil || updated.Status != ControlStatusInactive || updated.UpdatedBy != 32 || updated.CreatedBy != 31 {
		t.Fatalf("control update: %v %+v", err, updated)
	}
	request.Status = "active"
	if _, err := svc.SaveControl(control.ID, 32, request); err != nil {
		t.Fatal(err)
	}

	// Implementation persistence and update.
	impl, err := svc.SaveControlImplementation(0, 33, ControlImplementationRequest{
		ControlID: control.ID, AssetID: asset.ID, Status: "planned",
		OwnerProfileID: &f.profile.ID, OwningServiceID: &f.service.ID, ImplementationDetails: "Déploiement",
	})
	if err != nil || impl.Control == nil || impl.Asset == nil || impl.CreatedBy != 33 {
		t.Fatalf("implementation: %v %+v", err, impl)
	}

	// Ownership validated on assignment only, on PostgreSQL too.
	if err := db.Model(&organization.Service{}).Where("id = ?", f.service.ID).Update("active", false).Error; err != nil {
		t.Fatal(err)
	}
	implUpdate := ControlImplementationRequest{
		ControlID: control.ID, AssetID: asset.ID, Status: "implemented",
		OwnerProfileID: &f.profile.ID, OwningServiceID: &f.service.ID,
	}
	if updatedImpl, err := svc.SaveControlImplementation(impl.ID, 34, implUpdate); err != nil || updatedImpl.Status != ControlImplementationStatusImplemented {
		t.Fatalf("implementation update with an inactive service: %v %+v", err, updatedImpl)
	}
	if _, err := svc.SaveControl(control.ID, 34, request); err != nil {
		t.Fatalf("control update with an inactive service: %v", err)
	}
	implUpdate.OwnerProfileID = &f.inactiveProfile.ID
	_, err = svc.SaveControlImplementation(impl.ID, 34, implUpdate)
	requireServiceError(t, err, http.StatusConflict)
	if err := db.Model(&organization.Service{}).Where("id = ?", f.service.ID).Update("active", true).Error; err != nil {
		t.Fatal(err)
	}

	// UNIQUE.
	_, err = svc.SaveControl(0, 1, request)
	requireServiceError(t, err, http.StatusConflict)
	_, err = svc.SaveControlImplementation(0, 1, ControlImplementationRequest{ControlID: control.ID, AssetID: asset.ID, Status: "PLANNED"})
	requireServiceError(t, err, http.StatusConflict)
	requirePostgresError(t, db.Create(&ControlImplementation{ControlID: control.ID, AssetID: asset.ID, Status: "PLANNED", CreatedBy: 1, UpdatedBy: 1}).Error,
		pgUniqueViolation, "idx_grc_control_implementations_control_asset")

	// CHECK and FK.
	requirePostgresError(t, db.Create(&Control{Code: "PG-BAD", Name: "Bad", Status: "ARCHIVED", CreatedBy: 1, UpdatedBy: 1}).Error,
		pgCheckViolation, "grc_control_status_valid")
	requirePostgresError(t, db.Create(&ControlImplementation{ControlID: control.ID, AssetID: other.ID, Status: "DONE", CreatedBy: 1, UpdatedBy: 1}).Error,
		pgCheckViolation, "grc_control_implementation_status_valid")
	missing := uint(999999)
	requirePostgresError(t, db.Create(&Control{Code: "PG-FK", Name: "FK", Status: "ACTIVE", OwnerProfileID: &missing, CreatedBy: 1, UpdatedBy: 1}).Error,
		pgForeignKeyViolation, "")
	requirePostgresError(t, db.Create(&ControlImplementation{ControlID: control.ID, AssetID: missing, Status: "PLANNED", CreatedBy: 1, UpdatedBy: 1}).Error,
		pgForeignKeyViolation, "")

	// Requirement and risk relations.
	if _, err := svc.LinkRequirementControl(1, RequirementControlRequest{RequirementID: reg.r2.ID, ControlID: control.ID}); err != nil {
		t.Fatal(err)
	}
	risk, err := svc.SaveRisk(0, 1, validRiskRequest(f, asset.ID))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.LinkControlRisk(1, ControlRiskRequest{ControlID: control.ID, RiskID: risk.ID}); err != nil {
		t.Fatal(err)
	}
	_, err = svc.LinkControlRisk(1, ControlRiskRequest{ControlID: control.ID, RiskID: risk.ID})
	requireServiceError(t, err, http.StatusConflict)
	requirePostgresError(t, db.Create(&ControlRisk{ControlID: control.ID, RiskID: risk.ID, CreatedBy: 1}).Error,
		pgUniqueViolation, "idx_grc_control_risks_control_risk")

	found, err := svc.FindControl(control.ID)
	if err != nil || found.RequirementCount != 1 || found.RiskCount != 1 || found.ImplementationCount != 1 {
		t.Fatalf("relation counts: %v %+v", err, found)
	}
	risks, err := svc.ListRisks(RiskFilter{ControlID: &control.ID})
	if err != nil || risks.Total != 1 {
		t.Fatalf("risks of the control: %v %+v", err, risks)
	}
	requirements, err := svc.ListRequirements(RequirementFilter{ControlID: &control.ID})
	if err != nil || requirements.Total != 1 || requirements.Items[0].ID != reg.r2.ID {
		t.Fatalf("requirements of the control: %v %+v", err, requirements)
	}

	// Assessment and evidence through the implementation.
	evidence, err := svc.SaveEvidence(0, 1, EvidenceRequest{Reference: "EVD-PG-06", Title: "Preuve", ControlImplementationID: impl.ID, Type: EvidenceTypeReport, Status: EvidenceStatusDraft})
	if err != nil {
		t.Fatal(err)
	}
	assessment, err := svc.SaveAssessment(0, 1, AssessmentRequest{Reference: "ASM-PG-06", Title: "Évaluation", ControlImplementationID: impl.ID, Type: AssessmentTypeDesign, Status: AssessmentStatusDraft})
	if err != nil {
		t.Fatal(err)
	}
	evidences, err := svc.ListEvidence(EvidenceFilter{ControlImplementationID: &impl.ID})
	if err != nil || evidences.Total != 1 || evidences.Items[0].ID != evidence.ID {
		t.Fatalf("evidence of the implementation: %v %+v", err, evidences)
	}
	assessments, err := svc.ListAssessments(AssessmentFilter{ControlImplementationID: &impl.ID})
	if err != nil || assessments.Total != 1 || assessments.Items[0].ID != assessment.ID {
		t.Fatalf("assessments of the implementation: %v %+v", err, assessments)
	}

	// RESTRICT: nothing referenced can disappear.
	requirePostgresError(t, db.Delete(&ControlImplementation{}, impl.ID).Error, pgForeignKeyViolation, "")
	requirePostgresError(t, db.Delete(&Control{}, control.ID).Error, pgForeignKeyViolation, "")
	requirePostgresError(t, db.Delete(&Asset{}, asset.ID).Error, pgForeignKeyViolation, "")
	requirePostgresError(t, db.Delete(&Risk{}, risk.ID).Error, pgForeignKeyViolation, "")
	requireRowExists(t, db, &Control{}, control.ID)

	// Search, filters and pagination.
	page, err := svc.ListControls(ControlFilter{Search: "CTL-", Status: "ACTIVE", RiskID: &risk.ID})
	if err != nil || page.Total != 1 || page.Items[0].ID != control.ID {
		t.Fatalf("search + status + risk: %v %+v", err, page)
	}
	first, err := svc.ListControls(ControlFilter{Page: 1, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.ListControls(ControlFilter{Page: 2, Limit: 2})
	if err != nil || first.Total != 3 || second.TotalPages != 2 || len(second.Items) != 1 || first.Items[0].ID == second.Items[0].ID {
		t.Fatalf("pagination: %v %+v %+v", err, first, second)
	}
	impls, err := svc.ListControlImplementations(ControlImplementationFilter{ControlID: &control.ID, Status: "IMPLEMENTED", OwnerProfileID: &f.profile.ID})
	if err != nil || impls.Total != 1 {
		t.Fatalf("implementation filters: %v %+v", err, impls)
	}
}
