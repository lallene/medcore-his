package grc

import (
	"net/http"
	"strings"
	"testing"
)

// GRC-06: control and implementation validation, relation rules on update,
// registry filters and counts, and the read-only relations of a control.

func TestValidateControlRequests(t *testing.T) {
	f := newAssetFixture(t)
	zero := uint(0)

	control := validControlRequest(f)
	normalizeControlRequest(&control)
	if err := validateControlRequest(control); err != nil {
		t.Fatalf("valid control rejected: %v", err)
	}
	if control.Code != "CTL-001" || control.Name != "Sauvegardes chiffrées" || control.Category != "Continuité" || control.Status != ControlStatusActive {
		t.Fatalf("unexpected normalization: %+v", control)
	}
	atLimit := control
	atLimit.Code = strings.Repeat("É", controlCodeMaxLength)
	atLimit.Name = strings.Repeat("é", controlNameMaxLength)
	atLimit.Category = strings.Repeat("c", controlCategoryMaxLength)
	if err := validateControlRequest(atLimit); err != nil {
		t.Fatalf("limits rejected: %v", err)
	}
	for name, mutate := range map[string]func(*ControlRequest){
		"whitespace code":   func(r *ControlRequest) { r.Code = "  " },
		"whitespace name":   func(r *ControlRequest) { r.Name = "\t" },
		"code too long":     func(r *ControlRequest) { r.Code = strings.Repeat("É", controlCodeMaxLength+1) },
		"name too long":     func(r *ControlRequest) { r.Name = strings.Repeat("é", controlNameMaxLength+1) },
		"category too long": func(r *ControlRequest) { r.Category = strings.Repeat("c", controlCategoryMaxLength+1) },
		"invalid status":    func(r *ControlRequest) { r.Status = "ARCHIVED" },
		"zero owner":        func(r *ControlRequest) { r.OwnerProfileID = &zero },
		"zero service":      func(r *ControlRequest) { r.OwningServiceID = &zero },
	} {
		r := control
		mutate(&r)
		normalizeControlRequest(&r)
		if err := validateControlRequest(r); err == nil {
			t.Fatalf("%s accepted", name)
		}
		requireServiceError(t, validateControlRequest(r), http.StatusBadRequest)
	}

	implementation := ControlImplementationRequest{ControlID: 1, AssetID: 2, Status: " partial ", ImplementationDetails: "  Détails  "}
	normalizeControlImplementationRequest(&implementation)
	if err := validateControlImplementationRequest(implementation); err != nil || implementation.Status != ControlImplementationStatusPartial || implementation.ImplementationDetails != "Détails" {
		t.Fatalf("valid implementation: %v %+v", err, implementation)
	}
	for name, mutate := range map[string]func(*ControlImplementationRequest){
		"no control":     func(r *ControlImplementationRequest) { r.ControlID = 0 },
		"no asset":       func(r *ControlImplementationRequest) { r.AssetID = 0 },
		"invalid status": func(r *ControlImplementationRequest) { r.Status = "DONE" },
		"zero owner":     func(r *ControlImplementationRequest) { r.OwnerProfileID = &zero },
		"zero service":   func(r *ControlImplementationRequest) { r.OwningServiceID = &zero },
	} {
		r := implementation
		mutate(&r)
		requireServiceError(t, validateControlImplementationRequest(r), http.StatusBadRequest)
		if validateControlImplementationRequest(r) == nil {
			t.Fatalf("%s accepted", name)
		}
	}
}

