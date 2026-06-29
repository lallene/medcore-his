package database

import (
	"log"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func Connect(databaseURL string) *gorm.DB {
	if databaseURL == "" {
		log.Fatal("DATABASE_URL manquante")
	}

	db, err := gorm.Open(postgres.Open(databaseURL), &gorm.Config{})
	if err != nil {
		log.Fatal("Erreur connexion PostgreSQL:", err)
	}

	log.Println("Connexion PostgreSQL OK")

	return db
}
