package ticketing

import (
	"github.com/lallene/medcore-his/backend/internal/core/application"
	"github.com/lallene/medcore-his/backend/internal/core/logger"
	"github.com/lallene/medcore-his/backend/internal/modules/auth"
)

type Module struct{}

func (Module) Register(app *application.Application) {
	logger.Info("Chargement module", "module", "ticketing")
	// Schema owned by cmd/migrate (LOT 26I-3).
	s := NewService(app.DB)
	// Idempotent reference SLA/categories (data bootstrap, not DDL). Deferred
	// seed-ownership redesign; keep production bootstrap on API for now.
	if e := s.SeedDefaults(1); e != nil {
		panic(e)
	}
	g := app.API()
	g.Use(auth.Middleware(app.Config.JWTSecret, app.DB))
	RegisterRoutes(g, NewHandler(s))
}
