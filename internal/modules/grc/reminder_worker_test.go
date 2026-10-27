package grc

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/auth"
	"github.com/lallene/medcore-his/backend/internal/modules/staff"
	"github.com/lallene/medcore-his/backend/internal/shared/email"
)

type captureMail struct {
	messages []email.Message
	fn       func(email.Message) error
}

func (c *captureMail) ProviderName() string { return "fake" }

func (c *captureMail) Send(_ context.Context, msg email.Message) (email.Result, error) {
	c.messages = append(c.messages, msg)
	if c.fn != nil {
		return email.Result{}, c.fn(msg)
	}
	return email.Result{ProviderMessageID: "fake"}, nil
}

func claimReminderForTest(t *testing.T, f grc01DServiceFixture, id uint, now time.Time, attempts int) {
	t.Helper()
	lease := now.Add(time.Minute)
	res := f.db.Model(&GRCReminderIntent{}).Where("id = ?", id).Updates(map[string]any{
		"status":           ReminderStatusProcessing,
		"claimed_at":       now,
		"lease_expires_at": lease,
		"attempt_count":    attempts,
		"updated_at":       now,
	})
	if res.Error != nil || res.RowsAffected != 1 {
		t.Fatalf("claim fixture: %v rows %d", res.Error, res.RowsAffected)
	}
}

