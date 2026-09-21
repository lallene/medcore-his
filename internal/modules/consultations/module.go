package consultations

import (
	"github.com/lallene/medcore-his/backend/internal/core/application"
	"github.com/lallene/medcore-his/backend/internal/core/logger"
	"github.com/lallene/medcore-his/backend/internal/modules/auth"
	"github.com/lallene/medcore-his/backend/internal/modules/medical_records"
)

type Module struct{}

func (Module) Register(app *application.Application) {
	logger.Info("Chargement module", "module", "consultations")
	// Schema owned by cmd/migrate (LOT 26I-3).
	// Reference seed data retained on API (not DDL); seed ownership redesign deferred.

	SeedConsultationReferences(app.DB)
	SeedPhysicalExamAreas(app.DB)

	repository := NewRepository(app.DB)

	medicalRecordsRepository := medical_records.NewRepository(app.DB)
	medicalRecordsService := medical_records.NewService(
		medicalRecordsRepository,
	)

	service := NewService(
		repository,
		medicalRecordsService,
	)

	handler := NewHandler(service)

	protected := app.API()
	protected.Use(auth.Middleware(app.Config.JWTSecret, app.DB))

	RegisterRoutesWithHandler(protected, handler)
}
