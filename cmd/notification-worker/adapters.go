package main

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/lallene/medcore-his/backend/internal/modules/patient_queue"
	"github.com/lallene/medcore-his/backend/internal/shared/email/microsoft365"
	"gorm.io/gorm"
)

const (
	envM365TenantID     = "MEDCORE_M365_TENANT_ID"
	envM365ClientID     = "MEDCORE_M365_CLIENT_ID"
	envM365ClientSecret = "MEDCORE_M365_CLIENT_SECRET"
	envM365Sender       = "MEDCORE_M365_SENDER"
)

// loadWorkerMicrosoft365Config loads Graph bootstrap settings for the notification-worker only.
// Call only when MEDCORE_NOTIFICATION_EMAIL_ENABLED is true. Never logs secret values.
func loadWorkerMicrosoft365Config() (microsoft365.Config, error) {
	tenant := strings.TrimSpace(os.Getenv(envM365TenantID))
	if tenant == "" {
		return microsoft365.Config{}, fmt.Errorf("%s is required when notification email is enabled", envM365TenantID)
	}
	clientID := strings.TrimSpace(os.Getenv(envM365ClientID))
	if clientID == "" {
		return microsoft365.Config{}, fmt.Errorf("%s is required when notification email is enabled", envM365ClientID)
	}
	// Keep exact secret bytes; do not TrimSpace mid-secret (26E contract).
	secret := os.Getenv(envM365ClientSecret)
	if strings.TrimSpace(secret) == "" {
		return microsoft365.Config{}, fmt.Errorf("%s is required when notification email is enabled", envM365ClientSecret)
	}
	sender := strings.TrimSpace(os.Getenv(envM365Sender))
	if sender == "" {
		return microsoft365.Config{}, fmt.Errorf("%s is required when notification email is enabled", envM365Sender)
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

// buildNotificationDeliveryAdapters constructs the worker adapter registry.
// When emailEnabled is false, M365 env is ignored and only LOG is registered.
// When emailEnabled is true, full M365 config is required; construction fails closed (no LOG-only fallback).
func buildNotificationDeliveryAdapters(
	emailEnabled bool,
	db *gorm.DB,
	log *slog.Logger,
) (map[string]patient_queue.NotificationDeliveryAdapter, error) {
	if log == nil {
		log = slog.Default()
	}
	adapters := map[string]patient_queue.NotificationDeliveryAdapter{
		patient_queue.NotifChannelLog: patient_queue.NewLogDeliveryAdapter(patient_queue.NotifChannelLog, log),
	}
	if !emailEnabled {
		return adapters, nil
	}
	if db == nil {
		return nil, fmt.Errorf("database required for email delivery adapter")
	}
	m365Cfg, err := loadWorkerMicrosoft365Config()
	if err != nil {
		return nil, err
	}
	transport, err := microsoft365.New(m365Cfg)
	if err != nil {
		return nil, err
	}
	reader := patient_queue.NewGormPatientEmailReader(db)
	adapters[patient_queue.NotifChannelEmail] = patient_queue.NewEmailDeliveryAdapter(transport, reader)
	return adapters, nil
}
