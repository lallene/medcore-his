package grc

import (
	"net/http"
	"strings"
	"testing"
)

// GRC-05: control mapping coverage (single definition shared with the
// dashboard), requirement exploration filters and requirement → controls.

type complianceRegistry struct {
	iso, hds, old  Framework
	r1, r2, r3, r4 Requirement
	r5             Requirement
	c1, c2         Control
}

// seedComplianceRegistry: ISO (ACTIVE) has R1 (2 links), R2 (unmapped), R3
// (INACTIVE, mapped) and R4 (1 link); HDS (DRAFT) has R5 (mapped); OLD
// (ARCHIVED) has none. Expected coverage: ISO 2/3, HDS 0/0, overall 2/3.
func seedComplianceRegistry(t *testing.T, f assetFixture, svc *GRCService) complianceRegistry {
	t.Helper()

	framework := func(code, status string) Framework {
		r := validFrameworkRequest()
		r.Code, r.Name, r.Status = code, "Référentiel "+code, status
		fw, err := svc.SaveFramework(0, 1, r)
		if err != nil {
			t.Fatal(err)
		}
		return *fw
	}
	reg := complianceRegistry{
		iso: framework("ISO-27001", FrameworkStatusActive),
		hds: framework("HDS", FrameworkStatusDraft),
		old: framework("OLD", FrameworkStatusArchived),
	}
	reg.r1 = createRequirement(t, svc, reg.iso.ID, "A.5.1", nil)
	reg.r2 = createRequirement(t, svc, reg.iso.ID, "A.5.2", nil)
	reg.r3 = createRequirement(t, svc, reg.iso.ID, "A.8.1", nil)
	reg.r4 = createRequirement(t, svc, reg.iso.ID, "A.8.2", nil)
	reg.r5 = createRequirement(t, svc, reg.hds.ID, "HDS-1", nil)
	inactive := RequirementRequest{FrameworkID: reg.iso.ID, Code: reg.r3.Code, Title: "Chiffrement des postes", Status: RequirementStatusInactive}
	if _, err := svc.SaveRequirement(reg.r3.ID, 1, inactive); err != nil {
		t.Fatal(err)
	}
	reg.c1 = createControl(t, svc, f, "CTL-SAUV", ControlStatusActive)
	reg.c2 = createControl(t, svc, f, "CTL-MFA", ControlStatusActive)
	for _, link := range [][2]uint{
		{reg.r1.ID, reg.c1.ID}, {reg.r1.ID, reg.c2.ID}, {reg.r3.ID, reg.c1.ID},
		{reg.r4.ID, reg.c2.ID}, {reg.r5.ID, reg.c1.ID},
	} {
		if _, err := svc.LinkRequirementControl(1, RequirementControlRequest{RequirementID: link[0], ControlID: link[1]}); err != nil {
			t.Fatal(err)
		}
	}
	return reg
}

func requireCoverage(t *testing.T, name string, got ComplianceCoverage, requirements, applicable, mapped int64, percent float64) {
	t.Helper()
	if got.Requirements != requirements || got.ApplicableRequirements != applicable || got.MappedRequirements != mapped ||
		got.UnmappedRequirements != applicable-mapped ||
		got.ControlMappingCoverage != (DashboardRatio{Numerator: mapped, Denominator: applicable, Percent: percent}) {
		t.Fatalf("%s coverage = %+v, want %d/%d/%d %.2f%%", name, got, requirements, applicable, mapped, percent)
	}
}

func TestComplianceCoverageReport(t *testing.T) {
	f, svc := newComplianceServiceFixture(t)

	empty, err := svc.ComplianceCoverageReport(nil)
	if err != nil {
		t.Fatal(err)
	}
	requireCoverage(t, "empty", empty.Overall, 0, 0, 0, 0)
	if empty.Frameworks == nil || len(empty.Frameworks) != 0 {
		t.Fatalf("frameworks must be an empty list: %+v", empty.Frameworks)
	}

	reg := seedComplianceRegistry(t, f, svc)
	report, err := svc.ComplianceCoverageReport([]uint{reg.old.ID, reg.iso.ID, 999999, reg.iso.ID, reg.hds.ID})
	if err != nil {
		t.Fatal(err)
	}
	requireCoverage(t, "overall", report.Overall, 5, 3, 2, 66.67)
	// Requested order, unknown and duplicate ids skipped.
	if len(report.Frameworks) != 3 || *report.Frameworks[0].FrameworkID != reg.old.ID ||
		*report.Frameworks[1].FrameworkID != reg.iso.ID || *report.Frameworks[2].FrameworkID != reg.hds.ID {
		t.Fatalf("unexpected breakdown: %+v", report.Frameworks)
	}
	requireCoverage(t, "OLD (no requirement)", report.Frameworks[0], 0, 0, 0, 0)
	requireCoverage(t, "ISO", report.Frameworks[1], 4, 3, 2, 66.67)
	// A DRAFT framework has no applicable requirement: 0/0 is 0.
	requireCoverage(t, "HDS (draft)", report.Frameworks[2], 1, 0, 0, 0)
}

