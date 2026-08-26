package main

import (
	"log"
	"os"
	"strings"

	"github.com/lallene/medcore-his/backend/internal/config"
	"github.com/lallene/medcore-his/backend/internal/database"
	"github.com/lallene/medcore-his/backend/internal/modules/qa"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatal("usage: qa-import <qa-summary.json>")
	}
	cfg := config.Load()
	db := database.Connect(cfg.DatabaseURL)
	var name string
	if e := db.Raw("SELECT current_database()").Scan(&name).Error; e != nil {
		log.Fatal(e)
	}
	if name != "medcore_full_demo" && strings.ToLower(os.Getenv("QA_ALLOW_DATABASE")) != strings.ToLower(name) {
		log.Fatalf("import QA refusé sur la base %q", name)
	}
	summary, e := qa.ReadSummary(os.Args[1])
	if e != nil {
		log.Fatal(e)
	}
	if strings.EqualFold(summary.Environment, "production") {
		log.Fatal("ingestion de campagne destructive production interdite")
	}
	if e = qa.ImportSummary(db, summary); e != nil {
		log.Fatal(e)
	}
	log.Printf("Campagne QA %s importée", summary.RunID)
}
