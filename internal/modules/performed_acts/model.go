package performed_acts

import "time"

const (
	StatusPerformed = "PERFORMED"
	StatusVoided    = "VOIDED"
)

// Act is a durable record of an act actually performed for a patient (LOT27C).
// Snapshot fields preserve catalogue meaning at performance time.
type Act struct {
	ID                uint `gorm:"primaryKey" json:"id"`
	PatientID         uint `gorm:"not null;index:idx_performed_acts_patient_at,priority:1" json:"patientId"`
	ActCatalogEntryID uint `gorm:"not null;index" json:"actCatalogEntryId"`

	// Catalogue snapshots (server-derived at create; immutable thereafter).
	ActCode           string `gorm:"size:60;not null" json:"actCode"`
	ActLabel          string `gorm:"size:200;not null" json:"actLabel"`
	ActDescription    string `gorm:"type:text" json:"actDescription"`
	ActCategory       string `gorm:"size:30;not null;index" json:"actCategory"`
	BasePrice         int64  `gorm:"not null;check:performed_acts_base_price_nonnegative,base_price >= 0" json:"basePrice"`
	Currency          string `gorm:"size:3;not null" json:"currency"`
	Billable          bool   `gorm:"not null" json:"billable"`
	InsuranceEligible bool   `gorm:"not null" json:"insuranceEligible"`

	Quantity    float64   `gorm:"type:decimal(12,2);not null;check:performed_acts_quantity_positive,quantity > 0" json:"quantity"`
	PerformedAt time.Time `gorm:"not null;index:idx_performed_acts_patient_at,priority:2" json:"performedAt"`
	PerformedBy uint      `gorm:"not null;index" json:"performedBy"`

	Status string `gorm:"size:20;not null;index;check:performed_acts_status_valid,status IN ('PERFORMED','VOIDED')" json:"status"`

	// Optional clinical context (no hard FK — validated in service when set).
	ConsultationID    *uint  `gorm:"index" json:"consultationId,omitempty"`
	AppointmentID     *uint  `gorm:"index" json:"appointmentId,omitempty"`
	HospitalizationID *uint  `gorm:"index" json:"hospitalizationId,omitempty"`
	SourceType        string `gorm:"size:40" json:"sourceType,omitempty"`
	SourceID          *uint  `json:"sourceId,omitempty"`

	VoidedAt   *time.Time `json:"voidedAt,omitempty"`
	VoidedBy   *uint      `gorm:"index" json:"voidedBy,omitempty"`
	VoidReason string     `gorm:"size:240" json:"voidReason,omitempty"`

	CreatedBy uint      `gorm:"not null;index" json:"createdBy"`
	UpdatedBy uint      `gorm:"not null;index" json:"updatedBy"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (Act) TableName() string { return "performed_acts" }
