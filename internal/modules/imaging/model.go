package imaging

import (
	"github.com/lallene/medcore-his/backend/internal/modules/organization"
	"time"
)

const (
	StatusOrdered       = "ORDERED"
	StatusScheduled     = "SCHEDULED"
	StatusInProgress    = "IN_PROGRESS"
	StatusReportDrafted = "REPORT_DRAFTED"
	StatusValidated     = "VALIDATED"
	StatusCancelled     = "CANCELLED"
)

type Order struct {
	ID                  uint                  `gorm:"primaryKey" json:"id"`
	OrderNumber         string                `gorm:"size:40;uniqueIndex;not null" json:"orderNumber"`
	AccessionNumber     string                `gorm:"size:40;uniqueIndex;not null" json:"accessionNumber"`
	ConsultationID      uint                  `gorm:"not null;uniqueIndex:ux_imaging_orders_prescription;index" json:"consultationId"`
	MedicalExamID       uint                  `gorm:"not null;uniqueIndex:ux_imaging_orders_prescription;index" json:"medicalExamId"`
	PatientID           uint                  `gorm:"not null;index" json:"patientId"`
	MedicalRecordID     *uint                 `gorm:"index" json:"medicalRecordId"`
	Modality            string                `gorm:"size:30;not null;index" json:"modality"`
	Priority            string                `gorm:"size:20;not null;default:'ROUTINE';index" json:"priority"`
	Status              string                `gorm:"size:30;not null;default:'ORDERED';index" json:"status"`
	PrescribedBy        uint                  `gorm:"index" json:"prescribedBy"`
	CreatedBy           uint                  `gorm:"not null;index" json:"createdBy"`
	UpdatedBy           uint                  `gorm:"not null;index" json:"updatedBy"`
	RequestingServiceID *uint                 `gorm:"index" json:"requestingServiceId"`
	ExecutingServiceID  *uint                 `gorm:"index" json:"executingServiceId"`
	RequestingService   *organization.Service `gorm:"foreignKey:RequestingServiceID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"requestingService,omitempty"`
	ExecutingService    *organization.Service `gorm:"foreignKey:ExecutingServiceID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"executingService,omitempty"`
	ScheduledAt         *time.Time            `gorm:"index" json:"scheduledAt"`
	ScheduledBy         *uint                 `gorm:"index" json:"scheduledBy"`
	ScheduleComment     string                `gorm:"type:text" json:"scheduleComment"`
	PerformedAt         *time.Time            `gorm:"index" json:"performedAt"`
	PerformedBy         *uint                 `gorm:"index" json:"performedBy"`
	TechnicalNotes      string                `gorm:"type:text" json:"technicalNotes"`
	ContrastUsed        bool                  `gorm:"not null;default:false" json:"contrastUsed"`
	ContrastProduct     string                `gorm:"size:160" json:"contrastProduct"`
	StudyInstanceUID    string                `gorm:"size:160" json:"studyInstanceUid"`
	ExternalViewerURL   string                `gorm:"size:500" json:"externalViewerUrl"`
	CancelledReason     string                `gorm:"type:text" json:"cancelledReason"`
	CreatedAt           time.Time             `json:"createdAt"`
	UpdatedAt           time.Time             `json:"updatedAt"`
	Report              *Report               `gorm:"foreignKey:OrderID" json:"report,omitempty"`
	PatientName         string                `gorm:"-" json:"patientName"`
	PatientCode         string                `gorm:"-" json:"patientCode"`
	ExamName            string                `gorm:"-" json:"examName"`
	ExamCode            string                `gorm:"-" json:"examCode"`
	Category            string                `gorm:"-" json:"category"`
	Service             string                `gorm:"-" json:"service"`
	Prescriber          string                `gorm:"-" json:"prescriber"`
}

func (Order) TableName() string { return "imaging_orders" }

type Report struct {
	ID                 uint       `gorm:"primaryKey" json:"id"`
	OrderID            uint       `gorm:"not null;uniqueIndex" json:"orderId"`
	ClinicalIndication string     `gorm:"type:text" json:"clinicalIndication"`
	Technique          string     `gorm:"type:text" json:"technique"`
	Findings           string     `gorm:"type:text;not null" json:"findings"`
	Conclusion         string     `gorm:"type:text;not null" json:"conclusion"`
	Recommendation     string     `gorm:"type:text" json:"recommendation"`
	DocumentURL        string     `gorm:"size:500" json:"documentUrl"`
	DraftedBy          uint       `gorm:"not null;index" json:"draftedBy"`
	DraftedAt          time.Time  `gorm:"not null" json:"draftedAt"`
	ValidatedBy        *uint      `gorm:"index" json:"validatedBy"`
	ValidatedAt        *time.Time `json:"validatedAt"`
	CreatedAt          time.Time  `json:"createdAt"`
	UpdatedAt          time.Time  `json:"updatedAt"`
}

func (Report) TableName() string { return "imaging_reports" }

type ListItem struct {
	ID             uint       `json:"id"`
	OrderNumber    string     `json:"orderNumber"`
	PatientID      uint       `json:"patientId"`
	PatientName    string     `json:"patientName"`
	PatientCode    string     `json:"patientCode"`
	ConsultationID uint       `json:"consultationId"`
	ExamCode       string     `json:"examCode"`
	ExamName       string     `json:"examName"`
	Category       string     `json:"category"`
	Modality       string     `json:"modality"`
	Service        string     `json:"service"`
	Prescriber     string     `json:"prescriber"`
	PrescribedAt   time.Time  `json:"prescribedAt"`
	Priority       string     `json:"priority"`
	Status         string     `json:"status"`
	ScheduledAt    *time.Time `json:"scheduledAt"`
	PerformedAt    *time.Time `json:"performedAt"`
}
