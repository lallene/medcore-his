package grc

import (
	"net/http"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/organization"
	"github.com/lallene/medcore-his/backend/internal/modules/staff"
)

// The service clock of these fixtures is evidenceTestNow (see
// evidence_service_test.go); tests move it explicitly, never with sleeps.

type grc01DServiceFixture struct {
	remediationActionFixture
}

func newGRC01DServiceFixture(t *testing.T) grc01DServiceFixture {
	t.Helper()

	base := newRemediationActionFixture(t)
	if err := base.db.AutoMigrate(&AuditEngagement{}, &Assessment{}, &AssessmentEvidence{}, &Finding{}, &SecurityIncident{}); err != nil {
		t.Fatal(err)
	}
	return grc01DServiceFixture{remediationActionFixture: base}
}

func (f grc01DServiceFixture) setClock(at time.Time) {
	f.svc.now = func() time.Time { return at }
}

func (f grc01DServiceFixture) auditRequest(reference string) AuditEngagementRequest {
	return AuditEngagementRequest{
		Reference: reference,
		Title:     "Audit " + reference,
		Type:      AuditEngagementTypeInternal,
		Status:    AuditEngagementStatusPlanned,
	}
}

func (f grc01DServiceFixture) createAudit(t *testing.T, r AuditEngagementRequest) AuditEngagement {
	t.Helper()
	audit, err := f.svc.SaveAuditEngagement(0, 10, r)
	if err != nil {
		t.Fatal(err)
	}
	return *audit
}

func (f grc01DServiceFixture) assessmentRequest(reference string) AssessmentRequest {
	return AssessmentRequest{
		Reference:               reference,
		Title:                   "Évaluation " + reference,
		ControlImplementationID: f.implementation.ID,
		Type:                    AssessmentTypeOperatingEffectiveness,
		Status:                  AssessmentStatusDraft,
	}
}

func (f grc01DServiceFixture) completionOf(r AssessmentRequest) AssessmentRequest {
	r.Status = AssessmentStatusCompleted
	r.Result = stringPtr(AssessmentResultEffective)
	r.AssessorProfileID = &f.profile.ID
	r.AssessedAt = timePtr(evidenceTestNow.Add(-time.Hour))
	return r
}

func (f grc01DServiceFixture) createAssessment(t *testing.T, r AssessmentRequest) Assessment {
	t.Helper()
	assessment, err := f.svc.SaveAssessment(0, 10, r)
	if err != nil {
		t.Fatal(err)
	}
	return *assessment
}

func (f grc01DServiceFixture) completedAssessment(t *testing.T, reference string) Assessment {
	t.Helper()
	r := f.assessmentRequest(reference)
	created := f.createAssessment(t, r)
	completed, err := f.svc.SaveAssessment(created.ID, 10, f.completionOf(r))
	if err != nil {
		t.Fatal(err)
	}
	return *completed
}

func (f grc01DServiceFixture) validEvidence(t *testing.T, reference string, implementationID uint) Evidence {
	t.Helper()
	r := f.request(reference)
	r.ControlImplementationID = implementationID
	r.Status = EvidenceStatusValid
	return f.create(t, r)
}

func (f grc01DServiceFixture) inProgressAudit(t *testing.T, reference string) AuditEngagement {
	t.Helper()
	r := f.auditRequest(reference)
	r.Status = AuditEngagementStatusInProgress
	return f.createAudit(t, r)
}

func (f grc01DServiceFixture) findingRequest(reference string, auditID uint) FindingRequest {
	return FindingRequest{
		Reference:         reference,
		Title:             "Constat " + reference,
		AuditEngagementID: &auditID,
		Category:          FindingCategoryControlDeficiency,
		Severity:          FindingSeverityHigh,
		Status:            FindingStatusOpen,
	}
}

func (f grc01DServiceFixture) createFinding(t *testing.T, r FindingRequest) Finding {
	t.Helper()
	finding, err := f.svc.SaveFinding(0, 10, r)
	if err != nil {
		t.Fatal(err)
	}
	return *finding
}

func deactivate(t *testing.T, f grc01DServiceFixture, profile *staff.Profile, service *organization.Service) {
	t.Helper()
	if profile != nil {
		if err := f.db.Model(&staff.Profile{}).Where("id = ?", profile.ID).Update("active", false).Error; err != nil {
			t.Fatal(err)
		}
	}
	if service != nil {
		if err := f.db.Model(&organization.Service{}).Where("id = ?", service.ID).Update("active", false).Error; err != nil {
			t.Fatal(err)
		}
	}
}

// --- AuditEngagement ---

