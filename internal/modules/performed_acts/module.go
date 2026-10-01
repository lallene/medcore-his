package performed_acts

import (
	"github.com/lallene/medcore-his/backend/internal/core/application"
	"github.com/lallene/medcore-his/backend/internal/core/logger"
	"github.com/lallene/medcore-his/backend/internal/modules/auth"
)

type Module struct{}

func (Module) Register(app *application.Application) {
	logger.Info("Chargement module", "module", "performed_acts")
	// Schema owned by cmd/migrate (LOT 26I-3 / LOT27C / LOT27D). No runtime AutoMigrate.
	svc := NewService(app.DB).WithProducersEnabled(app.Config.PerformedActProducersEnabled)
	h := NewHandler(svc)
	g := app.API()
	g.Use(auth.Middleware(app.Config.JWTSecret, app.DB))
	RegisterRoutes(g, h)
}
