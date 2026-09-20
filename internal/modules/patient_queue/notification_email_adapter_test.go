package patient_queue

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/patients"
	"github.com/lallene/medcore-his/backend/internal/shared/email"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type mapPatientEmailReader struct {
	mu    sync.Mutex
	byID  map[uint]string
	err   map[uint]error
	calls []uint
}

func newMapPatientEmailReader() *mapPatientEmailReader {
	return &mapPatientEmailReader{
		byID: make(map[uint]string),
		err:  make(map[uint]error),
	}
}

func (m *mapPatientEmailReader) FindPatientEmail(_ context.Context, patientID uint) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, patientID)
	if err, ok := m.err[patientID]; ok {
		return "", err
	}
	v, ok := m.byID[patientID]
	if !ok {
		return "", gorm.ErrRecordNotFound
	}
	return v, nil
}

func (m *mapPatientEmailReader) set(id uint, addr string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.byID[id] = addr
	delete(m.err, id)
}

func (m *mapPatientEmailReader) setErr(id uint, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.err[id] = err
	delete(m.byID, id)
}

func (m *mapPatientEmailReader) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

// stubNotificationEmailRenderer isolates adapter wiring from AppointmentEmailRenderer.
type stubNotificationEmailRenderer struct {
	mu       sync.Mutex
	out      RenderedEmail
	err      error
	calls    int
	lastKind string
	lastPay  NotificationPayload
}

func (s *stubNotificationEmailRenderer) Render(kind string, payload NotificationPayload) (RenderedEmail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.lastKind = kind
	s.lastPay = payload
	return s.out, s.err
}

func (s *stubNotificationEmailRenderer) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func testAppointmentEmailRenderer() *AppointmentEmailRenderer {
	return NewAppointmentEmailRenderer(time.UTC)
}

func newTestEmailAdapter(transport email.Transport, patients PatientEmailReader) *EmailDeliveryAdapter {
	return NewEmailDeliveryAdapter(transport, patients, testAppointmentEmailRenderer())
}

func testEmailIntent(patientID uint, kind string) *AppointmentNotificationIntent {
	return &AppointmentNotificationIntent{
		ID:            42,
		AppointmentID: 7,
		PatientID:     patientID,
		Kind:          kind,
		Channel:       NotifChannelEmail,
	}
}

func testEmailPayload() NotificationPayload {
	return NotificationPayload{
		AppointmentID:       7,
		ScheduledAt:         "2026-09-20T10:00:00Z",
		AppointmentTypeName: "Consultation",
		ServiceName:         "Cardiology",
		ClinicLabel:         "Main Clinic",
	}
}

func TestEmailDeliveryAdapterChannelAndProvider(t *testing.T) {
	t.Parallel()
	fake := email.NewFake()
	ad := newTestEmailAdapter(fake, newMapPatientEmailReader())
	if ad.Channel() != NotifChannelEmail {
		t.Fatalf("channel=%q", ad.Channel())
	}
	if ad.ProviderName() != "fake" {
		t.Fatalf("provider=%q", ad.ProviderName())
	}
}