func TestSaveAuditEngagementCreation(t *testing.T) {
	f := newGRC01DServiceFixture(t)

	r := f.auditRequest(" aud-001 ")
	r.LeadAuditorProfileID = &f.profile.ID
	r.OwningServiceID = &f.service.ID
	r.PlannedStartAt = timePtr(evidenceTestNow.AddDate(0, 0, 7))
	planned, err := f.svc.SaveAuditEngagement(0, 41, r)
	if err != nil {
		t.Fatal(err)
	}
	if planned.Reference != "AUD-001" || planned.Status != AuditEngagementStatusPlanned ||
		planned.StartedAt != nil || planned.CompletedAt != nil ||
		planned.CreatedBy != 41 || planned.UpdatedBy != 41 ||
		planned.LeadAuditorProfile == nil || planned.OwningService == nil {
		t.Fatalf("unexpected planned audit: %+v", planned)
	}

	started := f.inProgressAudit(t, "AUD-STARTED")
	if started.StartedAt == nil || !started.StartedAt.Equal(evidenceTestNow) {
		t.Fatalf("StartedAt must default to the server clock: %v", started.StartedAt)
	}

	past := f.auditRequest("AUD-PAST")
	past.Status = AuditEngagementStatusInProgress
	past.StartedAt = timePtr(evidenceTestNow.Add(-24 * time.Hour))
	if got := f.createAudit(t, past); !got.StartedAt.Equal(*past.StartedAt) {
		t.Fatalf("provided StartedAt not kept: %v", got.StartedAt)
	}

	atNow := f.auditRequest("AUD-AT-NOW")
	atNow.Status = AuditEngagementStatusInProgress
	atNow.StartedAt = timePtr(evidenceTestNow)
	f.createAudit(t, atNow)

	f.createAudit(t, f.auditRequest("AUD-DUP"))
	unknown := uint(999999)
	cases := []struct {
		name   string
		status int
		mutate func(*AuditEngagementRequest)
	}{
		{"future started", http.StatusBadRequest, func(r *AuditEngagementRequest) {
			r.Status = AuditEngagementStatusInProgress
			r.StartedAt = timePtr(evidenceTestNow.Add(time.Second))
		}},
		{"planned with started", http.StatusBadRequest, func(r *AuditEngagementRequest) { r.StartedAt = timePtr(evidenceTestNow) }},
		{"initial completed", http.StatusBadRequest, func(r *AuditEngagementRequest) { r.Status = AuditEngagementStatusCompleted }},
		{"initial cancelled", http.StatusBadRequest, func(r *AuditEngagementRequest) { r.Status = AuditEngagementStatusCancelled }},
		{"duplicate", http.StatusConflict, func(r *AuditEngagementRequest) { r.Reference = " aud-dup " }},
		{"unknown lead", http.StatusNotFound, func(r *AuditEngagementRequest) { r.LeadAuditorProfileID = &unknown }},
		{"inactive lead", http.StatusConflict, func(r *AuditEngagementRequest) { r.LeadAuditorProfileID = &f.inactiveProfile.ID }},
		{"unknown service", http.StatusNotFound, func(r *AuditEngagementRequest) { r.OwningServiceID = &unknown }},
		{"inactive service", http.StatusConflict, func(r *AuditEngagementRequest) { r.OwningServiceID = &f.inactiveService.ID }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := f.auditRequest("AUD-NEW")
			tc.mutate(&r)
			_, err := f.svc.SaveAuditEngagement(0, 10, r)
			expectAppStatus(t, err, tc.status)
		})
	}
}

func TestSaveAuditEngagementLifecycle(t *testing.T) {
	f := newGRC01DServiceFixture(t)
	r := f.auditRequest("AUD-LIFE")
	audit := f.createAudit(t, r)

	_, err := f.svc.SaveAuditEngagement(audit.ID, 20, func() AuditEngagementRequest {
		next := r
		next.Status = AuditEngagementStatusCompleted
		return next
	}())
	expectAppStatus(t, err, http.StatusConflict) // PLANNED -> COMPLETED

	r.Status = AuditEngagementStatusInProgress
	inProgress, err := f.svc.SaveAuditEngagement(audit.ID, 21, r)
	if err != nil {
		t.Fatal(err)
	}
	if inProgress.StartedAt == nil || !inProgress.StartedAt.Equal(evidenceTestNow) || inProgress.CompletedAt != nil {
		t.Fatalf("PLANNED -> IN_PROGRESS must set StartedAt only: %+v", inProgress)
	}

	// Editing without StartedAt keeps the recorded start.
	f.setClock(evidenceTestNow.Add(2 * time.Hour))
	r.Title = "Audit modifié"
	edited, err := f.svc.SaveAuditEngagement(audit.ID, 22, r)
	if err != nil {
		t.Fatal(err)
	}
	if !edited.StartedAt.Equal(evidenceTestNow) || edited.CompletedAt != nil {
		t.Fatalf("edit must keep StartedAt and no CompletedAt: %+v", edited)
	}

	r.Status = AuditEngagementStatusCompleted
	completed, err := f.svc.SaveAuditEngagement(audit.ID, 23, r)
	if err != nil {
		t.Fatal(err)
	}
	if completed.CompletedAt == nil || !completed.CompletedAt.Equal(evidenceTestNow.Add(2*time.Hour)) ||
		!completed.StartedAt.Equal(evidenceTestNow) || completed.CreatedBy != 10 || completed.UpdatedBy != 23 {
		t.Fatalf("IN_PROGRESS -> COMPLETED must set CompletedAt from the clock: %+v", completed)
	}

	for _, status := range []string{AuditEngagementStatusCompleted, AuditEngagementStatusInProgress, AuditEngagementStatusCancelled} {
		next := r
		next.Status = status
		_, err := f.svc.SaveAuditEngagement(audit.ID, 24, next)
		expectAppStatus(t, err, http.StatusConflict)
	}

	cancelledRequest := f.auditRequest("AUD-CANCEL")
	cancelled := f.createAudit(t, cancelledRequest)
	cancelledRequest.Status = AuditEngagementStatusCancelled
	if got, err := f.svc.SaveAuditEngagement(cancelled.ID, 25, cancelledRequest); err != nil || got.Status != AuditEngagementStatusCancelled {
		t.Fatalf("PLANNED -> CANCELLED: %v %+v", err, got)
	}
	cancelledRequest.Status = AuditEngagementStatusPlanned
	_, err = f.svc.SaveAuditEngagement(cancelled.ID, 26, cancelledRequest)
	expectAppStatus(t, err, http.StatusConflict)

	// Cancelling an audit in progress keeps its real start.
	running := f.inProgressAudit(t, "AUD-RUNNING")
	cancelRunning := f.auditRequest("AUD-RUNNING")
	cancelRunning.Status = AuditEngagementStatusCancelled
	if got, err := f.svc.SaveAuditEngagement(running.ID, 27, cancelRunning); err != nil || got.StartedAt == nil {
		t.Fatalf("IN_PROGRESS -> CANCELLED must keep StartedAt: %v %+v", err, got)
	}

	// Historical lead auditor and service stay editable.
	lead := createExtraProfile(t, f.evidenceServiceFixture, "GRC-LEAD")
	service := createExtraService(t, f.evidenceServiceFixture, "AUD-SVC")
	hist := f.auditRequest("AUD-HIST")
	hist.LeadAuditorProfileID = &lead.ID
	hist.OwningServiceID = &service.ID
	histAudit := f.createAudit(t, hist)
	deactivate(t, f, &lead, &service)
	hist.Title = "Titre historique"
	if _, err := f.svc.SaveAuditEngagement(histAudit.ID, 28, hist); err != nil {
		t.Fatalf("historical audit update rejected: %v", err)
	}
}

