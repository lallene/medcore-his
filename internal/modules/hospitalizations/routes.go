package hospitalizations

import (
	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
)

func RegisterRoutes(router *gin.RouterGroup, handler *Handler, bedHandlers ...*BedHandler) {
	var beds *BedHandler
	if len(bedHandlers) > 0 {
		beds = bedHandlers[0]
	}
	if beds != nil {
		rooms := router.Group("/rooms")
		rooms.GET("", rbac.Permission("rooms.read"), beds.ListRooms)
		rooms.POST("", rbac.Permission("rooms.manage"), beds.CreateRoom)
		rooms.GET("/:id", rbac.Permission("rooms.read"), beds.FindRoom)
		rooms.PUT("/:id", rbac.Permission("rooms.manage"), beds.UpdateRoom)
		bedRoutes := router.Group("/beds")
		bedRoutes.GET("", rbac.Permission("beds.read"), beds.ListBeds)
		bedRoutes.POST("", rbac.Permission("beds.manage"), beds.CreateBed)
		bedRoutes.GET("/:id", rbac.Permission("beds.read"), beds.FindBed)
		bedRoutes.PUT("/:id", rbac.Permission("beds.manage"), beds.UpdateBed)
	}
	group := router.Group("/hospitalizations")
	group.GET("", rbac.Permission("hospitalizations.read"), handler.List)
	group.POST("", rbac.Permission("hospitalizations.create"), handler.Create)
	group.GET("/consultation/:consultationId", rbac.Permission("hospitalizations.read"), handler.FindByConsultation)
	group.GET("/:id", rbac.Permission("hospitalizations.read"), handler.FindByID)
	group.POST("/:id/admit", rbac.Permission("hospitalizations.update"), handler.Admit)
	group.POST("/:id/discharge", rbac.Permission("hospitalizations.discharge"), handler.Discharge)
	group.POST("/:id/cancel", rbac.Permission("hospitalizations.cancel"), handler.Cancel)
	if beds != nil {
		group.GET("/:id/bed-assignments", rbac.Permission("bed_assignments.read"), beds.ListAssignments)
		group.POST("/:id/bed-assignments", rbac.Permission("bed_assignments.create"), beds.Assign)
		group.POST("/:id/transfer", rbac.Permission("bed_assignments.transfer"), beds.Transfer)
		group.POST("/:id/release-bed", rbac.Permission("bed_assignments.release"), beds.Release)
	}
	router.GET("/patients/:id/hospitalizations", rbac.Permission("hospitalizations.read"), handler.ListByPatient)
}
