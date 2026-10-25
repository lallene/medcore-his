package grc

import (
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/auth"
	"github.com/lallene/medcore-his/backend/internal/modules/organization"
	"github.com/lallene/medcore-his/backend/internal/modules/staff"
	"gorm.io/gorm"
)

type evidenceFixture struct {
	complianceFixture
	implementation ControlImplementation
	profile        staff.Profile
	service        organization.Service
}

// newEvidenceFixture extends the compliance fixture (SQLite with foreign
// keys enabled) with GRC-01C tables, a control implementation, an owner
// profile and an owning service.
func newEvidenceFixture(t *testing.T) evidenceFixture {
	t.Helper()

	base := newComplianceFixture(t)
	db := base.db
	if err := db.AutoMigrate(&Evidence{}, &RemediationAction{}); err != nil {
		t.Fatal(err)
	}

	f := evidenceFixture{complianceFixture: base}

	f.implementation = ControlImplementation{
		ControlID: base.control.ID,
		AssetID:   base.asset.ID,
		Status:    ControlImplementationStatusImplemented,
		CreatedBy: 1,
		UpdatedBy: 1,
	}
	if err := db.Create(&f.implementation).Error; err != nil {
		t.Fatal(err)
	}

	department := organization.Department{Code: "GRC", Name: "GRC", Active: true, CreatedBy: 1, UpdatedBy: 1}
	if err := db.Create(&department).Error; err != nil {
		t.Fatal(err)
	}
	f.service = organization.Service{
		DepartmentID: department.ID,
		Code:         "SEC",
		Name:         "Sécurité",
		ServiceType:  organization.TypeAdministrative,
		Active:       true,
		CreatedBy:    1,
		UpdatedBy:    1,
	}
	if err := db.Create(&f.service).Error; err != nil {
		t.Fatal(err)
	}

	user := auth.User{Name: "Evidence Owner", Email: "grc.evidence@test.local", PasswordHash: "x", Role: "staff", IsActive: true}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	f.profile = staff.Profile{UserID: user.ID, EmployeeCode: "GRC-EVIDENCE", Active: true}
	if err := db.Create(&f.profile).Error; err != nil {
		t.Fatal(err)
	}

	return f
}

func validEvidence(f evidenceFixture, reference string) Evidence {
	return Evidence{
		Reference:               reference,
		Title:                   "Rapport de sauvegarde",
		ControlImplementationID: f.implementation.ID,
		Type:                    EvidenceTypeReport,
		Status:                  EvidenceStatusDraft,
		CreatedBy:               1,
		UpdatedBy:               1,
	}
}

func validRemediationAction(f evidenceFixture, reference string) RemediationAction {
	return RemediationAction{
		Reference:               reference,
		Title:                   "Activer le chiffrement des sauvegardes",
		ControlImplementationID: &f.implementation.ID,
		Priority:                RemediationActionPriorityHigh,
		Status:                  RemediationActionStatusOpen,
		CreatedBy:               1,
		UpdatedBy:               1,
	}
}

func requireRestrictConstraints(t *testing.T, db *gorm.DB, model any, relations ...string) {
	t.Helper()

	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(model); err != nil {
		t.Fatal(err)
	}
	for _, name := range relations {
		rel, ok := stmt.Schema.Relationships.Relations[name]
		if !ok {
			t.Fatalf("%s: relation %s missing", stmt.Schema.Name, name)
		}
		constraint := rel.ParseConstraint()
		if constraint == nil {
			t.Fatalf("%s.%s: no foreign key constraint", stmt.Schema.Name, name)
		}
		if constraint.OnDelete != "RESTRICT" || constraint.OnUpdate != "CASCADE" {
			t.Fatalf("%s.%s: OnUpdate=%q OnDelete=%q, want CASCADE/RESTRICT",
				stmt.Schema.Name, name, constraint.OnUpdate, constraint.OnDelete)
		}
	}
}

