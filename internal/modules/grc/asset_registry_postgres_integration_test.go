package grc

import (
	"net/http"
	"strings"
	"testing"

	"github.com/lallene/medcore-his/backend/internal/modules/auth"
	"github.com/lallene/medcore-his/backend/internal/modules/organization"
	"github.com/lallene/medcore-his/backend/internal/modules/staff"
)

// newPGAssetFixture builds the assetFixture of the SQLite tests in an isolated
// PostgreSQL schema, so the registry helpers run unchanged on PostgreSQL.
func newPGAssetFixture(t *testing.T) assetFixture {
	t.Helper()

	db := grcPostgres(t)
	withCaptureBus(t)
	f := assetFixture{db: db}

	department := organization.Department{Code: "SEC", Name: "Sécurité", Active: true, CreatedBy: 1, UpdatedBy: 1}
	mustCreateAll(t, db, &department)
	service := func(code string) organization.Service {
		return organization.Service{
			DepartmentID: department.ID, Code: code, Name: "Service " + code,
			ServiceType: organization.TypeAdministrative, Active: true, CreatedBy: 1, UpdatedBy: 1,
		}
	}
	f.service, f.inactiveService = service("GRC"), service("OLD")
	mustCreateAll(t, db, &f.service, &f.inactiveService)

	profile := func(code string) staff.Profile {
		user := auth.User{Name: code, Email: strings.ToLower(code) + "@test.local", PasswordHash: "x", Role: "staff", IsActive: true}
		mustCreateAll(t, db, &user)
		return staff.Profile{UserID: user.ID, EmployeeCode: code, Active: true}
	}
	f.profile, f.inactiveProfile = profile("GRC-PG-03"), profile("GRC-PG-03-OLD")
	mustCreateAll(t, db, &f.profile, &f.inactiveProfile)

	// Deactivated after creation: a false zero value would be replaced by the
	// column default on insert.
	if err := db.Model(&f.inactiveService).Update("active", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&f.inactiveProfile).Update("active", false).Error; err != nil {
		t.Fatal(err)
	}
	return f
}

