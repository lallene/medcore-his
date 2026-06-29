package config

import (
	"log"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	AppEnv      string
	Port        string
	DatabaseURL string
	JWTSecret   string
	CORSOrigin  string
}

func Load() Config {
	if err := godotenv.Load(); err != nil {
		log.Println("Fichier .env non trouvé, utilisation des variables système")
	}

	return Config{
		AppEnv:      getEnv("APP_ENV", "development"),
		Port:        getEnv("PORT", "8080"),
		DatabaseURL: getEnv("DATABASE_URL", ""),
		JWTSecret:   getEnv("JWT_SECRET", "change_me"),
		CORSOrigin:  getEnv("CORS_ORIGIN", "*"),
	}
}

func getEnv(key string, fallback string) string {
	value := os.Getenv(key)

	if value == "" {
		return fallback
	}

	return value
}
