package grc

import (
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"
)

// GRC-08: the single due-state definition (Go, SQL and dashboard), registry
// filters, lengths and the GRC-01D.08 finding invariants.

func TestRemediationActionDueStateBoundaries(t *testing.T) {
	now := evidenceTestNow
	limit := now.AddDate(0, 0, 30)
	at := func(d time.Duration) *time.Time { v := now.Add(d); return &v }
	after := limit.Add(time.Second)

	for _, tc := range []struct {
		name   string
		status string
		due    *time.Time
		want   string
	}{
		{"done, even if late", RemediationActionStatusDone, at(-time.Hour), RemediationActionDueClosed},
		{"cancelled", RemediationActionStatusCancelled, nil, RemediationActionDueClosed},
		{"open without due date", RemediationActionStatusOpen, nil, RemediationActionDueNoDueDate},
		{"past due", RemediationActionStatusInProgress, at(-time.Second), RemediationActionDueOverdue},
		{"blocked and late", RemediationActionStatusBlocked, at(-time.Hour), RemediationActionDueOverdue},
		{"due exactly at now", RemediationActionStatusOpen, at(0), RemediationActionDueSoon},
		{"window end", RemediationActionStatusOpen, &limit, RemediationActionDueSoon},
		{"after the window", RemediationActionStatusOpen, &after, RemediationActionDueUpcoming},
	} {
		if got := RemediationActionDueState(tc.status, tc.due, now, limit); got != tc.want {
			t.Errorf("%s: %s, want %s", tc.name, got, tc.want)
		}
	}
	if _, _, err := remediationActionDueCondition("LATE", now, limit); err == nil {
		t.Fatal("unknown due state accepted")
	}
}

// seedActionDueStates creates one action per boundary of the default window.
func seedActionDueStates(t *testing.T, f remediationActionFixture) {
	t.Helper()
	day := 24 * time.Hour
	at := func(d time.Duration) *time.Time { v := evidenceTestNow.Add(d); return &v }
	for _, a := range []struct {
		reference, status string
		due               *time.Time
	}{
		{"ACT-NODUE", RemediationActionStatusOpen, nil},
		{"ACT-LATE", RemediationActionStatusOpen, at(-time.Second)},
		{"ACT-NOW", RemediationActionStatusInProgress, at(0)},
		{"ACT-END", RemediationActionStatusBlocked, at(30 * day)},
		{"ACT-LATER", RemediationActionStatusOpen, at(30*day + time.Second)},
		{"ACT-DONE", RemediationActionStatusDone, at(-day)},
		{"ACT-CANCELLED", RemediationActionStatusCancelled, at(day)},
	} {
		r := f.actionRequest(a.reference)
		r.Status, r.DueAt = a.status, a.due
		f.createAction(t, r)
	}
}

