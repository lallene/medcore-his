package grc

import (
	"net/http"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/organization"
	"github.com/lallene/medcore-his/backend/internal/modules/staff"
)

type remediationActionFixture struct {
	evidenceServiceFixture
	risk         Risk
	otherRisk    Risk
	unlinkedImpl ControlImplementation
}

// newRemediationActionFixture links implementation's control to risk only:
// otherRisk and unlinkedImpl (another control) have no ControlRisk link.
func newRemediationActionFixture(t *testing.T) remediationActionFixture {
	t.Helper()

	base := newEvidenceServiceFixture(t)
	svc := base.svc

	asset := createRiskAsset(t, base.assetFixture)
	riskReq := validRiskRequest(base.assetFixture, asset.ID)
	risk, err := svc.SaveRisk(0, 10, riskReq)
	if err != nil {
		t.Fatal(err)
	}
	riskReq.Reference = "RISK-OTHER"
	otherRisk, err := svc.SaveRisk(0, 10, riskReq)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.LinkControlRisk(10, ControlRiskRequest{ControlID: base.implementation.ControlID, RiskID: risk.ID}); err != nil {
		t.Fatal(err)
	}

	otherControl := createControl(t, svc, base.assetFixture, "CTL-UNLINKED", ControlStatusActive)
	unlinked, err := svc.SaveControlImplementation(0, 10, ControlImplementationRequest{
		ControlID: otherControl.ID, AssetID: asset.ID, Status: ControlImplementationStatusPlanned,
	})
	if err != nil {
		t.Fatal(err)
	}

	return remediationActionFixture{
		evidenceServiceFixture: base,
		risk:                   *risk,
		otherRisk:              *otherRisk,
		unlinkedImpl:           *unlinked,
	}
}

func (f remediationActionFixture) actionRequest(reference string) RemediationActionRequest {
	return RemediationActionRequest{
		Reference:               reference,
		Title:                   "Action " + reference,
		ControlImplementationID: &f.implementation.ID,
		Priority:                RemediationActionPriorityHigh,
		Status:                  RemediationActionStatusOpen,
	}
}

func (f remediationActionFixture) createAction(t *testing.T, r RemediationActionRequest) RemediationAction {
	t.Helper()

	action, err := f.svc.SaveRemediationAction(0, 10, r)
	if err != nil {
		t.Fatal(err)
	}
	return *action
}

func requireCompletedAtNull(t *testing.T, f remediationActionFixture, id uint) {
	t.Helper()

	var count int64
	if err := f.db.Model(&RemediationAction{}).Where("id = ? AND completed_at IS NULL", id).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("action %d: completed_at must be NULL in database", id)
	}
}

