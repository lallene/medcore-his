package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// Demo seed opt-in and credential (LOT AIDE+SEED). No default value exists:
// a demo seed without both variables fails closed before any mutation.
const (
	EnvAllowDemoSeed    = "MEDCORE_ALLOW_DEMO_SEED"
	EnvDemoSeedPassword = "MEDCORE_DEMO_SEED_PASSWORD"

	// TEMPORARY until V1: exceptional opt-in allowing the demo seed on the pre-V1 instance
	// (APP_ENV=production, database exactly "medcore"). It never suffices on its own: the
	// regular opt-in and password rules still apply, and Neon is always refused.
	EnvAllowPreV1DemoSeed = "MEDCORE_ALLOW_PRE_V1_DEMO_SEED"
	preV1DemoDatabase     = "medcore"

	minDemoPasswordLen = 12
)

// demoGuardInput is everything the common demo guard decides on (pure, testable).
type demoGuardInput struct {
	AppEnv       string
	DatabaseName string
	AllowFlag    string
	Password     string
	PreV1Flag    string // MEDCORE_ALLOW_PRE_V1_DEMO_SEED (temporary, until V1)
	DatabaseHost string // connection host(s) from DATABASE_URL, used only to refuse Neon
}

// databaseHosts extracts the connection host(s) of a DSN (URL or keyword form, fallbacks
// included). The error never echoes the DSN.
func databaseHosts(dsn string) (string, error) {
	cfg, err := pgconn.ParseConfig(dsn)
	if err != nil {
		return "", errors.New("seed DEMO refusé : DATABASE_URL illisible")
	}
	// pgconn lists the primary host again among fallbacks (TLS variants): deduplicate.
	hosts, seen := []string{cfg.Host}, map[string]bool{cfg.Host: true}
	for _, f := range cfg.Fallbacks {
		if !seen[f.Host] {
			seen[f.Host] = true
			hosts = append(hosts, f.Host)
		}
	}
	return strings.Join(hosts, ","), nil
}

// isNeonTarget refuses Neon by database name or by connection host (*.neon.tech).
func isNeonTarget(databaseName, databaseHost string) bool {
	return strings.Contains(strings.ToLower(databaseName), "neon") || strings.Contains(strings.ToLower(databaseHost), "neon")
}

func optInSet(flag string) bool { return strings.TrimSpace(flag) == "1" }

// isPreV1DemoException is true only for the temporary pre-V1 instance: pre-V1 opt-in,
// production environment and the exact database name "medcore". Any other production
// database stays refused; the remaining guard conditions are checked by checkDemoGuard.
func isPreV1DemoException(in demoGuardInput) bool {
	return optInSet(in.PreV1Flag) && validateSeedEnvironment(in.AppEnv) != nil && in.DatabaseName == preV1DemoDatabase
}

// validateDemoScope decides on environment + database. Neon is refused first, whatever the opt-ins.
func validateDemoScope(in demoGuardInput) error {
	if isNeonTarget(in.DatabaseName, in.DatabaseHost) {
		return fmt.Errorf("seed DEMO refusé sur la base %q (Neon interdit)", in.DatabaseName)
	}
	if isPreV1DemoException(in) {
		return nil
	}
	if err := validateSeedEnvironment(in.AppEnv); err != nil {
		return err
	}
	return validateDemoTarget(in.DatabaseName)
}

// checkDemoGuard is the single fail-closed gate for every demo seed path.
// All conditions are required simultaneously (the pre-V1 exception only replaces the
// environment/database rule); error messages never echo the password.
func checkDemoGuard(in demoGuardInput) error {
	if err := validateDemoScope(in); err != nil {
		return err
	}
	if !optInSet(in.AllowFlag) {
		return fmt.Errorf("seed DEMO refusé : %s=1 obligatoire", EnvAllowDemoSeed)
	}
	if strings.TrimSpace(in.Password) == "" {
		return fmt.Errorf("seed DEMO refusé : %s obligatoire (aucune valeur par défaut)", EnvDemoSeedPassword)
	}
	if len(in.Password) < minDemoPasswordLen {
		return fmt.Errorf("seed DEMO refusé : %s trop court (%d caractères minimum)", EnvDemoSeedPassword, minDemoPasswordLen)
	}
	if len(in.Password) > 72 {
		return fmt.Errorf("seed DEMO refusé : %s trop long (bcrypt 72 octets maximum)", EnvDemoSeedPassword)
	}
	return nil
}

// demoPasswordHash is set only by requireDemoSeed; demo account creation refuses to run without it.
var demoPasswordHash string

// requireDemoSeed enforces checkDemoGuard against the connected database and prepares the
// demo credential hash. Any failure exits before the first write.
func requireDemoSeed(db *gorm.DB, appEnv, databaseURL string) {
	host, err := databaseHosts(databaseURL)
	if err != nil {
		log.Fatal(err)
	}
	var name string
	if err := db.Raw("SELECT current_database()").Scan(&name).Error; err != nil {
		log.Fatal(err)
	}
	password := os.Getenv(EnvDemoSeedPassword)
	in := demoGuardInput{
		AppEnv: appEnv, DatabaseName: name, AllowFlag: os.Getenv(EnvAllowDemoSeed), Password: password,
		PreV1Flag: os.Getenv(EnvAllowPreV1DemoSeed), DatabaseHost: host,
	}
	if err := checkDemoGuard(in); err != nil {
		log.Fatal(err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Fatal("hash du mot de passe DEMO impossible")
	}
	demoPasswordHash = string(hash)
	if isPreV1DemoException(in) {
		log.Printf("EXCEPTION PRÉ-V1 TEMPORAIRE (%s=1) : seed DEMO sur la base de production %s", EnvAllowPreV1DemoSeed, name)
		return
	}
	log.Printf("Base DEMO locale ciblée: %s", name)
}

var errDemoGuardNotPassed = errors.New("seed DEMO : garde-fou non validé (requireDemoSeed)")

func demoHash() string {
	if demoPasswordHash == "" {
		log.Fatal(errDemoGuardNotPassed)
	}
	return demoPasswordHash
}
