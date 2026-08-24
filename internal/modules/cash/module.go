package cash

import (
	"github.com/lallene/medcore-his/backend/internal/core/application"
	"github.com/lallene/medcore-his/backend/internal/core/logger"
	"github.com/lallene/medcore-his/backend/internal/modules/auth"
	"github.com/lallene/medcore-his/backend/internal/modules/billing"
)

type Module struct{}

func (Module) Register(app *application.Application) {
	logger.Info("Chargement module", "module", "cash")
	app.MustMigrate(&Register{}, &Session{}, &billing.Payment{}, &Receipt{})
	if e := app.DB.Exec("CREATE UNIQUE INDEX IF NOT EXISTS ux_cash_sessions_open_register ON cash_sessions (cash_register_id) WHERE status = 'OPEN'").Error; e != nil {
		panic(e)
	}
	g := app.API()
	g.Use(auth.Middleware(app.Config.JWTSecret))
	RegisterRoutes(g, NewHandler(NewService(app.DB)))
}
