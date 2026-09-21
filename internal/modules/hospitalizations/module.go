package hospitalizations

import (
	"github.com/lallene/medcore-his/backend/internal/core/application"
	"github.com/lallene/medcore-his/backend/internal/core/logger"
	"github.com/lallene/medcore-his/backend/internal/modules/auth"
)

type Module struct{}

func (Module) Register(app *application.Application) {
	logger.Info("Chargement module", "module", "hospitalizations")
	// Schema + partial unique indexes owned by cmd/migrate (LOT 26I-3).
	repo := NewRepository(app.DB)
	service := NewService(app.DB, repo)
	handler := NewHandler(service)
	protected := app.API()
	protected.Use(auth.Middleware(app.Config.JWTSecret, app.DB))
	RegisterRoutes(protected, handler, NewBedHandler(service))
}
