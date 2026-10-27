package grc

import (
	"net/http"
	"testing"
)

type actionFindingFixture struct {
	grc01DServiceFixture
	audit    AuditEngagement
	targeted Finding // on f.implementation and f.risk
	plain    Finding // no control implementation nor risk
	closed   Finding
	accepted Finding
}

func newActionFindingFixture(t *testing.T) actionFindingFixture {
	t.Helper()

	f := actionFindingFixture{grc01DServiceFixture: newGRC01DServiceFixture(t)}
	f.audit = f.inProgressAudit(t, "AUD-ACT-FND")

	targeted := f.findingRequest("FND-TARGETED", f.audit.ID)
	targeted.ControlImplementationID = &f.implementation.ID
	targeted.RiskID = &f.risk.ID
	f.targeted = f.createFinding(t, targeted)
	f.plain = f.createFinding(t, f.findingRequest("FND-PLAIN", f.audit.ID))

	closed := f.findingRequest("FND-CLOSED", f.audit.ID)
	f.closed = f.createFinding(t, closed)
	for _, status := range []string{FindingStatusResolved, FindingStatusClosed} {
		closed.Status = status
		updated, err := f.svc.SaveFinding(f.closed.ID, 10, closed)
		if err != nil {
			t.Fatal(err)
		}
		f.closed = *updated
	}

	accepted := f.findingRequest("FND-ACCEPTED", f.audit.ID)
	f.accepted = f.createFinding(t, accepted)
	accepted.Status = FindingStatusAccepted
	accepted.ClosureNote = "Risque accepté"
	updated, err := f.svc.SaveFinding(f.accepted.ID, 10, accepted)
	if err != nil {
		t.Fatal(err)
	}
	f.accepted = *updated
	return f
}

func (f actionFindingFixture) findingAction(reference string, findingID uint) RemediationActionRequest {
	r := f.actionRequest(reference)
	r.ControlImplementationID = nil
	r.FindingID = &findingID
	return r
}

func TestRemediationActionFindingTargetRules(t *testing.T) {
	f := newActionFindingFixture(t)

	findingOnly, err := f.svc.SaveRemediationAction(0, 31, f.findingAction("ACT-FND-ONLY", f.plain.ID))
	if err != nil {
		t.Fatal(err)
	}
	if findingOnly.FindingID == nil || *findingOnly.FindingID != f.plain.ID || findingOnly.Finding == nil ||
		findingOnly.ControlImplementationID != nil || findingOnly.RiskID != nil {
		t.Fatalf("finding-only action: %+v", findingOnly)
	}

	accepted := map[string]func(*RemediationActionRequest){
		"finding with its implementation": func(r *RemediationActionRequest) { r.ControlImplementationID = &f.implementation.ID },
		"finding with its risk":           func(r *RemediationActionRequest) { r.RiskID = &f.risk.ID },
		"finding with both of its targets": func(r *RemediationActionRequest) {
			r.ControlImplementationID, r.RiskID = &f.implementation.ID, &f.risk.ID
		},
	}
	i := 0
	for name, mutate := range accepted {
		i++
		r := f.findingAction("ACT-FND-OK-"+string(rune('A'+i)), f.targeted.ID)
		mutate(&r)
		if _, err := f.svc.SaveRemediationAction(0, 31, r); err != nil {
			t.Fatalf("%s rejected: %v", name, err)
		}
	}

	// A finding without implementation constrains nothing.
	free := f.findingAction("ACT-FND-FREE", f.plain.ID)
	free.ControlImplementationID = &f.other.ID
	if _, err := f.svc.SaveRemediationAction(0, 31, free); err != nil {
		t.Fatalf("finding without implementation must accept any implementation: %v", err)
	}

	zero := uint(0)
	unknown := uint(999999)
	cases := []struct {
		name   string
		status int
		mutate func(*RemediationActionRequest)
	}{
		{"zero finding", http.StatusBadRequest, func(r *RemediationActionRequest) { r.FindingID = &zero }},
		{"no target", http.StatusBadRequest, func(r *RemediationActionRequest) { r.FindingID = nil }},
		{"unknown finding", http.StatusNotFound, func(r *RemediationActionRequest) { r.FindingID = &unknown }},
		{"closed finding", http.StatusConflict, func(r *RemediationActionRequest) { r.FindingID = &f.closed.ID }},
		{"accepted finding", http.StatusConflict, func(r *RemediationActionRequest) { r.FindingID = &f.accepted.ID }},
		{"other implementation", http.StatusConflict, func(r *RemediationActionRequest) { r.ControlImplementationID = &f.other.ID }},
		{"other risk", http.StatusConflict, func(r *RemediationActionRequest) { r.RiskID = &f.otherRisk.ID }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := f.findingAction("ACT-FND-BAD", f.targeted.ID)
			tc.mutate(&r)
			_, err := f.svc.SaveRemediationAction(0, 31, r)
			expectAppStatus(t, err, tc.status)
		})
	}

	page, err := f.svc.ListRemediationActions(RemediationActionFilter{FindingID: &f.targeted.ID})
	if err != nil || page.Total != 3 {
		t.Fatalf("filter by finding: %v %+v", err, page)
	}
	_, err = f.svc.ListRemediationActions(RemediationActionFilter{FindingID: &zero})
	expectAppStatus(t, err, http.StatusBadRequest)
}

