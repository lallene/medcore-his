package medical_records

import (
	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
)

func RegisterRoutes(router *gin.RouterGroup, handler *Handler) {
	router.GET("/patients/:id/medical-record", rbac.Permission("medical_records.read"), handler.GetOrCreateByPatientID)

	router.GET("/medical-records/:recordId/overview", rbac.Permission("medical_records.read"), handler.GetOverview)

	router.POST("/medical-records/:recordId/alerts", rbac.Permission("medical_records.update"), handler.AddAlert)
	router.POST("/medical-records/:recordId/allergies", rbac.Permission("medical_records.update"), handler.AddAllergy)
	router.POST("/medical-records/:recordId/histories", rbac.Permission("medical_records.update"), handler.AddMedicalHistory)

	router.POST("/medical-records/:recordId/vital-signs", rbac.Permission("vital_signs.create"), handler.AddVitalSign)
	router.GET("/medical-records/:recordId/vital-signs", rbac.Permission("medical_records.read"), handler.ListVitalSigns)
	router.GET("/medical-records/:recordId/timeline", rbac.Permission("medical_records.read"), handler.ListTimelineEvents)
	router.GET("/patients/:id/medical-summary", rbac.Permission("medical_records.read"), handler.GetPatientMedicalSummary)
	router.GET("/patients/:id/common-medical-record", rbac.Permission("medical_records.read"), handler.GetCommonMedicalRecord)
	router.GET(
		"/patients/:id/summary",
		rbac.Permission("medical_records.read"), handler.GetPatientSummary,
	)

	router.PUT("/patients/:id/common-medical-record", rbac.Permission("medical_records.update"), handler.UpdateCommonMedicalRecord)
}
