package grc

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/lallene/medcore-his/backend/internal/modules/auth"
	"github.com/lallene/medcore-his/backend/internal/modules/staff"
	"github.com/lallene/medcore-his/backend/internal/shared/email"
	"github.com/lallene/medcore-his/backend/internal/testsupport/pgtest"
	"gorm.io/gorm"
)

func TestPostgresGRC13Automation(t *testing.T) {
	dsn := pgtest.DSN(t)
	parsed, err := url.Parse(dsn)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || parsed.Path == "" {
		t.Fatal("TEST_DATABASE_URL must be a postgres URL")
	}
	dbName := strings.TrimPrefix(parsed.Path, "/")
	if dbName == "" || dbName == "medcore" || dbName == "medcore_his" {
		t.Fatal("refusing production database")
	}

	f := newPGEvidenceActionFixture(t)
	if err = f.db.AutoMigrate(&GovernancePolicy{}, &GRCReminderIntent{}); err != nil {
		t.Fatal(err)
	}
	var currentDB, schema string
	if err = f.db.Raw("SELECT current_database()").Scan(&currentDB).Error; err != nil {
		t.Fatal(err)
	}
	if err = f.db.Raw("SELECT current_schema()").Scan(&schema).Error; err != nil {
		t.Fatal(err)
	}
	if currentDB != dbName || currentDB == "medcore" || currentDB == "medcore_his" {
		t.Fatalf("connected database %s is not the test database", currentDB)
	}
	if schema == "public" || !strings.HasPrefix(schema, "grc_") {
		t.Fatalf("expected ephemeral schema, got %s", schema)
	}
	var publicBefore int64
	if err = f.db.Raw(`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_type = 'BASE TABLE'`).Scan(&publicBefore).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		var publicAfter int64
		if err := f.db.Raw(`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_type = 'BASE TABLE'`).Scan(&publicAfter).Error; err != nil {
			t.Errorf("public schema count: %v", err)
			return
		}
		if publicAfter != publicBefore {
			t.Errorf("public tables changed %d -> %d", publicBefore, publicAfter)
		}
	})

	now := pgNow
	soon := now.Add(2 * time.Hour)
	f.svc.now = func() time.Time { return now }
	evidence := pgReminderEvidence(t, f, "EV-PG-13", &f.profile.ID, &soon)
	action := pgReminderAction(t, f, "ACT-PG-13", &f.profile.ID, &soon)
	policy := pgReminderPolicy(t, f, "POL-PG-13", &f.profile.ID, &soon)
	if err = f.db.Model(&Evidence{}).Where("id = ?", evidence.ID).Updates(map[string]any{
		"description": secretEvidence, "external_reference": secretExternal,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err = f.db.Model(&RemediationAction{}).Where("id = ?", action.ID).Update("description", secretAction).Error; err != nil {
		t.Fatal(err)
	}
	if err = f.db.Model(&Risk{}).Where("id = ?", f.risk.ID).Update("threat", secretThreat).Error; err != nil {
		t.Fatal(err)
	}

	postgresReminderConstraints(t, f.db, evidence.ID, action.ID, policy.ID, f.profile.ID, now)
	postgresReminderDetectAndClaim(t, f, now, evidence, action, policy)
}

func pgReminderEvidence(t *testing.T, f pgEvidenceActionFixture, reference string, owner *uint, expires *time.Time) Evidence {
	t.Helper()
	created, err := f.svc.SaveEvidence(0, 1, EvidenceRequest{
		Reference: reference, Title: "Titre " + reference, ControlImplementationID: f.implementation.ID,
		Type: EvidenceTypeReport, Status: EvidenceStatusValid, OwnerProfileID: owner,
		ValidFrom: expires, ExpiresAt: expires,
	})
	if err != nil {
		t.Fatal(err)
	}
	return *created
}

func pgReminderAction(t *testing.T, f pgEvidenceActionFixture, reference string, assignee *uint, due *time.Time) RemediationAction {
	t.Helper()
	created, err := f.svc.SaveRemediationAction(0, 1, RemediationActionRequest{
		Reference: reference, Title: "Titre " + reference, ControlImplementationID: &f.implementation.ID,
		Priority: RemediationActionPriorityHigh, Status: RemediationActionStatusOpen,
		AssigneeProfileID: assignee, DueAt: due,
	})
	if err != nil {
		t.Fatal(err)
	}
	return *created
}

func pgReminderPolicy(t *testing.T, f pgEvidenceActionFixture, reference string, owner *uint, effectiveTo *time.Time) GovernancePolicy {
	t.Helper()
	from := pgNow.Add(-24 * time.Hour)
	approved := pgNow.Add(-2 * time.Hour)
	published := pgNow.Add(-time.Hour)
	item := GovernancePolicy{
		Reference: reference, Title: "Titre " + reference, Description: secretPolicy, Version: "1",
		Status: GovernancePolicyStatusPublished, EffectiveFrom: &from, EffectiveTo: effectiveTo,
		OwnerProfileID: owner, ApprovedAt: &approved, PublishedAt: &published, CreatedBy: 1, UpdatedBy: 1,
	}
	if err := f.db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	return item
}

func requireCheckOneOf(t *testing.T, err error, names ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected check violation")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("expected pg error, got %T %v", err, err)
	}
	if pgErr.Code != pgCheckViolation {
		t.Fatalf("code %s constraint %s", pgErr.Code, pgErr.ConstraintName)
	}
	for _, name := range names {
		if pgErr.ConstraintName == name {
			return
		}
	}
	t.Fatalf("constraint %s, want one of %v", pgErr.ConstraintName, names)
}