func TestRemediationActionFindingUpdateCannotBypassRules(t *testing.T) {
	f := newActionFindingFixture(t)

	r := f.findingAction("ACT-FND-UPD", f.targeted.ID)
	action, err := f.svc.SaveRemediationAction(0, 32, r)
	if err != nil {
		t.Fatal(err)
	}

	unknown := uint(999999)
	cases := []struct {
		name   string
		status int
		mutate func(*RemediationActionRequest)
	}{
		{"to closed finding", http.StatusConflict, func(r *RemediationActionRequest) { r.FindingID = &f.closed.ID }},
		{"to accepted finding", http.StatusConflict, func(r *RemediationActionRequest) { r.FindingID = &f.accepted.ID }},
		{"to unknown finding", http.StatusNotFound, func(r *RemediationActionRequest) { r.FindingID = &unknown }},
		{"incompatible implementation", http.StatusConflict, func(r *RemediationActionRequest) { r.ControlImplementationID = &f.other.ID }},
		{"incompatible risk", http.StatusConflict, func(r *RemediationActionRequest) { r.RiskID = &f.otherRisk.ID }},
		{"drop every target", http.StatusBadRequest, func(r *RemediationActionRequest) { r.FindingID = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			next := r
			tc.mutate(&next)
			_, err := f.svc.SaveRemediationAction(action.ID, 33, next)
			expectAppStatus(t, err, tc.status)
		})
	}
	reloaded, err := f.svc.FindRemediationAction(action.ID)
	if err != nil || reloaded.FindingID == nil || *reloaded.FindingID != f.targeted.ID || reloaded.UpdatedBy != 32 {
		t.Fatalf("refused updates must not be written: %v %+v", err, reloaded)
	}

	// Moving to another open finding is a valid change.
	r.FindingID = &f.plain.ID
	if moved, err := f.svc.SaveRemediationAction(action.ID, 34, r); err != nil || *moved.FindingID != f.plain.ID {
		t.Fatalf("move to an open finding: %v %+v", err, moved)
	}

	// Historical: once the finding is accepted, the existing action stays editable.
	accept := f.findingRequest("FND-PLAIN", f.audit.ID)
	accept.Status = FindingStatusAccepted
	accept.ClosureNote = "Accepté"
	if _, err := f.svc.SaveFinding(f.plain.ID, 35, accept); err != nil {
		t.Fatal(err)
	}
	r.Title = "Titre historique"
	if _, err := f.svc.SaveRemediationAction(action.ID, 36, r); err != nil {
		t.Fatalf("historical action on an accepted finding rejected: %v", err)
	}
}

func TestRemediationActionFindingAudit(t *testing.T) {
	f := newActionFindingFixture(t)
	bus := withCaptureBus(t)

	r := f.findingAction("ACT-FND-AUDIT", f.plain.ID)
	action, err := f.svc.SaveRemediationAction(0, 41, r)
	if err != nil {
		t.Fatal(err)
	}
	_, next := assertAuditEvent(t, bus, 0, auditModuleGRCRemediationAction, auditActionCreate, action.ID, 41)
	assertSnapshotKeysExactly(t, next, remediationActionAuditAllowlist, remediationActionAuditForbiddenKeys)
	if next["findingId"] != float64(f.plain.ID) {
		t.Fatalf("create snapshot findingId=%v", next["findingId"])
	}

	r.FindingID = &f.targeted.ID
	if _, err := f.svc.SaveRemediationAction(action.ID, 42, r); err != nil {
		t.Fatal(err)
	}
	old, next := assertAuditEvent(t, bus, 1, auditModuleGRCRemediationAction, auditActionUpdate, action.ID, 42)
	if old["findingId"] != float64(f.plain.ID) || next["findingId"] != float64(f.targeted.ID) {
		t.Fatalf("update snapshots: old=%v new=%v", old["findingId"], next["findingId"])
	}

	// Refused targets publish nothing.
	r.FindingID = &f.closed.ID
	if _, err := f.svc.SaveRemediationAction(action.ID, 43, r); err == nil {
		t.Fatal("expected refused target")
	}
	requireEventCount(t, bus, 2, "refused finding target")
}

