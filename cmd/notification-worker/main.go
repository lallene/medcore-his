package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/lallene/medcore-his/backend/internal/config"
	"github.com/lallene/medcore-his/backend/internal/core/logger"
	"github.com/lallene/medcore-his/backend/internal/database"
	"github.com/lallene/medcore-his/backend/internal/modules/patient_queue"
)

// EnvNotificationWorkerPoll is the worker-only poll interval (Go duration).
const EnvNotificationWorkerPoll = "NOTIFICATION_WORKER_POLL"

// Dedicated appointment notification worker (LOG always; EMAIL when feature flag + M365 configured).
// Does not start inside the API process. Graceful SIGINT/SIGTERM shutdown.
// Graph client secret is worker-only (never required by the API).
// Schema ownership is cmd/migrate only (LOT 26I-3) — no AutoMigrate / Ensure* at startup.
func main() {
	cfg := config.Load()
	logger.Init(cfg.AppEnv)
	log := slog.Default()

	poll, err := ParseNotificationWorkerPoll(os.Getenv(EnvNotificationWorkerPoll), patient_queue.NotificationWorkerPollDefault)
	if err != nil {
		log.Error("notification worker config", "error", err)
		os.Exit(1)
	}

	db := database.Connect(cfg.DatabaseURL, cfg.BusinessTimezone)

	adapters, err := buildNotificationDeliveryAdapters(
		cfg.NotificationEmailEnabled,
		db,
		log,
		cfg.BusinessLocation(),
	)
	if err != nil {
		log.Error("notification worker adapters", "error", err)
		os.Exit(1)
	}

	svc := patient_queue.NewService(db)
	worker, err := patient_queue.NewNotificationWorker(svc, patient_queue.NotificationWorkerConfig{
		PollInterval: poll,
		BatchSize:    patient_queue.NotificationClaimBatchDefault,
		Adapters:     adapters,
		Logger:       log,
	})
	if err != nil {
		log.Error("notification worker config", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Info("notification worker started",
		"poll", poll.String(),
		"channels", worker.SupportedChannels(),
		"emailEnabled", cfg.NotificationEmailEnabled,
	)
	if err := worker.Run(ctx); err != nil && err != context.Canceled {
		log.Error("notification worker stopped", "error", err)
		os.Exit(1)
	}
	log.Info("notification worker shutdown complete")
}

// ParseNotificationWorkerPoll interprets NOTIFICATION_WORKER_POLL.
// Unset/empty/whitespace → fallback (typically 2s). Explicit malformed or
// non-positive values return an error (fail closed; never silent fallback).
func ParseNotificationWorkerPoll(raw string, fallback time.Duration) (time.Duration, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("%s: valeur invalide (durée Go positive attendue, ex. 2s)", EnvNotificationWorkerPoll)
	}
	if d <= 0 {
		return 0, fmt.Errorf("%s: durée non positive (durée Go positive attendue, ex. 2s)", EnvNotificationWorkerPoll)
	}
	return d, nil
}
