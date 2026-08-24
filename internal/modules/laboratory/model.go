package laboratory

import (
	"github.com/lallene/medcore-his/backend/internal/modules/organization"
	"time"
)

const (
	StatusOrdered         = "ORDERED"
	StatusSamplePending   = "SAMPLE_PENDING"
	StatusSampleCollected = "SAMPLE_COLLECTED"
	StatusInProgress      = "IN_PROGRESS"
	StatusResultEntered   = "RESULT_ENTERED"
	StatusValidated       = "VALIDATED"
	StatusCancelled       = "CANCELLED"
	StatusRejected        = "REJECTED"
)

type Order struct {
	ID                  uint                  `gorm:"primaryKey" json:"id"`
	RequestNumber       string                `gorm:"size:40;uniqueIndex;not null" json:"requestNumber"`
	ConsultationID      uint                  `gorm:"not null;uniqueIndex:ux_laboratory_orders_prescription;index" json:"consultationId"`
	MedicalExamID       uint                  `gorm:"not null;uniqueIndex:ux_laboratory_orders_prescription;index" json:"medicalExamId"`
	PatientID           uint                  `gorm:"not null;index" json:"patientId"`
	MedicalRecordID     *uint                 `gorm:"index" json:"medicalRecordId"`
	Priority            string                `gorm:"size:20;not null;default:'ROUTINE';index" json:"priority"`
	Status              string                `gorm:"size:30;not null;default:'ORDERED';index" json:"status"`
	PrescribedBy        uint                  `gorm:"index" json:"prescribedBy"`
	CreatedBy           uint                  `gorm:"not null;index" json:"createdBy"`
	UpdatedBy           uint                  `gorm:"not null;index" json:"updatedBy"`
	RequestingServiceID *uint                 `gorm:"index" json:"requestingServiceId"`
	ExecutingServiceID  *uint                 `gorm:"index" json:"executingServiceId"`
	RequestingService   *organization.Service `gorm:"foreignKey:RequestingServiceID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"requestingService,omitempty"`
	ExecutingService    *organization.Service `gorm:"foreignKey:ExecutingServiceID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"executingService,omitempty"`
	CancelledReason     string                `gorm:"type:text" json:"cancelledReason"`
	CreatedAt           time.Time             `json:"createdAt"`
	UpdatedAt           time.Time             `json:"updatedAt"`
	ValidatedAt         *time.Time            `json:"validatedAt"`
	ValidatedBy         *uint                 `json:"validatedBy"`
	Sample              *Sample               `gorm:"foreignKey:OrderID" json:"sample,omitempty"`
	Results             []Result              `gorm:"foreignKey:OrderID" json:"results"`
	PatientName         string                `gorm:"-" json:"patientName"`
	PatientCode         string                `gorm:"-" json:"patientCode"`
	ExamName            string                `gorm:"-" json:"examName"`
	ExamCode            string                `gorm:"-" json:"examCode"`
	Category            string                `gorm:"-" json:"category"`
	Service             string                `gorm:"-" json:"service"`
	Prescriber          string                `gorm:"-" json:"prescriber"`
}

func (Order) TableName() string { return "laboratory_orders" }

type Sample struct {
	ID               uint      `gorm:"primaryKey" json:"id"`
	OrderID          uint      `gorm:"not null;uniqueIndex" json:"orderId"`
	SampleIdentifier string    `gorm:"size:60;uniqueIndex;not null" json:"sampleIdentifier"`
	SampleType       string    `gorm:"size:80;not null" json:"sampleType"`
	Status           string    `gorm:"size:30;not null;default:'COLLECTED'" json:"status"`
	Comment          string    `gorm:"type:text" json:"comment"`
	CollectedBy      uint      `gorm:"not null;index" json:"collectedBy"`
	CollectedAt      time.Time `gorm:"not null" json:"collectedAt"`
	CreatedAt        time.Time `json:"createdAt"`
}

func (Sample) TableName() string { return "laboratory_samples" }

type Result struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	OrderID       uint      `gorm:"not null;index;uniqueIndex:ux_laboratory_results_parameter" json:"orderId"`
	Parameter     string    `gorm:"size:160;not null;uniqueIndex:ux_laboratory_results_parameter" json:"parameter"`
	Value         string    `gorm:"size:120;not null" json:"value"`
	NumericValue  *float64  `json:"numericValue"`
	Unit          string    `gorm:"size:50" json:"unit"`
	ReferenceMin  *float64  `json:"referenceMin"`
	ReferenceMax  *float64  `json:"referenceMax"`
	ReferenceText string    `gorm:"size:160" json:"referenceText"`
	Flag          string    `gorm:"size:20;not null;default:'NORMAL';index" json:"flag"`
	Comment       string    `gorm:"type:text" json:"comment"`
	EnteredBy     uint      `gorm:"not null;index" json:"enteredBy"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

func (Result) TableName() string { return "laboratory_results" }

type ListItem struct {
	ID               uint      `json:"id"`
	RequestNumber    string    `json:"requestNumber"`
	PatientID        uint      `json:"patientId"`
	PatientName      string    `json:"patientName"`
	PatientCode      string    `json:"patientCode"`
	MedicalRecordID  *uint     `json:"medicalRecordId"`
	ConsultationID   uint      `json:"consultationId"`
	ExamCode         string    `json:"examCode"`
	ExamName         string    `json:"examName"`
	Category         string    `json:"category"`
	Service          string    `json:"service"`
	Prescriber       string    `json:"prescriber"`
	PrescribedAt     time.Time `json:"prescribedAt"`
	Priority         string    `json:"priority"`
	Status           string    `json:"status"`
	SampleIdentifier string    `json:"sampleIdentifier"`
}
