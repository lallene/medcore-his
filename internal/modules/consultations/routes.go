package consultations

import (
	"github.com/gin-gonic/gin"

	"github.com/lallene/medcore-his/backend/internal/core/rbac"
)

func RegisterRoutesWithHandler(router *gin.RouterGroup, handler *Handler) {
	consultations := router.Group("/consultations")
	{
		consultations.GET("/reasons", rbac.Permission("consultations.read"), handler.GetReasons)
		consultations.POST("/reasons", rbac.Permission("consultations.references.manage"), handler.CreateReason)
		consultations.PUT("/reasons/:id", rbac.Permission("consultations.references.manage"), handler.UpdateReason)
		consultations.DELETE("/reasons/:id", rbac.Permission("consultations.references.manage"), handler.DeleteReason)

		consultations.GET("/exams", rbac.Permission("consultations.read"), handler.GetExams)
		consultations.POST("/exams", rbac.Permission("consultations.references.manage"), handler.CreateExam)
		consultations.PUT("/exams/:id", rbac.Permission("consultations.references.manage"), handler.UpdateExam)
		consultations.DELETE("/exams/:id", rbac.Permission("consultations.references.manage"), handler.DeleteExam)

		consultations.POST("", rbac.Permission("consultations.create"), handler.CreateConsultation)
		consultations.GET("", rbac.Permission("consultations.read"), handler.ListConsultations)
		consultations.GET("/:id", rbac.Permission("consultations.read"), handler.GetConsultation)
		consultations.PUT("/:id", rbac.Permission("consultations.update"), handler.UpdateConsultation)
		consultations.PATCH("/:id/status", rbac.Permission("consultations.update"), handler.UpdateStatus)

		consultations.GET("/:id/sick-leave/pdf", rbac.Permission("consultations.read"), handler.GenerateSickLeavePDF)
		consultations.GET("/:id/exam-request/pdf", rbac.Permission("consultations.read"), handler.GenerateExamRequestPDF)
		consultations.GET("/:id/prescription/pdf", rbac.Permission("consultations.read"), handler.GeneratePrescriptionPDF)
		consultations.GET("/:id/report/pdf", rbac.Permission("consultations.read"), handler.GenerateConsultationReportPDF)
		consultations.GET("/:id/hospitalization/pdf", rbac.Permission("consultations.read"), handler.GenerateHospitalizationPDF)
		consultations.GET("/physical-exam-areas", rbac.Permission("consultations.read"), handler.GetPhysicalExamAreas)

		router.GET("/consultations/:id/soap", rbac.Permission("consultations.read"), handler.GetSOAP)
		router.PUT("/consultations/:id/soap", rbac.Permission("consultations.update"), handler.UpsertSOAP)

		consultations.GET("/:id/specialty", rbac.Permission("consultations.read"), handler.GetSpecialtyData)
		consultations.PUT("/:id/specialty", rbac.Permission("consultations.update"), handler.UpsertSpecialtyData)
	}

	router.GET("/patients/:id/consultations", rbac.Permission("consultations.read"), handler.GetPatientConsultations)
	router.GET("/patients/:id/360", rbac.Permission("patients.360.read"), handler.GetPatient360)
}
