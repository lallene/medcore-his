package grc

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// GRC-09: audit lifecycle and finding lifecycle matrices, finding source
// immutability, audit finding counters (dashboard semantics), registry
// filters and lengths.

func TestAuditEngagementLifecycleMatrix(t *testing.T) {
	statuses := []string{AuditEngagementStatusPlanned, AuditEngagementStatusInProgress, AuditEngagementStatusCompleted, AuditEngagementStatusCancelled}
	allowed := map[string]bool{
		"PLANNED>PLANNED": true, "PLANNED>IN_PROGRESS": true, "PLANNED>CANCELLED": true,
		"IN_PROGRESS>IN_PROGRESS": true, "IN_PROGRESS>COMPLETED": true, "IN_PROGRESS>CANCELLED": true,
	}
	for _, from := range statuses {
		for _, to := range statuses {
			err := validateAuditEngagementTransition(from, to)
			if allowed[from+">"+to] {
				if err != nil {
					t.Errorf("%s -> %s rejected: %v", from, to, err)
				}
				continue
			}
			requireServiceError(t, err, http.StatusConflict)
		}
	}
	for _, status := range statuses {
		err := validateAuditEngagementInitialStatus(status)
		if (status == AuditEngagementStatusPlanned || status == AuditEngagementStatusInProgress) != (err == nil) {
			t.Errorf("initial status %s: %v", status, err)
		}
	}
}

func TestFindingLifecycleMatrix(t *testing.T) {
	statuses := []string{FindingStatusOpen, FindingStatusInRemediation, FindingStatusResolved, FindingStatusClosed, FindingStatusAccepted}
	allowed := map[string]bool{
		"OPEN>OPEN": true, "OPEN>IN_REMEDIATION": true, "OPEN>RESOLVED": true, "OPEN>ACCEPTED": true,
		"IN_REMEDIATION>IN_REMEDIATION": true, "IN_REMEDIATION>RESOLVED": true, "IN_REMEDIATION>ACCEPTED": true,
		"RESOLVED>RESOLVED": true, "RESOLVED>OPEN": true, "RESOLVED>CLOSED": true, "RESOLVED>ACCEPTED": true,
	}
	for _, from := range statuses {
		for _, to := range statuses {
			err := validateFindingTransition(from, to)
			if allowed[from+">"+to] {
				if err != nil {
					t.Errorf("%s -> %s rejected: %v", from, to, err)
				}
				continue
			}
			requireServiceError(t, err, http.StatusConflict)
		}
	}
	for _, status := range statuses {
		if err := validateFindingInitialStatus(status); (status == FindingStatusOpen) != (err == nil) {
			t.Errorf("initial status %s: %v", status, err)
		}
	}
}

func TestFindingSourceIsImmutable(t *testing.T) {
	f := newGRC01DServiceFixture(t)
	audit := f.inProgressAudit(t, "AUD-SRC-09")
	other := f.inProgressAudit(t, "AUD-SRC-09-B")
	assessment := f.createAssessment(t, f.assessmentRequest("ASM-SRC-09"))
	r := f.findingRequest("FND-SRC-09", audit.ID)
	finding := f.createFinding(t, r)

	moved := r
	moved.AuditEngagementID = &other.ID
	_, err := f.svc.SaveFinding(finding.ID, 1, moved)
	requireServiceError(t, err, http.StatusConflict)
	moved.AuditEngagementID, moved.AssessmentID = nil, &assessment.ID
	_, err = f.svc.SaveFinding(finding.ID, 1, moved)
	requireServiceError(t, err, http.StatusConflict)

	stored, err := f.svc.FindFinding(finding.ID)
	if err != nil || stored.AuditEngagementID == nil || *stored.AuditEngagementID != audit.ID || stored.AssessmentID != nil {
		t.Fatalf("source changed: %v %+v", err, stored)
	}
}

