package patients

import (
	"github.com/lallene/medcore-his/backend/internal/core/application"
	"github.com/lallene/medcore-his/backend/internal/core/events"
	"github.com/lallene/medcore-his/backend/internal/core/logger"
	"github.com/lallene/medcore-his/backend/internal/modules/auth"
)

type Module struct{}

func (Module) Register(app *application.Application) {
	logger.Info("Chargement module", "module", "patients")

	app.MustMigrate(&Patient{})

	Provider{}.Register(app)

	handler := application.Make[*Handler](app)

	protected := app.API()
	protected.Use(auth.Middleware(app.Config.JWTSecret, app.DB))

	RegisterRoutesWithHandler(protected, handler)

	events.DefaultBus.Subscribe(
		"patient.created",
		PatientAuditListener{},
	)
}
