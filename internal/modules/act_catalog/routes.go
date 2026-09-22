package act_catalog

import (
	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
)

func RegisterRoutes(r *gin.RouterGroup, h *Handler) {
	g := r.Group("/act-catalog")
	g.GET("", rbac.Permission("act_catalog.read"), h.List)
	g.GET("/:id", rbac.Permission("act_catalog.read"), h.Get)
	g.POST("", rbac.Permission("act_catalog.manage"), h.Create)
	g.PUT("/:id", rbac.Permission("act_catalog.manage"), h.Update)
}
