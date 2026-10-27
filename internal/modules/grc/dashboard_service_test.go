package grc

import (
	"net/http"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/core/audit"
	"github.com/lallene/medcore-his/backend/internal/modules/auth"
	"github.com/lallene/medcore-his/backend/internal/modules/organization"
	"github.com/lallene/medcore-his/backend/internal/modules/staff"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

var dashboardTestNow = time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

func dashboardModels() []any {
	return []any{
		&auth.User{}, &organization.Department{}, &organization.Service{}, &staff.Profile{},
		&Asset{}, &Risk{}, &Framework{}, &Requirement{}, &Control{}, &RequirementControl{}, &ControlRisk{},
		&ControlImplementation{}, &Evidence{}, &RemediationAction{}, &AuditEngagement{}, &Assessment{},
		&AssessmentEvidence{}, &Finding{}, &SecurityIncident{}, &audit.AuditLog{},
	}
}

func newDashboardService(t *testing.T) (*gorm.DB, *GRCService) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(dashboardModels()...); err != nil {
		t.Fatal(err)
	}
	svc := NewService(db)
	svc.now = func() time.Time { return dashboardTestNow }
	return db, svc
}

func defaultDashboardFilter() DashboardFilter {
	return DashboardFilter{ExpiringSoonDays: DashboardDefaultWindowDays, DueSoonDays: DashboardDefaultWindowDays}
}

func mustCreateAll(t *testing.T, db *gorm.DB, rows ...any) {
	t.Helper()
	for _, row := range rows {
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("create %T: %v", row, err)
		}
	}
}

