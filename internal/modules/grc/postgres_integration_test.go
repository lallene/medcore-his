package grc

import (
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/auth"
	"github.com/lallene/medcore-his/backend/internal/modules/organization"
	"github.com/lallene/medcore-his/backend/internal/modules/staff"
	"github.com/lallene/medcore-his/backend/internal/testsupport/pgtest"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func grcPostgres(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := pgtest.DSN(t)

	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}

	schema := fmt.Sprintf("grc_%d", time.Now().UnixNano())
	if err = admin.Exec(`CREATE SCHEMA "` + schema + `"`).Error; err != nil {
		t.Fatal(err)
	}

	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	scoped := dsn + sep + "search_path=" + url.QueryEscape(schema)

	db, err := gorm.Open(postgres.Open(scoped), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_ = sqlDB.Close()
		_ = admin.Exec(`DROP SCHEMA IF EXISTS "` + schema + `" CASCADE`).Error
	})

	if err = db.AutoMigrate(
		&auth.User{},
		&organization.Department{},
		&organization.Service{},
		&staff.Profile{},
		&Asset{},
		&Risk{},
	); err != nil {
		t.Fatal(err)
	}

	return db
}

func TestPostgresGRCAssetConstraints(t *testing.T) {
	db := grcPostgres(t)

	department := organization.Department{
		Code:      "SEC",
		Name:      "Sécurité",
		Active:    true,
		CreatedBy: 1,
		UpdatedBy: 1,
	}
	if err := db.Create(&department).Error; err != nil {
		t.Fatal(err)
	}

	service := organization.Service{
		DepartmentID: department.ID,
		Code:         "GRC",
		Name:         "GRC",
		ServiceType:  organization.TypeAdministrative,
		Active:       true,
		CreatedBy:    1,
		UpdatedBy:    1,
	}
	if err := db.Create(&service).Error; err != nil {
		t.Fatal(err)
	}

	user := auth.User{
		Name:         "GRC Owner",
		Email:        "grc.pg.owner@test.local",
		PasswordHash: "x",
		Role:         "staff",
		IsActive:     true,
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}

	profile := staff.Profile{
		UserID:       user.ID,
		EmployeeCode: "GRC-PG-001",
		Active:       true,
	}
	if err := db.Create(&profile).Error; err != nil {
		t.Fatal(err)
	}

	valid := Asset{
		Code:            "APP-MEDCORE",
		Name:            "MedCore HIS",
		Type:            AssetTypeApplication,
		Criticality:     AssetCriticalityCritical,
		OwningServiceID: &service.ID,
		OwnerProfileID:  &profile.ID,
		Status:          AssetStatusActive,
		CreatedBy:       1,
		UpdatedBy:       1,
	}
	if err := db.Create(&valid).Error; err != nil {
		t.Fatal(err)
	}

	duplicate := valid
	duplicate.ID = 0
	duplicate.Name = "Duplicate"
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("unique asset code not enforced")
	}

	badType := valid
	badType.ID = 0
	badType.Code = "BAD-TYPE"
	badType.Type = "UNKNOWN"
	if err := db.Create(&badType).Error; err == nil {
		t.Fatal("asset type check constraint not enforced")
	}

	badCriticality := valid
	badCriticality.ID = 0
	badCriticality.Code = "BAD-CRITICALITY"
	badCriticality.Criticality = "EXTREME"
	if err := db.Create(&badCriticality).Error; err == nil {
		t.Fatal("asset criticality check constraint not enforced")
	}

	badStatus := valid
	badStatus.ID = 0
	badStatus.Code = "BAD-STATUS"
	badStatus.Status = "DELETED"
	if err := db.Create(&badStatus).Error; err == nil {
		t.Fatal("asset status check constraint not enforced")
	}

	unknownService := valid
	unknownService.ID = 0
	unknownService.Code = "BAD-SERVICE"
	missingServiceID := uint(999999)
	unknownService.OwningServiceID = &missingServiceID
	if err := db.Create(&unknownService).Error; err == nil {
		t.Fatal("asset owning service foreign key not enforced")
	}

	unknownOwner := valid
	unknownOwner.ID = 0
	unknownOwner.Code = "BAD-OWNER"
	missingProfileID := uint(999999)
	unknownOwner.OwnerProfileID = &missingProfileID
	if err := db.Create(&unknownOwner).Error; err == nil {
		t.Fatal("asset owner profile foreign key not enforced")
	}

	if err := db.Delete(&service).Error; err == nil {
		t.Fatal("asset owning service RESTRICT not enforced")
	}

	if err := db.Delete(&profile).Error; err == nil {
		t.Fatal("asset owner profile RESTRICT not enforced")
	}
}

