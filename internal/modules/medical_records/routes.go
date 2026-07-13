package medical_records

import "github.com/gin-gonic/gin"

func RegisterRoutes(router *gin.RouterGroup, handler *Handler) {
	router.GET("/patients/:id/medical-record", handler.GetOrCreateByPatientID)

	router.GET("/medical-records/:recordId/overview", handler.GetOverview)

	router.POST("/medical-records/:recordId/alerts", handler.AddAlert)
	router.POST("/medical-records/:recordId/allergies", handler.AddAllergy)
	router.POST("/medical-records/:recordId/histories", handler.AddMedicalHistory)

	router.POST("/medical-records/:recordId/vital-signs", handler.AddVitalSign)
	router.GET("/medical-records/:recordId/vital-signs", handler.ListVitalSigns)
	router.GET("/medical-records/:recordId/timeline", handler.ListTimelineEvents)
	router.GET("/patients/:id/medical-summary", handler.GetPatientMedicalSummary)
	router.GET("/patients/:id/common-medical-record", handler.GetCommonMedicalRecord)
	router.GET(
		"/patients/:id/summary",
		handler.GetPatientSummary,
	)

	router.PUT("/patients/:id/common-medical-record", handler.UpdateCommonMedicalRecord)
}
