package main

import (
	"context"
	"fmt"
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

// Dedicated appointment notification worker (LOG always; EMAIL when feature flag + M365 configured).
// Does not start inside the API process. Graceful SIGINT/SIGTERM shutdown.
// Graph client secret is worker-only (never required by the API).
// Schema ownership is cmd/migrate only (LOT 26I-3) — no AutoMigrate / Ensure* at startup.
// Health/readiness: GET /healthz and GET /readyz on NOTIFICATION_WORKER_HEALTH_PORT (LOT 26I-4).
func main() {
	cfg := config.Load()
	logger.Init(cfg.AppEnv)
	log := slog.Default()

	poll, err := ParseNotificationWorkerPoll(os.Getenv(EnvNotificationWorkerPoll), patient_queue.NotificationWorkerPollDefault)
	if err != nil {
		log.Error("notification worker config", "error", err)
		os.Exit(1)
	}

	healthPort, err := ParseNotificationWorkerHealthPort(os.Getenv(EnvNotificationWorkerHealthPort))
	if err != nil {
		log.Error("notification worker config", "error", err)
		os.Exit(1)
	}

	db := database.Connect(cfg.DatabaseURL, cfg.BusinessTimezone)
	sqlDB, err := db.DB()
	if err != nil {
		log.Error("notification worker database", "error", "sql handle unavailable")
		os.Exit(1)
	}

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

	healthState := &HealthState{}
	healthAddr := fmt.Sprintf("0.0.0.0:%d", healthPort)
	healthSrv := NewHealthServer(healthAddr, healthState, PingFromSQLDB(sqlDB))
	ln, err := healthSrv.Listen()
	if err != nil {
		log.Error("notification worker health listen", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	healthErrCh := make(chan error, 1)
	go func() {
		serveErr := healthSrv.Serve(ln)
		healthErrCh <- serveErr
		// Fail closed if the health server dies while the worker is still running.
		if serveErr != nil {
			healthState.MarkShuttingDown()
			cancel()
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	go func() {
		select {
		case <-sigCh:
			// Readiness fails immediately on signal; then cancel worker context.
			healthState.MarkShuttingDown()
			cancel()
		case <-ctx.Done():
		}
	}()

	log.Info("notification worker started",
		"poll", poll.String(),
		"channels", worker.SupportedChannels(),
		"emailEnabled", cfg.NotificationEmailEnabled,
		"healthPort", healthPort,
	)

	healthState.MarkStarted()
	runErr := worker.Run(ctx)
	healthState.MarkStopped()
	healthState.MarkShuttingDown()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := healthSrv.Shutdown(shutdownCtx); err != nil {
		log.Error("notification worker health shutdown", "error", err)
	}

	var healthServeErr error
	select {
	case healthServeErr = <-healthErrCh:
	case <-time.After(5 * time.Second):
		log.Error("notification worker health server", "error", "shutdown timeout")
		os.Exit(1)
	}
	if healthServeErr != nil {
		log.Error("notification worker health server", "error", healthServeErr)
		os.Exit(1)
	}

	if runErr != nil && runErr != context.Canceled {
		log.Error("notification worker stopped", "error", runErr)
		os.Exit(1)
	}
	log.Info("notification worker shutdown complete")
}
