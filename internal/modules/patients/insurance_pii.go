package patients

import (
	"github.com/gin-gonic/gin"

	"github.com/lallene/medcore-his/backend/internal/core/rbac"
)

// InsuranceDetailsPermission gates the legacy patient insurance details (coverage rate,
// member number). patients:read keeps only the minimal isAssure indicator (LOT30-A′).
const InsuranceDetailsPermission = "insurance.coverage.read"

func canReadInsuranceDetails(c *gin.Context) bool {
	raw, _ := c.Get(rbac.ContextPermissions)
	perms, _ := raw.([]string)
	return rbac.HasAnyPermission(perms, InsuranceDetailsPermission)
}

func applyInsuranceVisibility(r *PatientResponse, visible bool) {
	r.InsuranceDetailsVisible = visible
	if !visible {
		r.TauxCouverture = 0
		r.MatriculeAssure = ""
	}
}

func applySummaryInsuranceVisibility(rows []PatientSummary, visible bool) {
	for i := range rows {
		rows[i].InsuranceDetailsVisible = visible
		if !visible {
			rows[i].TauxCouverture = 0
			rows[i].MatriculeAssure = ""
		}
	}
}
