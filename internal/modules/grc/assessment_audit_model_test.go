package grc

import (
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/organization"
	"github.com/lallene/medcore-his/backend/internal/modules/staff"
)

type assessmentFixture struct {
	evidenceFixture
	evidence Evidence
}

// newAssessmentFixture extends the evidence fixture (SQLite with foreign keys
// enabled) with the GRC-01D tables and one evidence.
func newAssessmentFixture(t *testing.T) assessmentFixture {
	t.Helper()

	base := newEvidenceFixture(t)
	if err := base.db.AutoMigrate(&AuditEngagement{}, &Assessment{}, &AssessmentEvidence{}, &Finding{}); err != nil {
		t.Fatal(err)
	}

	f := assessmentFixture{evidenceFixture: base, evidence: validEvidence(base, "EVD-ASSESS")}
	if err := base.db.Create(&f.evidence).Error; err != nil {
		t.Fatal(err)
	}
	return f
}

func stringPtr(value string) *string {
	return &value
}

var assessmentTestTime = time.Date(2026, 10, 25, 10, 0, 0, 0, time.UTC)

func (f assessmentFixture) draftAssessment(reference string) Assessment {
	return Assessment{
		Reference:               reference,
		Title:                   "Évaluation " + reference,
		ControlImplementationID: f.implementation.ID,
		Type:                    AssessmentTypeOperatingEffectiveness,
		Status:                  AssessmentStatusDraft,
		CreatedBy:               1,
		UpdatedBy:               1,
	}
}

func (f assessmentFixture) completedAssessment(reference string) Assessment {
	a := f.draftAssessment(reference)
	a.Status = AssessmentStatusCompleted
	a.Result = stringPtr(AssessmentResultEffective)
	a.AssessorProfileID = &f.profile.ID
	a.AssessedAt = timePtr(assessmentTestTime)
	a.CompletedAt = timePtr(assessmentTestTime)
	return a
}

func (f assessmentFixture) auditEngagement(reference string) AuditEngagement {
	return AuditEngagement{
		Reference: reference,
		Title:     "Audit " + reference,
		Type:      AuditEngagementTypeInternal,
		Status:    AuditEngagementStatusPlanned,
		CreatedBy: 1,
		UpdatedBy: 1,
	}
}

func (f assessmentFixture) createAuditEngagement(t *testing.T, reference string) AuditEngagement {
	t.Helper()

	audit := f.auditEngagement(reference)
	if err := f.db.Create(&audit).Error; err != nil {
		t.Fatal(err)
	}
	return audit
}

func (f assessmentFixture) finding(reference string, auditID uint) Finding {
	return Finding{
		Reference:         reference,
		Title:             "Constat " + reference,
		AuditEngagementID: &auditID,
		Category:          FindingCategoryControlDeficiency,
		Severity:          FindingSeverityHigh,
		Status:            FindingStatusOpen,
		CreatedBy:         1,
		UpdatedBy:         1,
	}
}

func requireCreate(t *testing.T, f assessmentFixture, name string, value any) {
	t.Helper()
	if err := f.db.Create(value).Error; err != nil {
		t.Fatalf("%s rejected: %v", name, err)
	}
}

func requireRejected(t *testing.T, f assessmentFixture, name string, value any) {
	t.Helper()
	if err := f.db.Create(value).Error; err == nil {
		t.Fatalf("%s must be rejected by the database", name)
	}
}

func TestAssessmentAuditModelTableNamesAndComputedFields(t *testing.T) {
	for want, model := range map[string]interface{ TableName() string }{
		"grc_assessments":         Assessment{},
		"grc_assessment_evidence": AssessmentEvidence{},
		"grc_audit_engagements":   AuditEngagement{},
		"grc_findings":            Finding{},
	} {
		if got := model.TableName(); got != want {
			t.Fatalf("TableName() = %q, want %q", got, want)
		}
	}

	f := newAssessmentFixture(t)
	migrator := f.db.Migrator()
	for _, column := range []string{"reassessment_due", "has_invalid_evidence", "requirement_id"} {
		if migrator.HasColumn(&Assessment{}, column) {
			t.Errorf("grc_assessments must not have column %s", column)
		}
	}
	for _, column := range []string{"status", "expires_at", "external_reference"} {
		if migrator.HasColumn(&AssessmentEvidence{}, column) {
			t.Errorf("grc_assessment_evidence must not copy evidence column %s", column)
		}
	}
	if migrator.HasColumn(&Finding{}, "overdue") {
		t.Error("grc_findings must not have column overdue")
	}
}