func TestFindAndListAuditEngagements(t *testing.T) {
	f := newGRC01DServiceFixture(t)

	_, err := f.svc.FindAuditEngagement(0)
	expectAppStatus(t, err, http.StatusBadRequest)
	_, err = f.svc.FindAuditEngagement(999999)
	expectAppStatus(t, err, http.StatusNotFound)

	late := f.auditRequest("AUD-LATE")
	late.PlannedStartAt = timePtr(evidenceTestNow.AddDate(0, 1, 0))
	f.createAudit(t, late)
	soon := f.auditRequest("AUD-SOON")
	soon.Title = "Audit fournisseur"
	soon.Type = AuditEngagementTypeVendor
	soon.PlannedStartAt = timePtr(evidenceTestNow.AddDate(0, 0, 1))
	soon.OwningServiceID = &f.service.ID
	f.createAudit(t, soon)
	running := f.auditRequest("AUD-RUN")
	running.Status = AuditEngagementStatusInProgress
	running.LeadAuditorProfileID = &f.profile.ID
	f.createAudit(t, running)

	refs := func(filter AuditEngagementFilter) []string {
		t.Helper()
		page, err := f.svc.ListAuditEngagements(filter)
		if err != nil {
			t.Fatal(err)
		}
		out := []string{}
		for _, item := range page.Items {
			out = append(out, item.Reference)
		}
		return out
	}
	requireRefs := func(filter AuditEngagementFilter, want ...string) {
		t.Helper()
		got := refs(filter)
		if len(got) != len(want) {
			t.Fatalf("filter %+v: got %v want %v", filter, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("filter %+v: got %v want %v", filter, got, want)
			}
		}
	}

	requireRefs(AuditEngagementFilter{}, "AUD-SOON", "AUD-LATE", "AUD-RUN")
	requireRefs(AuditEngagementFilter{Search: "fournisseur"}, "AUD-SOON")
	requireRefs(AuditEngagementFilter{Type: "vendor"}, "AUD-SOON")
	requireRefs(AuditEngagementFilter{Status: "in_progress"}, "AUD-RUN")
	requireRefs(AuditEngagementFilter{LeadAuditorProfileID: &f.profile.ID}, "AUD-RUN")
	requireRefs(AuditEngagementFilter{OwningServiceID: &f.service.ID}, "AUD-SOON")
	requireRefs(AuditEngagementFilter{PlannedStartAfter: timePtr(evidenceTestNow.AddDate(0, 0, 2))}, "AUD-LATE")
	requireRefs(AuditEngagementFilter{PlannedStartBefore: soon.PlannedStartAt}, "AUD-SOON")
	requireRefs(AuditEngagementFilter{Page: 2, Limit: 2}, "AUD-RUN")

	page, err := f.svc.ListAuditEngagements(AuditEngagementFilter{Limit: 2})
	if err != nil || page.Total != 3 || page.TotalPages != 2 || len(page.Items) != 2 {
		t.Fatalf("pagination: %v %+v", err, page)
	}
	_, err = f.svc.ListAuditEngagements(AuditEngagementFilter{Status: "DRAFT"})
	expectAppStatus(t, err, http.StatusBadRequest)
}

// --- Assessment ---

func TestSaveAssessmentCreationAndCompletion(t *testing.T) {
	f := newGRC01DServiceFixture(t)

	r := f.assessmentRequest(" asm-001 ")
	r.AssessorProfileID = &f.profile.ID
	created, err := f.svc.SaveAssessment(0, 41, r)
	if err != nil {
		t.Fatal(err)
	}
	if created.Reference != "ASM-001" || created.Status != AssessmentStatusDraft || created.CompletedAt != nil ||
		created.CreatedBy != 41 || created.ControlImplementation == nil || created.ControlImplementation.Control == nil ||
		created.AssessorProfile == nil {
		t.Fatalf("unexpected draft assessment: %+v", created)
	}

	f.setClock(evidenceTestNow.Add(time.Hour))
	completion := f.completionOf(r)
	completion.NextAssessmentAt = timePtr(evidenceTestNow.Add(3 * time.Hour))
	completed, err := f.svc.SaveAssessment(created.ID, 42, completion)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != AssessmentStatusCompleted || completed.CompletedAt == nil ||
		!completed.CompletedAt.Equal(evidenceTestNow.Add(time.Hour)) || completed.UpdatedBy != 42 || completed.CreatedBy != 41 {
		t.Fatalf("DRAFT -> COMPLETED must set CompletedAt from the clock: %+v", completed)
	}
	if completed.ReassessmentDue {
		t.Fatal("reassessment not due yet")
	}

	f.setClock(evidenceTestNow.Add(4 * time.Hour))
	found, err := f.svc.FindAssessment(created.ID)
	if err != nil || !found.ReassessmentDue || !found.CompletedAt.Equal(evidenceTestNow.Add(time.Hour)) {
		t.Fatalf("reads must compute ReassessmentDue without rewriting CompletedAt: %v %+v", err, found)
	}

	for _, status := range []string{AssessmentStatusCompleted, AssessmentStatusDraft, AssessmentStatusCancelled} {
		next := completion
		next.Status = status
		next.Title = "Modification"
		_, err := f.svc.SaveAssessment(created.ID, 43, next)
		expectAppStatus(t, err, http.StatusConflict)
	}

	cancelReq := f.assessmentRequest("ASM-CANCEL")
	cancelled := f.createAssessment(t, cancelReq)
	cancelReq.Status = AssessmentStatusCancelled
	if got, err := f.svc.SaveAssessment(cancelled.ID, 44, cancelReq); err != nil || got.Status != AssessmentStatusCancelled || got.CompletedAt != nil {
		t.Fatalf("DRAFT -> CANCELLED: %v %+v", err, got)
	}
	cancelReq.Status = AssessmentStatusDraft
	_, err = f.svc.SaveAssessment(cancelled.ID, 45, cancelReq)
	expectAppStatus(t, err, http.StatusConflict)
}