// seedDashboardDataset inserts a controlled dataset around now. Every row sits
// on a boundary the dashboard must classify: before now, at now, inside the
// 30-day window, at its end and after it. requireDashboardDataset holds the
// expected figures, computed by hand.
func seedDashboardDataset(t *testing.T, db *gorm.DB, now time.Time) {
	t.Helper()

	day := 24 * time.Hour
	window := 30 * day
	at := func(d time.Duration) *time.Time { v := now.Add(d); return &v }
	user := auth.User{Name: "Dashboard", Email: "grc.dashboard@test.local", PasswordHash: "x", Role: "staff", IsActive: true}
	mustCreateAll(t, db, &user)
	assessor := staff.Profile{UserID: user.ID, EmployeeCode: "GRC-DSH", Active: true}
	mustCreateAll(t, db, &assessor)

	a1 := Asset{Code: "DSH-A1", Name: "A1", Type: "APPLICATION", Criticality: AssetCriticalityCritical, Status: AssetStatusActive, CreatedBy: 1, UpdatedBy: 1}
	a2 := Asset{Code: "DSH-A2", Name: "A2", Type: "SERVER", Criticality: AssetCriticalityLow, Status: AssetStatusActive, CreatedBy: 1, UpdatedBy: 1}
	a3 := Asset{Code: "DSH-A3", Name: "A3", Type: "DATA", Criticality: AssetCriticalityHigh, Status: AssetStatusRetired, CreatedBy: 1, UpdatedBy: 1}
	mustCreateAll(t, db, &a1, &a2, &a3)

	risk := func(ref, status string, inherent, residual int) *Risk {
		return &Risk{
			Reference: ref, Title: ref, Description: ref, AssetID: &a1.ID,
			Likelihood: 1, Impact: 1, InherentScore: inherent,
			ResidualLikelihood: 1, ResidualImpact: 1, ResidualScore: residual,
			Treatment: "REDUCE", Status: status, CreatedBy: 1, UpdatedBy: 1,
		}
	}
	mustCreateAll(t, db,
		risk("DSH-R1", RiskStatusOpen, 25, 15),
		risk("DSH-R2", RiskStatusInTreatment, 20, 14),
		risk("DSH-R3", RiskStatusOpen, 10, 10),
		risk("DSH-R4", RiskStatusOpen, 9, 4),
		risk("DSH-R5", RiskStatusClosed, 25, 25),
		risk("DSH-R6", RiskStatusDraft, 16, 16),
	)

	c1 := Control{Code: "DSH-C1", Name: "C1", Status: ControlStatusActive, CreatedBy: 1, UpdatedBy: 1}
	c2 := Control{Code: "DSH-C2", Name: "C2", Status: ControlStatusDraft, CreatedBy: 1, UpdatedBy: 1}
	mustCreateAll(t, db, &c1, &c2)
	ci1 := ControlImplementation{ControlID: c1.ID, AssetID: a1.ID, Status: ControlImplementationStatusImplemented, CreatedBy: 1, UpdatedBy: 1}
	ci2 := ControlImplementation{ControlID: c1.ID, AssetID: a2.ID, Status: ControlImplementationStatusPartial, CreatedBy: 1, UpdatedBy: 1}
	ci3 := ControlImplementation{ControlID: c2.ID, AssetID: a1.ID, Status: ControlImplementationStatusPlanned, CreatedBy: 1, UpdatedBy: 1}
	mustCreateAll(t, db, &ci1, &ci2, &ci3)

	f1 := Framework{Code: "DSH-F1", Name: "F1", Version: "1", Status: FrameworkStatusActive, CreatedBy: 1, UpdatedBy: 1}
	f2 := Framework{Code: "DSH-F2", Name: "F2", Version: "1", Status: FrameworkStatusDraft, CreatedBy: 1, UpdatedBy: 1}
	f3 := Framework{Code: "DSH-F3", Name: "F3", Version: "1", Status: FrameworkStatusArchived, CreatedBy: 1, UpdatedBy: 1}
	mustCreateAll(t, db, &f1, &f2, &f3)
	requirement := func(framework uint, code, status string) *Requirement {
		return &Requirement{FrameworkID: framework, Code: code, Title: code, Status: status, CreatedBy: 1, UpdatedBy: 1}
	}
	q1 := requirement(f1.ID, "Q1", RequirementStatusActive)
	q3 := requirement(f1.ID, "Q3", RequirementStatusInactive)
	q4 := requirement(f2.ID, "Q4", RequirementStatusActive)
	mustCreateAll(t, db, q1, requirement(f1.ID, "Q2", RequirementStatusActive), q3, q4, requirement(f1.ID, "Q5", RequirementStatusActive))
	// Q1 counts as mapped (two links, counted once); Q3 (inactive) and Q4
	// (draft framework) are mapped but not applicable.
	mustCreateAll(t, db,
		&RequirementControl{RequirementID: q1.ID, ControlID: c1.ID, CreatedBy: 1},
		&RequirementControl{RequirementID: q1.ID, ControlID: c2.ID, CreatedBy: 1},
		&RequirementControl{RequirementID: q3.ID, ControlID: c1.ID, CreatedBy: 1},
		&RequirementControl{RequirementID: q4.ID, ControlID: c1.ID, CreatedBy: 1},
	)

	evidence := func(ref, status string, expires *time.Time) *Evidence {
		return &Evidence{Reference: ref, Title: ref, ControlImplementationID: ci1.ID, Type: "REPORT", Status: status, ExpiresAt: expires, CreatedBy: 1, UpdatedBy: 1}
	}
	e1 := evidence("DSH-E1", EvidenceStatusValid, at(-time.Second))
	e6 := evidence("DSH-E6", EvidenceStatusValid, nil)
	e7 := evidence("DSH-E7", EvidenceStatusDraft, at(-day))
	mustCreateAll(t, db,
		e1,
		evidence("DSH-E2", EvidenceStatusValid, at(0)),
		evidence("DSH-E3", EvidenceStatusValid, at(10*day)),
		evidence("DSH-E4", EvidenceStatusValid, at(window)),
		evidence("DSH-E5", EvidenceStatusValid, at(window+time.Second)),
		e6, e7,
		evidence("DSH-E8", EvidenceStatusRevoked, nil),
	)

	assessment := func(ref string, ci uint, status string, result *string, completed, next *time.Time) *Assessment {
		a := &Assessment{
			Reference: ref, Title: ref, ControlImplementationID: ci, Type: "DESIGN", Status: status,
			Result: result, CompletedAt: completed, NextAssessmentAt: next, CreatedBy: 1, UpdatedBy: 1,
		}
		if status == AssessmentStatusCompleted {
			a.AssessorProfileID, a.AssessedAt, a.Summary = &assessor.ID, at(-20*day), "Synthèse"
		}
		return a
	}
	as1 := assessment("DSH-AS1", ci1.ID, AssessmentStatusCompleted, stringPtr(AssessmentResultIneffective), at(-10*day), at(0))
	as2 := assessment("DSH-AS2", ci1.ID, AssessmentStatusCompleted, stringPtr(AssessmentResultEffective), at(-2*day), at(-time.Hour))
	as5 := assessment("DSH-AS5", ci2.ID, AssessmentStatusCompleted, stringPtr(AssessmentResultPartiallyEffective), at(-5*day), nil)
	// Same completion instant as AS5: the higher id wins.
	as6 := assessment("DSH-AS6", ci2.ID, AssessmentStatusCompleted, stringPtr(AssessmentResultNotApplicable), at(-5*day), at(day))
	mustCreateAll(t, db, as1, as2,
		assessment("DSH-AS3", ci2.ID, AssessmentStatusDraft, nil, nil, at(-day)),
		assessment("DSH-AS4", ci2.ID, AssessmentStatusCancelled, nil, nil, nil),
		as5, as6,
	)
	mustCreateAll(t, db,
		&AssessmentEvidence{AssessmentID: as1.ID, EvidenceID: e1.ID, CreatedBy: 1},
		&AssessmentEvidence{AssessmentID: as2.ID, EvidenceID: e6.ID, CreatedBy: 1},
		&AssessmentEvidence{AssessmentID: as5.ID, EvidenceID: e7.ID, CreatedBy: 1},
	)

	action := func(ref, status, priority string, due *time.Time) *RemediationAction {
		a := &RemediationAction{Reference: ref, Title: ref, ControlImplementationID: &ci1.ID, Priority: priority, Status: status, DueAt: due, CreatedBy: 1, UpdatedBy: 1}
		if status == RemediationActionStatusDone {
			a.CompletedAt = at(-day)
		}
		return a
	}
	mustCreateAll(t, db,
		action("DSH-X1", RemediationActionStatusOpen, RemediationActionPriorityCritical, at(-time.Second)),
		action("DSH-X2", RemediationActionStatusInProgress, RemediationActionPriorityHigh, at(0)),
		action("DSH-X3", RemediationActionStatusBlocked, RemediationActionPriorityHigh, at(window)),
		action("DSH-X4", RemediationActionStatusOpen, RemediationActionPriorityLow, at(window+day)),
		action("DSH-X5", RemediationActionStatusOpen, RemediationActionPriorityMedium, nil),
		action("DSH-X6", RemediationActionStatusDone, RemediationActionPriorityLow, at(-5*day)),
		action("DSH-X7", RemediationActionStatusCancelled, RemediationActionPriorityMedium, at(day)),
	)

	audit := func(ref, status string) *AuditEngagement {
		a := &AuditEngagement{Reference: ref, Title: ref, Type: "INTERNAL", Status: status, CreatedBy: 1, UpdatedBy: 1}
		if status == AuditEngagementStatusInProgress || status == AuditEngagementStatusCompleted {
			a.StartedAt = at(-10 * day)
		}
		if status == AuditEngagementStatusCompleted {
			a.CompletedAt = at(-day)
		}
		return a
	}
	au1 := audit("DSH-AU1", AuditEngagementStatusPlanned)
	mustCreateAll(t, db, au1,
		audit("DSH-AU2", AuditEngagementStatusInProgress),
		audit("DSH-AU3", AuditEngagementStatusCompleted),
		audit("DSH-AU4", AuditEngagementStatusCancelled),
		audit("DSH-AU5", AuditEngagementStatusPlanned),
	)

	finding := func(ref, status, severity string, due *time.Time) *Finding {
		f := &Finding{
			Reference: ref, Title: ref, AuditEngagementID: &au1.ID, Category: "OBSERVATION",
			Severity: severity, Status: status, DueAt: due, CreatedBy: 1, UpdatedBy: 1,
		}
		if status == FindingStatusResolved || status == FindingStatusClosed {
			f.ResolvedAt = at(-3 * day)
		}
		if status == FindingStatusClosed || status == FindingStatusAccepted {
			f.ClosedAt, f.ClosureNote = at(-day), "Clôture"
		}
		return f
	}
	mustCreateAll(t, db,
		finding("DSH-G1", FindingStatusOpen, FindingSeverityHigh, at(-time.Second)),
		finding("DSH-G2", FindingStatusInRemediation, FindingSeverityCritical, at(-day)),
		finding("DSH-G3", FindingStatusOpen, FindingSeverityLow, at(0)),
		finding("DSH-G4", FindingStatusResolved, FindingSeverityMedium, at(-5*day)),
		finding("DSH-G5", FindingStatusClosed, FindingSeverityLow, nil),
		finding("DSH-G6", FindingStatusAccepted, FindingSeverityMedium, at(-5*day)),
	)
}

