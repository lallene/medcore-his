package config

import (
	"testing"
	"time"
)

func TestValidateRejectsInvalidBusinessTimezone(t *testing.T) {
	cfg := Config{
		DatabaseURL:      "postgres://u:p@localhost:5432/db?sslmode=disable",
		Timezone:         "UTC",
		BusinessTimezone: "Not/AZone",
		AppEnv:           "development",
		JWTSecret:        "x",
		CORSOrigin:       "http://localhost",
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected invalid MEDCORE_BUSINESS_TIMEZONE error")
	}
}

func TestValidateAcceptsIndependentTimezones(t *testing.T) {
	cfg := Config{
		DatabaseURL:      "postgres://u:p@localhost:5432/db?sslmode=disable",
		Timezone:         "UTC",
		BusinessTimezone: "Europe/Paris",
		AppEnv:           "development",
		JWTSecret:        "x",
		CORSOrigin:       "http://localhost",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if cfg.Timezone == cfg.BusinessTimezone {
		t.Fatal("test fixture should keep scheduling and business TZ distinct")
	}
	if loc := cfg.BusinessLocation(); loc.String() != "Europe/Paris" {
		t.Fatalf("BusinessLocation=%s", loc)
	}
	if _, err := time.LoadLocation(cfg.Timezone); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultBusinessTimezoneIsUTCNotLocal(t *testing.T) {
	if DefaultBusinessTimezone != "UTC" {
		t.Fatalf("default=%s", DefaultBusinessTimezone)
	}
}

func TestParsePerformedActProducersEnabledDefaultSafe(t *testing.T) {
	v, err := ParsePerformedActProducersEnabled("")
	if err != nil || v {
		t.Fatalf("empty default want false, got %v err=%v", v, err)
	}
	v, err = ParsePerformedActProducersEnabled("true")
	if err != nil || !v {
		t.Fatalf("true want true, got %v err=%v", v, err)
	}
	v, err = ParsePerformedActProducersEnabled("0")
	if err != nil || v {
		t.Fatalf("0 want false, got %v err=%v", v, err)
	}
	if _, err := ParsePerformedActProducersEnabled("maybe"); err == nil {
		t.Fatal("expected invalid value error")
	}
}
