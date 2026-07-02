package voucher

import (
	"strings"
	"time"

	coremapper "github.com/lallene/medcore-his/backend/internal/core/mapper"
)

func ToResponse(item *InsuranceVoucher) VoucherResponse {
	if item == nil {
		return VoucherResponse{}
	}

	issueDate := ""
	if item.IssueDate != nil {
		issueDate = item.IssueDate.Format("2006-01-02")
	}

	patientName := strings.TrimSpace(item.Patient.Nom + " " + item.Patient.Prenoms)

	return VoucherResponse{
		ID:            item.ID,
		UUID:          item.UUID,
		VoucherNumber: item.VoucherNumber,

		CoverageID:  item.CoverageID,
		PatientID:   item.PatientID,
		CompanyID:   item.CompanyID,
		GuarantorID: item.GuarantorID,

		PatientName: patientName,
		CompanyName: item.Company.Name,

		Status:    item.Status,
		IssueDate: issueDate,

		GrossAmount:   item.GrossAmount,
		CoveredAmount: item.CoveredAmount,
		PatientAmount: item.PatientAmount,
		CoverageRate:  item.CoverageRate,

		Amount: item.GrossAmount,
		Reason: item.Notes,
		Notes:  item.Notes,

		CreatedAt: item.CreatedAt.Format(time.RFC3339),
	}
}

func ToSummary(item InsuranceVoucher) VoucherSummary {
	patientName := strings.TrimSpace(item.Patient.Nom + " " + item.Patient.Prenoms)

	return VoucherSummary{
		ID:            item.ID,
		VoucherNumber: item.VoucherNumber,

		PatientID:   item.PatientID,
		PatientName: patientName,

		CompanyID:   item.CompanyID,
		CompanyName: item.Company.Name,

		Status: item.Status,

		GrossAmount:   item.GrossAmount,
		CoveredAmount: item.CoveredAmount,
		PatientAmount: item.PatientAmount,

		Amount: item.GrossAmount,
		Reason: item.Notes,
	}
}

func ToSummaryList(items []InsuranceVoucher) []VoucherSummary {
	return coremapper.MapSlice(items, ToSummary)
}

func MapVoucherResponse(v InsuranceVoucher) InsuranceVoucherResponse {
	return InsuranceVoucherResponse{
		ID:            v.ID,
		VoucherNumber: v.VoucherNumber,

		PatientID:   v.PatientID,
		PatientName: strings.TrimSpace(v.Patient.Nom + " " + v.Patient.Prenoms),

		CompanyID:   v.CompanyID,
		CompanyName: v.Company.Name,

		CoverageID:  v.CoverageID,
		GuarantorID: v.GuarantorID,

		Status: v.Status,

		Notes: v.Notes,

		GrossAmount:   v.GrossAmount,
		CoveredAmount: v.CoveredAmount,
		PatientAmount: v.PatientAmount,
		CoverageRate:  v.CoverageRate,

		// Pour le frontend actuel
		Amount: v.GrossAmount,

		CreatedAt: v.CreatedAt.Format(time.RFC3339),
	}
}