func TestEvidenceModelTableNames(t *testing.T) {
	if got := (Evidence{}).TableName(); got != "grc_evidence" {
		t.Fatalf("Evidence.TableName() = %q", got)
	}
	if got := (RemediationAction{}).TableName(); got != "grc_remediation_actions" {
		t.Fatalf("RemediationAction.TableName() = %q", got)
	}
}

func TestEvidenceModelCreateAndUniqueReference(t *testing.T) {
	f := newEvidenceFixture(t)

	evidence := validEvidence(f, "EVD-1")
	evidence.OwnerProfileID = &f.profile.ID
	evidence.OwningServiceID = &f.service.ID
	if err := f.db.Create(&evidence).Error; err != nil {
		t.Fatal(err)
	}

	duplicate := validEvidence(f, "EVD-1")
	if err := f.db.Create(&duplicate).Error; err == nil {
		t.Fatal("evidence reference uniqueness not enforced")
	}
}

func TestEvidenceModelTypeAndStatusChecks(t *testing.T) {
	f := newEvidenceFixture(t)

	for i, value := range []string{
		EvidenceTypeDocument,
		EvidenceTypeConfiguration,
		EvidenceTypeLog,
		EvidenceTypeScreenshot,
		EvidenceTypeReport,
		EvidenceTypeTestResult,
		EvidenceTypeCertificate,
		EvidenceTypeOther,
	} {
		evidence := validEvidence(f, "EVD-TYPE-"+string(rune('A'+i)))
		evidence.Type = value
		if err := f.db.Create(&evidence).Error; err != nil {
			t.Fatalf("type %q rejected: %v", value, err)
		}
	}

	badType := validEvidence(f, "EVD-BAD-TYPE")
	badType.Type = "PASSWORD"
	if err := f.db.Create(&badType).Error; err == nil {
		t.Fatal("evidence type check constraint not enforced")
	}

	for i, value := range []string{EvidenceStatusDraft, EvidenceStatusValid, EvidenceStatusRevoked} {
		evidence := validEvidence(f, "EVD-STATUS-"+string(rune('A'+i)))
		evidence.Status = value
		if err := f.db.Create(&evidence).Error; err != nil {
			t.Fatalf("status %q rejected: %v", value, err)
		}
	}

	for _, value := range []string{"EXPIRED", "ARCHIVED", "valid"} {
		evidence := validEvidence(f, "EVD-BAD-"+value)
		evidence.Status = value
		if err := f.db.Create(&evidence).Error; err == nil {
			t.Fatalf("evidence status %q must be rejected", value)
		}
	}
}

func TestEvidenceModelValidityRange(t *testing.T) {
	f := newEvidenceFixture(t)

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(1, 0, 0)

	cases := []struct {
		reference string
		validFrom *time.Time
		expiresAt *time.Time
	}{
		{"EVD-RANGE-NONE", nil, nil},
		{"EVD-RANGE-START", &start, nil},
		{"EVD-RANGE-END", nil, &end},
		{"EVD-RANGE-BOTH", &start, &end},
		{"EVD-RANGE-EQUAL", &start, &start},
	}
	for _, tc := range cases {
		evidence := validEvidence(f, tc.reference)
		evidence.ValidFrom = tc.validFrom
		evidence.ExpiresAt = tc.expiresAt
		if err := f.db.Create(&evidence).Error; err != nil {
			t.Fatalf("%s rejected: %v", tc.reference, err)
		}
	}

	inverted := validEvidence(f, "EVD-RANGE-INVERTED")
	inverted.ValidFrom = &end
	inverted.ExpiresAt = &start
	if err := f.db.Create(&inverted).Error; err == nil {
		t.Fatal("evidence validity range check not enforced")
	}
}

