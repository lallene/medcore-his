package grc

import "testing"

func validRiskRequest(f assetFixture, assetID uint) RiskRequest {
	return RiskRequest{
		Reference:          " risk-001 ",
		Title:              " Indisponibilité MedCore ",
		Description:        " Indisponibilité du SI hospitalier ",
		AssetID:            &assetID,
		OwningServiceID:    &f.service.ID,
		OwnerProfileID:     &f.profile.ID,
		ApproverProfileID:  &f.profile.ID,
		Threat:             " Panne infrastructure ",
		Vulnerability:      " Absence de redondance ",
		Likelihood:         4,
		Impact:             5,
		ResidualLikelihood: 2,
		ResidualImpact:     3,
		Treatment:          " reduce ",
		Status:             " open ",
	}
}

func createRiskAsset(t *testing.T, f assetFixture) Asset {
	t.Helper()

	svc := NewService(f.db)
	asset, err := svc.SaveAsset(0, 10, validAssetRequest(f))
	if err != nil {
		t.Fatal(err)
	}

	return *asset
}

func TestSaveRiskCreatesNormalizedRiskScoresAndAuditActors(t *testing.T) {
	f := newAssetFixture(t)
	svc := NewService(f.db)
	asset := createRiskAsset(t, f)

	created, err := svc.SaveRisk(0, 41, validRiskRequest(f, asset.ID))
	if err != nil {
		t.Fatal(err)
	}

	if created.Reference != "RISK-001" ||
		created.Title != "Indisponibilité MedCore" ||
		created.Description != "Indisponibilité du SI hospitalier" ||
		created.Threat != "Panne infrastructure" ||
		created.Vulnerability != "Absence de redondance" ||
		created.Treatment != RiskTreatmentReduce ||
		created.Status != RiskStatusOpen {
		t.Fatalf("unexpected normalized risk: %#v", created)
	}

	if created.InherentScore != 20 {
		t.Fatalf("inherent score = %d, want 20", created.InherentScore)
	}
	if created.ResidualScore != 6 {
		t.Fatalf("residual score = %d, want 6", created.ResidualScore)
	}

	if created.CreatedBy != 41 || created.UpdatedBy != 41 {
		t.Fatalf(
			"actors = created:%d updated:%d, want 41/41",
			created.CreatedBy,
			created.UpdatedBy,
		)
	}

	if created.Asset == nil ||
		created.OwningService == nil ||
		created.OwnerProfile == nil ||
		created.ApproverProfile == nil {
		t.Fatalf("risk relations not preloaded: %#v", created)
	}
}

func TestSaveRiskUpdatePreservesCreatedByAndRecalculatesScores(t *testing.T) {
	f := newAssetFixture(t)
	svc := NewService(f.db)
	asset := createRiskAsset(t, f)

	created, err := svc.SaveRisk(0, 41, validRiskRequest(f, asset.ID))
	if err != nil {
		t.Fatal(err)
	}

	req := validRiskRequest(f, asset.ID)
	req.Title = "Risque MedCore réévalué"
	req.Likelihood = 3
	req.Impact = 4
	req.ResidualLikelihood = 1
	req.ResidualImpact = 2

	updated, err := svc.SaveRisk(created.ID, 52, req)
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

	if updated.InherentScore != 12 || updated.ResidualScore != 2 {
		t.Fatalf(
			"scores = inherent:%d residual:%d, want 12/2",
			updated.InherentScore,
			updated.ResidualScore,
		)
	}
}

