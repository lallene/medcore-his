package imaging

import (
	"github.com/lallene/medcore-his/backend/internal/core/application"
	"github.com/lallene/medcore-his/backend/internal/core/logger"
	"github.com/lallene/medcore-his/backend/internal/modules/auth"
	"github.com/lallene/medcore-his/backend/internal/modules/performed_acts"
)

type Module struct{}

func (Module) Register(app *application.Application) {
	logger.Info("Chargement module", "module", "imaging")
	svc := NewService(NewRepository(app.DB))
	if app.Config.PerformedActProducersEnabled {
		svc = svc.WithPerformedActs(performed_acts.NewService(app.DB))
	}
	h := NewHandler(svc)
	api := app.API()
	api.Use(auth.Middleware(app.Config.JWTSecret, app.DB))
	RegisterRoutes(api, h)
}
