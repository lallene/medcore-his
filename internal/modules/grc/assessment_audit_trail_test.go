package grc

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

const (
	summarySentinel     = "ASSESSMENT_SUMMARY_SECRET_SENTINEL"
	descriptionSentinel = "AUDIT_DESCRIPTION_SECRET_SENTINEL"
	scopeSentinel       = "AUDIT_SCOPE_SECRET_SENTINEL"
	findingSentinel     = "FINDING_DESCRIPTION_SECRET_SENTINEL"
	closureSentinel     = "FINDING_CLOSURE_NOTE_SECRET_SENTINEL"
)

var (
	assessmentAuditAllowlist = []string{"reference", "title", "controlImplementationId", "type", "status",
		"result", "assessorProfileId", "assessedAt", "completedAt", "nextAssessmentAt"}
	assessmentEvidenceAuditAllowlist = []string{"assessmentId", "evidenceId"}
	auditEngagementAuditAllowlist    = []string{"reference", "title", "type", "status", "leadAuditorProfileId",
		"owningServiceId", "plannedStartAt", "plannedEndAt", "startedAt", "completedAt"}
	findingAuditAllowlist = []string{"reference", "title", "auditEngagementId", "assessmentId", "category",
		"severity", "status", "controlImplementationId", "riskId", "ownerProfileId", "owningServiceId",
		"dueAt", "resolvedAt", "closedAt"}
	grc01DAuditForbiddenKeys = []string{"id", "summary", "description", "scope", "closureNote",
		"createdBy", "updatedBy", "createdAt", "updatedAt", "reassessmentDue", "hasInvalidEvidence", "overdue",
		"controlImplementation", "assessorProfile", "evidence", "assessment", "auditEngagement", "risk",
		"ownerProfile", "owningService", "leadAuditorProfile"}
)

func TestGRC01DAuditSnapshotsAreAllowlisted(t *testing.T) {
	for _, tc := range []struct {
		snapshot any
		keys     []string
	}{
		{assessmentAuditSnapshot{}, assessmentAuditAllowlist},
		{assessmentEvidenceAuditSnapshot{}, assessmentEvidenceAuditAllowlist},
		{auditEngagementAuditSnapshot{}, auditEngagementAuditAllowlist},
		{findingAuditSnapshot{}, findingAuditAllowlist},
	} {
		typ := reflect.TypeOf(tc.snapshot)
		var keys []string
		for i := 0; i < typ.NumField(); i++ {
			keys = append(keys, strings.Split(typ.Field(i).Tag.Get("json"), ",")[0])
		}
		if strings.Join(keys, ",") != strings.Join(tc.keys, ",") {
			t.Errorf("%s json keys=%v, want exactly %v", typ.Name(), keys, tc.keys)
		}
	}
}

func requireEventCount(t *testing.T, bus *captureBus, want int, context string) {
	t.Helper()
	if len(bus.events) != want {
		t.Fatalf("%s: audit events=%d want %d", context, len(bus.events), want)
	}
}

