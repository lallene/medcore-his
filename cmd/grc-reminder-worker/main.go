package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/lallene/medcore-his/backend/internal/config"
	"github.com/lallene/medcore-his/backend/internal/core/logger"
	"github.com/lallene/medcore-his/backend/internal/database"
	"github.com/lallene/medcore-his/backend/internal/modules/grc"
	"github.com/lallene/medcore-his/backend/internal/shared/email/microsoft365"
)

// Dedicated GRC reminder worker. It does not start inside the API process.
// Microsoft 365 must be configured or the process exits before any delivery.
// Schema ownership stays in cmd/migrate.
func main() {
	cfg := config.Load()
	logger.Init(cfg.AppEnv)
	log := slog.Default()

	interval, err := ParseGRCReminderInterval(os.Getenv(envGRCReminderInterval))
	if err != nil {
		log.Error("grc reminder worker config", "error", err)
		os.Exit(1)
	}
	lease, err := ParseGRCReminderLease(os.Getenv(envGRCReminderLease))
	if err != nil {
		log.Error("grc reminder worker config", "error", err)
		os.Exit(1)
	}
	windows, err := reminderWindowsFromEnv()
	if err != nil {
		log.Error("grc reminder worker config", "error", err)
		os.Exit(1)
	}
	m365Cfg, err := loadGRCReminderMicrosoft365Config()
	if err != nil {
		log.Error("grc reminder worker config", "error", err)
		os.Exit(1)
	}
	transport, err := microsoft365.New(m365Cfg)
	if err != nil {
		log.Error("grc reminder worker transport", "error", err)
		os.Exit(1)
	}

	db := database.Connect(cfg.DatabaseURL, cfg.BusinessTimezone)
	worker, err := grc.NewReminderWorker(grc.NewService(db), transport, windows, lease, log)
	if err != nil {
		log.Error("grc reminder worker config", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	go func() {
		select {
		case <-sigCh:
			cancel()
		case <-ctx.Done():
		}
	}()

	log.Info("grc reminder worker started", "interval", interval.String(), "lease", lease.String())
	runErr := worker.Run(ctx, interval)
	if runErr != nil && runErr != context.Canceled {
		log.Error("grc reminder worker stopped", "error", runErr)
		os.Exit(1)
	}
	log.Info("grc reminder worker shutdown complete")
}
