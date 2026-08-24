package receivables

import (
	"github.com/lallene/medcore-his/backend/internal/modules/billing"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
	"time"
)

type Metadata struct {
	ID        uint             `gorm:"primaryKey" json:"id"`
	InvoiceID uint             `gorm:"not null;uniqueIndex" json:"invoiceId"`
	Invoice   billing.Invoice  `gorm:"foreignKey:InvoiceID;constraint:OnDelete:RESTRICT" json:"-"`
	PatientID uint             `gorm:"not null;index" json:"patientId"`
	Patient   patients.Patient `gorm:"foreignKey:PatientID;constraint:OnDelete:RESTRICT" json:"-"`
	DueDate   *time.Time       `gorm:"type:date;index" json:"dueDate"`
	UpdatedBy uint             `gorm:"not null;index" json:"updatedBy"`
	CreatedAt time.Time        `json:"createdAt"`
	UpdatedAt time.Time        `json:"updatedAt"`
}

func (Metadata) TableName() string { return "patient_receivable_metadata" }

type FollowUp struct {
	ID                  uint             `gorm:"primaryKey" json:"id"`
	InvoiceID           uint             `gorm:"not null;index" json:"invoiceId"`
	Invoice             billing.Invoice  `gorm:"foreignKey:InvoiceID;constraint:OnDelete:RESTRICT" json:"-"`
	PatientID           uint             `gorm:"not null;index" json:"patientId"`
	Patient             patients.Patient `gorm:"foreignKey:PatientID;constraint:OnDelete:RESTRICT" json:"-"`
	ActionType          string           `gorm:"size:30;not null;index" json:"actionType"`
	Note                string           `gorm:"type:text" json:"note"`
	PromisedPaymentDate *time.Time       `gorm:"type:date" json:"promisedPaymentDate"`
	PromisedAmount      *int64           `gorm:"check:receivable_promised_amount_nonnegative,promised_amount IS NULL OR promised_amount >= 0" json:"promisedAmount"`
	CreatedBy           uint             `gorm:"not null;index" json:"createdBy"`
	CreatedAt           time.Time        `gorm:"not null;index" json:"createdAt"`
}

func (FollowUp) TableName() string { return "patient_receivable_follow_ups" }
