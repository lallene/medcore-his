package medical_records

import "time"

type CreateMedicalRecordRequest struct {
	PatientID uint `json:"patient_id" binding:"required"`
}

type MedicalRecordOverviewResponse struct {
	MedicalRecord    MedicalRecord    `json:"medical_record"`
	Alerts           []MedicalAlert   `json:"alerts"`
	Allergies        []Allergy        `json:"allergies"`
	MedicalHistories []MedicalHistory `json:"medical_histories"`
	LastVitalSigns   *VitalSign       `json:"last_vital_signs"`
}

type CreateAlertRequest struct {
	Type        string `json:"type" binding:"required"`
	Title       string `json:"title" binding:"required"`
	Description string `json:"description"`
	Severity    string `json:"severity"`
	CreatedBy   uint   `json:"created_by"`
}

type CreateAllergyRequest struct {
	AllergenType string `json:"allergen_type" binding:"required"`
	AllergenName string `json:"allergen_name" binding:"required"`
	Reaction     string `json:"reaction"`
	Severity     string `json:"severity"`
	Comment      string `json:"comment"`
	CreatedBy    uint   `json:"created_by"`
}

type CreateMedicalHistoryRequest struct {
	Type        string     `json:"type" binding:"required"`
	Title       string     `json:"title" binding:"required"`
	Description string     `json:"description"`
	StartDate   *time.Time `json:"start_date"`
	EndDate     *time.Time `json:"end_date"`
	Status      string     `json:"status"`
	Severity    string     `json:"severity"`
	Comment     string     `json:"comment"`
	CreatedBy   uint       `json:"created_by"`
}

type CreateVitalSignRequest struct {
	ConsultationID *uint `json:"consultation_id"`

	WeightKg             *float64 `json:"weight_kg"`
	HeightCm             *float64 `json:"height_cm"`
	TemperatureC         *float64 `json:"temperature_c"`
	SystolicBP           *int     `json:"systolic_bp"`
	DiastolicBP          *int     `json:"diastolic_bp"`
	HeartRate            *int     `json:"heart_rate"`
	RespiratoryRate      *int     `json:"respiratory_rate"`
	OxygenSaturation     *float64 `json:"oxygen_saturation"`
	BloodGlucose         *float64 `json:"blood_glucose"`
	WaistCircumferenceCm *float64 `json:"waist_circumference_cm"`
	PainScore            *int     `json:"pain_score"`
	Comment              string   `json:"comment"`

	MeasuredBy uint       `json:"measured_by"`
	MeasuredAt *time.Time `json:"measured_at"`
}

type MedicalSummaryConsultationItem struct {
	ID           uint      `json:"id"`
	PatientID    uint      `json:"patient_id"`
	DoctorName   string    `json:"doctor_name"`
	Service      string    `json:"service"`
	Status       string    `json:"status"`
	Diagnosis    string    `json:"diagnosis"`
	Observations string    `json:"observations"`
	Treatment    string    `json:"treatment"`
	CreatedAt    time.Time `json:"created_at"`
}

type MedicalSummaryDocumentItem struct {
	ConsultationID uint   `json:"consultation_id"`
	Type           string `json:"type"`
	Label          string `json:"label"`
	URL            string `json:"url"`
}

type PatientMedicalSummaryResponse struct {
	PatientID uint `json:"patient_id"`

	MedicalRecord    MedicalRecord          `json:"medical_record"`
	Alerts           []MedicalAlert         `json:"alerts"`
	Allergies        []Allergy              `json:"allergies"`
	MedicalHistories []MedicalHistory       `json:"medical_histories"`
	LastVitalSigns   *VitalSign             `json:"last_vital_signs"`
	Timeline         []MedicalTimelineEvent `json:"timeline"`

	RecentConsultations []MedicalSummaryConsultationItem `json:"recent_consultations"`
	Documents           []MedicalSummaryDocumentItem     `json:"documents"`
}