func TestSaveRemediationActionCreatesTargets(t *testing.T) {
	f := newRemediationActionFixture(t)

	controlOnly, err := f.svc.SaveRemediationAction(0, 41, RemediationActionRequest{
		Reference:               " act-ci ",
		Title:                   " Chiffrer ",
		ControlImplementationID: &f.implementation.ID,
		AssigneeProfileID:       &f.profile.ID,
		OwningServiceID:         &f.service.ID,
		Priority:                " critical ",
		Status:                  " open ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if controlOnly.Reference != "ACT-CI" || controlOnly.Title != "Chiffrer" ||
		controlOnly.Priority != RemediationActionPriorityCritical || controlOnly.Status != RemediationActionStatusOpen ||
		controlOnly.CreatedBy != 41 || controlOnly.UpdatedBy != 41 {
		t.Fatalf("unexpected action: %+v", controlOnly)
	}
	if controlOnly.ControlImplementation == nil || controlOnly.ControlImplementation.Control == nil ||
		controlOnly.ControlImplementation.Asset == nil || controlOnly.AssigneeProfile == nil || controlOnly.OwningService == nil {
		t.Fatalf("relations not preloaded: %+v", controlOnly)
	}

	riskOnly := f.actionRequest("ACT-RISK")
	riskOnly.ControlImplementationID = nil
	riskOnly.RiskID = &f.risk.ID
	created := f.createAction(t, riskOnly)
	if created.Risk == nil || created.ControlImplementation != nil {
		t.Fatalf("unexpected risk-only action: %+v", created)
	}

	both := f.actionRequest("ACT-BOTH")
	both.RiskID = &f.risk.ID
	created = f.createAction(t, both)
	if created.Risk == nil || created.ControlImplementation == nil {
		t.Fatalf("unexpected two-target action: %+v", created)
	}
}

func TestSaveRemediationActionRejectsInvalidCreations(t *testing.T) {
	f := newRemediationActionFixture(t)
	f.createAction(t, f.actionRequest("ACT-DUP"))
	unknown := uint(999999)

	cases := []struct {
		name   string
		status int
		mutate func(*RemediationActionRequest)
	}{
		{"two targets without control risk", http.StatusConflict, func(r *RemediationActionRequest) { r.RiskID = &f.otherRisk.ID }},
		{"unlinked control with risk", http.StatusConflict, func(r *RemediationActionRequest) {
			r.ControlImplementationID = &f.unlinkedImpl.ID
			r.RiskID = &f.risk.ID
		}},
		{"no target", http.StatusBadRequest, func(r *RemediationActionRequest) { r.ControlImplementationID = nil }},
		{"unknown control implementation", http.StatusNotFound, func(r *RemediationActionRequest) { r.ControlImplementationID = &unknown }},
		{"unknown risk", http.StatusNotFound, func(r *RemediationActionRequest) {
			r.ControlImplementationID = nil
			r.RiskID = &unknown
		}},
		{"duplicate reference", http.StatusConflict, func(r *RemediationActionRequest) { r.Reference = " act-dup " }},
		{"unknown assignee", http.StatusNotFound, func(r *RemediationActionRequest) { r.AssigneeProfileID = &unknown }},
		{"inactive assignee", http.StatusConflict, func(r *RemediationActionRequest) { r.AssigneeProfileID = &f.inactiveProfile.ID }},
		{"unknown service", http.StatusNotFound, func(r *RemediationActionRequest) { r.OwningServiceID = &unknown }},
		{"inactive service", http.StatusConflict, func(r *RemediationActionRequest) { r.OwningServiceID = &f.inactiveService.ID }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := f.actionRequest("ACT-NEW")
			tc.mutate(&r)

			_, err := f.svc.SaveRemediationAction(0, 10, r)
			expectAppStatus(t, err, tc.status)
		})
	}

	_, err := f.svc.SaveRemediationAction(999999, 10, f.actionRequest("ACT-MISSING"))
	expectAppStatus(t, err, http.StatusNotFound)
}

func TestSaveRemediationActionUpdatesTargets(t *testing.T) {
	f := newRemediationActionFixture(t)

	r := f.actionRequest("ACT-UPD")
	created, err := f.svc.SaveRemediationAction(0, 41, r)
	if err != nil {
		t.Fatal(err)
	}

	r.Title = "Titre modifié"
	updated, err := f.svc.SaveRemediationAction(created.ID, 52, r)
	if err != nil {
		t.Fatal(err)
	}
	if updated.CreatedBy != 41 || updated.UpdatedBy != 52 || updated.Title != "Titre modifié" {
		t.Fatalf("unexpected update: %+v", updated)
	}

	// Change the control implementation.
	r.ControlImplementationID = &f.other.ID
	if updated, err = f.svc.SaveRemediationAction(created.ID, 52, r); err != nil {
		t.Fatal(err)
	}
	if *updated.ControlImplementationID != f.other.ID {
		t.Fatalf("control implementation not changed: %+v", updated)
	}

	// Single target to both targets: f.other shares the control linked to risk.
	r.RiskID = &f.risk.ID
	if updated, err = f.svc.SaveRemediationAction(created.ID, 52, r); err != nil {
		t.Fatalf("adding a linked risk rejected: %v", err)
	}
	if updated.RiskID == nil || *updated.RiskID != f.risk.ID {
		t.Fatalf("risk not added: %+v", updated)
	}

	// Changing a target revalidates the control-risk link.
	next := r
	next.RiskID = &f.otherRisk.ID
	_, err = f.svc.SaveRemediationAction(created.ID, 53, next)
	expectAppStatus(t, err, http.StatusConflict)

	next = r
	next.ControlImplementationID = &f.unlinkedImpl.ID
	_, err = f.svc.SaveRemediationAction(created.ID, 53, next)
	expectAppStatus(t, err, http.StatusConflict)

	// Risk only: change the risk.
	next = r
	next.ControlImplementationID = nil
	next.RiskID = &f.otherRisk.ID
	if updated, err = f.svc.SaveRemediationAction(created.ID, 54, next); err != nil {
		t.Fatalf("changing the risk of a risk-only action rejected: %v", err)
	}
	if updated.ControlImplementationID != nil || *updated.RiskID != f.otherRisk.ID {
		t.Fatalf("unexpected risk-only update: %+v", updated)
	}
}

func TestSaveRemediationActionKeepsHistoricalTargetsAndPeople(t *testing.T) {
	f := newRemediationActionFixture(t)
	assignee := createExtraProfile(t, f.evidenceServiceFixture, "GRC-ACT-ASSIGNEE")
	service := createExtraService(t, f.evidenceServiceFixture, "ACT-SVC")

	r := f.actionRequest("ACT-HIST")
	r.RiskID = &f.risk.ID
	r.AssigneeProfileID = &assignee.ID
	r.OwningServiceID = &service.ID
	created := f.createAction(t, r)

	if err := f.svc.UnlinkControlRisk(10, f.implementation.ControlID, f.risk.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Model(&staff.Profile{}).Where("id = ?", assignee.ID).Update("active", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Model(&organization.Service{}).Where("id = ?", service.ID).Update("active", false).Error; err != nil {
		t.Fatal(err)
	}

	r.Title = "Titre historique modifié"
	r.Priority = RemediationActionPriorityLow
	updated, err := f.svc.SaveRemediationAction(created.ID, 60, r)
	if err != nil {
		t.Fatalf("historical action update rejected: %v", err)
	}
	if updated.Title != "Titre historique modifié" || updated.Priority != RemediationActionPriorityLow {
		t.Fatalf("unexpected historical update: %+v", updated)
	}

	for name, mutate := range map[string]func(*RemediationActionRequest){
		"inactive assignee": func(r *RemediationActionRequest) { r.AssigneeProfileID = &f.inactiveProfile.ID },
		"inactive service":  func(r *RemediationActionRequest) { r.OwningServiceID = &f.inactiveService.ID },
	} {
		t.Run(name, func(t *testing.T) {
			next := r
			mutate(&next)

			_, err := f.svc.SaveRemediationAction(created.ID, 61, next)
			expectAppStatus(t, err, http.StatusConflict)
		})
	}
}

func TestSaveRemediationActionCompletion(t *testing.T) {
	f := newRemediationActionFixture(t)
	past := evidenceTestNow.Add(-24 * time.Hour)

	r := f.actionRequest("ACT-DONE")
	created := f.createAction(t, r)

	r.Status = RemediationActionStatusDone
	done, err := f.svc.SaveRemediationAction(created.ID, 20, r)
	if err != nil {
		t.Fatal(err)
	}
	if done.CompletedAt == nil || !done.CompletedAt.Equal(evidenceTestNow) {
		t.Fatalf("CompletedAt=%v want %v", done.CompletedAt, evidenceTestNow)
	}

	// Editing a completed action without CompletedAt keeps the original date.
	f.svc.now = func() time.Time { return evidenceTestNow.Add(time.Hour) }
	r.Title = "Titre après réalisation"
	done, err = f.svc.SaveRemediationAction(created.ID, 21, r)
	if err != nil {
		t.Fatal(err)
	}
	if done.CompletedAt == nil || !done.CompletedAt.Equal(evidenceTestNow) {
		t.Fatalf("completion date changed on edit: %v", done.CompletedAt)
	}
	f.svc.now = func() time.Time { return evidenceTestNow }

	for name, completedAt := range map[string]time.Time{"past": past, "now": evidenceTestNow} {
		t.Run(name, func(t *testing.T) {
			req := f.actionRequest("ACT-DONE-" + name)
			req.Status = RemediationActionStatusDone
			req.CompletedAt = timePtr(completedAt)

			action, err := f.svc.SaveRemediationAction(0, 22, req)
			if err != nil {
				t.Fatal(err)
			}
			if action.CompletedAt == nil || !action.CompletedAt.Equal(completedAt) {
				t.Fatalf("CompletedAt=%v want %v", action.CompletedAt, completedAt)
			}
		})
	}

	future := f.actionRequest("ACT-DONE-FUTURE")
	future.Status = RemediationActionStatusDone
	future.CompletedAt = timePtr(evidenceTestNow.Add(time.Second))
	_, err = f.svc.SaveRemediationAction(0, 23, future)
	expectAppStatus(t, err, http.StatusBadRequest)

	var count int64
	if err = f.db.Model(&RemediationAction{}).Where("reference = ?", "ACT-DONE-FUTURE").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("future CompletedAt must not be written")
	}

	_, err = f.svc.SaveRemediationAction(created.ID, 24, RemediationActionRequest{
		Reference:               r.Reference,
		Title:                   r.Title,
		ControlImplementationID: r.ControlImplementationID,
		Priority:                r.Priority,
		Status:                  RemediationActionStatusDone,
		CompletedAt:             timePtr(evidenceTestNow.Add(time.Minute)),
	})
	expectAppStatus(t, err, http.StatusBadRequest)
	reloaded, err := f.svc.FindRemediationAction(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.UpdatedBy != 21 || !reloaded.CompletedAt.Equal(evidenceTestNow) {
		t.Fatalf("rejected future completion must not be written: %+v", reloaded)
	}
}

func TestSaveRemediationActionReopening(t *testing.T) {
	f := newRemediationActionFixture(t)

	for _, reopened := range []string{
		RemediationActionStatusOpen,
		RemediationActionStatusInProgress,
		RemediationActionStatusBlocked,
	} {
		t.Run(reopened, func(t *testing.T) {
			r := f.actionRequest("ACT-REOPEN-" + reopened)
			r.Status = RemediationActionStatusDone
			created := f.createAction(t, r)
			if created.CompletedAt == nil {
				t.Fatal("DONE action must have CompletedAt")
			}

			r.Status = reopened
			action, err := f.svc.SaveRemediationAction(created.ID, 30, r)
			if err != nil {
				t.Fatalf("DONE -> %s rejected: %v", reopened, err)
			}
			if action.CompletedAt != nil || action.Status != reopened {
				t.Fatalf("unexpected reopened action: %+v", action)
			}
			requireCompletedAtNull(t, f, created.ID)
		})
	}

	r := f.actionRequest("ACT-CANCELLED")
	r.Status = RemediationActionStatusCancelled
	created := f.createAction(t, r)

	r.Status = RemediationActionStatusOpen
	action, err := f.svc.SaveRemediationAction(created.ID, 31, r)
	if err != nil {
		t.Fatalf("CANCELLED -> OPEN rejected: %v", err)
	}
	if action.Status != RemediationActionStatusOpen || action.CompletedAt != nil {
		t.Fatalf("unexpected reopened action: %+v", action)
	}
}

func TestFindRemediationAction(t *testing.T) {
	f := newRemediationActionFixture(t)

	_, err := f.svc.FindRemediationAction(0)
	expectAppStatus(t, err, http.StatusBadRequest)

	_, err = f.svc.FindRemediationAction(999999)
	expectAppStatus(t, err, http.StatusNotFound)

	r := f.actionRequest("ACT-FIND")
	r.RiskID = &f.risk.ID
	r.AssigneeProfileID = &f.profile.ID
	r.OwningServiceID = &f.service.ID
	r.DueAt = timePtr(evidenceTestNow.Add(time.Hour))
	created := f.createAction(t, r)

	found, err := f.svc.FindRemediationAction(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if found.ControlImplementation == nil || found.ControlImplementation.Control == nil ||
		found.ControlImplementation.Asset == nil || found.Risk == nil ||
		found.AssigneeProfile == nil || found.OwningService == nil {
		t.Fatalf("relations not preloaded: %+v", found)
	}
	if found.Overdue {
		t.Fatal("action due in one hour must not be overdue")
	}

	f.svc.now = func() time.Time { return evidenceTestNow.Add(2 * time.Hour) }
	if found, err = f.svc.FindRemediationAction(created.ID); err != nil {
		t.Fatal(err)
	}
	if !found.Overdue {
		t.Fatal("action must be overdue with the later clock")
	}
}

func TestListRemediationActionsFiltersAndOrder(t *testing.T) {
	f := newRemediationActionFixture(t)
	assignee := createExtraProfile(t, f.evidenceServiceFixture, "GRC-ACT-LIST")
	service := createExtraService(t, f.evidenceServiceFixture, "ACT-LIST")

	late := f.actionRequest("ACT-LATE")
	late.DueAt = timePtr(evidenceTestNow.AddDate(0, 0, 20))
	late.Priority = RemediationActionPriorityLow
	lateAction := f.createAction(t, late)

	noDue := f.actionRequest("ACT-NODUE")
	noDue.Title = "Revue pare-feu"
	noDue.AssigneeProfileID = &assignee.ID
	noDueAction := f.createAction(t, noDue)

	soon := f.actionRequest("ACT-SOON")
	soon.ControlImplementationID = nil
	soon.RiskID = &f.risk.ID
	soon.DueAt = timePtr(evidenceTestNow.AddDate(0, 0, 5))
	soon.OwningServiceID = &service.ID
	soonAction := f.createAction(t, soon)

	sameDue := f.actionRequest("ACT-SAME")
	sameDue.ControlImplementationID = &f.other.ID
	sameDue.DueAt = late.DueAt
	sameDue.Status = RemediationActionStatusBlocked
	sameAction := f.createAction(t, sameDue)

	page, err := f.svc.ListRemediationActions(RemediationActionFilter{})
	if err != nil {
		t.Fatal(err)
	}
	want := []uint{soonAction.ID, lateAction.ID, sameAction.ID, noDueAction.ID}
	if len(page.Items) != len(want) {
		t.Fatalf("items=%d want %d", len(page.Items), len(want))
	}
	for i, id := range want {
		if page.Items[i].ID != id {
			t.Fatalf("order[%d]=%s want id %d", i, page.Items[i].Reference, id)
		}
	}
	if page.Items[0].Risk == nil || page.Items[1].ControlImplementation == nil || page.Items[1].ControlImplementation.Asset == nil {
		t.Fatal("list relations not preloaded")
	}

	page, err = f.svc.ListRemediationActions(RemediationActionFilter{Page: 2, Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 4 || page.TotalPages != 2 || len(page.Items) != 1 || page.Items[0].ID != noDueAction.ID {
		t.Fatalf("unexpected second page: %+v", page)
	}

	single := func(filter RemediationActionFilter, want string) {
		t.Helper()
		page, err := f.svc.ListRemediationActions(filter)
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != 1 || page.Items[0].Reference != want {
			t.Fatalf("filter %+v: got %+v, want %s", filter, page.Items, want)
		}
	}

	single(RemediationActionFilter{Search: "PARE-FEU"}, "ACT-NODUE")
	single(RemediationActionFilter{Search: "act-soon"}, "ACT-SOON")
	single(RemediationActionFilter{ControlImplementationID: &f.other.ID}, "ACT-SAME")
	single(RemediationActionFilter{RiskID: &f.risk.ID}, "ACT-SOON")
	single(RemediationActionFilter{AssigneeProfileID: &assignee.ID}, "ACT-NODUE")
	single(RemediationActionFilter{OwningServiceID: &service.ID}, "ACT-SOON")
	single(RemediationActionFilter{Priority: " low "}, "ACT-LATE")
	single(RemediationActionFilter{Status: "blocked"}, "ACT-SAME")
	single(RemediationActionFilter{DueBefore: soon.DueAt}, "ACT-SOON")
}

func TestListRemediationActionsOverdueUsesOneClock(t *testing.T) {
	f := newRemediationActionFixture(t)
	pastDue := timePtr(evidenceTestNow.Add(-time.Second))

	overdue := f.actionRequest("ACT-OVERDUE")
	overdue.DueAt = pastDue
	f.createAction(t, overdue)

	inProgress := f.actionRequest("ACT-OVERDUE-PROGRESS")
	inProgress.DueAt = pastDue
	inProgress.Status = RemediationActionStatusInProgress
	f.createAction(t, inProgress)

	atNow := f.actionRequest("ACT-AT-NOW")
	atNow.DueAt = timePtr(evidenceTestNow)
	f.createAction(t, atNow)

	noDue := f.actionRequest("ACT-NO-DUE")
	f.createAction(t, noDue)

	for i, status := range []string{RemediationActionStatusDone, RemediationActionStatusCancelled} {
		terminal := f.actionRequest("ACT-TERMINAL-" + string(rune('A'+i)))
		terminal.DueAt = pastDue
		terminal.Status = status
		f.createAction(t, terminal)
	}

	calls := 0
	f.svc.now = func() time.Time {
		calls++
		return evidenceTestNow
	}

	yes := true
	page, err := f.svc.ListRemediationActions(RemediationActionFilter{Overdue: &yes})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("ListRemediationActions read the clock %d times, want 1", calls)
	}
	if page.Total != 2 {
		t.Fatalf("overdue=true total=%d want 2: %+v", page.Total, page.Items)
	}
	for _, item := range page.Items {
		if !item.Overdue || (item.Reference != "ACT-OVERDUE" && item.Reference != "ACT-OVERDUE-PROGRESS") {
			t.Fatalf("unexpected overdue item: %+v", item)
		}
	}

	no := false
	page, err = f.svc.ListRemediationActions(RemediationActionFilter{Overdue: &no})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 4 {
		t.Fatalf("overdue=false total=%d want 4: %+v", page.Total, page.Items)
	}
	for _, item := range page.Items {
		if item.Overdue {
			t.Fatalf("overdue=false returned an overdue item: %+v", item)
		}
	}

	// One second later ACT-AT-NOW becomes overdue for the filter and the field.
	f.svc.now = func() time.Time { return evidenceTestNow.Add(time.Second) }
	page, err = f.svc.ListRemediationActions(RemediationActionFilter{Overdue: &yes})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 {
		t.Fatalf("overdue=true later total=%d want 3", page.Total)
	}
	for _, item := range page.Items {
		if !item.Overdue {
			t.Fatalf("filtered item not marked overdue: %+v", item)
		}
	}
}

func TestListRemediationActionsRejectsInvalidFilters(t *testing.T) {
	f := newRemediationActionFixture(t)
	zero := uint(0)

	for name, filter := range map[string]RemediationActionFilter{
		"zero control implementation": {ControlImplementationID: &zero},
		"zero risk":                   {RiskID: &zero},
		"zero assignee":               {AssigneeProfileID: &zero},
		"zero service":                {OwningServiceID: &zero},
		"invalid priority":            {Priority: "URGENT"},
		"invalid status":              {Status: "CLOSED"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := f.svc.ListRemediationActions(filter)
			expectAppStatus(t, err, http.StatusBadRequest)
		})
	}
}
