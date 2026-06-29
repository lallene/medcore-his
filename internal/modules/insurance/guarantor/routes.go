package guarantor

import (
	"github.com/gin-gonic/gin"

	"github.com/lallene/medcore-his/backend/internal/core/rbac"
)

func RegisterRoutes(router *gin.RouterGroup, handler *Handler) {
	group := router.Group("/insurance/guarantors")

	group.GET("", rbac.Permission("insurance.guarantor.read"), handler.List)
	group.GET("/:id", rbac.Permission("insurance.guarantor.read"), handler.FindByID)
	group.POST("", rbac.Permission("insurance.guarantor.create"), handler.Create)
	group.PUT("/:id", rbac.Permission("insurance.guarantor.update"), handler.Update)
	group.DELETE("/:id", rbac.Permission("insurance.guarantor.delete"), handler.Delete)
}
