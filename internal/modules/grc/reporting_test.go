package grc

import (
	"strings"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/organization"
	"gorm.io/gorm"
)

const reportingSecret = "SECRET-REPORTING-TEXT"

func newReportingService(t *testing.T) (*gorm.DB, *GRCService) {
	t.Helper()
	db, svc := newDashboardService(t)
	if err := db.AutoMigrate(&GovernancePolicy{}); err != nil {
		t.Fatal(err)
	}
	return db, svc
}

func defaultReportingFilter() ReportingFilter {
	return ReportingFilter{
		EvidenceExpiringSoonDays: DashboardDefaultWindowDays,
		ActionDueSoonDays:        DashboardDefaultWindowDays,
	}
}

type reportingFixture struct {
	frameworkA uint
	frameworkB uint
	frameworkC uint
}

func seedReportingDataset(t *testing.T, db *gorm.DB, now time.Time) reportingFixture {
	t.Helper()
	at := func(d time.Duration) *time.Time { v := now.Add(d); return &v }
	day := 24 * time.Hour

	dept := organization.Department{Code: "RPT", Name: "Reporting", Active: true, CreatedBy: 1, UpdatedBy: 1}
	mustCreateAll(t, db, &dept)
	svc := organization.Service{
		DepartmentID: dept.ID, Code: "RPT", Name: "Reporting", ServiceType: organization.TypeAdministrative,
		Active: true, CreatedBy: 1, UpdatedBy: 1,
	}
	mustCreateAll(t, db, &svc)

	critical := Asset{Code: "RPT-CRIT", Name: "Critique", Type: AssetTypeApplication, Criticality: AssetCriticalityCritical, Status: AssetStatusActive, CreatedBy: 1, UpdatedBy: 1}
	low := Asset{Code: "RPT-LOW", Name: "Faible", Type: AssetTypeServer, Criticality: AssetCriticalityLow, Status: AssetStatusActive, CreatedBy: 1, UpdatedBy: 1}
	mustCreateAll(t, db, &critical, &low)

	risk := func(ref, status, treatment string, inherent, residual int, assetID, serviceID *uint) *Risk {
		return &Risk{
			Reference: ref, Title: ref, Description: reportingSecret, Threat: reportingSecret, Vulnerability: reportingSecret,
			AssetID: assetID, OwningServiceID: serviceID,
			Likelihood: 1, Impact: 1, InherentScore: inherent,
			ResidualLikelihood: 1, ResidualImpact: 1, ResidualScore: residual,
			Treatment: treatment, Status: status, CreatedBy: 1, UpdatedBy: 1,
		}
	}
	openCritical := risk("RSK-OC", RiskStatusOpen, RiskTreatmentReduce, 4, 16, &critical.ID, &svc.ID)
	openHigh := risk("RSK-OH", RiskStatusOpen, RiskTreatmentAccept, 25, 10, &low.ID, &svc.ID)
	inTreatment := risk("RSK-IT", RiskStatusInTreatment, RiskTreatmentAvoid, 9, 9, nil, nil)
	draft := risk("RSK-DR", RiskStatusDraft, RiskTreatmentTransfer, 5, 5, nil, nil)
	closed := risk("RSK-CL", RiskStatusClosed, RiskTreatmentReduce, 1, 15, nil, nil)
	accepted := risk("RSK-AC", RiskStatusAccepted, RiskTreatmentAccept, 6, 20, nil, nil)
	mustCreateAll(t, db, openCritical, openHigh, inTreatment, draft, closed, accepted)

	fwA := Framework{Code: "FW-A", Name: "Référentiel A", Version: "1.0", Description: reportingSecret, Status: FrameworkStatusActive, CreatedBy: 1, UpdatedBy: 1}
	fwB := Framework{Code: "FW-B", Name: "Référentiel B", Version: "1.0", Description: reportingSecret, Status: FrameworkStatusDraft, CreatedBy: 1, UpdatedBy: 1}
	fwC := Framework{Code: "FW-C", Name: "Référentiel C", Version: "1.0", Status: FrameworkStatusActive, CreatedBy: 1, UpdatedBy: 1}
	mustCreateAll(t, db, &fwA, &fwB, &fwC)
	control := Control{Code: "CTL-A", Name: "Contrôle A", Description: reportingSecret, Status: ControlStatusActive, CreatedBy: 1, UpdatedBy: 1}
	draftControl := Control{Code: "CTL-D", Name: "Contrôle D", Status: ControlStatusDraft, CreatedBy: 1, UpdatedBy: 1}
	mustCreateAll(t, db, &control, &draftControl)
	reqMapped := Requirement{FrameworkID: fwA.ID, Code: "A1", Title: "A1", Description: reportingSecret, Status: RequirementStatusActive, CreatedBy: 1, UpdatedBy: 1}
	reqOpen := Requirement{FrameworkID: fwA.ID, Code: "A2", Title: "A2", Status: RequirementStatusActive, CreatedBy: 1, UpdatedBy: 1}
	reqInactive := Requirement{FrameworkID: fwA.ID, Code: "A3", Title: "A3", Status: RequirementStatusInactive, CreatedBy: 1, UpdatedBy: 1}
	reqDraftFW := Requirement{FrameworkID: fwB.ID, Code: "B1", Title: "B1", Status: RequirementStatusActive, CreatedBy: 1, UpdatedBy: 1}
	mustCreateAll(t, db, &reqMapped, &reqOpen, &reqInactive, &reqDraftFW)
	mustCreateAll(t, db, &RequirementControl{RequirementID: reqMapped.ID, ControlID: control.ID, CreatedBy: 1})

	impl := ControlImplementation{ControlID: control.ID, AssetID: critical.ID, Status: ControlImplementationStatusImplemented, CreatedBy: 1, UpdatedBy: 1}
	mustCreateAll(t, db, &impl)
	evidence := func(ref, status, kind string, expires *time.Time) *Evidence {
		return &Evidence{
			Reference: ref, Title: ref, Description: reportingSecret, ControlImplementationID: impl.ID,
			Type: kind, Status: status, ExternalReference: reportingSecret, ExpiresAt: expires,
			CreatedBy: 1, UpdatedBy: 1,
		}
	}
	mustCreateAll(t, db,
		evidence("EV-NOW", EvidenceStatusValid, EvidenceTypeDocument, at(0)),
		evidence("EV-SOON", EvidenceStatusValid, EvidenceTypeDocument, at(2*day)),
		evidence("EV-LATE", EvidenceStatusValid, EvidenceTypeDocument, at(-time.Second)),
		evidence("EV-OPEN", EvidenceStatusValid, EvidenceTypeDocument, nil),
		evidence("EV-DRAFT", EvidenceStatusDraft, EvidenceTypeCertificate, nil),
	)

	action := func(ref, status, priority string, due *time.Time) *RemediationAction {
		return &RemediationAction{
			Reference: ref, Title: ref, Description: reportingSecret, RiskID: &openCritical.ID,
			Priority: priority, Status: status, DueAt: due, CreatedBy: 1, UpdatedBy: 1,
		}
	}
	doneAt := now
	mustCreateAll(t, db,
		action("ACT-LATE", RemediationActionStatusOpen, RemediationActionPriorityHigh, at(-time.Second)),
		action("ACT-NOW", RemediationActionStatusOpen, RemediationActionPriorityMedium, at(0)),
		action("ACT-2D", RemediationActionStatusOpen, RemediationActionPriorityLow, at(2*day)),
		action("ACT-NONE", RemediationActionStatusOpen, RemediationActionPriorityCritical, nil),
		action("ACT-FAR", RemediationActionStatusInProgress, RemediationActionPriorityMedium, at(40*day)),
		&RemediationAction{
			Reference: "ACT-DONE", Title: "Faite", Description: reportingSecret, RiskID: &openCritical.ID,
			Priority: RemediationActionPriorityLow, Status: RemediationActionStatusDone, DueAt: at(-day),
			CompletedAt: &doneAt, CreatedBy: 1, UpdatedBy: 1,
		},
	)

	planned := AuditEngagement{Reference: "AUD-P", Title: "Planifié", Scope: reportingSecret, Type: AuditEngagementTypeInternal, Status: AuditEngagementStatusPlanned, CreatedBy: 1, UpdatedBy: 1}
	started := now.Add(-day)
	running := AuditEngagement{Reference: "AUD-R", Title: "En cours", Type: AuditEngagementTypeExternal, Status: AuditEngagementStatusInProgress, StartedAt: &started, CreatedBy: 1, UpdatedBy: 1}
	completed := AuditEngagement{Reference: "AUD-C", Title: "Terminé", Type: AuditEngagementTypeVendor, Status: AuditEngagementStatusCompleted, StartedAt: &started, CompletedAt: &now, CreatedBy: 1, UpdatedBy: 1}
	cancelled := AuditEngagement{Reference: "AUD-X", Title: "Annulé", Type: AuditEngagementTypeInternal, Status: AuditEngagementStatusCancelled, CreatedBy: 1, UpdatedBy: 1}
	mustCreateAll(t, db, &planned, &running, &completed, &cancelled)

	finding := func(ref, status, severity string, due *time.Time) *Finding {
		item := &Finding{
			Reference: ref, Title: ref, Description: reportingSecret, AuditEngagementID: &planned.ID,
			Category: FindingCategoryObservation, Severity: severity, Status: status, DueAt: due,
			CreatedBy: 1, UpdatedBy: 1,
		}
		if status == FindingStatusResolved || status == FindingStatusClosed {
			item.ResolvedAt = at(-time.Hour)
		}
		if status == FindingStatusClosed || status == FindingStatusAccepted {
			item.ClosedAt = at(-time.Hour)
			item.ClosureNote = "Clôture"
		}
		return item
	}
	mustCreateAll(t, db,
		finding("FND-LATE", FindingStatusOpen, FindingSeverityHigh, at(-time.Second)),
		finding("FND-NOW", FindingStatusInRemediation, FindingSeverityMedium, at(0)),
		finding("FND-RES", FindingStatusResolved, FindingSeverityLow, nil),
		finding("FND-CL", FindingStatusClosed, FindingSeverityCritical, nil),
		finding("FND-AC", FindingStatusAccepted, FindingSeverityLow, at(-day)),
	)

	detected := now
	mustCreateAll(t, db,
		&SecurityIncident{Reference: "INC-O", Title: "Ouvert", Description: reportingSecret, Severity: SecurityIncidentSeverityLow, Status: SecurityIncidentStatusOpen, DetectedAt: detected, CreatedBy: 1, UpdatedBy: 1},
		&SecurityIncident{Reference: "INC-I", Title: "Analyse", Description: reportingSecret, Severity: SecurityIncidentSeverityHigh, Status: SecurityIncidentStatusInvestigating, DetectedAt: detected, CreatedBy: 1, UpdatedBy: 1},
		&SecurityIncident{Reference: "INC-C", Title: "Clos", Description: reportingSecret, Severity: SecurityIncidentSeverityCritical, Status: SecurityIncidentStatusClosed, DetectedAt: detected, ResolvedAt: &now, ClosedAt: &now, CreatedBy: 1, UpdatedBy: 1},
	)

	publishedAt := now
	mustCreateAll(t, db,
		&GovernancePolicy{Reference: "POL-D", Title: "Brouillon", Description: reportingSecret, Version: "1.0", Status: GovernancePolicyStatusDraft, CreatedBy: 1, UpdatedBy: 1},
		&GovernancePolicy{Reference: "POL-R", Title: "Revue", Description: reportingSecret, Version: "1.0", Status: GovernancePolicyStatusInReview, CreatedBy: 1, UpdatedBy: 1},
		&GovernancePolicy{Reference: "POL-P", Title: "Publiée", Description: reportingSecret, Version: "1.0", Status: GovernancePolicyStatusPublished, EffectiveFrom: &now, OwningServiceID: &svc.ID, ApprovedAt: &publishedAt, PublishedAt: &publishedAt, CreatedBy: 1, UpdatedBy: 1},
		&GovernancePolicy{Reference: "POL-X", Title: "Retirée", Description: reportingSecret, Version: "1.0", Status: GovernancePolicyStatusRetired, EffectiveFrom: &now, ApprovedAt: &publishedAt, PublishedAt: &publishedAt, RetiredAt: &publishedAt, CreatedBy: 1, UpdatedBy: 1},
	)

	return reportingFixture{frameworkA: fwA.ID, frameworkB: fwB.ID, frameworkC: fwC.ID}
}

