package voucher

import (
	"strings"
	"time"
)

type CreateVoucherRequest struct {
	CoverageID     uint    `json:"coverageId" binding:"required"`
	ConsultationID *uint   `json:"consultationId"`
	IssueDate      string  `json:"issueDate"`
	GrossAmount    float64 `json:"grossAmount" binding:"gte=0"`
	Notes          string  `json:"notes"`
}

type UpdateVoucherRequest struct {
	IssueDate   string  `json:"issueDate"`
	GrossAmount float64 `json:"grossAmount" binding:"gte=0"`
	Notes       string  `json:"notes"`
}

type WorkflowActionRequest struct {
	Action string `json:"action" binding:"required"`
	Reason string `json:"reason"`
}

type VoucherResponse struct {
	ID            uint   `json:"id"`
	UUID          string `json:"uuid"`
	VoucherNumber string `json:"voucherNumber"`

	CoverageID  uint `json:"coverageId"`
	PatientID   uint `json:"patientId"`
	CompanyID   uint `json:"companyId"`
	GuarantorID uint `json:"guarantorId"`

	PatientName string `json:"patientName"`
	CompanyName string `json:"companyName"`

	Status    string `json:"status"`
	IssueDate string `json:"issueDate,omitempty"`

	GrossAmount   float64 `json:"grossAmount"`
	CoveredAmount float64 `json:"coveredAmount"`
	PatientAmount float64 `json:"patientAmount"`
	CoverageRate  float64 `json:"coverageRate"`

	Amount float64 `json:"amount"`
	Reason string  `json:"reason"`
	Notes  string  `json:"notes"`

	CreatedAt string `json:"createdAt"`
}

type VoucherSummary struct {
	ID            uint   `json:"id"`
	VoucherNumber string `json:"voucherNumber"`

	PatientID   uint   `json:"patientId"`
	PatientName string `json:"patientName"`

	CompanyID   uint   `json:"companyId"`
	CompanyName string `json:"companyName"`

	Status string `json:"status"`

	GrossAmount   float64 `json:"grossAmount"`
	CoveredAmount float64 `json:"coveredAmount"`
	PatientAmount float64 `json:"patientAmount"`

	Amount float64 `json:"amount"`
	Reason string  `json:"reason"`
}
type InsuranceVoucherResponse struct {
	ID            uint   `json:"id"`
	VoucherNumber string `json:"voucherNumber"`

	PatientID   uint   `json:"patientId"`
	PatientName string `json:"patientName"`

	CompanyID   uint   `json:"companyId"`
	CompanyName string `json:"companyName"`

	CoverageID  uint `json:"coverageId"`
	GuarantorID uint `json:"guarantorId"`

	Status string `json:"status"`

	Reason string `json:"reason"`
	Notes  string `json:"notes"`

	GrossAmount   float64 `json:"grossAmount"`
	CoveredAmount float64 `json:"coveredAmount"`
	PatientAmount float64 `json:"patientAmount"`
	CoverageRate  float64 `json:"coverageRate"`

	Amount float64 `json:"amount"`

	CreatedAt string `json:"createdAt"`
}

func MapInsuranceVoucherResponse(v InsuranceVoucher) InsuranceVoucherResponse {
	patientName := strings.TrimSpace(v.Patient.Nom + " " + v.Patient.Prenoms)

	return InsuranceVoucherResponse{
		ID:            v.ID,
		VoucherNumber: v.VoucherNumber,

		PatientID:   v.PatientID,
		PatientName: patientName,

		CompanyID:   v.CompanyID,
		CompanyName: v.Company.Name,

		CoverageID:  v.CoverageID,
		GuarantorID: v.GuarantorID,

		Status: v.Status,

		Reason: v.Notes,
		Notes:  v.Notes,

		GrossAmount:   v.GrossAmount,
		CoveredAmount: v.CoveredAmount,
		PatientAmount: v.PatientAmount,
		CoverageRate:  v.CoverageRate,

		Amount: v.GrossAmount,

		CreatedAt: v.CreatedAt.Format(time.RFC3339),
	}
}
