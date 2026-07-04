package consultations

import (
	"github.com/lallene/medcore-his/backend/internal/core/application"
	"github.com/lallene/medcore-his/backend/internal/core/logger"
	"github.com/lallene/medcore-his/backend/internal/modules/auth"
)

type Module struct{}

func (Module) Register(app *application.Application) {
	logger.Info("Chargement module", "module", "consultations")

	app.MustMigrate(
		&Consultation{},
		&ConsultationReason{},
		&MedicalExam{},
		&ConsultationVitals{},
		&ConsultationExamRequest{},
		&ConsultationPrescription{},
		&ConsultationAntecedent{},
		&PhysicalExamArea{},
		&ConsultationPhysicalExam{},
		&ConsultationPhysicalExam{},
		&ConsultationAdministeredTreatment{},
		&ConsultationPreviousMedication{},
		&ConsultationSurgicalHistory{},
		&ConsultationGynecoObstetricHistory{},
	)

	SeedConsultationReferences(app.DB)
	SeedPhysicalExamAreas(app.DB)

	repository := NewRepository(app.DB)
	service := NewService(repository)
	handler := NewHandler(service)

	protected := app.API()
	protected.Use(auth.Middleware(app.Config.JWTSecret))

	RegisterRoutesWithHandler(protected, handler)
}
