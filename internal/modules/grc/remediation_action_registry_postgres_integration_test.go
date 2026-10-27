package grc

import (
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/organization"
)

// TestPostgresGRC08Actions runs the remediation action registry on
// PostgreSQL: persistence and update, UNIQUE/CHECK/FK/RESTRICT, targets
// (implementation, risk, finding) and their consistency rules, assignee and
// service validated on assignment only, the shared due-state definition on
// timestamptz boundaries written at +02:00 (filters, computed field and
// dashboard), search with filters, pagination and the historical edit of an
// action whose finding is closed.
func TestPostgresGRC08Actions(t *testing.T) {
	f := newPGEvidenceActionFixture(t)
	db := f.db
	if err := db.AutoMigrate(dashboardModels()...); err != nil {
		t.Fatal(err)
	}
	svc := f.svc
	day := 24 * time.Hour
	at := func(d time.Duration) *time.Time { v := pgNowParis.Add(d); return &v }
	request := func(reference, status string, due *time.Time) RemediationActionRequest {
		return RemediationActionRequest{
			Reference: reference, Title: "Action " + reference, Description: "Texte libre",
			ControlImplementationID: &f.implementation.ID, Priority: RemediationActionPriorityMedium, Status: status, DueAt: due,
		}
	}

	// Persistence, update and completion date.
	r := request("ACT-PG-08A", RemediationActionStatusOpen, at(5*day))
	r.AssigneeProfileID, r.OwningServiceID = &f.profile.ID, &f.service.ID
	created, err := svc.SaveRemediationAction(0, 41, r)
	if err != nil || created.DueState != RemediationActionDueSoon || created.CreatedBy != 41 {
		t.Fatalf("create: %v %+v", err, created)
	}
	r.Status = RemediationActionStatusDone
	done, err := svc.SaveRemediationAction(created.ID, 42, r)
	if err != nil || done.CompletedAt == nil || done.DueState != RemediationActionDueClosed || done.UpdatedBy != 42 {
		t.Fatalf("done: %v %+v", err, done)
	}
	var stored RemediationAction
	if err := db.First(&stored, created.ID).Error; err != nil || stored.CompletedAt == nil || stored.Status != RemediationActionStatusDone {
		t.Fatalf("stored action: %v %+v", err, stored)
	}

	// Assignee and service validated on assignment only.
	if err := db.Model(&organization.Service{}).Where("id = ?", f.service.ID).Update("active", false).Error; err != nil {
		t.Fatal(err)
	}
	r.Title = "Titre modifié"
	if _, err := svc.SaveRemediationAction(created.ID, 43, r); err != nil {
		t.Fatalf("edit with an inactive service kept: %v", err)
	}
	other := request("ACT-PG-SVC", RemediationActionStatusOpen, nil)
	other.OwningServiceID = &f.service.ID
	_, err = svc.SaveRemediationAction(0, 1, other)
	requireServiceError(t, err, http.StatusConflict)
	if err := db.Model(&organization.Service{}).Where("id = ?", f.service.ID).Update("active", true).Error; err != nil {
		t.Fatal(err)
	}

	// Target rules: control-risk link required, risk alone accepted.
	mismatch := request("ACT-PG-MISMATCH", RemediationActionStatusOpen, nil)
	mismatch.RiskID = &f.otherRisk.ID
	_, err = svc.SaveRemediationAction(0, 1, mismatch)
	requireServiceError(t, err, http.StatusConflict)
	riskOnly := request("ACT-PG-RISK", RemediationActionStatusOpen, nil)
	riskOnly.ControlImplementationID, riskOnly.RiskID = nil, &f.risk.ID
	riskAction, err := svc.SaveRemediationAction(0, 1, riskOnly)
	if err != nil {
		t.Fatal(err)
	}

	// UNIQUE, CHECK, FK.
	_, err = svc.SaveRemediationAction(0, 1, request("ACT-PG-08A", RemediationActionStatusOpen, nil))
	requireServiceError(t, err, http.StatusConflict)
	direct := RemediationAction{Reference: "ACT-PG-DIRECT", Title: "Direct", ControlImplementationID: &f.implementation.ID,
		Priority: RemediationActionPriorityLow, Status: RemediationActionStatusOpen, CreatedBy: 1, UpdatedBy: 1}
	bad := direct
	bad.Status = "ARCHIVED"
	requirePostgresError(t, db.Create(&bad).Error, pgCheckViolation, "grc_remediation_action_status_valid")
	bad = direct
	bad.Priority = "URGENT"
	requirePostgresError(t, db.Create(&bad).Error, pgCheckViolation, "grc_remediation_action_priority_valid")
	bad = direct
	bad.Status = RemediationActionStatusDone
	requirePostgresError(t, db.Create(&bad).Error, pgCheckViolation, "grc_remediation_action_completion")
	bad = direct
	bad.ControlImplementationID = nil
	requirePostgresError(t, db.Create(&bad).Error, pgCheckViolation, RemediationActionTargetConstraint)
	missing := uint(999999)
	bad = direct
	bad.RiskID = &missing
	requirePostgresError(t, db.Create(&bad).Error, pgForeignKeyViolation, "")

	// Due-state boundaries (written at +02:00, compared as instants).
	for _, a := range []struct {
		reference, status string
		due               *time.Time
	}{
		{"ACT-NODUE", RemediationActionStatusOpen, nil},
		{"ACT-LATE", RemediationActionStatusOpen, at(-time.Second)},
		{"ACT-NOW", RemediationActionStatusInProgress, at(0)},
		{"ACT-END", RemediationActionStatusBlocked, at(30 * day)},
		{"ACT-LATER", RemediationActionStatusOpen, at(30*day + time.Second)},
		{"ACT-CANCELLED", RemediationActionStatusCancelled, at(-day)},
	} {
		if _, err := svc.SaveRemediationAction(0, 1, request(a.reference, a.status, a.due)); err != nil {
			t.Fatal(err)
		}
	}
	want := map[string]string{
		RemediationActionDueOverdue:   "ACT-LATE",
		RemediationActionDueSoon:      "ACT-END,ACT-NOW",
		RemediationActionDueUpcoming:  "ACT-LATER",
		RemediationActionDueNoDueDate: "ACT-NODUE,ACT-PG-RISK",
		RemediationActionDueClosed:    "ACT-CANCELLED,ACT-PG-08A",
	}
	for _, state := range remediationActionDueStates {
		page, err := svc.ListRemediationActions(RemediationActionFilter{DueState: state, Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		refs := []string{}
		for _, a := range page.Items {
			if a.DueState != state {
				t.Fatalf("%s listed under %s has due state %s", a.Reference, state, a.DueState)
			}
			refs = append(refs, a.Reference)
		}
		sort.Strings(refs)
		if strings.Join(refs, ",") != want[state] {
			t.Fatalf("%s = %v, want %s", state, refs, want[state])
		}
	}
	dashboard, err := svc.Dashboard(defaultDashboardFilter())
	if err != nil {
		t.Fatal(err)
	}
	if a := dashboard.Actions; a.Total != 8 || a.Open != 6 || a.Overdue != 1 || a.DueSoon != 2 {
		t.Fatalf("dashboard actions %+v diverge from the due states", a)
	}

	// Search with filters, and pagination.
	page, err := svc.ListRemediationActions(RemediationActionFilter{Search: "act-", RiskID: &f.risk.ID, Status: "OPEN", DueState: "NO_DUE_DATE"})
	if err != nil || page.Total != 1 || page.Items[0].ID != riskAction.ID {
		t.Fatalf("search + filters: %v %+v", err, page)
	}
	first, err := svc.ListRemediationActions(RemediationActionFilter{Page: 1, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.ListRemediationActions(RemediationActionFilter{Page: 2, Limit: 5})
	if err != nil || first.Total != 8 || second.TotalPages != 2 || len(second.Items) != 3 || first.Items[0].ID == second.Items[0].ID {
		t.Fatalf("pagination: %v %+v %+v", err, first, second)
	}

	// Finding relation: historical edit after closure, terminal targets refused.
	audit, err := svc.SaveAuditEngagement(0, 1, AuditEngagementRequest{Reference: "AUD-PG-08", Title: "Audit", Type: "INTERNAL", Status: "IN_PROGRESS"})
	if err != nil {
		t.Fatal(err)
	}
	findingRequest := FindingRequest{Reference: "FND-PG-08", Title: "Constat", AuditEngagementID: &audit.ID, Category: "OBSERVATION", Severity: "LOW", Status: "OPEN"}
	finding, err := svc.SaveFinding(0, 1, findingRequest)
	if err != nil {
		t.Fatal(err)
	}
	linked := RemediationActionRequest{Reference: "ACT-PG-FND", Title: "Action du constat", FindingID: &finding.ID, Priority: "LOW", Status: "DONE"}
	linkedAction, err := svc.SaveRemediationAction(0, 1, linked)
	if err != nil {
		t.Fatal(err)
	}
	findingRequest.Status = FindingStatusResolved
	if _, err := svc.SaveFinding(finding.ID, 1, findingRequest); err != nil {
		t.Fatal(err)
	}
	svc.now = func() time.Time { return pgNow.Add(time.Hour) }
	findingRequest.Status = FindingStatusClosed
	if _, err := svc.SaveFinding(finding.ID, 1, findingRequest); err != nil {
		t.Fatalf("closure with every action done: %v", err)
	}
	linked.Title = "Titre corrigé"
	if edited, err := svc.SaveRemediationAction(linkedAction.ID, 2, linked); err != nil || edited.Title != "Titre corrigé" {
		t.Fatalf("historical edit: %v %+v", err, edited)
	}
	late := linked
	late.Reference, late.Status = "ACT-PG-FND-LATE", RemediationActionStatusOpen
	_, err = svc.SaveRemediationAction(0, 1, late)
	requireServiceError(t, err, http.StatusConflict)

	// RESTRICT on every target.
	requirePostgresError(t, db.Delete(&Finding{}, finding.ID).Error, pgForeignKeyViolation, "")
	requirePostgresError(t, db.Delete(&Risk{}, f.risk.ID).Error, pgForeignKeyViolation, "")
	requirePostgresError(t, db.Delete(&ControlImplementation{}, f.implementation.ID).Error, pgForeignKeyViolation, "")
	requireRowExists(t, db, &RemediationAction{}, riskAction.ID)
}