func postgresReminderConstraints(t *testing.T, db *gorm.DB, evidenceID, actionID, policyID, profileID uint, now time.Time) {
	t.Helper()
	base := GRCReminderIntent{
		Kind: ReminderKindEvidenceExpiring, Status: ReminderStatusPending,
		EvidenceID: &evidenceID, RecipientProfileID: profileID,
		OccurrenceKey: "constraint-ok", ScheduledFor: now,
	}
	if err := db.Create(&base).Error; err != nil {
		t.Fatal(err)
	}
	dup := base
	dup.ID = 0
	requirePostgresError(t, db.Create(&dup).Error, pgUniqueViolation, "uidx_grc_reminder_recipient_occurrence")

	badKind := base
	badKind.ID, badKind.OccurrenceKey = 0, "bad-kind"
	badKind.Kind = "NOPE"
	requireCheckOneOf(t, db.Create(&badKind).Error, "grc_reminder_kind_valid", "grc_reminder_kind_target")

	badStatus := base
	badStatus.ID, badStatus.OccurrenceKey = 0, "bad-status"
	badStatus.Status = "SENT"
	requirePostgresError(t, db.Create(&badStatus).Error, pgCheckViolation, "grc_reminder_status_valid")

	mismatch := base
	mismatch.ID, mismatch.OccurrenceKey = 0, "bad-target"
	mismatch.EvidenceID = nil
	mismatch.GovernancePolicyID = &policyID
	requirePostgresError(t, db.Create(&mismatch).Error, pgCheckViolation, "grc_reminder_kind_target")

	both := base
	both.ID, both.OccurrenceKey = 0, "two-targets"
	both.RemediationActionID = &actionID
	requireCheckOneOf(t, db.Create(&both).Error, "grc_reminder_one_target", "grc_reminder_kind_target")

	none := base
	none.ID, none.OccurrenceKey, none.EvidenceID = 0, "no-target", nil
	requireCheckOneOf(t, db.Create(&none).Error, "grc_reminder_one_target", "grc_reminder_kind_target")

	missing := uint(999999)
	badFK := base
	badFK.ID, badFK.OccurrenceKey, badFK.EvidenceID = 0, "bad-fk", &missing
	requirePostgresError(t, db.Create(&badFK).Error, pgForeignKeyViolation, "")

	negative := base
	negative.ID, negative.OccurrenceKey, negative.AttemptCount = 0, "bad-attempt", -1
	requirePostgresError(t, db.Create(&negative).Error, pgCheckViolation, "grc_reminder_attempt_nonneg")

	delivered := base
	delivered.ID, delivered.OccurrenceKey = 0, "bad-delivered"
	delivered.Status = ReminderStatusDelivered
	requirePostgresError(t, db.Create(&delivered).Error, pgCheckViolation, "grc_reminder_timestamps")

	processing := base
	processing.ID, processing.OccurrenceKey = 0, "bad-processing"
	processing.Status = ReminderStatusProcessing
	requirePostgresError(t, db.Create(&processing).Error, pgCheckViolation, "grc_reminder_timestamps")

	err := db.Exec(`INSERT INTO grc_reminder_intents
		(kind, status, evidence_id, recipient_profile_id, occurrence_key, scheduled_for, attempt_count, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, NULL, 0, ?, ?)`,
		ReminderKindEvidenceExpiring, ReminderStatusPending, evidenceID, profileID, "null-schedule", now, now).Error
	requirePostgresError(t, err, "23502", "")

	var indexes []string
	if err = db.Raw(`SELECT indexname FROM pg_indexes WHERE schemaname = current_schema() AND tablename = 'grc_reminder_intents'`).Scan(&indexes).Error; err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, name := range indexes {
		found[name] = true
	}
	for _, name := range []string{"uidx_grc_reminder_recipient_occurrence", "idx_grc_reminder_claim", "idx_grc_reminder_lease"} {
		if !found[name] {
			t.Fatalf("missing index %s in %v", name, indexes)
		}
	}
	var checks []string
	if err = db.Raw(`
		SELECT c.conname
		FROM pg_constraint c
		JOIN pg_class t ON t.oid = c.conrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace
		WHERE n.nspname = current_schema() AND t.relname = 'grc_reminder_intents' AND c.contype = 'c'
	`).Scan(&checks).Error; err != nil {
		t.Fatal(err)
	}
	haveCheck := map[string]bool{}
	for _, name := range checks {
		haveCheck[name] = true
	}
	for _, name := range []string{
		"grc_reminder_kind_valid", "grc_reminder_status_valid", "grc_reminder_one_target",
		"grc_reminder_kind_target", "grc_reminder_attempt_nonneg", "grc_reminder_timestamps",
	} {
		if !haveCheck[name] {
			t.Fatalf("missing check %s in %v", name, checks)
		}
	}

	recipient := staff.Profile{}
	user := auth.User{Name: "Recipient only", Email: "grc.pg.reminder.recipient@test.local", PasswordHash: "x", Role: "staff", IsActive: true}
	if err = db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	recipient = staff.Profile{UserID: user.ID, EmployeeCode: "GRC-PG-REMINDER", Active: true}
	if err = db.Create(&recipient).Error; err != nil {
		t.Fatal(err)
	}
	onlyRecipient := base
	onlyRecipient.ID, onlyRecipient.OccurrenceKey, onlyRecipient.RecipientProfileID = 0, "recipient-fk", recipient.ID
	if err = db.Create(&onlyRecipient).Error; err != nil {
		t.Fatal(err)
	}
	actionIntent := GRCReminderIntent{
		Kind: ReminderKindActionDue, Status: ReminderStatusPending, RemediationActionID: &actionID,
		RecipientProfileID: profileID, OccurrenceKey: "action-fk", ScheduledFor: now,
	}
	if err = db.Create(&actionIntent).Error; err != nil {
		t.Fatal(err)
	}
	policyIntent := GRCReminderIntent{
		Kind: ReminderKindPolicyExpiring, Status: ReminderStatusPending, GovernancePolicyID: &policyID,
		RecipientProfileID: profileID, OccurrenceKey: "policy-fk", ScheduledFor: now,
	}
	if err = db.Create(&policyIntent).Error; err != nil {
		t.Fatal(err)
	}
	requirePostgresError(t, db.Delete(&staff.Profile{}, recipient.ID).Error, pgForeignKeyViolation, "fk_grc_reminder_intents_recipient_profile")
	requirePostgresError(t, db.Delete(&Evidence{}, evidenceID).Error, pgForeignKeyViolation, "fk_grc_reminder_intents_evidence")
	requirePostgresError(t, db.Delete(&RemediationAction{}, actionID).Error, pgForeignKeyViolation, "fk_grc_reminder_intents_remediation_action")
	requirePostgresError(t, db.Delete(&GovernancePolicy{}, policyID).Error, pgForeignKeyViolation, "fk_grc_reminder_intents_governance_policy")
}

