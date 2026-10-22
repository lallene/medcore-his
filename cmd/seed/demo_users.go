package main

import (
	"errors"
	"log"

	"gorm.io/gorm"

	"github.com/lallene/medcore-his/backend/internal/modules/auth"
)

// ensureDemoUser (called by createDemoAccount, inside its transaction) creates a demo account only when no account with this email exists
// (soft-deleted rows included). An existing account is NEVER modified by the seed:
// no password hash, name, role, activation or deactivation change. created=false
// tells callers to leave the account (and its staff profile) untouched.
func ensureDemoUser(db *gorm.DB, name, email, role string, active bool) (auth.User, bool, error) {
	var existing auth.User
	err := db.Unscoped().Where("email = ?", email).First(&existing).Error
	if err == nil {
		return existing, false, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return auth.User{}, false, err
	}
	user := auth.User{Name: name, Email: email, PasswordHash: demoHash(), Role: role, IsActive: true}
	if err := db.Create(&user).Error; err != nil {
		return auth.User{}, false, err
	}
	if !active {
		// GORM applies default:true to a false bool on insert: set the flag explicitly,
		// only on the row created right now.
		if err := db.Model(&user).Update("is_active", false).Error; err != nil {
			return auth.User{}, false, err
		}
		user.IsActive = false
	}
	return user, true, nil
}

// createDemoAccount is the only entry point that creates demo accounts. For a NEW account,
// the user row and its profile (when profile != nil) are written in ONE transaction:
// BEGIN → user → profile → COMMIT, or ROLLBACK on any error, so an interrupted or failed
// seed never leaves an account without its profile. An account that already exists
// (soft-deleted included) is returned untouched and the profile callback is NOT called:
// the seed never repairs or completes a pre-existing account.
func createDemoAccount(db *gorm.DB, name, email, role string, active bool, profile func(tx *gorm.DB, user auth.User) error) (auth.User, bool, error) {
	var user auth.User
	created := false
	err := db.Transaction(func(tx *gorm.DB) error {
		u, isNew, err := ensureDemoUser(tx, name, email, role, active)
		if err != nil {
			return err
		}
		user, created = u, isNew
		if !isNew || profile == nil {
			return nil
		}
		return profile(tx, u)
	})
	if err != nil {
		return auth.User{}, false, err
	}
	return user, created, nil
}

// seedAdmin creates the demo administrator only on a fresh demo database.
// It never resets the password, role or activation of an existing admin@medcore.local.
func seedAdmin(db *gorm.DB) {
	_, created, err := createDemoAccount(db, "Administrateur", "admin@medcore.local", "admin", true, nil)
	if err != nil {
		log.Fatalf("Erreur seed admin: %v", err)
	}
	if !created {
		log.Println("Administrateur DEMO existant conservé sans modification")
	}
}
