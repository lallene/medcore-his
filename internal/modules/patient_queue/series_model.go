package patient_queue

import "time"

// Appointment series frequency (LOT 23O-A P0).
const (
	SeriesFreqWeekly = "WEEKLY"
)

// Appointment series status (LOT 23O-A P0).
const (
	SeriesStatusActive = "ACTIVE"
)

// AppointmentSeries is the parent recurrence row; occurrences are materialized Appointment rows.
// PractitionerID is required and fixed for the entire series (P0).
// Does not store diagnosis, clinical notes, phone/email copies, or Appointment.Reason.
type AppointmentSeries struct {
	ID                uint       `gorm:"primaryKey" json:"id"`
	PatientID         uint       `gorm:"not null;index" json:"patientId"`
	ServiceID         uint       `gorm:"not null;index" json:"serviceId"`
	PractitionerID    uint       `gorm:"not null;index" json:"practitionerId"` // users.id — required & fixed
	AppointmentTypeID *uint      `gorm:"index" json:"appointmentTypeId,omitempty"`
	Freq              string     `gorm:"size:16;not null" json:"freq"` // WEEKLY
	IntervalWeeks     int        `gorm:"not null" json:"intervalWeeks"`
	ByWeekdays        string     `gorm:"type:jsonb;not null" json:"byWeekdays"` // sorted unique Go weekdays [0–6]
	Count             *int       `json:"count,omitempty"`
	Until             *time.Time `json:"until,omitempty"`                  // inclusive UTC instant bound
	Timezone          string     `gorm:"size:64;not null" json:"timezone"` // IANA
	AnchorStartAt     time.Time  `gorm:"not null;index" json:"anchorStartAt"`
	DurationMinutes   int        `gorm:"not null" json:"durationMinutes"` // snapshot at create
	Status            string     `gorm:"size:24;not null;index" json:"status"`
	CreatedBy         uint       `gorm:"not null;index" json:"createdBy"`
	IdempotencyKey    *string    `gorm:"size:150" json:"idempotencyKey,omitempty"`
	Version           int        `gorm:"not null;default:1" json:"version"` // reserved for 23O-B
	CreatedAt         time.Time  `gorm:"not null" json:"createdAt"`
	UpdatedAt         time.Time  `gorm:"not null" json:"updatedAt"`
}

func (AppointmentSeries) TableName() string { return "patient_queue_appointment_series" }
