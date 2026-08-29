package config

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	AppEnv      string
	Port        string
	DatabaseURL string
	JWTSecret   string
	CORSOrigin  string
	// Timezone is the IANA zone for recurring wall-clock schedules (MEDCORE_TIMEZONE).
	Timezone string
}

func Load() Config {
	if err := godotenv.Load(); err != nil {
		log.Println("Fichier .env non trouvé, utilisation des variables système")
	}

	cfg := Config{
		AppEnv:      getEnv("APP_ENV", "development"),
		Port:        getEnv("PORT", "8080"),
		DatabaseURL: getEnv("DATABASE_URL", ""),
		JWTSecret:   getEnv("JWT_SECRET", "change_me"),
		CORSOrigin:  getEnv("CORS_ORIGIN", "http://localhost:5173"),
		Timezone:    getEnv("MEDCORE_TIMEZONE", "UTC"),
	}

	if err := cfg.Validate(); err != nil {
		log.Fatalf("configuration invalide: %v", err)
	}

	return cfg
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.DatabaseURL) == "" {
		return fmt.Errorf("DATABASE_URL est obligatoire")
	}

	if strings.TrimSpace(c.Timezone) != "" {
		if _, err := time.LoadLocation(c.Timezone); err != nil {
			return fmt.Errorf("MEDCORE_TIMEZONE invalide %q: %w", c.Timezone, err)
		}
	}

	if strings.EqualFold(strings.TrimSpace(c.AppEnv), "production") {
		if strings.TrimSpace(c.JWTSecret) == "" ||
			c.JWTSecret == "change_me" {
			return fmt.Errorf("JWT_SECRET sécurisé obligatoire en production")
		}

		if strings.TrimSpace(c.CORSOrigin) == "" ||
			c.CORSOrigin == "*" {
			return fmt.Errorf("CORS_ORIGIN explicite obligatoire en production")
		}
	}

	return nil
}

func getEnv(key string, fallback string) string {
	value := os.Getenv(key)

	if value == "" {
		return fallback
	}

	return value
}
