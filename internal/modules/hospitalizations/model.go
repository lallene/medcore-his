package hospitalizations

import (
	"time"

	"github.com/lallene/medcore-his/backend/internal/core/entity"
	"github.com/lallene/medcore-his/backend/internal/modules/consultations"
	"github.com/lallene/medcore-his/backend/internal/modules/medical_records"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
)

const (
	StatusPlanned    = "PLANNED"
	StatusAdmitted   = "ADMITTED"
	StatusDischarged = "DISCHARGED"
	StatusCancelled  = "CANCELLED"
)

type Hospitalization struct {
	entity.BaseEntity

	PatientID uint             `gorm:"not null;index" json:"patientId"`
	Patient   patients.Patient `gorm:"foreignKey:PatientID" json:"patient"`

	MedicalRecordID uint                          `gorm:"not null;index" json:"medicalRecordId"`
	MedicalRecord   medical_records.MedicalRecord `gorm:"foreignKey:MedicalRecordID" json:"-"`

	SourceConsultationID uint                       `gorm:"not null;uniqueIndex" json:"sourceConsultationId"`
	SourceConsultation   consultations.Consultation `gorm:"foreignKey:SourceConsultationID" json:"sourceConsultation"`

	AdmissionNumber     string     `gorm:"size:50;not null;uniqueIndex" json:"admissionNumber"`
	HospitalizationType string     `gorm:"size:50;not null" json:"hospitalizationType"`
	AdmissionReason     string     `gorm:"type:text;not null" json:"admissionReason"`
	AdmissionDiagnosis  string     `gorm:"type:text" json:"admissionDiagnosis"`
	Department          string     `gorm:"size:150;index" json:"department"`
	Status              string     `gorm:"size:20;not null;index;check:hospitalization_status_valid,status IN ('PLANNED','ADMITTED','DISCHARGED','CANCELLED')" json:"status"`
	AdmittedAt          *time.Time `json:"admittedAt"`
	ExpectedDischargeAt *time.Time `json:"expectedDischargeAt"`
	DischargedAt        *time.Time `json:"dischargedAt"`
	DischargeDiagnosis  string     `gorm:"type:text" json:"dischargeDiagnosis"`
	DischargeSummary    string     `gorm:"type:text" json:"dischargeSummary"`
}

func (Hospitalization) TableName() string { return "hospitalizations" }
