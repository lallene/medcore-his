package grc

import (
	"errors"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/modules/auth"
	"github.com/lallene/medcore-his/backend/internal/modules/organization"
	"github.com/lallene/medcore-his/backend/internal/modules/staff"
	"gorm.io/gorm"
)

const (
	pgUniqueViolation     = "23505"
	pgForeignKeyViolation = "23503"
	pgCheckViolation      = "23514"
)

// grcCompliancePostgres reuses the isolated GRC-01A schema (pgtest DSN,
// unique schema, search_path, DROP SCHEMA on cleanup) and adds GRC-01B tables.
func grcCompliancePostgres(t *testing.T) *gorm.DB {
	t.Helper()

	db := grcPostgres(t)
	if err := db.AutoMigrate(
		&Framework{},
		&Requirement{},
		&Control{},
		&RequirementControl{},
		&ControlRisk{},
		&ControlImplementation{},
	); err != nil {
		t.Fatal(err)
	}

	return db
}

// requirePostgresError asserts a rejection raised by PostgreSQL itself.
func requirePostgresError(t *testing.T, err error, code, constraint string) {
	t.Helper()

	if err == nil {
		t.Fatalf("expected PostgreSQL error %s (%s), got nil", code, constraint)
	}

	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("expected PostgreSQL error %s, got %T: %v", code, err, err)
	}
	if pgErr.Code != code {
		t.Fatalf("PostgreSQL code=%s constraint=%s, want %s", pgErr.Code, pgErr.ConstraintName, code)
	}
	if constraint != "" && pgErr.ConstraintName != constraint {
		t.Fatalf("PostgreSQL constraint=%s, want %s", pgErr.ConstraintName, constraint)
	}
}

// requireServiceError asserts a rejection returned by GRCService.
func requireServiceError(t *testing.T, err error, status int) {
	t.Helper()

	var appErr *coreerrors.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected service AppError %d, got %T: %v", status, err, err)
	}
	if appErr.Status != status {
		t.Fatalf("service status=%d (%s), want %d", appErr.Status, appErr.Message, status)
	}
}

func requireRowExists(t *testing.T, db *gorm.DB, model any, id uint) {
	t.Helper()

	var count int64
	if err := db.Model(model).Where("id = ?", id).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("%T id=%d must still exist after rejected delete", model, id)
	}
}

