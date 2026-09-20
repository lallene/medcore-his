package patient_queue

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/lallene/medcore-his/backend/internal/shared/email"
)

// NotificationWorkerConfig bounds poll/claim behavior (LOT 23N-B / 26F-3).
type NotificationWorkerConfig struct {
	PollInterval time.Duration
	BatchSize    int
	// Adapters maps channel → delivery adapter. Keys must equal adapter.Channel().
	Adapters map[string]NotificationDeliveryAdapter
	Logger   *slog.Logger
}

// NotificationWorker processes claimed intents for registered channels only.
type NotificationWorker struct {
	svc      *Service
	cfg      NotificationWorkerConfig
	log      *slog.Logger
	adapters map[string]NotificationDeliveryAdapter
	channels []string // sorted supported channels for claim
}

// NewNotificationWorker validates the adapter registry and returns a worker.
func NewNotificationWorker(svc *Service, cfg NotificationWorkerConfig) (*NotificationWorker, error) {
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
	registry, channels, err := validateNotificationAdapters(cfg.Adapters)
	if err != nil {
		return nil, err
	}
	return &NotificationWorker{
		svc:      svc,
		cfg:      cfg,
		log:      log,
		adapters: registry,
		channels: channels,
	}, nil
}

func validateNotificationAdapters(adapters map[string]NotificationDeliveryAdapter) (map[string]NotificationDeliveryAdapter, []string, error) {
	if len(adapters) == 0 {
		return nil, nil, errors.New("notification worker: adapters required")
	}
	registry := make(map[string]NotificationDeliveryAdapter, len(adapters))
	for key, ad := range adapters {
		if ad == nil {
			return nil, nil, errors.New("notification worker: nil adapter")
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, nil, errors.New("notification worker: empty adapter key")
		}
		ch := strings.TrimSpace(ad.Channel())
		if ch == "" {
			return nil, nil, errors.New("notification worker: empty adapter channel")
		}
		if key != ch {
			return nil, nil, fmt.Errorf("notification worker: adapter key %q != Channel() %q", key, ch)
		}
		registry[ch] = ad
	}
	channels := make([]string, 0, len(registry))
	for ch := range registry {
		channels = append(channels, ch)
	}
	sort.Strings(channels)
	return registry, channels, nil
}

// SupportedChannels returns the sorted channel list used for claim filtering.
func (w *NotificationWorker) SupportedChannels() []string {
	out := make([]string, len(w.channels))
	copy(out, w.channels)
	return out
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
	claimed, err := w.svc.ClaimDueNotificationIntents(now, w.cfg.BatchSize, w.channels)
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
	adap, ok := w.adapters[intent.Channel]
	if !ok {
		msg := "adapter unavailable"
		if e := w.svc.failOrRetryAfterAttempt(intent.ID, "worker", nil, &msg, now, 0); e != nil {
			w.log.Error("notification_finalize_failure", "intentId", intent.ID, "error", e.Error())
		}
		return
	}

	payload, err := ParseNotificationPayload(intent.PayloadJSON)
	if err != nil {
		msg := "invalid payload"
		if e := w.svc.failOrRetryAfterAttempt(intent.ID, adap.ProviderName(), nil, &msg, now, 0); e != nil {
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
		if _, e := w.svc.FinalizeNotificationSkipped(intent.ID, adap.ProviderName(), &msg); e != nil {
			w.log.Error("notification_finalize_skipped", "intentId", intent.ID, "error", e.Error())
		}
		return
	}

	res, sendErr := adap.Send(ctx, intent, payload)
	if sendErr != nil {
		w.finalizeSendError(intent.ID, adap.ProviderName(), sendErr, now)
		return
	}
	if res.Skipped {
		msg := strings.TrimSpace(res.SkipReason)
		if msg == "" {
			msg = "adapter skipped"
		}
		if _, e := w.svc.FinalizeNotificationSkipped(intent.ID, adap.ProviderName(), &msg); e != nil {
			w.log.Error("notification_finalize_skipped", "intentId", intent.ID, "error", e.Error())
		}
		return
	}
	var pmid *string
	if res.ProviderMessageID != "" {
		id := res.ProviderMessageID
		pmid = &id
	}
	// Residual 26H window (not closed by ErrAmbiguousDelivery alone): if Send
	// returned success (provider accepted) but FinalizeNotificationSent fails or
	// the process crashes before that TX commits, the intent stays PROCESSING
	// with no durable "provider accepted" marker. Stale recovery may still
	// re-Send. There is no distributed transaction with the provider; exactly-once
	// is not guaranteed. Closing that gap needs a dedicated durable-ack follow-up.
	if _, e := w.svc.FinalizeNotificationSent(intent.ID, adap.ProviderName(), pmid); e != nil {
		w.log.Error("notification_finalize_sent", "intentId", intent.ID, "error", e.Error())
	}
}

// notificationAmbiguousDeliveryReason is the privacy-safe attempt error persisted
// for ErrAmbiguousDelivery (no recipient, body, clinical content, or provider payload).
const notificationAmbiguousDeliveryReason = "delivery outcome ambiguous"

func (w *NotificationWorker) finalizeSendError(intentID uint, provider string, sendErr error, now time.Time) {
	// Ambiguous before context: a transport may wrap ErrAmbiguousDelivery around a
	// deadline/cancel after dispatch. That must be terminal FAILED, not leave-PROCESSING.
	if errors.Is(sendErr, email.ErrAmbiguousDelivery) {
		msg := notificationAmbiguousDeliveryReason
		_, e := w.svc.FinalizeNotificationFailedTerminal(intentID, provider, nil, &msg)
		if e != nil {
			w.log.Error("notification_finalize_failure", "intentId", intentID, "error", e.Error())
		}
		return
	}
	if errors.Is(sendErr, context.Canceled) || errors.Is(sendErr, context.DeadlineExceeded) {
		// Leave PROCESSING; stale lease recovery will reclaim.
		// Generic context failures are NOT auto-promoted to ambiguous (LOT 26H-2):
		// pre-dispatch cancel must not become systematic message loss.
		return
	}
	msg := sendErr.Error()
	if len(msg) > 500 {
		msg = msg[:500]
	}
	terminal := errors.Is(sendErr, email.ErrPermanent) ||
		errors.Is(sendErr, email.ErrInvalidMessage) ||
		errors.Is(sendErr, email.ErrNotConfigured)
	var e error
	if terminal {
		_, e = w.svc.FinalizeNotificationFailedTerminal(intentID, provider, nil, &msg)
	} else {
		// Transient, unknown, and ErrTransient → existing retry/backoff.
		// Provider Retry-After (if any) is a floor only (LOT 26H-4).
		hint, _ := email.RetryAfter(sendErr)
		e = w.svc.failOrRetryAfterAttempt(intentID, provider, nil, &msg, now, hint)
	}
	if e != nil {
		w.log.Error("notification_finalize_failure", "intentId", intentID, "error", e.Error())
	}
}

// preSendShouldSkip re-checks appointment authority before adapter I/O (reminders only).
// Lifecycle BOOKED/RESCHEDULED/CANCELLED intents are not skipped for appointment status.
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
