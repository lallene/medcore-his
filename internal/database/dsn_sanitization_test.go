package database_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/lallene/medcore-his/backend/internal/database"
)

func TestParseDatabaseURLRejectsEmpty(t *testing.T) {
	t.Parallel()
	_, err := database.ParseDatabaseURL("")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("error=%v", err)
	}
}

func TestParseDatabaseURLDoesNotLeakSecretInError(t *testing.T) {
	t.Parallel()
	const marker = "sekrit-LEAK-TEST-XYZ-26I3"
	// Malformed URL embedding a synthetic secret marker.
	raw := "postgres://user:" + marker + "@not a valid host!!!!/db"
	_, err := database.ParseDatabaseURL(raw)
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !errors.Is(err, database.ErrInvalidDatabaseURL) {
		t.Fatalf("want ErrInvalidDatabaseURL, got %v", err)
	}
	if strings.Contains(err.Error(), marker) {
		t.Fatalf("error leaked secret marker: %v", err)
	}
	if strings.Contains(err.Error(), "postgres://") {
		t.Fatalf("error leaked DSN scheme: %v", err)
	}
	msg := err.Error()
	if msg != database.ErrInvalidDatabaseURL.Error() {
		t.Fatalf("want exact safe message %q, got %q", database.ErrInvalidDatabaseURL.Error(), msg)
	}
}

func TestParseDatabaseURLAcceptsValidSynthetic(t *testing.T) {
	t.Parallel()
	cfg, err := database.ParseDatabaseURL("postgres://u:p@localhost:5432/db?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil {
		t.Fatal("nil config")
	}
	if cfg.Database != "db" {
		t.Fatalf("database=%q", cfg.Database)
	}
}