func TestAssessmentModelEnumsAndReference(t *testing.T) {
	f := newAssessmentFixture(t)

	for i, value := range []string{AssessmentTypeDesign, AssessmentTypeOperatingEffectiveness, AssessmentTypeCompliance} {
		a := f.draftAssessment("ASM-TYPE-" + string(rune('A'+i)))
		a.Type = value
		requireCreate(t, f, "type "+value, &a)
	}
	for _, value := range []string{"IMPLEMENTATION", "PERIODIC_REVIEW", "design"} {
		a := f.draftAssessment("ASM-BAD-TYPE-" + value)
		a.Type = value
		requireRejected(t, f, "type "+value, &a)
	}

	for _, value := range []string{"IN_REVIEW", "APPROVED", "draft"} {
		a := f.draftAssessment("ASM-BAD-STATUS-" + value)
		a.Status = value
		requireRejected(t, f, "status "+value, &a)
	}
	cancelled := f.draftAssessment("ASM-CANCELLED")
	cancelled.Status = AssessmentStatusCancelled
	requireCreate(t, f, "cancelled assessment", &cancelled)

	for i, value := range []string{
		AssessmentResultEffective,
		AssessmentResultPartiallyEffective,
		AssessmentResultIneffective,
	} {
		a := f.draftAssessment("ASM-RESULT-" + string(rune('A'+i)))
		a.Result = stringPtr(value)
		requireCreate(t, f, "draft result "+value, &a)
	}
	noResult := f.draftAssessment("ASM-NO-RESULT")
	requireCreate(t, f, "draft without result", &noResult)

	for _, value := range []string{"NOT_ASSESSED", "UNKNOWN"} {
		a := f.draftAssessment("ASM-BAD-RESULT-" + value)
		a.Result = stringPtr(value)
		requireRejected(t, f, "result "+value, &a)
	}

	duplicate := f.draftAssessment("ASM-RESULT-A")
	requireRejected(t, f, "duplicate reference", &duplicate)
}

func TestAssessmentModelCompletionChecks(t *testing.T) {
	f := newAssessmentFixture(t)

	completed := f.completedAssessment("ASM-COMPLETED")
	requireCreate(t, f, "completed assessment", &completed)

	cases := map[string]func(*Assessment){
		"completed without completed_at": func(a *Assessment) { a.CompletedAt = nil },
		"completed without result":       func(a *Assessment) { a.Result = nil },
		"completed without assessor":     func(a *Assessment) { a.AssessorProfileID = nil },
		"completed without assessed_at":  func(a *Assessment) { a.AssessedAt = nil },
	}
	for name, mutate := range cases {
		a := f.completedAssessment("ASM-BAD-" + name)
		mutate(&a)
		requireRejected(t, f, name, &a)
	}

	draftWithCompletion := f.draftAssessment("ASM-DRAFT-COMPLETED-AT")
	draftWithCompletion.CompletedAt = timePtr(assessmentTestTime)
	requireRejected(t, f, "draft with completed_at", &draftWithCompletion)

	cancelledWithCompletion := f.draftAssessment("ASM-CANCELLED-COMPLETED-AT")
	cancelledWithCompletion.Status = AssessmentStatusCancelled
	cancelledWithCompletion.CompletedAt = timePtr(assessmentTestTime)
	requireRejected(t, f, "cancelled with completed_at", &cancelledWithCompletion)

	// AssessedAt in the future is a service rule, not a database rule.
	future := f.completedAssessment("ASM-FUTURE-ASSESSED")
	future.AssessedAt = timePtr(time.Now().AddDate(10, 0, 0))
	future.CompletedAt = future.AssessedAt
	requireCreate(t, f, "future assessed_at (service rule only)", &future)
}

