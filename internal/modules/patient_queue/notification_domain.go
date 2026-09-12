package patient_queue

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
)

// LOT 23N-A — notification kinds (appointment domain only).
const (
	NotifKindReminderT24H = "REMINDER_T24H"
	NotifKindBooked       = "BOOKED"
	NotifKindRescheduled  = "RESCHEDULED"
	NotifKindCancelled    = "CANCELLED"
)

// Channels — EMAIL/SMS are domain values only; 23N-A does not send externally.
const (
	NotifChannelLog   = "LOG"
	NotifChannelEmail = "EMAIL"
	NotifChannelSMS   = "SMS"
)

// Intent statuses.
const (
	NotifStatusPending    = "PENDING"
	NotifStatusProcessing = "PROCESSING"
	NotifStatusSent       = "SENT"
	NotifStatusFailed     = "FAILED"
	NotifStatusCancelled  = "CANCELLED"
	NotifStatusSkipped    = "SKIPPED"
)

var notifKinds = map[string]struct{}{
	NotifKindReminderT24H: {},
	NotifKindBooked:       {},
	NotifKindRescheduled:  {},
	NotifKindCancelled:    {},
}

var notifChannels = map[string]struct{}{
	NotifChannelLog:   {},
	NotifChannelEmail: {},
	NotifChannelSMS:   {},
}

var notifStatuses = map[string]struct{}{
	NotifStatusPending:    {},
	NotifStatusProcessing: {},
	NotifStatusSent:       {},
	NotifStatusFailed:     {},
	NotifStatusCancelled:  {},
	NotifStatusSkipped:    {},
}

// Allowed status transitions (23N-A). Worker retry may return PROCESSING → PENDING.
var notifAllowedTransitions = map[string]map[string]struct{}{
	NotifStatusPending: {
		NotifStatusProcessing: {},
		NotifStatusCancelled:  {},
		NotifStatusSkipped:    {},
	},
	NotifStatusProcessing: {
		NotifStatusSent:    {},
		NotifStatusFailed:  {},
		NotifStatusPending: {}, // retry reclaim
	},
}

func ValidateNotificationKind(kind string) error {
	if _, ok := notifKinds[kind]; !ok {
		return coreerrors.BadRequest("kind de notification non supporté")
	}
	return nil
}

func ValidateNotificationChannel(channel string) error {
	if _, ok := notifChannels[channel]; !ok {
		return coreerrors.BadRequest("canal de notification non supporté")
	}
	return nil
}

func ValidateNotificationStatus(status string) error {
	if _, ok := notifStatuses[status]; !ok {
		return coreerrors.BadRequest("statut de notification non supporté")
	}
	return nil
}

// CanTransitionNotificationStatus reports whether from → to is allowed.
func CanTransitionNotificationStatus(from, to string) bool {
	next, ok := notifAllowedTransitions[from]
	if !ok {
		return false
	}
	_, ok = next[to]
	return ok
}

func AssertNotificationTransition(from, to string) error {
	if err := ValidateNotificationStatus(from); err != nil {
		return err
	}
	if err := ValidateNotificationStatus(to); err != nil {
		return err
	}
	if !CanTransitionNotificationStatus(from, to) {
		return coreerrors.Conflict(fmt.Sprintf("transition notification interdite: %s → %s", from, to))
	}
	return nil
}

// OccurrenceKeyFromScheduledAt builds a deterministic key from the appointment target instant.
// Uses UTC nanoseconds so location-equivalent instants collide; a reschedule changes the key.
// Not a random UUID.
func OccurrenceKeyFromScheduledAt(scheduledAt time.Time) string {
	return fmt.Sprintf("%d", scheduledAt.UTC().UnixNano())
}

// ReminderSendAfterT24H returns the UTC instant exactly 24 absolute hours before scheduledAt.
// Browser/scheduling wall-clock Location does not participate — persistence is the UTC instant.
func ReminderSendAfterT24H(scheduledAt time.Time) time.Time {
	return scheduledAt.UTC().Add(-24 * time.Hour)
}

// NotificationPayload is the only allowed persisted template data for intents.
//
// Explicitly prohibited (structurally absent):
//   - Appointment.Reason / diagnosis / clinical free text
//   - patient telephone / email
type NotificationPayload struct {
	AppointmentID       uint   `json:"appointmentId"`
	ScheduledAt         string `json:"scheduledAt"` // RFC3339 / RFC3339Nano, canonicalizable to UTC
	AppointmentTypeName string `json:"appointmentTypeName,omitempty"`
	ServiceName         string `json:"serviceName,omitempty"`
	ClinicLabel         string `json:"clinicLabel,omitempty"`
}

