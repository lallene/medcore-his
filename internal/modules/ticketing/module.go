package ticketing

import (
	"github.com/lallene/medcore-his/backend/internal/core/application"
	"github.com/lallene/medcore-his/backend/internal/core/logger"
	"github.com/lallene/medcore-his/backend/internal/modules/auth"
)

type Module struct{}

func (Module) Register(app *application.Application) {
	logger.Info("Chargement module", "module", "ticketing")
	app.MustMigrate(&Category{}, &SLA{}, &Ticket{}, &Comment{}, &Attachment{}, &Assignment{}, &History{}, &Notification{})
	s := NewService(app.DB)
	if e := s.SeedDefaults(1); e != nil {
		panic(e)
	}
	g := app.API()
	g.Use(auth.Middleware(app.Config.JWTSecret, app.DB))
	RegisterRoutes(g, NewHandler(s))
}
