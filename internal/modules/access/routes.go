package access

import (
	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
)

func RegisterRoutes(r *gin.RouterGroup, h *Handler) {
	g := r.Group("/access")
	g.GET("/kpis", rbac.AnyPermission("rbac.read", "staff.read", "staff.manage"), h.KPIs)
	g.GET("/users", rbac.AnyPermission("rbac.read", "staff.read", "staff.manage"), h.ListUsers)
	g.GET("/users/:id", rbac.AnyPermission("rbac.read", "staff.read", "staff.manage"), h.GetUser)
	g.GET("/users/:id/simulate", rbac.AnyPermission("rbac.read", "staff.read", "staff.manage"), h.Simulate)
	g.GET("/users/:id/audit", rbac.AnyPermission("rbac.audit.read", "staff.audit.read", "staff.manage"), h.Audit)
	g.PUT("/users/:id/active", rbac.AnyPermission("rbac.user.manage", "staff.manage"), h.SetActive)
	g.PUT("/users/:id/functions", rbac.AnyPermission("rbac.user.manage", "staff.manage"), h.SetFunctions)
	g.PUT("/users/:id/services", rbac.AnyPermission("rbac.user.manage", "staff.manage"), h.SetServices)
	g.POST("/users/:id/overrides", rbac.AnyPermission("rbac.override.manage", "staff.manage"), h.SetOverride)
	g.DELETE("/users/:id/overrides/:permission", rbac.AnyPermission("rbac.override.manage", "staff.manage"), h.ClearOverride)
	g.GET("/matrix", rbac.AnyPermission("rbac.read", "staff.read", "staff.manage"), h.Matrix)
	g.POST("/matrix", rbac.Permission("rbac.matrix.manage"), h.ToggleMatrix)
	g.GET("/permissions", rbac.AnyPermission("rbac.read", "staff.read", "staff.manage"), h.Permissions)
	g.GET("/audit", rbac.AnyPermission("rbac.audit.read", "staff.audit.read", "staff.manage"), h.Audit)
}
