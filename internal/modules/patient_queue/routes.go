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
	g.POST("/appointments/:id/no-show", rbac.AnyPermission("appointment.no_show.service", "appointment.no_show.all"), h.MarkNoShow)
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

	// LOT 23B — working schedules & exceptions (no /availability)
	sg := r.Group("/schedules")
	sg.GET("/mine", rbac.AnyPermission("schedule.read.own", "schedule.read.all", "schedule.manage.own"), h.ListMySchedules)
	sg.GET("", rbac.AnyPermission("schedule.read.own", "schedule.read.service", "schedule.read.all", "schedule.manage.service", "schedule.manage.all"), h.ListSchedules)
	sg.POST("", rbac.AnyPermission("schedule.manage.own", "schedule.manage.service", "schedule.manage.all"), h.CreateSchedule)
	sg.GET("/:id", rbac.AnyPermission("schedule.read.own", "schedule.read.service", "schedule.read.all", "schedule.manage.service", "schedule.manage.all"), h.GetSchedule)
	sg.PATCH("/:id", rbac.AnyPermission("schedule.manage.own", "schedule.manage.service", "schedule.manage.all"), h.UpdateSchedule)
	sg.DELETE("/:id", rbac.AnyPermission("schedule.manage.own", "schedule.manage.service", "schedule.manage.all"), h.DeleteSchedule)

	eg := r.Group("/schedule-exceptions")
	eg.GET("", rbac.AnyPermission("schedule.read.own", "schedule.read.service", "schedule.read.all", "schedule.manage.service", "schedule.manage.all"), h.ListScheduleExceptions)
	eg.POST("", rbac.AnyPermission("schedule.manage.own", "schedule.manage.service", "schedule.manage.all"), h.CreateScheduleException)
	eg.GET("/:id", rbac.AnyPermission("schedule.read.own", "schedule.read.service", "schedule.read.all", "schedule.manage.service", "schedule.manage.all"), h.GetScheduleException)
	eg.PATCH("/:id", rbac.AnyPermission("schedule.manage.own", "schedule.manage.service", "schedule.manage.all"), h.UpdateScheduleException)
	eg.DELETE("/:id", rbac.AnyPermission("schedule.manage.own", "schedule.manage.service", "schedule.manage.all"), h.DeleteScheduleException)

	// LOT 23C — read-only availability (no booking, no persisted slots)
	ag := r.Group("/availability")
	ag.GET("", rbac.AnyPermission("schedule.read.own", "schedule.read.service", "schedule.read.all", "schedule.manage.service", "schedule.manage.all"), h.GetAvailability)
	ag.GET("/first", rbac.AnyPermission("schedule.read.own", "schedule.read.service", "schedule.read.all", "schedule.manage.service", "schedule.manage.all"), h.GetFirstAvailability)
	ag.GET("/mine", rbac.AnyPermission("schedule.read.own", "schedule.read.all", "schedule.manage.own"), h.GetMyAvailability)

	// LOT 23D — transactional booking (authoritative; does not trust availability snapshots)
	r.POST("/appointments", rbac.AnyPermission("queue.checkin", "schedule.manage.service", "schedule.manage.all"), h.BookAppointment)
	// LOT 23F.1 — agenda read APIs (schedule.read.*; not queue.checkin / consultations.read)
	r.GET("/appointments", rbac.AnyPermission("schedule.read.own", "schedule.read.service", "schedule.read.all"), h.ListAppointments)
	r.GET("/appointments/:id", rbac.AnyPermission("schedule.read.own", "schedule.read.service", "schedule.read.all"), h.GetAppointment)
	r.GET("/appointment-types", rbac.AnyPermission("schedule.read.own", "schedule.read.service", "schedule.read.all"), h.ListAppointmentTypes)
	// LOT 23E — lifecycle (reschedule / cancel / no-show); queue.checkin is NOT lifecycle authority
	r.PATCH("/appointments/:id/reschedule", rbac.AnyPermission(
		"appointment.reschedule.service", "appointment.reschedule.all",
		"schedule.manage.service", "schedule.manage.all",
	), h.RescheduleAppointment)
	r.POST("/appointments/:id/cancel", rbac.AnyPermission(
		"appointment.cancel.service", "appointment.cancel.all",
	), h.CancelAppointment)
	r.POST("/appointments/:id/no-show", rbac.AnyPermission(
		"appointment.no_show.service", "appointment.no_show.all",
	), h.MarkNoShowAuthoritative)
}
