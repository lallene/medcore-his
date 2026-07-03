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

	SickLeaveRequired bool `json:"sickLeaveRequired"`
	SickLeaveDays     int  `json:"sickLeaveDays"`

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

	SickLeaveRequired *bool `json:"sickLeaveRequired"`
	SickLeaveDays     *int  `json:"sickLeaveDays"`
}

type PrescriptionRequest struct {
	MedicationName string `json:"medicationName" binding:"required"`
	Dosage         string `json:"dosage"`
	Form           string `json:"form"`
	Frequency      string `json:"frequency"`
	Duration       string `json:"duration"`
	Route          string `json:"route"`
	Instructions   string `json:"instructions"`
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