func TestAssessmentModelNextRangeAndNotApplicableReason(t *testing.T) {
	f := newAssessmentFixture(t)

	for name, next := range map[string]*time.Time{
		"none":  nil,
		"equal": timePtr(assessmentTestTime),
		"later": timePtr(assessmentTestTime.AddDate(1, 0, 0)),
	} {
		a := f.completedAssessment("ASM-NEXT-" + name)
		a.NextAssessmentAt = next
		requireCreate(t, f, "next "+name, &a)
	}

	nextWithoutAssessed := f.draftAssessment("ASM-NEXT-NO-ASSESSED")
	nextWithoutAssessed.NextAssessmentAt = timePtr(assessmentTestTime)
	requireCreate(t, f, "next without assessed_at", &nextWithoutAssessed)

	before := f.completedAssessment("ASM-NEXT-BEFORE")
	before.NextAssessmentAt = timePtr(assessmentTestTime.Add(-time.Second))
	requireRejected(t, f, "next before assessed_at", &before)

	justified := f.completedAssessment("ASM-NA-JUSTIFIED")
	justified.Result = stringPtr(AssessmentResultNotApplicable)
	justified.Summary = "Actif hors périmètre du contrôle"
	requireCreate(t, f, "justified NOT_APPLICABLE", &justified)

	for name, summary := range map[string]string{"empty": "", "spaces": "   "} {
		a := f.draftAssessment("ASM-NA-" + name)
		a.Result = stringPtr(AssessmentResultNotApplicable)
		a.Summary = summary
		requireRejected(t, f, "NOT_APPLICABLE with "+name+" summary", &a)
	}

	nullSummary := f.draftAssessment("ASM-NA-NULL")
	nullSummary.Result = stringPtr(AssessmentResultNotApplicable)
	nullSummary.Summary = "justification"
	requireCreate(t, f, "NOT_APPLICABLE with summary", &nullSummary)
	if err := f.db.Exec("UPDATE grc_assessments SET summary = NULL WHERE id = ?", nullSummary.ID).Error; err == nil {
		t.Fatal("NOT_APPLICABLE with NULL summary must be rejected")
	}
}

func TestAssessmentModelForeignKeys(t *testing.T) {
	f := newAssessmentFixture(t)

	requireRestrictConstraints(t, f.db, &Assessment{}, "ControlImplementation", "AssessorProfile")

	unknown := uint(999999)
	orphan := f.draftAssessment("ASM-ORPHAN")
	orphan.ControlImplementationID = unknown
	requireRejected(t, f, "unknown control implementation", &orphan)

	badAssessor := f.draftAssessment("ASM-BAD-ASSESSOR")
	badAssessor.AssessorProfileID = &unknown
	requireRejected(t, f, "unknown assessor", &badAssessor)

	linked := f.completedAssessment("ASM-LINKED")
	requireCreate(t, f, "linked assessment", &linked)

	if err := f.db.Delete(&ControlImplementation{}, f.implementation.ID).Error; err == nil {
		t.Fatal("deleting an assessed control implementation must be restricted")
	}
	if err := f.db.Delete(&staff.Profile{}, f.profile.ID).Error; err == nil {
		t.Fatal("deleting an assessor profile must be restricted")
	}
}

func TestAssessmentEvidenceModel(t *testing.T) {
	f := newAssessmentFixture(t)

	requireRestrictConstraints(t, f.db, &AssessmentEvidence{}, "Assessment", "Evidence")

	assessment := f.draftAssessment("ASM-EVIDENCE")
	requireCreate(t, f, "assessment", &assessment)

	link := AssessmentEvidence{AssessmentID: assessment.ID, EvidenceID: f.evidence.ID, CreatedBy: 1}
	requireCreate(t, f, "assessment evidence link", &link)

	duplicate := AssessmentEvidence{AssessmentID: assessment.ID, EvidenceID: f.evidence.ID, CreatedBy: 1}
	requireRejected(t, f, "duplicate assessment evidence link", &duplicate)

	unknown := uint(999999)
	requireRejected(t, f, "unknown assessment", &AssessmentEvidence{AssessmentID: unknown, EvidenceID: f.evidence.ID, CreatedBy: 1})
	requireRejected(t, f, "unknown evidence", &AssessmentEvidence{AssessmentID: assessment.ID, EvidenceID: unknown, CreatedBy: 1})

	if err := f.db.Delete(&Assessment{}, assessment.ID).Error; err == nil {
		t.Fatal("deleting an assessment with linked evidence must be restricted")
	}
	if err := f.db.Delete(&Evidence{}, f.evidence.ID).Error; err == nil {
		t.Fatal("deleting an examined evidence must be restricted")
	}
}

