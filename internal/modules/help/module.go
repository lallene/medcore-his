package help

import (
	"github.com/lallene/medcore-his/backend/internal/core/application"
	"github.com/lallene/medcore-his/backend/internal/core/logger"
	"github.com/lallene/medcore-his/backend/internal/modules/auth"
)

type Module struct{}

// Register mounts GET /api/help/entries. Schema and catalog synchronization are owned
// by cmd/migrate; the API never writes help rows and starts even if the table is empty.
func (Module) Register(app *application.Application) {
	logger.Info("Chargement module", "module", "help")
	g := app.API()
	g.Use(auth.Middleware(app.Config.JWTSecret, app.DB))
	RegisterRoutes(g, NewHandler(NewService(app.DB)))
}
