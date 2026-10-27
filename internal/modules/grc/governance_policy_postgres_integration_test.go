package grc

import (
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/auth"
	"github.com/lallene/medcore-his/backend/internal/modules/organization"
	"github.com/lallene/medcore-his/backend/internal/modules/staff"
)

func TestPostgresGRC11Governance(t *testing.T) {
	f := newPGEvidenceActionFixture(t)
	if err := f.db.AutoMigrate(&GovernancePolicy{}, &GovernancePolicyFramework{}, &GovernancePolicyControl{}); err != nil {
		t.Fatal(err)
	}
	db := f.db
	svc := f.svc
	svc.now = func() time.Time { return pgNow }

	dedicatedControl, err := svc.SaveControl(0, 1, ControlRequest{Code: "CTL-POL-PG", Name: "Contrôle politique", Status: "ACTIVE"})
	if err != nil {
		t.Fatal(err)
	}
	framework, err := svc.SaveFramework(0, 1, FrameworkRequest{Code: "FW-POL-PG", Name: "Référentiel", Version: "1", Status: "ACTIVE"})
	if err != nil {
		t.Fatal(err)
	}
	otherFramework, err := svc.SaveFramework(0, 1, FrameworkRequest{Code: "FW-POL-PG-2", Name: "Autre", Version: "1", Status: "ACTIVE"})
	if err != nil {
		t.Fatal(err)
	}
	department := organization.Department{Code: "POL", Name: "Politique", Active: true, CreatedBy: 1, UpdatedBy: 1}
	if err = db.Create(&department).Error; err != nil {
		t.Fatal(err)
	}
	service := organization.Service{
		DepartmentID: department.ID, Code: "POL", Name: "Gouvernance",
		ServiceType: organization.TypeAdministrative, Active: true, CreatedBy: 1, UpdatedBy: 1,
	}
	if err = db.Create(&service).Error; err != nil {
		t.Fatal(err)
	}
	user := auth.User{Name: "Policy", Email: "grc.pg.policy@test.local", PasswordHash: "x", Role: "staff", IsActive: true}
	if err = db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	profile := staff.Profile{UserID: user.ID, EmployeeCode: "GRC-PG-POL", Active: true}
	if err = db.Create(&profile).Error; err != nil {
		t.Fatal(err)
	}
	approverUser := auth.User{Name: "Approver", Email: "grc.pg.policy.approver@test.local", PasswordHash: "x", Role: "staff", IsActive: true}
	if err = db.Create(&approverUser).Error; err != nil {
		t.Fatal(err)
	}
	approver := staff.Profile{UserID: approverUser.ID, EmployeeCode: "GRC-PG-POL-APP", Active: true}
	if err = db.Create(&approver).Error; err != nil {
		t.Fatal(err)
	}

	from := pgNow.Add(-24 * time.Hour)
	created, err := svc.CreateGovernancePolicy(4, GovernancePolicyRequest{
		Reference: " pol-pg ", Title: "Politique PG", Description: "hors snapshot", Version: "1.0",
		EffectiveFrom: &from, OwningServiceID: &service.ID, OwnerProfileID: &profile.ID, ApproverProfileID: &approver.ID,
		FrameworkIDs: policyIDs(framework.ID), ControlIDs: policyIDs(dedicatedControl.ID),
	})
	if err != nil || created.Status != GovernancePolicyStatusDraft || created.Reference != "POL-PG" || created.ApprovedAt != nil {
		t.Fatalf("insert: %v %+v", err, created)
	}
	updated, err := svc.UpdateGovernancePolicy(created.ID, 5, GovernancePolicyRequest{
		Reference: "POL-PG", Title: "Politique PG modifiée", Version: "1.1", Status: GovernancePolicyStatusInReview,
		EffectiveFrom: &from, OwningServiceID: &service.ID, OwnerProfileID: &profile.ID, ApproverProfileID: &approver.ID,
		FrameworkIDs: policyIDs(otherFramework.ID), ControlIDs: policyIDs(dedicatedControl.ID),
	})
	if err != nil || updated.Title != "Politique PG modifiée" || updated.Frameworks[0].ID != otherFramework.ID {
		t.Fatalf("update: %v %+v", err, updated)
	}
	approved, err := svc.ApproveGovernancePolicy(created.ID, 6)
	if err != nil || approved.ApprovedAt == nil || !approved.ApprovedAt.Equal(pgNow) || approved.PublishedAt != nil {
		t.Fatalf("approve: %v %+v", err, approved)
	}
	published, err := svc.PublishGovernancePolicy(created.ID, 7)
	if err != nil || published.PublishedAt == nil || !published.PublishedAt.Equal(pgNow) || published.ApprovedAt == nil || published.RetiredAt != nil {
		t.Fatalf("publish: %v %+v", err, published)
	}
	retired, err := svc.RetireGovernancePolicy(created.ID, 8)
	if err != nil || retired.RetiredAt == nil || !retired.RetiredAt.Equal(pgNow) || retired.Status != GovernancePolicyStatusRetired {
		t.Fatalf("retire: %v %+v", err, retired)
	}

	other, err := svc.CreateGovernancePolicy(4, GovernancePolicyRequest{
		Reference: "POL-PG-OTHER", Title: "Autre alpha", Version: "2.0", FrameworkIDs: policyIDs(framework.ID),
	})
	if err != nil {
		t.Fatal(err)
	}
	page, err := svc.ListGovernancePolicies(GovernancePolicyFilter{Search: "alpha", Status: GovernancePolicyStatusDraft, Page: 1, Limit: 1})
	if err != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != other.ID {
		t.Fatalf("search+status: %v %+v", err, page)
	}
	byFramework, err := svc.ListGovernancePolicies(GovernancePolicyFilter{FrameworkID: &framework.ID, OwningServiceID: &service.ID})
	if err != nil || byFramework.Total != 0 {
		t.Fatalf("framework+service of the retired policy must not match the draft filter combination: %v %+v", err, byFramework)
	}
	byControl, err := svc.ListGovernancePolicies(GovernancePolicyFilter{ControlID: &dedicatedControl.ID, OwnerProfileID: &profile.ID, ApproverProfileID: &approver.ID})
	if err != nil || byControl.Total != 1 || byControl.Items[0].ID != created.ID {
		t.Fatalf("control+owner+approver: %v %+v", err, byControl)
	}

	var stored GovernancePolicy
	if err = db.First(&stored, created.ID).Error; err != nil {
		t.Fatal(err)
	}
	duplicate := stored
	duplicate.ID = 0
	requirePostgresError(t, db.Create(&duplicate).Error, pgUniqueViolation, "uidx_grc_governance_policies_reference")

	bad := stored
	bad.ID, bad.Reference = 0, "POL-PG-BAD-STATUS"
	bad.Status = "CANCELLED"
	requirePostgresError(t, db.Create(&bad).Error, pgCheckViolation, "grc_governance_policy_status_valid")

	bad = stored
	bad.ID, bad.Reference = 0, "POL-PG-BAD-EFFECTIVE"
	bad.EffectiveFrom = nil
	later := pgNow
	bad.EffectiveTo = &later
	requirePostgresError(t, db.Create(&bad).Error, pgCheckViolation, "grc_governance_policy_effective")

	bad = stored
	bad.ID, bad.Reference = 0, "POL-PG-BAD-STAMP"
	bad.Status = GovernancePolicyStatusDraft
	bad.ApprovedAt, bad.PublishedAt, bad.RetiredAt = nil, nil, nil
	bad.ApprovedAt = &pgNow
	requirePostgresError(t, db.Create(&bad).Error, pgCheckViolation, "grc_governance_policy_timestamps")

	missing := uint(999999)
	bad = stored
	bad.ID, bad.Reference = 0, "POL-PG-BAD-FK"
	bad.OwningServiceID = &missing
	requirePostgresError(t, db.Create(&bad).Error, pgForeignKeyViolation, "")

	requirePostgresError(t, db.Create(&GovernancePolicyFramework{PolicyID: other.ID, FrameworkID: framework.ID, CreatedBy: 1}).Error, pgUniqueViolation, "uidx_grc_governance_policy_frameworks")
	requirePostgresError(t, db.Create(&GovernancePolicyFramework{PolicyID: other.ID, FrameworkID: missing, CreatedBy: 1}).Error, pgForeignKeyViolation, "")
	requirePostgresError(t, db.Create(&GovernancePolicyControl{PolicyID: created.ID, ControlID: dedicatedControl.ID, CreatedBy: 1}).Error, pgUniqueViolation, "uidx_grc_governance_policy_controls")
	requirePostgresError(t, db.Create(&GovernancePolicyControl{PolicyID: missing, ControlID: dedicatedControl.ID, CreatedBy: 1}).Error, pgForeignKeyViolation, "")

	// PostgreSQL 18 reports ON DELETE RESTRICT as 23001. The helper accepts it
	// when the caller asks for 23503. Dedicated parents keep the blocking FK
	// unambiguous.
	requirePostgresError(t, db.Delete(&Framework{}, otherFramework.ID).Error, pgForeignKeyViolation, "fk_grc_governance_policy_frameworks_framework")
	requirePostgresError(t, db.Delete(&Control{}, dedicatedControl.ID).Error, pgForeignKeyViolation, "fk_grc_governance_policy_controls_control")
	requirePostgresError(t, db.Delete(&organization.Service{}, service.ID).Error, pgForeignKeyViolation, "fk_grc_governance_policies_owning_service")
	requirePostgresError(t, db.Delete(&staff.Profile{}, profile.ID).Error, pgForeignKeyViolation, "fk_grc_governance_policies_owner_profile")
	requirePostgresError(t, db.Delete(&staff.Profile{}, approver.ID).Error, pgForeignKeyViolation, "fk_grc_governance_policies_approver_profile")
	if err = db.Where("policy_id = ?", created.ID).Delete(&GovernancePolicyFramework{}).Error; err != nil {
		t.Fatal(err)
	}
	requirePostgresError(t, db.Delete(&GovernancePolicy{}, created.ID).Error, pgForeignKeyViolation, "fk_grc_governance_policy_controls_policy")
}
