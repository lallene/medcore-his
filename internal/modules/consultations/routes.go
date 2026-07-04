package consultations

import (
	"github.com/gin-gonic/gin"

	"github.com/lallene/medcore-his/backend/internal/core/rbac"
)

func RegisterRoutesWithHandler(router *gin.RouterGroup, handler *Handler) {
	consultations := router.Group("/consultations")
	{
		consultations.GET("/reasons", handler.GetReasons)
		consultations.POST("/reasons", rbac.Permission("consultations.references.manage"), handler.CreateReason)
		consultations.PUT("/reasons/:id", rbac.Permission("consultations.references.manage"), handler.UpdateReason)
		consultations.DELETE("/reasons/:id", rbac.Permission("consultations.references.manage"), handler.DeleteReason)

		consultations.GET("/exams", handler.GetExams)
		consultations.POST("/exams", rbac.Permission("consultations.references.manage"), handler.CreateExam)
		consultations.PUT("/exams/:id", rbac.Permission("consultations.references.manage"), handler.UpdateExam)
		consultations.DELETE("/exams/:id", rbac.Permission("consultations.references.manage"), handler.DeleteExam)

		consultations.POST("", handler.CreateConsultation)
		consultations.GET("/:id", handler.GetConsultation)
		consultations.PUT("/:id", handler.UpdateConsultation)
		consultations.PATCH("/:id/status", handler.UpdateStatus)

		consultations.GET("/:id/sick-leave/pdf", handler.GenerateSickLeavePDF)
		consultations.GET("/:id/exam-request/pdf", handler.GenerateExamRequestPDF)
		consultations.GET("/:id/prescription/pdf", handler.GeneratePrescriptionPDF)
		consultations.GET("/:id/report/pdf", handler.GenerateConsultationReportPDF)
		consultations.GET("/:id/hospitalization/pdf", handler.GenerateHospitalizationPDF)
		consultations.GET("/physical-exam-areas", handler.GetPhysicalExamAreas)
	}

	router.GET("/patients/:id/consultations", handler.GetPatientConsultations)
	router.GET("/patients/:id/360", handler.GetPatient360)
}
