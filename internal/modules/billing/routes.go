package billing

import (
	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
)

func RegisterRoutes(r *gin.RouterGroup, h *Handler) {
	g := r.Group("/billing")
	g.GET("/tariffs", rbac.Permission("billing.tariff.read"), h.Tariffs)
	g.POST("/tariffs", rbac.Permission("billing.tariff.manage"), h.CreateTariff)
	g.PUT("/tariffs/:id", rbac.Permission("billing.tariff.manage"), h.UpdateTariff)
	g.GET("/billable-acts", rbac.Permission("billing.read"), h.Billable)
	g.GET("/act-status", rbac.Permission("billing.read"), h.ActStatus)
	g.GET("/invoices", rbac.Permission("billing.read"), h.List)
	g.POST("/invoices", rbac.Permission("billing.create"), h.Create)
	g.GET("/invoices/:id", rbac.Permission("billing.read"), h.Get)
	g.POST("/invoices/:id/issue", rbac.Permission("billing.issue"), h.Issue)
	g.POST("/invoices/:id/payments", rbac.Permission("billing.payment.create"), h.Pay)
	g.POST("/invoices/:id/cancel", rbac.Permission("billing.cancel"), h.Cancel)
	g.GET("/kpis", rbac.Permission("billing.read"), h.KPIs)
	r.GET("/patients/:id/invoices", rbac.Permission("billing.read"), func(c *gin.Context) {
		q := c.Request.URL.Query()
		q.Set("patientId", c.Param("id"))
		c.Request.URL.RawQuery = q.Encode()
		h.List(c)
	})
}