func TestSaveAssessmentRejectsInvalidRequests(t *testing.T) {
	f := newGRC01DServiceFixture(t)
	f.createAssessment(t, f.assessmentRequest("ASM-DUP"))
	unknown := uint(999999)

	cases := []struct {
		name   string
		status int
		mutate func(*AssessmentRequest)
	}{
		{"initial completed", http.StatusBadRequest, func(r *AssessmentRequest) { *r = f.completionOf(*r) }},
		{"unknown implementation", http.StatusNotFound, func(r *AssessmentRequest) { r.ControlImplementationID = unknown }},
		{"unknown assessor", http.StatusNotFound, func(r *AssessmentRequest) { r.AssessorProfileID = &unknown }},
		{"inactive assessor", http.StatusConflict, func(r *AssessmentRequest) { r.AssessorProfileID = &f.inactiveProfile.ID }},
		{"future assessed", http.StatusBadRequest, func(r *AssessmentRequest) { r.AssessedAt = timePtr(evidenceTestNow.Add(time.Second)) }},
		{"not applicable without reason", http.StatusBadRequest, func(r *AssessmentRequest) {
			r.Result = stringPtr(AssessmentResultNotApplicable)
		}},
		{"next before assessed", http.StatusBadRequest, func(r *AssessmentRequest) {
			r.AssessedAt = timePtr(evidenceTestNow)
			r.NextAssessmentAt = timePtr(evidenceTestNow.Add(-time.Second))
		}},
		{"duplicate", http.StatusConflict, func(r *AssessmentRequest) { r.Reference = "asm-dup" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := f.assessmentRequest("ASM-NEW")
			tc.mutate(&r)
			_, err := f.svc.SaveAssessment(0, 10, r)
			expectAppStatus(t, err, tc.status)
		})
	}

	assessedNow := f.assessmentRequest("ASM-NOW")
	assessedNow.AssessedAt = timePtr(evidenceTestNow)
	assessedNow.Result = stringPtr(AssessmentResultNotApplicable)
	assessedNow.Summary = "Actif hors périmètre"
	f.createAssessment(t, assessedNow)

	// Completion still requires result, assessor and assessed_at.
	draft := f.createAssessment(t, f.assessmentRequest("ASM-INCOMPLETE"))
	incomplete := f.assessmentRequest("ASM-INCOMPLETE")
	incomplete.Status = AssessmentStatusCompleted
	_, err := f.svc.SaveAssessment(draft.ID, 10, incomplete)
	expectAppStatus(t, err, http.StatusBadRequest)

	_, err = f.svc.SaveAssessment(999999, 10, f.assessmentRequest("ASM-MISSING"))
	expectAppStatus(t, err, http.StatusNotFound)
}

func TestAssessmentEvidenceLinks(t *testing.T) {
	f := newGRC01DServiceFixture(t)
	r := f.assessmentRequest("ASM-EVD")
	assessment := f.createAssessment(t, r)

	valid := f.validEvidence(t, "EVD-OK", f.implementation.ID)
	other := f.validEvidence(t, "EVD-OTHER-CI", f.other.ID)
	draft := f.create(t, f.request("EVD-DRAFT"))
	expiredReq := f.request("EVD-EXPIRED")
	expiredReq.Status = EvidenceStatusValid
	expiredReq.ExpiresAt = timePtr(evidenceTestNow.Add(-time.Second))
	expired := f.create(t, expiredReq)

	link, err := f.svc.LinkAssessmentEvidence(51, assessment.ID, AssessmentEvidenceRequest{EvidenceID: valid.ID})
	if err != nil || link.ID == 0 || link.CreatedBy != 51 {
		t.Fatalf("valid link: %v %+v", err, link)
	}

	cases := []struct {
		name         string
		assessmentID uint
		evidenceID   uint
		status       int
	}{
		{"zero evidence", assessment.ID, 0, http.StatusBadRequest},
		{"unknown assessment", 999999, valid.ID, http.StatusNotFound},
		{"unknown evidence", assessment.ID, 999999, http.StatusNotFound},
		{"other control implementation", assessment.ID, other.ID, http.StatusConflict},
		{"draft evidence", assessment.ID, draft.ID, http.StatusConflict},
		{"expired evidence", assessment.ID, expired.ID, http.StatusConflict},
		{"duplicate", assessment.ID, valid.ID, http.StatusConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := f.svc.LinkAssessmentEvidence(51, tc.assessmentID, AssessmentEvidenceRequest{EvidenceID: tc.evidenceID})
			expectAppStatus(t, err, tc.status)
		})
	}

	links, err := f.svc.ListAssessmentEvidence(assessment.ID)
	if err != nil || len(links) != 1 || links[0].Evidence == nil || links[0].EvidenceID != valid.ID {
		t.Fatalf("list links: %v %+v", err, links)
	}

	// Changing the control implementation of an assessment with evidence is refused.
	moved := r
	moved.ControlImplementationID = f.other.ID
	_, err = f.svc.SaveAssessment(assessment.ID, 52, moved)
	expectAppStatus(t, err, http.StatusConflict)

	if err := f.svc.UnlinkAssessmentEvidence(53, assessment.ID, valid.ID); err != nil {
		t.Fatal(err)
	}
	expectAppStatus(t, f.svc.UnlinkAssessmentEvidence(53, assessment.ID, valid.ID), http.StatusNotFound)
	if _, err := f.svc.LinkAssessmentEvidence(54, assessment.ID, AssessmentEvidenceRequest{EvidenceID: valid.ID}); err != nil {
		t.Fatal(err)
	}

	// Completed assessments freeze their evidence.
	if _, err := f.svc.SaveAssessment(assessment.ID, 55, f.completionOf(r)); err != nil {
		t.Fatal(err)
	}
	another := f.validEvidence(t, "EVD-LATE", f.implementation.ID)
	_, err = f.svc.LinkAssessmentEvidence(56, assessment.ID, AssessmentEvidenceRequest{EvidenceID: another.ID})
	expectAppStatus(t, err, http.StatusConflict)
	expectAppStatus(t, f.svc.UnlinkAssessmentEvidence(56, assessment.ID, valid.ID), http.StatusConflict)
}

