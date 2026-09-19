package config

import (
	"os"
	"strings"
	"testing"
)

func TestParseNotificationEmailEnabled(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		raw     string
		want    bool
		wantErr bool
	}{
		{"unset_empty", "", false, false},
		{"whitespace", "  \t  ", false, false},
		{"false", "false", false, false},
		{"FALSE", "FALSE", false, false},
		{"zero", "0", false, false},
		{"true", "true", true, false},
		{"TRUE", "TRUE", true, false},
		{"one", "1", true, false},
		{"trimmed_true", "  true  ", true, false},
		{"trimmed_false", " false ", false, false},
		{"invalid_yes", "yes", false, true},
		{"invalid_enabled", "enabled", false, true},
		{"invalid_abc", "abc", false, true},
		{"invalid_two", "2", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseNotificationEmailEnabled(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				if !strings.Contains(err.Error(), EnvNotificationEmailEnabled) {
					t.Fatalf("error should name key: %v", err)
				}
				trimmed := strings.TrimSpace(tc.raw)
				if trimmed != "" && strings.Contains(err.Error(), trimmed) {
					t.Fatalf("error must not echo value: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestConfigValidateDoesNotRequireM365WhenEmailEnabled(t *testing.T) {
	t.Parallel()
	cfg := Config{
		DatabaseURL:              "postgres://u:p@localhost:5432/db?sslmode=disable",
		Timezone:                 "UTC",
		BusinessTimezone:         "UTC",
		AppEnv:                   "development",
		JWTSecret:                "x",
		CORSOrigin:               "http://localhost",
		NotificationEmailEnabled: true,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("API/shared Validate must not require Graph secret: %v", err)
	}
}

// API/shared enablement must not couple to Graph credentials (Option C).
// Uses the pure parser + Validate path (Load itself calls log.Fatalf).
func TestAPISharedFlagEnabledWithoutM365ClientSecret(t *testing.T) {
	t.Setenv(EnvNotificationEmailEnabled, "true")
	t.Setenv("MEDCORE_M365_CLIENT_SECRET", "")
	if os.Getenv("MEDCORE_M365_CLIENT_SECRET") != "" {
		t.Fatal("test isolation: M365 secret must be unset")
	}
	enabled, err := ParseNotificationEmailEnabled(os.Getenv(EnvNotificationEmailEnabled))
	if err != nil {
		t.Fatal(err)
	}
	if !enabled {
		t.Fatal("want NotificationEmailEnabled=true")
	}
	cfg := Config{
		DatabaseURL:              "postgres://u:p@localhost:5432/db?sslmode=disable",
		Timezone:                 "UTC",
		BusinessTimezone:         "UTC",
		AppEnv:                   "development",
		JWTSecret:                "x",
		CORSOrigin:               "http://localhost",
		NotificationEmailEnabled: enabled,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("shared Config must load with email enabled and no Graph secret: %v", err)
	}
}
