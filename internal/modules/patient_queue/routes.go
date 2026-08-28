package patient_queue

import (
	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
)

func RegisterRoutes(r *gin.RouterGroup, h *Handler) {
	g := r.Group("/queue")
	g.POST("/appointments", rbac.Permission("queue.checkin"), h.CreateAppointment)
	g.GET("/appointments/today", rbac.Permission("queue.reception.read"), h.ListAppointmentsToday)
	g.POST("/appointments/:id/check-in", rbac.Permission("queue.checkin"), h.CheckInAppointment)
	g.POST("/appointments/:id/no-show", rbac.AnyPermission("queue.checkin", "queue.cancel"), h.MarkNoShow)
	g.POST("/check-in/walk-in", rbac.Permission("queue.checkin"), h.CheckInWalkIn)
	g.GET("/tickets", rbac.AnyPermission("queue.reception.read", "queue.triage.read", "queue.doctor.read", "queue.read.service", "queue.read.all"), h.List)
	g.GET("/doctor/worklist", rbac.AnyPermission("queue.doctor.read", "queue.read.service", "queue.read.all"), h.DoctorWorklist)
	g.GET("/tickets/by-consultation/:id", rbac.AnyPermission("queue.doctor.read", "queue.read.service", "queue.read.all"), h.GetByConsultation)
	g.GET("/patients/:patientId/active-ticket", rbac.AnyPermission("queue.doctor.read", "queue.read.service", "queue.read.all", "patients.360.read"), h.GetActiveTicketForPatient)
	g.GET("/tickets/:id", rbac.AnyPermission("queue.reception.read", "queue.triage.read", "queue.doctor.read", "queue.read.service", "queue.read.all"), h.Get)
	g.POST("/tickets/:id/triage/take", rbac.Permission("queue.triage.update"), h.TakeTriage)
	g.POST("/tickets/:id/triage/complete", rbac.Permission("queue.triage.update"), h.CompleteTriage)
	g.POST("/tickets/:id/doctor/take", rbac.Permission("queue.doctor.take"), h.TakeDoctor)
	g.POST("/tickets/:id/complete", rbac.Permission("queue.doctor.take"), h.Complete)
	g.POST("/tickets/:id/cancel", rbac.Permission("queue.cancel"), h.Cancel)
	g.POST("/tickets/:id/priority", rbac.Permission("queue.priority.update"), h.SetPriority)
	g.GET("/kpis", rbac.AnyPermission("queue.reception.read", "queue.read.service", "queue.read.all"), h.KPIs)
	g.GET("/finance/:patientId", rbac.Permission("queue.checkin"), h.EvaluateFinance)
}
