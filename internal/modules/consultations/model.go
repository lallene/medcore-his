package consultations

import (
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/patients"
)

const (
	ConsultationStatusDraft      = "draft"
	ConsultationStatusInProgress = "in_progress"
	ConsultationStatusCompleted  = "completed"
	ConsultationStatusCancelled  = "cancelled"
)

type Consultation struct {
	ID        uint             `gorm:"primaryKey" json:"id"`
	PatientID uint             `gorm:"not null;index" json:"patientId"`
	Patient   patients.Patient `gorm:"foreignKey:PatientID" json:"patient"`

	DoctorName         string     `json:"doctorName"`
	Service            string     `json:"service"`
	Status             string     `gorm:"not null;default:draft;index" json:"status"`
	StartedAt          *time.Time `json:"startedAt"`
	CompletedAt        *time.Time `json:"completedAt"`
	CancelledAt        *time.Time `json:"cancelledAt"`
	CancellationReason string     `gorm:"type:text" json:"cancellationReason"`

	Diagnosis    string `gorm:"type:text" json:"diagnosis"`
	Observations string `gorm:"type:text" json:"observations"`
	Treatment    string `gorm:"type:text" json:"treatment"`

	SickLeaveRequired  bool       `json:"sickLeaveRequired"`
	SickLeaveDays      int        `json:"sickLeaveDays"`
	SickLeaveStartDate *time.Time `json:"sickLeaveStartDate"`
	SickLeaveEndDate   *time.Time `json:"sickLeaveEndDate"`

	Vitals                  ConsultationVitals         `gorm:"foreignKey:ConsultationID" json:"vitals"`
	Reasons                 []ConsultationReason       `gorm:"many2many:consultation_reason_items;" json:"reasons"`
	Exams                   []MedicalExam              `gorm:"many2many:consultation_exam_requests;" json:"exams"`
	Prescriptions           []ConsultationPrescription `gorm:"foreignKey:ConsultationID" json:"prescriptions"`
	HospitalizationRequired bool                       `json:"hospitalizationRequired"`
	HospitalizationReason   string                     `gorm:"type:text" json:"hospitalizationReason"`
	HospitalizationType     string                     `gorm:"size:50" json:"hospitalizationType"` // medicale / chirurgicale
	HospitalizationDuration int                        `json:"hospitalizationDuration"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type ConsultationReason struct {
	ID       uint   `gorm:"primaryKey" json:"id"`
	Code     string `gorm:"unique;not null" json:"code"`
	Name     string `gorm:"not null" json:"name"`
	Category string `json:"category"`
	IsActive bool   `gorm:"default:true" json:"isActive"`
}

type MedicalExam struct {
	ID       uint   `gorm:"primaryKey" json:"id"`
	Code     string `gorm:"unique;not null" json:"code"`
	Name     string `gorm:"not null" json:"name"`
	Category string `json:"category"`
	IsActive bool   `gorm:"default:true" json:"isActive"`
}

type ConsultationVitals struct {
	ID             uint `gorm:"primaryKey" json:"id"`
	ConsultationID uint `gorm:"uniqueIndex;not null" json:"consultationId"`

	Temperature            *float64 `json:"temperature"`
	BloodPressureSystolic  *int     `json:"bloodPressureSystolic"`
	BloodPressureDiastolic *int     `json:"bloodPressureDiastolic"`
	HeartRate              *int     `json:"heartRate"`
	RespiratoryRate        *int     `json:"respiratoryRate"`
	OxygenSaturation       *int     `json:"oxygenSaturation"`
	Weight                 *float64 `json:"weight"`
	Height                 *float64 `json:"height"`
	BloodGlucose           *float64 `json:"bloodGlucose"`
	PainScore              *int     `json:"painScore"`
}

type ConsultationExamRequest struct {
	ConsultationID uint   `gorm:"primaryKey" json:"consultationId"`
	MedicalExamID  uint   `gorm:"primaryKey" json:"examId"`
	Status         string `gorm:"default:requested" json:"status"`
	Notes          string `gorm:"type:text" json:"notes"`
}

type ConsultationPrescription struct {
	ID uint `gorm:"primaryKey" json:"id"`

	ConsultationID uint `gorm:"not null;index" json:"consultationId"`

	PresentationID *uint `gorm:"index" json:"presentationId"`

	MedicationName string `gorm:"size:200;not null" json:"medicationName"`
	Dosage         string `gorm:"size:100" json:"dosage"`
	Form           string `gorm:"size:100" json:"form"`
	Route          string `gorm:"size:100" json:"route"`

	Quantity float64 `gorm:"type:decimal(12,2);default:0" json:"quantity"`

	Frequency    string `gorm:"size:200" json:"frequency"`
	Duration     string `gorm:"size:100" json:"duration"`
	Instructions string `gorm:"type:text" json:"instructions"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
