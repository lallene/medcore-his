package main

import (
	_ "github.com/lallene/medcore-his/backend/docs"

	"github.com/lallene/medcore-his/backend/internal/core/application"
	"github.com/lallene/medcore-his/backend/internal/modules/auth"
	"github.com/lallene/medcore-his/backend/internal/modules/billing"
	"github.com/lallene/medcore-his/backend/internal/modules/cash"
	"github.com/lallene/medcore-his/backend/internal/modules/consultations"
	"github.com/lallene/medcore-his/backend/internal/modules/dashboard"
	"github.com/lallene/medcore-his/backend/internal/modules/hospitalizations"
	"github.com/lallene/medcore-his/backend/internal/modules/imaging"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance_receivables"
	"github.com/lallene/medcore-his/backend/internal/modules/laboratory"
	"github.com/lallene/medcore-his/backend/internal/modules/medical_records"
	"github.com/lallene/medcore-his/backend/internal/modules/organization"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
	"github.com/lallene/medcore-his/backend/internal/modules/pharmacy"
	"github.com/lallene/medcore-his/backend/internal/modules/receivables"
	"github.com/lallene/medcore-his/backend/internal/modules/staff"
)

// @title						MedCore HIS API
// @version					0.1.0
// @description				Modern Hospital Information System API
// @description				Modules disponibles : Auth, Patients, Insurance, Workflow, Audit.
// @contact.name				MedCore Team
// @contact.email				support@medcore.local
// @license.name				MIT
// @license.url				https://opensource.org/licenses/MIT
// @host						medcore-his-api-latest.onrender.com
// @BasePath					/api
// @schemes					https
// @securityDefinitions.apikey	BearerAuth
// @in							header
// @name						Authorization
// @description				JWT Bearer token. Exemple : Bearer {token}
func main() {
	app := application.New()

	app.RegisterModule(auth.Module{})
	app.RegisterModule(organization.Module{})
	app.RegisterModule(staff.Module{})
	app.RegisterModule(billing.Module{})
	app.RegisterModule(receivables.Module{})
	app.RegisterModule(insurance_receivables.Module{})
	app.RegisterModule(cash.Module{})
	app.RegisterModule(patients.Module{})
	app.RegisterModule(insurance.Module{})
	app.RegisterModule(dashboard.Module{})
	app.RegisterModule(consultations.Module{})
	app.RegisterModule(pharmacy.Module{})
	app.RegisterModule(medical_records.Module{})
	app.RegisterModule(hospitalizations.Module{})
	app.RegisterModule(laboratory.Module{})
	app.RegisterModule(imaging.Module{})
	if err := organization.BackfillLegacy(app.DB, 1); err != nil {
		panic(err)
	}

	app.Run()
}
