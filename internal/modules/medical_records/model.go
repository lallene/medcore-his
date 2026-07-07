package medical_records

import "time"

type MedicalRecord struct {
	ID           uint      `json:"id" gorm:"primaryKey"`
	PatientID    uint      `json:"patient_id" gorm:"not null;uniqueIndex"`
	RecordNumber string    `json:"record_number" gorm:"uniqueIndex;not null"`
	Status       string    `json:"status" gorm:"default:active"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`

	Alerts           []MedicalAlert   `json:"alerts,omitempty" gorm:"foreignKey:MedicalRecordID"`
	Allergies        []Allergy        `json:"allergies,omitempty" gorm:"foreignKey:MedicalRecordID"`
	MedicalHistories []MedicalHistory `json:"medical_histories,omitempty" gorm:"foreignKey:MedicalRecordID"`
	VitalSigns       []VitalSign      `json:"vital_signs,omitempty" gorm:"foreignKey:MedicalRecordID"`
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
