package medical_records

import (
	"bytes"
	"encoding/json"
	"time"
)

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
}

type CreateAllergyRequest struct {
	AllergenType string `json:"allergen_type" binding:"required"`
	AllergenName string `json:"allergen_name" binding:"required"`
	Reaction     string `json:"reaction"`
	Severity     string `json:"severity"`
	Comment      string `json:"comment"`
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
	ExpectedUpdatedAt *time.Time                    `json:"expected_updated_at"`
	Profile           *PatientMedicalProfileRequest `json:"profile"`

	SurgicalHistories      PatchCollection[SurgicalHistoryRequest]      `json:"surgical_histories"`
	FamilyMedicalHistories PatchCollection[FamilyMedicalHistoryRequest] `json:"family_medical_histories"`
	RegularTreatments      PatchCollection[RegularTreatmentRequest]     `json:"regular_treatments"`
	Vaccinations           PatchCollection[VaccinationRequest]          `json:"vaccinations"`
	Disabilities           PatchCollection[DisabilityRequest]           `json:"disabilities"`
	Allergies              PatchCollection[AllergyRequest]              `json:"allergies"`
	MedicalHistories       PatchCollection[MedicalHistoryRequest]       `json:"medical_histories"`
	VitalSigns             PatchCollection[VitalSignRequest]            `json:"vital_signs"`
	Documents              PatchCollection[MedicalDocumentRequest]      `json:"documents"`

	Lifestyle      *LifestyleRequest                     `json:"lifestyle"`
	MedicalDevices PatchCollection[MedicalDeviceRequest] `json:"medical_devices"`

	authorID uint
}

// PatchCollection accepts the new {"upsert": [...], "delete_ids": [...]}
// contract and, temporarily, the legacy array form. A legacy array is always
// interpreted as upsert-only and never as a replacement instruction.
type PatchCollection[T any] struct {
	Present   bool
	Upsert    []T    `json:"upsert"`
	DeleteIDs []uint `json:"delete_ids"`
}

func (p *PatchCollection[T]) UnmarshalJSON(data []byte) error {
	p.Present = true
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		return json.Unmarshal(trimmed, &p.Upsert)
	}

	type payload struct {
		Upsert    []T    `json:"upsert"`
		DeleteIDs []uint `json:"delete_ids"`
	}
	var value payload
	if err := json.Unmarshal(trimmed, &value); err != nil {
		return err
	}
	p.Upsert = value.Upsert
	p.DeleteIDs = value.DeleteIDs
	return nil
}

// NullableTimePatch distinguishes an absent field from an explicit JSON null.
type NullableTimePatch struct {
	Set   bool
	Value *time.Time
}

func (p *NullableTimePatch) UnmarshalJSON(data []byte) error {
	p.Set = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		p.Value = nil
		return nil
	}
	var value time.Time
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	p.Value = &value
	return nil
}

// NullableUintPatch distinguishes an absent nullable foreign key from null.
type NullableUintPatch struct {
	Set   bool
	Value *uint
}

// NullableValuePatch preserves the three states needed by nullable scalar
// columns: absent, explicit null, and an explicit value (including zero).
type NullableValuePatch[T any] struct {
	Set   bool
	Value *T
}

func (p *NullableValuePatch[T]) UnmarshalJSON(data []byte) error {
	p.Set = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		p.Value = nil
		return nil
	}
	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	p.Value = &value
	return nil
}

func (p *NullableUintPatch) UnmarshalJSON(data []byte) error {
	p.Set = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		p.Value = nil
		return nil
	}
	var value uint
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	p.Value = &value
	return nil
}

type AllergyRequest struct {
	ID           uint    `json:"id"`
	AllergenType *string `json:"allergen_type"`
	AllergenName *string `json:"allergen_name"`
	Reaction     *string `json:"reaction"`
	Severity     *string `json:"severity"`
	Comment      *string `json:"comment"`
	IsActive     *bool   `json:"is_active"`
}

type MedicalHistoryRequest struct {
	ID          uint              `json:"id"`
	Type        *string           `json:"type"`
	Title       *string           `json:"title"`
	Description *string           `json:"description"`
	StartDate   NullableTimePatch `json:"start_date"`
	EndDate     NullableTimePatch `json:"end_date"`
	Status      *string           `json:"status"`
	Severity    *string           `json:"severity"`
	Comment     *string           `json:"comment"`
}