func TestAssessmentAuditTrail(t *testing.T) {
	f := newGRC01DServiceFixture(t)
	evidenceReq := f.request("EVD-TRAIL")
	evidenceReq.Status = EvidenceStatusValid
	evidenceReq.ExternalReference = externalReferenceSentinel
	evidence := f.create(t, evidenceReq)
	bus := withCaptureBus(t)

	r := f.assessmentRequest("ASM-TRAIL")
	r.Summary = summarySentinel
	created, err := f.svc.SaveAssessment(0, 41, r)
	if err != nil {
		t.Fatal(err)
	}
	old, next := assertAuditEvent(t, bus, 0, auditModuleGRCAssessment, auditActionCreate, created.ID, 41)
	if old != nil {
		t.Fatalf("create old value=%#v", old)
	}
	assertSnapshotKeysExactly(t, next, assessmentAuditAllowlist, grc01DAuditForbiddenKeys)

	r.Title = "Titre modifié"
	if _, err = f.svc.SaveAssessment(created.ID, 42, r); err != nil {
		t.Fatal(err)
	}
	old, next = assertAuditEvent(t, bus, 1, auditModuleGRCAssessment, auditActionUpdate, created.ID, 42)
	if old["title"] != created.Title || next["title"] != "Titre modifié" {
		t.Fatalf("update snapshots: old=%#v new=%#v", old, next)
	}

	link, err := f.svc.LinkAssessmentEvidence(43, created.ID, AssessmentEvidenceRequest{EvidenceID: evidence.ID})
	if err != nil {
		t.Fatal(err)
	}
	old, next = assertAuditEvent(t, bus, 2, auditModuleGRCAssessmentEvidence, auditActionLink, link.ID, 43)
	assertSnapshotKeysExactly(t, next, assessmentEvidenceAuditAllowlist, grc01DAuditForbiddenKeys)
	if old != nil || next["assessmentId"] != float64(created.ID) || next["evidenceId"] != float64(evidence.ID) {
		t.Fatalf("link snapshot: old=%#v new=%#v", old, next)
	}

	if err = f.svc.UnlinkAssessmentEvidence(44, created.ID, evidence.ID); err != nil {
		t.Fatal(err)
	}
	old, next = assertAuditEvent(t, bus, 3, auditModuleGRCAssessmentEvidence, auditActionUnlink, link.ID, 44)
	if next != nil || old["evidenceId"] != float64(evidence.ID) {
		t.Fatalf("unlink snapshot: old=%#v new=%#v", old, next)
	}
	if _, err = f.svc.LinkAssessmentEvidence(45, created.ID, AssessmentEvidenceRequest{EvidenceID: evidence.ID}); err != nil {
		t.Fatal(err)
	}

	f.setClock(evidenceTestNow.Add(time.Hour))
	if _, err = f.svc.SaveAssessment(created.ID, 46, f.completionOf(r)); err != nil {
		t.Fatal(err)
	}
	old, next = assertAuditEvent(t, bus, 5, auditModuleGRCAssessment, auditActionUpdate, created.ID, 46)
	assertSnapshotKeysExactly(t, next, assessmentAuditAllowlist, grc01DAuditForbiddenKeys)
	completedAt, _ := time.Parse(time.RFC3339Nano, next["completedAt"].(string))
	if old["status"] != AssessmentStatusDraft || old["completedAt"] != nil ||
		next["status"] != AssessmentStatusCompleted || !completedAt.Equal(evidenceTestNow.Add(time.Hour)) {
		t.Fatalf("completion snapshots: old=%#v new=%#v", old, next)
	}

	cancel := f.createAssessment(t, f.assessmentRequest("ASM-TRAIL-CANCEL"))
	cancelReq := f.assessmentRequest("ASM-TRAIL-CANCEL")
	cancelReq.Status = AssessmentStatusCancelled
	if _, err = f.svc.SaveAssessment(cancel.ID, 47, cancelReq); err != nil {
		t.Fatal(err)
	}
	old, next = assertAuditEvent(t, bus, 7, auditModuleGRCAssessment, auditActionUpdate, cancel.ID, 47)
	if old["status"] != AssessmentStatusDraft || next["status"] != AssessmentStatusCancelled {
		t.Fatalf("cancel snapshots: old=%#v new=%#v", old, next)
	}

	assertSerializedAuditExcludes(t, bus, summarySentinel, externalReferenceSentinel)
}