func TestAssessmentCompletionChecksEvidenceAndHistory(t *testing.T) {
	f := newGRC01DServiceFixture(t)

	evidenceReq := f.request("EVD-SHORT")
	evidenceReq.Status = EvidenceStatusValid
	evidenceReq.ExpiresAt = timePtr(evidenceTestNow.Add(time.Hour))
	shortLived := f.create(t, evidenceReq)

	r := f.assessmentRequest("ASM-EXPIRING")
	assessment := f.createAssessment(t, r)
	if _, err := f.svc.LinkAssessmentEvidence(10, assessment.ID, AssessmentEvidenceRequest{EvidenceID: shortLived.ID}); err != nil {
		t.Fatal(err)
	}

	// The evidence expires before completion: completion is refused.
	f.setClock(evidenceTestNow.Add(2 * time.Hour))
	_, err := f.svc.SaveAssessment(assessment.ID, 11, f.completionOf(r))
	expectAppStatus(t, err, http.StatusConflict)
	if reloaded, _ := f.svc.FindAssessment(assessment.ID); reloaded.Status != AssessmentStatusDraft || reloaded.CompletedAt != nil {
		t.Fatalf("refused completion must not be written: %+v", reloaded)
	}

	// Valid at completion, revoked afterwards: the assessment stays completed
	// and reports invalid evidence.
	f.setClock(evidenceTestNow)
	valid := f.validEvidence(t, "EVD-LONG", f.implementation.ID)
	r2 := f.assessmentRequest("ASM-HISTORY")
	second := f.createAssessment(t, r2)
	if _, err := f.svc.LinkAssessmentEvidence(10, second.ID, AssessmentEvidenceRequest{EvidenceID: valid.ID}); err != nil {
		t.Fatal(err)
	}
	completed, err := f.svc.SaveAssessment(second.ID, 12, f.completionOf(r2))
	if err != nil || completed.HasInvalidEvidence {
		t.Fatalf("completion: %v %+v", err, completed)
	}

	revoke := f.request("EVD-LONG")
	revoke.Status = EvidenceStatusRevoked
	if _, err := f.svc.SaveEvidence(valid.ID, 13, revoke); err != nil {
		t.Fatal(err)
	}
	found, err := f.svc.FindAssessment(second.ID)
	if err != nil || found.Status != AssessmentStatusCompleted || !found.HasInvalidEvidence ||
		!found.CompletedAt.Equal(*completed.CompletedAt) {
		t.Fatalf("revoked evidence must be flagged without rewriting history: %v %+v", err, found)
	}
}

func TestListAssessmentsFiltersAndPagination(t *testing.T) {
	f := newGRC01DServiceFixture(t)

	_, err := f.svc.FindAssessment(0)
	expectAppStatus(t, err, http.StatusBadRequest)
	_, err = f.svc.FindAssessment(999999)
	expectAppStatus(t, err, http.StatusNotFound)

	due := f.completedAssessment(t, "ASM-B-DUE")
	// Give ASM-B-DUE a next assessment in the past relative to a later clock.
	if err := f.db.Model(&Assessment{}).Where("id = ?", due.ID).
		Update("next_assessment_at", evidenceTestNow).Error; err != nil {
		t.Fatal(err)
	}

	design := f.assessmentRequest("ASM-A-DESIGN")
	design.Type = AssessmentTypeDesign
	design.Title = "Revue de conception"
	design.ControlImplementationID = f.other.ID
	f.createAssessment(t, design)

	withInvalid := f.assessmentRequest("ASM-C-INVALID")
	invalidAssessment := f.createAssessment(t, withInvalid)
	evidence := f.validEvidence(t, "EVD-LIST", f.implementation.ID)
	if _, err := f.svc.LinkAssessmentEvidence(10, invalidAssessment.ID, AssessmentEvidenceRequest{EvidenceID: evidence.ID}); err != nil {
		t.Fatal(err)
	}
	revoke := f.request("EVD-LIST")
	revoke.Status = EvidenceStatusRevoked
	if _, err := f.svc.SaveEvidence(evidence.ID, 10, revoke); err != nil {
		t.Fatal(err)
	}

	f.setClock(evidenceTestNow.Add(time.Second))
	refs := func(filter AssessmentFilter) []string {
		t.Helper()
		page, err := f.svc.ListAssessments(filter)
		if err != nil {
			t.Fatal(err)
		}
		out := []string{}
		for _, item := range page.Items {
			out = append(out, item.Reference)
		}
		return out
	}
	requireRefs := func(filter AssessmentFilter, want ...string) {
		t.Helper()
		got := refs(filter)
		if len(got) != len(want) {
			t.Fatalf("filter %+v: got %v want %v", filter, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("filter %+v: got %v want %v", filter, got, want)
			}
		}
	}

	yes, no := true, false
	requireRefs(AssessmentFilter{}, "ASM-A-DESIGN", "ASM-B-DUE", "ASM-C-INVALID")
	requireRefs(AssessmentFilter{Search: "conception"}, "ASM-A-DESIGN")
	requireRefs(AssessmentFilter{Type: "design"}, "ASM-A-DESIGN")
	requireRefs(AssessmentFilter{Status: "completed"}, "ASM-B-DUE")
	requireRefs(AssessmentFilter{Result: "effective"}, "ASM-B-DUE")
	requireRefs(AssessmentFilter{ControlImplementationID: &f.other.ID}, "ASM-A-DESIGN")
	requireRefs(AssessmentFilter{AssessorProfileID: &f.profile.ID}, "ASM-B-DUE")
	requireRefs(AssessmentFilter{ReassessmentDue: &yes}, "ASM-B-DUE")
	requireRefs(AssessmentFilter{ReassessmentDue: &no}, "ASM-A-DESIGN", "ASM-C-INVALID")
	requireRefs(AssessmentFilter{HasInvalidEvidence: &yes}, "ASM-C-INVALID")
	requireRefs(AssessmentFilter{HasInvalidEvidence: &no}, "ASM-A-DESIGN", "ASM-B-DUE")
	requireRefs(AssessmentFilter{AssessedBefore: timePtr(evidenceTestNow)}, "ASM-B-DUE")
	requireRefs(AssessmentFilter{Page: 2, Limit: 2}, "ASM-C-INVALID")

	page, err := f.svc.ListAssessments(AssessmentFilter{})
	if err != nil || page.Total != 3 {
		t.Fatalf("list: %v %+v", err, page)
	}
	for _, item := range page.Items {
		if item.ReassessmentDue != (item.Reference == "ASM-B-DUE") || item.HasInvalidEvidence != (item.Reference == "ASM-C-INVALID") {
			t.Fatalf("computed fields of %s: due=%v invalid=%v", item.Reference, item.ReassessmentDue, item.HasInvalidEvidence)
		}
	}

	_, err = f.svc.ListAssessments(AssessmentFilter{Result: "NOT_ASSESSED"})
	expectAppStatus(t, err, http.StatusBadRequest)
}