func TestEmailDeliveryAdapterUsesRendererOutput(t *testing.T) {
	t.Parallel()
	fake := email.NewFake()
	fake.NextProviderMessageID = "msg-1"
	reader := newMapPatientEmailReader()
	reader.set(9, "patient@example.com")
	stub := &stubNotificationEmailRenderer{
		out: RenderedEmail{
			Subject:  "Stub subject from renderer",
			TextBody: "Stub body from renderer\n",
		},
	}
	ad := NewEmailDeliveryAdapter(fake, reader, stub)

	res, err := ad.Send(context.Background(), testEmailIntent(9, NotifKindBooked), testEmailPayload())
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped || res.ProviderMessageID != "msg-1" {
		t.Fatalf("result=%+v", res)
	}
	sent := fake.Sent()
	if len(sent) != 1 {
		t.Fatalf("sent=%d", len(sent))
	}
	msg := sent[0]
	if msg.To.Address != "patient@example.com" {
		t.Fatalf("to=%q", msg.To.Address)
	}
	if msg.To.DisplayName != "" {
		t.Fatalf("displayName=%q", msg.To.DisplayName)
	}
	if msg.Subject != "Stub subject from renderer" {
		t.Fatalf("subject=%q", msg.Subject)
	}
	if msg.TextBody != "Stub body from renderer\n" {
		t.Fatalf("textBody=%q", msg.TextBody)
	}
	if msg.HTMLBody != "" {
		t.Fatalf("HTMLBody must stay empty, got %q", msg.HTMLBody)
	}
	if msg.IdempotencyKey != "notification-intent:42" {
		t.Fatalf("idempotency=%q", msg.IdempotencyKey)
	}
	if stub.callCount() != 1 {
		t.Fatalf("renderer calls=%d", stub.callCount())
	}
	if stub.lastKind != NotifKindBooked {
		t.Fatalf("renderer kind=%q", stub.lastKind)
	}
}

func TestEmailDeliveryAdapterSuccessWithAppointmentRenderer(t *testing.T) {
	t.Parallel()
	fake := email.NewFake()
	fake.NextProviderMessageID = "msg-1"
	reader := newMapPatientEmailReader()
	reader.set(9, "patient@example.com")
	ad := newTestEmailAdapter(fake, reader)

	res, err := ad.Send(context.Background(), testEmailIntent(9, NotifKindBooked), testEmailPayload())
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped || res.ProviderMessageID != "msg-1" {
		t.Fatalf("result=%+v", res)
	}
	sent := fake.Sent()
	if len(sent) != 1 {
		t.Fatalf("sent=%d", len(sent))
	}
	msg := sent[0]
	if msg.To.Address != "patient@example.com" {
		t.Fatalf("to=%q", msg.To.Address)
	}
	if msg.Subject != "MedCore — Rendez-vous confirmé" {
		t.Fatalf("subject=%q", msg.Subject)
	}
	if msg.HTMLBody != "" {
		t.Fatalf("HTMLBody must stay empty, got %q", msg.HTMLBody)
	}
	if msg.IdempotencyKey != "notification-intent:42" {
		t.Fatalf("idempotency=%q", msg.IdempotencyKey)
	}
	wantWhen := "dimanche 20 septembre 2026 à 10:00"
	if !strings.Contains(msg.TextBody, wantWhen) {
		t.Fatalf("body missing business-local datetime %q: %q", wantWhen, msg.TextBody)
	}
	if !strings.Contains(msg.TextBody, "Votre rendez-vous est confirmé.") {
		t.Fatalf("body missing French booked copy: %q", msg.TextBody)
	}
	// Legacy English/generic path and allowlisted labels must not appear.
	for _, bad := range []string{
		"Appointment booked", "Scheduled at:", "Consultation", "Cardiology", "Main Clinic",
	} {
		if strings.Contains(msg.Subject, bad) || strings.Contains(msg.TextBody, bad) {
			t.Fatalf("legacy/content leak %q in message: subject=%q body=%q", bad, msg.Subject, msg.TextBody)
		}
	}
	if strings.Contains(msg.TextBody, "Appointment ID") || strings.Contains(msg.TextBody, "appointmentId") {
		t.Fatalf("body must not expose appointmentId: %q", msg.TextBody)
	}
}

func TestEmailDeliveryAdapterSubjectsByKind(t *testing.T) {
	t.Parallel()
	cases := []struct {
		kind    string
		subject string
	}{
		{NotifKindBooked, "MedCore — Rendez-vous confirmé"},
		{NotifKindRescheduled, "MedCore — Rendez-vous modifié"},
		{NotifKindCancelled, "MedCore — Rendez-vous annulé"},
		{NotifKindReminderT24H, "MedCore — Rappel de rendez-vous"},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			t.Parallel()
			fake := email.NewFake()
			reader := newMapPatientEmailReader()
			reader.set(1, "a@b.co")
			ad := newTestEmailAdapter(fake, reader)
			if _, err := ad.Send(context.Background(), testEmailIntent(1, tc.kind), testEmailPayload()); err != nil {
				t.Fatal(err)
			}
			if got := fake.Sent()[0].Subject; got != tc.subject {
				t.Fatalf("got %q want %q", got, tc.subject)
			}
			if fake.Sent()[0].HTMLBody != "" {
				t.Fatalf("HTMLBody must stay empty")
			}
		})
	}
}

