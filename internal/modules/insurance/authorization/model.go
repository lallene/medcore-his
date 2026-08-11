package authorization

import "time"

const (
	StatusDraft             = "DRAFT"
	StatusSubmitted         = "SUBMITTED"
	StatusPending           = "PENDING"
	StatusApproved          = "APPROVED"
	StatusPartiallyApproved = "PARTIALLY_APPROVED"
	StatusRejected          = "REJECTED"
	StatusCancelled         = "CANCELLED"
)

var finalStatuses = map[string]bool{
	StatusApproved: true, StatusPartiallyApproved: true, StatusRejected: true,
}

type InsuranceAuthorization struct {
	ID                   uint       `gorm:"primaryKey" json:"id"`
	AuthorizationNumber  string     `gorm:"size:30;not null;uniqueIndex" json:"authorizationNumber"`
	PatientID            uint       `gorm:"not null;index" json:"patientId"`
	MedicalRecordID      uint       `gorm:"not null;index" json:"medicalRecordId"`
	PatientCoverageID    uint       `gorm:"not null;index" json:"patientCoverageId"`
	InsuranceCompanyID   uint       `gorm:"not null;index" json:"insuranceCompanyId"`
	GuarantorID          uint       `gorm:"not null;index" json:"guarantorId"`
	ReferenceType        string     `gorm:"size:30;not null;index:idx_authorization_reference" json:"referenceType"`
	ReferenceID          uint       `gorm:"not null;index:idx_authorization_reference" json:"referenceId"`
	Service              string     `gorm:"size:150;index" json:"service"`
	RequestedAmount      *float64   `gorm:"type:decimal(14,2)" json:"requestedAmount"`
	RequestedAt          time.Time  `gorm:"not null;index" json:"requestedAt"`
	RequestedBy          uint       `gorm:"not null;index" json:"requestedBy"`
	SubmittedAt          *time.Time `json:"submittedAt"`
	SubmittedBy          *uint      `gorm:"index" json:"submittedBy"`
	Status               string     `gorm:"size:30;not null;index" json:"status"`
	ExternalReference    string     `gorm:"size:150;index" json:"externalReference"`
	ExternalDecisionDate *time.Time `json:"externalDecisionDate"`
	ApprovedRate         *float64   `gorm:"type:decimal(5,2)" json:"approvedRate"`
	ApprovedAmount       *float64   `gorm:"type:decimal(14,2)" json:"approvedAmount"`
	InsuranceAmount      *float64   `gorm:"type:decimal(14,2)" json:"insuranceAmount"`
	PatientAmount        *float64   `gorm:"type:decimal(14,2)" json:"patientAmount"`
	CeilingAmount        *float64   `gorm:"type:decimal(14,2)" json:"ceilingAmount"`
	RejectionReason      string     `gorm:"type:text" json:"rejectionReason"`
	Comment              string     `gorm:"type:text" json:"comment"`
	DecidedBy            *uint      `gorm:"index" json:"decidedBy"`
	CreatedBy            uint       `gorm:"not null;index" json:"createdBy"`
	UpdatedBy            uint       `gorm:"not null;index" json:"updatedBy"`
	CreatedAt            time.Time  `json:"createdAt"`
	UpdatedAt            time.Time  `json:"updatedAt"`
}

func (InsuranceAuthorization) TableName() string { return "insurance_authorizations" }
