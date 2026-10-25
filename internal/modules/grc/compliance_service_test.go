package grc

import (
	"errors"
	"testing"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
)

func newComplianceServiceFixture(t *testing.T) (assetFixture, *GRCService) {
	t.Helper()

	f := newAssetFixture(t)
	if err := f.db.AutoMigrate(
		&Framework{},
		&Requirement{},
		&Control{},
		&RequirementControl{},
		&ControlRisk{},
		&ControlImplementation{},
	); err != nil {
		t.Fatal(err)
	}

	return f, NewService(f.db)
}

func expectAppStatus(t *testing.T, err error, status int) {
	t.Helper()

	if err == nil {
		t.Fatalf("expected error with status %d, got nil", status)
	}

	var appErr *coreerrors.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected AppError with status %d, got %v", status, err)
	}
	if appErr.Status != status {
		t.Fatalf("status = %d (%s), want %d", appErr.Status, appErr.Message, status)
	}
}

func validFrameworkRequest() FrameworkRequest {
	return FrameworkRequest{
		Code:        " fw-test ",
		Name:        " Référentiel de test ",
		Version:     " 1.0 ",
		Description: " Description ",
		Source:      " interne ",
		Status:      " draft ",
	}
}

func createFramework(t *testing.T, svc *GRCService, code, version string) Framework {
	t.Helper()

	r := validFrameworkRequest()
	r.Code = code
	r.Version = version
	framework, err := svc.SaveFramework(0, 10, r)
	if err != nil {
		t.Fatal(err)
	}

	return *framework
}

func createRequirement(t *testing.T, svc *GRCService, frameworkID uint, code string, parentID *uint) Requirement {
	t.Helper()

	requirement, err := svc.SaveRequirement(0, 10, RequirementRequest{
		FrameworkID:         frameworkID,
		Code:                code,
		Title:               "Exigence " + code,
		ParentRequirementID: parentID,
		Status:              RequirementStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}

	return *requirement
}

func validControlRequest(f assetFixture) ControlRequest {
	return ControlRequest{
		Code:            " ctl-001 ",
		Name:            " Sauvegardes chiffrées ",
		Description:     " Sauvegarde quotidienne ",
		Category:        " Continuité ",
		OwnerProfileID:  &f.profile.ID,
		OwningServiceID: &f.service.ID,
		Status:          " active ",
	}
}

func createControl(t *testing.T, svc *GRCService, f assetFixture, code, status string) Control {
	t.Helper()

	r := validControlRequest(f)
	r.Code = code
	r.Status = status
	control, err := svc.SaveControl(0, 10, r)
	if err != nil {
		t.Fatal(err)
	}

	return *control
}

func createComplianceAsset(t *testing.T, svc *GRCService, f assetFixture, code, status string) Asset {
	t.Helper()

	r := validAssetRequest(f)
	r.Code = code
	r.Status = status
	asset, err := svc.SaveAsset(0, 10, r)
	if err != nil {
		t.Fatal(err)
	}

	return *asset
}

func TestSaveFrameworkCreatesNormalizedFramework(t *testing.T) {
	_, svc := newComplianceServiceFixture(t)

	created, err := svc.SaveFramework(0, 21, validFrameworkRequest())
	if err != nil {
		t.Fatal(err)
	}

	if created.Code != "FW-TEST" ||
		created.Name != "Référentiel de test" ||
		created.Version != "1.0" ||
		created.Description != "Description" ||
		created.Source != "interne" ||
		created.Status != FrameworkStatusDraft ||
		created.CreatedBy != 21 ||
		created.UpdatedBy != 21 {
		t.Fatalf("unexpected framework: %+v", created)
	}

	r := validFrameworkRequest()
	r.Status = "active"
	updated, err := svc.SaveFramework(created.ID, 22, r)
	if err != nil {
		t.Fatal(err)
	}
	if updated.CreatedBy != 21 || updated.UpdatedBy != 22 || updated.Status != FrameworkStatusActive {
		t.Fatalf("unexpected updated framework: %+v", updated)
	}
}

func TestSaveFrameworkRejectsInvalidInputs(t *testing.T) {
	_, svc := newComplianceServiceFixture(t)

	tests := map[string]func(*FrameworkRequest){
		"missing code":    func(r *FrameworkRequest) { r.Code = "  " },
		"missing name":    func(r *FrameworkRequest) { r.Name = "" },
		"missing version": func(r *FrameworkRequest) { r.Version = " " },
		"invalid status":  func(r *FrameworkRequest) { r.Status = "COMPLIANT" },
	}

	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			r := validFrameworkRequest()
			mutate(&r)

			_, err := svc.SaveFramework(0, 10, r)
			expectAppStatus(t, err, 400)
		})
	}

	_, err := svc.SaveFramework(999999, 10, validFrameworkRequest())
	expectAppStatus(t, err, 404)
}

