package patient_queue

import (
	"context"
	"log/slog"
	"time"
)

// NotificationWorkerConfig bounds poll/claim behavior (LOT 23N-B).
type NotificationWorkerConfig struct {
	PollInterval time.Duration
	BatchSize    int
	Adapter      NotificationDeliveryAdapter
	Logger       *slog.Logger
}

// NotificationWorker processes claimed LOG intents without mutating appointments.
type NotificationWorker struct {
	svc  *Service
	cfg  NotificationWorkerConfig
	log  *slog.Logger
	adap NotificationDeliveryAdapter
}

func NewNotificationWorker(svc *Service, cfg NotificationWorkerConfig) *NotificationWorker {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = NotificationWorkerPollDefault
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = NotificationClaimBatchDefault
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	adap := cfg.Adapter
	if adap == nil {
		adap = NewLogDeliveryAdapter(NotifChannelLog, log)
	}
	return &NotificationWorker{svc: svc, cfg: cfg, log: log, adap: adap}
}

// Run polls until ctx is cancelled. Recover stale leases each tick, then claim+deliver.
func (w *NotificationWorker) Run(ctx context.Context) error {
	ticker := time.NewTicker(w.cfg.PollInterval)
	defer ticker.Stop()
	for {
		if err := w.Tick(ctx); err != nil {
			w.log.Error("notification_worker_tick", "error", err.Error())
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// Tick runs one recovery + claim + deliver cycle (exported for tests).
func (w *NotificationWorker) Tick(ctx context.Context) error {
	now := time.Now().UTC()
	if _, err := w.svc.RecoverStaleProcessingClaims(now, w.cfg.BatchSize); err != nil {
		return err
	}
	claimed, err := w.svc.ClaimDueNotificationIntents(now, w.cfg.BatchSize)
	if err != nil {
		return err
	}
	for i := range claimed {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		w.processClaimed(ctx, &claimed[i], now)
	}
	return nil
}

func (w *NotificationWorker) processClaimed(ctx context.Context, intent *AppointmentNotificationIntent, now time.Time) {
	payload, err := ParseNotificationPayload(intent.PayloadJSON)
	if err != nil {
		msg := "invalid payload"
		if e := w.svc.failOrRetryAfterAttempt(intent.ID, w.adap.ProviderName(), nil, &msg, now); e != nil {
			w.log.Error("notification_finalize_failure", "intentId", intent.ID, "error", e.Error())
		}
		return
	}
	if skip, reason := w.preSendShouldSkip(intent, payload); skip {
		w.log.Info("appointment_notification_skip",
			"intentId", intent.ID,
			"appointmentId", intent.AppointmentID,
			"kind", intent.Kind,
			"reason", reason,
		)
		msg := reason
		if _, e := w.svc.FinalizeNotificationSkipped(intent.ID, w.adap.ProviderName(), &msg); e != nil {
			w.log.Error("notification_finalize_skipped", "intentId", intent.ID, "error", e.Error())
		}
		return
	}

	res, sendErr := w.adap.Send(ctx, intent, payload)
	if sendErr != nil {
		msg := sendErr.Error()
		if len(msg) > 500 {
			msg = msg[:500]
		}
		if e := w.svc.failOrRetryAfterAttempt(intent.ID, w.adap.ProviderName(), nil, &msg, now); e != nil {
			w.log.Error("notification_finalize_failure", "intentId", intent.ID, "error", e.Error())
		}
		return
	}
	if res.Skipped {
		msg := "adapter skipped"
		if _, e := w.svc.FinalizeNotificationSkipped(intent.ID, w.adap.ProviderName(), &msg); e != nil {
			w.log.Error("notification_finalize_skipped", "intentId", intent.ID, "error", e.Error())
		}
		return
	}
	var pmid *string
	if res.ProviderMessageID != "" {
		id := res.ProviderMessageID
		pmid = &id
	}
	if _, e := w.svc.FinalizeNotificationSent(intent.ID, w.adap.ProviderName(), pmid); e != nil {
		w.log.Error("notification_finalize_sent", "intentId", intent.ID, "error", e.Error())
	}
}

// preSendShouldSkip re-checks appointment authority before adapter I/O (reminders only).
// Lifecycle BOOKED/RESCHEDULED/CANCELLED LOG intents are not skipped for appointment status.
func (w *NotificationWorker) preSendShouldSkip(intent *AppointmentNotificationIntent, payload NotificationPayload) (bool, string) {
	if intent.Kind != NotifKindReminderT24H {
		return false, ""
	}
	var appt Appointment
	if err := w.svc.db.First(&appt, intent.AppointmentID).Error; err != nil {
		return true, "appointment missing"
	}
	if appt.Status == ApptCancelled {
		return true, "appointment cancelled"
	}
	if appt.Status == ApptNoShow {
		return true, "appointment no-show"
	}
	if appt.Status == ApptCompleted {
		return true, "appointment completed"
	}
	wantKey := OccurrenceKeyFromScheduledAt(appt.ScheduledAt)
	if intent.OccurrenceKey != wantKey {
		return true, "occurrence obsolete"
	}
	if payload.AppointmentID != 0 && payload.AppointmentID != appt.ID {
		return true, "payload appointment mismatch"
	}
	return false, ""
}
