package qa

import (
	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
)

func RegisterRoutes(r *gin.RouterGroup, h *Handler) {
	g := r.Group("/qa")
	g.GET("/campaigns", rbac.Permission("qa.read"), h.List)
	g.GET("/campaigns/:id", rbac.Permission("qa.read"), h.Get)
	g.GET("/campaigns/:id/results", rbac.Permission("qa.audit.read"), h.Results)
	g.GET("/kpis", rbac.Permission("qa.read"), h.KPIs)
}