func TestEmailDeliveryAdapterResolvesCurrentEmailAtSendTime(t *testing.T) {
	t.Parallel()
	fake := email.NewFake()
	reader := newMapPatientEmailReader()
	reader.set(3, "old@example.com")
	ad := newTestEmailAdapter(fake, reader)

	if _, err := ad.Send(context.Background(), testEmailIntent(3, NotifKindBooked), testEmailPayload()); err != nil {
		t.Fatal(err)
	}
	reader.set(3, "new@example.com")
	if _, err := ad.Send(context.Background(), testEmailIntent(3, NotifKindBooked), testEmailPayload()); err != nil {
		t.Fatal(err)
	}
	sent := fake.Sent()
	if len(sent) != 2 {
		t.Fatalf("sent=%d", len(sent))
	}
	if sent[0].To.Address != "old@example.com" || sent[1].To.Address != "new@example.com" {
		t.Fatalf("addresses=%q %q", sent[0].To.Address, sent[1].To.Address)
	}
}

func TestEmailDeliveryAdapterRecipientSkipped(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		prep func(*mapPatientEmailReader)
	}{
		{"missing_patient", func(r *mapPatientEmailReader) { /* unset */ }},
		{"empty", func(r *mapPatientEmailReader) { r.set(1, "") }},
		{"whitespace", func(r *mapPatientEmailReader) { r.set(1, "   \t  ") }},
		{"malformed", func(r *mapPatientEmailReader) { r.set(1, "not-an-email") }},
		{"display_name", func(r *mapPatientEmailReader) { r.set(1, "John Doe <a@b.co>") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fake := email.NewFake()
			reader := newMapPatientEmailReader()
			tc.prep(reader)
			stub := &stubNotificationEmailRenderer{out: RenderedEmail{Subject: "x", TextBody: "y"}}
			ad := NewEmailDeliveryAdapter(fake, reader, stub)
			res, err := ad.Send(context.Background(), testEmailIntent(1, NotifKindBooked), testEmailPayload())
			if err != nil {
				t.Fatal(err)
			}
			if !res.Skipped || res.SkipReason != NotifSkipReasonRecipientUnavailable {
				t.Fatalf("result=%+v", res)
			}
			if len(fake.Sent()) != 0 {
				t.Fatal("transport must not be called")
			}
			if stub.callCount() != 0 {
				t.Fatal("renderer must not be called when recipient is unavailable")
			}
			if strings.Contains(res.SkipReason, "@") {
				t.Fatalf("skip reason leaked address: %q", res.SkipReason)
			}
		})
	}
}

func TestEmailDeliveryAdapterPaddedRecipientTrimmedPreservesCase(t *testing.T) {
	t.Parallel()
	fake := email.NewFake()
	reader := newMapPatientEmailReader()
	reader.set(1, "  Patient@Example.com  ")
	ad := newTestEmailAdapter(fake, reader)

	res, err := ad.Send(context.Background(), testEmailIntent(1, NotifKindBooked), testEmailPayload())
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped {
		t.Fatalf("unexpected skip: %+v", res)
	}
	sent := fake.Sent()
	if len(sent) != 1 {
		t.Fatalf("sent=%d, want exactly 1", len(sent))
	}
	if got := sent[0].To.Address; got != "Patient@Example.com" {
		t.Fatalf("To.Address=%q, want Patient@Example.com", got)
	}
}

