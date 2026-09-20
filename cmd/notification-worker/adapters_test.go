package main

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/patient_queue"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const testTenantGUID = "11111111-1111-1111-1111-111111111111"

func clearM365Env(t *testing.T) {
	t.Helper()
	for _, k := range []string{envM365TenantID, envM365ClientID, envM365ClientSecret, envM365Sender} {
		t.Setenv(k, "")
	}
}

func setValidM365Env(t *testing.T, secret string) {
	t.Helper()
	t.Setenv(envM365TenantID, testTenantGUID)
	t.Setenv(envM365ClientID, "test-client-id")
	t.Setenv(envM365ClientSecret, secret)
	t.Setenv(envM365Sender, "noreply@example.com")
}

func memDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestBuildAdaptersEmailDisabledIgnoresM365(t *testing.T) {
	clearM365Env(t)
	t.Setenv(envM365TenantID, "!!!not-a-tenant!!!")
	t.Setenv(envM365Sender, "not an addr")

	adapters, err := buildNotificationDeliveryAdapters(false, nil, slog.Default(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(adapters) != 1 || adapters[patient_queue.NotifChannelLog] == nil {
		t.Fatalf("adapters=%v", adapters)
	}
	if _, ok := adapters[patient_queue.NotifChannelEmail]; ok {
		t.Fatal("EMAIL must be absent when disabled")
	}

	w, err := patient_queue.NewNotificationWorker(patient_queue.NewService(nil), patient_queue.NotificationWorkerConfig{
		Adapters: adapters,
	})
	if err != nil {
		t.Fatal(err)
	}
	ch := w.SupportedChannels()
	if len(ch) != 1 || ch[0] != patient_queue.NotifChannelLog {
		t.Fatalf("SupportedChannels=%v", ch)
	}
}

func TestBuildAdaptersEmailEnabledValid(t *testing.T) {
	setValidM365Env(t, "dummy-client-secret")
	adapters, err := buildNotificationDeliveryAdapters(true, memDB(t), slog.Default(), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if adapters[patient_queue.NotifChannelLog] == nil || adapters[patient_queue.NotifChannelEmail] == nil {
		t.Fatalf("want LOG+EMAIL, got %v", adapters)
	}
	w, err := patient_queue.NewNotificationWorker(patient_queue.NewService(nil), patient_queue.NotificationWorkerConfig{
		Adapters: adapters,
	})
	if err != nil {
		t.Fatal(err)
	}
	ch := w.SupportedChannels()
	// Worker registry sorts alphabetically: EMAIL, LOG
	if len(ch) != 2 || ch[0] != patient_queue.NotifChannelEmail || ch[1] != patient_queue.NotifChannelLog {
		t.Fatalf("SupportedChannels=%v want [EMAIL LOG]", ch)
	}
}

func TestBuildAdaptersEmailEnabledNilBusinessLocation(t *testing.T) {
	setValidM365Env(t, "dummy-client-secret")
	_, err := buildNotificationDeliveryAdapters(true, memDB(t), slog.Default(), nil)
	if err == nil {
		t.Fatal("expected error when business location is nil")
	}
	if !strings.Contains(err.Error(), "business location") {
		t.Fatalf("error=%v", err)
	}
}

func TestBuildAdaptersEmailEnabledMissingFields(t *testing.T) {
	secret := "sekrit-LEAK-TEST-XYZ-26F5"
	base := func(t *testing.T) {
		t.Helper()
		setValidM365Env(t, secret)
	}

	cases := []struct {
		name string
		mut  func(t *testing.T)
		key  string
	}{
		{"missing_tenant", func(t *testing.T) { t.Setenv(envM365TenantID, "") }, envM365TenantID},
		{"missing_client_id", func(t *testing.T) { t.Setenv(envM365ClientID, "") }, envM365ClientID},
		{"missing_secret", func(t *testing.T) { t.Setenv(envM365ClientSecret, "") }, envM365ClientSecret},
		{"missing_sender", func(t *testing.T) { t.Setenv(envM365Sender, "") }, envM365Sender},
		{"invalid_tenant", func(t *testing.T) { t.Setenv(envM365TenantID, "not a tenant") }, ""},
		{"invalid_sender", func(t *testing.T) { t.Setenv(envM365Sender, "Display Name <a@b.co>") }, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base(t)
			tc.mut(t)
			_, err := buildNotificationDeliveryAdapters(true, memDB(t), slog.Default(), time.UTC)
			if err == nil {
				t.Fatal("expected error")
			}
			if tc.key != "" && !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("error should name %s: %v", tc.key, err)
			}
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("error leaked secret")
			}
		})
	}
}

func TestLoadWorkerMicrosoft365ConfigSecretNotInError(t *testing.T) {
	secret := "sekrit-LEAK-TEST-XYZ-26F5"
	setValidM365Env(t, secret)
	t.Setenv(envM365TenantID, "bad tenant!!")
	_, err := loadWorkerMicrosoft365Config()
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatal("error leaked secret")
	}
}
