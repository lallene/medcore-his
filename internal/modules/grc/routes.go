package grc

import (
	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
)

func RegisterRoutes(r *gin.RouterGroup, h *Handler) {
	g := r.Group("/grc")

	assets := g.Group("/assets")
	assets.GET("", rbac.Permission("grc.asset.read"), h.ListAssets)
	assets.GET("/:id", rbac.Permission("grc.asset.read"), h.GetAsset)
	assets.POST("", rbac.Permission("grc.asset.manage"), h.CreateAsset)
	assets.PUT("/:id", rbac.Permission("grc.asset.manage"), h.UpdateAsset)

	risks := g.Group("/risks")
	risks.GET("", rbac.Permission("grc.risk.read"), h.ListRisks)
	risks.GET("/:id", rbac.Permission("grc.risk.read"), h.GetRisk)
	risks.POST("", rbac.Permission("grc.risk.create"), h.CreateRisk)
	risks.PUT("/:id", rbac.Permission("grc.risk.update"), h.UpdateRisk)

	frameworks := g.Group("/frameworks")
	frameworks.GET("", rbac.Permission("grc.framework.read"), h.ListFrameworks)
	frameworks.GET("/:id", rbac.Permission("grc.framework.read"), h.GetFramework)
	frameworks.POST("", rbac.Permission("grc.framework.manage"), h.CreateFramework)
	frameworks.PUT("/:id", rbac.Permission("grc.framework.manage"), h.UpdateFramework)

	requirements := g.Group("/requirements")
	requirements.GET("", rbac.Permission("grc.requirement.read"), h.ListRequirements)
	requirements.GET("/:id", rbac.Permission("grc.requirement.read"), h.GetRequirement)
	requirements.POST("", rbac.Permission("grc.requirement.manage"), h.CreateRequirement)
	requirements.PUT("/:id", rbac.Permission("grc.requirement.manage"), h.UpdateRequirement)

	controls := g.Group("/controls")
	controls.GET("", rbac.Permission("grc.control.read"), h.ListControls)
	controls.GET("/:id", rbac.Permission("grc.control.read"), h.GetControl)
	controls.POST("", rbac.Permission("grc.control.manage"), h.CreateControl)
	controls.PUT("/:id", rbac.Permission("grc.control.manage"), h.UpdateControl)

	implementations := g.Group("/control-implementations")
	implementations.GET("", rbac.Permission("grc.control.implementation.read"), h.ListControlImplementations)
	implementations.GET("/:id", rbac.Permission("grc.control.implementation.read"), h.GetControlImplementation)
	implementations.POST("", rbac.Permission("grc.control.implementation.manage"), h.CreateControlImplementation)
	implementations.PUT("/:id", rbac.Permission("grc.control.implementation.manage"), h.UpdateControlImplementation)

	// Explicit relations are managed with the control permission.
	requirementControls := g.Group("/requirement-controls")
	requirementControls.POST("", rbac.Permission("grc.control.manage"), h.LinkRequirementControl)
	requirementControls.DELETE("/:requirementId/:controlId", rbac.Permission("grc.control.manage"), h.UnlinkRequirementControl)

	controlRisks := g.Group("/control-risks")
	controlRisks.POST("", rbac.Permission("grc.control.manage"), h.LinkControlRisk)
	controlRisks.DELETE("/:controlId/:riskId", rbac.Permission("grc.control.manage"), h.UnlinkControlRisk)
}
