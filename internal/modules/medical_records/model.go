package medical_records

import "time"

type MedicalRecord struct {
	ID           uint      `json:"id" gorm:"primaryKey"`
	PatientID    uint      `json:"patient_id" gorm:"not null;uniqueIndex"`
	RecordNumber string    `json:"record_number" gorm:"uniqueIndex;not null"`
	Status       string    `json:"status" gorm:"default:active"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`

	Alerts                 []MedicalAlert         `json:"alerts,omitempty" gorm:"foreignKey:MedicalRecordID"`
	Allergies              []Allergy              `json:"allergies,omitempty" gorm:"foreignKey:MedicalRecordID"`
	MedicalHistories       []MedicalHistory       `json:"medical_histories,omitempty" gorm:"foreignKey:MedicalRecordID"`
	VitalSigns             []VitalSign            `json:"vital_signs,omitempty" gorm:"foreignKey:MedicalRecordID"`
	TimelineEvents         []MedicalTimelineEvent `json:"timeline_events,omitempty" gorm:"foreignKey:MedicalRecordID"`
	Profile                *PatientMedicalProfile `json:"profile,omitempty" gorm:"foreignKey:MedicalRecordID"`
	SurgicalHistories      []SurgicalHistory      `json:"surgical_histories,omitempty" gorm:"foreignKey:MedicalRecordID"`
	FamilyMedicalHistories []FamilyMedicalHistory `json:"family_medical_histories,omitempty" gorm:"foreignKey:MedicalRecordID"`
	RegularTreatments      []RegularTreatment     `json:"regular_treatments,omitempty" gorm:"foreignKey:MedicalRecordID"`
	Vaccinations           []Vaccination          `json:"vaccinations,omitempty" gorm:"foreignKey:MedicalRecordID"`
	Disabilities           []Disability           `json:"disabilities,omitempty" gorm:"foreignKey:MedicalRecordID"`
	Lifestyle              *Lifestyle             `json:"lifestyle,omitempty" gorm:"foreignKey:MedicalRecordID"`
	MedicalDevices         []MedicalDevice        `json:"medical_devices,omitempty" gorm:"foreignKey:MedicalRecordID"`
	Documents              []MedicalDocument      `json:"documents,omitempty" gorm:"foreignKey:MedicalRecordID"`
}

