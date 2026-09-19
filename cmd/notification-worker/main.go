package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lallene/medcore-his/backend/internal/config"
	"github.com/lallene/medcore-his/backend/internal/core/logger"
	"github.com/lallene/medcore-his/backend/internal/database"
	"github.com/lallene/medcore-his/backend/internal/modules/patient_queue"
)

// LOT 23N-B — dedicated appointment notification worker (LOG channel only).
// Does not start inside the API process. Graceful SIGINT/SIGTERM shutdown.
func main() {
	cfg := config.Load()
	logger.Init(cfg.AppEnv)
	log := slog.Default()

	db := database.Connect(cfg.DatabaseURL, cfg.BusinessTimezone)
	if err := db.AutoMigrate(
		&patient_queue.AppointmentNotificationIntent{},
		&patient_queue.AppointmentNotificationAttempt{},
	); err != nil {
		log.Error("notification worker migrate", "error", err)
		os.Exit(1)
	}
	if err := patient_queue.EnsureNotificationIndexes(db); err != nil {
		log.Error("notification worker indexes", "error", err)
		os.Exit(1)
	}

	svc := patient_queue.NewService(db)
	logAdapter := patient_queue.NewLogDeliveryAdapter(patient_queue.NotifChannelLog, log)
	worker, err := patient_queue.NewNotificationWorker(svc, patient_queue.NotificationWorkerConfig{
		PollInterval: envDuration("NOTIFICATION_WORKER_POLL", patient_queue.NotificationWorkerPollDefault),
		BatchSize:    patient_queue.NotificationClaimBatchDefault,
		Adapters: map[string]patient_queue.NotificationDeliveryAdapter{
			patient_queue.NotifChannelLog: logAdapter,
		},
		Logger: log,
	})
	if err != nil {
		log.Error("notification worker config", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Info("notification worker started",
		"poll", workerPollLabel(),
		"channels", worker.SupportedChannels(),
	)
	if err := worker.Run(ctx); err != nil && err != context.Canceled {
		log.Error("notification worker stopped", "error", err)
		os.Exit(1)
	}
	log.Info("notification worker shutdown complete")
}

func envDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return fallback
	}
	return d
}

func workerPollLabel() string {
	return envDuration("NOTIFICATION_WORKER_POLL", patient_queue.NotificationWorkerPollDefault).String()
}
