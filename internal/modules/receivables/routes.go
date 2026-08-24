package receivables

import (
	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
)

func RegisterRoutes(r *gin.RouterGroup, h *Handler) {
	g := r.Group("/receivables")
	g.GET("", rbac.Permission("receivables.read"), h.List)
	g.GET("/kpis", rbac.Permission("receivables.read"), h.KPIs)
	g.GET("/:id", rbac.Permission("receivables.read"), h.Get)
	g.PUT("/:id/due-date", rbac.Permission("receivables.due_date.manage"), h.Due)
	g.POST("/:id/follow-ups", rbac.Permission("receivables.followup.create"), h.Follow)
	r.GET("/patients/:id/receivables", rbac.Permission("receivables.read"), h.Patient)
}