func TestRemediationActionDueFilterMatchesDefinitionAndDashboard(t *testing.T) {
	f := newRemediationActionFixture(t)
	if err := f.db.AutoMigrate(dashboardModels()...); err != nil {
		t.Fatal(err)
	}
	seedActionDueStates(t, f)

	limit := evidenceTestNow.AddDate(0, 0, 30)
	all, err := f.svc.ListRemediationActions(RemediationActionFilter{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	byState := map[string][]string{}
	for _, a := range all.Items {
		if want := RemediationActionDueState(a.Status, a.DueAt, evidenceTestNow, limit); a.DueState != want {
			t.Fatalf("%s dueState=%s, want %s", a.Reference, a.DueState, want)
		}
		byState[a.DueState] = append(byState[a.DueState], a.Reference)
	}
	for state, want := range map[string]string{
		RemediationActionDueOverdue:   "ACT-LATE",
		RemediationActionDueSoon:      "ACT-END,ACT-NOW",
		RemediationActionDueUpcoming:  "ACT-LATER",
		RemediationActionDueNoDueDate: "ACT-NODUE",
		RemediationActionDueClosed:    "ACT-CANCELLED,ACT-DONE",
	} {
		page, err := f.svc.ListRemediationActions(RemediationActionFilter{DueState: strings.ToLower(state), Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		got := []string{}
		for _, a := range page.Items {
			got = append(got, a.Reference)
		}
		sort.Strings(got)
		if strings.Join(got, ",") != want {
			t.Fatalf("%s filter = %v, want %s", state, got, want)
		}
		sorted := append([]string{}, byState[state]...)
		sort.Strings(sorted)
		if strings.Join(sorted, ",") != want {
			t.Fatalf("%s computed = %v, want %s", state, sorted, want)
		}
	}

	// The dashboard counts the same states.
	dashboard, err := f.svc.Dashboard(defaultDashboardFilter())
	if err != nil {
		t.Fatal(err)
	}
	open := int64(len(all.Items) - len(byState[RemediationActionDueClosed]))
	if d := dashboard.Actions; d.Open != open || d.Overdue != int64(len(byState[RemediationActionDueOverdue])) ||
		d.DueSoon != int64(len(byState[RemediationActionDueSoon])) {
		t.Fatalf("dashboard %+v diverges from %v", d, byState)
	}
	// The legacy overdue filter keeps the same meaning.
	yes := true
	overdue, err := f.svc.ListRemediationActions(RemediationActionFilter{Overdue: &yes})
	if err != nil || overdue.Total != 1 || overdue.Items[0].Reference != "ACT-LATE" {
		t.Fatalf("overdue filter: %v %+v", err, overdue)
	}

	// Window and invalid inputs.
	page, err := f.svc.ListRemediationActions(RemediationActionFilter{DueState: "DUE_SOON", DueSoonDays: 1})
	if err != nil || page.Total != 1 || page.Items[0].Reference != "ACT-NOW" {
		t.Fatalf("one-day window: %v %+v", err, page)
	}
	for _, bad := range []RemediationActionFilter{{DueState: "LATE"}, {DueSoonDays: -1}, {DueSoonDays: 366}} {
		_, err := f.svc.ListRemediationActions(bad)
		requireServiceError(t, err, http.StatusBadRequest)
	}
	found, err := f.svc.FindRemediationActionWithWindow(page.Items[0].ID, 1)
	if err != nil || found.DueState != RemediationActionDueSoon {
		t.Fatalf("detail due state: %v %+v", err, found)
	}
}

func TestRemediationActionRegistrySearchAndValidation(t *testing.T) {
	f := newRemediationActionFixture(t)
	seedActionDueStates(t, f)
	riskAction := f.actionRequest("ACT-RISK-08")
	riskAction.Title = "Action sur le risque"
	riskAction.ControlImplementationID, riskAction.RiskID = nil, &f.risk.ID
	created := f.createAction(t, riskAction)

	// The search never widens status, risk or due filters.
	page, err := f.svc.ListRemediationActions(RemediationActionFilter{Search: "act-", Status: "open", RiskID: &f.risk.ID, DueState: "NO_DUE_DATE"})
	if err != nil || page.Total != 1 || page.Items[0].ID != created.ID {
		t.Fatalf("search + status + risk + due state: %v %+v", err, page)
	}
	page, err = f.svc.ListRemediationActions(RemediationActionFilter{Search: "act-", DueState: "OVERDUE", RiskID: &f.risk.ID})
	if err != nil || page.Total != 0 || page.Items == nil {
		t.Fatalf("empty combination: %v %+v", err, page)
	}
	page, err = f.svc.ListRemediationActions(RemediationActionFilter{Page: 2, Limit: 3})
	if err != nil || page.Total != 8 || page.TotalPages != 3 || len(page.Items) != 3 {
		t.Fatalf("pagination: %v %+v", err, page)
	}

	r := f.actionRequest(strings.Repeat("É", remediationActionReferenceMaxLength))
	r.Title = strings.Repeat("é", remediationActionTitleMaxLength)
	if _, err := f.svc.SaveRemediationAction(0, 1, r); err != nil {
		t.Fatalf("limits rejected: %v", err)
	}
	r.Reference = strings.Repeat("É", remediationActionReferenceMaxLength+1)
	_, err = f.svc.SaveRemediationAction(0, 1, r)
	requireServiceError(t, err, http.StatusBadRequest)
	r.Reference, r.Title = "ACT-LONG", strings.Repeat("é", remediationActionTitleMaxLength+1)
	_, err = f.svc.SaveRemediationAction(0, 1, r)
	requireServiceError(t, err, http.StatusBadRequest)
}

// TestGRC08FindingInvariants re-asserts the GRC-01D.08 rules in one place.
func TestGRC08FindingInvariants(t *testing.T) {
	f := newActionFindingFixture(t)
	unknown := uint(999999)

	_, err := f.svc.SaveRemediationAction(0, 1, f.findingAction("ACT-INV-404", unknown))
	requireServiceError(t, err, http.StatusNotFound)
	_, err = f.svc.SaveRemediationAction(0, 1, f.findingAction("ACT-INV-CLOSED", f.closed.ID))
	requireServiceError(t, err, http.StatusConflict)
	_, err = f.svc.SaveRemediationAction(0, 1, f.findingAction("ACT-INV-ACCEPTED", f.accepted.ID))
	requireServiceError(t, err, http.StatusConflict)

	// Retarget an existing action to a terminal finding.
	moving := f.findingAction("ACT-INV-MOVE", f.plain.ID)
	movingAction, err := f.svc.SaveRemediationAction(0, 1, moving)
	if err != nil {
		t.Fatal(err)
	}
	moving.FindingID = &f.closed.ID
	_, err = f.svc.SaveRemediationAction(movingAction.ID, 1, moving)
	requireServiceError(t, err, http.StatusConflict)
	moving.FindingID = &f.accepted.ID
	_, err = f.svc.SaveRemediationAction(movingAction.ID, 1, moving)
	requireServiceError(t, err, http.StatusConflict)

	// Closure guard: blocked while an action is open, then allowed.
	findingRequest := f.findingRequest("FND-INV-CLOSE", f.audit.ID)
	finding := f.createFinding(t, findingRequest)
	action := f.findingAction("ACT-INV-GUARD", finding.ID)
	guard, err := f.svc.SaveRemediationAction(0, 1, action)
	if err != nil {
		t.Fatal(err)
	}
	cancelled := f.findingAction("ACT-INV-CANCELLED", finding.ID)
	cancelled.Status = RemediationActionStatusCancelled
	if _, err := f.svc.SaveRemediationAction(0, 1, cancelled); err != nil {
		t.Fatal(err)
	}
	findingRequest.Status = FindingStatusResolved
	if _, err := f.svc.SaveFinding(finding.ID, 1, findingRequest); err != nil {
		t.Fatal(err)
	}
	f.setClock(evidenceTestNow.Add(time.Hour))
	findingRequest.Status = FindingStatusClosed
	_, err = f.svc.SaveFinding(finding.ID, 1, findingRequest)
	requireServiceError(t, err, http.StatusConflict)
	action.Status = RemediationActionStatusDone
	if _, err := f.svc.SaveRemediationAction(guard.ID, 1, action); err != nil {
		t.Fatal(err)
	}
	closed, err := f.svc.SaveFinding(finding.ID, 1, findingRequest)
	if err != nil || closed.Status != FindingStatusClosed {
		t.Fatalf("closure with every action DONE/CANCELLED: %v %+v", err, closed)
	}

	// A linked action stays editable after its finding became terminal.
	action.Title = "Titre corrigé après clôture"
	edited, err := f.svc.SaveRemediationAction(guard.ID, 2, action)
	if err != nil || edited.Title != "Titre corrigé après clôture" || edited.FindingID == nil || *edited.FindingID != finding.ID {
		t.Fatalf("historical edit after closure: %v %+v", err, edited)
	}
}
