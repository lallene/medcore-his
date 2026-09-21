package config

import (
	"strings"
	"testing"
	"time"
)

func testConfigBase() Config {
	return Config{
		DatabaseURL: "postgres://u:p@localhost:5432/db?sslmode=disable",
		AppEnv:      "development",
		JWTSecret:   "x",
		CORSOrigin:  "http://localhost",
	}
}

func TestNormalizeTimezoneEnv(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		raw      string
		fallback string
		want     string
	}{
		{"unset_empty", "", DefaultBusinessTimezone, "UTC"},
		{"whitespace", "  \t  ", DefaultBusinessTimezone, "UTC"},
		{"padded_paris", " Europe/Paris ", DefaultBusinessTimezone, "Europe/Paris"},
		{"paris", "Europe/Paris", DefaultBusinessTimezone, "Europe/Paris"},
		{"scheduling_whitespace", "   ", DefaultSchedulingTimezone, "UTC"},
		{"scheduling_padded", " Africa/Abidjan ", DefaultSchedulingTimezone, "Africa/Abidjan"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := NormalizeTimezoneEnv(tc.raw, tc.fallback)
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestBusinessTimezoneContract(t *testing.T) {
	t.Parallel()

	t.Run("unset_default_utc", func(t *testing.T) {
		t.Parallel()
		cfg := testConfigBase()
		cfg.Timezone = DefaultSchedulingTimezone
		cfg.BusinessTimezone = NormalizeTimezoneEnv("", DefaultBusinessTimezone)
		if cfg.BusinessTimezone != "UTC" {
			t.Fatalf("got %q", cfg.BusinessTimezone)
		}
		if err := cfg.Validate(); err != nil {
			t.Fatal(err)
		}
		if loc := cfg.BusinessLocation(); loc != time.UTC && loc.String() != "UTC" {
			t.Fatalf("BusinessLocation=%s", loc)
		}
	})

	t.Run("whitespace_utc", func(t *testing.T) {
		t.Parallel()
		cfg := testConfigBase()
		cfg.Timezone = DefaultSchedulingTimezone
		cfg.BusinessTimezone = NormalizeTimezoneEnv("  \t ", DefaultBusinessTimezone)
		if cfg.BusinessTimezone != "UTC" {
			t.Fatalf("got %q", cfg.BusinessTimezone)
		}
		if err := cfg.Validate(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("padded_valid_iana", func(t *testing.T) {
		t.Parallel()
		cfg := testConfigBase()
		cfg.Timezone = DefaultSchedulingTimezone
		cfg.BusinessTimezone = NormalizeTimezoneEnv(" Europe/Paris ", DefaultBusinessTimezone)
		if cfg.BusinessTimezone != "Europe/Paris" {
			t.Fatalf("normalized=%q", cfg.BusinessTimezone)
		}
		if err := cfg.Validate(); err != nil {
			t.Fatal(err)
		}
		if loc := cfg.BusinessLocation(); loc.String() != "Europe/Paris" {
			t.Fatalf("BusinessLocation=%s", loc)
		}
	})

	t.Run("invalid_explicit", func(t *testing.T) {
		t.Parallel()
		cfg := testConfigBase()
		cfg.Timezone = DefaultSchedulingTimezone
		cfg.BusinessTimezone = NormalizeTimezoneEnv("Not/AZone", DefaultBusinessTimezone)
		err := cfg.Validate()
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "MEDCORE_BUSINESS_TIMEZONE") {
			t.Fatalf("error=%v", err)
		}
	})
}

func TestSchedulingTimezoneContract(t *testing.T) {
	t.Parallel()

	t.Run("unset_default_utc", func(t *testing.T) {
		t.Parallel()
		cfg := testConfigBase()
		cfg.BusinessTimezone = DefaultBusinessTimezone
		cfg.Timezone = NormalizeTimezoneEnv("", DefaultSchedulingTimezone)
		if cfg.Timezone != "UTC" {
			t.Fatalf("got %q", cfg.Timezone)
		}
		if err := cfg.Validate(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("whitespace_utc", func(t *testing.T) {
		t.Parallel()
		cfg := testConfigBase()
		cfg.BusinessTimezone = DefaultBusinessTimezone
		cfg.Timezone = NormalizeTimezoneEnv("   ", DefaultSchedulingTimezone)
		if cfg.Timezone != "UTC" {
			t.Fatalf("got %q", cfg.Timezone)
		}
		if err := cfg.Validate(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("padded_valid_iana", func(t *testing.T) {
		t.Parallel()
		cfg := testConfigBase()
		cfg.BusinessTimezone = DefaultBusinessTimezone
		cfg.Timezone = NormalizeTimezoneEnv(" Europe/Paris ", DefaultSchedulingTimezone)
		if cfg.Timezone != "Europe/Paris" {
			t.Fatalf("normalized=%q", cfg.Timezone)
		}
		if err := cfg.Validate(); err != nil {
			t.Fatal(err)
		}
		if _, err := time.LoadLocation(cfg.Timezone); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("invalid_explicit", func(t *testing.T) {
		t.Parallel()
		cfg := testConfigBase()
		cfg.BusinessTimezone = DefaultBusinessTimezone
		cfg.Timezone = NormalizeTimezoneEnv("Not/AZone", DefaultSchedulingTimezone)
		err := cfg.Validate()
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "MEDCORE_TIMEZONE") {
			t.Fatalf("error=%v", err)
		}
	})
}
