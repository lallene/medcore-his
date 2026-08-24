package organization

import (
	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
)

func RegisterRoutes(r *gin.RouterGroup, h *Handler) {
	g := r.Group("/organization")
	g.GET("/catalog", rbac.Permission("organization.read"), h.Catalog)
	g.GET("/departments", rbac.Permission("organization.read"), h.Departments)
	g.POST("/departments", rbac.Permission("organization.manage"), h.SaveDepartment)
	g.PUT("/departments/:id", rbac.Permission("organization.manage"), h.SaveDepartment)
	g.GET("/services", rbac.Permission("organization.read"), h.Services)
	g.GET("/services/:id", rbac.Permission("organization.read"), h.Service)
	g.POST("/services", rbac.Permission("organization.manage"), h.SaveService)
	g.PUT("/services/:id", rbac.Permission("organization.manage"), h.SaveService)
}