func TestSaveFrameworkRejectsDuplicateCodeVersion(t *testing.T) {
	_, svc := newComplianceServiceFixture(t)

	createFramework(t, svc, "FW-DUP", "1.0")
	createFramework(t, svc, "FW-DUP", "2.0")

	r := validFrameworkRequest()
	r.Code = " fw-dup "
	r.Version = "1.0"
	_, err := svc.SaveFramework(0, 10, r)
	expectAppStatus(t, err, 409)
}

func TestListFrameworksFiltersSearchAndPagination(t *testing.T) {
	_, svc := newComplianceServiceFixture(t)

	createFramework(t, svc, "FW-B", "1.0")
	createFramework(t, svc, "FW-A", "2.0")
	createFramework(t, svc, "FW-A", "1.0")

	page, err := svc.ListFrameworks(FrameworkFilter{Code: " fw-a ", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || page.TotalPages != 2 || len(page.Items) != 1 || page.Items[0].Version != "1.0" {
		t.Fatalf("unexpected framework page: %+v", page)
	}

	page, err = svc.ListFrameworks(FrameworkFilter{Search: "2.0"})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.Items[0].Code != "FW-A" {
		t.Fatalf("unexpected search result: %+v", page)
	}

	_, err = svc.ListFrameworks(FrameworkFilter{Status: "PUBLISHED"})
	expectAppStatus(t, err, 400)
}

func TestSaveRequirementCreatesWithSameFrameworkParent(t *testing.T) {
	_, svc := newComplianceServiceFixture(t)

	framework := createFramework(t, svc, "FW-REQ", "1.0")
	parent := createRequirement(t, svc, framework.ID, "REQ-1", nil)

	child, err := svc.SaveRequirement(0, 31, RequirementRequest{
		FrameworkID:         framework.ID,
		Code:                " req-1.1 ",
		Title:               " Sous-exigence ",
		Description:         " Détail ",
		ParentRequirementID: &parent.ID,
		Status:              " active ",
	})
	if err != nil {
		t.Fatal(err)
	}

	if child.Code != "REQ-1.1" ||
		child.Title != "Sous-exigence" ||
		child.Status != RequirementStatusActive ||
		child.CreatedBy != 31 ||
		child.Framework == nil ||
		child.Framework.ID != framework.ID ||
		child.ParentRequirement == nil ||
		child.ParentRequirement.ID != parent.ID {
		t.Fatalf("unexpected requirement: %+v", child)
	}

	page, err := svc.ListRequirements(RequirementFilter{ParentRequirementID: &parent.ID})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.Items[0].ID != child.ID {
		t.Fatalf("unexpected requirement page: %+v", page)
	}
}

func TestSaveRequirementRejectsInvalidInputs(t *testing.T) {
	_, svc := newComplianceServiceFixture(t)

	framework := createFramework(t, svc, "FW-REQ", "1.0")
	valid := func() RequirementRequest {
		return RequirementRequest{FrameworkID: framework.ID, Code: "REQ-X", Title: "Titre", Status: "ACTIVE"}
	}

	badRequests := map[string]func(*RequirementRequest){
		"missing framework": func(r *RequirementRequest) { r.FrameworkID = 0 },
		"missing code":      func(r *RequirementRequest) { r.Code = " " },
		"missing title":     func(r *RequirementRequest) { r.Title = "" },
		"invalid status":    func(r *RequirementRequest) { r.Status = "DRAFT" },
	}
	for name, mutate := range badRequests {
		t.Run(name, func(t *testing.T) {
			r := valid()
			mutate(&r)

			_, err := svc.SaveRequirement(0, 10, r)
			expectAppStatus(t, err, 400)
		})
	}

	t.Run("unknown framework", func(t *testing.T) {
		r := valid()
		r.FrameworkID = 999999

		_, err := svc.SaveRequirement(0, 10, r)
		expectAppStatus(t, err, 404)
	})
}

func TestSaveRequirementRejectsParentFromOtherFramework(t *testing.T) {
	_, svc := newComplianceServiceFixture(t)

	framework := createFramework(t, svc, "FW-ONE", "1.0")
	other := createFramework(t, svc, "FW-TWO", "1.0")
	foreignParent := createRequirement(t, svc, other.ID, "REQ-1", nil)

	_, err := svc.SaveRequirement(0, 10, RequirementRequest{
		FrameworkID:         framework.ID,
		Code:                "REQ-1.1",
		Title:               "Sous-exigence",
		ParentRequirementID: &foreignParent.ID,
		Status:              RequirementStatusActive,
	})
	expectAppStatus(t, err, 409)

	unknown := uint(999999)
	_, err = svc.SaveRequirement(0, 10, RequirementRequest{
		FrameworkID:         framework.ID,
		Code:                "REQ-1.2",
		Title:               "Sous-exigence",
		ParentRequirementID: &unknown,
		Status:              RequirementStatusActive,
	})
	expectAppStatus(t, err, 404)
}

func TestSaveRequirementRejectsSelfParentAndCycles(t *testing.T) {
	_, svc := newComplianceServiceFixture(t)

	framework := createFramework(t, svc, "FW-CYCLE", "1.0")
	a := createRequirement(t, svc, framework.ID, "REQ-A", nil)
	b := createRequirement(t, svc, framework.ID, "REQ-B", &a.ID)
	c := createRequirement(t, svc, framework.ID, "REQ-C", &b.ID)

	update := func(parentID uint) error {
		_, err := svc.SaveRequirement(a.ID, 10, RequirementRequest{
			FrameworkID:         framework.ID,
			Code:                a.Code,
			Title:               a.Title,
			ParentRequirementID: &parentID,
			Status:              RequirementStatusActive,
		})
		return err
	}

	expectAppStatus(t, update(a.ID), 409) // A -> A
	expectAppStatus(t, update(b.ID), 409) // A -> B -> A
	expectAppStatus(t, update(c.ID), 409) // A -> C -> B -> A

	reloaded, err := svc.FindRequirement(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.ParentRequirementID != nil {
		t.Fatalf("rejected cycle must not be persisted: %+v", reloaded.ParentRequirementID)
	}

	// A valid re-parenting deeper in the same tree remains possible.
	d := createRequirement(t, svc, framework.ID, "REQ-D", nil)
	if _, err = svc.SaveRequirement(c.ID, 10, RequirementRequest{
		FrameworkID:         framework.ID,
		Code:                c.Code,
		Title:               c.Title,
		ParentRequirementID: &d.ID,
		Status:              RequirementStatusActive,
	}); err != nil {
		t.Fatalf("valid re-parenting rejected: %v", err)
	}
}

func TestSaveRequirementRejectsFrameworkChangeWithChildren(t *testing.T) {
	_, svc := newComplianceServiceFixture(t)

	framework := createFramework(t, svc, "FW-ONE", "1.0")
	other := createFramework(t, svc, "FW-TWO", "1.0")
	parent := createRequirement(t, svc, framework.ID, "REQ-1", nil)
	createRequirement(t, svc, framework.ID, "REQ-1.1", &parent.ID)

	_, err := svc.SaveRequirement(parent.ID, 10, RequirementRequest{
		FrameworkID: other.ID,
		Code:        parent.Code,
		Title:       parent.Title,
		Status:      RequirementStatusActive,
	})
	expectAppStatus(t, err, 409)
}

func TestSaveRequirementRejectsDuplicateFrameworkCode(t *testing.T) {
	_, svc := newComplianceServiceFixture(t)

	framework := createFramework(t, svc, "FW-ONE", "1.0")
	other := createFramework(t, svc, "FW-TWO", "1.0")
	createRequirement(t, svc, framework.ID, "REQ-1", nil)
	createRequirement(t, svc, other.ID, "REQ-1", nil)

	_, err := svc.SaveRequirement(0, 10, RequirementRequest{
		FrameworkID: framework.ID,
		Code:        " req-1 ",
		Title:       "Doublon",
		Status:      RequirementStatusActive,
	})
	expectAppStatus(t, err, 409)
}

func TestSaveControlCreatesNormalizedControl(t *testing.T) {
	f, svc := newComplianceServiceFixture(t)

	created, err := svc.SaveControl(0, 41, validControlRequest(f))
	if err != nil {
		t.Fatal(err)
	}

	if created.Code != "CTL-001" ||
		created.Name != "Sauvegardes chiffrées" ||
		created.Category != "Continuité" ||
		created.Status != ControlStatusActive ||
		created.CreatedBy != 41 ||
		created.UpdatedBy != 41 ||
		created.OwnerProfile == nil ||
		created.OwningService == nil {
		t.Fatalf("unexpected control: %+v", created)
	}

	r := validControlRequest(f)
	r.Status = "retired"
	updated, err := svc.SaveControl(created.ID, 42, r)
	if err != nil {
		t.Fatal(err)
	}
	if updated.CreatedBy != 41 || updated.UpdatedBy != 42 || updated.Status != ControlStatusRetired {
		t.Fatalf("unexpected updated control: %+v", updated)
	}

	page, err := svc.ListControls(ControlFilter{Category: " Continuité ", ServiceID: &f.service.ID})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.Items[0].ID != created.ID {
		t.Fatalf("unexpected control page: %+v", page)
	}
}

func TestSaveControlRejectsInvalidInputs(t *testing.T) {
	f, svc := newComplianceServiceFixture(t)

	tests := []struct {
		name   string
		status int
		mutate func(*ControlRequest)
	}{
		{"missing code", 400, func(r *ControlRequest) { r.Code = " " }},
		{"missing name", 400, func(r *ControlRequest) { r.Name = "" }},
		{"invalid status", 400, func(r *ControlRequest) { r.Status = "ARCHIVED" }},
		{"inactive owner", 409, func(r *ControlRequest) { r.OwnerProfileID = &f.inactiveProfile.ID }},
		{"inactive service", 409, func(r *ControlRequest) { r.OwningServiceID = &f.inactiveService.ID }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := validControlRequest(f)
			tt.mutate(&r)

			_, err := svc.SaveControl(0, 10, r)
			expectAppStatus(t, err, tt.status)
		})
	}
}

func TestSaveControlRejectsDuplicateCode(t *testing.T) {
	f, svc := newComplianceServiceFixture(t)

	createControl(t, svc, f, "CTL-DUP", ControlStatusActive)

	r := validControlRequest(f)
	r.Code = " ctl-dup "
	_, err := svc.SaveControl(0, 10, r)
	expectAppStatus(t, err, 409)
}

func TestSaveControlImplementationCreatesValidAssignment(t *testing.T) {
	f, svc := newComplianceServiceFixture(t)

	control := createControl(t, svc, f, "CTL-1", ControlStatusActive)
	asset := createComplianceAsset(t, svc, f, "AST-1", AssetStatusActive)

	created, err := svc.SaveControlImplementation(0, 51, ControlImplementationRequest{
		ControlID:             control.ID,
		AssetID:               asset.ID,
		OwningServiceID:       &f.service.ID,
		OwnerProfileID:        &f.profile.ID,
		Status:                " planned ",
		ImplementationDetails: " Déploiement prévu ",
	})
	if err != nil {
		t.Fatal(err)
	}

	if created.Status != ControlImplementationStatusPlanned ||
		created.ImplementationDetails != "Déploiement prévu" ||
		created.CreatedBy != 51 ||
		created.UpdatedBy != 51 ||
		created.Control == nil ||
		created.Asset == nil ||
		created.OwningService == nil ||
		created.OwnerProfile == nil {
		t.Fatalf("unexpected implementation: %+v", created)
	}

	page, err := svc.ListControlImplementations(ControlImplementationFilter{AssetID: &asset.ID, Status: "planned"})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.Items[0].ID != created.ID {
		t.Fatalf("unexpected implementation page: %+v", page)
	}
}

func TestSaveControlImplementationRejectsInvalidAssignments(t *testing.T) {
	f, svc := newComplianceServiceFixture(t)

	active := createControl(t, svc, f, "CTL-ACTIVE", ControlStatusActive)
	inactive := createControl(t, svc, f, "CTL-INACTIVE", ControlStatusInactive)
	retired := createControl(t, svc, f, "CTL-RETIRED", ControlStatusRetired)
	asset := createComplianceAsset(t, svc, f, "AST-ACTIVE", AssetStatusActive)
	inactiveAsset := createComplianceAsset(t, svc, f, "AST-INACTIVE", AssetStatusInactive)

	tests := []struct {
		name   string
		status int
		req    ControlImplementationRequest
	}{
		{"missing control", 400, ControlImplementationRequest{AssetID: asset.ID, Status: "PLANNED"}},
		{"missing asset", 400, ControlImplementationRequest{ControlID: active.ID, Status: "PLANNED"}},
		{"invalid status", 400, ControlImplementationRequest{ControlID: active.ID, AssetID: asset.ID, Status: "COMPLIANT"}},
		{"unknown control", 404, ControlImplementationRequest{ControlID: 999999, AssetID: asset.ID, Status: "PLANNED"}},
		{"unknown asset", 404, ControlImplementationRequest{ControlID: active.ID, AssetID: 999999, Status: "PLANNED"}},
		{"inactive control", 409, ControlImplementationRequest{ControlID: inactive.ID, AssetID: asset.ID, Status: "PLANNED"}},
		{"retired control", 409, ControlImplementationRequest{ControlID: retired.ID, AssetID: asset.ID, Status: "PLANNED"}},
		{"inactive asset", 409, ControlImplementationRequest{ControlID: active.ID, AssetID: inactiveAsset.ID, Status: "PLANNED"}},
		{"inactive owner", 409, ControlImplementationRequest{ControlID: active.ID, AssetID: asset.ID, OwnerProfileID: &f.inactiveProfile.ID, Status: "PLANNED"}},
		{"inactive service", 409, ControlImplementationRequest{ControlID: active.ID, AssetID: asset.ID, OwningServiceID: &f.inactiveService.ID, Status: "PLANNED"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.SaveControlImplementation(0, 10, tt.req)
			expectAppStatus(t, err, tt.status)
		})
	}
}

func TestSaveControlImplementationRejectsDuplicateControlAsset(t *testing.T) {
	f, svc := newComplianceServiceFixture(t)

	control := createControl(t, svc, f, "CTL-1", ControlStatusActive)
	asset := createComplianceAsset(t, svc, f, "AST-1", AssetStatusActive)
	req := ControlImplementationRequest{ControlID: control.ID, AssetID: asset.ID, Status: "PLANNED"}

	if _, err := svc.SaveControlImplementation(0, 10, req); err != nil {
		t.Fatal(err)
	}

	req.Status = "IMPLEMENTED"
	_, err := svc.SaveControlImplementation(0, 10, req)
	expectAppStatus(t, err, 409)
}

func TestSaveControlImplementationUpdatePreservesHistoricalAssignment(t *testing.T) {
	f, svc := newComplianceServiceFixture(t)

	control := createControl(t, svc, f, "CTL-1", ControlStatusActive)
	asset := createComplianceAsset(t, svc, f, "AST-1", AssetStatusActive)

	created, err := svc.SaveControlImplementation(0, 61, ControlImplementationRequest{
		ControlID: control.ID,
		AssetID:   asset.ID,
		Status:    ControlImplementationStatusPlanned,
	})
	if err != nil {
		t.Fatal(err)
	}

	assetReq := validAssetRequest(f)
	assetReq.Code = asset.Code
	assetReq.Status = AssetStatusRetired
	if _, err = svc.SaveAsset(asset.ID, 10, assetReq); err != nil {
		t.Fatal(err)
	}
	controlReq := validControlRequest(f)
	controlReq.Code = control.Code
	controlReq.Status = ControlStatusRetired
	if _, err = svc.SaveControl(control.ID, 10, controlReq); err != nil {
		t.Fatal(err)
	}

	updated, err := svc.SaveControlImplementation(created.ID, 62, ControlImplementationRequest{
		ControlID:             control.ID,
		AssetID:               asset.ID,
		Status:                ControlImplementationStatusNotApplicable,
		ImplementationDetails: "Actif retiré",
	})
	if err != nil {
		t.Fatalf("historical implementation update rejected: %v", err)
	}
	if updated.CreatedBy != 61 || updated.UpdatedBy != 62 || updated.Status != ControlImplementationStatusNotApplicable {
		t.Fatalf("unexpected updated implementation: %+v", updated)
	}
}

func TestSaveControlImplementationUpdateRevalidatesChangedRelations(t *testing.T) {
	f, svc := newComplianceServiceFixture(t)

	control := createControl(t, svc, f, "CTL-1", ControlStatusActive)
	inactiveControl := createControl(t, svc, f, "CTL-INACTIVE", ControlStatusInactive)
	asset := createComplianceAsset(t, svc, f, "AST-1", AssetStatusActive)
	inactiveAsset := createComplianceAsset(t, svc, f, "AST-INACTIVE", AssetStatusInactive)

	created, err := svc.SaveControlImplementation(0, 10, ControlImplementationRequest{
		ControlID: control.ID,
		AssetID:   asset.ID,
		Status:    ControlImplementationStatusPlanned,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.SaveControlImplementation(created.ID, 10, ControlImplementationRequest{
		ControlID: control.ID,
		AssetID:   inactiveAsset.ID,
		Status:    ControlImplementationStatusPlanned,
	})
	expectAppStatus(t, err, 409)

	_, err = svc.SaveControlImplementation(created.ID, 10, ControlImplementationRequest{
		ControlID: inactiveControl.ID,
		AssetID:   asset.ID,
		Status:    ControlImplementationStatusPlanned,
	})
	expectAppStatus(t, err, 409)

	reloaded, err := svc.FindControlImplementation(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.ControlID != control.ID || reloaded.AssetID != asset.ID {
		t.Fatalf("rejected relation change must not be persisted: %+v", reloaded)
	}
}

func TestRequirementControlLinkLifecycle(t *testing.T) {
	f, svc := newComplianceServiceFixture(t)

	framework := createFramework(t, svc, "FW-LINK", "1.0")
	requirement := createRequirement(t, svc, framework.ID, "REQ-1", nil)
	control := createControl(t, svc, f, "CTL-1", ControlStatusActive)
	req := RequirementControlRequest{RequirementID: requirement.ID, ControlID: control.ID}

	link, err := svc.LinkRequirementControl(71, req)
	if err != nil {
		t.Fatal(err)
	}
	if link.ID == 0 || link.CreatedBy != 71 {
		t.Fatalf("unexpected link: %+v", link)
	}

	_, err = svc.LinkRequirementControl(71, req)
	expectAppStatus(t, err, 409)

	_, err = svc.LinkRequirementControl(71, RequirementControlRequest{RequirementID: 0, ControlID: control.ID})
	expectAppStatus(t, err, 400)
	_, err = svc.LinkRequirementControl(71, RequirementControlRequest{RequirementID: 999999, ControlID: control.ID})
	expectAppStatus(t, err, 404)
	_, err = svc.LinkRequirementControl(71, RequirementControlRequest{RequirementID: requirement.ID, ControlID: 999999})
	expectAppStatus(t, err, 404)

	if err = svc.UnlinkRequirementControl(71, requirement.ID, control.ID); err != nil {
		t.Fatal(err)
	}
	expectAppStatus(t, svc.UnlinkRequirementControl(71, requirement.ID, control.ID), 404)

	if _, err = svc.FindRequirement(requirement.ID); err != nil {
		t.Fatalf("unlink must keep requirement: %v", err)
	}
	if _, err = svc.FindControl(control.ID); err != nil {
		t.Fatalf("unlink must keep control: %v", err)
	}
}

func TestControlRiskLinkLifecycle(t *testing.T) {
	f, svc := newComplianceServiceFixture(t)

	control := createControl(t, svc, f, "CTL-1", ControlStatusActive)
	asset := createRiskAsset(t, f)
	riskReq := validRiskRequest(f, asset.ID)
	riskReq.Status = RiskStatusClosed
	risk, err := svc.SaveRisk(0, 10, riskReq)
	if err != nil {
		t.Fatal(err)
	}
	req := ControlRiskRequest{ControlID: control.ID, RiskID: risk.ID}

	link, err := svc.LinkControlRisk(81, req)
	if err != nil {
		t.Fatalf("closed risk link rejected: %v", err)
	}
	if link.ID == 0 || link.CreatedBy != 81 {
		t.Fatalf("unexpected link: %+v", link)
	}

	_, err = svc.LinkControlRisk(81, req)
	expectAppStatus(t, err, 409)

	_, err = svc.LinkControlRisk(81, ControlRiskRequest{ControlID: control.ID, RiskID: 0})
	expectAppStatus(t, err, 400)
	_, err = svc.LinkControlRisk(81, ControlRiskRequest{ControlID: 999999, RiskID: risk.ID})
	expectAppStatus(t, err, 404)
	_, err = svc.LinkControlRisk(81, ControlRiskRequest{ControlID: control.ID, RiskID: 999999})
	expectAppStatus(t, err, 404)

	if err = svc.UnlinkControlRisk(81, control.ID, risk.ID); err != nil {
		t.Fatal(err)
	}
	expectAppStatus(t, svc.UnlinkControlRisk(81, control.ID, risk.ID), 404)

	if _, err = svc.FindControl(control.ID); err != nil {
		t.Fatalf("unlink must keep control: %v", err)
	}
	if _, err = svc.FindRisk(risk.ID); err != nil {
		t.Fatalf("unlink must keep risk: %v", err)
	}
}
