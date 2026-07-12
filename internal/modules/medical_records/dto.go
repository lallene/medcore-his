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

	HasExams                bool `json:"has_exams"`
	HasPrescriptions        bool `json:"has_prescriptions"`
	SickLeaveRequired       bool `json:"sick_leave_required"`
	HospitalizationRequired bool `json:"hospitalization_required"`
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

type CommonMedicalRecordResponse struct {
	MedicalRecord MedicalRecord `json:"medical_record"`

	Profile *PatientMedicalProfile `json:"profile"`

	Allergies        []Allergy        `json:"allergies"`
	MedicalHistories []MedicalHistory `json:"medical_histories"`

	SurgicalHistories      []SurgicalHistory      `json:"surgical_histories"`
	FamilyMedicalHistories []FamilyMedicalHistory `json:"family_medical_histories"`
	RegularTreatments      []RegularTreatment     `json:"regular_treatments"`
	Vaccinations           []Vaccination          `json:"vaccinations"`
	Disabilities           []Disability           `json:"disabilities"`

	Lifestyle *Lifestyle `json:"lifestyle"`

	MedicalDevices []MedicalDevice   `json:"medical_devices"`
	VitalSigns     []VitalSign       `json:"vital_signs"`
	Documents      []MedicalDocument `json:"documents"`
}

type UpdateCommonMedicalRecordRequest struct {
	Profile *PatientMedicalProfileRequest `json:"profile"`

	SurgicalHistories      []SurgicalHistoryRequest      `json:"surgical_histories"`
	FamilyMedicalHistories []FamilyMedicalHistoryRequest `json:"family_medical_histories"`
	RegularTreatments      []RegularTreatmentRequest     `json:"regular_treatments"`
	Vaccinations           []VaccinationRequest          `json:"vaccinations"`
	Disabilities           []DisabilityRequest           `json:"disabilities"`
	Allergies              []AllergyRequest              `json:"allergies"`
	MedicalHistories       []MedicalHistoryRequest       `json:"medical_histories"`

	Lifestyle *LifestyleRequest `json:"lifestyle"`

	MedicalDevices []MedicalDeviceRequest `json:"medical_devices"`

	UpdatedBy uint `json:"updated_by"`
}

type AllergyRequest struct {
	AllergenType string `json:"allergen_type"`
	AllergenName string `json:"allergen_name"`
	Reaction     string `json:"reaction"`
	Severity     string `json:"severity"`
	Comment      string `json:"comment"`
	IsActive     bool   `json:"is_active"`
}

type MedicalHistoryRequest struct {
	Type        string     `json:"type"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	StartDate   *time.Time `json:"start_date"`
	EndDate     *time.Time `json:"end_date"`
	Status      string     `json:"status"`
	Severity    string     `json:"severity"`
	Comment     string     `json:"comment"`
}

type PatientMedicalProfileRequest struct {
	Email         string `json:"email"`
	Address       string `json:"address"`
	MaritalStatus string `json:"marital_status"`
	Profession    string `json:"profession"`
	PhotoURL      string `json:"photo_url"`

	EmergencyContactFirstName    string `json:"emergency_contact_first_name"`
	EmergencyContactLastName     string `json:"emergency_contact_last_name"`
	EmergencyContactRelationship string `json:"emergency_contact_relationship"`
	EmergencyContactPhone        string `json:"emergency_contact_phone"`

	LegalGuardianName         string `json:"legal_guardian_name"`
	LegalGuardianRelationship string `json:"legal_guardian_relationship"`
	LegalGuardianPhone        string `json:"legal_guardian_phone"`
	LegalGuardianAddress      string `json:"legal_guardian_address"`

	InsuranceName        string `json:"insurance_name"`
	MutualName           string `json:"mutual_name"`
	InsuranceNumber      string `json:"insurance_number"`
	CoverageOrganization string `json:"coverage_organization"`

	BloodGroup string `json:"blood_group"`
	Rhesus     string `json:"rhesus"`
}

type SurgicalHistoryRequest struct {
	ProcedureName string     `json:"procedure_name"`
	ProcedureDate *time.Time `json:"procedure_date"`
	Facility      string     `json:"facility"`
	Complications string     `json:"complications"`
	Comment       string     `json:"comment"`
}

type FamilyMedicalHistoryRequest struct {
	Disease      string `json:"disease"`
	Relationship string `json:"relationship"`
	Comment      string `json:"comment"`
}

type RegularTreatmentRequest struct {
	MedicationName string     `json:"medication_name"`
	Dosage         string     `json:"dosage"`
	Frequency      string     `json:"frequency"`
	StartDate      *time.Time `json:"start_date"`
	Prescriber     string     `json:"prescriber"`
	IsActive       bool       `json:"is_active"`
}

type VaccinationRequest struct {
	VaccineName     string     `json:"vaccine_name"`
	Dose            string     `json:"dose"`
	VaccinationDate *time.Time `json:"vaccination_date"`
	NextBoosterDate *time.Time `json:"next_booster_date"`
	Status          string     `json:"status"`
}

type DisabilityRequest struct {
	Type         string `json:"type"`
	Level        string `json:"level"`
	SpecialNeeds string `json:"special_needs"`
}

type LifestyleRequest struct {
	Tobacco          string `json:"tobacco"`
	Alcohol          string `json:"alcohol"`
	PhysicalActivity string `json:"physical_activity"`
	Diet             string `json:"diet"`
}

type MedicalDeviceRequest struct {
	Type             string     `json:"type"`
	Name             string     `json:"name"`
	Reference        string     `json:"reference"`
	ImplantationDate *time.Time `json:"implantation_date"`
	Comment          string     `json:"comment"`
	IsActive         bool       `json:"is_active"`
}
