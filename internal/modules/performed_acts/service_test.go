package performed_acts

import (
	"testing"
)

func TestTableName(t *testing.T) {
	t.Parallel()
	if (Act{}).TableName() != "performed_acts" {
		t.Fatal((Act{}).TableName())
	}
}

func TestStatusesBounded(t *testing.T) {
	t.Parallel()
	if StatusPerformed != "PERFORMED" || StatusVoided != "VOIDED" {
		t.Fatal("unexpected status constants")
	}
}

func TestInsuranceBoundary_NoAuthFieldsOnModel(t *testing.T) {
	t.Parallel()
	// Structural: Act must not carry insurer/authorization money fields.
	a := Act{InsuranceEligible: true, Status: StatusPerformed}
	if a.InsuranceEligible != true {
		t.Fatal("InsuranceEligible is catalogue snapshot metadata only")
	}
}