func TestPostgresGRCRiskConstraints(t *testing.T) {
	db := grcPostgres(t)

	department := organization.Department{
		Code:      "RISK",
		Name:      "Risk Management",
		Active:    true,
		CreatedBy: 1,
		UpdatedBy: 1,
	}
	if err := db.Create(&department).Error; err != nil {
		t.Fatal(err)
	}

	service := organization.Service{
		DepartmentID: department.ID,
		Code:         "RISK",
		Name:         "Risk Management",
		ServiceType:  organization.TypeAdministrative,
		Active:       true,
		CreatedBy:    1,
		UpdatedBy:    1,
	}
	if err := db.Create(&service).Error; err != nil {
		t.Fatal(err)
	}

	user := auth.User{
		Name:         "Risk Owner",
		Email:        "grc.pg.risk.owner@test.local",
		PasswordHash: "x",
		Role:         "staff",
		IsActive:     true,
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}

	owner := staff.Profile{
		UserID:       user.ID,
		EmployeeCode: "GRC-RISK-OWNER",
		Active:       true,
	}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatal(err)
	}

	approverUser := auth.User{
		Name:         "Risk Approver",
		Email:        "grc.pg.risk.approver@test.local",
		PasswordHash: "x",
		Role:         "staff",
		IsActive:     true,
	}
	if err := db.Create(&approverUser).Error; err != nil {
		t.Fatal(err)
	}

	approver := staff.Profile{
		UserID:       approverUser.ID,
		EmployeeCode: "GRC-RISK-APPROVER",
		Active:       true,
	}
	if err := db.Create(&approver).Error; err != nil {
		t.Fatal(err)
	}

	asset := Asset{
		Code:        "APP-RISK",
		Name:        "Risk Test Application",
		Type:        AssetTypeApplication,
		Criticality: AssetCriticalityCritical,
		Status:      AssetStatusActive,
		CreatedBy:   1,
		UpdatedBy:   1,
	}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}

	valid := Risk{
		Reference:          "RISK-PG-001",
		Title:              "PostgreSQL risk constraint test",
		Description:        "Valid risk used as constraint baseline",
		AssetID:            &asset.ID,
		OwningServiceID:    &service.ID,
		OwnerProfileID:     &owner.ID,
		ApproverProfileID:  &approver.ID,
		Threat:             "Threat",
		Vulnerability:      "Vulnerability",
		Likelihood:         4,
		Impact:             5,
		InherentScore:      20,
		ResidualLikelihood: 2,
		ResidualImpact:     3,
		ResidualScore:      6,
		Treatment:          RiskTreatmentReduce,
		Status:             RiskStatusOpen,
		CreatedBy:          1,
		UpdatedBy:          1,
	}
	if err := db.Create(&valid).Error; err != nil {
		t.Fatal(err)
	}

	duplicate := valid
	duplicate.ID = 0
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("unique risk reference not enforced")
	}

	numericCases := []struct {
		name   string
		change func(*Risk)
	}{
		{"likelihood below", func(r *Risk) { r.Likelihood = 0 }},
		{"likelihood above", func(r *Risk) { r.Likelihood = 6 }},
		{"impact below", func(r *Risk) { r.Impact = 0 }},
		{"impact above", func(r *Risk) { r.Impact = 6 }},
		{"inherent score below", func(r *Risk) { r.InherentScore = 0 }},
		{"inherent score above", func(r *Risk) { r.InherentScore = 26 }},
		{"residual likelihood below", func(r *Risk) { r.ResidualLikelihood = 0 }},
		{"residual likelihood above", func(r *Risk) { r.ResidualLikelihood = 6 }},
		{"residual impact below", func(r *Risk) { r.ResidualImpact = 0 }},
		{"residual impact above", func(r *Risk) { r.ResidualImpact = 6 }},
		{"residual score below", func(r *Risk) { r.ResidualScore = 0 }},
		{"residual score above", func(r *Risk) { r.ResidualScore = 26 }},
	}

	for i, tc := range numericCases {
		t.Run(tc.name, func(t *testing.T) {
			candidate := valid
			candidate.ID = 0
			candidate.Reference = fmt.Sprintf("RISK-NUM-%02d", i)
			tc.change(&candidate)

			if err := db.Create(&candidate).Error; err == nil {
				t.Fatal("risk numeric check constraint not enforced")
			}
		})
	}

	badTreatment := valid
	badTreatment.ID = 0
	badTreatment.Reference = "RISK-BAD-TREATMENT"
	badTreatment.Treatment = "IGNORE"
	if err := db.Create(&badTreatment).Error; err == nil {
		t.Fatal("risk treatment check constraint not enforced")
	}

	badStatus := valid
	badStatus.ID = 0
	badStatus.Reference = "RISK-BAD-STATUS"
	badStatus.Status = "DELETED"
	if err := db.Create(&badStatus).Error; err == nil {
		t.Fatal("risk status check constraint not enforced")
	}

	missingID := uint(999999)

	unknownAsset := valid
	unknownAsset.ID = 0
	unknownAsset.Reference = "RISK-BAD-ASSET"
	unknownAsset.AssetID = &missingID
	if err := db.Create(&unknownAsset).Error; err == nil {
		t.Fatal("risk asset foreign key not enforced")
	}

	unknownService := valid
	unknownService.ID = 0
	unknownService.Reference = "RISK-BAD-SERVICE"
	unknownService.OwningServiceID = &missingID
	if err := db.Create(&unknownService).Error; err == nil {
		t.Fatal("risk owning service foreign key not enforced")
	}

	unknownOwner := valid
	unknownOwner.ID = 0
	unknownOwner.Reference = "RISK-BAD-OWNER"
	unknownOwner.OwnerProfileID = &missingID
	if err := db.Create(&unknownOwner).Error; err == nil {
		t.Fatal("risk owner profile foreign key not enforced")
	}

	unknownApprover := valid
	unknownApprover.ID = 0
	unknownApprover.Reference = "RISK-BAD-APPROVER"
	unknownApprover.ApproverProfileID = &missingID
	if err := db.Create(&unknownApprover).Error; err == nil {
		t.Fatal("risk approver profile foreign key not enforced")
	}

	if err := db.Delete(&asset).Error; err == nil {
		t.Fatal("risk asset RESTRICT not enforced")
	}

	if err := db.Delete(&service).Error; err == nil {
		t.Fatal("risk owning service RESTRICT not enforced")
	}

	if err := db.Delete(&owner).Error; err == nil {
		t.Fatal("risk owner profile RESTRICT not enforced")
	}

	if err := db.Delete(&approver).Error; err == nil {
		t.Fatal("risk approver profile RESTRICT not enforced")
	}
}
