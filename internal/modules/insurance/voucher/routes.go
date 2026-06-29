package voucher

import (
	"github.com/gin-gonic/gin"

	"github.com/lallene/medcore-his/backend/internal/core/rbac"
)

func RegisterRoutes(router *gin.RouterGroup, handler *Handler) {
	group := router.Group("/insurance/vouchers")

	group.GET("", rbac.Permission("insurance.voucher.read"), handler.List)
	group.GET("/:id", rbac.Permission("insurance.voucher.read"), handler.FindByID)
	group.POST("", rbac.Permission("insurance.voucher.create"), handler.Create)
	group.PUT("/:id", rbac.Permission("insurance.voucher.update"), handler.Update)
	group.DELETE("/:id", rbac.Permission("insurance.voucher.delete"), handler.Delete)

	group.POST("/:id/workflow", rbac.Permission("insurance.voucher.workflow"), handler.ApplyWorkflow)
}
