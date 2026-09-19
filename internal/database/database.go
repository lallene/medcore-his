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

// Connect opens PostgreSQL via the GORM postgres/pgx stack and applies
// businessTimezone as a pgx RuntimeParams["timezone"] session default on every
// pooled connection (startup parameter), matching gorm.io/driver/postgres
// TimeZone handling — not a one-shot SET TIME ZONE on a single borrowed conn.
func Connect(databaseURL, businessTimezone string) *gorm.DB {
	if databaseURL == "" {
		log.Fatal("DATABASE_URL manquante")
	}
	loc, err := time.LoadLocation(businessTimezone)
	if err != nil {
		log.Fatalf("MEDCORE_BUSINESS_TIMEZONE invalide %q: %v", businessTimezone, err)
	}

	pgConfig, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		log.Fatal("DATABASE_URL invalide:", err)
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
		log.Fatal("Erreur connexion PostgreSQL:", err)
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