func TestEmailDeliveryAdapterUnexpectedLookupErrorNotSkipped(t *testing.T) {
	t.Parallel()
	fake := email.NewFake()
	reader := newMapPatientEmailReader()
	lookupErr := errors.New("db connection lost")
	reader.setErr(1, lookupErr)
	stub := &stubNotificationEmailRenderer{out: RenderedEmail{Subject: "x", TextBody: "y"}}
	ad := NewEmailDeliveryAdapter(fake, reader, stub)

	res, err := ad.Send(context.Background(), testEmailIntent(1, NotifKindBooked), testEmailPayload())
	if err == nil {
		t.Fatal("expected lookup error")
	}
	if !errors.Is(err, lookupErr) {
		t.Fatalf("got %v, want wrapped/identity of lookupErr", err)
	}
	if res.Skipped {
		t.Fatalf("infrastructure failure must not be SKIPPED: %+v", res)
	}
	if res.SkipReason != "" {
		t.Fatalf("SkipReason=%q on error path", res.SkipReason)
	}
	if len(fake.Sent()) != 0 {
		t.Fatal("transport must not be called")
	}
	if stub.callCount() != 0 {
		t.Fatal("renderer must not be called on lookup failure")
	}
}

func TestEmailDeliveryAdapterRendererErrInvalidMessagePropagates(t *testing.T) {
	t.Parallel()
	fake := email.NewFake()
	reader := newMapPatientEmailReader()
	reader.set(1, "ok@example.com")
	stub := &stubNotificationEmailRenderer{
		err: fmt.Errorf("%w: bad scheduledAt", email.ErrInvalidMessage),
	}
	ad := NewEmailDeliveryAdapter(fake, reader, stub)

	res, err := ad.Send(context.Background(), testEmailIntent(1, NotifKindBooked), testEmailPayload())
	if !errors.Is(err, email.ErrInvalidMessage) {
		t.Fatalf("got %v, want errors.Is ErrInvalidMessage", err)
	}
	if res.Skipped {
		t.Fatalf("renderer terminal must not be SKIPPED: %+v", res)
	}
	if len(fake.Sent()) != 0 {
		t.Fatal("transport must not be called when renderer fails")
	}
	if stub.callCount() != 1 {
		t.Fatalf("renderer calls=%d", stub.callCount())
	}
	errStr := err.Error()
	if strings.Contains(errStr, "ok@example.com") || strings.Contains(errStr, "Stub") {
		t.Fatalf("error leaked recipient/body: %q", errStr)
	}
}

func TestEmailDeliveryAdapterContextDeadlineExceeded(t *testing.T) {
	t.Parallel()
	fake := email.NewFake()
	reader := newMapPatientEmailReader()
	reader.set(1, "ok@example.com")
	ad := newTestEmailAdapter(fake, reader)

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	res, err := ad.Send(ctx, testEmailIntent(1, NotifKindBooked), testEmailPayload())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want context.DeadlineExceeded", err)
	}
	if res.Skipped {
		t.Fatalf("deadline must not be SKIPPED: %+v", res)
	}
	if len(fake.Sent()) != 0 {
		t.Fatal("transport must not record a successful send")
	}
}

func TestEmailDeliveryAdapterNoMedicalProfileFallback(t *testing.T) {
	t.Parallel()
	fake := email.NewFake()
	reader := newMapPatientEmailReader()
	reader.setErr(5, gorm.ErrRecordNotFound)
	stub := &stubNotificationEmailRenderer{out: RenderedEmail{Subject: "x", TextBody: "y"}}
	ad := NewEmailDeliveryAdapter(fake, reader, stub)
	res, err := ad.Send(context.Background(), testEmailIntent(5, NotifKindBooked), testEmailPayload())
	if err != nil {
		t.Fatal(err)
	}
	if !res.Skipped || len(fake.Sent()) != 0 {
		t.Fatalf("expected skip without send, got %+v sent=%d", res, len(fake.Sent()))
	}
	if reader.callCount() != 1 {
		t.Fatalf("calls=%d", reader.callCount())
	}
	if stub.callCount() != 0 {
		t.Fatal("renderer must not run when patient missing")
	}
}

