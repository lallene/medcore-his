package grc

import (
	"testing"

	"github.com/lallene/medcore-his/backend/internal/modules/auth"
	"github.com/lallene/medcore-his/backend/internal/modules/organization"
	"github.com/lallene/medcore-his/backend/internal/modules/staff"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type complianceFixture struct {
	db          *gorm.DB
	framework   Framework
	requirement Requirement
	control     Control
	asset       Asset
	risk        Risk
}

func newComplianceFixture(t *testing.T) complianceFixture {
	t.Helper()

	db, err := gorm.Open(
		sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared&_foreign_keys=1"),
		&gorm.Config{},
	)
	if err != nil {
		t.Fatal(err)
	}

	if err = db.AutoMigrate(
		&auth.User{},
		&organization.Department{},
		&organization.Service{},
		&staff.Profile{},
		&Asset{},
		&Risk{},
		&Framework{},
		&Requirement{},
		&Control{},
		&RequirementControl{},
		&ControlRisk{},
		&ControlImplementation{},
	); err != nil {
		t.Fatal(err)
	}

	f := complianceFixture{db: db}

	f.framework = Framework{
		Code:      "FW-TEST",
		Name:      "Référentiel de test",
		Version:   "1.0",
		Status:    FrameworkStatusDraft,
		CreatedBy: 1,
		UpdatedBy: 1,
	}
	if err = db.Create(&f.framework).Error; err != nil {
		t.Fatal(err)
	}

	f.requirement = Requirement{
		FrameworkID: f.framework.ID,
		Code:        "REQ-1",
		Title:       "Exigence de test",
		Status:      RequirementStatusActive,
		CreatedBy:   1,
		UpdatedBy:   1,
	}
	if err = db.Create(&f.requirement).Error; err != nil {
		t.Fatal(err)
	}

	f.control = Control{
		Code:      "CTL-1",
		Name:      "Contrôle de test",
		Status:    ControlStatusDraft,
		CreatedBy: 1,
		UpdatedBy: 1,
	}
	if err = db.Create(&f.control).Error; err != nil {
		t.Fatal(err)
	}

	f.asset = Asset{
		Code:        "AST-1",
		Name:        "Actif de test",
		Type:        AssetTypeApplication,
		Criticality: AssetCriticalityHigh,
		Status:      AssetStatusActive,
		CreatedBy:   1,
		UpdatedBy:   1,
	}
	if err = db.Create(&f.asset).Error; err != nil {
		t.Fatal(err)
	}

	f.risk = Risk{
		Reference:          "RSK-1",
		Title:              "Risque de test",
		Description:        "Description",
		Likelihood:         2,
		Impact:             3,
		InherentScore:      6,
		ResidualLikelihood: 1,
		ResidualImpact:     3,
		ResidualScore:      3,
		Treatment:          RiskTreatmentReduce,
		Status:             RiskStatusOpen,
		CreatedBy:          1,
		UpdatedBy:          1,
	}
	if err = db.Create(&f.risk).Error; err != nil {
		t.Fatal(err)
	}

	return f
}

func TestComplianceModelTableNames(t *testing.T) {
	cases := map[string]interface{ TableName() string }{
		"grc_frameworks":              Framework{},
		"grc_requirements":            Requirement{},
		"grc_controls":                Control{},
		"grc_requirement_controls":    RequirementControl{},
		"grc_control_risks":           ControlRisk{},
		"grc_control_implementations": ControlImplementation{},
	}
	for want, model := range cases {
		if got := model.TableName(); got != want {
			t.Fatalf("TableName() = %q, want %q", got, want)
		}
	}
}

func TestComplianceStatusConstants(t *testing.T) {
	pairs := [][2]string{
		{FrameworkStatusDraft, "DRAFT"},
		{FrameworkStatusActive, "ACTIVE"},
		{FrameworkStatusArchived, "ARCHIVED"},
		{RequirementStatusActive, "ACTIVE"},
		{RequirementStatusInactive, "INACTIVE"},
		{ControlStatusDraft, "DRAFT"},
		{ControlStatusActive, "ACTIVE"},
		{ControlStatusInactive, "INACTIVE"},
		{ControlStatusRetired, "RETIRED"},
		{ControlImplementationStatusPlanned, "PLANNED"},
		{ControlImplementationStatusImplemented, "IMPLEMENTED"},
		{ControlImplementationStatusPartial, "PARTIAL"},
		{ControlImplementationStatusNotImplemented, "NOT_IMPLEMENTED"},
		{ControlImplementationStatusNotApplicable, "NOT_APPLICABLE"},
	}
	for _, pair := range pairs {
		if pair[0] != pair[1] {
			t.Fatalf("status constant = %q, want %q", pair[0], pair[1])
		}
	}
}

func TestFrameworkCodeVersionUniqueness(t *testing.T) {
	f := newComplianceFixture(t)

	sameCodeNewVersion := Framework{
		Code:      f.framework.Code,
		Name:      "Référentiel de test v2",
		Version:   "2.0",
		Status:    FrameworkStatusDraft,
		CreatedBy: 1,
		UpdatedBy: 1,
	}
	if err := f.db.Create(&sameCodeNewVersion).Error; err != nil {
		t.Fatalf("same code with another version must be accepted: %v", err)
	}

	duplicate := Framework{
		Code:      f.framework.Code,
		Name:      "Doublon",
		Version:   f.framework.Version,
		Status:    FrameworkStatusDraft,
		CreatedBy: 1,
		UpdatedBy: 1,
	}
	if err := f.db.Create(&duplicate).Error; err == nil {
		t.Fatal("framework code+version uniqueness not enforced")
	}
}

func TestRequirementFrameworkCodeUniqueness(t *testing.T) {
	f := newComplianceFixture(t)

	otherFramework := Framework{
		Code:      "FW-OTHER",
		Name:      "Autre référentiel",
		Version:   "1.0",
		Status:    FrameworkStatusDraft,
		CreatedBy: 1,
		UpdatedBy: 1,
	}
	if err := f.db.Create(&otherFramework).Error; err != nil {
		t.Fatal(err)
	}

	sameCodeOtherFramework := Requirement{
		FrameworkID: otherFramework.ID,
		Code:        f.requirement.Code,
		Title:       "Même code, autre référentiel",
		Status:      RequirementStatusActive,
		CreatedBy:   1,
		UpdatedBy:   1,
	}
	if err := f.db.Create(&sameCodeOtherFramework).Error; err != nil {
		t.Fatalf("same code in another framework must be accepted: %v", err)
	}

	duplicate := Requirement{
		FrameworkID: f.framework.ID,
		Code:        f.requirement.Code,
		Title:       "Doublon",
		Status:      RequirementStatusActive,
		CreatedBy:   1,
		UpdatedBy:   1,
	}
	if err := f.db.Create(&duplicate).Error; err == nil {
		t.Fatal("requirement framework+code uniqueness not enforced")
	}
}

func TestRequirementParentRelation(t *testing.T) {
	f := newComplianceFixture(t)

	child := Requirement{
		FrameworkID:         f.framework.ID,
		Code:                "REQ-1.1",
		Title:               "Sous-exigence",
		ParentRequirementID: &f.requirement.ID,
		Status:              RequirementStatusActive,
		CreatedBy:           1,
		UpdatedBy:           1,
	}
	if err := f.db.Create(&child).Error; err != nil {
		t.Fatal(err)
	}

	var loaded Requirement
	if err := f.db.Preload("ParentRequirement").First(&loaded, child.ID).Error; err != nil {
		t.Fatal(err)
	}
	if loaded.ParentRequirement == nil || loaded.ParentRequirement.ID != f.requirement.ID {
		t.Fatalf("parent requirement not loaded: %+v", loaded.ParentRequirement)
	}

	if err := f.db.Delete(&Requirement{}, f.requirement.ID).Error; err == nil {
		t.Fatal("deleting a parent requirement must be restricted")
	}
}

func TestControlCodeUniqueness(t *testing.T) {
	f := newComplianceFixture(t)

	duplicate := Control{
		Code:      f.control.Code,
		Name:      "Doublon",
		Status:    ControlStatusDraft,
		CreatedBy: 1,
		UpdatedBy: 1,
	}
	if err := f.db.Create(&duplicate).Error; err == nil {
		t.Fatal("control code uniqueness not enforced")
	}
}

func TestRequirementControlUniqueness(t *testing.T) {
	f := newComplianceFixture(t)

	link := RequirementControl{RequirementID: f.requirement.ID, ControlID: f.control.ID, CreatedBy: 1}
	if err := f.db.Create(&link).Error; err != nil {
		t.Fatal(err)
	}

	duplicate := RequirementControl{RequirementID: f.requirement.ID, ControlID: f.control.ID, CreatedBy: 1}
	if err := f.db.Create(&duplicate).Error; err == nil {
		t.Fatal("requirement+control uniqueness not enforced")
	}
}

func TestControlRiskUniqueness(t *testing.T) {
	f := newComplianceFixture(t)

	link := ControlRisk{ControlID: f.control.ID, RiskID: f.risk.ID, CreatedBy: 1}
	if err := f.db.Create(&link).Error; err != nil {
		t.Fatal(err)
	}

	duplicate := ControlRisk{ControlID: f.control.ID, RiskID: f.risk.ID, CreatedBy: 1}
	if err := f.db.Create(&duplicate).Error; err == nil {
		t.Fatal("control+risk uniqueness not enforced")
	}
}

func TestControlImplementationUniqueness(t *testing.T) {
	f := newComplianceFixture(t)

	implementation := ControlImplementation{
		ControlID: f.control.ID,
		AssetID:   f.asset.ID,
		Status:    ControlImplementationStatusPlanned,
		CreatedBy: 1,
		UpdatedBy: 1,
	}
	if err := f.db.Create(&implementation).Error; err != nil {
		t.Fatal(err)
	}

	duplicate := ControlImplementation{
		ControlID: f.control.ID,
		AssetID:   f.asset.ID,
		Status:    ControlImplementationStatusImplemented,
		CreatedBy: 1,
		UpdatedBy: 1,
	}
	if err := f.db.Create(&duplicate).Error; err == nil {
		t.Fatal("control+asset uniqueness not enforced")
	}
}

func TestComplianceLinkedRecordsRestrictDelete(t *testing.T) {
	f := newComplianceFixture(t)

	if err := f.db.Create(&RequirementControl{RequirementID: f.requirement.ID, ControlID: f.control.ID, CreatedBy: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Create(&ControlRisk{ControlID: f.control.ID, RiskID: f.risk.ID, CreatedBy: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Create(&ControlImplementation{
		ControlID: f.control.ID,
		AssetID:   f.asset.ID,
		Status:    ControlImplementationStatusPlanned,
		CreatedBy: 1,
		UpdatedBy: 1,
	}).Error; err != nil {
		t.Fatal(err)
	}

	deletions := map[string]func() error{
		"framework":   func() error { return f.db.Delete(&Framework{}, f.framework.ID).Error },
		"requirement": func() error { return f.db.Delete(&Requirement{}, f.requirement.ID).Error },
		"control":     func() error { return f.db.Delete(&Control{}, f.control.ID).Error },
		"risk":        func() error { return f.db.Delete(&Risk{}, f.risk.ID).Error },
		"asset":       func() error { return f.db.Delete(&Asset{}, f.asset.ID).Error },
	}
	for name, deleteFn := range deletions {
		if err := deleteFn(); err == nil {
			t.Fatalf("deleting a linked %s must be restricted", name)
		}
	}
}

func TestComplianceStatusCheckConstraints(t *testing.T) {
	f := newComplianceFixture(t)

	if err := f.db.Create(&Framework{
		Code: "FW-BAD", Name: "Bad", Version: "1.0", Status: "PUBLISHED", CreatedBy: 1, UpdatedBy: 1,
	}).Error; err == nil {
		t.Fatal("framework status check constraint not enforced")
	}

	if err := f.db.Create(&Requirement{
		FrameworkID: f.framework.ID, Code: "REQ-BAD", Title: "Bad", Status: "DRAFT", CreatedBy: 1, UpdatedBy: 1,
	}).Error; err == nil {
		t.Fatal("requirement status check constraint not enforced")
	}

	if err := f.db.Create(&Control{
		Code: "CTL-BAD", Name: "Bad", Status: "ARCHIVED", CreatedBy: 1, UpdatedBy: 1,
	}).Error; err == nil {
		t.Fatal("control status check constraint not enforced")
	}

	if err := f.db.Create(&ControlImplementation{
		ControlID: f.control.ID, AssetID: f.asset.ID, Status: "DONE", CreatedBy: 1, UpdatedBy: 1,
	}).Error; err == nil {
		t.Fatal("control implementation status check constraint not enforced")
	}
}
