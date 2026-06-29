package voucher

import coremapper "github.com/lallene/medcore-his/backend/internal/core/mapper"

func ToResponse(item *InsuranceVoucher) VoucherResponse {
	if item == nil {
		return VoucherResponse{}
	}

	issueDate := ""
	if item.IssueDate != nil {
		issueDate = item.IssueDate.Format("2006-01-02")
	}

	return VoucherResponse{
		ID:            item.ID,
		UUID:          item.UUID,
		VoucherNumber: item.VoucherNumber,
		CoverageID:    item.CoverageID,
		PatientID:     item.PatientID,
		CompanyID:     item.CompanyID,
		GuarantorID:   item.GuarantorID,
		Status:        item.Status,
		IssueDate:     issueDate,
		GrossAmount:   item.GrossAmount,
		CoveredAmount: item.CoveredAmount,
		PatientAmount: item.PatientAmount,
		CoverageRate:  item.CoverageRate,
		Notes:         item.Notes,
	}
}

func ToSummary(item InsuranceVoucher) VoucherSummary {
	return VoucherSummary{
		ID:            item.ID,
		VoucherNumber: item.VoucherNumber,
		PatientID:     item.PatientID,
		Status:        item.Status,
		GrossAmount:   item.GrossAmount,
		CoveredAmount: item.CoveredAmount,
		PatientAmount: item.PatientAmount,
	}
}

func ToSummaryList(items []InsuranceVoucher) []VoucherSummary {
	return coremapper.MapSlice(items, ToSummary)
}
