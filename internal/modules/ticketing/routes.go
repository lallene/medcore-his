package ticketing

import (
	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
)

func RegisterRoutes(r *gin.RouterGroup, h *Handler) {
	g := r.Group("/tickets")
	g.POST("", rbac.Permission("ticket.create"), h.Create)
	g.GET("", rbac.Permission("ticket.read.own"), h.List)
	g.GET("/:id", rbac.Permission("ticket.read.own"), h.Get)
	g.PATCH("/:id", rbac.Permission("ticket.update"), h.Update)
	g.POST("/:id/comments", rbac.Permission("ticket.comment"), h.Comment)
	g.GET("/:id/comments", rbac.Permission("ticket.read.own"), h.Comments)
	g.POST("/:id/assign", rbac.Permission("ticket.assign"), h.Assign)
	g.POST("/:id/workflow", rbac.AnyPermission("ticket.update", "ticket.resolve", "ticket.close", "ticket.reopen"), h.Workflow)
	g.GET("/:id/history", rbac.Permission("ticket.audit.read"), h.History)
	g.POST("/:id/attachments", rbac.Permission("ticket.comment"), h.Upload)
	g.GET("/:id/attachments/:attachmentId", rbac.Permission("ticket.read.own"), h.Download)
	t := r.Group("/ticketing")
	t.GET("/categories", rbac.Permission("ticket.create"), h.Categories)
	t.GET("/kpis", rbac.Permission("ticket.read.service"), h.KPIs)
	t.GET("/notifications", rbac.Permission("ticket.read.own"), h.Notifications)
	t.GET("/agents", rbac.Permission("ticket.assign"), h.Agents)
}
