package company

import (
	"github.com/gin-gonic/gin"

	"github.com/lallene/medcore-his/backend/internal/core/rbac"
)

func RegisterRoutes(router *gin.RouterGroup, handler *Handler) {
	group := router.Group("/insurance/companies")

	group.GET("", rbac.Permission("insurance.company.read"), handler.List)
	group.GET("/:id", rbac.Permission("insurance.company.read"), handler.FindByID)
	group.POST("", rbac.Permission("insurance.company.create"), handler.Create)
	group.PUT("/:id", rbac.Permission("insurance.company.update"), handler.Update)
	group.DELETE("/:id", rbac.Permission("insurance.company.delete"), handler.Delete)
}