func TestSaveRiskRejectsInvalidIntrinsicInputs(t *testing.T) {
	f := newAssetFixture(t)
	svc := NewService(f.db)
	asset := createRiskAsset(t, f)

	tests := []struct {
		name   string
		mutate func(*RiskRequest)
	}{
		{
			name: "missing reference",
			mutate: func(r *RiskRequest) {
				r.Reference = " "
			},
		},
		{
			name: "missing title",
			mutate: func(r *RiskRequest) {
				r.Title = " "
			},
		},
		{
			name: "likelihood below scale",
			mutate: func(r *RiskRequest) {
				r.Likelihood = 0
			},
		},
		{
			name: "likelihood above scale",
			mutate: func(r *RiskRequest) {
				r.Likelihood = 6
			},
		},
		{
			name: "impact below scale",
			mutate: func(r *RiskRequest) {
				r.Impact = 0
			},
		},
		{
			name: "impact above scale",
			mutate: func(r *RiskRequest) {
				r.Impact = 6
			},
		},
		{
			name: "residual likelihood below scale",
			mutate: func(r *RiskRequest) {
				r.ResidualLikelihood = 0
			},
		},
		{
			name: "residual likelihood above scale",
			mutate: func(r *RiskRequest) {
				r.ResidualLikelihood = 6
			},
		},
		{
			name: "residual impact below scale",
			mutate: func(r *RiskRequest) {
				r.ResidualImpact = 0
			},
		},
		{
			name: "residual impact above scale",
			mutate: func(r *RiskRequest) {
				r.ResidualImpact = 6
			},
		},
		{
			name: "invalid treatment",
			mutate: func(r *RiskRequest) {
				r.Treatment = "IGNORE"
			},
		},
		{
			name: "missing treatment",
			mutate: func(r *RiskRequest) {
				r.Treatment = " "
			},
		},
		{
			name: "invalid status",
			mutate: func(r *RiskRequest) {
				r.Status = "DELETED"
			},
		},
		{
			name: "missing status",
			mutate: func(r *RiskRequest) {
				r.Status = " "
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := validRiskRequest(f, asset.ID)
			tt.mutate(&req)

			if _, err := svc.SaveRisk(0, 41, req); err == nil {
				t.Fatal("invalid risk accepted")
			}
		})
	}
}

func TestSaveRiskRejectsInvalidRelations(t *testing.T) {
	f := newAssetFixture(t)
	svc := NewService(f.db)

	activeAsset := createRiskAsset(t, f)

	inactiveAssetReq := validAssetRequest(f)
	inactiveAssetReq.Code = "APP-RETIRED"
	inactiveAssetReq.Name = "Ancienne application"
	inactiveAssetReq.Status = "inactive"

	inactiveAsset, err := svc.SaveAsset(0, 10, inactiveAssetReq)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		mutate func(*RiskRequest)
	}{
		{
			name: "unknown asset",
			mutate: func(r *RiskRequest) {
				id := uint(999999)
				r.AssetID = &id
			},
		},
		{
			name: "inactive asset",
			mutate: func(r *RiskRequest) {
				r.AssetID = &inactiveAsset.ID
			},
		},
		{
			name: "unknown service",
			mutate: func(r *RiskRequest) {
				id := uint(999999)
				r.OwningServiceID = &id
			},
		},
		{
			name: "inactive service",
			mutate: func(r *RiskRequest) {
				r.OwningServiceID = &f.inactiveService.ID
			},
		},
		{
			name: "unknown owner",
			mutate: func(r *RiskRequest) {
				id := uint(999999)
				r.OwnerProfileID = &id
			},
		},
		{
			name: "inactive owner",
			mutate: func(r *RiskRequest) {
				r.OwnerProfileID = &f.inactiveProfile.ID
			},
		},
		{
			name: "unknown approver",
			mutate: func(r *RiskRequest) {
				id := uint(999999)
				r.ApproverProfileID = &id
			},
		},
		{
			name: "inactive approver",
			mutate: func(r *RiskRequest) {
				r.ApproverProfileID = &f.inactiveProfile.ID
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := validRiskRequest(f, activeAsset.ID)
			tt.mutate(&req)

			if _, err := svc.SaveRisk(0, 41, req); err == nil {
				t.Fatal("risk with invalid relation accepted")
			}
		})
	}
}

