package database

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// ErrInvalidDatabaseURL is returned when DATABASE_URL cannot be parsed.
// The error string never includes the raw DSN, password, or parse-error fragments.
var ErrInvalidDatabaseURL = fmt.Errorf("invalid DATABASE_URL configuration")

// ParseDatabaseURL parses a PostgreSQL connection URL without exposing DSN details
// in the returned error. Empty input yields a distinct missing-URL error.
func ParseDatabaseURL(databaseURL string) (*pgx.ConnConfig, error) {
	if databaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL manquante")
	}
	cfg, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return nil, ErrInvalidDatabaseURL
	}
	return cfg, nil
}

// Connect opens PostgreSQL via the GORM postgres/pgx stack and applies
// businessTimezone as a pgx RuntimeParams["timezone"] session default on every
// pooled connection (startup parameter), matching gorm.io/driver/postgres
// TimeZone handling — not a one-shot SET TIME ZONE on a single borrowed conn.
func Connect(databaseURL, businessTimezone string) *gorm.DB {
	pgConfig, err := ParseDatabaseURL(databaseURL)
	if err != nil {
		log.Fatal(err)
	}
	loc, err := time.LoadLocation(businessTimezone)
	if err != nil {
		log.Fatalf("MEDCORE_BUSINESS_TIMEZONE invalide %q: %v", businessTimezone, err)
	}

	if pgConfig.RuntimeParams == nil {
		pgConfig.RuntimeParams = map[string]string{}
	}
	// Validated IANA name only — never concatenate untrusted input into SQL.
	pgConfig.RuntimeParams["timezone"] = businessTimezone

	sqlDB := stdlib.OpenDB(*pgConfig, stdlib.OptionAfterConnect(func(ctx context.Context, conn *pgx.Conn) error {
		// Same timestamp ScanLocation registration as gorm postgres dialector when TimeZone is set.
		conn.TypeMap().RegisterType(&pgtype.Type{
			Name:  "timestamp",
			OID:   pgtype.TimestampOID,
			Codec: &pgtype.TimestampCodec{ScanLocation: loc},
		})
		return nil
	}))

	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		// Do not wrap/log the underlying dial error (may embed host/user fragments).
		log.Fatal("Erreur connexion PostgreSQL")
	}
	log.Printf("Connexion PostgreSQL OK (business_timezone=%s)", businessTimezone)
	return db
}

// SessionTimezone returns current_setting('TimeZone') for diagnostics/tests.
func SessionTimezone(db *gorm.DB) (string, error) {
	var tz string
	if err := db.Raw("SELECT current_setting('TimeZone')").Scan(&tz).Error; err != nil {
		return "", err
	}
	if tz == "" {
		return "", fmt.Errorf("empty TimeZone setting")
	}
	return tz, nil
}