func TestEvidenceModelForeignKeys(t *testing.T) {
	f := newEvidenceFixture(t)

	requireRestrictConstraints(t, f.db, &Evidence{}, "ControlImplementation", "OwnerProfile", "OwningService")

	unknown := uint(999999)

	orphan := validEvidence(f, "EVD-ORPHAN")
	orphan.ControlImplementationID = unknown
	if err := f.db.Create(&orphan).Error; err == nil {
		t.Fatal("evidence control implementation foreign key not enforced")
	}

	badOwner := validEvidence(f, "EVD-BAD-OWNER")
	badOwner.OwnerProfileID = &unknown
	if err := f.db.Create(&badOwner).Error; err == nil {
		t.Fatal("evidence owner profile foreign key not enforced")
	}

	badService := validEvidence(f, "EVD-BAD-SERVICE")
	badService.OwningServiceID = &unknown
	if err := f.db.Create(&badService).Error; err == nil {
		t.Fatal("evidence owning service foreign key not enforced")
	}

	evidence := validEvidence(f, "EVD-LINKED")
	evidence.OwnerProfileID = &f.profile.ID
	evidence.OwningServiceID = &f.service.ID
	if err := f.db.Create(&evidence).Error; err != nil {
		t.Fatal(err)
	}

	if err := f.db.Delete(&ControlImplementation{}, f.implementation.ID).Error; err == nil {
		t.Fatal("deleting a control implementation with evidence must be restricted")
	}
	if err := f.db.Delete(&staff.Profile{}, f.profile.ID).Error; err == nil {
		t.Fatal("deleting an evidence owner profile must be restricted")
	}
	if err := f.db.Delete(&organization.Service{}, f.service.ID).Error; err == nil {
		t.Fatal("deleting an evidence owning service must be restricted")
	}
}

func TestRemediationActionModelTargets(t *testing.T) {
	f := newEvidenceFixture(t)

	controlOnly := validRemediationAction(f, "ACT-CI")
	if err := f.db.Create(&controlOnly).Error; err != nil {
		t.Fatalf("control implementation target rejected: %v", err)
	}

	riskOnly := validRemediationAction(f, "ACT-RISK")
	riskOnly.ControlImplementationID = nil
	riskOnly.RiskID = &f.risk.ID
	if err := f.db.Create(&riskOnly).Error; err != nil {
		t.Fatalf("risk target rejected: %v", err)
	}

	both := validRemediationAction(f, "ACT-BOTH")
	both.RiskID = &f.risk.ID
	if err := f.db.Create(&both).Error; err != nil {
		t.Fatalf("both targets rejected: %v", err)
	}

	none := validRemediationAction(f, "ACT-NONE")
	none.ControlImplementationID = nil
	if err := f.db.Create(&none).Error; err == nil {
		t.Fatal("remediation action target_present check not enforced")
	}

	duplicate := validRemediationAction(f, "ACT-CI")
	if err := f.db.Create(&duplicate).Error; err == nil {
		t.Fatal("remediation action reference uniqueness not enforced")
	}
}

func TestRemediationActionModelPriorityAndStatusChecks(t *testing.T) {
	f := newEvidenceFixture(t)

	for i, value := range []string{
		RemediationActionPriorityLow,
		RemediationActionPriorityMedium,
		RemediationActionPriorityHigh,
		RemediationActionPriorityCritical,
	} {
		action := validRemediationAction(f, "ACT-PRIO-"+string(rune('A'+i)))
		action.Priority = value
		if err := f.db.Create(&action).Error; err != nil {
			t.Fatalf("priority %q rejected: %v", value, err)
		}
	}

	badPriority := validRemediationAction(f, "ACT-BAD-PRIO")
	badPriority.Priority = "URGENT"
	if err := f.db.Create(&badPriority).Error; err == nil {
		t.Fatal("remediation action priority check not enforced")
	}

	completed := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	for i, value := range []string{
		RemediationActionStatusOpen,
		RemediationActionStatusInProgress,
		RemediationActionStatusBlocked,
		RemediationActionStatusDone,
		RemediationActionStatusCancelled,
	} {
		action := validRemediationAction(f, "ACT-STATUS-"+string(rune('A'+i)))
		action.Status = value
		if value == RemediationActionStatusDone {
			action.CompletedAt = &completed
		}
		if err := f.db.Create(&action).Error; err != nil {
			t.Fatalf("status %q rejected: %v", value, err)
		}
	}

	badStatus := validRemediationAction(f, "ACT-BAD-STATUS")
	badStatus.Status = "CLOSED"
	if err := f.db.Create(&badStatus).Error; err == nil {
		t.Fatal("remediation action status check not enforced")
	}
}

