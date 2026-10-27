package grc

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/staff"
)

// TestPostgresGRC04Risks runs the risk register on PostgreSQL: server-side
// scores and derived levels, CHECK/UNIQUE/FK/RESTRICT constraints, search,
// level filters and pagination, and the read-only relations (controls via
// grc_control_risks, remediation actions and findings via risk_id).
func TestPostgresGRC04Risks(t *testing.T) {
	f := newPGAssetFixture(t)
	db := f.db
	if err := db.AutoMigrate(dashboardModels()...); err != nil {
		t.Fatal(err)
	}
	svc := NewService(db)
	svc.now = func() time.Time { return pgNow }
	asset, err := svc.SaveAsset(0, 1, validAssetRequest(f))
	if err != nil {
		t.Fatal(err)
	}

	// Persist: scores computed by the server, levels derived, never stored.
	request := validRiskRequest(f, asset.ID)
	created, err := svc.SaveRisk(0, 41, request)
	if err != nil {
		t.Fatal(err)
	}
	var stored Risk
	if err := db.First(&stored, created.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.InherentScore != 20 || stored.ResidualScore != 6 || stored.CreatedBy != 41 ||
		created.InherentLevel != RiskLevelCritical || created.ResidualLevel != RiskLevelMedium {
		t.Fatalf("unexpected stored risk: %+v / %+v", stored, created)
	}
	var levelColumns int64
	if err := db.Raw(`SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'grc_risks' AND column_name LIKE '%level%'`).
		Scan(&levelColumns).Error; err != nil || levelColumns != 0 {
		t.Fatalf("risk levels must not be persisted: %v columns=%d", err, levelColumns)
	}

	// Update recomputes and persists the residual score.
	request.ResidualLikelihood, request.ResidualImpact, request.Status = 5, 5, "in_treatment"
	updated, err := svc.SaveRisk(created.ID, 42, request)
	if err != nil || updated.ResidualScore != 25 || updated.ResidualLevel != RiskLevelCritical || updated.CreatedBy != 41 || updated.UpdatedBy != 42 {
		t.Fatalf("update: %v %+v", err, updated)
	}
	if err := db.First(&stored, created.ID).Error; err != nil || stored.ResidualScore != 25 {
		t.Fatalf("residual score not persisted: %v %+v", err, stored)
	}

	// Duplicate reference and column lengths.
	_, err = svc.SaveRisk(0, 41, validRiskRequest(f, asset.ID))
	requireServiceError(t, err, http.StatusConflict)
	long := validRiskRequest(f, asset.ID)
	long.Reference = strings.Repeat("É", riskReferenceMaxLength)
	long.Title = strings.Repeat("é", riskTitleMaxLength)
	if _, err := svc.SaveRisk(0, 41, long); err != nil {
		t.Fatalf("maximum lengths rejected by PostgreSQL: %v", err)
	}

	// CHECK constraints.
	direct := stored
	direct.ID, direct.Reference = 0, "PG-DIRECT"
	bad := direct
	bad.Likelihood = 6
	requirePostgresError(t, db.Create(&bad).Error, pgCheckViolation, "grc_risk_likelihood_valid")
	bad = direct
	bad.ResidualScore = 26
	requirePostgresError(t, db.Create(&bad).Error, pgCheckViolation, "grc_risk_residual_score_valid")
	bad = direct
	bad.Treatment = "IGNORE"
	requirePostgresError(t, db.Create(&bad).Error, pgCheckViolation, "grc_risk_treatment_valid")
	bad = direct
	bad.Status = "ARCHIVED"
	requirePostgresError(t, db.Create(&bad).Error, pgCheckViolation, "grc_risk_status_valid")

	// Foreign keys.
	missing := uint(999999)
	for _, mutate := range []func(*Risk){
		func(r *Risk) { r.AssetID = &missing },
		func(r *Risk) { r.OwnerProfileID = &missing },
		func(r *Risk) { r.ApproverProfileID = &missing },
		func(r *Risk) { r.OwningServiceID = &missing },
	} {
		bad = direct
		mutate(&bad)
		requirePostgresError(t, db.Create(&bad).Error, pgForeignKeyViolation, "")
	}

	// Relations read by risk: control, remediation action, finding.
	control, err := svc.SaveControl(0, 1, validControlRequest(f))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.LinkControlRisk(1, ControlRiskRequest{ControlID: control.ID, RiskID: created.ID}); err != nil {
		t.Fatal(err)
	}
	controls, err := svc.ListControls(ControlFilter{RiskID: &created.ID})
	if err != nil || controls.Total != 1 || controls.Items[0].ID != control.ID {
		t.Fatalf("controls of the risk: %v %+v", err, controls)
	}
	if _, err := svc.SaveRemediationAction(0, 1, RemediationActionRequest{
		Reference: "ACT-PG-04", Title: "Action", RiskID: &created.ID,
		Priority: RemediationActionPriorityHigh, Status: RemediationActionStatusOpen,
	}); err != nil {
		t.Fatal(err)
	}
	actions, err := svc.ListRemediationActions(RemediationActionFilter{RiskID: &created.ID})
	if err != nil || actions.Total != 1 {
		t.Fatalf("actions of the risk: %v %+v", err, actions)
	}
	audit, err := svc.SaveAuditEngagement(0, 1, AuditEngagementRequest{Reference: "AUD-PG-04", Title: "Audit", Type: "INTERNAL", Status: "IN_PROGRESS"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveFinding(0, 1, FindingRequest{
		Reference: "FND-PG-04", Title: "Constat", AuditEngagementID: &audit.ID,
		Category: "OBSERVATION", Severity: "LOW", Status: "OPEN", RiskID: &created.ID,
	}); err != nil {
		t.Fatal(err)
	}
	findings, err := svc.ListFindings(FindingFilter{RiskID: &created.ID})
	if err != nil || findings.Total != 1 {
		t.Fatalf("findings of the risk: %v %+v", err, findings)
	}

	// RESTRICT: the risk and its asset and owner cannot disappear.
	requirePostgresError(t, db.Delete(&Risk{}, created.ID).Error, pgForeignKeyViolation, "")
	requirePostgresError(t, db.Delete(&Asset{}, asset.ID).Error, pgForeignKeyViolation, "")
	requirePostgresError(t, db.Delete(&staff.Profile{}, f.profile.ID).Error, pgForeignKeyViolation, "")
	requireRowExists(t, db, &Risk{}, created.ID)

	// A risk stays editable after its asset was retired.
	retire := validAssetRequest(f)
	retire.Status = AssetStatusRetired
	if _, err := svc.SaveAsset(asset.ID, 1, retire); err != nil {
		t.Fatal(err)
	}
	request.Status = "accepted"
	if _, err := svc.SaveRisk(created.ID, 43, request); err != nil {
		t.Fatalf("update after the asset was retired: %v", err)
	}

	// Search, level filters and pagination.
	page, err := svc.ListRisks(RiskFilter{Search: "INDISPONIBILITÉ", ResidualLevel: "CRITICAL", InherentLevel: "critical"})
	if err != nil || page.Total != 1 || page.Items[0].ID != created.ID {
		t.Fatalf("search + levels: %v %+v", err, page)
	}
	page, err = svc.ListRisks(RiskFilter{ResidualLevel: "MEDIUM", Status: "OPEN"})
	if err != nil || page.Total != 1 || page.Items[0].Reference != strings.Repeat("É", riskReferenceMaxLength) {
		t.Fatalf("medium residual open risks: %v %+v", err, page)
	}
	// Two pages of one item: disjoint and complete, whatever the collation.
	first, err := svc.ListRisks(RiskFilter{Page: 1, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.ListRisks(RiskFilter{Page: 2, Limit: 1})
	if err != nil || second.Total != 2 || second.TotalPages != 2 || len(first.Items) != 1 || len(second.Items) != 1 ||
		first.Items[0].ID == second.Items[0].ID {
		t.Fatalf("pagination: %v %+v %+v", err, first, second)
	}
}
