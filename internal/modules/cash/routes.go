package cash

import (
	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
)

func RegisterRoutes(r *gin.RouterGroup, h *Handler) {
	g := r.Group("/cash")
	g.GET("/registers", rbac.Permission("cash.register.read"), h.Registers)
	g.POST("/registers", rbac.Permission("cash.register.manage"), h.CreateRegister)
	g.PUT("/registers/:id", rbac.Permission("cash.register.manage"), h.UpdateRegister)
	g.GET("/sessions/current", rbac.Permission("cash.session.read"), h.Current)
	g.GET("/sessions", rbac.Permission("cash.session.read"), h.Sessions)
	g.POST("/sessions/open", rbac.Permission("cash.session.open"), h.Open)
	g.GET("/sessions/:id", rbac.Permission("cash.session.read"), h.Get)
	g.GET("/sessions/:id/journal", rbac.Permission("cash.payment.read"), h.Journal)
	g.POST("/sessions/:id/payments", rbac.Permission("cash.payment.create"), h.Pay)
	g.POST("/sessions/:id/close", rbac.Permission("cash.session.close"), h.Close)
	g.GET("/receipts", rbac.Permission("cash.receipt.read"), h.Receipts)
	g.GET("/receipts/:id", rbac.Permission("cash.receipt.read"), h.Receipt)
}