func TestAuditFindingCountersMatchDashboard(t *testing.T) {
	f := newGRC01DServiceFixture(t)
	if err := f.db.AutoMigrate(dashboardModels()...); err != nil {
		t.Fatal(err)
	}
	day := 24 * time.Hour
	at := func(d time.Duration) *time.Time { v := evidenceTestNow.Add(d); return &v }
	first := f.inProgressAudit(t, "AUD-CNT-A")
	second := f.inProgressAudit(t, "AUD-CNT-B")
	empty := f.createAudit(t, f.auditRequest("AUD-CNT-EMPTY"))

	// AUD-CNT-A: one overdue open, one due exactly now (not overdue), one accepted late.
	late := f.findingRequest("FND-CNT-LATE", first.ID)
	late.DueAt = at(-time.Second)
	f.createFinding(t, late)
	now := f.findingRequest("FND-CNT-NOW", first.ID)
	now.DueAt = at(0)
	f.createFinding(t, now)
	accepted := f.findingRequest("FND-CNT-ACC", first.ID)
	accepted.DueAt = at(-day)
	acc := f.createFinding(t, accepted)
	accepted.Status, accepted.ClosureNote = FindingStatusAccepted, "Risque accepté"
	if _, err := f.svc.SaveFinding(acc.ID, 1, accepted); err != nil {
		t.Fatal(err)
	}
	// AUD-CNT-B: one in remediation, late.
	remediation := f.findingRequest("FND-CNT-REM", second.ID)
	remediation.DueAt = at(-day)
	rem := f.createFinding(t, remediation)
	remediation.Status = FindingStatusInRemediation
	if _, err := f.svc.SaveFinding(rem.ID, 1, remediation); err != nil {
		t.Fatal(err)
	}

	page, err := f.svc.ListAuditEngagements(AuditEngagementFilter{Search: "aud-cnt"})
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string][3]int64{}
	var total, open, overdue int64
	for _, a := range page.Items {
		counts[a.Reference] = [3]int64{a.FindingCount, a.OpenFindingCount, a.OverdueFindingCount}
		total, open, overdue = total+a.FindingCount, open+a.OpenFindingCount, overdue+a.OverdueFindingCount
	}
	if counts["AUD-CNT-A"] != [3]int64{3, 2, 1} || counts["AUD-CNT-B"] != [3]int64{1, 1, 1} || counts["AUD-CNT-EMPTY"] != [3]int64{0, 0, 0} {
		t.Fatalf("finding counters: %v", counts)
	}
	detail, err := f.svc.FindAuditEngagement(empty.ID)
	if err != nil || detail.FindingCount != 0 {
		t.Fatalf("detail counters: %v %+v", err, detail)
	}

	// Every finding of the dataset comes from these audits: same figures.
	dashboard, err := f.svc.Dashboard(defaultDashboardFilter())
	if err != nil {
		t.Fatal(err)
	}
	if d := dashboard.Findings; d.Total != total || d.Open != open || d.Overdue != overdue {
		t.Fatalf("dashboard %+v diverges from audit counters %d/%d/%d", d, total, open, overdue)
	}
	yes := true
	overduePage, err := f.svc.ListFindings(FindingFilter{Overdue: &yes})
	if err != nil || overduePage.Total != overdue {
		t.Fatalf("finding overdue filter: %v %+v", err, overduePage)
	}
	if dashboard.Audits.Total != 3 || countOf(dashboard.Audits.ByStatus, AuditEngagementStatusInProgress) != 2 ||
		countOf(dashboard.Audits.ByStatus, AuditEngagementStatusPlanned) != 1 {
		t.Fatalf("dashboard audits: %+v", dashboard.Audits)
	}
}