// BuildNotificationPayload creates a PHI-safe JSON payload. reason/phone/email are never accepted.
func BuildNotificationPayload(
	appointmentID uint,
	scheduledAt time.Time,
	appointmentTypeName, serviceName, clinicLabel string,
) (NotificationPayload, string, error) {
	if appointmentID == 0 {
		return NotificationPayload{}, "", coreerrors.BadRequest("appointmentId requis")
	}
	if scheduledAt.IsZero() {
		return NotificationPayload{}, "", coreerrors.BadRequest("scheduledAt requis")
	}
	p := NotificationPayload{
		AppointmentID:       appointmentID,
		ScheduledAt:         scheduledAt.UTC().Format(time.RFC3339Nano),
		AppointmentTypeName: strings.TrimSpace(appointmentTypeName),
		ServiceName:         strings.TrimSpace(serviceName),
		ClinicLabel:         strings.TrimSpace(clinicLabel),
	}
	b, err := json.Marshal(p)
	if err != nil {
		return NotificationPayload{}, "", coreerrors.Internal("échec sérialisation payload notification")
	}
	validated, err := ValidatePersistedNotificationPayload(string(b), appointmentID)
	if err != nil {
		return NotificationPayload{}, "", err
	}
	return validated, string(b), nil
}

func assertPayloadSafe(p NotificationPayload) error {
	raw, err := json.Marshal(p)
	if err != nil {
		return coreerrors.Internal("échec contrôle payload notification")
	}
	s := strings.ToLower(string(raw))
	// Structural fields never include reason/phone/email keys; belt-and-suspenders against future edits.
	forbidden := []string{`"reason"`, `"telephone"`, `"email"`, `"phone"`, `"diagnosis"`, `"diagnostic"`}
	for _, f := range forbidden {
		if strings.Contains(s, f) {
			return coreerrors.BadRequest("payload notification contient un champ interdit")
		}
	}
	return nil
}

// ParseNotificationPayload unmarshals stored JSON into the typed safe payload (key safety only).
// Prefer ValidatePersistedNotificationPayload when enqueueing or requiring domain integrity.
func ParseNotificationPayload(raw string) (NotificationPayload, error) {
	var p NotificationPayload
	if strings.TrimSpace(raw) == "" {
		return p, coreerrors.BadRequest("payload notification requis")
	}
	if err := assertRawPayloadJSONSafe(raw); err != nil {
		return NotificationPayload{}, err
	}
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return NotificationPayload{}, coreerrors.BadRequest("payload notification invalide")
	}
	if err := assertPayloadSafe(p); err != nil {
		return NotificationPayload{}, err
	}
	return p, nil
}

// ValidatePersistedNotificationPayload enforces domain integrity for every persisted intent payload:
// typed JSON object, appointmentId > 0 matching the intent, non-empty RFC3339/RFC3339Nano scheduledAt
// canonicalizable to UTC, and no prohibited/unknown keys.
func ValidatePersistedNotificationPayload(raw string, expectedAppointmentID uint) (NotificationPayload, error) {
	if strings.TrimSpace(raw) == "" {
		return NotificationPayload{}, coreerrors.BadRequest("payload notification requis")
	}
	if err := assertRawPayloadJSONSafe(raw); err != nil {
		return NotificationPayload{}, err
	}
	var p NotificationPayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return NotificationPayload{}, coreerrors.BadRequest("payload notification invalide")
	}
	if p.AppointmentID == 0 {
		return NotificationPayload{}, coreerrors.BadRequest("payload.appointmentId requis")
	}
	if expectedAppointmentID == 0 {
		return NotificationPayload{}, coreerrors.BadRequest("appointmentId requis")
	}
	if p.AppointmentID != expectedAppointmentID {
		return NotificationPayload{}, coreerrors.BadRequest("payload.appointmentId ne correspond pas à l'intent")
	}
	if strings.TrimSpace(p.ScheduledAt) == "" {
		return NotificationPayload{}, coreerrors.BadRequest("payload.scheduledAt requis")
	}
	if _, err := parsePayloadScheduledAtUTC(p.ScheduledAt); err != nil {
		return NotificationPayload{}, err
	}
	if err := assertPayloadSafe(p); err != nil {
		return NotificationPayload{}, err
	}
	return p, nil
}

func parsePayloadScheduledAtUTC(raw string) (time.Time, error) {
	s := strings.TrimSpace(raw)
	ts, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		ts, err = time.Parse(time.RFC3339, s)
	}
	if err != nil {
		return time.Time{}, coreerrors.BadRequest("payload.scheduledAt invalide (RFC3339 requis)")
	}
	return ts.UTC(), nil
}

// allowedNotificationPayloadKeys is the closed set of JSON object keys permitted in payload_json.
var allowedNotificationPayloadKeys = map[string]struct{}{
	"appointmentId":       {},
	"scheduledAt":         {},
	"appointmentTypeName": {},
	"serviceName":         {},
	"clinicLabel":         {},
}

func assertRawPayloadJSONSafe(raw string) error {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return coreerrors.BadRequest("payload notification invalide")
	}
	forbidden := map[string]struct{}{
		"reason": {}, "telephone": {}, "email": {}, "phone": {},
		"diagnosis": {}, "diagnostic": {}, "messageBody": {}, "body": {},
	}
	for k := range obj {
		lk := strings.ToLower(k)
		if _, bad := forbidden[lk]; bad {
			return coreerrors.BadRequest("payload notification contient un champ interdit")
		}
		if _, ok := allowedNotificationPayloadKeys[k]; !ok {
			return coreerrors.BadRequest("payload notification contient un champ non autorisé")
		}
	}
	return nil
}
