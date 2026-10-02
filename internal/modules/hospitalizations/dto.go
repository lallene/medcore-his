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
	ServiceID   *uint
	From, To    *time.Time
	// AssignedServiceIDs, when non-nil, restricts rows to those service_ids (server-authoritative).
	// Nil means unrestricted (*). Empty non-nil means fail-closed (no rows).
	AssignedServiceIDs []uint
	ServiceScopeActive bool
}

type ListResult struct {
	Data       []Hospitalization
	Page       int
	Limit      int
	Total      int64
	TotalPages int
}
