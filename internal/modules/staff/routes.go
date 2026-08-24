package staff

import (
	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
)

func RegisterRoutes(r *gin.RouterGroup, h *Handler) {
	g := r.Group("/staff")
	g.GET("", rbac.Permission("staff.read"), h.List)
	g.GET("/catalog", rbac.Permission("staff.read"), h.Catalog)
	g.GET("/users", rbac.Permission("staff.manage"), h.Users)
	g.GET("/:id", rbac.Permission("staff.read"), h.Get)
	g.GET("/:id/audit", rbac.Permission("staff.audit.read"), h.Audit)
	g.POST("", rbac.Permission("staff.manage"), h.Create)
	g.PUT("/:id", rbac.Permission("staff.manage"), h.Update)
}
