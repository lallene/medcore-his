package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/grc"
	"github.com/lallene/medcore-his/backend/internal/shared/email/microsoft365"
)

const (
	envGRCReminderInterval       = "GRC_REMINDER_INTERVAL"
	envGRCReminderEvidenceWindow = "GRC_REMINDER_EVIDENCE_WINDOW_DAYS"
	envGRCReminderActionWindow   = "GRC_REMINDER_ACTION_WINDOW_DAYS"
	envGRCReminderPolicyWindow   = "GRC_REMINDER_POLICY_WINDOW_DAYS"
	envGRCReminderLease          = "GRC_REMINDER_LEASE"

	envM365TenantID     = "MEDCORE_M365_TENANT_ID"
	envM365ClientID     = "MEDCORE_M365_CLIENT_ID"
	envM365ClientSecret = "MEDCORE_M365_CLIENT_SECRET"
	envM365Sender       = "MEDCORE_M365_SENDER"
)

// ParseGRCReminderInterval reads a Go duration. Empty means one hour.
// The value must be > 0. The production default is one hour; tests call
// RunOnce directly and do not wait for this interval.
func ParseGRCReminderInterval(raw string) (time.Duration, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return grc.ReminderIntervalDefault, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%s: durée Go positive attendue", envGRCReminderInterval)
	}
	return d, nil
}

// ParseGRCReminderLease reads a Go duration. Empty means 15 minutes.
func ParseGRCReminderLease(raw string) (time.Duration, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return grc.ReminderLeaseDefault, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%s: durée Go positive attendue", envGRCReminderLease)
	}
	return d, nil
}

// ParseGRCReminderWindowDays reads one window. Empty means 30 days.
// Explicit values must be integers in 1..365.
func ParseGRCReminderWindowDays(envName, raw string) (int, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return grc.DashboardDefaultWindowDays, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("%s: entier attendu", envName)
	}
	if err := grc.ValidateReminderWindowDays(n); err != nil {
		return 0, fmt.Errorf("%s: %w", envName, err)
	}
	return n, nil
}

func reminderWindowsFromEnv() (grc.ReminderWindows, error) {
	evidence, err := ParseGRCReminderWindowDays(envGRCReminderEvidenceWindow, os.Getenv(envGRCReminderEvidenceWindow))
	if err != nil {
		return grc.ReminderWindows{}, err
	}
	action, err := ParseGRCReminderWindowDays(envGRCReminderActionWindow, os.Getenv(envGRCReminderActionWindow))
	if err != nil {
		return grc.ReminderWindows{}, err
	}
	policy, err := ParseGRCReminderWindowDays(envGRCReminderPolicyWindow, os.Getenv(envGRCReminderPolicyWindow))
	if err != nil {
		return grc.ReminderWindows{}, err
	}
	return grc.ReminderWindows{EvidenceDays: evidence, ActionDays: action, PolicyDays: policy}, nil
}

// loadGRCReminderMicrosoft365Config reads the same Graph env contract as the
// appointment notification worker. It does not reuse appointment intents.
// The secret value is never included in the error text.
func loadGRCReminderMicrosoft365Config() (microsoft365.Config, error) {
	tenant := strings.TrimSpace(os.Getenv(envM365TenantID))
	if tenant == "" {
		return microsoft365.Config{}, fmt.Errorf("%s est requis pour le worker de rappels GRC", envM365TenantID)
	}
	clientID := strings.TrimSpace(os.Getenv(envM365ClientID))
	if clientID == "" {
		return microsoft365.Config{}, fmt.Errorf("%s est requis pour le worker de rappels GRC", envM365ClientID)
	}
	secret := os.Getenv(envM365ClientSecret)
	if strings.TrimSpace(secret) == "" {
		return microsoft365.Config{}, fmt.Errorf("%s est requis pour le worker de rappels GRC", envM365ClientSecret)
	}
	sender := strings.TrimSpace(os.Getenv(envM365Sender))
	if sender == "" {
		return microsoft365.Config{}, fmt.Errorf("%s est requis pour le worker de rappels GRC", envM365Sender)
	}
	cfg := microsoft365.Config{
		TenantID:     tenant,
		ClientID:     clientID,
		ClientSecret: secret,
		Sender:       sender,
	}
	if err := cfg.Validate(); err != nil {
		return microsoft365.Config{}, err
	}
	return cfg, nil
}
