package main

import (
	"strings"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/grc"
)

func TestParseGRCReminderInterval(t *testing.T) {
	got, err := ParseGRCReminderInterval("  ")
	if err != nil || got != time.Hour {
		t.Fatalf("default interval: %v %v", got, err)
	}
	got, err = ParseGRCReminderInterval("90m")
	if err != nil || got != 90*time.Minute {
		t.Fatalf("explicit interval: %v %v", got, err)
	}
	if _, err = ParseGRCReminderInterval("0s"); err == nil {
		t.Fatal("zero interval must fail")
	}
	if _, err = ParseGRCReminderInterval("-1s"); err == nil {
		t.Fatal("negative interval must fail")
	}
	if _, err = ParseGRCReminderInterval("daily"); err == nil {
		t.Fatal("malformed interval must fail")
	}
}

func TestParseGRCReminderLeaseAndWindows(t *testing.T) {
	lease, err := ParseGRCReminderLease("")
	if err != nil || lease != grc.ReminderLeaseDefault {
		t.Fatalf("default lease: %v %v", lease, err)
	}
	if _, err = ParseGRCReminderLease("0s"); err == nil {
		t.Fatal("zero lease must fail")
	}
	days, err := ParseGRCReminderWindowDays(envGRCReminderEvidenceWindow, "")
	if err != nil || days != grc.DashboardDefaultWindowDays {
		t.Fatalf("default window: %d %v", days, err)
	}
	days, err = ParseGRCReminderWindowDays(envGRCReminderActionWindow, "1")
	if err != nil || days != 1 {
		t.Fatalf("min window: %d %v", days, err)
	}
	days, err = ParseGRCReminderWindowDays(envGRCReminderPolicyWindow, "365")
	if err != nil || days != 365 {
		t.Fatalf("max window: %d %v", days, err)
	}
	if _, err = ParseGRCReminderWindowDays(envGRCReminderEvidenceWindow, "0"); err == nil {
		t.Fatal("window 0 must fail")
	}
	if _, err = ParseGRCReminderWindowDays(envGRCReminderEvidenceWindow, "366"); err == nil {
		t.Fatal("window 366 must fail")
	}
	if _, err = ParseGRCReminderWindowDays(envGRCReminderEvidenceWindow, "x"); err == nil {
		t.Fatal("window text must fail")
	}
}

func TestLoadGRCReminderMicrosoft365ConfigDoesNotEchoSecret(t *testing.T) {
	t.Setenv(envM365TenantID, "")
	t.Setenv(envM365ClientID, "")
	t.Setenv(envM365ClientSecret, "")
	t.Setenv(envM365Sender, "")
	if _, err := loadGRCReminderMicrosoft365Config(); err == nil {
		t.Fatal("missing config must fail")
	}

	const secret = "super-secret-value"
	t.Setenv(envM365TenantID, "11111111-1111-1111-1111-111111111111")
	t.Setenv(envM365ClientID, "client")
	t.Setenv(envM365ClientSecret, secret)
	t.Setenv(envM365Sender, "")
	_, err := loadGRCReminderMicrosoft365Config()
	if err == nil {
		t.Fatal("missing sender must fail")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatal("config error must not include the client secret")
	}
}