func requireCounts(t *testing.T, name string, got []DashboardCount, want ...any) {
	t.Helper()
	if len(got) != len(want)/2 {
		t.Fatalf("%s: got %d buckets %+v, want %d", name, len(got), got, len(want)/2)
	}
	for i := range got {
		key, count := want[2*i].(string), int64(want[2*i+1].(int))
		if got[i].Key != key || got[i].Count != count {
			t.Fatalf("%s[%d] = %+v, want {%s %d}", name, i, got[i], key, count)
		}
	}
}

func requireValue(t *testing.T, name string, got int64, want int64) {
	t.Helper()
	if got != want {
		t.Fatalf("%s = %d, want %d", name, got, want)
	}
}

// requireDashboardDataset checks the figures of seedDashboardDataset with the
// default 30-day windows.
func requireDashboardDataset(t *testing.T, d *GRCDashboard, now time.Time) {
	t.Helper()

	if !d.GeneratedAt.Equal(now) || d.Windows != (DashboardWindows{ExpiringSoonDays: 30, DueSoonDays: 30}) {
		t.Fatalf("unexpected header: %v %+v", d.GeneratedAt, d.Windows)
	}

	requireValue(t, "assets.total", d.Assets.Total, 3)
	requireValue(t, "assets.active", d.Assets.Active, 2)
	requireValue(t, "assets.critical", d.Assets.Critical, 1)
	requireCounts(t, "assets.byStatus", d.Assets.ByStatus, "ACTIVE", 2, "INACTIVE", 0, "RETIRED", 1)
	requireCounts(t, "assets.byCriticality", d.Assets.ByCriticality, "LOW", 1, "MEDIUM", 0, "HIGH", 1, "CRITICAL", 1)

	requireValue(t, "risks.total", d.Risks.Total, 6)
	requireValue(t, "risks.open", d.Risks.Open, 4)
	requireValue(t, "risks.critical", d.Risks.Critical, 1)
	requireValue(t, "risks.high", d.Risks.High, 2)
	requireCounts(t, "risks.byStatus", d.Risks.ByStatus, "DRAFT", 1, "OPEN", 3, "IN_TREATMENT", 1, "ACCEPTED", 0, "CLOSED", 1)
	requireCounts(t, "risks.residualByLevel", d.Risks.ResidualByLevel, "LOW", 1, "MEDIUM", 0, "HIGH", 2, "CRITICAL", 1)
	requireCounts(t, "risks.inherentByLevel", d.Risks.InherentByLevel, "LOW", 0, "MEDIUM", 1, "HIGH", 1, "CRITICAL", 2)

	requireValue(t, "controls.total", d.Controls.Total, 2)
	requireCounts(t, "controls.byStatus", d.Controls.ByStatus, "DRAFT", 1, "ACTIVE", 1, "INACTIVE", 0, "RETIRED", 0)
	requireValue(t, "controls.implementations", d.Controls.Implementations, 3)
	requireCounts(t, "controls.implementationsByStatus", d.Controls.ImplementationsByStatus,
		"PLANNED", 1, "IMPLEMENTED", 1, "PARTIAL", 1, "NOT_IMPLEMENTED", 0, "NOT_APPLICABLE", 0)
	requireCounts(t, "controls.implementationsByLatestAssessmentResult", d.Controls.ImplementationsByLatestAssessmentResult,
		"EFFECTIVE", 1, "PARTIALLY_EFFECTIVE", 0, "INEFFECTIVE", 0, "NOT_APPLICABLE", 1, "NOT_ASSESSED", 1)

	c := d.Compliance
	requireValue(t, "compliance.frameworks", c.Frameworks, 3)
	requireCounts(t, "compliance.frameworksByStatus", c.FrameworksByStatus, "DRAFT", 1, "ACTIVE", 1, "ARCHIVED", 1)
	requireValue(t, "compliance.requirements", c.Requirements, 5)
	requireValue(t, "compliance.applicable", c.ApplicableRequirements, 3)
	requireValue(t, "compliance.mapped", c.MappedRequirements, 1)
	requireValue(t, "compliance.unmapped", c.UnmappedRequirements, 2)
	if c.ControlMappingCoverage != (DashboardRatio{Numerator: 1, Denominator: 3, Percent: 33.33}) {
		t.Fatalf("unexpected coverage: %+v", c.ControlMappingCoverage)
	}

	e := d.Evidence
	requireValue(t, "evidence.total", e.Total, 8)
	requireCounts(t, "evidence.byStatus", e.ByStatus, "DRAFT", 1, "VALID", 6, "REVOKED", 1)
	requireValue(t, "evidence.expired", e.Expired, 1)
	requireValue(t, "evidence.expiringSoon", e.ExpiringSoon, 3)
	requireValue(t, "evidence.valid", e.Valid, 5)
	requireValue(t, "evidence.invalid", e.Invalid, 3)

	a := d.Actions
	requireValue(t, "actions.total", a.Total, 7)
	requireCounts(t, "actions.byStatus", a.ByStatus, "OPEN", 3, "IN_PROGRESS", 1, "BLOCKED", 1, "DONE", 1, "CANCELLED", 1)
	requireCounts(t, "actions.byPriority", a.ByPriority, "LOW", 2, "MEDIUM", 2, "HIGH", 2, "CRITICAL", 1)
	requireValue(t, "actions.open", a.Open, 5)
	requireValue(t, "actions.overdue", a.Overdue, 1)
	requireValue(t, "actions.dueSoon", a.DueSoon, 2)

	requireValue(t, "assessments.total", d.Assessments.Total, 6)
	requireCounts(t, "assessments.byStatus", d.Assessments.ByStatus, "DRAFT", 1, "COMPLETED", 4, "CANCELLED", 1)
	requireValue(t, "assessments.reassessmentDue", d.Assessments.ReassessmentDue, 1)
	requireValue(t, "assessments.hasInvalidEvidence", d.Assessments.HasInvalidEvidence, 2)

	requireValue(t, "audits.total", d.Audits.Total, 5)
	requireCounts(t, "audits.byStatus", d.Audits.ByStatus, "PLANNED", 2, "IN_PROGRESS", 1, "COMPLETED", 1, "CANCELLED", 1)

	f := d.Findings
	requireValue(t, "findings.total", f.Total, 6)
	requireCounts(t, "findings.byStatus", f.ByStatus, "OPEN", 2, "IN_REMEDIATION", 1, "RESOLVED", 1, "CLOSED", 1, "ACCEPTED", 1)
	requireCounts(t, "findings.bySeverity", f.BySeverity, "LOW", 2, "MEDIUM", 2, "HIGH", 1, "CRITICAL", 1)
	requireValue(t, "findings.open", f.Open, 4)
	requireValue(t, "findings.overdue", f.Overdue, 2)

	if d.Attention != (DashboardAttention{
		CriticalResidualRisks: 1, HighResidualRisks: 2, ExpiredEvidence: 1, EvidenceExpiringSoon: 3,
		OverdueActions: 1, ActionsDueSoon: 2, ReassessmentsDue: 1, OverdueFindings: 2,
	}) {
		t.Fatalf("unexpected attention: %+v", d.Attention)
	}
	if d.Summary != (DashboardSummary{
		Assets: 3, OpenRisks: 4, CriticalOrHighRisks: 3, Controls: 2, EvidenceIssues: 3,
		OpenRemediationActions: 5, OpenFindings: 4, OverdueItems: 3, ControlMappingCoveragePct: 33.33,
	}) {
		t.Fatalf("unexpected summary: %+v", d.Summary)
	}
}