func TestAuditEngagementModelChecks(t *testing.T) {
	f := newAssessmentFixture(t)

	for i, value := range []string{AuditEngagementTypeInternal, AuditEngagementTypeExternal, AuditEngagementTypeVendor} {
		a := f.auditEngagement("AUD-TYPE-" + string(rune('A'+i)))
		a.Type = value
		requireCreate(t, f, "type "+value, &a)
	}
	for _, value := range []string{"COMPLIANCE", "SECURITY", "PROCESS", "internal"} {
		a := f.auditEngagement("AUD-BAD-TYPE-" + value)
		a.Type = value
		requireRejected(t, f, "type "+value, &a)
	}

	start := assessmentTestTime
	end := start.Add(8 * time.Hour)

	valid := map[string]func(*AuditEngagement){
		"planned without dates": func(a *AuditEngagement) {},
		"planned range":         func(a *AuditEngagement) { a.PlannedStartAt, a.PlannedEndAt = &start, &end },
		"in progress": func(a *AuditEngagement) {
			a.Status = AuditEngagementStatusInProgress
			a.StartedAt = &start
		},
		"completed": func(a *AuditEngagement) {
			a.Status = AuditEngagementStatusCompleted
			a.StartedAt, a.CompletedAt = &start, &end
		},
		"completed same instant": func(a *AuditEngagement) {
			a.Status = AuditEngagementStatusCompleted
			a.StartedAt, a.CompletedAt = &start, &start
		},
		"cancelled": func(a *AuditEngagement) { a.Status = AuditEngagementStatusCancelled },
	}
	for name, mutate := range valid {
		a := f.auditEngagement("AUD-OK-" + name)
		mutate(&a)
		requireCreate(t, f, name, &a)
	}

	invalid := map[string]func(*AuditEngagement){
		"draft status":                func(a *AuditEngagement) { a.Status = "DRAFT" },
		"planned end before start":    func(a *AuditEngagement) { a.PlannedStartAt, a.PlannedEndAt = &end, &start },
		"in progress without started": func(a *AuditEngagement) { a.Status = AuditEngagementStatusInProgress },
		"completed without started": func(a *AuditEngagement) {
			a.Status = AuditEngagementStatusCompleted
			a.CompletedAt = &end
		},
		"completed without completed_at": func(a *AuditEngagement) {
			a.Status = AuditEngagementStatusCompleted
			a.StartedAt = &start
		},
		"in progress with completed_at": func(a *AuditEngagement) {
			a.Status = AuditEngagementStatusInProgress
			a.StartedAt, a.CompletedAt = &start, &end
		},
		"completed before started": func(a *AuditEngagement) {
			a.Status = AuditEngagementStatusCompleted
			a.StartedAt, a.CompletedAt = &end, &start
		},
	}
	for name, mutate := range invalid {
		a := f.auditEngagement("AUD-BAD-" + name)
		mutate(&a)
		requireRejected(t, f, name, &a)
	}

	duplicate := f.auditEngagement("AUD-TYPE-A")
	requireRejected(t, f, "duplicate reference", &duplicate)
}

func TestAuditEngagementModelForeignKeys(t *testing.T) {
	f := newAssessmentFixture(t)

	requireRestrictConstraints(t, f.db, &AuditEngagement{}, "LeadAuditorProfile", "OwningService")

	unknown := uint(999999)
	badLead := f.auditEngagement("AUD-BAD-LEAD")
	badLead.LeadAuditorProfileID = &unknown
	requireRejected(t, f, "unknown lead auditor", &badLead)

	badService := f.auditEngagement("AUD-BAD-SERVICE")
	badService.OwningServiceID = &unknown
	requireRejected(t, f, "unknown owning service", &badService)

	linked := f.auditEngagement("AUD-LINKED")
	linked.LeadAuditorProfileID = &f.profile.ID
	linked.OwningServiceID = &f.service.ID
	requireCreate(t, f, "linked audit", &linked)

	if err := f.db.Delete(&staff.Profile{}, f.profile.ID).Error; err == nil {
		t.Fatal("deleting a lead auditor must be restricted")
	}
	if err := f.db.Delete(&organization.Service{}, f.service.ID).Error; err == nil {
		t.Fatal("deleting an audited service must be restricted")
	}
}

