package insurance_receivables

import (
	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
)

func RegisterRoutes(r *gin.RouterGroup, h *Handler) {
	g := r.Group("/insurance-receivables")
	g.GET("", rbac.Permission("insurance_receivables.read"), h.List)
	g.GET("/kpis", rbac.Permission("insurance_receivables.read"), h.KPIs)
	g.GET("/companies", rbac.Permission("insurance_receivables.read"), h.Companies)
	g.GET("/:id", rbac.Permission("insurance_receivables.read"), h.Receivable)
	g.PUT("/:id/due-date", rbac.Permission("insurance_receivables.followup"), h.Due)
	g.POST("/:id/followups", rbac.Permission("insurance_receivables.followup"), h.FollowUp)
	g.GET("/settlements", rbac.Permission("insurance_settlements.read"), h.Settlements)
	g.POST("/settlements", rbac.Permission("insurance_settlements.create"), h.CreateSettlement)
	g.GET("/settlements/:id", rbac.Permission("insurance_settlements.read"), h.Settlement)
	g.POST("/settlements/:id/allocations", rbac.Permission("insurance_settlements.allocate"), h.Allocate)
	g.POST("/settlements/:id/post", rbac.Permission("insurance_settlements.allocate"), h.Post)
	g.POST("/settlements/:id/cancel", rbac.Permission("insurance_settlements.create"), h.Cancel)
	g.GET("/batches", rbac.Permission("insurance_batches.read"), h.Batches)
	g.POST("/batches", rbac.Permission("insurance_batches.create"), h.CreateBatch)
	g.GET("/batches/:id", rbac.Permission("insurance_batches.read"), h.Batch)
	g.POST("/batches/:id/submit", rbac.Permission("insurance_batches.submit"), h.SubmitBatch)
	r.GET("/patients/:id/insurance-receivables", rbac.Permission("insurance_receivables.read"), h.Patient)
}