func TestAuditEngagementAuditTrail(t *testing.T) {
	f := newGRC01DServiceFixture(t)
	bus := withCaptureBus(t)

	r := f.auditRequest("AUD-TRAIL")
	r.Description = descriptionSentinel
	r.Scope = scopeSentinel
	created, err := f.svc.SaveAuditEngagement(0, 51, r)
	if err != nil {
		t.Fatal(err)
	}
	_, next := assertAuditEvent(t, bus, 0, auditModuleGRCAuditEngagement, auditActionCreate, created.ID, 51)
	assertSnapshotKeysExactly(t, next, auditEngagementAuditAllowlist, grc01DAuditForbiddenKeys)

	r.Status = AuditEngagementStatusInProgress
	if _, err = f.svc.SaveAuditEngagement(created.ID, 52, r); err != nil {
		t.Fatal(err)
	}
	old, next := assertAuditEvent(t, bus, 1, auditModuleGRCAuditEngagement, auditActionUpdate, created.ID, 52)
	if old["status"] != AuditEngagementStatusPlanned || old["startedAt"] != nil ||
		next["status"] != AuditEngagementStatusInProgress || next["startedAt"] == nil {
		t.Fatalf("start snapshots: old=%#v new=%#v", old, next)
	}

	f.setClock(evidenceTestNow.Add(2 * time.Hour))
	r.Status = AuditEngagementStatusCompleted
	if _, err = f.svc.SaveAuditEngagement(created.ID, 53, r); err != nil {
		t.Fatal(err)
	}
	old, next = assertAuditEvent(t, bus, 2, auditModuleGRCAuditEngagement, auditActionUpdate, created.ID, 53)
	completedAt, _ := time.Parse(time.RFC3339Nano, next["completedAt"].(string))
	if old["status"] != AuditEngagementStatusInProgress || old["completedAt"] != nil ||
		next["status"] != AuditEngagementStatusCompleted || !completedAt.Equal(evidenceTestNow.Add(2*time.Hour)) {
		t.Fatalf("completion snapshots: old=%#v new=%#v", old, next)
	}

	assertSerializedAuditExcludes(t, bus, descriptionSentinel, scopeSentinel)
}

func TestFindingAuditTrail(t *testing.T) {
	f := newGRC01DServiceFixture(t)
	audit := f.inProgressAudit(t, "AUD-FND-TRAIL")
	bus := withCaptureBus(t)

	r := f.findingRequest("FND-TRAIL", audit.ID)
	r.Description = findingSentinel
	created, err := f.svc.SaveFinding(0, 61, r)
	if err != nil {
		t.Fatal(err)
	}
	_, next := assertAuditEvent(t, bus, 0, auditModuleGRCFinding, auditActionCreate, created.ID, 61)
	assertSnapshotKeysExactly(t, next, findingAuditAllowlist, grc01DAuditForbiddenKeys)

	steps := []struct {
		status, from string
		at           time.Time
		check        func(old, next map[string]any) bool
	}{
		{FindingStatusInRemediation, FindingStatusOpen, evidenceTestNow, nil},
		{FindingStatusResolved, FindingStatusInRemediation, evidenceTestNow.Add(time.Hour),
			func(old, next map[string]any) bool { return old["resolvedAt"] == nil && next["resolvedAt"] != nil }},
		{FindingStatusOpen, FindingStatusResolved, evidenceTestNow.Add(2 * time.Hour),
			func(old, next map[string]any) bool { return old["resolvedAt"] != nil && next["resolvedAt"] == nil }},
		{FindingStatusResolved, FindingStatusOpen, evidenceTestNow.Add(3 * time.Hour), nil},
		{FindingStatusClosed, FindingStatusResolved, evidenceTestNow.Add(4 * time.Hour),
			func(old, next map[string]any) bool { return old["closedAt"] == nil && next["closedAt"] != nil }},
	}
	for i, step := range steps {
		f.setClock(step.at)
		r.Status = step.status
		if _, err := f.svc.SaveFinding(created.ID, uint(62+i), r); err != nil {
			t.Fatalf("%s -> %s: %v", step.from, step.status, err)
		}
		old, next := assertAuditEvent(t, bus, i+1, auditModuleGRCFinding, auditActionUpdate, created.ID, uint(62+i))
		if old["status"] != step.from || next["status"] != step.status || (step.check != nil && !step.check(old, next)) {
			t.Fatalf("%s -> %s snapshots: old=%#v new=%#v", step.from, step.status, old, next)
		}
	}

	toAccept := f.findingRequest("FND-TRAIL-ACCEPT", audit.ID)
	accepted := f.createFinding(t, toAccept)
	toAccept.Status = FindingStatusAccepted
	toAccept.ClosureNote = closureSentinel
	if _, err := f.svc.SaveFinding(accepted.ID, 70, toAccept); err != nil {
		t.Fatal(err)
	}
	old, next := assertAuditEvent(t, bus, len(bus.events)-1, auditModuleGRCFinding, auditActionUpdate, accepted.ID, 70)
	if old["status"] != FindingStatusOpen || next["status"] != FindingStatusAccepted || next["closedAt"] == nil {
		t.Fatalf("accept snapshots: old=%#v new=%#v", old, next)
	}

	assertSerializedAuditExcludes(t, bus, findingSentinel, closureSentinel)
}

