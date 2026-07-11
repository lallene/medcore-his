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

	Antecedent             ConsultationAntecedent              `gorm:"constraint:OnDelete:CASCADE;" json:"antecedent"`
	PhysicalExams          []ConsultationPhysicalExam          `gorm:"constraint:OnDelete:CASCADE;" json:"physicalExams"`
	AdministeredTreatments []ConsultationAdministeredTreatment `gorm:"constraint:OnDelete:CASCADE;" json:"administeredTreatments"`

	PreviousMedications []ConsultationPreviousMedication `gorm:"constraint:OnDelete:CASCADE;" json:"previousMedications"`

	SurgicalHistories []ConsultationSurgicalHistory `gorm:"constraint:OnDelete:CASCADE;" json:"surgicalHistories"`

	GynecoObstetricHistories []ConsultationGynecoObstetricHistory `gorm:"constraint:OnDelete:CASCADE;" json:"gynecoObstetricHistories"`

	SOAP *ConsultationSOAP `json:"soap,omitempty" gorm:"foreignKey:ConsultationID"`

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

type ConsultationAntecedent struct {
	ID             uint `gorm:"primaryKey" json:"id"`
	ConsultationID uint `gorm:"not null;index" json:"consultationId"`

	PreviousMedication string `json:"previousMedication"`

	HasHTA       *bool  `json:"hasHta"`
	HasDiabetes  *bool  `json:"hasDiabetes"`
	OtherMedical string `json:"otherMedical"`

	SurgicalHistory string `json:"surgicalHistory"`

	GynecoObstetricHistory string `json:"gynecoObstetricHistory"`
	DDR                    string `json:"ddr"`
	PregnancyOngoing       *bool  `json:"pregnancyOngoing"`

	Tobacco *bool `json:"tobacco"`
	Alcohol *bool `json:"alcohol"`

	VisitType string `json:"visitType"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type ConsultationPhysicalExam struct {
	ID             uint `gorm:"primaryKey" json:"id"`
	ConsultationID uint `gorm:"not null;index" json:"consultationId"`

	AreaID uint             `gorm:"not null;index" json:"areaId"`
	Area   PhysicalExamArea `gorm:"foreignKey:AreaID" json:"area"`

	Observation string `gorm:"type:text" json:"observation"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type ConsultationAdministeredTreatment struct {
	ID             uint `gorm:"primaryKey" json:"id"`
	ConsultationID uint `gorm:"not null;index" json:"consultationId"`

	PresentationID *uint `json:"presentationId"`

	MedicationName string `gorm:"not null" json:"medicationName"`
	Dosage         string `json:"dosage"`
	Form           string `json:"form"`
	Route          string `json:"route"`

	Quantity     float64 `json:"quantity"`
	Instructions string  `gorm:"type:text" json:"instructions"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type PhysicalExamArea struct {
	ID uint `gorm:"primaryKey" json:"id"`

	Code     string `gorm:"size:100;uniqueIndex;not null" json:"code"`
	Category string `gorm:"size:150;index;not null" json:"category"`
	Name     string `gorm:"size:150;index;not null" json:"name"`

	IsActive bool `gorm:"not null;default:true" json:"isActive"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type ConsultationPreviousMedication struct {
	ID             uint `gorm:"primaryKey" json:"id"`
	ConsultationID uint `gorm:"not null;index" json:"consultationId"`

	PresentationID *uint `json:"presentationId"`

	MedicationName string `gorm:"not null" json:"medicationName"`
	Dosage         string `json:"dosage"`
	Form           string `json:"form"`
	Route          string `json:"route"`

	Instructions string `gorm:"type:text" json:"instructions"`
	Status       string `gorm:"size:50;default:'ONGOING'" json:"status"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type ConsultationSurgicalHistory struct {
	ID             uint `gorm:"primaryKey" json:"id"`
	ConsultationID uint `gorm:"not null;index" json:"consultationId"`

	ProcedureName string `gorm:"not null" json:"procedureName"`
	ProcedureDate string `json:"procedureDate"`
	Indication    string `gorm:"type:text" json:"indication"`
	Complications string `gorm:"type:text" json:"complications"`
	Notes         string `gorm:"type:text" json:"notes"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type ConsultationGynecoObstetricHistory struct {
	ID             uint `gorm:"primaryKey" json:"id"`
	ConsultationID uint `gorm:"not null;index" json:"consultationId"`

	EventType string `gorm:"size:100;not null" json:"eventType"`
	EventDate string `json:"eventDate"`
	Outcome   string `gorm:"type:text" json:"outcome"`
	Notes     string `gorm:"type:text" json:"notes"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type ConsultationSOAP struct {
	ID             uint `json:"id" gorm:"primaryKey"`
	ConsultationID uint `json:"consultation_id" gorm:"not null;uniqueIndex"`

	// S — Subjectif
	ChiefComplaint          string `json:"chief_complaint" gorm:"type:text"`
	HistoryOfPresentIllness string `json:"history_of_present_illness" gorm:"type:text"`
	AssociatedSymptoms      string `json:"associated_symptoms" gorm:"type:text"`
	PatientReportedNotes    string `json:"patient_reported_notes" gorm:"type:text"`

	// O — Objectif
	GeneralAppearance   string `json:"general_appearance" gorm:"type:text"`
	Consciousness       string `json:"consciousness" gorm:"type:text"`
	HydrationStatus     string `json:"hydration_status" gorm:"type:text"`
	PhysicalExamSummary string `json:"physical_exam_summary" gorm:"type:text"`

	// A — Assessment
	PrimaryDiagnosis    string `json:"primary_diagnosis" gorm:"type:text"`
	AssociatedDiagnoses string `json:"associated_diagnoses" gorm:"type:text"`
	ClinicalImpression  string `json:"clinical_impression" gorm:"type:text"`

	// P — Plan
	TreatmentPlan     string `json:"treatment_plan" gorm:"type:text"`
	InvestigationPlan string `json:"investigation_plan" gorm:"type:text"`
	FollowUpPlan      string `json:"follow_up_plan" gorm:"type:text"`
	PatientAdvice     string `json:"patient_advice" gorm:"type:text"`
	Disposition       string `json:"disposition" gorm:"type:varchar(50)"`

	CreatedBy uint      `json:"created_by"`
	UpdatedBy uint      `json:"updated_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
