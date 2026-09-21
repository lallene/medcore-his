package main

import (
	"log"

	"github.com/lallene/medcore-his/backend/internal/config"
	"github.com/lallene/medcore-his/backend/internal/core/logger"
	"github.com/lallene/medcore-his/backend/internal/database"
)

// Sole production schema owner (LOT 26I-3). Exactly one migration execution
// must succeed before API / notification-worker replicas start.
func main() {
	cfg := config.Load()
	logger.Init(cfg.AppEnv)

	db := database.Connect(cfg.DatabaseURL, cfg.BusinessTimezone)
	if err := applyMigrations(db); err != nil {
		log.Fatal("Erreur migration:", err)
	}
	log.Println("Migrations exécutées avec succès")
}