func TestSaveControlAndImplementationKeepHistoricalRelationsEditable(t *testing.T) {
	f, svc := newComplianceServiceFixture(t)
	asset := createComplianceAsset(t, svc, f, "AST-CTL", AssetStatusActive)
	other := createComplianceAsset(t, svc, f, "AST-CTL-2", AssetStatusActive)

	r := validControlRequest(f)
	control, err := svc.SaveControl(0, 1, r)
	if err != nil {
		t.Fatal(err)
	}
	ir := ControlImplementationRequest{ControlID: control.ID, AssetID: asset.ID, Status: "planned", OwnerProfileID: &f.profile.ID, OwningServiceID: &f.service.ID}
	implementation, err := svc.SaveControlImplementation(0, 1, ir)
	if err != nil {
		t.Fatal(err)
	}

	// Owner and service deactivated after assignment.
	if err := f.db.Model(&f.service).Update("active", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Model(&f.profile).Update("active", false).Error; err != nil {
		t.Fatal(err)
	}
	r.Status = ControlStatusInactive
	updated, err := svc.SaveControl(control.ID, 2, r)
	if err != nil || updated.Status != ControlStatusInactive || updated.CreatedBy != 1 || updated.UpdatedBy != 2 {
		t.Fatalf("control update with historical relations: %v %+v", err, updated)
	}
	// Existing rule kept: an inactive control is not assignable, but the
	// implementation that already uses it stays editable.
	ir.Status = "implemented"
	editedImpl, err := svc.SaveControlImplementation(implementation.ID, 2, ir)
	if err != nil || editedImpl.Status != ControlImplementationStatusImplemented {
		t.Fatalf("implementation update: %v %+v", err, editedImpl)
	}
	_, err = svc.SaveControlImplementation(0, 1, ControlImplementationRequest{ControlID: control.ID, AssetID: other.ID, Status: "PLANNED"})
	requireServiceError(t, err, http.StatusConflict)

	// Newly assigned inactive relations are refused; unknown ones are 404.
	r.OwnerProfileID = &f.inactiveProfile.ID
	_, err = svc.SaveControl(control.ID, 2, r)
	requireServiceError(t, err, http.StatusConflict)
	unknown := uint(999999)
	r.OwnerProfileID = &unknown
	_, err = svc.SaveControl(control.ID, 2, r)
	requireServiceError(t, err, http.StatusNotFound)
	ir.OwningServiceID = &f.inactiveService.ID
	_, err = svc.SaveControlImplementation(implementation.ID, 2, ir)
	requireServiceError(t, err, http.StatusConflict)

	_, err = svc.SaveControl(999999, 2, validControlRequest(f))
	requireServiceError(t, err, http.StatusNotFound)
	_, err = svc.FindControlImplementation(999999)
	requireServiceError(t, err, http.StatusNotFound)
}

func TestListControlsRegistry(t *testing.T) {
	f, svc := newComplianceServiceFixture(t)
	reg := seedComplianceRegistry(t, f, svc)
	asset := createComplianceAsset(t, svc, f, "AST-REG", AssetStatusActive)
	other := createComplianceAsset(t, svc, f, "AST-REG-2", AssetStatusActive)
	risk, err := svc.SaveRisk(0, 1, validRiskRequest(f, asset.ID))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.LinkControlRisk(1, ControlRiskRequest{ControlID: reg.c1.ID, RiskID: risk.ID}); err != nil {
		t.Fatal(err)
	}
	for _, a := range []Asset{asset, other} {
		if _, err := svc.SaveControlImplementation(0, 1, ControlImplementationRequest{ControlID: reg.c1.ID, AssetID: a.ID, Status: "PLANNED"}); err != nil {
			t.Fatal(err)
		}
	}
	draft := validControlRequest(f)
	draft.Code, draft.Name, draft.Category, draft.Status = "CTL-JOURNAL", "Journalisation", "Traçabilité", "draft"
	if _, err := svc.SaveControl(0, 1, draft); err != nil {
		t.Fatal(err)
	}

	list := func(filter ControlFilter) *ControlPage {
		t.Helper()
		page, err := svc.ListControls(filter)
		if err != nil {
			t.Fatal(err)
		}
		return page
	}
	codes := func(page *ControlPage) string {
		out := []string{}
		for _, c := range page.Items {
			out = append(out, c.Code)
		}
		return strings.Join(out, ",")
	}
	same := func(name string, page *ControlPage, want string) {
		t.Helper()
		if got := codes(page); got != want {
			t.Fatalf("%s = %s, want %s", name, got, want)
		}
	}

	all := list(ControlFilter{})
	same("all", all, "CTL-JOURNAL,CTL-MFA,CTL-SAUV")
	counts := map[string][3]int64{}
	for _, c := range all.Items {
		counts[c.Code] = [3]int64{c.RequirementCount, c.RiskCount, c.ImplementationCount}
	}
	if counts["CTL-SAUV"] != [3]int64{3, 1, 2} || counts["CTL-MFA"] != [3]int64{2, 0, 0} || counts["CTL-JOURNAL"] != [3]int64{0, 0, 0} {
		t.Fatalf("relation counts: %v", counts)
	}
	detail, err := svc.FindControl(reg.c1.ID)
	if err != nil || detail.RequirementCount != 3 || detail.RiskCount != 1 || detail.ImplementationCount != 2 {
		t.Fatalf("detail counts: %v %+v", err, detail)
	}

	// The search is parenthesized: it never widens status/relation filters.
	same("search", list(ControlFilter{Search: "ctl-"}), "CTL-JOURNAL,CTL-MFA,CTL-SAUV")
	same("search + status", list(ControlFilter{Search: "ctl-", Status: "active"}), "CTL-MFA,CTL-SAUV")
	same("search + requirement", list(ControlFilter{Search: "ctl-", RequirementID: &reg.r4.ID}), "CTL-MFA")
	same("search + risk", list(ControlFilter{Search: "ctl-", RiskID: &risk.ID}), "CTL-SAUV")
	same("category", list(ControlFilter{Category: "Traçabilité"}), "CTL-JOURNAL")
	page := list(ControlFilter{Page: 2, Limit: 2})
	same("page 2", page, "CTL-SAUV")
	if page.Total != 3 || page.TotalPages != 2 {
		t.Fatalf("page meta: %+v", page)
	}
	empty := list(ControlFilter{RequirementID: &reg.r2.ID})
	if empty.Items == nil || empty.Total != 0 || empty.TotalPages != 0 {
		t.Fatalf("empty list: %+v", empty)
	}

	// Reverse relations of a control, through the existing tables only.
	risks, err := svc.ListRisks(RiskFilter{ControlID: &reg.c1.ID})
	if err != nil || risks.Total != 1 || risks.Items[0].ID != risk.ID {
		t.Fatalf("risks of the control: %v %+v", err, risks)
	}
	none, err := svc.ListRisks(RiskFilter{ControlID: &reg.c2.ID})
	if err != nil || none.Total != 0 {
		t.Fatalf("risks of an unlinked control: %v %+v", err, none)
	}
	requirements, err := svc.ListRequirements(RequirementFilter{ControlID: &reg.c1.ID})
	if err != nil || requirements.Total != 3 {
		t.Fatalf("requirements of the control: %v %+v", err, requirements)
	}
	implementations, err := svc.ListControlImplementations(ControlImplementationFilter{ControlID: &reg.c1.ID})
	if err != nil || implementations.Total != 2 || implementations.Items[0].Asset == nil {
		t.Fatalf("implementations of the control: %v %+v", err, implementations)
	}
}

func TestImplementationAssessmentAndEvidenceRelations(t *testing.T) {
	f := newGRC01DServiceFixture(t)

	evidence := f.create(t, f.request("EVD-IMPL-06"))
	f.create(t, func() EvidenceRequest {
		r := f.request("EVD-OTHER-06")
		r.ControlImplementationID = f.other.ID
		return r
	}())
	assessment := f.completedAssessment(t, "ASM-IMPL-06")

	evidences, err := f.svc.ListEvidence(EvidenceFilter{ControlImplementationID: &f.implementation.ID})
	if err != nil || evidences.Total != 1 || evidences.Items[0].ID != evidence.ID {
		t.Fatalf("evidence of the implementation: %v %+v", err, evidences)
	}
	assessments, err := f.svc.ListAssessments(AssessmentFilter{ControlImplementationID: &f.implementation.ID})
	if err != nil || assessments.Total != 1 || assessments.Items[0].ID != assessment.ID {
		t.Fatalf("assessments of the implementation: %v %+v", err, assessments)
	}
	none, err := f.svc.ListAssessments(AssessmentFilter{ControlImplementationID: &f.other.ID})
	if err != nil || none.Total != 0 {
		t.Fatalf("assessments of another implementation: %v %+v", err, none)
	}
}