func TestFindingModelSourceAndEnums(t *testing.T) {
	f := newAssessmentFixture(t)
	audit := f.createAuditEngagement(t, "AUD-FINDING")
	assessment := f.completedAssessment("ASM-FINDING")
	requireCreate(t, f, "assessment", &assessment)

	fromAudit := f.finding("FND-AUDIT", audit.ID)
	requireCreate(t, f, "finding from audit", &fromAudit)

	fromAssessment := f.finding("FND-ASSESSMENT", audit.ID)
	fromAssessment.AuditEngagementID = nil
	fromAssessment.AssessmentID = &assessment.ID
	fromAssessment.ControlImplementationID = &f.implementation.ID
	requireCreate(t, f, "finding from assessment", &fromAssessment)

	noSource := f.finding("FND-NO-SOURCE", audit.ID)
	noSource.AuditEngagementID = nil
	requireRejected(t, f, "finding without source", &noSource)

	bothSources := f.finding("FND-BOTH", audit.ID)
	bothSources.AssessmentID = &assessment.ID
	requireRejected(t, f, "finding with two sources", &bothSources)

	for i, value := range []string{
		FindingCategoryNonConformity, FindingCategoryControlDeficiency,
		FindingCategoryObservation, FindingCategoryImprovement,
	} {
		finding := f.finding("FND-CAT-"+string(rune('A'+i)), audit.ID)
		finding.Category = value
		requireCreate(t, f, "category "+value, &finding)
	}
	for i, value := range []string{FindingSeverityLow, FindingSeverityMedium, FindingSeverityHigh, FindingSeverityCritical} {
		finding := f.finding("FND-SEV-"+string(rune('A'+i)), audit.ID)
		finding.Severity = value
		requireCreate(t, f, "severity "+value, &finding)
	}

	invalid := map[string]func(*Finding){
		"category RISK":  func(x *Finding) { x.Category = "RISK" },
		"severity INFO":  func(x *Finding) { x.Severity = "INFO" },
		"status UNKNOWN": func(x *Finding) { x.Status = "REJECTED" },
		"lowercase":      func(x *Finding) { x.Status = "open" },
	}
	for name, mutate := range invalid {
		finding := f.finding("FND-BAD-"+name, audit.ID)
		mutate(&finding)
		requireRejected(t, f, name, &finding)
	}

	duplicate := f.finding("FND-AUDIT", audit.ID)
	requireRejected(t, f, "duplicate reference", &duplicate)
}

