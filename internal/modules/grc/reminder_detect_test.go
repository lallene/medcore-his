package grc

import (
	"context"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/auth"
	"github.com/lallene/medcore-his/backend/internal/modules/staff"
)

const (
	secretExternal = "SECRET-EVIDENCE-EXTERNAL"
	secretEvidence = "SECRET-EVIDENCE-DESCRIPTION"
	secretAction   = "SECRET-ACTION-DESCRIPTION"
	secretPolicy   = "SECRET-POLICY-DESCRIPTION"
	secretThreat   = "SECRET-RISK-THREAT"
)

func newReminderFixture(t *testing.T) grc01DServiceFixture {
	t.Helper()
	f := newGRC01DServiceFixture(t)
	if err := f.db.AutoMigrate(&GovernancePolicy{}, &GRCReminderIntent{}); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Model(&Risk{}).Where("id = ?", f.risk.ID).Update("threat", secretThreat).Error; err != nil {
		t.Fatal(err)
	}
	return f
}

func reminderRows(t *testing.T, f grc01DServiceFixture) []GRCReminderIntent {
	t.Helper()
	var rows []GRCReminderIntent
	if err := f.db.Order("id ASC").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	return rows
}

func detectReminders(t *testing.T, f grc01DServiceFixture, now time.Time) int {
	t.Helper()
	n, err := f.svc.DetectGRCReminders(context.Background(), now, DefaultReminderWindows())
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func (f grc01DServiceFixture) reminderEvidence(t *testing.T, reference, status string, owner *uint, expires *time.Time) Evidence {
	t.Helper()
	req := f.request(reference)
	req.Status = status
	req.Title = "Titre " + reference
	req.OwnerProfileID = owner
	req.ExpiresAt = expires
	req.ValidFrom = expires
	req.ExternalReference = secretExternal
	req.Description = secretEvidence
	return f.create(t, req)
}

func (f grc01DServiceFixture) reminderAction(t *testing.T, reference, status string, assignee *uint, due *time.Time) RemediationAction {
	t.Helper()
	req := f.actionRequest(reference)
	req.Status = status
	req.Title = "Titre " + reference
	req.AssigneeProfileID = assignee
	req.DueAt = due
	req.Description = secretAction
	if status == RemediationActionStatusDone {
		completed := evidenceTestNow.Add(-time.Hour)
		req.CompletedAt = &completed
	}
	return f.createAction(t, req)
}

func (f grc01DServiceFixture) reminderPolicy(t *testing.T, reference, status string, owner *uint, effectiveTo *time.Time) GovernancePolicy {
	t.Helper()
	from := evidenceTestNow.Add(-24 * time.Hour)
	approved := evidenceTestNow.Add(-2 * time.Hour)
	published := evidenceTestNow.Add(-time.Hour)
	retired := evidenceTestNow.Add(-time.Minute)
	item := GovernancePolicy{
		Reference:      reference,
		Title:          "Titre " + reference,
		Description:    secretPolicy,
		Version:        "1",
		Status:         status,
		OwnerProfileID: owner,
		CreatedBy:      1,
		UpdatedBy:      1,
	}
	switch status {
	case GovernancePolicyStatusPublished:
		item.EffectiveFrom = &from
		item.EffectiveTo = effectiveTo
		item.ApprovedAt = &approved
		item.PublishedAt = &published
	case GovernancePolicyStatusRetired:
		item.EffectiveFrom = &from
		item.EffectiveTo = effectiveTo
		item.ApprovedAt = &approved
		item.PublishedAt = &published
		item.RetiredAt = &retired
	case GovernancePolicyStatusDraft:
		item.EffectiveFrom = nil
		item.EffectiveTo = nil
	default:
		t.Fatalf("unsupported policy status %s", status)
	}
	if err := f.db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	return item
}

func TestDetectEvidenceReminderMatchesValidity(t *testing.T) {
	f := newReminderFixture(t)
	now := evidenceTestNow
	limit := now.AddDate(0, 0, DashboardDefaultWindowDays)
	soon := now.Add(48 * time.Hour)
	expired := now.Add(-time.Second)
	outside := limit.Add(time.Second)
	atNow := now
	atLimit := limit

	included := f.reminderEvidence(t, "EV-SOON", EvidenceStatusValid, &f.profile.ID, &soon)
	boundaryNow := f.reminderEvidence(t, "EV-NOW", EvidenceStatusValid, &f.profile.ID, &atNow)
	boundaryLimit := f.reminderEvidence(t, "EV-LIMIT", EvidenceStatusValid, &f.profile.ID, &atLimit)
	f.reminderEvidence(t, "EV-EXPIRED", EvidenceStatusValid, &f.profile.ID, &expired)
	f.reminderEvidence(t, "EV-OUT", EvidenceStatusValid, &f.profile.ID, &outside)
	f.reminderEvidence(t, "EV-DRAFT", EvidenceStatusDraft, &f.profile.ID, &soon)
	f.reminderEvidence(t, "EV-REVOKED", EvidenceStatusRevoked, &f.profile.ID, &soon)
	f.reminderEvidence(t, "EV-NOOWNER", EvidenceStatusValid, nil, &soon)

	if got := EvidenceValidity(EvidenceStatusValid, &atNow, now, limit); got != EvidenceValidityExpiringSoon {
		t.Fatalf("boundary now validity %s", got)
	}
	if got := EvidenceValidity(EvidenceStatusValid, &atLimit, now, limit); got != EvidenceValidityExpiringSoon {
		t.Fatalf("boundary limit validity %s", got)
	}
	if got := EvidenceValidity(EvidenceStatusValid, &expired, now, limit); got != EvidenceValidityExpired {
		t.Fatalf("expired validity %s", got)
	}

	if n := detectReminders(t, f, now); n != 3 {
		t.Fatalf("created %d, want 3", n)
	}
	rows := reminderRows(t, f)
	if len(rows) != 3 {
		t.Fatalf("rows %d", len(rows))
	}
	want := map[uint]string{
		included.ID:      ReminderOccurrenceEvidence(included.ID, soon),
		boundaryNow.ID:   ReminderOccurrenceEvidence(boundaryNow.ID, atNow),
		boundaryLimit.ID: ReminderOccurrenceEvidence(boundaryLimit.ID, atLimit),
	}
	for _, row := range rows {
		if row.Kind != ReminderKindEvidenceExpiring || row.Status != ReminderStatusPending || row.RecipientProfileID != f.profile.ID {
			t.Fatalf("unexpected intent %+v", row)
		}
		if row.EvidenceID == nil || want[*row.EvidenceID] != row.OccurrenceKey {
			t.Fatalf("occurrence %+v", row)
		}
		if row.RemediationActionID != nil || row.GovernancePolicyID != nil {
			t.Fatalf("intent must target evidence only: %+v", row)
		}
	}
	if n := detectReminders(t, f, now); n != 0 || len(reminderRows(t, f)) != 3 {
		t.Fatal("second scan must not create intents")
	}
}

func TestDetectEvidenceDateChangeIsNewOccurrence(t *testing.T) {
	f := newReminderFixture(t)
	now := evidenceTestNow
	first := now.Add(time.Hour)
	evidence := f.reminderEvidence(t, "EV-MOVE", EvidenceStatusValid, &f.profile.ID, &first)
	if n := detectReminders(t, f, now); n != 1 {
		t.Fatalf("created %d", n)
	}
	next := first.Add(time.Hour)
	if err := f.db.Model(&Evidence{}).Where("id = ?", evidence.ID).Update("expires_at", next).Error; err != nil {
		t.Fatal(err)
	}
	if n := detectReminders(t, f, now); n != 1 {
		t.Fatalf("new occurrence created %d", n)
	}
	rows := reminderRows(t, f)
	if len(rows) != 2 || rows[0].OccurrenceKey == rows[1].OccurrenceKey {
		t.Fatalf("occurrences: %+v", rows)
	}
}

func TestDetectEvidenceDoesNotUseImplementationOwner(t *testing.T) {
	f := newReminderFixture(t)
	now := evidenceTestNow
	soon := now.Add(time.Hour)
	if err := f.db.Model(&ControlImplementation{}).Where("id = ?", f.implementation.ID).Update("owner_profile_id", f.profile.ID).Error; err != nil {
		t.Fatal(err)
	}
	f.reminderEvidence(t, "EV-IMPL", EvidenceStatusValid, nil, &soon)
	if n := detectReminders(t, f, now); n != 0 || len(reminderRows(t, f)) != 0 {
		t.Fatal("implementation owner must not receive an evidence reminder")
	}
}

func TestDetectActionReminderMatchesDueState(t *testing.T) {
	f := newReminderFixture(t)
	now := evidenceTestNow
	limit := now.AddDate(0, 0, DashboardDefaultWindowDays)
	atNow := now
	atLimit := limit
	overdueAt := now.Add(-time.Second)
	outside := limit.Add(time.Second)

	dueSoon := f.reminderAction(t, "ACT-SOON", RemediationActionStatusOpen, &f.profile.ID, &atNow)
	dueLimit := f.reminderAction(t, "ACT-LIMIT", RemediationActionStatusInProgress, &f.profile.ID, &atLimit)
	overdue := f.reminderAction(t, "ACT-LATE", RemediationActionStatusBlocked, &f.profile.ID, &overdueAt)
	f.reminderAction(t, "ACT-OUT", RemediationActionStatusOpen, &f.profile.ID, &outside)
	f.reminderAction(t, "ACT-NONE", RemediationActionStatusOpen, &f.profile.ID, nil)
	f.reminderAction(t, "ACT-NOASSIGN", RemediationActionStatusOpen, nil, &atNow)
	f.reminderAction(t, "ACT-DONE", RemediationActionStatusDone, &f.profile.ID, &overdueAt)
	f.reminderAction(t, "ACT-CANCEL", RemediationActionStatusCancelled, &f.profile.ID, &overdueAt)

	if RemediationActionDueState(RemediationActionStatusOpen, &atNow, now, limit) != RemediationActionDueSoon {
		t.Fatal("dueAt == now must be DUE_SOON")
	}
	if RemediationActionDueState(RemediationActionStatusOpen, &atLimit, now, limit) != RemediationActionDueSoon {
		t.Fatal("dueAt == limit must be DUE_SOON")
	}
	if RemediationActionDueState(RemediationActionStatusBlocked, &overdueAt, now, limit) != RemediationActionDueOverdue {
		t.Fatal("dueAt < now must be OVERDUE")
	}

	if n := detectReminders(t, f, now); n != 3 {
		t.Fatalf("created %d, want 3", n)
	}
	rows := reminderRows(t, f)
	want := map[uint]string{
		dueSoon.ID:  ReminderOccurrenceActionDueSoon(dueSoon.ID, atNow),
		dueLimit.ID: ReminderOccurrenceActionDueSoon(dueLimit.ID, atLimit),
		overdue.ID:  ReminderOccurrenceActionOverdue(overdue.ID, overdueAt),
	}
	if len(rows) != len(want) {
		t.Fatalf("rows %+v", rows)
	}
	for _, row := range rows {
		if row.Kind != ReminderKindActionDue || row.RecipientProfileID != f.profile.ID || row.RemediationActionID == nil {
			t.Fatalf("unexpected %+v", row)
		}
		if want[*row.RemediationActionID] != row.OccurrenceKey {
			t.Fatalf("key got %s", row.OccurrenceKey)
		}
	}
}

func TestDetectActionSoonThenOverdueAreTwoOccurrences(t *testing.T) {
	f := newReminderFixture(t)
	due := evidenceTestNow.Add(time.Hour)
	action := f.reminderAction(t, "ACT-STAGE", RemediationActionStatusOpen, &f.profile.ID, &due)
	if n := detectReminders(t, f, due); n != 1 {
		t.Fatalf("due soon created %d", n)
	}
	if n := detectReminders(t, f, due.Add(time.Second)); n != 1 {
		t.Fatalf("overdue created %d", n)
	}
	if n := detectReminders(t, f, due.Add(time.Second)); n != 0 {
		t.Fatal("third scan created an intent")
	}
	rows := reminderRows(t, f)
	if len(rows) != 2 {
		t.Fatalf("want 2 occurrences, got %+v", rows)
	}
	if rows[0].OccurrenceKey != ReminderOccurrenceActionDueSoon(action.ID, due) ||
		rows[1].OccurrenceKey != ReminderOccurrenceActionOverdue(action.ID, due) {
		t.Fatalf("keys %+v", rows)
	}
}

func TestDetectPolicyReminder(t *testing.T) {
	f := newReminderFixture(t)
	now := evidenceTestNow
	limit := now.AddDate(0, 0, DashboardDefaultWindowDays)
	atNow := now
	atLimit := limit
	past := now.Add(-time.Second)
	outside := limit.Add(time.Second)

	included := f.reminderPolicy(t, "POL-NOW", GovernancePolicyStatusPublished, &f.profile.ID, &atNow)
	limitPolicy := f.reminderPolicy(t, "POL-LIMIT", GovernancePolicyStatusPublished, &f.profile.ID, &atLimit)
	f.reminderPolicy(t, "POL-PAST", GovernancePolicyStatusPublished, &f.profile.ID, &past)
	f.reminderPolicy(t, "POL-OUT", GovernancePolicyStatusPublished, &f.profile.ID, &outside)
	f.reminderPolicy(t, "POL-OPEN", GovernancePolicyStatusPublished, &f.profile.ID, nil)
	f.reminderPolicy(t, "POL-DRAFT", GovernancePolicyStatusDraft, &f.profile.ID, nil)
	f.reminderPolicy(t, "POL-RETIRED", GovernancePolicyStatusRetired, &f.profile.ID, &atNow)
	f.reminderPolicy(t, "POL-NOOWNER", GovernancePolicyStatusPublished, nil, &atNow)

	if n := detectReminders(t, f, now); n != 2 {
		t.Fatalf("created %d, want 2", n)
	}
	rows := reminderRows(t, f)
	want := map[uint]string{
		included.ID:    ReminderOccurrencePolicy(included.ID, atNow),
		limitPolicy.ID: ReminderOccurrencePolicy(limitPolicy.ID, atLimit),
	}
	if len(rows) != 2 {
		t.Fatalf("rows %+v", rows)
	}
	for _, row := range rows {
		if row.Kind != ReminderKindPolicyExpiring || row.GovernancePolicyID == nil || want[*row.GovernancePolicyID] != row.OccurrenceKey {
			t.Fatalf("unexpected %+v", row)
		}
		if row.RecipientProfileID != f.profile.ID {
			t.Fatalf("recipient %+v", row)
		}
	}
}

func TestDetectSkipsInactiveProfileAndUnusableEmail(t *testing.T) {
	f := newReminderFixture(t)
	now := evidenceTestNow
	soon := now.Add(time.Hour)
	inactiveOwner := createExtraProfile(t, f.evidenceServiceFixture, "OWNER-OFF")
	f.reminderEvidence(t, "EV-INACTIVE", EvidenceStatusValid, &inactiveOwner.ID, &soon)
	if err := f.db.Model(&staff.Profile{}).Where("id = ?", inactiveOwner.ID).Update("active", false).Error; err != nil {
		t.Fatal(err)
	}

	bad := createExtraProfile(t, f.evidenceServiceFixture, "BAD-MAIL")
	if err := f.db.Model(&auth.User{}).Where("id = ?", bad.UserID).Update("email", "pas-une-adresse").Error; err != nil {
		t.Fatal(err)
	}
	f.reminderEvidence(t, "EV-BADEMAIL", EvidenceStatusValid, &bad.ID, &soon)

	inactiveUser := createExtraProfile(t, f.evidenceServiceFixture, "USER-OFF")
	if err := f.db.Model(&auth.User{}).Where("id = ?", inactiveUser.UserID).Update("is_active", false).Error; err != nil {
		t.Fatal(err)
	}
	f.reminderEvidence(t, "EV-USER-OFF", EvidenceStatusValid, &inactiveUser.ID, &soon)

	if n := detectReminders(t, f, now); n != 0 || len(reminderRows(t, f)) != 0 {
		t.Fatal("inactive profile, inactive user, and unusable email must not create intents")
	}

	// A historical intent is kept when the recipient later becomes inactive.
	kept := f.reminderEvidence(t, "EV-KEPT", EvidenceStatusValid, &f.profile.ID, &soon)
	if n := detectReminders(t, f, now); n != 1 {
		t.Fatalf("created %d", n)
	}
	if err := f.db.Model(&staff.Profile{}).Where("id = ?", f.profile.ID).Update("active", false).Error; err != nil {
		t.Fatal(err)
	}
	if n := detectReminders(t, f, now); n != 0 {
		t.Fatal("inactive recipient must not receive a new intent")
	}
	rows := reminderRows(t, f)
	if len(rows) != 1 || rows[0].EvidenceID == nil || *rows[0].EvidenceID != kept.ID {
		t.Fatalf("historical intent lost: %+v", rows)
	}
}

func TestDetectDoesNotChangeDomainStatus(t *testing.T) {
	f := newReminderFixture(t)
	now := evidenceTestNow
	soon := now.Add(time.Hour)
	evidence := f.reminderEvidence(t, "EV-SAME", EvidenceStatusValid, &f.profile.ID, &soon)
	action := f.reminderAction(t, "ACT-SAME", RemediationActionStatusOpen, &f.profile.ID, &soon)
	policy := f.reminderPolicy(t, "POL-SAME", GovernancePolicyStatusPublished, &f.profile.ID, &soon)
	beforeEvidence := evidence.Status
	beforeAction := action.Status
	beforePolicy := policy.Status
	detectReminders(t, f, now)

	var gotEvidence Evidence
	var gotAction RemediationAction
	var gotPolicy GovernancePolicy
	if err := f.db.First(&gotEvidence, evidence.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.First(&gotAction, action.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.First(&gotPolicy, policy.ID).Error; err != nil {
		t.Fatal(err)
	}
	if gotEvidence.Status != beforeEvidence || gotAction.Status != beforeAction || gotPolicy.Status != beforePolicy {
		t.Fatalf("domain status changed: %s %s %s", gotEvidence.Status, gotAction.Status, gotPolicy.Status)
	}
}

func TestDetectRejectsWindowOutOfRange(t *testing.T) {
	f := newReminderFixture(t)
	windows := DefaultReminderWindows()
	windows.EvidenceDays = 0
	if _, err := f.svc.DetectGRCReminders(context.Background(), evidenceTestNow, windows); err == nil {
		t.Fatal("window 0 must fail")
	}
	if len(reminderRows(t, f)) != 0 {
		t.Fatal("failed detection must not insert")
	}
}
