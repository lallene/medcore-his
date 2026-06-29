package company

import coremapper "github.com/lallene/medcore-his/backend/internal/core/mapper"

func ToResponse(item *InsuranceCompany) CompanyResponse {
	if item == nil {
		return CompanyResponse{}
	}

	return CompanyResponse{
		ID:          item.ID,
		UUID:        item.UUID,
		Code:        item.Code,
		Name:        item.Name,
		Description: item.Description,
		Phone:       item.Phone,
		Email:       item.Email,
		Address:     item.Address,
		City:        item.City,
		Country:     item.Country,
		IsActive:    item.IsActive,
	}
}

func ToSummary(item InsuranceCompany) CompanySummary {
	return CompanySummary{
		ID:       item.ID,
		Code:     item.Code,
		Name:     item.Name,
		IsActive: item.IsActive,
	}
}

func ToSummaryList(items []InsuranceCompany) []CompanySummary {
	return coremapper.MapSlice(items, ToSummary)
}
