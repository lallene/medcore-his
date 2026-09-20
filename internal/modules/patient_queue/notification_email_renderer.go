package patient_queue

import (
	"fmt"
	"strings"
	"time"

	"github.com/lallene/medcore-his/backend/internal/shared/email"
)

// RenderedEmail is privacy-safe appointment notification copy for email.Transport.
// HTML is intentionally out of scope for LOT 26G (plain text only).
type RenderedEmail struct {
	Subject  string
	TextBody string
}

// NotificationEmailRenderer builds deterministic Subject/TextBody from intent kind
// and the allowlisted NotificationPayload. It must not resolve recipients, query
// Patient/Appointment/clinical stores, call time.Now, or import microsoft365.
type NotificationEmailRenderer interface {
	Render(kind string, payload NotificationPayload) (RenderedEmail, error)
}

// AppointmentEmailRenderer is the concrete LOT 26G renderer.
type AppointmentEmailRenderer struct {
	loc *time.Location
}

// NewAppointmentEmailRenderer binds presentation to a non-nil IANA location
// (typically config.BusinessLocation() / MEDCORE_BUSINESS_TIMEZONE).
func NewAppointmentEmailRenderer(loc *time.Location) *AppointmentEmailRenderer {
	if loc == nil {
		panic("appointment email renderer: location required")
	}
	return &AppointmentEmailRenderer{loc: loc}
}

// Render produces French minimum-necessary subject/body for supported kinds.
func (r *AppointmentEmailRenderer) Render(kind string, payload NotificationPayload) (RenderedEmail, error) {
	when, err := formatPayloadScheduledLocal(payload.ScheduledAt, r.loc)
	if err != nil {
		return RenderedEmail{}, err
	}

	switch kind {
	case NotifKindBooked:
		return RenderedEmail{
			Subject:  "MedCore — Rendez-vous confirmé",
			TextBody: "Votre rendez-vous est confirmé.\n\nDate et heure : " + when + "\n",
		}, nil
	case NotifKindRescheduled:
		return RenderedEmail{
			Subject:  "MedCore — Rendez-vous modifié",
			TextBody: "Votre rendez-vous a été modifié.\n\nNouvelle date et heure : " + when + "\n",
		}, nil
	case NotifKindCancelled:
		return RenderedEmail{
			Subject:  "MedCore — Rendez-vous annulé",
			TextBody: "Votre rendez-vous a été annulé.\n\nDate et heure prévues : " + when + "\n",
		}, nil
	case NotifKindReminderT24H:
		// Preserve reminder kind with the same minimum-necessary presentation.
		return RenderedEmail{
			Subject:  "MedCore — Rappel de rendez-vous",
			TextBody: "Rappel : vous avez un rendez-vous.\n\nDate et heure : " + when + "\n",
		}, nil
	default:
		// No kind value in the error string (avoid echoing unexpected inputs).
		return RenderedEmail{}, fmt.Errorf("%w: unsupported notification kind", email.ErrInvalidMessage)
	}
}

var frenchWeekdays = [...]string{
	"dimanche", "lundi", "mardi", "mercredi", "jeudi", "vendredi", "samedi",
}

var frenchMonths = [...]string{
	"", "janvier", "février", "mars", "avril", "mai", "juin",
	"juillet", "août", "septembre", "octobre", "novembre", "décembre",
}

// formatPayloadScheduledLocal parses UTC RFC3339/RFC3339Nano payload.ScheduledAt
// and renders an unambiguous French civil date/time in loc.
func formatPayloadScheduledLocal(scheduledAt string, loc *time.Location) (string, error) {
	raw := strings.TrimSpace(scheduledAt)
	if raw == "" {
		return "", fmt.Errorf("%w: scheduled time required", email.ErrInvalidMessage)
	}
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		t, err = time.Parse(time.RFC3339, raw)
	}
	if err != nil {
		return "", fmt.Errorf("%w: scheduled time invalid", email.ErrInvalidMessage)
	}
	if loc == nil {
		loc = time.UTC
	}
	local := t.In(loc)
	wd := frenchWeekdays[local.Weekday()]
	month := frenchMonths[local.Month()]
	return fmt.Sprintf("%s %d %s %d à %02d:%02d", wd, local.Day(), month, local.Year(), local.Hour(), local.Minute()), nil
}
