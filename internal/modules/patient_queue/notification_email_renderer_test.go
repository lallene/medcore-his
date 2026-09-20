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

func TestAppointmentEmailRendererReminderT24H(t *testing.T) {
	t.Parallel()
	r := NewAppointmentEmailRenderer(testRendererLoc(t))
	got, err := r.Render(NotifKindReminderT24H, testRendererPayload())
	if err != nil {
		t.Fatal(err)
	}
	if got.Subject != "MedCore — Rappel de rendez-vous" {
		t.Fatalf("subject=%q", got.Subject)
	}
	wantBody := "Rappel : vous avez un rendez-vous.\n\nDate et heure : lundi 21 septembre 2026 à 14:00\n"
	if got.TextBody != wantBody {
		t.Fatalf("body=%q want %q", got.TextBody, wantBody)
	}
}

func TestAppointmentEmailRendererExactCopyMatrix(t *testing.T) {
	t.Parallel()
	r := NewAppointmentEmailRenderer(testRendererLoc(t))
	when := "lundi 21 septembre 2026 à 14:00"
	cases := []struct {
		kind    string
		subject string
		body    string
	}{
		{
			NotifKindBooked,
			"MedCore — Rendez-vous confirmé",
			"Votre rendez-vous est confirmé.\n\nDate et heure : " + when + "\n",
		},
		{
			NotifKindRescheduled,
			"MedCore — Rendez-vous modifié",
			"Votre rendez-vous a été modifié.\n\nNouvelle date et heure : " + when + "\n",
		},
		{
			NotifKindCancelled,
			"MedCore — Rendez-vous annulé",
			"Votre rendez-vous a été annulé.\n\nDate et heure prévues : " + when + "\n",
		},
		{
			NotifKindReminderT24H,
			"MedCore — Rappel de rendez-vous",
			"Rappel : vous avez un rendez-vous.\n\nDate et heure : " + when + "\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			t.Parallel()
			got, err := r.Render(tc.kind, testRendererPayload())
			if err != nil {
				t.Fatal(err)
			}
			if got.Subject != tc.subject {
				t.Fatalf("subject=%q want %q", got.Subject, tc.subject)
			}
			if got.TextBody != tc.body {
				t.Fatalf("body=%q want %q", got.TextBody, tc.body)
			}
		})
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

func TestAppointmentEmailRendererDSTSpringForwardProjection(t *testing.T) {
	t.Parallel()
	// Europe/Paris springs forward 2026-03-29 02:00 local → 03:00 (CET→CEST).
	// Prove renderer uses time.Time.In(loc), not manual hour arithmetic.
	r := NewAppointmentEmailRenderer(testRendererLoc(t))
	cases := []struct {
		name string
		utc  string
		want string
	}{
		{
			"before_spring_forward_cet",
			"2026-03-29T00:30:00Z",
			"dimanche 29 mars 2026 à 01:30", // UTC+1
		},
		{
			"after_spring_forward_cest",
			"2026-03-29T01:30:00Z",
			"dimanche 29 mars 2026 à 03:30", // UTC+2
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := r.Render(NotifKindBooked, NotificationPayload{
				AppointmentID: 1,
				ScheduledAt:   tc.utc,
			})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(got.TextBody, tc.want) {
				t.Fatalf("body=%q want datetime %q", got.TextBody, tc.want)
			}
		})
	}
}

func TestAppointmentEmailRendererDeterministic(t *testing.T) {
	t.Parallel()
	r := NewAppointmentEmailRenderer(testRendererLoc(t))
	p := testRendererPayload()
	kinds := []string{NotifKindBooked, NotifKindRescheduled, NotifKindCancelled, NotifKindReminderT24H}
	for _, kind := range kinds {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			a, err := r.Render(kind, p)
			if err != nil {
				t.Fatal(err)
			}
			b, err := r.Render(kind, p)
			if err != nil {
				t.Fatal(err)
			}
			if a != b {
				t.Fatalf("non-deterministic: %+v vs %+v", a, b)
			}
		})
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
	const (
		typeSentinel    = "TYPE_SENTINEL_ZZ9_diagnostic_grippe"
		serviceSentinel = "SERVICE_SENTINEL_ZZ9_prescription_antibiotique"
		clinicSentinel  = "CLINIC_SENTINEL_ZZ9_patient@example.test_motif_douleur"
	)
	payload := NotificationPayload{
		AppointmentID:       9,
		ScheduledAt:         "2026-09-21T12:00:00Z",
		AppointmentTypeName: typeSentinel,
		ServiceName:         serviceSentinel,
		ClinicLabel:         clinicSentinel,
	}
	for _, kind := range []string{NotifKindBooked, NotifKindRescheduled, NotifKindCancelled, NotifKindReminderT24H} {
		got, err := r.Render(kind, payload)
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		blob := got.Subject + "\n" + got.TextBody
		for _, f := range []string{typeSentinel, serviceSentinel, clinicSentinel} {
			if strings.Contains(blob, f) {
				t.Fatalf("%s leaked sentinel %q into email copy: %q", kind, f, blob)
			}
		}
		lower := strings.ToLower(blob)
		for _, f := range []string{
			"diagnostic", "grippe", "prescription", "antibiotique",
			"patient@example.test", "motif", "douleur", "symptôme", "imagerie", "laboratoire",
		} {
			if strings.Contains(lower, f) {
				t.Fatalf("%s leaked %q into email copy: %q", kind, f, blob)
			}
		}
	}
}

func TestAppointmentEmailRendererRescheduleExposesOnlyNewDatetime(t *testing.T) {
	t.Parallel()
	// NotificationPayload has no previous/old scheduledAt field — structural privacy.
	// RESCHEDULED copy must expose only the (new) payload ScheduledAt.
	r := NewAppointmentEmailRenderer(testRendererLoc(t))
	got, err := r.Render(NotifKindRescheduled, testRendererPayload())
	if err != nil {
		t.Fatal(err)
	}
	want := "Votre rendez-vous a été modifié.\n\nNouvelle date et heure : lundi 21 septembre 2026 à 14:00\n"
	if got.TextBody != want {
		t.Fatalf("body=%q", got.TextBody)
	}
	lower := strings.ToLower(got.Subject + "\n" + got.TextBody)
	for _, bad := range []string{
		"ancienne", "précédent", "precedent", "old time", "previous",
		"reason", "motif", "actor", "doctor", "médecin", "patient",
	} {
		if strings.Contains(lower, bad) {
			t.Fatalf("reschedule leaked %q: %q", bad, got.TextBody)
		}
	}
}

func TestAppointmentEmailRendererErrorPrivacyNoPayloadSentinels(t *testing.T) {
	t.Parallel()
	r := NewAppointmentEmailRenderer(testRendererLoc(t))
	const sentinel = "ERR_PRIVACY_SENTINEL_clinic_label_ZZ9"
	payload := NotificationPayload{
		AppointmentID: 1,
		ScheduledAt:   "not-a-timestamp",
		ClinicLabel:   sentinel,
		ServiceName:   sentinel,
	}
	_, err := r.Render(NotifKindBooked, payload)
	if !errors.Is(err, email.ErrInvalidMessage) {
		t.Fatalf("got %v", err)
	}
	if strings.Contains(err.Error(), sentinel) || strings.Contains(err.Error(), "not-a-timestamp") {
		t.Fatalf("error leaked payload: %v", err)
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