func postgresReminderDetectAndClaim(t *testing.T, f pgEvidenceActionFixture, now time.Time, evidence Evidence, action RemediationAction, policy GovernancePolicy) {
	t.Helper()
	ctx := context.Background()
	windows := DefaultReminderWindows()
	if err := f.db.Exec("DELETE FROM grc_reminder_intents").Error; err != nil {
		t.Fatal(err)
	}
	var createdA, createdB int
	var errA, errB error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		createdA, errA = f.svc.DetectGRCReminders(ctx, now, windows)
	}()
	go func() {
		defer wg.Done()
		createdB, errB = f.svc.DetectGRCReminders(ctx, now, windows)
	}()
	wg.Wait()
	if errA != nil || errB != nil {
		t.Fatalf("concurrent detect: %v %v", errA, errB)
	}
	var intents []GRCReminderIntent
	if err := f.db.Order("id ASC").Find(&intents).Error; err != nil {
		t.Fatal(err)
	}
	if len(intents) != 3 || createdA+createdB != 3 {
		t.Fatalf("concurrent detect created %d+%d, stored %d", createdA, createdB, len(intents))
	}
	if again, err := f.svc.DetectGRCReminders(ctx, now, windows); err != nil || again != 0 {
		t.Fatalf("second detect %d %v", again, err)
	}

	var before Evidence
	var beforeAction RemediationAction
	var beforePolicy GovernancePolicy
	if err := f.db.First(&before, evidence.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.First(&beforeAction, action.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.First(&beforePolicy, policy.ID).Error; err != nil {
		t.Fatal(err)
	}

	mail := &captureMail{}
	worker, err := NewReminderWorker(f.svc, mail, windows, time.Minute, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	// One live claim must not be visible to a second worker.
	one, err := f.svc.ClaimDueGRCReminders(ctx, now, time.Minute, 1)
	if err != nil || len(one) != 1 {
		t.Fatalf("first claim %+v %v", one, err)
	}
	other, err := f.svc.ClaimDueGRCReminders(ctx, now, time.Minute, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range other {
		if row.ID == one[0].ID {
			t.Fatal("second worker claimed a live lease")
		}
	}
	if len(other) != 2 {
		t.Fatalf("independent claims %+v", other)
	}
	claimedIDs := map[uint]bool{one[0].ID: true}
	for _, row := range other {
		claimedIDs[row.ID] = true
	}
	if len(claimedIDs) != 3 {
		t.Fatalf("claimed ids %v", claimedIDs)
	}

	// Expire one lease and keep another live at exactly now.
	staleID := other[0].ID
	liveID := other[1].ID
	past := now.Add(-time.Second)
	if err = f.db.Model(&GRCReminderIntent{}).Where("id = ?", staleID).Update("lease_expires_at", past).Error; err != nil {
		t.Fatal(err)
	}
	// A lease that expires exactly at now is still live.
	if err = f.db.Model(&GRCReminderIntent{}).Where("id = ?", liveID).Update("lease_expires_at", now).Error; err != nil {
		t.Fatal(err)
	}
	if n, err := f.svc.RecoverStaleGRCReminders(ctx, now, 10); err != nil || n != 1 {
		t.Fatalf("recover %d %v", n, err)
	}
	var stale GRCReminderIntent
	if err = f.db.First(&stale, staleID).Error; err != nil {
		t.Fatal(err)
	}
	if stale.Status != ReminderStatusFailed || stale.AttemptCount != 1 || stale.LeaseExpiresAt != nil || stale.LastError == nil || *stale.LastError != reminderErrStale {
		t.Fatalf("stale %+v", stale)
	}
	var live GRCReminderIntent
	if err = f.db.First(&live, liveID).Error; err != nil {
		t.Fatal(err)
	}
	if live.Status != ReminderStatusProcessing {
		t.Fatalf("live lease recovered: %+v", live)
	}
	reclaimed, err := f.svc.ClaimDueGRCReminders(ctx, now, time.Minute, 10)
	if err != nil || len(reclaimed) != 1 || reclaimed[0].ID != staleID || reclaimed[0].AttemptCount != 2 {
		t.Fatalf("reclaim %+v %v", reclaimed, err)
	}

	mail.fn = func(msg email.Message) error {
		for _, secret := range []string{secretExternal, secretEvidence, secretAction, secretPolicy, secretThreat} {
			if strings.Contains(msg.TextBody, secret) || strings.Contains(msg.Subject, secret) {
				t.Errorf("mail contains %s", secret)
			}
		}
		return nil
	}
	for _, intent := range []GRCReminderIntent{one[0], reclaimed[0], live} {
		var current GRCReminderIntent
		if err = f.db.First(&current, intent.ID).Error; err != nil {
			t.Fatal(err)
		}
		if current.Status != ReminderStatusProcessing {
			continue
		}
		if err = worker.deliverOne(ctx, now, current); err != nil {
			t.Fatal(err)
		}
	}
	if len(mail.messages) != 3 {
		t.Fatalf("delivered %d", len(mail.messages))
	}
	var delivered int64
	if err = f.db.Model(&GRCReminderIntent{}).Where("status = ? AND id IN ?", ReminderStatusDelivered, []uint{one[0].ID, reclaimed[0].ID, live.ID}).Count(&delivered).Error; err != nil {
		t.Fatal(err)
	}
	if delivered != 3 {
		t.Fatalf("delivered rows %d", delivered)
	}
	var after Evidence
	var afterAction RemediationAction
	var afterPolicy GovernancePolicy
	if err = f.db.First(&after, evidence.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err = f.db.First(&afterAction, action.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err = f.db.First(&afterPolicy, policy.ID).Error; err != nil {
		t.Fatal(err)
	}
	if after.Status != before.Status || afterAction.Status != beforeAction.Status || afterPolicy.Status != beforePolicy.Status {
		t.Fatal("delivery changed a domain status")
	}

	// Two workers racing one pending intent: only one claim succeeds.
	raceAt := now.Add(150 * time.Minute)
	raceEvidence := pgReminderEvidence(t, f, "EV-PG-RACE", &f.profile.ID, &raceAt)
	if n, err := f.svc.DetectGRCReminders(ctx, now, windows); err != nil || n != 1 {
		t.Fatalf("race detect %d %v", n, err)
	}
	start := make(chan struct{})
	var raced [2][]GRCReminderIntent
	var raceErr [2]error
	var raceWG sync.WaitGroup
	raceWG.Add(2)
	for i := 0; i < 2; i++ {
		go func(i int) {
			defer raceWG.Done()
			<-start
			raced[i], raceErr[i] = f.svc.ClaimDueGRCReminders(ctx, now, time.Minute, 1)
		}(i)
	}
	close(start)
	raceWG.Wait()
	if raceErr[0] != nil || raceErr[1] != nil {
		t.Fatalf("race claim %v %v", raceErr[0], raceErr[1])
	}
	winners := 0
	var winner GRCReminderIntent
	for _, batch := range raced {
		winners += len(batch)
		if len(batch) == 1 {
			winner = batch[0]
		}
	}
	if winners != 1 || winner.EvidenceID == nil || *winner.EvidenceID != raceEvidence.ID {
		t.Fatalf("race winners %d intent %+v", winners, winner)
	}
	mail.fn = func(email.Message) error { return email.AmbiguousDelivery(errors.New("race")) }
	if err = worker.deliverOne(ctx, now, winner); err == nil {
		t.Fatal("ambiguous race delivery must fail")
	}

	// A new occurrence exercises bounded transient retries without touching the delivered rows.
	later := now.Add(3 * time.Hour)
	retryEvidence := pgReminderEvidence(t, f, "EV-PG-RETRY", &f.profile.ID, &later)
	if n, err := f.svc.DetectGRCReminders(ctx, now, windows); err != nil || n != 1 {
		t.Fatalf("retry detect %d %v", n, err)
	}
	mail.messages = nil
	mail.fn = func(email.Message) error { return email.Transient(errors.New(secretThreat)) }
	for pass := 1; pass <= ReminderMaxAttempts; pass++ {
		if err = worker.RunOnce(ctx, now); err != nil {
			t.Fatal(err)
		}
	}
	if len(mail.messages) != ReminderMaxAttempts {
		t.Fatalf("attempts sent %d", len(mail.messages))
	}
	if err = worker.RunOnce(ctx, now); err != nil {
		t.Fatal(err)
	}
	if len(mail.messages) != ReminderMaxAttempts {
		t.Fatal("attempt past the max was sent")
	}
	var retry GRCReminderIntent
	if err = f.db.Where("evidence_id = ?", retryEvidence.ID).First(&retry).Error; err != nil {
		t.Fatal(err)
	}
	if retry.Status != ReminderStatusFailed || retry.AttemptCount != ReminderMaxAttempts || retry.DeliveredAt != nil {
		t.Fatalf("retry intent %+v", retry)
	}
	_ = worker
}