func TestDashboardEmptyDatabase(t *testing.T) {
	_, svc := newDashboardService(t)

	d, err := svc.Dashboard(defaultDashboardFilter())
	if err != nil {
		t.Fatal(err)
	}
	if d.Summary != (DashboardSummary{}) || d.Attention != (DashboardAttention{}) {
		t.Fatalf("empty database must give zero figures: %+v %+v", d.Summary, d.Attention)
	}
	if d.Compliance.ControlMappingCoverage != (DashboardRatio{}) {
		t.Fatalf("0/0 coverage must be 0: %+v", d.Compliance.ControlMappingCoverage)
	}
	requireCounts(t, "risks.residualByLevel", d.Risks.ResidualByLevel, "LOW", 0, "MEDIUM", 0, "HIGH", 0, "CRITICAL", 0)
	requireCounts(t, "controls.implementationsByLatestAssessmentResult", d.Controls.ImplementationsByLatestAssessmentResult,
		"EFFECTIVE", 0, "PARTIALLY_EFFECTIVE", 0, "INEFFECTIVE", 0, "NOT_APPLICABLE", 0, "NOT_ASSESSED", 0)
	requireCounts(t, "findings.byStatus", d.Findings.ByStatus, "OPEN", 0, "IN_REMEDIATION", 0, "RESOLVED", 0, "CLOSED", 0, "ACCEPTED", 0)
}

