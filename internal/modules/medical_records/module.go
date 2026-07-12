package medical_records

import (
	"github.com/lallene/medcore-his/backend/internal/core/application"
	"github.com/lallene/medcore-his/backend/internal/core/logger"
	"github.com/lallene/medcore-his/backend/internal/modules/auth"
)

type Module struct{}

func (Module) Register(app *application.Application) {
	logger.Info("Chargement module", "module", "medical_records")

	app.MustMigrate(
		&MedicalRecord{},
		&MedicalAlert{},
		&Allergy{},
		&MedicalHistory{},
		&VitalSign{},
		&MedicalTimelineEvent{},
		&PatientMedicalProfile{},
		&SurgicalHistory{},
		&FamilyMedicalHistory{},
		&RegularTreatment{},
		&Vaccination{},
		&Disability{},
		&Lifestyle{},
		&MedicalDevice{},
		&MedicalDocument{},
	)

	repository := NewRepository(app.DB)
	service := NewService(repository)
	handler := NewHandler(service)

	protected := app.API()
	protected.Use(auth.Middleware(app.Config.JWTSecret))

	RegisterRoutes(protected, handler)
}