func TestComplianceCoverageMatchesDashboard(t *testing.T) {
	f, svc := newComplianceServiceFixture(t)
	if err := f.db.AutoMigrate(dashboardModels()...); err != nil {
		t.Fatal(err)
	}
	seedComplianceRegistry(t, f, svc)

	dashboard, err := svc.Dashboard(defaultDashboardFilter())
	if err != nil {
		t.Fatal(err)
	}
	report, err := svc.ComplianceCoverageReport(nil)
	if err != nil {
		t.Fatal(err)
	}
	c := dashboard.Compliance
	got := ComplianceCoverage{
		Requirements: c.Requirements, ApplicableRequirements: c.ApplicableRequirements,
		MappedRequirements: c.MappedRequirements, UnmappedRequirements: c.UnmappedRequirements,
		ControlMappingCoverage: c.ControlMappingCoverage,
	}
	if got != report.Overall || dashboard.Summary.ControlMappingCoveragePct != report.Overall.ControlMappingCoverage.Percent {
		t.Fatalf("dashboard %+v diverges from compliance %+v", got, report.Overall)
	}
}

func TestListRequirementsExploration(t *testing.T) {
	f, svc := newComplianceServiceFixture(t)
	reg := seedComplianceRegistry(t, f, svc)
	yes, no := true, false

	codes := func(filter RequirementFilter) string {
		t.Helper()
		page, err := svc.ListRequirements(filter)
		if err != nil {
			t.Fatal(err)
		}
		out := make([]string, 0, len(page.Items))
		for _, r := range page.Items {
			out = append(out, r.Code)
		}
		return strings.Join(out, ",")
	}
	same := func(name, got, want string) {
		t.Helper()
		if got != want {
			t.Fatalf("%s = %s, want %s", name, got, want)
		}
	}

	same("all", codes(RequirementFilter{}), "A.5.1,A.5.2,A.8.1,A.8.2,HDS-1")
	same("mapped", codes(RequirementFilter{Mapped: &yes}), "A.5.1,A.8.1,A.8.2,HDS-1")
	same("unmapped", codes(RequirementFilter{Mapped: &no}), "A.5.2")
	same("control", codes(RequirementFilter{ControlID: &reg.c2.ID}), "A.5.1,A.8.2")
	same("framework + mapped", codes(RequirementFilter{FrameworkID: &reg.iso.ID, Mapped: &yes, Status: "active"}), "A.5.1,A.8.2")
	// Parenthesized search: it never widens the other filters.
	same("search + unmapped", codes(RequirementFilter{Search: "a.5", Mapped: &no}), "A.5.2")
	same("search title", codes(RequirementFilter{Search: "CHIFFREMENT"}), "A.8.1")

	page, err := svc.ListRequirements(RequirementFilter{FrameworkID: &reg.iso.ID, Page: 2, Limit: 3})
	if err != nil || page.Total != 4 || page.TotalPages != 2 || len(page.Items) != 1 || page.Items[0].Code != "A.8.2" {
		t.Fatalf("page 2: %v %+v", err, page)
	}
	empty, err := svc.ListRequirements(RequirementFilter{FrameworkID: &reg.old.ID})
	if err != nil || empty.Items == nil || empty.Total != 0 || empty.TotalPages != 0 {
		t.Fatalf("empty requirements: %v %+v", err, empty)
	}

	// Mapped control counts, in the list and the detail.
	all, err := svc.ListRequirements(RequirementFilter{FrameworkID: &reg.iso.ID})
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int64{}
	for _, r := range all.Items {
		counts[r.Code] = r.MappedControls
	}
	if counts["A.5.1"] != 2 || counts["A.5.2"] != 0 || counts["A.8.1"] != 1 || counts["A.8.2"] != 1 {
		t.Fatalf("mapped controls: %v", counts)
	}
	detail, err := svc.FindRequirement(reg.r1.ID)
	if err != nil || detail.MappedControls != 2 || detail.Framework == nil || detail.Framework.ID != reg.iso.ID {
		t.Fatalf("requirement detail: %v %+v", err, detail)
	}
	_, err = svc.FindRequirement(999999)
	requireServiceError(t, err, http.StatusNotFound)

	// Requirement → mapped controls through grc_requirement_controls only.
	controls, err := svc.ListControls(ControlFilter{RequirementID: &reg.r1.ID})
	if err != nil || controls.Total != 2 {
		t.Fatalf("controls of R1: %v %+v", err, controls)
	}
	none, err := svc.ListControls(ControlFilter{RequirementID: &reg.r2.ID})
	if err != nil || none.Total != 0 || none.Items == nil {
		t.Fatalf("controls of R2: %v %+v", err, none)
	}

	// Unlinking updates both the mapping and the coverage.
	if err := svc.UnlinkRequirementControl(1, reg.r4.ID, reg.c2.ID); err != nil {
		t.Fatal(err)
	}
	report, err := svc.ComplianceCoverageReport([]uint{reg.iso.ID})
	if err != nil {
		t.Fatal(err)
	}
	requireCoverage(t, "ISO after unlink", report.Frameworks[0], 4, 3, 1, 33.33)
}