func TestRemediationActionModelCompletionCheck(t *testing.T) {
	f := newEvidenceFixture(t)
	completed := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	doneWithDate := validRemediationAction(f, "ACT-DONE-DATE")
	doneWithDate.Status = RemediationActionStatusDone
	doneWithDate.CompletedAt = &completed
	if err := f.db.Create(&doneWithDate).Error; err != nil {
		t.Fatalf("DONE with CompletedAt rejected: %v", err)
	}

	doneWithoutDate := validRemediationAction(f, "ACT-DONE-NODATE")
	doneWithoutDate.Status = RemediationActionStatusDone
	if err := f.db.Create(&doneWithoutDate).Error; err == nil {
		t.Fatal("DONE without CompletedAt must be rejected")
	}

	openWithoutDate := validRemediationAction(f, "ACT-OPEN-NODATE")
	if err := f.db.Create(&openWithoutDate).Error; err != nil {
		t.Fatalf("OPEN without CompletedAt rejected: %v", err)
	}

	for _, status := range []string{
		RemediationActionStatusOpen,
		RemediationActionStatusInProgress,
		RemediationActionStatusBlocked,
		RemediationActionStatusCancelled,
	} {
		action := validRemediationAction(f, "ACT-"+status+"-DATE")
		action.Status = status
		action.CompletedAt = &completed
		if err := f.db.Create(&action).Error; err == nil {
			t.Fatalf("%s with CompletedAt must be rejected", status)
		}
	}
}

func TestRemediationActionModelForeignKeys(t *testing.T) {
	f := newEvidenceFixture(t)

	requireRestrictConstraints(t, f.db, &RemediationAction{},
		"ControlImplementation", "Risk", "AssigneeProfile", "OwningService")

	unknown := uint(999999)

	badImplementation := validRemediationAction(f, "ACT-BAD-CI")
	badImplementation.ControlImplementationID = &unknown
	if err := f.db.Create(&badImplementation).Error; err == nil {
		t.Fatal("remediation action control implementation foreign key not enforced")
	}

	badRisk := validRemediationAction(f, "ACT-BAD-RISK")
	badRisk.RiskID = &unknown
	if err := f.db.Create(&badRisk).Error; err == nil {
		t.Fatal("remediation action risk foreign key not enforced")
	}

	badAssignee := validRemediationAction(f, "ACT-BAD-ASSIGNEE")
	badAssignee.AssigneeProfileID = &unknown
	if err := f.db.Create(&badAssignee).Error; err == nil {
		t.Fatal("remediation action assignee foreign key not enforced")
	}

	badService := validRemediationAction(f, "ACT-BAD-SERVICE")
	badService.OwningServiceID = &unknown
	if err := f.db.Create(&badService).Error; err == nil {
		t.Fatal("remediation action owning service foreign key not enforced")
	}

	linked := validRemediationAction(f, "ACT-LINKED")
	linked.RiskID = &f.risk.ID
	linked.AssigneeProfileID = &f.profile.ID
	linked.OwningServiceID = &f.service.ID
	if err := f.db.Create(&linked).Error; err != nil {
		t.Fatal(err)
	}

	deletions := map[string]func() error{
		"control implementation": func() error { return f.db.Delete(&ControlImplementation{}, f.implementation.ID).Error },
		"risk":                   func() error { return f.db.Delete(&Risk{}, f.risk.ID).Error },
		"assignee profile":       func() error { return f.db.Delete(&staff.Profile{}, f.profile.ID).Error },
		"owning service":         func() error { return f.db.Delete(&organization.Service{}, f.service.ID).Error },
	}
	for name, deleteFn := range deletions {
		if err := deleteFn(); err == nil {
			t.Fatalf("deleting a referenced %s must be restricted", name)
		}
	}
}
