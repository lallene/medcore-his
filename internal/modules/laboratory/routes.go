package laboratory

import (
	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
)

func RegisterRoutes(r *gin.RouterGroup, h *Handler) {
	g := r.Group("/laboratory")
	g.GET("/orders", rbac.Permission("laboratory.read"), h.List)
	g.GET("/orders/:id", rbac.Permission("laboratory.read"), h.Get)
	g.POST("/orders/:id/sample-pending", rbac.Permission("laboratory.collect"), h.PrepareSample)
	g.POST("/orders/:id/collect", rbac.Permission("laboratory.collect"), h.Collect)
	g.POST("/orders/:id/start", rbac.Permission("laboratory.process"), h.Start)
	g.PUT("/orders/:id/results", rbac.Permission("laboratory.result.write"), h.Results)
	g.POST("/orders/:id/validate", rbac.Permission("laboratory.validate"), h.Validate)
	g.POST("/orders/:id/cancel", rbac.Permission("laboratory.cancel"), h.Cancel)
	r.GET("/patients/:id/laboratory-orders", rbac.Permission("laboratory.read"), h.List)
}
