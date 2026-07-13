package medical_records

import "time"

type PatientSummaryResponse struct {
	Patient          PatientSummaryPatient    `json:"patient"`
	MedicalRecord    MedicalRecordSummary     `json:"medical_record"`
	Allergies        []AllergySummary         `json:"allergies"`
	ChronicDiseases  []ChronicDiseaseSummary  `json:"chronic_diseases"`
	ActiveTreatments []TreatmentSummary       `json:"active_treatments"`
	LastVitals       *LastVitalSummary        `json:"last_vitals,omitempty"`
	LastConsultation *LastConsultationSummary `json:"last_consultation,omitempty"`
	ClinicalAlerts   []ClinicalAlertSummary   `json:"clinical_alerts"`
	Statistics       PatientSummaryStatistics `json:"statistics"`
}

type PatientSummaryPatient struct {
	ID              uint       `json:"id"`
	CodePatient     string     `json:"code_patient"`
	NumeroDossier   string     `json:"numero_dossier"`
	LastName        string     `json:"last_name"`
	FirstNames      string     `json:"first_names"`
	Sex             string     `json:"sex"`
	BirthDate       *time.Time `json:"birth_date"`
	Age             *int       `json:"age"`
	Phone           string     `json:"phone"`
	Address         string     `json:"address"`
	IsInsured       bool       `json:"is_insured"`
	CoverageRate    float64    `json:"coverage_rate"`
	InsuranceNumber string     `json:"insurance_number"`
	BloodGroup      string     `json:"blood_group"`
	Rhesus          string     `json:"rhesus"`
	InsuranceName   string     `json:"insurance_name"`
	MutualName      string     `json:"mutual_name"`
	PhotoURL        string     `json:"photo_url"`
}

type MedicalRecordSummary struct {
	ID                uint   `json:"id"`
	RecordNumber      string `json:"record_number"`
	Status            string `json:"status"`
	ActiveAllergies   int64  `json:"active_allergies"`
	ChronicDiseases   int64  `json:"chronic_diseases"`
	CurrentTreatments int64  `json:"current_treatments"`
}

type AllergySummary struct {
	ID         uint   `json:"id"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	Reaction   string `json:"reaction"`
	Severity   string `json:"severity"`
	IsHighRisk bool   `json:"is_high_risk"`
}

type ChronicDiseaseSummary struct {
	ID          uint       `json:"id"`
	Name        string     `json:"name"`
	Severity    string     `json:"severity"`
	DiagnosedAt *time.Time `json:"diagnosed_at"`
}

type TreatmentSummary struct {
	ID             uint       `json:"id"`
	MedicationName string     `json:"medication_name"`
	Dosage         string     `json:"dosage"`
	Frequency      string     `json:"frequency"`
	StartDate      *time.Time `json:"start_date"`
	Prescriber     string     `json:"prescriber"`
}

type LastVitalSummary struct {
	ID                   uint      `json:"id"`
	MeasuredAt           time.Time `json:"measured_at"`
	WeightKg             *float64  `json:"weight_kg"`
	HeightCm             *float64  `json:"height_cm"`
	BMI                  *float64  `json:"bmi"`
	SystolicBP           *int      `json:"systolic_bp"`
	DiastolicBP          *int      `json:"diastolic_bp"`
	HeartRate            *int      `json:"heart_rate"`
	Temperature          *float64  `json:"temperature"`
	RespiratoryRate      *int      `json:"respiratory_rate"`
	OxygenSaturation     *float64  `json:"oxygen_saturation"`
	BloodGlucose         *float64  `json:"blood_glucose"`
	PainScore            *int      `json:"pain_score"`
	WaistCircumferenceCm *float64  `json:"waist_circumference_cm"`
}

type LastConsultationSummary struct {
	ID         uint      `json:"id"`
	Date       time.Time `json:"date"`
	Service    string    `json:"service"`
	DoctorName string    `json:"doctor_name"`
	Diagnosis  string    `json:"diagnosis"`
	Status     string    `json:"status"`
}

type ClinicalAlertSummary struct {
	Severity    string `json:"severity"`
	Code        string `json:"code"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Source      string `json:"source"`
}

type PatientSummaryStatistics struct {
	Consultations    int64 `json:"consultations"`
	Hospitalizations int64 `json:"hospitalizations"`
	Prescriptions    int64 `json:"prescriptions"`
	Exams            int64 `json:"exams"`
	Documents        int64 `json:"documents"`
}