func TestDashboardControlledDataset(t *testing.T) {
	db, svc := newDashboardService(t)
	seedDashboardDataset(t, db, dashboardTestNow)

	d, err := svc.Dashboard(defaultDashboardFilter())
	if err != nil {
		t.Fatal(err)
	}
	requireDashboardDataset(t, d, dashboardTestNow)
}

func TestDashboardWindowsDriveSoonFigures(t *testing.T) {
	db, svc := newDashboardService(t)
	seedDashboardDataset(t, db, dashboardTestNow)

	// One day: only the items due exactly now stay "soon"; overdue and
	// expired figures do not depend on the windows.
	d, err := svc.Dashboard(DashboardFilter{ExpiringSoonDays: 1, DueSoonDays: 1})
	if err != nil {
		t.Fatal(err)
	}
	requireValue(t, "evidence.expiringSoon", d.Evidence.ExpiringSoon, 1)
	requireValue(t, "actions.dueSoon", d.Actions.DueSoon, 1)
	requireValue(t, "evidence.expired", d.Evidence.Expired, 1)
	requireValue(t, "actions.overdue", d.Actions.Overdue, 1)
	if d.Windows != (DashboardWindows{ExpiringSoonDays: 1, DueSoonDays: 1}) {
		t.Fatalf("unexpected windows: %+v", d.Windows)
	}

	// 365 days reaches E5 (window + 1s) and X4 (window + 1 day).
	d, err = svc.Dashboard(DashboardFilter{ExpiringSoonDays: 365, DueSoonDays: 365})
	if err != nil {
		t.Fatal(err)
	}
	requireValue(t, "evidence.expiringSoon", d.Evidence.ExpiringSoon, 4)
	requireValue(t, "actions.dueSoon", d.Actions.DueSoon, 3)
}

