package auth

import (
	"github.com/lallene/medcore-his/backend/internal/core/application"
	"github.com/lallene/medcore-his/backend/internal/core/logger"
)

type Module struct{}

func (Module) Register(app *application.Application) {
	logger.Info("Chargement module", "module", "auth")

	app.MustMigrate(&User{})

	handler := NewHandler(app.DB, app.Config.JWTSecret)

	RegisterRoutes(app.API(), handler)
}
