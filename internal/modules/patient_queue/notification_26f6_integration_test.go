package patient_queue

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/shared/email"
	"gorm.io/gorm"
)

func f6Setup(t *testing.T) (*gorm.DB, *Service, Access, uint, *AppointmentType) {
	t.Helper()
	db, base, admin, prac, _, at := lifeSetup(t)
	if err := EnsureNotificationIndexes(db); err != nil {
		t.Fatal(err)
	}
	svc := base.WithNotificationLifecycleConfig(NotificationLifecycleConfig{EmailEnabled: true})
	return db, svc, admin, prac, at
}

func f6SetPatientEmail(t *testing.T, db *gorm.DB, patientID uint, addr string) {
	t.Helper()
	if err := db.Exec(`UPDATE patients SET email = ? WHERE id = ?`, addr, patientID).Error; err != nil {
		t.Fatal(err)
	}
}

func f6MustIntent(t *testing.T, db *gorm.DB, appointmentID uint, kind, channel string) AppointmentNotificationIntent {
	t.Helper()
	var row AppointmentNotificationIntent
	if err := db.Where("appointment_id = ? AND kind = ? AND channel = ?", appointmentID, kind, channel).
		First(&row).Error; err != nil {
		t.Fatalf("intent %s/%s appt=%d: %v", kind, channel, appointmentID, err)
	}
	return row
}

func f6ReloadIntent(t *testing.T, db *gorm.DB, id uint) AppointmentNotificationIntent {
	t.Helper()
	var row AppointmentNotificationIntent
	if err := db.First(&row, id).Error; err != nil {
		t.Fatal(err)
	}
	return row
}

func f6Worker(t *testing.T, svc *Service, fake *email.Fake) *NotificationWorker {
	t.Helper()
	logAd := NewLogDeliveryAdapter(NotifChannelLog, nil)
	emailAd := NewEmailDeliveryAdapter(
		fake,
		NewGormPatientEmailReader(svc.db),
		NewAppointmentEmailRenderer(time.UTC),
	)
	return mustNotificationWorker(t, svc, NotificationWorkerConfig{
		Adapters: notificationAdapters(logAd, emailAd),
	})
}

func f6AssertPayloadPrivacy(t *testing.T, payloadJSON string, forbidSubstrings ...string) {
	t.Helper()
	var raw map[string]any
	if err := json.Unmarshal([]byte(payloadJSON), &raw); err != nil {
		t.Fatalf("payload json: %v", err)
	}
	allowed := map[string]struct{}{
		"appointmentId": {}, "scheduledAt": {},
		"appointmentTypeName": {}, "serviceName": {}, "clinicLabel": {},
	}
	if _, ok := raw["appointmentId"]; !ok {
		t.Fatal("payload missing appointmentId")
	}
	if _, ok := raw["scheduledAt"]; !ok {
		t.Fatal("payload missing scheduledAt")
	}
	for k := range raw {
		if _, ok := allowed[k]; !ok {
			t.Fatalf("unexpected payload key %q in %s", k, payloadJSON)
		}
	}
	lower := strings.ToLower(payloadJSON)
	for _, s := range forbidSubstrings {
		if s == "" {
			continue
		}
		if strings.Contains(lower, strings.ToLower(s)) {
			t.Fatalf("payload must not contain %q: %s", s, payloadJSON)
		}
	}
}

func f6LatestAttempt(t *testing.T, db *gorm.DB, intentID uint) AppointmentNotificationAttempt {
	t.Helper()
	var att AppointmentNotificationAttempt
	if err := db.Where("intent_id = ?", intentID).Order("attempt_no DESC").First(&att).Error; err != nil {
		t.Fatalf("attempt for intent %d: %v", intentID, err)
	}
	return att
}

func f6BookFarFuture(t *testing.T, svc *Service, admin Access, patient, prac uint, atID uint) *Appointment {
	t.Helper()
	// Relative future Monday 10:00 UTC (lifeSetup schedule: Monday 08:00–16:00).
	// ≥30 days ahead keeps REMINDER_T24H not due during Tick; BOOKED remains due immediately.
	now := time.Now().UTC()
	candidate := now.AddDate(0, 0, 30)
	var start time.Time
	for i := 0; i < 7; i++ {
		d := candidate.AddDate(0, 0, i)
		if d.Weekday() != time.Monday {
			continue
		}
		start = time.Date(d.Year(), d.Month(), d.Day(), 10, 0, 0, 0, time.UTC)
		break
	}
	if start.IsZero() || !start.After(now.Add(24*time.Hour)) {
		t.Fatalf("failed to derive bookable future Monday from %s", now)
	}
	return bookLife(t, svc, admin, patient, prac, start, atID)
}