func TestDashboardFollowsTheClock(t *testing.T) {
	db, svc := newDashboardService(t)
	seedDashboardDataset(t, db, dashboardTestNow)

	// One second later, the items due exactly at the seed instant are late and
	// the window end moves past E5 (window + 1s).
	svc.now = func() time.Time { return dashboardTestNow.Add(time.Second) }
	d, err := svc.Dashboard(defaultDashboardFilter())
	if err != nil {
		t.Fatal(err)
	}
	requireValue(t, "evidence.expired", d.Evidence.Expired, 2)
	requireValue(t, "evidence.expiringSoon", d.Evidence.ExpiringSoon, 3)
	requireValue(t, "actions.overdue", d.Actions.Overdue, 2)
	requireValue(t, "actions.dueSoon", d.Actions.DueSoon, 1)
	requireValue(t, "findings.overdue", d.Findings.Overdue, 3)
	requireValue(t, "assessments.reassessmentDue", d.Assessments.ReassessmentDue, 2)
}

func TestDashboardRejectsInvalidWindows(t *testing.T) {
	_, svc := newDashboardService(t)

	for _, f := range []DashboardFilter{
		{ExpiringSoonDays: 0, DueSoonDays: 30},
		{ExpiringSoonDays: 366, DueSoonDays: 30},
		{ExpiringSoonDays: 30, DueSoonDays: 0},
		{ExpiringSoonDays: 30, DueSoonDays: -1},
		{ExpiringSoonDays: 30, DueSoonDays: 366},
	} {
		_, err := svc.Dashboard(f)
		requireServiceError(t, err, http.StatusBadRequest)
	}
}
