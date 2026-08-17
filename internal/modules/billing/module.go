package billing

import (
	"github.com/lallene/medcore-his/backend/internal/core/application"
	"github.com/lallene/medcore-his/backend/internal/core/logger"
	"github.com/lallene/medcore-his/backend/internal/modules/auth"
)

type Module struct{}

func (Module) Register(app *application.Application) {
	logger.Info("Chargement module", "module", "billing")
	app.MustMigrate(&Tariff{}, &Invoice{}, &InvoiceLine{}, &AuthorizationAllocation{}, &Payment{})
	for _, sql := range []string{"CREATE UNIQUE INDEX IF NOT EXISTS ux_billing_active_billable_key ON billing_invoice_lines (billable_key) WHERE is_active = true"} {
		if e := app.DB.Exec(sql).Error; e != nil {
			panic(e)
		}
	}
	h := NewHandler(NewService(app.DB))
	g := app.API()
	g.Use(auth.Middleware(app.Config.JWTSecret))
	RegisterRoutes(g, h)
}
