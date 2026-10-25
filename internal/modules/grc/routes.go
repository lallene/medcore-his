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
}