func requireBuckets(t *testing.T, name string, got []DashboardCount, want map[string]int64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s has %d buckets, want %d: %+v", name, len(got), len(want), got)
	}
	seen := map[string]bool{}
	for _, item := range got {
		wantCount, ok := want[item.Key]
		if !ok || item.Count != wantCount {
			t.Fatalf("%s[%s]=%d, want %d (ok=%v)", name, item.Key, item.Count, wantCount, ok)
		}
		seen[item.Key] = true
	}
	if len(seen) != len(want) {
		t.Fatalf("%s missing keys: %+v", name, got)
	}
}

func serviceTotal(rows []ReportingServiceCount, code string) int64 {
	for _, row := range rows {
		if row.Code == code {
			return row.Count
		}
	}
	return -1
}

func requireReportingDataset(t *testing.T, got *GRCReporting, windowDays int) {
	t.Helper()
	ex := got.Executive
	requireValue(t, "assets", ex.AssetsTotal, 2)
	requireValue(t, "openRisks", ex.OpenRisks, 3)
	requireValue(t, "highCritical", ex.HighCriticalOpenRisks, 2)
	requireValue(t, "activeFrameworks", ex.ActiveFrameworks, 2)
	requireValue(t, "applicable", ex.ApplicableRequirements, 2)
	requireValue(t, "mapped", ex.MappedRequirements, 1)
	if ex.MappingCoverage.Numerator != 1 || ex.MappingCoverage.Denominator != 2 || ex.MappingCoverage.Percent != 50 {
		t.Fatalf("coverage %+v", ex.MappingCoverage)
	}
	requireValue(t, "controls", ex.ControlsTotal, 2)
	requireValue(t, "implementations", ex.ImplementationsTotal, 1)
	requireValue(t, "evidenceExpired", ex.EvidenceExpired, 1)
	requireValue(t, "evidenceInvalid", ex.EvidenceInvalid, 1)
	requireValue(t, "overdueActions", ex.OverdueActions, 1)
	requireValue(t, "activeAudits", ex.ActiveAudits, 2)
	requireValue(t, "openFindings", ex.OpenFindings, 3)
	requireValue(t, "overdueFindings", ex.OverdueFindings, 1)
	requireValue(t, "activeIncidents", ex.ActiveIncidents, 2)
	requireValue(t, "published", ex.PublishedPolicies, 1)
	requireValue(t, "inReview", ex.PoliciesInReview, 1)
	if windowDays == 30 {
		requireValue(t, "expiring", ex.EvidenceExpiringSoon, 2)
		requireValue(t, "dueSoon", ex.DueSoonActions, 2)
	} else if windowDays == 1 {
		requireValue(t, "expiring", ex.EvidenceExpiringSoon, 1)
		requireValue(t, "dueSoon", ex.DueSoonActions, 1)
	}

	requireBuckets(t, "riskStatus", got.Risks.ByStatus, map[string]int64{
		RiskStatusDraft: 1, RiskStatusOpen: 2, RiskStatusInTreatment: 1, RiskStatusAccepted: 1, RiskStatusClosed: 1,
	})
	requireBuckets(t, "treatment", got.Risks.ByTreatment, map[string]int64{
		RiskTreatmentReduce: 2, RiskTreatmentAccept: 2, RiskTreatmentAvoid: 1, RiskTreatmentTransfer: 1,
	})
	requireBuckets(t, "residual", got.Risks.OpenByResidualLevel, map[string]int64{
		RiskLevelLow: 0, RiskLevelMedium: 1, RiskLevelHigh: 1, RiskLevelCritical: 1,
	})
	requireBuckets(t, "inherent", got.Risks.OpenByInherentLevel, map[string]int64{
		RiskLevelLow: 1, RiskLevelMedium: 1, RiskLevelHigh: 0, RiskLevelCritical: 1,
	})
	requireBuckets(t, "assetCriticality", got.Risks.OpenByAssetCriticality, map[string]int64{
		AssetCriticalityLow: 1, AssetCriticalityMedium: 0, AssetCriticalityHigh: 0, AssetCriticalityCritical: 1, reportingAssetCriticalityNone: 1,
	})
	if serviceTotal(got.Risks.OpenByOwningService, "RPT") != 2 || serviceTotal(got.Risks.OpenByOwningService, "") != 1 {
		t.Fatalf("risk services %+v", got.Risks.OpenByOwningService)
	}

	if len(got.Compliance.ByFramework) != 3 || got.Compliance.ByFramework[0].Code != "FW-A" || got.Compliance.MappingCoverage.Percent != 50 {
		t.Fatalf("compliance %+v", got.Compliance)
	}
	if got.Compliance.ActiveRequirements != 3 {
		t.Fatalf("active requirements %d", got.Compliance.ActiveRequirements)
	}

	requireValue(t, "openActions", got.Actions.Open, 5)
	if windowDays == 30 {
		requireBuckets(t, "due", got.Actions.ByDueState, map[string]int64{
			RemediationActionDueOverdue: 1, RemediationActionDueSoon: 2, RemediationActionDueUpcoming: 1,
			RemediationActionDueNoDueDate: 1, RemediationActionDueClosed: 1,
		})
	}
	requireBuckets(t, "openPriority", got.Actions.OpenByPriority, map[string]int64{
		RemediationActionPriorityLow: 1, RemediationActionPriorityMedium: 2,
		RemediationActionPriorityHigh: 1, RemediationActionPriorityCritical: 1,
	})
	requireValue(t, "auditsCompleted", got.Audits.Completed, 1)
	requireBuckets(t, "auditType", got.Audits.ByType, map[string]int64{
		AuditEngagementTypeInternal: 2, AuditEngagementTypeExternal: 1, AuditEngagementTypeVendor: 1,
	})
	requireBuckets(t, "findingStatus", got.Findings.ByStatus, map[string]int64{
		FindingStatusOpen: 1, FindingStatusInRemediation: 1, FindingStatusResolved: 1,
		FindingStatusClosed: 1, FindingStatusAccepted: 1,
	})
	requireValue(t, "policiesPublished", got.Policies.Published, 1)
	if serviceTotal(got.Policies.ByOwningService, "RPT") != 1 || serviceTotal(got.Policies.ByOwningService, "") != 3 {
		t.Fatalf("policy services %+v", got.Policies.ByOwningService)
	}
	if got.Evidence.Valid+got.Evidence.ExpiringSoon+got.Evidence.Expired+got.Evidence.Invalid != 5 {
		t.Fatalf("evidence states do not partition: %+v", got.Evidence)
	}
}