func TestAuditRegistryFiltersAndValidation(t *testing.T) {
	f := newGRC01DServiceFixture(t)
	planned := f.auditRequest("AUD-REG-PLAN")
	planned.Title = "Audit annuel sécurité"
	planned.PlannedStartAt, planned.PlannedEndAt = timePtr(evidenceTestNow.AddDate(0, 1, 0)), timePtr(evidenceTestNow.AddDate(0, 1, 5))
	f.createAudit(t, planned)
	external := f.auditRequest("AUD-REG-EXT")
	external.Title, external.Type = "Audit annuel prestataire", AuditEngagementTypeExternal
	f.createAudit(t, external)
	f.inProgressAudit(t, "AUD-REG-RUN")

	list := func(filter AuditEngagementFilter) string {
		t.Helper()
		page, err := f.svc.ListAuditEngagements(filter)
		if err != nil {
			t.Fatal(err)
		}
		refs := []string{}
		for _, a := range page.Items {
			refs = append(refs, a.Reference)
		}
		return strings.Join(refs, ",")
	}
	// Planned start first, then the others by id.
	if got := list(AuditEngagementFilter{Search: "aud-reg"}); got != "AUD-REG-PLAN,AUD-REG-EXT,AUD-REG-RUN" {
		t.Fatalf("order: %s", got)
	}
	// The search never widens type or status.
	if got := list(AuditEngagementFilter{Search: "annuel", Type: "external"}); got != "AUD-REG-EXT" {
		t.Fatalf("search + type: %s", got)
	}
	if got := list(AuditEngagementFilter{Search: "annuel", Status: "IN_PROGRESS"}); got != "" {
		t.Fatalf("search + status: %s", got)
	}
	if got := list(AuditEngagementFilter{PlannedStartAfter: timePtr(evidenceTestNow)}); got != "AUD-REG-PLAN" {
		t.Fatalf("planned start filter: %s", got)
	}
	page, err := f.svc.ListAuditEngagements(AuditEngagementFilter{Search: "aud-reg", Page: 2, Limit: 2})
	if err != nil || page.Total != 3 || page.TotalPages != 2 || len(page.Items) != 1 {
		t.Fatalf("pagination: %v %+v", err, page)
	}
	none, err := f.svc.ListAuditEngagements(AuditEngagementFilter{Search: "absent"})
	if err != nil || none.Items == nil || none.Total != 0 {
		t.Fatalf("empty list: %v %+v", err, none)
	}

	long := f.auditRequest(strings.Repeat("É", auditEngagementReferenceMaxLength))
	long.Title = strings.Repeat("é", auditEngagementTitleMaxLength)
	if _, err := f.svc.SaveAuditEngagement(0, 1, long); err != nil {
		t.Fatalf("audit limits rejected: %v", err)
	}
	long.Reference = strings.Repeat("É", auditEngagementReferenceMaxLength+1)
	_, err = f.svc.SaveAuditEngagement(0, 1, long)
	requireServiceError(t, err, http.StatusBadRequest)

	audit := f.inProgressAudit(t, "AUD-REG-FND")
	finding := f.findingRequest(strings.Repeat("É", findingReferenceMaxLength), audit.ID)
	finding.Title = strings.Repeat("é", findingTitleMaxLength)
	if _, err := f.svc.SaveFinding(0, 1, finding); err != nil {
		t.Fatalf("finding limits rejected: %v", err)
	}
	finding.Reference, finding.Title = "FND-LONG", strings.Repeat("é", findingTitleMaxLength+1)
	_, err = f.svc.SaveFinding(0, 1, finding)
	requireServiceError(t, err, http.StatusBadRequest)

	// Planning rule kept: the planned end cannot precede the planned start.
	bad := f.auditRequest("AUD-REG-BAD")
	bad.PlannedStartAt, bad.PlannedEndAt = timePtr(evidenceTestNow.AddDate(0, 0, 5)), timePtr(evidenceTestNow)
	_, err = f.svc.SaveAuditEngagement(0, 1, bad)
	requireServiceError(t, err, http.StatusBadRequest)
}
