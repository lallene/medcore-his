package grc

import (
	"testing"

	"github.com/lallene/medcore-his/backend/internal/modules/auth"
	"github.com/lallene/medcore-his/backend/internal/modules/organization"
	"github.com/lallene/medcore-his/backend/internal/modules/staff"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type assetFixture struct {
	db              *gorm.DB
	service         organization.Service
	inactiveService organization.Service
	profile         staff.Profile
	inactiveProfile staff.Profile
}

func newAssetFixture(t *testing.T) assetFixture {
	t.Helper()

	db, err := gorm.Open(
		sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"),
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
	); err != nil {
		t.Fatal(err)
	}

	department := organization.Department{
		Code:      "GRC",
		Name:      "GRC Test",
		Active:    true,
		CreatedBy: 1,
		UpdatedBy: 1,
	}
	if err = db.Create(&department).Error; err != nil {
		t.Fatal(err)
	}

	activeService := organization.Service{
		DepartmentID: department.ID,
		Code:         "SEC",
		Name:         "Sécurité",
		ServiceType:  organization.TypeAdministrative,
		Active:       true,
		CreatedBy:    1,
		UpdatedBy:    1,
	}
	if err = db.Create(&activeService).Error; err != nil {
		t.Fatal(err)
	}

	inactiveService := organization.Service{
		DepartmentID: department.ID,
		Code:         "OLDSEC",
		Name:         "Ancienne sécurité",
		ServiceType:  organization.TypeAdministrative,
		Active:       false,
		CreatedBy:    1,
		UpdatedBy:    1,
	}
	if err = db.Create(&inactiveService).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Model(&inactiveService).Update("active", false).Error; err != nil {
		t.Fatal(err)
	}

	activeUser := auth.User{
		Name:         "GRC Owner",
		Email:        "grc.owner@test.local",
		PasswordHash: "x",
		Role:         "staff",
		IsActive:     true,
	}
	if err = db.Create(&activeUser).Error; err != nil {
		t.Fatal(err)
	}

	activeProfile := staff.Profile{
		UserID:       activeUser.ID,
		EmployeeCode: "GRC-OWNER",
		Active:       true,
	}
	if err = db.Create(&activeProfile).Error; err != nil {
		t.Fatal(err)
	}

	inactiveUser := auth.User{
		Name:         "Inactive GRC Owner",
		Email:        "grc.inactive@test.local",
		PasswordHash: "x",
		Role:         "staff",
		IsActive:     true,
	}
	if err = db.Create(&inactiveUser).Error; err != nil {
		t.Fatal(err)
	}

	inactiveProfile := staff.Profile{
		UserID:       inactiveUser.ID,
		EmployeeCode: "GRC-INACTIVE",
		Active:       false,
	}
	if err = db.Create(&inactiveProfile).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Model(&inactiveProfile).Update("active", false).Error; err != nil {
		t.Fatal(err)
	}

	return assetFixture{
		db:              db,
		service:         activeService,
		inactiveService: inactiveService,
		profile:         activeProfile,
		inactiveProfile: inactiveProfile,
	}
}

func validAssetRequest(f assetFixture) AssetRequest {
	return AssetRequest{
		Code:            " app-medcore ",
		Name:            " MedCore HIS ",
		Type:            " application ",
		Description:     " Système d'information hospitalier ",
		Criticality:     " critical ",
		OwningServiceID: &f.service.ID,
		OwnerProfileID:  &f.profile.ID,
		Status:          " active ",
	}
}

func TestSaveAssetCreatesNormalizedAssetAndAuditActors(t *testing.T) {
	f := newAssetFixture(t)
	svc := NewService(f.db)

	created, err := svc.SaveAsset(0, 41, validAssetRequest(f))
	if err != nil {
		t.Fatal(err)
	}

	if created.Code != "APP-MEDCORE" ||
		created.Name != "MedCore HIS" ||
		created.Type != AssetTypeApplication ||
		created.Description != "Système d'information hospitalier" ||
		created.Criticality != AssetCriticalityCritical ||
		created.Status != AssetStatusActive {
		t.Fatalf("unexpected normalized asset: %#v", created)
	}

	if created.CreatedBy != 41 || created.UpdatedBy != 41 {
		t.Fatalf(
			"actors = created:%d updated:%d, want 41/41",
			created.CreatedBy,
			created.UpdatedBy,
		)
	}
}

func TestSaveAssetUpdatePreservesCreatedBy(t *testing.T) {
	f := newAssetFixture(t)
	svc := NewService(f.db)

	created, err := svc.SaveAsset(0, 41, validAssetRequest(f))
	if err != nil {
		t.Fatal(err)
	}

	req := validAssetRequest(f)
	req.Name = "MedCore HIS Production"

	updated, err := svc.SaveAsset(created.ID, 52, req)
	if err != nil {
		t.Fatal(err)
	}

	if updated.CreatedBy != 41 || updated.UpdatedBy != 52 {
		t.Fatalf(
			"actors = created:%d updated:%d, want 41/52",
			updated.CreatedBy,
			updated.UpdatedBy,
		)
	}
}

func TestSaveAssetRejectsInvalidBusinessInputs(t *testing.T) {
	f := newAssetFixture(t)
	svc := NewService(f.db)

	tests := []struct {
		name   string
		mutate func(*AssetRequest)
	}{
		{
			name: "missing code",
			mutate: func(r *AssetRequest) {
				r.Code = " "
			},
		},
		{
			name: "missing name",
			mutate: func(r *AssetRequest) {
				r.Name = " "
			},
		},
		{
			name: "invalid type",
			mutate: func(r *AssetRequest) {
				r.Type = "UNKNOWN"
			},
		},
		{
			name: "invalid criticality",
			mutate: func(r *AssetRequest) {
				r.Criticality = "EXTREME"
			},
		},
		{
			name: "invalid status",
			mutate: func(r *AssetRequest) {
				r.Status = "DELETED"
			},
		},
		{
			name: "unknown service",
			mutate: func(r *AssetRequest) {
				id := uint(999999)
				r.OwningServiceID = &id
			},
		},
		{
			name: "inactive service",
			mutate: func(r *AssetRequest) {
				r.OwningServiceID = &f.inactiveService.ID
			},
		},
		{
			name: "unknown owner",
			mutate: func(r *AssetRequest) {
				id := uint(999999)
				r.OwnerProfileID = &id
			},
		},
		{
			name: "inactive owner",
			mutate: func(r *AssetRequest) {
				r.OwnerProfileID = &f.inactiveProfile.ID
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := validAssetRequest(f)
			tt.mutate(&req)

			if _, err := svc.SaveAsset(0, 41, req); err == nil {
				t.Fatal("invalid asset accepted")
			}
		})
	}
}

func TestSaveAssetRejectsDuplicateCode(t *testing.T) {
	f := newAssetFixture(t)
	svc := NewService(f.db)

	if _, err := svc.SaveAsset(0, 41, validAssetRequest(f)); err != nil {
		t.Fatal(err)
	}

	req := validAssetRequest(f)
	req.Name = "Duplicate"

	if _, err := svc.SaveAsset(0, 42, req); err == nil {
		t.Fatal("duplicate asset code accepted")
	}
}

func TestListAssetsFiltersSearchPaginationAndRelations(t *testing.T) {
	f := newAssetFixture(t)
	svc := NewService(f.db)

	first, err := svc.SaveAsset(0, 41, validAssetRequest(f))
	if err != nil {
		t.Fatal(err)
	}

	secondReq := validAssetRequest(f)
	secondReq.Code = "srv-pacs"
	secondReq.Name = "Serveur PACS"
	secondReq.Description = "Infrastructure imagerie"
	secondReq.Type = "server"
	secondReq.Criticality = "high"
	secondReq.Status = "inactive"

	second, err := svc.SaveAsset(0, 41, secondReq)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("defaults and relations", func(t *testing.T) {
		page, err := svc.ListAssets(AssetFilter{})
		if err != nil {
			t.Fatal(err)
		}

		if page.Page != 1 || page.Limit != 20 {
			t.Fatalf("pagination = %d/%d, want 1/20", page.Page, page.Limit)
		}
		if page.Total != 2 || page.TotalPages != 1 || len(page.Items) != 2 {
			t.Fatalf(
				"page = total:%d pages:%d items:%d, want 2/1/2",
				page.Total,
				page.TotalPages,
				len(page.Items),
			)
		}

		for _, asset := range page.Items {
			if asset.OwningService == nil {
				t.Fatalf("asset %d missing owning service preload", asset.ID)
			}
			if asset.OwnerProfile == nil {
				t.Fatalf("asset %d missing owner profile preload", asset.ID)
			}
		}
	})

	t.Run("search", func(t *testing.T) {
		page, err := svc.ListAssets(AssetFilter{Search: "pacs"})
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != second.ID {
			t.Fatalf("unexpected search result: %#v", page)
		}
	})

	t.Run("type criticality and status", func(t *testing.T) {
		page, err := svc.ListAssets(AssetFilter{
			Type:        " server ",
			Criticality: " high ",
			Status:      " inactive ",
		})
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != second.ID {
			t.Fatalf("unexpected enum-filter result: %#v", page)
		}
	})

	t.Run("service and owner", func(t *testing.T) {
		page, err := svc.ListAssets(AssetFilter{
			ServiceID:      &f.service.ID,
			OwnerProfileID: &f.profile.ID,
		})
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != 2 || len(page.Items) != 2 {
			t.Fatalf("unexpected relation-filter result: %#v", page)
		}
	})

	t.Run("pagination", func(t *testing.T) {
		page, err := svc.ListAssets(AssetFilter{
			Page:  2,
			Limit: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		if page.Page != 2 ||
			page.Limit != 1 ||
			page.Total != 2 ||
			page.TotalPages != 2 ||
			len(page.Items) != 1 {
			t.Fatalf("unexpected paginated result: %#v", page)
		}

		if page.Items[0].ID != second.ID && page.Items[0].ID != first.ID {
			t.Fatalf("unexpected asset in page: %#v", page.Items[0])
		}
	})
}

func TestListAssetsRejectsInvalidEnumFilters(t *testing.T) {
	f := newAssetFixture(t)
	svc := NewService(f.db)

	tests := []AssetFilter{
		{Type: "UNKNOWN"},
		{Criticality: "EXTREME"},
		{Status: "DELETED"},
	}

	for _, filter := range tests {
		if _, err := svc.ListAssets(filter); err == nil {
			t.Fatalf("invalid filter accepted: %#v", filter)
		}
	}
}
