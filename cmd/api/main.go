package main

import (
	_ "github.com/lallene/medcore-his/backend/docs"

	"github.com/lallene/medcore-his/backend/internal/core/application"
	"github.com/lallene/medcore-his/backend/internal/modules/auth"
	"github.com/lallene/medcore-his/backend/internal/modules/consultations"
	"github.com/lallene/medcore-his/backend/internal/modules/dashboard"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
)

// @title MedCore HIS API
// @version 0.1.0
// @description Modern Hospital Information System API
// @description Modules disponibles : Auth, Patients, Insurance, Workflow, Audit.
// @contact.name MedCore Team
// @contact.email support@medcore.local
// @license.name MIT
// @license.url https://opensource.org/licenses/MIT
// @host medcore-his-api-latest.onrender.com
// @BasePath /api
// @schemes https
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description JWT Bearer token. Exemple : Bearer {token}
func main() {
	app := application.New()

	app.RegisterModule(auth.Module{})
	app.RegisterModule(patients.Module{})
	app.RegisterModule(insurance.Module{})
	app.RegisterModule(dashboard.Module{})
	app.RegisterModule(consultations.Module{})

	app.Run()
}
