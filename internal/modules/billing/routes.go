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
	g.POST("/payments/:id/reverse", rbac.Permission("billing.payment.reverse"), h.ReversePayment)
	g.POST("/invoices/:id/cancel", rbac.Permission("billing.cancel"), h.Cancel)
	g.POST("/invoices/:id/credit-notes", rbac.Permission("billing.credit_note.create"), h.IssueCreditNote)
	g.GET("/credit-notes/:id", rbac.Permission("billing.credit_note.read"), h.GetCreditNote)
	g.POST("/invoices/:id/credit-applications", rbac.Permission("billing.credit.apply"), h.ApplyCredit)
	g.POST("/credit-applications/:id/reverse", rbac.Permission("billing.credit.apply"), h.ReverseCreditApplication)
	g.GET("/credit-summary", rbac.Permission("billing.credit.read"), h.GetCreditSummary)
	g.GET("/credit-ledger", rbac.Permission("billing.credit.read"), h.ListCreditLedger)
	g.GET("/patients/:patientId/credit-balances", rbac.Permission("billing.credit.read"), h.ListPatientCreditBalances)
	g.GET("/patients/:patientId/financial-statement", rbac.Permission("billing.statement.read"), h.GetFinancialStatement)
	g.GET("/patients/:patientId/financial-history", rbac.Permission("billing.statement.read"), h.ListFinancialHistory)
	g.GET("/refunds", rbac.Permission("billing.refund.read"), h.ListRefunds)
	g.POST("/refunds", rbac.Permission("billing.refund.request"), h.RequestRefund)
	g.GET("/refunds/:id", rbac.Permission("billing.refund.read"), h.GetRefund)
	g.POST("/refunds/:id/approve", rbac.Permission("billing.refund.approve"), h.ApproveRefund)
	g.POST("/refunds/:id/reject", rbac.Permission("billing.refund.approve"), h.RejectRefund)
	g.POST("/refunds/:id/cancel", rbac.AnyPermission("billing.refund.cancel", "billing.refund.request", "billing.refund.approve"), h.CancelRefund)
	g.POST("/refunds/:id/execute", rbac.Permission("billing.refund.execute"), h.ExecuteRefund)
	g.GET("/kpis", rbac.Permission("billing.read"), h.KPIs)
	r.GET("/patients/:id/invoices", rbac.Permission("billing.read"), func(c *gin.Context) {
		q := c.Request.URL.Query()
		q.Set("patientId", c.Param("id"))
		c.Request.URL.RawQuery = q.Encode()
		h.List(c)
	})
}