// --- Finding ---

func TestSaveFindingSources(t *testing.T) {
	f := newGRC01DServiceFixture(t)
	audit := f.inProgressAudit(t, "AUD-SRC")
	planned := f.createAudit(t, f.auditRequest("AUD-PLANNED"))
	completed := f.completedAssessment(t, "ASM-SRC")
	draft := f.createAssessment(t, f.assessmentRequest("ASM-DRAFT"))

	fromAudit := f.findingRequest(" fnd-audit ", audit.ID)
	fromAudit.OwnerProfileID = &f.profile.ID
	fromAudit.OwningServiceID = &f.service.ID
	created, err := f.svc.SaveFinding(0, 61, fromAudit)
	if err != nil {
		t.Fatal(err)
	}
	if created.Reference != "FND-AUDIT" || created.CreatedBy != 61 || created.AuditEngagement == nil ||
		created.OwnerProfile == nil || created.OwningService == nil || created.ResolvedAt != nil || created.ClosedAt != nil {
		t.Fatalf("unexpected finding from audit: %+v", created)
	}

	fromAssessment := f.findingRequest("FND-ASSESSMENT", 0)
	fromAssessment.AuditEngagementID = nil
	fromAssessment.AssessmentID = &completed.ID
	got, err := f.svc.SaveFinding(0, 62, fromAssessment)
	if err != nil {
		t.Fatal(err)
	}
	if got.ControlImplementationID == nil || *got.ControlImplementationID != completed.ControlImplementationID ||
		got.Assessment == nil || got.ControlImplementation == nil {
		t.Fatalf("finding from assessment must sit on the assessed implementation: %+v", got)
	}

	withRisk := f.findingRequest("FND-RISK", audit.ID)
	withRisk.ControlImplementationID = &f.implementation.ID
	withRisk.RiskID = &f.risk.ID
	if got := f.createFinding(t, withRisk); got.Risk == nil {
		t.Fatalf("risk not preloaded: %+v", got)
	}

	f.createFinding(t, f.findingRequest("FND-DUP", audit.ID))
	unknown := uint(999999)
	cases := []struct {
		name   string
		status int
		mutate func(*FindingRequest)
	}{
		{"no source", http.StatusBadRequest, func(r *FindingRequest) { r.AuditEngagementID = nil }},
		{"two sources", http.StatusBadRequest, func(r *FindingRequest) { r.AssessmentID = &completed.ID }},
		{"unknown audit", http.StatusNotFound, func(r *FindingRequest) { r.AuditEngagementID = &unknown }},
		{"audit not in progress", http.StatusConflict, func(r *FindingRequest) { r.AuditEngagementID = &planned.ID }},
		{"unknown assessment", http.StatusNotFound, func(r *FindingRequest) { r.AuditEngagementID, r.AssessmentID = nil, &unknown }},
		{"draft assessment", http.StatusConflict, func(r *FindingRequest) { r.AuditEngagementID, r.AssessmentID = nil, &draft.ID }},
		{"implementation differs from assessment", http.StatusConflict, func(r *FindingRequest) {
			r.AuditEngagementID, r.AssessmentID, r.ControlImplementationID = nil, &completed.ID, &f.other.ID
		}},
		{"unknown implementation", http.StatusNotFound, func(r *FindingRequest) { r.ControlImplementationID = &unknown }},
		{"unknown risk", http.StatusNotFound, func(r *FindingRequest) { r.RiskID = &unknown }},
		{"risk not linked to control", http.StatusConflict, func(r *FindingRequest) {
			r.ControlImplementationID, r.RiskID = &f.implementation.ID, &f.otherRisk.ID
		}},
		{"unknown owner", http.StatusNotFound, func(r *FindingRequest) { r.OwnerProfileID = &unknown }},
		{"inactive owner", http.StatusConflict, func(r *FindingRequest) { r.OwnerProfileID = &f.inactiveProfile.ID }},
		{"unknown service", http.StatusNotFound, func(r *FindingRequest) { r.OwningServiceID = &unknown }},
		{"initial resolved", http.StatusBadRequest, func(r *FindingRequest) { r.Status = FindingStatusResolved }},
		{"duplicate", http.StatusConflict, func(r *FindingRequest) { r.Reference = "fnd-dup" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := f.findingRequest("FND-NEW", audit.ID)
			tc.mutate(&r)
			_, err := f.svc.SaveFinding(0, 10, r)
			expectAppStatus(t, err, tc.status)
		})
	}

	// The source of a finding is immutable.
	moved := fromAudit
	other := f.inProgressAudit(t, "AUD-OTHER")
	moved.AuditEngagementID = &other.ID
	_, err = f.svc.SaveFinding(created.ID, 63, moved)
	expectAppStatus(t, err, http.StatusConflict)
}

