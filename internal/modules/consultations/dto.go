package consultations

type CreateConsultationRequest struct {
	PatientID  uint   `json:"patientId" binding:"required"`
	DoctorName string `json:"doctorName" binding:"required"`
	Service    string `json:"service"`

	ReasonIDs     []uint                `json:"reasonIds"`
	Prescriptions []PrescriptionRequest `json:"prescriptions"`

	HospitalizationRequired bool   `json:"hospitalizationRequired"`
	HospitalizationReason   string `json:"hospitalizationReason"`
	HospitalizationType     string `json:"hospitalizationType"`
	HospitalizationDuration int    `json:"hospitalizationDuration"`

	Vitals ConsultationVitalsRequest `json:"vitals"`

	Diagnosis    string `json:"diagnosis"`
	Observations string `json:"observations"`
	Treatment    string `json:"treatment"`

	SickLeaveRequired      bool                           `json:"sickLeaveRequired"`
	SickLeaveDays          int                            `json:"sickLeaveDays"`
	Antecedent             *AntecedentRequest             `json:"antecedent"`
	PhysicalExams          []PhysicalExamRequest          `json:"physicalExams"`
	AdministeredTreatments []AdministeredTreatmentRequest `json:"administeredTreatments"`

	ExamIDs []uint `json:"examIds"`
}

type ConsultationVitalsRequest struct {
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

type CreateReferenceRequest struct {
	Code     string `json:"code" binding:"required"`
	Name     string `json:"name" binding:"required"`
	Category string `json:"category"`
}

type UpdateReferenceRequest struct {
	Code     string `json:"code"`
	Name     string `json:"name"`
	Category string `json:"category"`
	IsActive *bool  `json:"isActive"`
}

type UpdateConsultationStatusRequest struct {
	Status             string `json:"status" binding:"required"`
	CancellationReason string `json:"cancellationReason"`
}

type UpdateConsultationRequest struct {
	DoctorName *string `json:"doctorName"`
	Service    *string `json:"service"`

	ReasonIDs     *[]uint                `json:"reasonIds"`
	ExamIDs       *[]uint                `json:"examIds"`
	Prescriptions *[]PrescriptionRequest `json:"prescriptions"`

	HospitalizationRequired *bool   `json:"hospitalizationRequired"`
	HospitalizationReason   *string `json:"hospitalizationReason"`
	HospitalizationType     *string `json:"hospitalizationType"`
	HospitalizationDuration *int    `json:"hospitalizationDuration"`

	Vitals *ConsultationVitalsRequest `json:"vitals"`

	Diagnosis    *string `json:"diagnosis"`
	Observations *string `json:"observations"`
	Treatment    *string `json:"treatment"`

	Antecedent             *AntecedentRequest              `json:"antecedent"`
	PhysicalExams          *[]PhysicalExamRequest          `json:"physicalExams"`
	AdministeredTreatments *[]AdministeredTreatmentRequest `json:"administeredTreatments"`

	SickLeaveRequired *bool `json:"sickLeaveRequired"`
	SickLeaveDays     *int  `json:"sickLeaveDays"`
}

type Patient360Response struct {
	PatientID     uint                  `json:"patientId"`
	Consultations []Consultation        `json:"consultations"`
	Documents     []PatientDocumentItem `json:"documents"`
}

type PatientDocumentItem struct {
	ConsultationID uint   `json:"consultationId"`
	Type           string `json:"type"`
	Label          string `json:"label"`
	URL            string `json:"url"`
}

type ConsultationPrescriptionRequest struct {
	PresentationID uint `json:"presentationId" binding:"required"`

	Quantity float64 `json:"quantity" binding:"required,gt=0"`

	Duration string `json:"duration" binding:"required"`

	Instructions string `json:"instructions" binding:"required"`
}

type CreateDispensationRequest struct {
	PresentationID uint    `json:"presentationId" binding:"required"`
	Quantity       float64 `json:"quantity" binding:"required,gt=0"`

	PatientID *uint `json:"patientId"`

	PrescriptionID *uint `json:"prescriptionId"`

	Notes string `json:"notes"`
}

type PrescriptionRequest struct {
	PresentationID uint    `json:"presentationId" binding:"required"`
	Quantity       float64 `json:"quantity" binding:"required,gt=0"`
	Duration       string  `json:"duration" binding:"required"`
	Instructions   string  `json:"instructions" binding:"required"`
}

type AntecedentRequest struct {
	PreviousMedication string `json:"previousMedication" example:"Metformine 500 mg"`
	HasHTA             *bool  `json:"hasHta" example:"false"`
	HasDiabetes        *bool  `json:"hasDiabetes" example:"true"`
	OtherMedical       string `json:"otherMedical" example:"Diabète de type 2 connu"`
	SurgicalHistory    string `json:"surgicalHistory" example:"Appendicectomie en 2020"`

	GynecoObstetricHistory string `json:"gynecoObstetricHistory" example:"G2P2"`
	DDR                    string `json:"ddr" example:"2026-06-10"`
	PregnancyOngoing       *bool  `json:"pregnancyOngoing" example:"false"`

	Tobacco *bool `json:"tobacco" example:"false"`
	Alcohol *bool `json:"alcohol" example:"false"`

	VisitType string `json:"visitType" example:"CONTROLE"`
}

type PhysicalExamRequest struct {
	Organ string `json:"organ" binding:"required" example:"Appareil cardiovasculaire"`

	Observation string `json:"observation" example:"Rythme régulier, absence de souffle"`
}

type AdministeredTreatmentRequest struct {
	PresentationID uint `json:"presentationId" binding:"required" example:"1"`

	Quantity float64 `json:"quantity" binding:"required,gt=0" example:"2"`

	Instructions string `json:"instructions" example:"2 comprimés administrés sur place"`
}