type PatientMedicalProfileRequest struct {
	Email         *string `json:"email"`
	Address       *string `json:"address"`
	MaritalStatus *string `json:"marital_status"`
	Profession    *string `json:"profession"`
	PhotoURL      *string `json:"photo_url"`

	EmergencyContactFirstName    *string `json:"emergency_contact_first_name"`
	EmergencyContactLastName     *string `json:"emergency_contact_last_name"`
	EmergencyContactRelationship *string `json:"emergency_contact_relationship"`
	EmergencyContactPhone        *string `json:"emergency_contact_phone"`

	LegalGuardianName         *string `json:"legal_guardian_name"`
	LegalGuardianRelationship *string `json:"legal_guardian_relationship"`
	LegalGuardianPhone        *string `json:"legal_guardian_phone"`
	LegalGuardianAddress      *string `json:"legal_guardian_address"`

	InsuranceName        *string `json:"insurance_name"`
	MutualName           *string `json:"mutual_name"`
	InsuranceNumber      *string `json:"insurance_number"`
	CoverageOrganization *string `json:"coverage_organization"`

	BloodGroup *string `json:"blood_group"`
	Rhesus     *string `json:"rhesus"`
}

type SurgicalHistoryRequest struct {
	ID            uint              `json:"id"`
	ProcedureName *string           `json:"procedure_name"`
	ProcedureDate NullableTimePatch `json:"procedure_date"`
	Facility      *string           `json:"facility"`
	Complications *string           `json:"complications"`
	Comment       *string           `json:"comment"`
}

type FamilyMedicalHistoryRequest struct {
	ID           uint    `json:"id"`
	Disease      *string `json:"disease"`
	Relationship *string `json:"relationship"`
	Comment      *string `json:"comment"`
}

type RegularTreatmentRequest struct {
	ID             uint              `json:"id"`
	MedicationName *string           `json:"medication_name"`
	Dosage         *string           `json:"dosage"`
	Frequency      *string           `json:"frequency"`
	StartDate      NullableTimePatch `json:"start_date"`
	Prescriber     *string           `json:"prescriber"`
	IsActive       *bool             `json:"is_active"`
}

type VaccinationRequest struct {
	ID              uint              `json:"id"`
	VaccineName     *string           `json:"vaccine_name"`
	Dose            *string           `json:"dose"`
	VaccinationDate NullableTimePatch `json:"vaccination_date"`
	NextBoosterDate NullableTimePatch `json:"next_booster_date"`
	Status          *string           `json:"status"`
}

type DisabilityRequest struct {
	ID           uint    `json:"id"`
	Type         *string `json:"type"`
	Level        *string `json:"level"`
	SpecialNeeds *string `json:"special_needs"`
}

type LifestyleRequest struct {
	Tobacco          *string `json:"tobacco"`
	Alcohol          *string `json:"alcohol"`
	PhysicalActivity *string `json:"physical_activity"`
	Diet             *string `json:"diet"`
}

type MedicalDeviceRequest struct {
	ID               uint              `json:"id"`
	Type             *string           `json:"type"`
	Name             *string           `json:"name"`
	Reference        *string           `json:"reference"`
	ImplantationDate NullableTimePatch `json:"implantation_date"`
	Comment          *string           `json:"comment"`
	IsActive         *bool             `json:"is_active"`
}

type VitalSignRequest struct {
	ID             uint              `json:"id"`
	ConsultationID NullableUintPatch `json:"consultation_id"`

	WeightKg             NullableValuePatch[float64] `json:"weight_kg"`
	HeightCm             NullableValuePatch[float64] `json:"height_cm"`
	TemperatureC         NullableValuePatch[float64] `json:"temperature_c"`
	SystolicBP           NullableValuePatch[int]     `json:"systolic_bp"`
	DiastolicBP          NullableValuePatch[int]     `json:"diastolic_bp"`
	HeartRate            NullableValuePatch[int]     `json:"heart_rate"`
	RespiratoryRate      NullableValuePatch[int]     `json:"respiratory_rate"`
	OxygenSaturation     NullableValuePatch[float64] `json:"oxygen_saturation"`
	BloodGlucose         NullableValuePatch[float64] `json:"blood_glucose"`
	WaistCircumferenceCm NullableValuePatch[float64] `json:"waist_circumference_cm"`
	PainScore            NullableValuePatch[int]     `json:"pain_score"`

	PainLocation *string `json:"pain_location"`
	PainType     *string `json:"pain_type"`
	PainDuration *string `json:"pain_duration"`

	MeasuredAt NullableTimePatch `json:"measured_at"`
	Comment    *string           `json:"comment"`
}

type MedicalDocumentRequest struct {
	ID             uint              `json:"id"`
	ConsultationID NullableUintPatch `json:"consultation_id"`

	Type         *string           `json:"type"`
	Label        *string           `json:"label"`
	DocumentDate NullableTimePatch `json:"document_date"`
	FileName     *string           `json:"file_name"`
	MimeType     *string           `json:"mime_type"`
	FileURL      *string           `json:"file_url"`
	Description  *string           `json:"description"`
}
