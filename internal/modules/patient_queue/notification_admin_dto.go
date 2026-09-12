package patient_queue

import "time"

// LOT 23N-C1 — read-only appointment notification admin DTOs.
// Explicit allow-list only; never serialize AppointmentNotificationIntent.PayloadJSON raw.

// NotificationAdminListFilter — query filters for GET /appointment-notification-intents.
type NotificationAdminListFilter struct {
	Status        string
	Kind          string
	Channel       string
	AppointmentID *uint
	SendAfterFrom *time.Time
	SendAfterTo   *time.Time
	CreatedAtFrom *time.Time
	CreatedAtTo   *time.Time
	Page          int
	Limit         int
}

// NotificationPayloadAdminDTO is the only payload shape exposed over HTTP.
type NotificationPayloadAdminDTO struct {
	AppointmentID       uint   `json:"appointmentId"`
	ScheduledAt         string `json:"scheduledAt,omitempty"`
	AppointmentTypeName string `json:"appointmentTypeName,omitempty"`
	ServiceName         string `json:"serviceName,omitempty"`
	ClinicLabel         string `json:"clinicLabel,omitempty"`
}

// NotificationIntentAdminDTO — safe intent metadata for administration.
type NotificationIntentAdminDTO struct {
	ID                  uint                        `json:"id"`
	AppointmentID       uint                        `json:"appointmentId"`
	PatientID           uint                        `json:"patientId"`
	Kind                string                      `json:"kind"`
	Channel             string                      `json:"channel"`
	Status              string                      `json:"status"`
	OccurrenceKey       string                      `json:"occurrenceKey"`
	SendAfter           time.Time                   `json:"sendAfter"`
	ProcessingStartedAt *time.Time                  `json:"processingStartedAt,omitempty"`
	SentAt              *time.Time                  `json:"sentAt,omitempty"`
	CancelledAt         *time.Time                  `json:"cancelledAt,omitempty"`
	CreatedAt           time.Time                   `json:"createdAt"`
	UpdatedAt           time.Time                   `json:"updatedAt"`
	AttemptCount        int                         `json:"attemptCount"`
	Payload             NotificationPayloadAdminDTO `json:"payload"`
}

// NotificationIntentAdminListResponse — paginated list shape matching MedCore conventions.
type NotificationIntentAdminListResponse struct {
	Items []NotificationIntentAdminDTO `json:"items"`
	Total int64                        `json:"total"`
	Page  int                          `json:"page"`
	Limit int                          `json:"limit"`
}

// NotificationAttemptAdminDTO — operational attempt fields only (no invented columns).
type NotificationAttemptAdminDTO struct {
	ID                uint      `json:"id"`
	IntentID          uint      `json:"intentId"`
	AttemptNo         int       `json:"attemptNo"`
	Provider          string    `json:"provider"`
	ProviderMessageID *string   `json:"providerMessageId,omitempty"`
	Error             *string   `json:"error,omitempty"`
	CreatedAt         time.Time `json:"createdAt"`
}
