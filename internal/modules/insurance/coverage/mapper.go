package coverage

import (
	coremapper "github.com/lallene/medcore-his/backend/internal/core/mapper"
)

func patientName(item *PatientCoverage) string {
	if item == nil {
		return ""
	}

	return item.Patient.Prenoms + " " + item.Patient.Nom
}

func dateToString(value any) string {
	return ""
}

func ToResponse(item *PatientCoverage) CoverageResponse {
	if item == nil {
		return CoverageResponse{}
	}

	validFrom := ""
	validTo := ""

	if item.ValidFrom != nil {
		validFrom = item.ValidFrom.Format("2006-01-02")
	}

	if item.ValidTo != nil {
		validTo = item.ValidTo.Format("2006-01-02")
	}

	return CoverageResponse{
		ID:            item.ID,
		UUID:          item.UUID,
		PatientID:     item.PatientID,
		PatientName:   patientName(item),
		CompanyID:     item.CompanyID,
		CompanyName:   item.Company.Name,
		GuarantorID:   item.GuarantorID,
		GuarantorName: item.Guarantor.Name,
		MemberNumber:  item.MemberNumber,
		Subscriber:    item.Subscriber,
		Beneficiary:   item.Beneficiary,
		CoverageRate:  item.CoverageRate,
		ValidFrom:     validFrom,
		ValidTo:       validTo,
		IsPrincipal:   item.IsPrincipal,
		IsActive:      item.IsActive,
	}
}

func ToSummary(item PatientCoverage) CoverageSummary {
	validFrom := ""
	validTo := ""

	if item.ValidFrom != nil {
		validFrom = item.ValidFrom.Format("2006-01-02")
	}

	if item.ValidTo != nil {
		validTo = item.ValidTo.Format("2006-01-02")
	}

	return CoverageSummary{
		ID:            item.ID,
		PatientID:     item.PatientID,
		PatientName:   item.Patient.Prenoms + " " + item.Patient.Nom,
		CompanyName:   item.Company.Name,
		GuarantorName: item.Guarantor.Name,
		MemberNumber:  item.MemberNumber,
		Subscriber:    item.Subscriber,
		Beneficiary:   item.Beneficiary,
		CoverageRate:  item.CoverageRate,
		ValidFrom:     validFrom,
		ValidTo:       validTo,
		IsPrincipal:   item.IsPrincipal,
		IsActive:      item.IsActive,
	}
}

func ToSummaryList(items []PatientCoverage) []CoverageSummary {
	return coremapper.MapSlice(items, ToSummary)
}
