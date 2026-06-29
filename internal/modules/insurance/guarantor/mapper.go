package guarantor

import coremapper "github.com/lallene/medcore-his/backend/internal/core/mapper"

func ToResponse(item *InsuranceGuarantor) GuarantorResponse {
	if item == nil {
		return GuarantorResponse{}
	}

	return GuarantorResponse{
		ID:                  item.ID,
		UUID:                item.UUID,
		CompanyID:           item.CompanyID,
		CompanyName:         item.Company.Name,
		Code:                item.Code,
		Name:                item.Name,
		Description:         item.Description,
		DefaultCoverageRate: item.DefaultCoverageRate,
		PaymentDelayDays:    item.PaymentDelayDays,
		IsActive:            item.IsActive,
	}
}

func ToSummary(item InsuranceGuarantor) GuarantorSummary {
	return GuarantorSummary{
		ID:                  item.ID,
		CompanyID:           item.CompanyID,
		CompanyName:         item.Company.Name,
		Code:                item.Code,
		Name:                item.Name,
		DefaultCoverageRate: item.DefaultCoverageRate,
		IsActive:            item.IsActive,
	}
}

func ToSummaryList(items []InsuranceGuarantor) []GuarantorSummary {
	return coremapper.MapSlice(items, ToSummary)
}