func TestSaveRiskRejectsDuplicateReference(t *testing.T) {
	f := newAssetFixture(t)
	svc := NewService(f.db)
	asset := createRiskAsset(t, f)

	first := validRiskRequest(f, asset.ID)
	first.Reference = "RISK-DUPLICATE"

	if _, err := svc.SaveRisk(0, 41, first); err != nil {
		t.Fatal(err)
	}

	second := validRiskRequest(f, asset.ID)
	second.Reference = " risk-duplicate "
	second.Title = "Autre risque"

	if _, err := svc.SaveRisk(0, 52, second); err == nil {
		t.Fatal("duplicate risk reference accepted")
	}
}

func TestListRisksFiltersSearchPaginationAndRelations(t *testing.T) {
	f := newAssetFixture(t)
	svc := NewService(f.db)
	asset := createRiskAsset(t, f)

	firstReq := validRiskRequest(f, asset.ID)
	firstReq.Reference = "RISK-001"
	firstReq.Title = "Indisponibilité MedCore"

	first, err := svc.SaveRisk(0, 41, firstReq)
	if err != nil {
		t.Fatal(err)
	}

	secondReq := validRiskRequest(f, asset.ID)
	secondReq.Reference = "RISK-PACS"
	secondReq.Title = "Compromission PACS"
	secondReq.Description = "Risque de compromission de l'imagerie"
	secondReq.Threat = "Accès non autorisé"
	secondReq.Vulnerability = "Contrôle d'accès insuffisant"
	secondReq.Treatment = "transfer"
	secondReq.Status = "in_treatment"

	second, err := svc.SaveRisk(0, 41, secondReq)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("defaults and relations", func(t *testing.T) {
		page, err := svc.ListRisks(RiskFilter{})
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

		for _, risk := range page.Items {
			if risk.Asset == nil {
				t.Fatalf("risk %d missing asset preload", risk.ID)
			}
			if risk.OwningService == nil {
				t.Fatalf("risk %d missing owning service preload", risk.ID)
			}
			if risk.OwnerProfile == nil {
				t.Fatalf("risk %d missing owner profile preload", risk.ID)
			}
			if risk.ApproverProfile == nil {
				t.Fatalf("risk %d missing approver profile preload", risk.ID)
			}
		}
	})

	t.Run("search", func(t *testing.T) {
		page, err := svc.ListRisks(RiskFilter{Search: "pacs"})
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != second.ID {
			t.Fatalf("unexpected search result: %#v", page)
		}
	})

	t.Run("status and treatment", func(t *testing.T) {
		page, err := svc.ListRisks(RiskFilter{
			Status:    " in_treatment ",
			Treatment: " transfer ",
		})
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != second.ID {
			t.Fatalf("unexpected enum-filter result: %#v", page)
		}
	})

	t.Run("relations", func(t *testing.T) {
		page, err := svc.ListRisks(RiskFilter{
			AssetID:           &asset.ID,
			ServiceID:         &f.service.ID,
			OwnerProfileID:    &f.profile.ID,
			ApproverProfileID: &f.profile.ID,
		})
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != 2 || len(page.Items) != 2 {
			t.Fatalf("unexpected relation-filter result: %#v", page)
		}
	})

	t.Run("pagination", func(t *testing.T) {
		page, err := svc.ListRisks(RiskFilter{
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

		if page.Items[0].ID != first.ID && page.Items[0].ID != second.ID {
			t.Fatalf("unexpected risk in page: %#v", page.Items[0])
		}
	})
}

func TestListRisksRejectsInvalidEnumFilters(t *testing.T) {
	f := newAssetFixture(t)
	svc := NewService(f.db)

	tests := []RiskFilter{
		{Status: "DELETED"},
		{Treatment: "IGNORE"},
	}

	for _, filter := range tests {
		if _, err := svc.ListRisks(filter); err == nil {
			t.Fatalf("invalid filter accepted: %#v", filter)
		}
	}
}
