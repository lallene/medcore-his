package hospitalizations

import "time"

type CreateRequest struct {
	PatientID            uint   `json:"patientId" binding:"required"`
	SourceConsultationID uint   `json:"sourceConsultationId" binding:"required"`
	AdmissionDiagnosis   string `json:"admissionDiagnosis"`
	ExpectedDischargeAt  string `json:"expectedDischargeAt"`
}

type AdmitRequest struct {
	AdmittedAt         string `json:"admittedAt"`
	AdmissionDiagnosis string `json:"admissionDiagnosis"`
}

type DischargeRequest struct {
	DischargedAt       string `json:"dischargedAt"`
	DischargeDiagnosis string `json:"dischargeDiagnosis" binding:"required"`
	DischargeSummary   string `json:"dischargeSummary" binding:"required"`
}

type ListFilter struct {
	Page, Limit int
	PatientID   *uint
	Status      string
	Department  string
	From, To    *time.Time
}

type ListResult struct {
	Data       []Hospitalization
	Page       int
	Limit      int
	Total      int64
	TotalPages int
}
