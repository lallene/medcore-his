package medical_records

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
)

// LOT30-A′: clinical medical-record readers keep clinical data but not insurance member numbers.
func TestLOT30APrime_MedicalRecordInsuranceRedaction(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := func(perms []string) *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		rbac.SetUser(c, 1, "staff", perms)
		return c
	}
	if canReadInsuranceDetails(ctx([]string{"medical_records.read", "patients.360.read"})) {
		t.Fatal("medical_records.read must not grant insurance details")
	}
	if !canReadInsuranceDetails(ctx([]string{"insurance.coverage.read"})) || !canReadInsuranceDetails(ctx([]string{"*"})) {
		t.Fatal("insurance.coverage.read / admin must see insurance details")
	}
	rec := &MedicalRecord{Profile: &PatientMedicalProfile{InsuranceNumber: "INS-42", InsuranceName: "Assureur"}}
	redactRecordInsurance(rec)
	if rec.Profile.InsuranceNumber != "" || rec.Profile.InsuranceName != "Assureur" {
		t.Fatalf("only the member number is redacted: %#v", rec.Profile)
	}
	redactRecordInsurance(&MedicalRecord{}) // nil profile is safe
}
