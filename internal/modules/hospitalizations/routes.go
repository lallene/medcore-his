package hospitalizations

import (
	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
)

func RegisterRoutes(router *gin.RouterGroup, handler *Handler) {
	group := router.Group("/hospitalizations")
	group.GET("", rbac.Permission("hospitalizations.read"), handler.List)
	group.POST("", rbac.Permission("hospitalizations.create"), handler.Create)
	group.GET("/consultation/:consultationId", rbac.Permission("hospitalizations.read"), handler.FindByConsultation)
	group.GET("/:id", rbac.Permission("hospitalizations.read"), handler.FindByID)
	group.POST("/:id/admit", rbac.Permission("hospitalizations.update"), handler.Admit)
	group.POST("/:id/discharge", rbac.Permission("hospitalizations.discharge"), handler.Discharge)
	group.POST("/:id/cancel", rbac.Permission("hospitalizations.cancel"), handler.Cancel)
	router.GET("/patients/:id/hospitalizations", rbac.Permission("hospitalizations.read"), handler.ListByPatient)
}