type MedicalAlert struct {
	ID              uint `json:"id" gorm:"primaryKey"`
	MedicalRecordID uint `json:"medical_record_id" gorm:"not null;index"`
	PatientID       uint `json:"patient_id" gorm:"not null;index"`

	Type        string `json:"type" gorm:"not null"` // allergy, chronic_disease, pregnancy, anticoagulant, critical_result
	Title       string `json:"title" gorm:"not null"`
	Description string `json:"description"`
	Severity    string `json:"severity" gorm:"default:medium"` // low, medium, high, critical
	IsActive    bool   `json:"is_active" gorm:"default:true"`

	CreatedBy uint      `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Allergy struct {
	ID              uint `json:"id" gorm:"primaryKey"`
	MedicalRecordID uint `json:"medical_record_id" gorm:"not null;index"`
	PatientID       uint `json:"patient_id" gorm:"not null;index"`

	AllergenType string `json:"allergen_type" gorm:"not null"` // medication, food, product, other
	AllergenName string `json:"allergen_name" gorm:"not null"`
	Reaction     string `json:"reaction"`
	Severity     string `json:"severity" gorm:"default:medium"` // low, medium, high, critical
	Comment      string `json:"comment"`
	IsActive     bool   `json:"is_active" gorm:"default:true"`

	CreatedBy uint      `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type MedicalHistory struct {
	ID              uint `json:"id" gorm:"primaryKey"`
	MedicalRecordID uint `json:"medical_record_id" gorm:"not null;index"`
	PatientID       uint `json:"patient_id" gorm:"not null;index"`

	Type        string     `json:"type" gorm:"not null"` // medical, surgical, family, chronic, gyneco, pediatric
	Title       string     `json:"title" gorm:"not null"`
	Description string     `json:"description"`
	StartDate   *time.Time `json:"start_date"`
	EndDate     *time.Time `json:"end_date"`
	Status      string     `json:"status" gorm:"default:active"` // active, resolved, unknown
	Severity    string     `json:"severity" gorm:"default:medium"`
	Comment     string     `json:"comment"`

	CreatedBy uint      `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type VitalSign struct {
	ID              uint  `json:"id" gorm:"primaryKey"`
	MedicalRecordID uint  `json:"medical_record_id" gorm:"not null;index"`
	PatientID       uint  `json:"patient_id" gorm:"not null;index"`
	ConsultationID  *uint `json:"consultation_id" gorm:"index"`

	WeightKg             *float64 `json:"weight_kg"`
	HeightCm             *float64 `json:"height_cm"`
	BMI                  *float64 `json:"bmi"`
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

	MeasuredBy uint      `json:"measured_by"`
	MeasuredAt time.Time `json:"measured_at"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type MedicalTimelineEvent struct {
	ID              uint `json:"id" gorm:"primaryKey"`
	MedicalRecordID uint `json:"medical_record_id" gorm:"not null;index"`
	PatientID       uint `json:"patient_id" gorm:"not null;index"`

	EventType string `json:"event_type" gorm:"not null;index"`
	Category  string `json:"category" gorm:"not null;index"`

	Title       string `json:"title" gorm:"not null"`
	Description string `json:"description"`

	DepartmentID *uint `json:"department_id" gorm:"index"`

	ReferenceType string `json:"reference_type" gorm:"index"`
	ReferenceID   *uint  `json:"reference_id" gorm:"index"`

	Severity string `json:"severity" gorm:"default:info"`

	EventDate time.Time `json:"event_date" gorm:"not null;index"`
	CreatedBy uint      `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
}

type PatientMedicalProfile struct {
	ID              uint `json:"id" gorm:"primaryKey"`
	MedicalRecordID uint `json:"medical_record_id" gorm:"not null;uniqueIndex"`
	PatientID       uint `json:"patient_id" gorm:"not null;uniqueIndex"`

	Email         string `json:"email" gorm:"size:150"`
	Address       string `json:"address" gorm:"size:255"`
	MaritalStatus string `json:"marital_status" gorm:"size:50"`
	Profession    string `json:"profession" gorm:"size:150"`
	PhotoURL      string `json:"photo_url" gorm:"size:500"`

	EmergencyContactFirstName    string `json:"emergency_contact_first_name" gorm:"size:100"`
	EmergencyContactLastName     string `json:"emergency_contact_last_name" gorm:"size:100"`
	EmergencyContactRelationship string `json:"emergency_contact_relationship" gorm:"size:100"`
	EmergencyContactPhone        string `json:"emergency_contact_phone" gorm:"size:50"`

	LegalGuardianName         string `json:"legal_guardian_name" gorm:"size:200"`
	LegalGuardianRelationship string `json:"legal_guardian_relationship" gorm:"size:100"`
	LegalGuardianPhone        string `json:"legal_guardian_phone" gorm:"size:50"`
	LegalGuardianAddress      string `json:"legal_guardian_address" gorm:"size:255"`

	InsuranceName        string `json:"insurance_name" gorm:"size:150"`
	MutualName           string `json:"mutual_name" gorm:"size:150"`
	InsuranceNumber      string `json:"insurance_number" gorm:"size:100"`
	CoverageOrganization string `json:"coverage_organization" gorm:"size:150"`

	BloodGroup string `json:"blood_group" gorm:"size:5"`
	Rhesus     string `json:"rhesus" gorm:"size:10"`

	UpdatedBy uint      `json:"updated_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type SurgicalHistory struct {
	ID              uint `json:"id" gorm:"primaryKey"`
	MedicalRecordID uint `json:"medical_record_id" gorm:"not null;index"`
	PatientID       uint `json:"patient_id" gorm:"not null;index"`

	ProcedureName string     `json:"procedure_name" gorm:"not null;size:200"`
	ProcedureDate *time.Time `json:"procedure_date"`
	Facility      string     `json:"facility" gorm:"size:200"`
	Complications string     `json:"complications" gorm:"type:text"`
	Comment       string     `json:"comment" gorm:"type:text"`

	CreatedBy uint      `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type FamilyMedicalHistory struct {
	ID              uint `json:"id" gorm:"primaryKey"`
	MedicalRecordID uint `json:"medical_record_id" gorm:"not null;index"`
	PatientID       uint `json:"patient_id" gorm:"not null;index"`

	Disease      string `json:"disease" gorm:"not null;size:200"`
	Relationship string `json:"relationship" gorm:"size:100"`
	Comment      string `json:"comment" gorm:"type:text"`

	CreatedBy uint      `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type RegularTreatment struct {
	ID              uint `json:"id" gorm:"primaryKey"`
	MedicalRecordID uint `json:"medical_record_id" gorm:"not null;index"`
	PatientID       uint `json:"patient_id" gorm:"not null;index"`

	MedicationName string     `json:"medication_name" gorm:"not null;size:200"`
	Dosage         string     `json:"dosage" gorm:"size:100"`
	Frequency      string     `json:"frequency" gorm:"size:100"`
	StartDate      *time.Time `json:"start_date"`
	Prescriber     string     `json:"prescriber" gorm:"size:200"`
	IsActive       bool       `json:"is_active" gorm:"default:true"`

	CreatedBy uint      `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Vaccination struct {
	ID              uint `json:"id" gorm:"primaryKey"`
	MedicalRecordID uint `json:"medical_record_id" gorm:"not null;index"`
	PatientID       uint `json:"patient_id" gorm:"not null;index"`

	VaccineName     string     `json:"vaccine_name" gorm:"not null;size:200"`
	Dose            string     `json:"dose" gorm:"size:100"`
	VaccinationDate *time.Time `json:"vaccination_date"`
	NextBoosterDate *time.Time `json:"next_booster_date"`
	Status          string     `json:"status" gorm:"size:50"`

	CreatedBy uint      `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Disability struct {
	ID              uint `json:"id" gorm:"primaryKey"`
	MedicalRecordID uint `json:"medical_record_id" gorm:"not null;index"`
	PatientID       uint `json:"patient_id" gorm:"not null;index"`

	Type         string `json:"type" gorm:"not null;size:150"`
	Level        string `json:"level" gorm:"size:100"`
	SpecialNeeds string `json:"special_needs" gorm:"type:text"`

	CreatedBy uint      `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Lifestyle struct {
	ID              uint `json:"id" gorm:"primaryKey"`
	MedicalRecordID uint `json:"medical_record_id" gorm:"not null;uniqueIndex"`
	PatientID       uint `json:"patient_id" gorm:"not null;uniqueIndex"`

	Tobacco          string `json:"tobacco" gorm:"size:100"`
	Alcohol          string `json:"alcohol" gorm:"size:100"`
	PhysicalActivity string `json:"physical_activity" gorm:"size:150"`
	Diet             string `json:"diet" gorm:"type:text"`

	UpdatedBy uint      `json:"updated_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type MedicalDevice struct {
	ID              uint `json:"id" gorm:"primaryKey"`
	MedicalRecordID uint `json:"medical_record_id" gorm:"not null;index"`
	PatientID       uint `json:"patient_id" gorm:"not null;index"`

	Type             string     `json:"type" gorm:"not null;size:100"`
	Name             string     `json:"name" gorm:"size:200"`
	Reference        string     `json:"reference" gorm:"size:150"`
	ImplantationDate *time.Time `json:"implantation_date"`
	Comment          string     `json:"comment" gorm:"type:text"`
	IsActive         bool       `json:"is_active" gorm:"default:true"`

	CreatedBy uint      `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type MedicalDocument struct {
	ID              uint  `json:"id" gorm:"primaryKey"`
	MedicalRecordID uint  `json:"medical_record_id" gorm:"not null;index"`
	PatientID       uint  `json:"patient_id" gorm:"not null;index"`
	ConsultationID  *uint `json:"consultation_id" gorm:"index"`

	Type        string `json:"type" gorm:"not null;size:100"`
	Label       string `json:"label" gorm:"not null;size:255"`
	FileName    string `json:"file_name" gorm:"size:255"`
	MimeType    string `json:"mime_type" gorm:"size:100"`
	FileURL     string `json:"file_url" gorm:"size:500"`
	Description string `json:"description" gorm:"type:text"`

	UploadedBy uint      `json:"uploaded_by"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}
