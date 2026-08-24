package imaging

import (
	"github.com/lallene/medcore-his/backend/internal/core/application"
	"github.com/lallene/medcore-his/backend/internal/core/logger"
	"github.com/lallene/medcore-his/backend/internal/modules/auth"
)

type Module struct{}

func (Module) Register(app *application.Application) {
	logger.Info("Chargement module", "module", "imaging")
	app.MustMigrate(&Order{}, &Report{})
	h := NewHandler(NewService(NewRepository(app.DB)))
	api := app.API()
	api.Use(auth.Middleware(app.Config.JWTSecret, app.DB))
	RegisterRoutes(api, h)
}
