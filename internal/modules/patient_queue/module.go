package patient_queue

import (
	"github.com/lallene/medcore-his/backend/internal/core/application"
	"github.com/lallene/medcore-his/backend/internal/core/logger"
	"github.com/lallene/medcore-his/backend/internal/modules/auth"
)

type Module struct{}

func (Module) Register(app *application.Application) {
	logger.Info("Chargement module", "module", "patient_queue")
	app.MustMigrate(&AppointmentType{}, &Appointment{}, &AppointmentHistory{}, &Ticket{}, &History{},
		&StaffWorkingSchedule{}, &ScheduleException{}, &ScheduleAuditEvent{})
	if err := EnsureAppointmentIndexes(app.DB); err != nil {
		logger.Error("Index patient_queue appointments", "error", err)
	}
	if err := EnsureScheduleIndexes(app.DB); err != nil {
		logger.Error("Index patient_queue schedules", "error", err)
	}
	// LOT 23F: one lifetime ticket per appointment — hard invariant; must abort startup.
	if err := EnsureTicketIndexes(app.DB); err != nil {
		logger.Error("Index patient_queue tickets", "error", err)
		panic(err)
	}
	s := NewService(app.DB)
	g := app.API()
	g.Use(auth.Middleware(app.Config.JWTSecret, app.DB))
	RegisterRoutes(g, NewHandler(s))
}
