package main

import (
	"log"

	"github.com/lallene/medcore-his/backend/internal/config"
	"github.com/lallene/medcore-his/backend/internal/core/audit"
	"github.com/lallene/medcore-his/backend/internal/core/logger"
	"github.com/lallene/medcore-his/backend/internal/core/workflow"
	"github.com/lallene/medcore-his/backend/internal/database"
	"github.com/lallene/medcore-his/backend/internal/modules/auth"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/company"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/coverage"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/guarantor"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/voucher"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
)

func main() {
	cfg := config.Load()

	logger.Init(cfg.AppEnv)

	db := database.Connect(cfg.DatabaseURL)

	err := db.AutoMigrate(
		&audit.AuditLog{},
		&workflow.History{},
		&auth.User{},
		&patients.Patient{},
		&company.InsuranceCompany{},
		&guarantor.InsuranceGuarantor{},
		&coverage.PatientCoverage{},
		&voucher.InsuranceVoucher{},
	)

	if err != nil {
		log.Fatal("Erreur migration:", err)
	}

	log.Println("Migrations exécutées avec succès")
}
