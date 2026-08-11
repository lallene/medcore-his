package authorization

import (
	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
)

func RegisterRoutes(router *gin.RouterGroup, h *Handler) {
	g := router.Group("/insurance/authorizations")
	g.GET("", rbac.Permission("insurance.authorization.read"), h.List)
	g.GET("/:id", rbac.Permission("insurance.authorization.read"), h.Find)
	g.POST("", rbac.Permission("insurance.authorization.create"), h.Create)
	g.PUT("/:id", rbac.Permission("insurance.authorization.create"), h.Update)
	g.POST("/:id/submit", rbac.Permission("insurance.authorization.submit"), h.Submit)
	g.POST("/:id/pending", rbac.Permission("insurance.authorization.submit"), h.Pending)
	g.POST("/:id/decision", rbac.Permission("insurance.authorization.decide"), h.Decide)
	g.POST("/:id/cancel", rbac.Permission("insurance.authorization.cancel"), h.Cancel)
}