func TestReportingEmpty(t *testing.T) {
	_, svc := newReportingService(t)
	got, err := svc.Reporting(defaultReportingFilter())
	if err != nil {
		t.Fatal(err)
	}
	if got.Executive != (ReportingExecutive{}) || got.Compliance.MappingCoverage != (DashboardRatio{}) {
		t.Fatalf("empty reporting is not zero: %+v", got.Executive)
	}
	if got.FrameworkScope.AppliesTo != ReportingFrameworkScopeCompliance || len(got.FrameworkScope.FrameworkIDs) != 0 {
		t.Fatalf("scope %+v", got.FrameworkScope)
	}
	if got.Risks.ByStatus == nil || got.Compliance.ByFramework == nil || got.Evidence.ByValidity == nil || got.Policies.ByStatus == nil {
		t.Fatal("empty distributions must be arrays, not null")
	}
	if !got.GeneratedAt.Equal(dashboardTestNow) {
		t.Fatalf("generatedAt %s", got.GeneratedAt)
	}
	if got.Windows.EvidenceExpiringSoonDays != 30 || got.Windows.ActionDueSoonDays != 30 {
		t.Fatalf("windows %+v", got.Windows)
	}
}

func TestReportingDatasetAndWindows(t *testing.T) {
	db, svc := newReportingService(t)
	seed := seedReportingDataset(t, db, dashboardTestNow)
	got, err := svc.Reporting(defaultReportingFilter())
	if err != nil {
		t.Fatal(err)
	}
	requireReportingDataset(t, got, 30)

	narrow := defaultReportingFilter()
	narrow.EvidenceExpiringSoonDays = 1
	narrow.ActionDueSoonDays = 1
	got, err = svc.Reporting(narrow)
	if err != nil {
		t.Fatal(err)
	}
	requireReportingDataset(t, got, 1)
	if got.Executive.OpenRisks != 3 || got.Executive.HighCriticalOpenRisks != 2 {
		t.Fatal("windows must not change risk figures")
	}

	filtered := defaultReportingFilter()
	filtered.FrameworkIDs = []uint{seed.frameworkA, seed.frameworkA, 999999}
	got, err = svc.Reporting(filtered)
	if err != nil {
		t.Fatal(err)
	}
	if got.Executive.OpenRisks != 3 || got.Executive.ActiveIncidents != 2 || got.Executive.PublishedPolicies != 1 {
		t.Fatal("framework filter leaked outside compliance")
	}
	if got.Compliance.ApplicableRequirements != 2 || got.Compliance.MappedRequirements != 1 || len(got.Compliance.ByFramework) != 1 || got.Compliance.ByFramework[0].Code != "FW-A" {
		t.Fatalf("filtered compliance %+v", got.Compliance)
	}
	if got.Executive.ActiveFrameworks != 1 || got.Executive.MappingCoverage.Percent != 50 {
		t.Fatalf("executive compliance was not filtered: %+v", got.Executive)
	}

	unknown := defaultReportingFilter()
	unknown.FrameworkIDs = []uint{999999}
	got, err = svc.Reporting(unknown)
	if err != nil {
		t.Fatal(err)
	}
	if got.Compliance.ApplicableRequirements != 0 || len(got.Compliance.ByFramework) != 0 || got.Executive.OpenRisks != 3 {
		t.Fatalf("unknown framework %+v risks %d", got.Compliance, got.Executive.OpenRisks)
	}
	if _, err = svc.Reporting(ReportingFilter{EvidenceExpiringSoonDays: 0, ActionDueSoonDays: 30}); err == nil {
		t.Fatal("window 0 must be rejected")
	}
	if _, err = svc.Reporting(ReportingFilter{EvidenceExpiringSoonDays: 30, ActionDueSoonDays: 366}); err == nil {
		t.Fatal("window 366 must be rejected")
	}
	_ = seed.frameworkB
	_ = seed.frameworkC
}

func TestReportingOmitsFreeText(t *testing.T) {
	db, svc := newReportingService(t)
	seedReportingDataset(t, db, dashboardTestNow)
	got, err := svc.Reporting(defaultReportingFilter())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got.Compliance.ByFramework[0].Name, reportingSecret) {
		t.Fatal("framework name must not carry the secret")
	}
	if got.Risks.OpenByOwningService[0].Name == reportingSecret {
		t.Fatal("service name must not carry the secret")
	}
}