func TestSaveFindingWorkflowTimestamps(t *testing.T) {
	f := newGRC01DServiceFixture(t)
	audit := f.inProgressAudit(t, "AUD-FLOW")
	r := f.findingRequest("FND-FLOW", audit.ID)
	finding := f.createFinding(t, r)

	save := func(status string, at time.Time) (*Finding, error) {
		f.setClock(at)
		r.Status = status
		return f.svc.SaveFinding(finding.ID, 70, r)
	}

	got, err := save(FindingStatusInRemediation, evidenceTestNow)
	if err != nil || got.ResolvedAt != nil || got.ClosedAt != nil {
		t.Fatalf("OPEN -> IN_REMEDIATION: %v %+v", err, got)
	}

	resolvedAt := evidenceTestNow.Add(time.Hour)
	got, err = save(FindingStatusResolved, resolvedAt)
	if err != nil || got.ResolvedAt == nil || !got.ResolvedAt.Equal(resolvedAt) {
		t.Fatalf("-> RESOLVED must set ResolvedAt from the clock: %v %+v", err, got)
	}

	// Staying RESOLVED keeps the resolution instant.
	got, err = save(FindingStatusResolved, resolvedAt.Add(time.Hour))
	if err != nil || !got.ResolvedAt.Equal(resolvedAt) {
		t.Fatalf("editing a resolved finding must keep ResolvedAt: %v %+v", err, got)
	}

	// Reopening clears ResolvedAt; a new resolution records a fresh instant.
	got, err = save(FindingStatusOpen, resolvedAt.Add(2*time.Hour))
	if err != nil || got.ResolvedAt != nil || got.ClosedAt != nil {
		t.Fatalf("RESOLVED -> OPEN must clear ResolvedAt: %v %+v", err, got)
	}
	var cleared int64
	f.db.Model(&Finding{}).Where("id = ? AND resolved_at IS NULL", finding.ID).Count(&cleared)
	if cleared != 1 {
		t.Fatal("resolved_at must be NULL in database after reopening")
	}

	reResolved := resolvedAt.Add(3 * time.Hour)
	if got, err = save(FindingStatusResolved, reResolved); err != nil || !got.ResolvedAt.Equal(reResolved) {
		t.Fatalf("second resolution: %v %+v", err, got)
	}

	closedAt := reResolved.Add(time.Hour)
	got, err = save(FindingStatusClosed, closedAt)
	if err != nil || got.ClosedAt == nil || !got.ClosedAt.Equal(closedAt) || !got.ResolvedAt.Equal(reResolved) ||
		got.ClosedAt.Before(*got.ResolvedAt) {
		t.Fatalf("RESOLVED -> CLOSED must set ClosedAt >= ResolvedAt: %v %+v", err, got)
	}

	// Terminal: every further change is refused (the note keeps ACCEPTED valid).
	r.ClosureNote = "Justification"
	for _, status := range []string{FindingStatusClosed, FindingStatusOpen, FindingStatusAccepted} {
		_, err := save(status, closedAt.Add(time.Hour))
		expectAppStatus(t, err, http.StatusConflict)
	}
}

func TestSaveFindingAcceptanceAndClosureGuards(t *testing.T) {
	f := newGRC01DServiceFixture(t)
	audit := f.inProgressAudit(t, "AUD-ACCEPT")

	r := f.findingRequest("FND-ACCEPT", audit.ID)
	finding := f.createFinding(t, r)

	r.Status = FindingStatusAccepted
	_, err := f.svc.SaveFinding(finding.ID, 80, r)
	expectAppStatus(t, err, http.StatusBadRequest)

	r.ClosureNote = "Risque accepté par la direction"
	accepted, err := f.svc.SaveFinding(finding.ID, 81, r)
	if err != nil || accepted.Status != FindingStatusAccepted || accepted.ClosedAt == nil ||
		!accepted.ClosedAt.Equal(evidenceTestNow) || accepted.ResolvedAt != nil {
		t.Fatalf("OPEN -> ACCEPTED: %v %+v", err, accepted)
	}
	r.Title = "Modification après acceptation"
	_, err = f.svc.SaveFinding(finding.ID, 82, r)
	expectAppStatus(t, err, http.StatusConflict)

	// Closing requires every linked remediation action to be done or cancelled.
	closing := f.findingRequest("FND-CLOSE", audit.ID)
	toClose := f.createFinding(t, closing)
	closing.Status = FindingStatusResolved
	if _, err := f.svc.SaveFinding(toClose.ID, 83, closing); err != nil {
		t.Fatal(err)
	}
	action := RemediationAction{
		Reference: "ACT-FINDING-OPEN", Title: "Action", FindingID: &toClose.ID,
		Priority: RemediationActionPriorityHigh, Status: RemediationActionStatusOpen, CreatedBy: 1, UpdatedBy: 1,
	}
	if err := f.db.Create(&action).Error; err != nil {
		t.Fatal(err)
	}
	closing.Status = FindingStatusClosed
	_, err = f.svc.SaveFinding(toClose.ID, 84, closing)
	expectAppStatus(t, err, http.StatusConflict)
	if reloaded, _ := f.svc.FindFinding(toClose.ID); reloaded.Status != FindingStatusResolved || reloaded.ClosedAt != nil {
		t.Fatalf("refused closure must not be written: %+v", reloaded)
	}

	if err := f.db.Model(&RemediationAction{}).Where("id = ?", action.ID).
		Updates(map[string]any{"status": RemediationActionStatusDone, "completed_at": evidenceTestNow}).Error; err != nil {
		t.Fatal(err)
	}
	closed, err := f.svc.SaveFinding(toClose.ID, 85, closing)
	if err != nil || closed.Status != FindingStatusClosed {
		t.Fatalf("closure after actions done: %v %+v", err, closed)
	}
	_, err = f.svc.SaveFinding(toClose.ID, 86, closing)
	expectAppStatus(t, err, http.StatusConflict)

	// Historical owner and service stay editable.
	owner := createExtraProfile(t, f.evidenceServiceFixture, "GRC-FND-OWNER")
	service := createExtraService(t, f.evidenceServiceFixture, "FND-SVC")
	hist := f.findingRequest("FND-HIST", audit.ID)
	hist.OwnerProfileID = &owner.ID
	hist.OwningServiceID = &service.ID
	histFinding := f.createFinding(t, hist)
	deactivate(t, f, &owner, &service)
	hist.Title = "Titre historique"
	if _, err := f.svc.SaveFinding(histFinding.ID, 87, hist); err != nil {
		t.Fatalf("historical finding update rejected: %v", err)
	}
}

