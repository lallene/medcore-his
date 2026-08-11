package imaging

import (
	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
)

func RegisterRoutes(r *gin.RouterGroup, h *Handler) {
	g := r.Group("/imaging")
	g.GET("/orders", rbac.Permission("imaging.read"), h.List)
	g.GET("/orders/:id", rbac.Permission("imaging.read"), h.Get)
	g.POST("/orders/:id/schedule", rbac.Permission("imaging.schedule"), h.Schedule)
	g.POST("/orders/:id/start", rbac.Permission("imaging.perform"), h.Start)
	g.PUT("/orders/:id/report", rbac.Permission("imaging.report.write"), h.Report)
	g.POST("/orders/:id/validate", rbac.Permission("imaging.validate"), h.Validate)
	g.POST("/orders/:id/cancel", rbac.Permission("imaging.cancel"), h.Cancel)
	r.GET("/patients/:id/imaging-orders", rbac.Permission("imaging.read"), h.List)
}
