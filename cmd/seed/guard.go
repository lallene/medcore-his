package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// Demo seed opt-in and credential (LOT AIDE+SEED). No default value exists:
// a demo seed without both variables fails closed before any mutation.
const (
	EnvAllowDemoSeed    = "MEDCORE_ALLOW_DEMO_SEED"
	EnvDemoSeedPassword = "MEDCORE_DEMO_SEED_PASSWORD"

	minDemoPasswordLen = 12
)

// demoGuardInput is everything the common demo guard decides on (pure, testable).
type demoGuardInput struct {
	AppEnv       string
	DatabaseName string
	AllowFlag    string
	Password     string
}

// checkDemoGuard is the single fail-closed gate for every demo seed path.
// All conditions are required simultaneously; error messages never echo the password.
func checkDemoGuard(in demoGuardInput) error {
	if err := validateSeedEnvironment(in.AppEnv); err != nil {
		return err
	}
	if err := validateDemoTarget(in.DatabaseName); err != nil {
		return err
	}
	if strings.TrimSpace(in.AllowFlag) != "1" {
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
func requireDemoSeed(db *gorm.DB, appEnv string) {
	var name string
	if err := db.Raw("SELECT current_database()").Scan(&name).Error; err != nil {
		log.Fatal(err)
	}
	password := os.Getenv(EnvDemoSeedPassword)
	if err := checkDemoGuard(demoGuardInput{
		AppEnv: appEnv, DatabaseName: name, AllowFlag: os.Getenv(EnvAllowDemoSeed), Password: password,
	}); err != nil {
		log.Fatal(err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Fatal("hash du mot de passe DEMO impossible")
	}
	demoPasswordHash = string(hash)
	log.Printf("Base DEMO locale ciblée: %s", name)
}

var errDemoGuardNotPassed = errors.New("seed DEMO : garde-fou non validé (requireDemoSeed)")

func demoHash() string {
	if demoPasswordHash == "" {
		log.Fatal(errDemoGuardNotPassed)
	}
	return demoPasswordHash
}