func TestGRC01DRefusedMutationsAreNotAudited(t *testing.T) {
	f := newGRC01DServiceFixture(t)
	audit := f.inProgressAudit(t, "AUD-REFUSED")
	planned := f.createAudit(t, f.auditRequest("AUD-REFUSED-PLANNED"))
	draft := f.createAssessment(t, f.assessmentRequest("ASM-REFUSED"))
	finding := f.createFinding(t, f.findingRequest("FND-REFUSED", audit.ID))
	resolved := f.findingRequest("FND-REFUSED", audit.ID)
	resolved.Status = FindingStatusResolved
	if _, err := f.svc.SaveFinding(finding.ID, 10, resolved); err != nil {
		t.Fatal(err)
	}
	action := RemediationAction{
		Reference: "ACT-REFUSED", Title: "Action", FindingID: &finding.ID,
		Priority: RemediationActionPriorityHigh, Status: RemediationActionStatusOpen, CreatedBy: 1, UpdatedBy: 1,
	}
	if err := f.db.Create(&action).Error; err != nil {
		t.Fatal(err)
	}
	other := f.createFinding(t, f.findingRequest("FND-REFUSED-ACCEPT", audit.ID))
	bus := withCaptureBus(t)

	incomplete := f.assessmentRequest("ASM-REFUSED")
	incomplete.Status = AssessmentStatusCompleted
	if _, err := f.svc.SaveAssessment(draft.ID, 80, incomplete); err == nil {
		t.Fatal("expected invalid completion")
	}
	plannedComplete := f.auditRequest("AUD-REFUSED-PLANNED")
	plannedComplete.Status = AuditEngagementStatusCompleted
	if _, err := f.svc.SaveAuditEngagement(planned.ID, 81, plannedComplete); err == nil {
		t.Fatal("expected forbidden audit completion")
	}
	resolved.Status = FindingStatusClosed
	if _, err := f.svc.SaveFinding(finding.ID, 82, resolved); err == nil {
		t.Fatal("expected closure refused by the open action")
	}
	accept := f.findingRequest("FND-REFUSED-ACCEPT", audit.ID)
	accept.Status = FindingStatusAccepted
	if _, err := f.svc.SaveFinding(other.ID, 83, accept); err == nil {
		t.Fatal("expected acceptance without justification refused")
	}
	if _, err := f.svc.LinkAssessmentEvidence(84, draft.ID, AssessmentEvidenceRequest{EvidenceID: 999999}); err == nil {
		t.Fatal("expected unknown evidence")
	}
	assertNoAudit(t, bus, "refused service mutations")

	// HTTP: PUT bypasses and reads publish nothing.
	r := grcServiceRouter(f.svc, []string{"*"})
	for _, tc := range []struct{ path, body string }{
		{"/api/grc/assessments/" + jsonUint(draft.ID), `{"status":"COMPLETED"}`},
		{"/api/grc/audits/" + jsonUint(audit.ID), `{"status":"COMPLETED"}`},
		{"/api/grc/findings/" + jsonUint(finding.ID), `{"status":"CLOSED"}`},
		{"/api/grc/findings/" + jsonUint(other.ID), `{"status":"ACCEPTED","closureNote":"x"}`},
	} {
		if code := grcJSONRequest(r, http.MethodPut, tc.path, tc.body).Code; code != http.StatusForbidden {
			t.Fatalf("PUT %s status=%d", tc.path, code)
		}
	}
	if code := grcJSONRequest(r, http.MethodPost, "/api/grc/findings/"+jsonUint(finding.ID)+"/close", "").Code; code != http.StatusConflict {
		t.Fatalf("close with an open action status=%d", code)
	}
	for _, path := range []string{"/api/grc/assessments", "/api/grc/assessments/" + jsonUint(draft.ID),
		"/api/grc/assessments/" + jsonUint(draft.ID) + "/evidence", "/api/grc/audits", "/api/grc/findings"} {
		if code := grcRequest(r, http.MethodGet, path).Code; code != http.StatusOK {
			t.Fatalf("GET %s status=%d", path, code)
		}
	}
	assertNoAudit(t, bus, "refused HTTP mutations and reads")
}

