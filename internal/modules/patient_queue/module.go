package patient_queue

import (
	"github.com/lallene/medcore-his/backend/internal/core/application"
	"github.com/lallene/medcore-his/backend/internal/core/logger"
	"github.com/lallene/medcore-his/backend/internal/modules/auth"
)

type Module struct{}

func (Module) Register(app *application.Application) {
	logger.Info("Chargement module", "module", "patient_queue")
	app.MustMigrate(&AppointmentType{}, &AppointmentSeries{}, &Appointment{}, &AppointmentHistory{}, &Ticket{}, &History{},
		&StaffWorkingSchedule{}, &ScheduleException{}, &ScheduleAuditEvent{},
		&AppointmentNotificationIntent{}, &AppointmentNotificationAttempt{})
	if err := EnsureAppointmentIndexes(app.DB); err != nil {
		logger.Error("Index patient_queue appointments", "error", err)
	}
	if err := EnsureScheduleIndexes(app.DB); err != nil {
		logger.Error("Index patient_queue schedules", "error", err)
	}
	// LOT 23O-A: series integrity (unique, CHECK pair, FK) — hard fail.
	if err := EnsureAppointmentSeriesIndexes(app.DB); err != nil {
		logger.Error("Index appointment series", "error", err)
		panic(err)
	}
	// LOT 23F: one lifetime ticket per appointment — hard invariant; must abort startup.
	if err := EnsureTicketIndexes(app.DB); err != nil {
		logger.Error("Index patient_queue tickets", "error", err)
		panic(err)
	}
	// LOT 23N-A: unique index + attempt→intent FK underwrite idempotency/integrity — hard fail.
	if err := EnsureNotificationIndexes(app.DB); err != nil {
		logger.Error("Index appointment notifications", "error", err)
		panic(err)
	}
	s := NewService(app.DB).WithNotificationLifecycleConfig(NotificationLifecycleConfig{
		EmailEnabled: app.Config.NotificationEmailEnabled,
	})
	g := app.API()
	g.Use(auth.Middleware(app.Config.JWTSecret, app.DB))
	RegisterRoutes(g, NewHandler(s))
}
