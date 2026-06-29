package main

import (
	"github.com/lallene/medcore-his/backend/internal/core/application"
	"github.com/lallene/medcore-his/backend/internal/modules/auth"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
)

func main() {
	app := application.New()

	app.RegisterModule(auth.Module{})
	app.RegisterModule(patients.Module{})
	app.RegisterModule(insurance.Module{})

	app.Run()
}