// The audited actor is the authenticated user (7), never a body field.
func TestGRC01DAuditActorComesFromAuthentication(t *testing.T) {
	f := newGRC01DServiceFixture(t)
	audit := f.inProgressAudit(t, "AUD-ACTOR")
	bus := withCaptureBus(t)
	r := grcServiceRouter(f.svc, writerPermissions)

	w := grcJSONRequest(r, http.MethodPost, "/api/grc/assessments", `{"reference":"ASM-ACTOR","title":"X","controlImplementationId":`+
		jsonUint(f.implementation.ID)+`,"type":"DESIGN","status":"DRAFT","createdBy":99,"updatedBy":99,"actor":99,"userId":99}`)
	assessment := decodeJSON[Assessment](t, w.Body.Bytes())
	completion := `{"result":"EFFECTIVE","assessorProfileId":` + jsonUint(f.profile.ID) + `,"assessedAt":"2026-06-01T11:00:00Z","actor":99}`
	if code := grcJSONRequest(r, http.MethodPost, "/api/grc/assessments/"+jsonUint(assessment.ID)+"/complete", completion).Code; code != http.StatusOK {
		t.Fatalf("complete status=%d", code)
	}
	if code := grcJSONRequest(r, http.MethodPost, "/api/grc/audits/"+jsonUint(audit.ID)+"/complete", "").Code; code != http.StatusOK {
		t.Fatalf("audit complete status=%d", code)
	}

	finding := decodeJSON[Finding](t, grcJSONRequest(r, http.MethodPost, "/api/grc/findings",
		`{"reference":"FND-ACTOR","title":"X","assessmentId":`+jsonUint(assessment.ID)+
			`,"category":"OBSERVATION","severity":"LOW","status":"OPEN","userId":99}`).Body.Bytes())
	resolve := `{"reference":"FND-ACTOR","title":"X","assessmentId":` + jsonUint(assessment.ID) +
		`,"controlImplementationId":` + jsonUint(f.implementation.ID) + `,"category":"OBSERVATION","severity":"LOW","status":"RESOLVED"}`
	if code := grcJSONRequest(r, http.MethodPut, "/api/grc/findings/"+jsonUint(finding.ID), resolve).Code; code != http.StatusOK {
		t.Fatalf("resolve status=%d", code)
	}
	if code := grcJSONRequest(r, http.MethodPost, "/api/grc/findings/"+jsonUint(finding.ID)+"/close", "").Code; code != http.StatusOK {
		t.Fatalf("close status=%d", code)
	}
	accepted := decodeJSON[Finding](t, grcJSONRequest(r, http.MethodPost, "/api/grc/findings",
		`{"reference":"FND-ACTOR-ACCEPT","title":"X","assessmentId":`+jsonUint(assessment.ID)+
			`,"category":"OBSERVATION","severity":"LOW","status":"OPEN"}`).Body.Bytes())
	if code := grcJSONRequest(r, http.MethodPost, "/api/grc/findings/"+jsonUint(accepted.ID)+"/accept",
		`{"closureNote":"Accepté","actor":99}`).Code; code != http.StatusOK {
		t.Fatalf("accept status=%d", code)
	}

	// assessment create + complete, audit complete, finding create + resolve +
	// close, second finding create + accept.
	requireEventCount(t, bus, 8, "HTTP mutations")
	statuses := map[string]bool{}
	for i := range bus.events {
		event := requireAuditEvent(t, bus, i)
		if event.UserID == nil || *event.UserID != 7 {
			t.Fatalf("event %d %s/%s actor=%v, want 7", i, event.Module, event.Action, event.UserID)
		}
		if event.NewValue != "" {
			statuses[event.Module+":"+decodeAuditJSON(t, event.NewValue)["status"].(string)] = true
		}
	}
	for _, want := range []string{
		"GRC_ASSESSMENT:COMPLETED", "GRC_AUDIT_ENGAGEMENT:COMPLETED", "GRC_FINDING:CLOSED", "GRC_FINDING:ACCEPTED",
	} {
		if !statuses[want] {
			t.Errorf("sensitive transition %s not audited (got %v)", want, statuses)
		}
	}
}
