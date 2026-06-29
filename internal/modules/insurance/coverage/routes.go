package coverage

import (
	"github.com/gin-gonic/gin"

	"github.com/lallene/medcore-his/backend/internal/core/rbac"
)

func RegisterRoutes(router *gin.RouterGroup, handler *Handler) {
	group := router.Group("/insurance/coverages")

	group.GET("", rbac.Permission("insurance.coverage.read"), handler.List)
	group.GET("/:id", rbac.Permission("insurance.coverage.read"), handler.FindByID)
	group.POST("", rbac.Permission("insurance.coverage.create"), handler.Create)
	group.PUT("/:id", rbac.Permission("insurance.coverage.update"), handler.Update)
	group.DELETE("/:id", rbac.Permission("insurance.coverage.delete"), handler.Delete)

	group.GET(
		"/patient/:patientId",
		rbac.Permission("insurance.coverage.read"),
		handler.FindByPatient,
	)
}
