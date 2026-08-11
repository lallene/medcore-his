package pharmacy

import (
	"github.com/lallene/medcore-his/backend/internal/core/application"
	"github.com/lallene/medcore-his/backend/internal/core/logger"
	"github.com/lallene/medcore-his/backend/internal/modules/auth"
)

type Module struct{}

func (Module) Register(app *application.Application) {
	logger.Info("Chargement module", "module", "pharmacy")

	app.MustMigrate(
		&MedicationFamily{},
		&Medication{},
		&MedicationPresentation{},
		&PharmacyStock{},
		&PharmacyBatch{},
		&StockMovement{},
		&PharmacyDispensation{},
		&PharmacyDispensationItem{},
		&PharmacyVoucher{},
		&PharmacyVoucherLine{},
	)

	repository := NewRepository(app.DB)
	service := NewService(repository)
	handler := NewHandler(service)

	protected := app.API()
	protected.Use(auth.Middleware(app.Config.JWTSecret))

	RegisterRoutesWithHandler(protected, handler)
}