func TestEmailDeliveryAdapterTransportErrorsPreserved(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		err    error
		target error
	}{
		{"transient", email.Transient(errors.New("net")), email.ErrTransient},
		{"permanent", email.Permanent(errors.New("policy")), email.ErrPermanent},
		{"invalid", email.ErrInvalidMessage, email.ErrInvalidMessage},
		{"not_configured", email.NotConfigured(nil), email.ErrNotConfigured},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fake := email.NewFake()
			fake.Err = tc.err
			reader := newMapPatientEmailReader()
			reader.set(1, "ok@example.com")
			ad := newTestEmailAdapter(fake, reader)
			_, err := ad.Send(context.Background(), testEmailIntent(1, NotifKindBooked), testEmailPayload())
			if !errors.Is(err, tc.target) {
				t.Fatalf("got %v, want %v", err, tc.target)
			}
			if len(fake.Sent()) != 0 {
				t.Fatal("failed send must not record message")
			}
		})
	}
}

func TestEmailDeliveryAdapterContextCanceled(t *testing.T) {
	t.Parallel()
	fake := email.NewFake()
	reader := newMapPatientEmailReader()
	reader.set(1, "ok@example.com")
	ad := newTestEmailAdapter(fake, reader)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := ad.Send(ctx, testEmailIntent(1, NotifKindBooked), testEmailPayload())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}

func TestEmailDeliveryAdapterPrivacyNoClinicalOrContactLeak(t *testing.T) {
	t.Parallel()
	fake := email.NewFake()
	reader := newMapPatientEmailReader()
	addr := "secret.patient@example.com"
	reader.set(1, addr)
	ad := newTestEmailAdapter(fake, reader)

	payload := NotificationPayload{
		AppointmentID:       99,
		ScheduledAt:         "2026-01-01T08:00:00Z",
		AppointmentTypeName: "Follow-up",
		ServiceName:         "General",
		ClinicLabel:         "Wing A",
	}
	if _, err := ad.Send(context.Background(), testEmailIntent(1, NotifKindReminderT24H), payload); err != nil {
		t.Fatal(err)
	}
	msg := fake.Sent()[0]
	if msg.Subject != "MedCore — Rappel de rendez-vous" {
		t.Fatalf("reminder subject regression: %q", msg.Subject)
	}
	blob := strings.ToLower(msg.Subject + "\n" + msg.TextBody + "\n" + msg.HTMLBody)
	for _, bad := range []string{
		"diagnosis", "diagnostic", "reason", "telephone", "phone",
		"prescription", "lab result", "imaging", addr, "secret.patient",
		"follow-up", "general", "wing a", "doctor", "specialty",
	} {
		if strings.Contains(blob, strings.ToLower(bad)) {
			t.Fatalf("message leaked %q: %q", bad, blob)
		}
	}

	raw := `{"appointmentId":1,"scheduledAt":"2026-01-01T08:00:00Z","reason":"chest pain","email":"x@y.z","diagnosis":"flu"}`
	if _, err := ParseNotificationPayload(raw); err == nil {
		t.Fatal("expected forbidden payload rejection")
	}
}

func TestGormPatientEmailReaderCanonicalOnly(t *testing.T) {
	t.Parallel()
	db, err := gorm.Open(sqlite.Open("file:email_reader?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&patients.Patient{}); err != nil {
		t.Fatal(err)
	}
	p := patients.Patient{CodePatient: "E1", NumeroDossier: "D1", Nom: "TEST", Email: "canonical@example.com"}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	reader := NewGormPatientEmailReader(db)
	got, err := reader.FindPatientEmail(context.Background(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got != "canonical@example.com" {
		t.Fatalf("got %q", got)
	}
	_, err = reader.FindPatientEmail(context.Background(), p.ID+999)
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("got %v", err)
	}
}
