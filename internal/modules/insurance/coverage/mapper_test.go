package coverage

import (
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/insurance/company"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/guarantor"
)

func TestToSummaryPreservesPatient360CoverageFields(t *testing.T) {
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	item := PatientCoverage{
		Company:      company.InsuranceCompany{Name: "Assureur"},
		Guarantor:    guarantor.InsuranceGuarantor{Name: "Garant"},
		MemberNumber: "MEMBER-1",
		Subscriber:   "Souscripteur",
		Beneficiary:  "Bénéficiaire",
		CoverageRate: 80,
		ValidFrom:    &from,
		ValidTo:      &to,
		IsPrincipal:  true,
		IsActive:     true,
	}

	result := ToSummary(item)
	if result.CompanyName != "Assureur" || result.GuarantorName != "Garant" {
		t.Fatalf("organismes perdus: %#v", result)
	}
	if result.MemberNumber != "MEMBER-1" || result.Subscriber != "Souscripteur" || result.Beneficiary != "Bénéficiaire" {
		t.Fatalf("identifiants de couverture perdus: %#v", result)
	}
	if result.CoverageRate != 80 || result.ValidFrom != "2026-01-01" || result.ValidTo != "2026-12-31" {
		t.Fatalf("conditions de couverture perdues: %#v", result)
	}
}