// TestPostgresGRC03Assets runs the asset registry on PostgreSQL: service
// persistence and update, rune-based length limits against varchar columns,
// CHECK/UNIQUE/FK/RESTRICT constraints, Unicode search, filters, pagination
// and the Risk.AssetID relation.
func TestPostgresGRC03Assets(t *testing.T) {
	f := newPGAssetFixture(t)
	db := f.db
	svc := NewService(db)

	// Persist through the service, then read the raw row back.
	created, err := svc.SaveAsset(0, 31, validAssetRequest(f))
	if err != nil {
		t.Fatal(err)
	}
	var stored Asset
	if err := db.First(&stored, created.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Code != "APP-MEDCORE" || stored.Type != AssetTypeApplication || stored.Criticality != AssetCriticalityCritical ||
		stored.Status != AssetStatusActive || stored.CreatedBy != 31 || stored.UpdatedBy != 31 ||
		stored.OwningServiceID == nil || *stored.OwningServiceID != f.service.ID ||
		stored.OwnerProfileID == nil || *stored.OwnerProfileID != f.profile.ID {
		t.Fatalf("unexpected stored asset: %+v", stored)
	}

	// Duplicate code: PostgreSQL unique violation mapped to 409.
	duplicate := validAssetRequest(f)
	duplicate.Name = "Doublon"
	_, err = svc.SaveAsset(0, 31, duplicate)
	requireServiceError(t, err, http.StatusConflict)

	// Column lengths count characters: 60 accented runes fit varchar(60).
	long := validAssetRequest(f)
	long.Code = strings.Repeat("É", assetCodeMaxLength)
	long.Name = strings.Repeat("é", assetNameMaxLength)
	if _, err := svc.SaveAsset(0, 31, long); err != nil {
		t.Fatalf("maximum lengths rejected by PostgreSQL: %v", err)
	}
	long.Code = strings.Repeat("É", assetCodeMaxLength+1)
	_, err = svc.SaveAsset(0, 31, long)
	requireServiceError(t, err, http.StatusBadRequest)

	// Risk.AssetID is the existing relation (risks need an ACTIVE asset).
	risk, err := svc.SaveRisk(0, 33, validRiskRequest(f, created.ID))
	if err != nil {
		t.Fatal(err)
	}

	// Update keeps createdBy and stays possible after the relations were
	// deactivated; a newly assigned inactive relation is refused.
	if err := db.Model(&f.service).Update("active", false).Error; err != nil {
		t.Fatal(err)
	}
	update := validAssetRequest(f)
	update.Status = "retired"
	updated, err := svc.SaveAsset(created.ID, 32, update)
	if err != nil || updated.Status != AssetStatusRetired || updated.CreatedBy != 31 || updated.UpdatedBy != 32 {
		t.Fatalf("update on PostgreSQL: %v %+v", err, updated)
	}
	update.OwnerProfileID = &f.inactiveProfile.ID
	_, err = svc.SaveAsset(created.ID, 32, update)
	requireServiceError(t, err, http.StatusConflict)
	if err := db.Model(&f.service).Update("active", true).Error; err != nil {
		t.Fatal(err)
	}

	// CHECK constraints.
	direct := Asset{Code: "PG-DIRECT", Name: "Direct", Type: AssetTypeServer, Criticality: AssetCriticalityLow, Status: AssetStatusActive, CreatedBy: 1, UpdatedBy: 1}
	bad := direct
	bad.Type = "LAPTOP"
	requirePostgresError(t, db.Create(&bad).Error, pgCheckViolation, "grc_asset_type_valid")
	bad = direct
	bad.Criticality = "VERY_HIGH"
	requirePostgresError(t, db.Create(&bad).Error, pgCheckViolation, "grc_asset_criticality_valid")
	bad = direct
	bad.Status = "DELETED"
	requirePostgresError(t, db.Create(&bad).Error, pgCheckViolation, "grc_asset_status_valid")

	// Foreign keys and RESTRICT on owner, service and risk.
	missing := uint(999999)
	bad = direct
	bad.OwningServiceID = &missing
	requirePostgresError(t, db.Create(&bad).Error, pgForeignKeyViolation, "")
	bad = direct
	bad.OwnerProfileID = &missing
	requirePostgresError(t, db.Create(&bad).Error, pgForeignKeyViolation, "")
	requirePostgresError(t, db.Delete(&organization.Service{}, f.service.ID).Error, pgForeignKeyViolation, "")
	requirePostgresError(t, db.Delete(&staff.Profile{}, f.profile.ID).Error, pgForeignKeyViolation, "")

	requirePostgresError(t, db.Delete(&Asset{}, created.ID).Error, pgForeignKeyViolation, "")
	requireRowExists(t, db, &Asset{}, created.ID)
	risks, err := svc.ListRisks(RiskFilter{AssetID: &created.ID})
	if err != nil || risks.Total != 1 || risks.Items[0].ID != risk.ID {
		t.Fatalf("risks of the asset: %v %+v", err, risks)
	}

	// Search, filters and pagination on PostgreSQL.
	if err := db.Where("id <> ?", created.ID).Delete(&Asset{}).Error; err != nil {
		t.Fatal(err)
	}
	seedAssetRegistry(t, f, svc)
	list := func(filter AssetFilter) []string {
		t.Helper()
		page, err := svc.ListAssets(filter)
		if err != nil {
			t.Fatal(err)
		}
		return assetCodes(page)
	}
	same := func(name string, got []string, want ...string) {
		t.Helper()
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("%s = %v, want %v", name, got, want)
		}
	}
	same("order", list(AssetFilter{}), "DB-LAB", "APP-DPI", "APP-DPI-2", "APP-MEDCORE", "NET-CORE", "SRV-01")
	// The search term is lowered in Go, so accented capitals match.
	same("unicode search", list(AssetFilter{Search: "RÉSEAU CŒUR"}), "NET-CORE")
	same("filters", list(AssetFilter{Type: "APPLICATION", Status: "ACTIVE"}), "APP-DPI", "APP-DPI-2")
	same("owner", list(AssetFilter{OwnerProfileID: &f.profile.ID}), "DB-LAB", "APP-DPI", "APP-MEDCORE")
	page, err := svc.ListAssets(AssetFilter{Page: 2, Limit: 4})
	if err != nil || page.Total != 6 || page.TotalPages != 2 {
		t.Fatalf("page 2: %v %+v", err, page)
	}
	same("page 2", assetCodes(page), "NET-CORE", "SRV-01")
}