// End to end through the real router: an action created by the API blocks
// the closure of its finding until it is done.
func TestRemediationActionFindingClosureAPI(t *testing.T) {
	f := newActionFindingFixture(t)
	permissions := append(append([]string{}, writerPermissions...), "grc.action.create", "grc.action.update")
	r := grcServiceRouter(f.svc, permissions)

	w := grcJSONRequest(r, http.MethodPost, "/api/grc/findings", `{"reference":"FND-E2E","title":"Constat E2E","auditEngagementId":`+
		jsonUint(f.audit.ID)+`,"category":"CONTROL_DEFICIENCY","severity":"HIGH","status":"OPEN"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create finding status=%d body=%s", w.Code, w.Body.String())
	}
	finding := decodeJSON[Finding](t, w.Body.Bytes())
	findingPath := "/api/grc/findings/" + jsonUint(finding.ID)

	actionBodyFor := func(reference, findingID, status string) string {
		return `{"reference":"` + reference + `","title":"Action E2E","findingId":` + findingID +
			`,"priority":"HIGH","status":"` + status + `","createdBy":99}`
	}
	actionBody := func(findingID, status string) string { return actionBodyFor("ACT-E2E", findingID, status) }
	w = grcJSONRequest(r, http.MethodPost, "/api/grc/actions", actionBody(jsonUint(finding.ID), "OPEN"))
	if w.Code != http.StatusCreated {
		t.Fatalf("create action status=%d body=%s", w.Code, w.Body.String())
	}
	action := decodeJSON[RemediationAction](t, w.Body.Bytes())
	if action.FindingID == nil || *action.FindingID != finding.ID || action.Finding == nil || action.CreatedBy != 7 {
		t.Fatalf("action response must expose its finding target: %+v", action)
	}

	resolve := `{"reference":"FND-E2E","title":"Constat E2E","auditEngagementId":` + jsonUint(f.audit.ID) +
		`,"category":"CONTROL_DEFICIENCY","severity":"HIGH","status":"RESOLVED"}`
	if code := grcJSONRequest(r, http.MethodPut, findingPath, resolve).Code; code != http.StatusOK {
		t.Fatalf("resolve finding status=%d", code)
	}
	if code := grcJSONRequest(r, http.MethodPost, findingPath+"/close", "").Code; code != http.StatusConflict {
		t.Fatalf("close with an active action status=%d", code)
	}

	actionPath := "/api/grc/actions/" + jsonUint(action.ID)
	if w := grcJSONRequest(r, http.MethodPut, actionPath, actionBody(jsonUint(finding.ID), "DONE")); w.Code != http.StatusOK {
		t.Fatalf("complete action status=%d body=%s", w.Code, w.Body.String())
	}
	w = grcJSONRequest(r, http.MethodPost, findingPath+"/close", "")
	if w.Code != http.StatusOK {
		t.Fatalf("close after the action is done status=%d body=%s", w.Code, w.Body.String())
	}
	if closed := decodeJSON[Finding](t, w.Body.Bytes()); closed.Status != FindingStatusClosed {
		t.Fatalf("finding not closed: %+v", closed)
	}

	for name, tc := range map[string]struct {
		reference, findingID string
		status               int
	}{
		"unknown finding":   {"ACT-E2E-UNKNOWN", "999999", http.StatusNotFound},
		"closed finding":    {"ACT-E2E-CLOSED", jsonUint(finding.ID), http.StatusConflict},
		"accepted finding":  {"ACT-E2E-ACCEPTED", jsonUint(f.accepted.ID), http.StatusConflict},
		"zero finding":      {"ACT-E2E-ZERO", "0", http.StatusBadRequest},
		"malformed finding": {"ACT-E2E-MALFORMED", `"abc"`, http.StatusBadRequest},
	} {
		body := actionBodyFor(tc.reference, tc.findingID, "OPEN")
		if w := grcJSONRequest(r, http.MethodPost, "/api/grc/actions", body); w.Code != tc.status {
			t.Errorf("%s: status=%d want %d body=%s", name, w.Code, tc.status, w.Body.String())
		}
	}

	w = grcRequest(r, http.MethodGet, "/api/grc/actions?findingId="+jsonUint(finding.ID))
	if page := decodeJSON[RemediationActionPage](t, w.Body.Bytes()); w.Code != http.StatusOK || page.Total != 1 {
		t.Fatalf("list by finding status=%d body=%s", w.Code, w.Body.String())
	}
	if code := grcRequest(r, http.MethodGet, "/api/grc/actions?findingId=0").Code; code != http.StatusBadRequest {
		t.Fatalf("list with findingId=0 status=%d", code)
	}
}
