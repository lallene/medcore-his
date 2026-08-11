package laboratory

import (
	"github.com/lallene/medcore-his/backend/internal/core/application"
	"github.com/lallene/medcore-his/backend/internal/core/logger"
	"github.com/lallene/medcore-his/backend/internal/modules/auth"
)

type Module struct{}

func (Module) Register(app *application.Application) {
	logger.Info("Chargement module", "module", "laboratory")
	app.MustMigrate(&Order{}, &Sample{}, &Result{})
	h := NewHandler(NewService(NewRepository(app.DB)))
	p := app.API()
	p.Use(auth.Middleware(app.Config.JWTSecret))
	RegisterRoutes(p, h)
}
