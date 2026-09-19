package config

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// DefaultBusinessTimezone is the deployment business calendar when
// MEDCORE_BUSINESS_TIMEZONE is unset. UTC is explicit (never process time.Local).
const DefaultBusinessTimezone = "UTC"

// DefaultSchedulingTimezone is MEDCORE_TIMEZONE when unset (wall-clock schedules).
const DefaultSchedulingTimezone = "UTC"

type Config struct {
	AppEnv      string
	Port        string
	DatabaseURL string
	JWTSecret   string
	CORSOrigin  string
	// Timezone is the IANA zone for recurring wall-clock schedules (MEDCORE_TIMEZONE).
	Timezone string
	// BusinessTimezone is the IANA zone for hospital civil "today" / CURRENT_DATE
	// alignment (MEDCORE_BUSINESS_TIMEZONE). Independent of Timezone.
	BusinessTimezone string
}

func Load() Config {
	if err := godotenv.Load(); err != nil {
		log.Println("Fichier .env non trouvé, utilisation des variables système")
	}

	cfg := Config{
		AppEnv:           getEnv("APP_ENV", "development"),
		Port:             getEnv("PORT", "8080"),
		DatabaseURL:      getEnv("DATABASE_URL", ""),
		JWTSecret:        getEnv("JWT_SECRET", "change_me"),
		CORSOrigin:       getEnv("CORS_ORIGIN", "http://localhost:5173"),
		Timezone:         getEnv("MEDCORE_TIMEZONE", DefaultSchedulingTimezone),
		BusinessTimezone: getEnv("MEDCORE_BUSINESS_TIMEZONE", DefaultBusinessTimezone),
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

	if strings.TrimSpace(c.BusinessTimezone) == "" {
		return fmt.Errorf("MEDCORE_BUSINESS_TIMEZONE est obligatoire")
	}
	if _, err := time.LoadLocation(c.BusinessTimezone); err != nil {
		return fmt.Errorf("MEDCORE_BUSINESS_TIMEZONE invalide %q: %w", c.BusinessTimezone, err)
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

// BusinessLocation returns the validated business IANA location.
func (c Config) BusinessLocation() *time.Location {
	loc, err := time.LoadLocation(c.BusinessTimezone)
	if err != nil {
		// Validate() already rejected invalid names; fall back to UTC if raced.
		return time.UTC
	}
	return loc
}

func getEnv(key string, fallback string) string {
	value := os.Getenv(key)

	if value == "" {
		return fallback
	}

	return value
}
