package receivables

import (
	"github.com/lallene/medcore-his/backend/internal/core/application"
	"github.com/lallene/medcore-his/backend/internal/core/logger"
	"github.com/lallene/medcore-his/backend/internal/modules/auth"
)

type Module struct{}

func (Module) Register(app *application.Application) {
	logger.Info("Chargement module", "module", "receivables")
	app.MustMigrate(&Metadata{}, &FollowUp{})
	g := app.API()
	g.Use(auth.Middleware(app.Config.JWTSecret))
	RegisterRoutes(g, NewHandler(NewService(app.DB)))
}
