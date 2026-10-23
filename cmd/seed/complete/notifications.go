package complete

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/patient_queue"
)

// Appointment notifications. Intents are created by the agenda service itself when future
// appointments are booked / rescheduled / cancelled (channels LOG and EMAIL: the seeder wires
// the queue service with email enabled, as a production instance would). SMS is not a
// lifecycle channel of the application: no SMS intent exists.
//
// At the end of the run the real notification worker processes the intents that are due
// at the present instant, with SAFE adapters only — nothing ever leaves the machine:
//   - LOG   → the application's LogDeliveryAdapter      → SENT
//   - EMAIL → demoEmailAdapter (no network): half "noop" → SKIPPED, half a labelled
//     provider outage → FAILED (retry then terminal), exercising the worker's error paths.
// Future intents stay PENDING; intents of cancelled appointments are CANCELLED by the
// lifecycle. One claimed intent is deliberately left PROCESSING (interrupted worker).

type demoEmailAdapter struct{ n int }

func (a *demoEmailAdapter) Channel() string      { return patient_queue.NotifChannelEmail }
func (a *demoEmailAdapter) ProviderName() string { return "demo-no-network" }
func (a *demoEmailAdapter) Send(_ context.Context, intent *patient_queue.AppointmentNotificationIntent, _ patient_queue.NotificationPayload) (patient_queue.DeliveryResult, error) {
	a.n++
	if intent != nil && intent.ID%9 == 0 { // persistent outage for these recipients: retried, then FAILED
		return patient_queue.DeliveryResult{}, errors.New("DEMO: fournisseur e-mail indisponible (simulation, aucun envoi)")
	}
	return patient_queue.DeliveryResult{Skipped: true}, nil
}

type notificationPlanner struct{}

func (notificationPlanner) Name() string         { return "Notifications de rendez-vous" }
func (notificationPlanner) Setup(w *World) error { return nil }

// PlanDay: the worker runs every evening on the day's due intents (booking confirmations,
// reschedule / cancellation notices, retries).
func (notificationPlanner) PlanDay(w *World, d Day) {
	w.Sched.At(d.At(23, 50), "notifications.worker-quotidien", func(ctx *StepContext) error { return runWorker(ctx.W, ctx.Sim, 40) })
}

var emailAdapter = &demoEmailAdapter{}

func runWorker(w *World, asOf time.Time, ticks int) error {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	worker, err := patient_queue.NewNotificationWorker(w.Svc.Queue, patient_queue.NotificationWorkerConfig{BatchSize: 50, Logger: logger,
		Adapters: map[string]patient_queue.NotificationDeliveryAdapter{
			patient_queue.NotifChannelLog:   patient_queue.NewLogDeliveryAdapter(patient_queue.NotifChannelLog, logger),
			patient_queue.NotifChannelEmail: emailAdapter,
		}})
	if err != nil {
		return err
	}
	for i := 0; i < ticks; i++ {
		var due int64
		w.DB.Table("appointment_notification_intents").Where("status = ? AND send_after <= ?", patient_queue.NotifStatusPending, asOf).Count(&due)
		if due == 0 {
			break
		}
		if err := worker.Tick(context.Background()); err != nil {
			return err
		}
	}
	return nil
}

func processNotifications(w *World) error {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	worker, err := patient_queue.NewNotificationWorker(w.Svc.Queue, patient_queue.NotificationWorkerConfig{BatchSize: 50, Logger: logger,
		Adapters: map[string]patient_queue.NotificationDeliveryAdapter{
			patient_queue.NotifChannelLog:   patient_queue.NewLogDeliveryAdapter(patient_queue.NotifChannelLog, logger),
			patient_queue.NotifChannelEmail: &demoEmailAdapter{},
		}})
	if err != nil {
		return err
	}
	// An interrupted worker: one due LOG intent claimed and never finalised (PROCESSING).
	if _, err := w.Svc.Queue.ClaimDueNotificationIntents(w.Horizon.UTC(), 1, []string{patient_queue.NotifChannelLog}); err != nil {
		return err
	}
	for i := 0; i < 6; i++ {
		if err := worker.Tick(context.Background()); err != nil {
			return err
		}
	}
	return nil
}