func TestNotification26F6BookedEmailDeliveredEndToEnd(t *testing.T) {
	db, svc, admin, prac, at := f6Setup(t)
	const patientID uint = 901
	const addr = "patient26f6.sent@example.test"
	f6SetPatientEmail(t, db, patientID, addr)

	appt := f6BookFarFuture(t, svc, admin, patientID, prac, at.ID)
	emailIntent := f6MustIntent(t, db, appt.ID, NotifKindBooked, NotifChannelEmail)
	logIntent := f6MustIntent(t, db, appt.ID, NotifKindBooked, NotifChannelLog)
	if emailIntent.Status != NotifStatusPending || logIntent.Status != NotifStatusPending {
		t.Fatalf("pre-tick EMAIL=%s LOG=%s", emailIntent.Status, logIntent.Status)
	}
	f6AssertPayloadPrivacy(t, emailIntent.PayloadJSON, addr, "Life", "Dupont", "diagnosis", "prescription", "motif")

	now := time.Now().UTC()
	var rem AppointmentNotificationIntent
	if err := db.Where("appointment_id = ? AND kind = ? AND channel = ?", appt.ID, NotifKindReminderT24H, NotifChannelEmail).
		First(&rem).Error; err == nil {
		if !rem.SendAfter.After(now) {
			t.Fatalf("REMINDER_T24H unexpectedly due: send_after=%s now=%s", rem.SendAfter, now)
		}
	}

	fake := email.NewFake()
	w := f6Worker(t, svc, fake)
	if err := w.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	gotEmail := f6ReloadIntent(t, db, emailIntent.ID)
	if gotEmail.Status != NotifStatusSent {
		t.Fatalf("EMAIL status=%s", gotEmail.Status)
	}
	if gotEmail.SentAt == nil {
		t.Fatal("EMAIL sent_at required")
	}
	if gotEmail.ProcessingStartedAt != nil {
		t.Fatal("EMAIL lease must be cleared")
	}

	gotLog := f6ReloadIntent(t, db, logIntent.ID)
	if gotLog.Status != NotifStatusSent {
		t.Fatalf("LOG status=%s", gotLog.Status)
	}
	if gotLog.ProcessingStartedAt != nil {
		t.Fatal("LOG lease must be cleared")
	}

	sent := fake.Sent()
	if len(sent) != 1 {
		t.Fatalf("fake emails=%d want 1", len(sent))
	}
	msg := sent[0]
	if msg.To.Address != addr {
		t.Fatalf("To=%q want %q", msg.To.Address, addr)
	}
	wantKey := fmt.Sprintf("notification-intent:%d", emailIntent.ID)
	if msg.IdempotencyKey != wantKey {
		t.Fatalf("IdempotencyKey=%q want %q", msg.IdempotencyKey, wantKey)
	}
	if msg.HTMLBody != "" {
		t.Fatalf("HTMLBody must stay empty, got %q", msg.HTMLBody)
	}
	if msg.Subject != "MedCore — Rendez-vous confirmé" {
		t.Fatalf("Subject=%q want French booked subject", msg.Subject)
	}
	if !strings.Contains(msg.TextBody, "Votre rendez-vous est confirmé.") {
		t.Fatalf("TextBody missing French booked copy: %q", msg.TextBody)
	}
	if strings.Contains(msg.TextBody, "Scheduled at:") || strings.Contains(msg.Subject, "Appointment booked") {
		t.Fatal("legacy English adapter copy must not be used")
	}
	// Appointment start is Monday 10:00 UTC; renderer uses UTC in this fixture.
	localWhen := appt.ScheduledAt.UTC().Format("15:04")
	if !strings.Contains(msg.TextBody, "à "+localWhen) {
		t.Fatalf("TextBody missing business-local time %q: %q", localWhen, msg.TextBody)
	}
	blob := strings.ToLower(msg.Subject + "\n" + msg.TextBody)
	for _, bad := range []string{strings.ToLower(addr), "diagnosis", "prescription", "motif"} {
		if strings.Contains(blob, bad) {
			t.Fatalf("message leaked %q", bad)
		}
	}

	att := f6LatestAttempt(t, db, emailIntent.ID)
	if att.AttemptNo != 1 || att.Provider != "fake" {
		t.Fatalf("attempt=%+v", att)
	}
	if att.Error != nil {
		t.Fatalf("success attempt error=%v", att.Error)
	}
	if att.ProviderMessageID == nil || *att.ProviderMessageID == "" {
		t.Fatal("provider message id expected")
	}
}