func TestPostgresGRCComplianceServiceAndConstraints(t *testing.T) {
	db := grcCompliancePostgres(t)
	// Keep the global bus untouched: audit publication is covered by unit tests.
	withCaptureBus(t)
	svc := NewService(db)

	department := organization.Department{Code: "GRC", Name: "GRC", Active: true, CreatedBy: 1, UpdatedBy: 1}
	if err := db.Create(&department).Error; err != nil {
		t.Fatal(err)
	}
	service := organization.Service{
		DepartmentID: department.ID,
		Code:         "SEC",
		Name:         "Sécurité",
		ServiceType:  organization.TypeAdministrative,
		Active:       true,
		CreatedBy:    1,
		UpdatedBy:    1,
	}
	if err := db.Create(&service).Error; err != nil {
		t.Fatal(err)
	}
	user := auth.User{
		Name:         "GRC Compliance Owner",
		Email:        "grc.pg.compliance@test.local",
		PasswordHash: "x",
		Role:         "staff",
		IsActive:     true,
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	profile := staff.Profile{UserID: user.ID, EmployeeCode: "GRC-PG-COMPLIANCE", Active: true}
	if err := db.Create(&profile).Error; err != nil {
		t.Fatal(err)
	}

	// A. Framework
	framework, err := svc.SaveFramework(0, 11, FrameworkRequest{
		Code: "fw-pg", Name: "Référentiel PG", Version: "1.0", Status: "draft",
	})
	if err != nil {
		t.Fatal(err)
	}
	otherFramework, err := svc.SaveFramework(0, 11, FrameworkRequest{
		Code: "FW-PG", Name: "Référentiel PG v2", Version: "2.0", Status: "DRAFT",
	})
	if err != nil {
		t.Fatalf("same code with another version must be accepted: %v", err)
	}

	_, err = svc.SaveFramework(0, 11, FrameworkRequest{
		Code: " fw-pg ", Name: "Doublon", Version: "1.0", Status: "DRAFT",
	})
	requireServiceError(t, err, http.StatusConflict)

	duplicateFramework := Framework{
		Code: "FW-PG", Name: "Doublon direct", Version: "1.0", Status: FrameworkStatusDraft, CreatedBy: 1, UpdatedBy: 1,
	}
	requirePostgresError(t, db.Create(&duplicateFramework).Error, pgUniqueViolation, "idx_grc_frameworks_code_version")

	// B. Requirement
	parent, err := svc.SaveRequirement(0, 12, RequirementRequest{
		FrameworkID: framework.ID, Code: "REQ-1", Title: "Exigence parente", Status: "ACTIVE",
	})
	if err != nil {
		t.Fatal(err)
	}
	child, err := svc.SaveRequirement(0, 12, RequirementRequest{
		FrameworkID: framework.ID, Code: "REQ-1.1", Title: "Exigence enfant", ParentRequirementID: &parent.ID, Status: "ACTIVE",
	})
	if err != nil {
		t.Fatal(err)
	}
	if child.Framework == nil || child.Framework.ID != framework.ID ||
		child.ParentRequirement == nil || child.ParentRequirement.ID != parent.ID {
		t.Fatalf("requirement relations not loaded from PostgreSQL: %+v", child)
	}

	foreignParent, err := svc.SaveRequirement(0, 12, RequirementRequest{
		FrameworkID: otherFramework.ID, Code: "REQ-1", Title: "Autre référentiel", Status: "ACTIVE",
	})
	if err != nil {
		t.Fatalf("same code in another framework must be accepted: %v", err)
	}
	_, err = svc.SaveRequirement(0, 12, RequirementRequest{
		FrameworkID: framework.ID, Code: "REQ-2", Title: "Parent étranger", ParentRequirementID: &foreignParent.ID, Status: "ACTIVE",
	})
	requireServiceError(t, err, http.StatusConflict)

	_, err = svc.SaveRequirement(parent.ID, 12, RequirementRequest{
		FrameworkID: framework.ID, Code: parent.Code, Title: parent.Title, ParentRequirementID: &child.ID, Status: "ACTIVE",
	})
	requireServiceError(t, err, http.StatusConflict)

	_, err = svc.SaveRequirement(0, 12, RequirementRequest{
		FrameworkID: framework.ID, Code: " req-1 ", Title: "Doublon", Status: "ACTIVE",
	})
	requireServiceError(t, err, http.StatusConflict)

	duplicateRequirement := Requirement{
		FrameworkID: framework.ID, Code: "REQ-1", Title: "Doublon direct", Status: RequirementStatusActive, CreatedBy: 1, UpdatedBy: 1,
	}
	requirePostgresError(t, db.Create(&duplicateRequirement).Error, pgUniqueViolation, "idx_grc_requirements_framework_code")

	orphanRequirement := Requirement{
		FrameworkID: 999999, Code: "REQ-ORPHAN", Title: "Sans référentiel", Status: RequirementStatusActive, CreatedBy: 1, UpdatedBy: 1,
	}
	requirePostgresError(t, db.Create(&orphanRequirement).Error, pgForeignKeyViolation, "")

	// C. Control
	control, err := svc.SaveControl(0, 13, ControlRequest{
		Code: "ctl-pg", Name: "Sauvegardes", Category: "Continuité",
		OwnerProfileID: &profile.ID, OwningServiceID: &service.ID, Status: "active",
	})
	if err != nil {
		t.Fatal(err)
	}
	if control.OwnerProfile == nil || control.OwningService == nil {
		t.Fatalf("control relations not loaded from PostgreSQL: %+v", control)
	}
	linkedOnlyControl, err := svc.SaveControl(0, 13, ControlRequest{
		Code: "CTL-PG-LINKED", Name: "Contrôle lié à une exigence", Status: "ACTIVE",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.SaveControl(0, 13, ControlRequest{Code: " ctl-pg ", Name: "Doublon", Status: "ACTIVE"})
	requireServiceError(t, err, http.StatusConflict)

	duplicateControl := Control{Code: "CTL-PG", Name: "Doublon direct", Status: ControlStatusActive, CreatedBy: 1, UpdatedBy: 1}
	requirePostgresError(t, db.Create(&duplicateControl).Error, pgUniqueViolation, "idx_grc_controls_code")

	// D. ControlImplementation
	asset, err := svc.SaveAsset(0, 14, AssetRequest{
		Code: "APP-PG", Name: "Application PG", Type: "APPLICATION", Criticality: "HIGH", Status: "ACTIVE",
	})
	if err != nil {
		t.Fatal(err)
	}
	implementation, err := svc.SaveControlImplementation(0, 14, ControlImplementationRequest{
		ControlID: control.ID, AssetID: asset.ID, OwningServiceID: &service.ID, OwnerProfileID: &profile.ID, Status: "planned",
	})
	if err != nil {
		t.Fatal(err)
	}
	if implementation.Control == nil || implementation.Asset == nil {
		t.Fatalf("implementation relations not loaded from PostgreSQL: %+v", implementation)
	}

	_, err = svc.SaveControlImplementation(0, 14, ControlImplementationRequest{
		ControlID: control.ID, AssetID: asset.ID, Status: "IMPLEMENTED",
	})
	requireServiceError(t, err, http.StatusConflict)

	duplicateImplementation := ControlImplementation{
		ControlID: control.ID, AssetID: asset.ID, Status: ControlImplementationStatusPlanned, CreatedBy: 1, UpdatedBy: 1,
	}
	requirePostgresError(t, db.Create(&duplicateImplementation).Error, pgUniqueViolation, "idx_grc_control_implementations_control_asset")

	unknownControl := ControlImplementation{
		ControlID: 999999, AssetID: asset.ID, Status: ControlImplementationStatusPlanned, CreatedBy: 1, UpdatedBy: 1,
	}
	requirePostgresError(t, db.Create(&unknownControl).Error, pgForeignKeyViolation, "")

	unknownAsset := ControlImplementation{
		ControlID: linkedOnlyControl.ID, AssetID: 999999, Status: ControlImplementationStatusPlanned, CreatedBy: 1, UpdatedBy: 1,
	}
	requirePostgresError(t, db.Create(&unknownAsset).Error, pgForeignKeyViolation, "")

	// E. RequirementControl
	requirementLink, err := svc.LinkRequirementControl(15, RequirementControlRequest{RequirementID: parent.ID, ControlID: linkedOnlyControl.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.LinkRequirementControl(15, RequirementControlRequest{RequirementID: child.ID, ControlID: control.ID}); err != nil {
		t.Fatal(err)
	}

	_, err = svc.LinkRequirementControl(15, RequirementControlRequest{RequirementID: parent.ID, ControlID: linkedOnlyControl.ID})
	requireServiceError(t, err, http.StatusConflict)

	duplicateRequirementLink := RequirementControl{RequirementID: parent.ID, ControlID: linkedOnlyControl.ID, CreatedBy: 1}
	requirePostgresError(t, db.Create(&duplicateRequirementLink).Error, pgUniqueViolation, "idx_grc_requirement_controls_requirement_control")

	// F. ControlRisk
	risk, err := svc.SaveRisk(0, 16, RiskRequest{
		Reference: "RISK-PG", Title: "Perte de sauvegarde", Description: "Risque PG",
		Likelihood: 3, Impact: 4, ResidualLikelihood: 2, ResidualImpact: 2,
		Treatment: "REDUCE", Status: "OPEN",
	})
	if err != nil {
		t.Fatal(err)
	}
	riskLink, err := svc.LinkControlRisk(16, ControlRiskRequest{ControlID: control.ID, RiskID: risk.ID})
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.LinkControlRisk(16, ControlRiskRequest{ControlID: control.ID, RiskID: risk.ID})
	requireServiceError(t, err, http.StatusConflict)

	duplicateRiskLink := ControlRisk{ControlID: control.ID, RiskID: risk.ID, CreatedBy: 1}
	requirePostgresError(t, db.Create(&duplicateRiskLink).Error, pgUniqueViolation, "idx_grc_control_risks_control_risk")

	// DELETE RESTRICT, raised by PostgreSQL foreign keys.
	requirePostgresError(t, db.Delete(&Framework{}, framework.ID).Error, pgForeignKeyViolation, "")
	requireRowExists(t, db, &Framework{}, framework.ID)
	requireRowExists(t, db, &Requirement{}, parent.ID)

	requirePostgresError(t, db.Delete(&Requirement{}, parent.ID).Error, pgForeignKeyViolation, "")
	requireRowExists(t, db, &Requirement{}, parent.ID)
	requireRowExists(t, db, &Requirement{}, child.ID)

	// linkedOnlyControl is referenced by grc_requirement_controls only.
	requirePostgresError(t, db.Delete(&Control{}, linkedOnlyControl.ID).Error, pgForeignKeyViolation, "")
	requireRowExists(t, db, &Control{}, linkedOnlyControl.ID)
	requireRowExists(t, db, &RequirementControl{}, requirementLink.ID)

	// asset is referenced by grc_control_implementations only.
	requirePostgresError(t, db.Delete(&Asset{}, asset.ID).Error, pgForeignKeyViolation, "")
	requireRowExists(t, db, &Asset{}, asset.ID)
	requireRowExists(t, db, &ControlImplementation{}, implementation.ID)

	// risk has no asset and is referenced by grc_control_risks only.
	requirePostgresError(t, db.Delete(&Risk{}, risk.ID).Error, pgForeignKeyViolation, "")
	requireRowExists(t, db, &Risk{}, risk.ID)
	requireRowExists(t, db, &ControlRisk{}, riskLink.ID)

	// CHECK constraints, bypassing service validation on purpose.
	badFramework := Framework{
		Code: "FW-BAD", Name: "Statut invalide", Version: "1.0", Status: "COMPLIANT", CreatedBy: 1, UpdatedBy: 1,
	}
	requirePostgresError(t, db.Create(&badFramework).Error, pgCheckViolation, "grc_framework_status_valid")

	badImplementation := ControlImplementation{
		ControlID: linkedOnlyControl.ID, AssetID: asset.ID, Status: "DONE", CreatedBy: 1, UpdatedBy: 1,
	}
	requirePostgresError(t, db.Create(&badImplementation).Error, pgCheckViolation, "grc_control_implementation_status_valid")

	// Unlink removes only the relation rows.
	if err = svc.UnlinkRequirementControl(17, parent.ID, linkedOnlyControl.ID); err != nil {
		t.Fatal(err)
	}
	if err = svc.UnlinkControlRisk(17, control.ID, risk.ID); err != nil {
		t.Fatal(err)
	}
	requireRowExists(t, db, &Requirement{}, parent.ID)
	requireRowExists(t, db, &Control{}, linkedOnlyControl.ID)
	requireRowExists(t, db, &Control{}, control.ID)
	requireRowExists(t, db, &Risk{}, risk.ID)
}
