package medical_records

import (
	"github.com/gin-gonic/gin"

	"github.com/lallene/medcore-his/backend/internal/core/rbac"
)

// insuranceDetailsPermission gates insurance member numbers / coverage rate in clinical
// payloads; medical_records.read alone keeps the minimal "is insured" indicator (LOT30-A′).
const insuranceDetailsPermission = "insurance.coverage.read"

func canReadInsuranceDetails(c *gin.Context) bool {
	raw, _ := c.Get(rbac.ContextPermissions)
	perms, _ := raw.([]string)
	return rbac.HasAnyPermission(perms, insuranceDetailsPermission)
}

func redactProfileInsurance(p *PatientMedicalProfile) {
	if p != nil {
		p.InsuranceNumber = ""
	}
}

func redactRecordInsurance(r *MedicalRecord) {
	if r != nil {
		redactProfileInsurance(r.Profile)
	}
}