func TestFindAndListFindings(t *testing.T) {
	f := newGRC01DServiceFixture(t)

	_, err := f.svc.FindFinding(0)
	expectAppStatus(t, err, http.StatusBadRequest)
	_, err = f.svc.FindFinding(999999)
	expectAppStatus(t, err, http.StatusNotFound)

	audit := f.inProgressAudit(t, "AUD-LIST")
	assessment := f.completedAssessment(t, "ASM-LIST")

	overdue := f.findingRequest("FND-OVERDUE", audit.ID)
	overdue.DueAt = timePtr(evidenceTestNow.Add(-time.Second))
	overdue.Severity = FindingSeverityCritical
	f.createFinding(t, overdue)

	atNow := f.findingRequest("FND-AT-NOW", audit.ID)
	atNow.DueAt = timePtr(evidenceTestNow)
	atNow.Category = FindingCategoryObservation
	f.createFinding(t, atNow)

	fromAssessment := f.findingRequest("FND-FROM-ASM", 0)
	fromAssessment.AuditEngagementID = nil
	fromAssessment.AssessmentID = &assessment.ID
	fromAssessment.Title = "Contrôle inefficace"
	fromAssessment.RiskID = &f.risk.ID
	fromAssessment.OwnerProfileID = &f.profile.ID
	fromAssessment.OwningServiceID = &f.service.ID
	f.createFinding(t, fromAssessment)

	resolvedPast := f.findingRequest("FND-RESOLVED", audit.ID)
	resolvedPast.DueAt = timePtr(evidenceTestNow.Add(-time.Hour))
	created := f.createFinding(t, resolvedPast)
	resolvedPast.Status = FindingStatusResolved
	if _, err := f.svc.SaveFinding(created.ID, 10, resolvedPast); err != nil {
		t.Fatal(err)
	}

	refs := func(filter FindingFilter) []string {
		t.Helper()
		page, err := f.svc.ListFindings(filter)
		if err != nil {
			t.Fatal(err)
		}
		out := []string{}
		for _, item := range page.Items {
			out = append(out, item.Reference)
		}
		return out
	}
	requireRefs := func(filter FindingFilter, want ...string) {
		t.Helper()
		got := refs(filter)
		if len(got) != len(want) {
			t.Fatalf("filter %+v: got %v want %v", filter, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("filter %+v: got %v want %v", filter, got, want)
			}
		}
	}

	yes, no := true, false
	requireRefs(FindingFilter{}, "FND-RESOLVED", "FND-OVERDUE", "FND-AT-NOW", "FND-FROM-ASM")
	requireRefs(FindingFilter{Search: "inefficace"}, "FND-FROM-ASM")
	requireRefs(FindingFilter{AssessmentID: &assessment.ID}, "FND-FROM-ASM")
	requireRefs(FindingFilter{AuditEngagementID: &audit.ID}, "FND-RESOLVED", "FND-OVERDUE", "FND-AT-NOW")
	requireRefs(FindingFilter{ControlImplementationID: &f.implementation.ID}, "FND-FROM-ASM")
	requireRefs(FindingFilter{RiskID: &f.risk.ID}, "FND-FROM-ASM")
	requireRefs(FindingFilter{OwnerProfileID: &f.profile.ID}, "FND-FROM-ASM")
	requireRefs(FindingFilter{OwningServiceID: &f.service.ID}, "FND-FROM-ASM")
	requireRefs(FindingFilter{Category: "observation"}, "FND-AT-NOW")
	requireRefs(FindingFilter{Severity: "critical"}, "FND-OVERDUE")
	requireRefs(FindingFilter{Status: "resolved"}, "FND-RESOLVED")
	requireRefs(FindingFilter{Overdue: &yes}, "FND-OVERDUE")
	requireRefs(FindingFilter{Overdue: &no}, "FND-RESOLVED", "FND-AT-NOW", "FND-FROM-ASM")
	requireRefs(FindingFilter{DueBefore: timePtr(evidenceTestNow.Add(-time.Second))}, "FND-RESOLVED", "FND-OVERDUE")
	requireRefs(FindingFilter{Page: 2, Limit: 3}, "FND-FROM-ASM")

	page, err := f.svc.ListFindings(FindingFilter{})
	if err != nil || page.Total != 4 || page.TotalPages != 1 {
		t.Fatalf("list: %v %+v", err, page)
	}
	for _, item := range page.Items {
		if item.Overdue != (item.Reference == "FND-OVERDUE") {
			t.Fatalf("Overdue of %s = %v", item.Reference, item.Overdue)
		}
	}

	// One second later FND-AT-NOW becomes overdue for the filter and the field.
	f.setClock(evidenceTestNow.Add(time.Second))
	requireRefs(FindingFilter{Overdue: &yes}, "FND-OVERDUE", "FND-AT-NOW")

	_, err = f.svc.ListFindings(FindingFilter{Severity: "INFO"})
	expectAppStatus(t, err, http.StatusBadRequest)
}
