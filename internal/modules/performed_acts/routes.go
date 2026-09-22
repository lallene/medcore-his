package performed_acts

import (
	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
)

func RegisterRoutes(r *gin.RouterGroup, h *Handler) {
	g := r.Group("/performed-acts")
	g.GET("", rbac.Permission("performed_acts.read"), h.List)
	g.GET("/:id", rbac.Permission("performed_acts.read"), h.Get)
	g.POST("", rbac.Permission("performed_acts.create"), h.Create)
	g.POST("/:id/void", rbac.Permission("performed_acts.void"), h.Void)
}