func TestFindingModelWorkflowChecks(t *testing.T) {
	f := newAssessmentFixture(t)
	audit := f.createAuditEngagement(t, "AUD-WORKFLOW")
	resolved := assessmentTestTime
	closed := resolved.Add(time.Hour)

	valid := map[string]func(*Finding){
		"open":           func(x *Finding) {},
		"in remediation": func(x *Finding) { x.Status = FindingStatusInRemediation },
		"resolved":       func(x *Finding) { x.Status, x.ResolvedAt = FindingStatusResolved, &resolved },
		"closed": func(x *Finding) {
			x.Status, x.ResolvedAt, x.ClosedAt = FindingStatusClosed, &resolved, &closed
		},
		"accepted with note": func(x *Finding) {
			x.Status, x.ClosedAt, x.ClosureNote = FindingStatusAccepted, &closed, "Risque accepté par la direction"
		},
		"accepted after resolution": func(x *Finding) {
			x.Status, x.ResolvedAt, x.ClosedAt, x.ClosureNote = FindingStatusAccepted, &resolved, &closed, "Accepté"
		},
	}
	for name, mutate := range valid {
		finding := f.finding("FND-OK-"+name, audit.ID)
		mutate(&finding)
		requireCreate(t, f, name, &finding)
	}

	invalid := map[string]func(*Finding){
		"resolved without resolved_at": func(x *Finding) { x.Status = FindingStatusResolved },
		"closed without resolved_at":   func(x *Finding) { x.Status, x.ClosedAt = FindingStatusClosed, &closed },
		"closed without closed_at":     func(x *Finding) { x.Status, x.ResolvedAt = FindingStatusClosed, &resolved },
		"open with closed_at":          func(x *Finding) { x.ClosedAt = &closed },
		"resolved with closed_at": func(x *Finding) {
			x.Status, x.ResolvedAt, x.ClosedAt = FindingStatusResolved, &resolved, &closed
		},
		"accepted without note": func(x *Finding) { x.Status, x.ClosedAt = FindingStatusAccepted, &closed },
		"accepted with blank note": func(x *Finding) {
			x.Status, x.ClosedAt, x.ClosureNote = FindingStatusAccepted, &closed, "   "
		},
		"accepted without closed_at": func(x *Finding) { x.Status, x.ClosureNote = FindingStatusAccepted, "Accepté" },
		"closed before resolved": func(x *Finding) {
			x.Status, x.ResolvedAt, x.ClosedAt = FindingStatusClosed, &closed, &resolved
		},
	}
	for name, mutate := range invalid {
		finding := f.finding("FND-BAD-"+name, audit.ID)
		mutate(&finding)
		requireRejected(t, f, name, &finding)
	}
}

func TestFindingModelForeignKeys(t *testing.T) {
	f := newAssessmentFixture(t)

	requireRestrictConstraints(t, f.db, &Finding{},
		"AuditEngagement", "Assessment", "ControlImplementation", "Risk", "OwnerProfile", "OwningService")

	audit := f.createAuditEngagement(t, "AUD-FK")
	unknown := uint(999999)

	for name, mutate := range map[string]func(*Finding){
		"unknown audit":                  func(x *Finding) { x.AuditEngagementID = &unknown },
		"unknown assessment":             func(x *Finding) { x.AuditEngagementID, x.AssessmentID = nil, &unknown },
		"unknown control implementation": func(x *Finding) { x.ControlImplementationID = &unknown },
		"unknown risk":                   func(x *Finding) { x.RiskID = &unknown },
		"unknown owner":                  func(x *Finding) { x.OwnerProfileID = &unknown },
		"unknown service":                func(x *Finding) { x.OwningServiceID = &unknown },
	} {
		finding := f.finding("FND-FK-"+name, audit.ID)
		mutate(&finding)
		requireRejected(t, f, name, &finding)
	}

	linked := f.finding("FND-LINKED", audit.ID)
	linked.ControlImplementationID = &f.implementation.ID
	linked.RiskID = &f.risk.ID
	linked.OwnerProfileID = &f.profile.ID
	linked.OwningServiceID = &f.service.ID
	requireCreate(t, f, "linked finding", &linked)

	assessment := f.completedAssessment("ASM-FK")
	requireCreate(t, f, "assessment", &assessment)
	fromAssessment := f.finding("FND-FROM-ASSESSMENT", audit.ID)
	fromAssessment.AuditEngagementID = nil
	fromAssessment.AssessmentID = &assessment.ID
	requireCreate(t, f, "finding from assessment", &fromAssessment)

	deletions := map[string]func() error{
		"audit engagement":       func() error { return f.db.Delete(&AuditEngagement{}, audit.ID).Error },
		"assessment":             func() error { return f.db.Delete(&Assessment{}, assessment.ID).Error },
		"control implementation": func() error { return f.db.Delete(&ControlImplementation{}, f.implementation.ID).Error },
		"risk":                   func() error { return f.db.Delete(&Risk{}, f.risk.ID).Error },
		"owner profile":          func() error { return f.db.Delete(&staff.Profile{}, f.profile.ID).Error },
		"owning service":         func() error { return f.db.Delete(&organization.Service{}, f.service.ID).Error },
	}
	for name, deleteFn := range deletions {
		if err := deleteFn(); err == nil {
			t.Fatalf("deleting a referenced %s must be restricted", name)
		}
	}
}
