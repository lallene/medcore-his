package patient_queue

import (
	"github.com/lallene/medcore-his/backend/internal/core/application"
	"github.com/lallene/medcore-his/backend/internal/core/logger"
	"github.com/lallene/medcore-his/backend/internal/modules/auth"
	"github.com/lallene/medcore-his/backend/internal/modules/performed_acts"
)

type Module struct{}

func (Module) Register(app *application.Application) {
	logger.Info("Chargement module", "module", "patient_queue")
	// Schema/indexes owned by cmd/migrate (LOT 26I-3).
	s := NewService(app.DB).
		WithNotificationLifecycleConfig(NotificationLifecycleConfig{
			EmailEnabled: app.Config.NotificationEmailEnabled,
		})
	if app.Config.PerformedActProducersEnabled {
		s = s.WithPerformedActs(performed_acts.NewService(app.DB))
	}
	g := app.API()
	g.Use(auth.Middleware(app.Config.JWTSecret, app.DB))
	RegisterRoutes(g, NewHandler(s))
}
