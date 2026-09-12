package patient_queue

import (
	"time"
)

// AppointmentNotificationIntent is a durable scheduling reminder/notification intent (LOT 23N-A).
// Side-effect queue only — never mutates appointment state. Distinct from ticketing_notifications.
//
// PHI policy: payload_json must not contain Appointment.Reason, diagnosis, telephone, or email.
type AppointmentNotificationIntent struct {
	ID            uint       `gorm:"primaryKey" json:"id"`
	AppointmentID uint       `gorm:"not null;uniqueIndex:ux_appt_notif_intent;index:idx_appt_notif_intent_appt" json:"appointmentId"`
	PatientID     uint       `gorm:"not null;index:idx_appt_notif_intent_patient" json:"patientId"`
	Kind          string     `gorm:"size:32;not null;uniqueIndex:ux_appt_notif_intent" json:"kind"`
	Channel       string     `gorm:"size:16;not null;uniqueIndex:ux_appt_notif_intent" json:"channel"`
	OccurrenceKey string     `gorm:"size:64;not null;uniqueIndex:ux_appt_notif_intent" json:"occurrenceKey"`
	SendAfter     time.Time  `gorm:"not null;index:idx_appt_notif_intent_due,priority:2" json:"sendAfter"`
	Status        string     `gorm:"size:24;not null;index:idx_appt_notif_intent_due,priority:1" json:"status"`
	PayloadJSON   string     `gorm:"type:jsonb;not null;default:'{}'" json:"payloadJson"`
	CreatedAt     time.Time  `gorm:"not null" json:"createdAt"`
	UpdatedAt     time.Time  `gorm:"not null" json:"updatedAt"`
	CancelledAt   *time.Time `json:"cancelledAt,omitempty"`
	SentAt        *time.Time `json:"sentAt,omitempty"`
	// ProcessingStartedAt is the claim lease clock (LOT 23N-B). Nil when not PROCESSING.
	ProcessingStartedAt *time.Time `json:"processingStartedAt,omitempty"`
}

func (AppointmentNotificationIntent) TableName() string {
	return "appointment_notification_intents"
}

// AppointmentNotificationAttempt records a delivery attempt without full message bodies or contact PHI.
//
// FK: intent_id → appointment_notification_intents(id)
// ON DELETE RESTRICT — preserve delivery audit integrity (do not cascade-wipe attempts;
// do not leave orphans). Intent rows with attempts must be retained or attempts removed first.
type AppointmentNotificationAttempt struct {
	ID                uint                          `gorm:"primaryKey" json:"id"`
	IntentID          uint                          `gorm:"not null;uniqueIndex:ux_appt_notif_attempt;index" json:"intentId"`
	Intent            AppointmentNotificationIntent `gorm:"foreignKey:IntentID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	AttemptNo         int                           `gorm:"not null;uniqueIndex:ux_appt_notif_attempt" json:"attemptNo"`
	Provider          string                        `gorm:"size:64;not null" json:"provider"`
	ProviderMessageID *string                       `gorm:"size:128" json:"providerMessageId,omitempty"`
	Error             *string                       `gorm:"size:500" json:"error,omitempty"`
	CreatedAt         time.Time                     `gorm:"not null" json:"createdAt"`
}

func (AppointmentNotificationAttempt) TableName() string {
	return "appointment_notification_attempts"
}