func TestNotification26F6EmailRecipientResolvedAtDeliveryTime(t *testing.T) {
	db, svc, admin, prac, at := f6Setup(t)
	const patientID uint = 902
	const oldAddr = "old@example.test"
	const newAddr = "new@example.test"
	f6SetPatientEmail(t, db, patientID, oldAddr)

	appt := f6BookFarFuture(t, svc, admin, patientID, prac, at.ID)
	emailIntent := f6MustIntent(t, db, appt.ID, NotifKindBooked, NotifChannelEmail)
	if emailIntent.Status != NotifStatusPending {
		t.Fatalf("status=%s", emailIntent.Status)
	}
	f6AssertPayloadPrivacy(t, emailIntent.PayloadJSON, oldAddr, newAddr)

	f6SetPatientEmail(t, db, patientID, newAddr)

	fake := email.NewFake()
	w := f6Worker(t, svc, fake)
	if err := w.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	sent := fake.Sent()
	if len(sent) != 1 {
		t.Fatalf("fake emails=%d", len(sent))
	}
	if sent[0].To.Address != newAddr {
		t.Fatalf("To=%q want %q", sent[0].To.Address, newAddr)
	}
	if sent[0].To.Address == oldAddr {
		t.Fatal("must not use pre-delivery email")
	}

	after := f6ReloadIntent(t, db, emailIntent.ID)
	if after.Status != NotifStatusSent {
		t.Fatalf("status=%s", after.Status)
	}
	f6AssertPayloadPrivacy(t, after.PayloadJSON, oldAddr, newAddr)
}

func TestNotification26F6MissingPatientEmailSkippedEndToEnd(t *testing.T) {
	db, svc, admin, prac, at := f6Setup(t)
	const patientID uint = 903
	f6SetPatientEmail(t, db, patientID, "")

	appt := f6BookFarFuture(t, svc, admin, patientID, prac, at.ID)
	emailIntent := f6MustIntent(t, db, appt.ID, NotifKindBooked, NotifChannelEmail)
	logIntent := f6MustIntent(t, db, appt.ID, NotifKindBooked, NotifChannelLog)
	if emailIntent.Status != NotifStatusPending {
		t.Fatalf("EMAIL must be PENDING despite empty email, got %s", emailIntent.Status)
	}

	fake := email.NewFake()
	w := f6Worker(t, svc, fake)
	if err := w.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	gotEmail := f6ReloadIntent(t, db, emailIntent.ID)
	if gotEmail.Status != NotifStatusSkipped {
		t.Fatalf("EMAIL status=%s want SKIPPED", gotEmail.Status)
	}
	if gotEmail.ProcessingStartedAt != nil {
		t.Fatal("EMAIL lease must be cleared")
	}
	if len(fake.Sent()) != 0 {
		t.Fatalf("transport must not be called, got %d", len(fake.Sent()))
	}

	att := f6LatestAttempt(t, db, emailIntent.ID)
	if att.Provider != "fake" {
		t.Fatalf("provider=%q", att.Provider)
	}
	if att.Error == nil || *att.Error != NotifSkipReasonRecipientUnavailable {
		t.Fatalf("error=%v want %q", att.Error, NotifSkipReasonRecipientUnavailable)
	}

	gotLog := f6ReloadIntent(t, db, logIntent.ID)
	if gotLog.Status != NotifStatusSent {
		t.Fatalf("LOG status=%s want SENT", gotLog.Status)
	}
	if gotLog.ProcessingStartedAt != nil {
		t.Fatal("LOG lease must be cleared")
	}
}

func TestNotification26F6TransientEmailFailureSchedulesRetry(t *testing.T) {
	db, svc, admin, prac, at := f6Setup(t)
	const patientID uint = 904
	const addr = "retry26f6@example.test"
	f6SetPatientEmail(t, db, patientID, addr)

	appt := f6BookFarFuture(t, svc, admin, patientID, prac, at.ID)
	emailIntent := f6MustIntent(t, db, appt.ID, NotifKindBooked, NotifChannelEmail)

	fake := email.NewFake()
	fake.Err = email.ErrTransient
	w := f6Worker(t, svc, fake)

	before := time.Now().UTC()
	if err := w.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	after := time.Now().UTC()

	got := f6ReloadIntent(t, db, emailIntent.ID)
	if got.Status != NotifStatusPending {
		t.Fatalf("status=%s want PENDING", got.Status)
	}
	if got.ProcessingStartedAt != nil {
		t.Fatal("lease must be cleared")
	}
	n, err := svc.CountNotificationAttempts(emailIntent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("attempts=%d want 1", n)
	}

	minSA := before.Add(time.Minute)
	maxSA := after.Add(time.Minute)
	if got.SendAfter.Before(minSA.Add(-2*time.Second)) || got.SendAfter.After(maxSA.Add(2*time.Second)) {
		t.Fatalf("send_after=%s outside [%s, %s]", got.SendAfter, minSA, maxSA)
	}

	att := f6LatestAttempt(t, db, emailIntent.ID)
	if att.AttemptNo != 1 || att.Provider != "fake" {
		t.Fatalf("attempt=%+v", att)
	}
	// Causal proof Fake.Send returned email.ErrTransient (worker persists sendErr.Error() verbatim).
	// Distinguishes from SQL/lookup/payload failures that also yield PENDING+retry.
	if att.Error == nil || *att.Error != email.ErrTransient.Error() {
		t.Fatalf("attempt error=%v want exact %q", att.Error, email.ErrTransient.Error())
	}
	if strings.Contains(strings.ToLower(*att.Error), strings.ToLower(addr)) {
		t.Fatalf("attempt error leaked email")
	}
	if len(fake.Sent()) != 0 {
		t.Fatal("failed Send must not record message")
	}
}
