package patient_queue

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/shared/email"
)

func testRendererLoc(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatalf("load Europe/Paris: %v", err)
	}
	return loc
}

func testRendererPayload() NotificationPayload {
	// 2026-09-21 12:00 UTC → 14:00 Europe/Paris (CEST) → lundi
	return NotificationPayload{
		AppointmentID: 7,
		ScheduledAt:   "2026-09-21T12:00:00Z",
	}
}

func TestAppointmentEmailRendererBooked(t *testing.T) {
	t.Parallel()
	r := NewAppointmentEmailRenderer(testRendererLoc(t))
	got, err := r.Render(NotifKindBooked, testRendererPayload())
	if err != nil {
		t.Fatal(err)
	}
	if got.Subject != "MedCore — Rendez-vous confirmé" {
		t.Fatalf("subject=%q", got.Subject)
	}
	if !strings.Contains(got.TextBody, "confirmé") {
		t.Fatalf("body missing confirmation: %q", got.TextBody)
	}
	if !strings.Contains(got.TextBody, "lundi 21 septembre 2026 à 14:00") {
		t.Fatalf("body missing local datetime: %q", got.TextBody)
	}
}

func TestAppointmentEmailRendererRescheduled(t *testing.T) {
	t.Parallel()
	r := NewAppointmentEmailRenderer(testRendererLoc(t))
	got, err := r.Render(NotifKindRescheduled, testRendererPayload())
	if err != nil {
		t.Fatal(err)
	}
	if got.Subject != "MedCore — Rendez-vous modifié" {
		t.Fatalf("subject=%q", got.Subject)
	}
	if !strings.Contains(got.TextBody, "modifié") || !strings.Contains(got.TextBody, "Nouvelle date") {
		t.Fatalf("body=%q", got.TextBody)
	}
	if !strings.Contains(got.TextBody, "lundi 21 septembre 2026 à 14:00") {
		t.Fatalf("body missing NEW local datetime: %q", got.TextBody)
	}
}

func TestAppointmentEmailRendererCancelled(t *testing.T) {
	t.Parallel()
	r := NewAppointmentEmailRenderer(testRendererLoc(t))
	got, err := r.Render(NotifKindCancelled, testRendererPayload())
	if err != nil {
		t.Fatal(err)
	}
	if got.Subject != "MedCore — Rendez-vous annulé" {
		t.Fatalf("subject=%q", got.Subject)
	}
	if !strings.Contains(got.TextBody, "annulé") {
		t.Fatalf("body=%q", got.TextBody)
	}
	if !strings.Contains(got.TextBody, "lundi 21 septembre 2026 à 14:00") {
		t.Fatalf("body missing local datetime: %q", got.TextBody)
	}
}

func TestAppointmentEmailRendererBusinessTimezoneProjection(t *testing.T) {
	t.Parallel()
	r := NewAppointmentEmailRenderer(testRendererLoc(t))
	// Winter UTC+1: 2026-01-15 12:00 UTC → 13:00 Paris
	payload := NotificationPayload{AppointmentID: 1, ScheduledAt: "2026-01-15T12:00:00.000000000Z"}
	got, err := r.Render(NotifKindBooked, payload)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.TextBody, "jeudi 15 janvier 2026 à 13:00") {
		t.Fatalf("winter Paris projection: %q", got.TextBody)
	}
}

func TestAppointmentEmailRendererDeterministic(t *testing.T) {
	t.Parallel()
	r := NewAppointmentEmailRenderer(testRendererLoc(t))
	p := testRendererPayload()
	a, err := r.Render(NotifKindBooked, p)
	if err != nil {
		t.Fatal(err)
	}
	b, err := r.Render(NotifKindBooked, p)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("non-deterministic: %+v vs %+v", a, b)
	}
}

func TestAppointmentEmailRendererMissingScheduledAt(t *testing.T) {
	t.Parallel()
	r := NewAppointmentEmailRenderer(testRendererLoc(t))
	_, err := r.Render(NotifKindBooked, NotificationPayload{AppointmentID: 1, ScheduledAt: "  "})
	if !errors.Is(err, email.ErrInvalidMessage) {
		t.Fatalf("got %v, want ErrInvalidMessage", err)
	}
	if strings.Contains(err.Error(), "  ") {
		t.Fatalf("error echoed input: %v", err)
	}
}

func TestAppointmentEmailRendererMalformedScheduledAt(t *testing.T) {
	t.Parallel()
	r := NewAppointmentEmailRenderer(testRendererLoc(t))
	_, err := r.Render(NotifKindBooked, NotificationPayload{AppointmentID: 1, ScheduledAt: "not-a-timestamp"})
	if !errors.Is(err, email.ErrInvalidMessage) {
		t.Fatalf("got %v, want ErrInvalidMessage", err)
	}
	if strings.Contains(err.Error(), "not-a-timestamp") {
		t.Fatalf("error echoed input: %v", err)
	}
}

func TestAppointmentEmailRendererUnsupportedKind(t *testing.T) {
	t.Parallel()
	r := NewAppointmentEmailRenderer(testRendererLoc(t))
	_, err := r.Render("UNKNOWN_KIND", testRendererPayload())
	if !errors.Is(err, email.ErrInvalidMessage) {
		t.Fatalf("got %v, want ErrInvalidMessage", err)
	}
	if strings.Contains(err.Error(), "UNKNOWN_KIND") {
		t.Fatalf("error echoed kind: %v", err)
	}
}

func TestAppointmentEmailRendererIgnoresOptionalLabelsAndClinicalBait(t *testing.T) {
	t.Parallel()
	r := NewAppointmentEmailRenderer(testRendererLoc(t))
	payload := NotificationPayload{
		AppointmentID:       9,
		ScheduledAt:         "2026-09-21T12:00:00Z",
		AppointmentTypeName: "diagnostic grippe",
		ServiceName:         "prescription antibiotique",
		ClinicLabel:         "patient@example.test / motif douleur",
	}
	got, err := r.Render(NotifKindBooked, payload)
	if err != nil {
		t.Fatal(err)
	}
	blob := strings.ToLower(got.Subject + "\n" + got.TextBody)
	forbidden := []string{
		"diagnostic", "grippe", "prescription", "antibiotique",
		"patient@example.test", "motif", "douleur", "symptôme", "imagerie", "laboratoire",
	}
	for _, f := range forbidden {
		if strings.Contains(blob, f) {
			t.Fatalf("leaked %q into email copy: %q", f, got.TextBody)
		}
	}
}

func TestAppointmentEmailRendererNoPatientIdentityRequired(t *testing.T) {
	t.Parallel()
	r := NewAppointmentEmailRenderer(testRendererLoc(t))
	// Render succeeds with payload-only inputs (no PatientID / name / email fields).
	got, err := r.Render(NotifKindCancelled, NotificationPayload{
		AppointmentID: 1,
		ScheduledAt:   "2026-09-21T12:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Subject == "" || got.TextBody == "" {
		t.Fatalf("empty render: %+v", got)
	}
}

func TestNewAppointmentEmailRendererPanicsOnNilLocation(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	_ = NewAppointmentEmailRenderer(nil)
}