func TestDeliverReminderSuccessDoesNotMutateDomain(t *testing.T) {
	f := newReminderFixture(t)
	now := evidenceTestNow
	soon := now.Add(2 * time.Hour)
	evidence := f.reminderEvidence(t, "EV-MAIL", EvidenceStatusValid, &f.profile.ID, &soon)
	if n := detectReminders(t, f, now); n != 1 {
		t.Fatalf("created %d", n)
	}
	rows := reminderRows(t, f)
	claimReminderForTest(t, f, rows[0].ID, now, 1)

	mail := &captureMail{}
	mail.fn = func(msg email.Message) error {
		var status string
		if err := f.db.Model(&GRCReminderIntent{}).Select("status").Where("id = ?", rows[0].ID).Scan(&status).Error; err != nil {
			t.Fatal(err)
		}
		if status != ReminderStatusProcessing {
			t.Fatal("send must happen before the delivery status update")
		}
		return nil
	}
	worker, err := NewReminderWorker(f.svc, mail, DefaultReminderWindows(), ReminderLeaseDefault, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if err = worker.deliverOne(context.Background(), now, rows[0]); err != nil {
		t.Fatal(err)
	}
	if len(mail.messages) != 1 {
		t.Fatalf("sent %d", len(mail.messages))
	}
	msg := mail.messages[0]
	if msg.Subject != reminderSubjectEvidence {
		t.Fatalf("subject %q", msg.Subject)
	}
	if msg.To.Address != "grc.owner@test.local" {
		t.Fatalf("recipient %q", msg.To.Address)
	}
	for _, secret := range []string{secretExternal, secretEvidence, secretThreat, "http://", "https://"} {
		if strings.Contains(msg.TextBody, secret) || strings.Contains(msg.Subject, secret) {
			t.Fatalf("message contains %q\n%s", secret, msg.TextBody)
		}
	}
	if !strings.Contains(msg.TextBody, evidence.Reference) || !strings.Contains(msg.TextBody, "Titre EV-MAIL") {
		t.Fatalf("body missing public fields:\n%s", msg.TextBody)
	}
	var stored GRCReminderIntent
	if err = f.db.First(&stored, rows[0].ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != ReminderStatusDelivered || stored.DeliveredAt == nil || stored.ClaimedAt != nil || stored.LeaseExpiresAt != nil {
		t.Fatalf("delivered intent %+v", stored)
	}
	var got Evidence
	if err = f.db.First(&got, evidence.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Status != EvidenceStatusValid {
		t.Fatalf("evidence status %s", got.Status)
	}

	before := len(mail.messages)
	if err = worker.deliverOne(context.Background(), now, stored); err == nil {
		t.Fatal("delivered intent must not be sent again")
	}
	if len(mail.messages) != before {
		t.Fatal("second deliver sent mail")
	}
}

func TestDeliverReminderFailureIsStoredAndBounded(t *testing.T) {
	f := newReminderFixture(t)
	now := evidenceTestNow
	soon := now.Add(time.Hour)
	action := f.reminderAction(t, "ACT-MAIL", RemediationActionStatusOpen, &f.profile.ID, &soon)
	policy := f.reminderPolicy(t, "POL-MAIL", GovernancePolicyStatusPublished, &f.profile.ID, &soon)
	detectReminders(t, f, now)
	rows := reminderRows(t, f)
	if len(rows) != 2 {
		t.Fatalf("rows %+v", rows)
	}

	mail := &captureMail{fn: func(email.Message) error {
		return email.Transient(errors.New(secretAction))
	}}
	worker, err := NewReminderWorker(f.svc, mail, DefaultReminderWindows(), ReminderLeaseDefault, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	var actionIntent GRCReminderIntent
	for _, row := range rows {
		if row.Kind == ReminderKindActionDue {
			actionIntent = row
		}
	}
	claimReminderForTest(t, f, actionIntent.ID, now, 1)
	if err = worker.deliverOne(context.Background(), now, actionIntent); err == nil {
		t.Fatal("transient failure must be returned")
	}
	var failedAction GRCReminderIntent
	if err = f.db.First(&failedAction, actionIntent.ID).Error; err != nil {
		t.Fatal(err)
	}
	if failedAction.Status != ReminderStatusFailed || failedAction.AttemptCount != 1 || failedAction.LastError == nil || *failedAction.LastError != reminderErrTransient {
		t.Fatalf("failed intent %+v", failedAction)
	}
	if strings.Contains(*failedAction.LastError, secretAction) {
		t.Fatal("last error contains the action description")
	}
	if failedAction.DeliveredAt != nil {
		t.Fatal("failed intent must not be delivered")
	}

	mail.fn = func(email.Message) error { return email.AmbiguousDelivery(errors.New("timeout")) }
	var policyIntent GRCReminderIntent
	for _, row := range rows {
		if row.GovernancePolicyID != nil && *row.GovernancePolicyID == policy.ID {
			policyIntent = row
		}
	}
	claimReminderForTest(t, f, policyIntent.ID, now, 1)
	if err = worker.deliverOne(context.Background(), now, policyIntent); err == nil {
		t.Fatal("ambiguous delivery must fail")
	}
	var failedPolicy GRCReminderIntent
	if err = f.db.First(&failedPolicy, policyIntent.ID).Error; err != nil {
		t.Fatal(err)
	}
	if failedPolicy.Status != ReminderStatusFailed || failedPolicy.AttemptCount != ReminderMaxAttempts || failedPolicy.LastError == nil || *failedPolicy.LastError != reminderErrAmbiguous {
		t.Fatalf("ambiguous intent %+v", failedPolicy)
	}
	var gotAction RemediationAction
	var gotPolicy GovernancePolicy
	if err = f.db.First(&gotAction, action.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err = f.db.First(&gotPolicy, policy.ID).Error; err != nil {
		t.Fatal(err)
	}
	if gotAction.Status != RemediationActionStatusOpen || gotPolicy.Status != GovernancePolicyStatusPublished {
		t.Fatalf("domain status changed %s %s", gotAction.Status, gotPolicy.Status)
	}
}

func TestDeliverInactiveRecipientIsTerminal(t *testing.T) {
	f := newReminderFixture(t)
	now := evidenceTestNow
	soon := now.Add(time.Hour)
	f.reminderEvidence(t, "EV-LATER-OFF", EvidenceStatusValid, &f.profile.ID, &soon)
	detectReminders(t, f, now)
	rows := reminderRows(t, f)
	claimReminderForTest(t, f, rows[0].ID, now, 1)
	if err := f.db.Model(&staff.Profile{}).Where("id = ?", f.profile.ID).Update("active", false).Error; err != nil {
		t.Fatal(err)
	}
	mail := &captureMail{}
	worker, err := NewReminderWorker(f.svc, mail, DefaultReminderWindows(), ReminderLeaseDefault, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = worker.deliverOne(context.Background(), now, rows[0]); err == nil {
		t.Fatal("inactive recipient must not be delivered")
	}
	if len(mail.messages) != 0 {
		t.Fatal("inactive recipient must not be emailed")
	}
	var stored GRCReminderIntent
	if err = f.db.First(&stored, rows[0].ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != ReminderStatusFailed || stored.AttemptCount != ReminderMaxAttempts || stored.LastError == nil || *stored.LastError != reminderErrInactive {
		t.Fatalf("intent %+v", stored)
	}
}

func TestReminderDeliveryLogUsesOutcomeClassOnly(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	logReminderDelivery(log, GRCReminderIntent{ID: 7, Kind: ReminderKindEvidenceExpiring}, errors.New(reminderErrTransient))
	logReminderDelivery(log, GRCReminderIntent{ID: 8, Kind: ReminderKindActionDue}, nil)
	logReminderDelivery(log, GRCReminderIntent{ID: 9, Kind: ReminderKindPolicyExpiring}, errors.New("client_secret=sekrit"))
	text := buf.String()
	if !strings.Contains(text, reminderErrTransient) || !strings.Contains(text, "grc reminder delivered") || !strings.Contains(text, "outcome=internal") {
		t.Fatalf("log missing outcome classes:\n%s", text)
	}
	if strings.Contains(text, "sekrit") || strings.Contains(text, "client_secret") {
		t.Fatalf("log leaked provider text:\n%s", text)
	}
}

func TestReminderRunOnceStopsWhenCanceled(t *testing.T) {
	f := newReminderFixture(t)
	worker, err := NewReminderWorker(f.svc, &captureMail{}, DefaultReminderWindows(), ReminderLeaseDefault, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = worker.RunOnce(ctx, evidenceTestNow); !errors.Is(err, context.Canceled) {
		t.Fatalf("RunOnce: %v", err)
	}
	if err = worker.Run(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run: %v", err)
	}
	if len(reminderRows(t, f)) != 0 {
		t.Fatal("canceled worker must not detect")
	}
}

func TestReminderWorkerRejectsMissingTransport(t *testing.T) {
	f := newReminderFixture(t)
	if _, err := NewReminderWorker(f.svc, nil, DefaultReminderWindows(), ReminderLeaseDefault, nil); err == nil {
		t.Fatal("nil transport must fail")
	}
	if _, err := f.svc.ClaimDueGRCReminders(context.Background(), evidenceTestNow, ReminderLeaseDefault, 10); err == nil {
		t.Fatal("sqlite claim must fail closed")
	}
}

func TestRenderReminderOmitsSensitiveText(t *testing.T) {
	when := evidenceTestNow
	msg, err := renderReminderMessage(email.Address{Address: "owner@example.com", DisplayName: "Owner"}, GRCReminderIntent{
		ID:            4,
		Kind:          ReminderKindActionDue,
		OccurrenceKey: ReminderOccurrenceActionOverdue(9, when),
	}, reminderFacts{Reference: "ACT-1", Title: "Corriger", At: when, Stage: RemediationActionDueOverdue})
	if err != nil {
		t.Fatal(err)
	}
	if msg.Subject != reminderSubjectAction || !strings.Contains(msg.TextBody, "OVERDUE") || strings.Contains(msg.TextBody, secretAction) {
		t.Fatalf("message %+v", msg)
	}
	if msg.HTMLBody != "" || strings.Contains(msg.TextBody, "http") {
		t.Fatalf("unexpected body %q", msg.TextBody)
	}
}

func TestDeliverSkipsObsoleteReminders(t *testing.T) {
	f := newReminderFixture(t)
	now := evidenceTestNow
	soon := now.Add(time.Hour)
	evidence := f.reminderEvidence(t, "EV-STALE", EvidenceStatusValid, &f.profile.ID, &soon)
	action := f.reminderAction(t, "ACT-STALE", RemediationActionStatusOpen, &f.profile.ID, &soon)
	policy := f.reminderPolicy(t, "POL-STALE", GovernancePolicyStatusPublished, &f.profile.ID, &soon)
	if n := detectReminders(t, f, now); n != 3 {
		t.Fatalf("created %d", n)
	}
	if err := f.db.Model(&Evidence{}).Where("id = ?", evidence.ID).Update("status", EvidenceStatusRevoked).Error; err != nil {
		t.Fatal(err)
	}
	completed := now
	if err := f.db.Model(&RemediationAction{}).Where("id = ?", action.ID).Updates(map[string]any{
		"status": RemediationActionStatusDone, "completed_at": completed,
	}).Error; err != nil {
		t.Fatal(err)
	}
	retired := now
	if err := f.db.Model(&GovernancePolicy{}).Where("id = ?", policy.ID).Updates(map[string]any{
		"status": GovernancePolicyStatusRetired, "retired_at": retired,
	}).Error; err != nil {
		t.Fatal(err)
	}

	mail := &captureMail{}
	worker, err := NewReminderWorker(f.svc, mail, DefaultReminderWindows(), ReminderLeaseDefault, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range reminderRows(t, f) {
		claimReminderForTest(t, f, row.ID, now, 1)
		if err = worker.deliverOne(context.Background(), now, row); err == nil {
			t.Fatalf("obsolete %s was delivered", row.Kind)
		}
	}
	if len(mail.messages) != 0 {
		t.Fatal("obsolete reminders must not be emailed")
	}
	var stored []GRCReminderIntent
	if err = f.db.Order("id").Find(&stored).Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range stored {
		if row.Status != ReminderStatusFailed || row.AttemptCount != ReminderMaxAttempts || row.DeliveredAt != nil || row.LastError == nil || *row.LastError != reminderErrObsolete {
			t.Fatalf("stale intent %+v", row)
		}
	}
	var gotEvidence Evidence
	var gotAction RemediationAction
	var gotPolicy GovernancePolicy
	if err = f.db.First(&gotEvidence, evidence.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err = f.db.First(&gotAction, action.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err = f.db.First(&gotPolicy, policy.ID).Error; err != nil {
		t.Fatal(err)
	}
	if gotEvidence.Status != EvidenceStatusRevoked || gotAction.Status != RemediationActionStatusDone || gotPolicy.Status != GovernancePolicyStatusRetired {
		t.Fatal("skipping a stale reminder changed a business status")
	}
}

func TestDeliverSkipsDueSoonAfterItBecomesOverdue(t *testing.T) {
	f := newReminderFixture(t)
	due := evidenceTestNow.Add(time.Hour)
	action := f.reminderAction(t, "ACT-LATE-SEND", RemediationActionStatusOpen, &f.profile.ID, &due)
	if n := detectReminders(t, f, due); n != 1 {
		t.Fatalf("created %d", n)
	}
	row := reminderRows(t, f)[0]
	if row.OccurrenceKey != ReminderOccurrenceActionDueSoon(action.ID, due) {
		t.Fatalf("key %s", row.OccurrenceKey)
	}
	later := due.Add(time.Second)
	claimReminderForTest(t, f, row.ID, later, 1)
	mail := &captureMail{}
	worker, err := NewReminderWorker(f.svc, mail, DefaultReminderWindows(), ReminderLeaseDefault, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if err = worker.deliverOne(context.Background(), later, row); err == nil || len(mail.messages) != 0 {
		t.Fatal("a DUE_SOON reminder must not be sent once the action is overdue")
	}
	var got RemediationAction
	if err = f.db.First(&got, action.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Status != RemediationActionStatusOpen {
		t.Fatalf("status %s", got.Status)
	}
}

func TestDeliverResolvesEmailAtSendTime(t *testing.T) {
	f := newReminderFixture(t)
	now := evidenceTestNow
	soon := now.Add(time.Hour)
	f.reminderEvidence(t, "EV-MOVED", EvidenceStatusValid, &f.profile.ID, &soon)
	detectReminders(t, f, now)
	row := reminderRows(t, f)[0]
	claimReminderForTest(t, f, row.ID, now, 1)
	if err := f.db.Model(&auth.User{}).Where("id = ?", f.profile.UserID).Update("email", "owner.moved@test.local").Error; err != nil {
		t.Fatal(err)
	}
	mail := &captureMail{}
	worker, err := NewReminderWorker(f.svc, mail, DefaultReminderWindows(), ReminderLeaseDefault, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if err = worker.deliverOne(context.Background(), now, row); err != nil {
		t.Fatal(err)
	}
	if len(mail.messages) != 1 || mail.messages[0].To.Address != "owner.moved@test.local" {
		t.Fatalf("address resolved at delivery: %+v", mail.messages)
	}
}

func TestReminderOccurrenceKeyIsUTCStable(t *testing.T) {
	utc := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	paris := utc.In(time.FixedZone("UTC+2", 2*3600))
	if ReminderOccurrenceEvidence(7, utc) != ReminderOccurrenceEvidence(7, paris) ||
		ReminderOccurrenceActionDueSoon(7, utc) != ReminderOccurrenceActionDueSoon(7, paris) ||
		ReminderOccurrenceActionOverdue(7, utc) != ReminderOccurrenceActionOverdue(7, paris) ||
		ReminderOccurrencePolicy(7, utc) != ReminderOccurrencePolicy(7, paris) {
		t.Fatal("the same instant must keep one occurrence key across locations")
	}
}

func TestReminderLogsDoNotIncludeMailBody(t *testing.T) {
	f := newReminderFixture(t)
	var buf bytes.Buffer
	worker, err := NewReminderWorker(f.svc, &captureMail{}, DefaultReminderWindows(), ReminderLeaseDefault, slog.New(slog.NewTextHandler(&buf, nil)))
	if err != nil {
		t.Fatal(err)
	}
	soon := evidenceTestNow.Add(time.Hour)
	f.reminderEvidence(t, "EV-LOG", EvidenceStatusValid, &f.profile.ID, &soon)
	// Claim is PostgreSQL-only. Detection and the scan log still run first.
	_ = worker.RunOnce(context.Background(), evidenceTestNow)
	text := buf.String()
	if !strings.Contains(text, "grc reminder scan start") || !strings.Contains(text, "grc reminder intents created") {
		t.Fatalf("logs missing scan markers: %s", text)
	}
	if strings.Contains(text, secretExternal) || strings.Contains(text, secretEvidence) || strings.Contains(text, "grc.owner@test.local") {
		t.Fatalf("logs contain sensitive data: %s", text)
	}
}
