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
	// Observer receives Tick/claim/stale/delivery signals (LOT 26I-5B / 5C). Nil → no-op.
	Observer WorkerLoopObserver
	// QueueObserver receives successful queue snapshots (LOT 26I-5D). Nil → no-op.
	QueueObserver NotificationQueueSnapshotObserver
}

// notificationLeaseStore is the recover+claim surface used by Tick (LOT 26I-5B tests).
// Production uses *Service; signatures match RecoverStaleProcessingClaims / ClaimDueNotificationIntents.
type notificationLeaseStore interface {
	RecoverStaleProcessingClaims(asOf time.Time, batch int) (int, error)
	ClaimDueNotificationIntents(asOf time.Time, batch int, channels []string) ([]AppointmentNotificationIntent, error)
}

// notificationQueueSnapshotter is the queue aggregate surface used by Tick (LOT 26I-5D tests).
type notificationQueueSnapshotter interface {
	NotificationQueueSnapshot(ctx context.Context, asOf time.Time) (NotificationQueueSnapshot, error)
}

// notificationDeliveryFinalizer is the finalize surface used by processClaimed (tests).
// Production uses *Service; signatures match existing finalize helpers (unchanged).
type notificationDeliveryFinalizer interface {
	failOrRetryAfterAttempt(intentID uint, provider string, providerMessageID, errMsg *string, now time.Time, providerRetryFloor time.Duration) error
	FinalizeNotificationSent(intentID uint, provider string, providerMessageID *string) (*AppointmentNotificationAttempt, error)
	FinalizeNotificationSkipped(intentID uint, provider string, errMsg *string) (*AppointmentNotificationAttempt, error)
	FinalizeNotificationFailedTerminal(intentID uint, provider string, providerMessageID, errMsg *string) (*AppointmentNotificationAttempt, error)
}

// NotificationWorker processes claimed intents for registered channels only.
type NotificationWorker struct {
	svc           *Service
	lease         notificationLeaseStore // defaults to svc; overridden in Tick unit tests only
	queue         notificationQueueSnapshotter
	finalizer     notificationDeliveryFinalizer
	cfg           NotificationWorkerConfig
	log           *slog.Logger
	adapters      map[string]NotificationDeliveryAdapter
	channels      []string // sorted supported channels for claim
	observer      WorkerLoopObserver
	queueObserver NotificationQueueSnapshotObserver
	// preSendCheck, when non-nil, replaces preSendShouldSkip (unit tests only).
	preSendCheck func(intent *AppointmentNotificationIntent, payload NotificationPayload) (bool, string)
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
	obs := cfg.Observer
	if obs == nil {
		obs = noopWorkerLoopObserver{}
	}
	qObs := cfg.QueueObserver
	if qObs == nil {
		qObs = noopQueueSnapshotObserver{}
	}
	registry, channels, err := validateNotificationAdapters(cfg.Adapters)
	if err != nil {
		return nil, err
	}
	w := &NotificationWorker{
		svc:           svc,
		cfg:           cfg,
		log:           log,
		adapters:      registry,
		channels:      channels,
		observer:      obs,
		queueObserver: qObs,
	}
	if svc != nil {
		w.lease = svc
		w.queue = svc
		w.finalizer = svc
	}
	return w, nil
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
func (w *NotificationWorker) Tick(ctx context.Context) (err error) {
	start := time.Now()
	defer func() {
		w.observer.ObserveTick(time.Since(start), err)
	}()

	now := time.Now().UTC()
	recovered, err := w.lease.RecoverStaleProcessingClaims(now, w.cfg.BatchSize)
	w.observer.ObserveStaleRecovered(recovered)
	if err != nil {
		return err
	}
	claimed, err := w.lease.ClaimDueNotificationIntents(now, w.cfg.BatchSize, w.channels)
	if err != nil {
		return err
	}
	w.observer.ObserveClaimed(len(claimed))
	for i := range claimed {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		w.processClaimed(ctx, &claimed[i], now)
	}
	// LOT 26I-5D: one best-effort queue snapshot after primary Tick work (same asOf).
	// Snapshot failure never becomes Tick error and never zeros gauges.
	w.refreshQueueSnapshot(ctx, now)
	return nil
}

// refreshQueueSnapshot updates queue gauges after recover/claim/process.
// Skipped when ctx is already canceled. Errors are logged only.
func (w *NotificationWorker) refreshQueueSnapshot(ctx context.Context, asOf time.Time) {
	if ctx.Err() != nil {
		return
	}
	if w.queue == nil {
		return
	}
	snap, err := w.queue.NotificationQueueSnapshot(ctx, asOf)
	if err != nil {
		w.log.Error("notification_queue_snapshot", "error", err.Error())
		return
	}
	w.queueObserver.ObserveQueueSnapshot(snap)
}

func (w *NotificationWorker) processClaimed(ctx context.Context, intent *AppointmentNotificationIntent, now time.Time) {
	adap, ok := w.adapters[intent.Channel]
	if !ok {
		msg := "adapter unavailable"
		if e := w.finalizer.failOrRetryAfterAttempt(intent.ID, "worker", nil, &msg, now, 0); e != nil {
			w.log.Error("notification_finalize_failure", "intentId", intent.ID, "error", e.Error())
		}
		// Worker selected adapter_unavailable → retry path (finalize may still fail).
		w.observeDelivery(intent.Channel, DeliveryOutcomeAdapterUnavailable)
		return
	}

	payload, err := ParseNotificationPayload(intent.PayloadJSON)
	if err != nil {
		msg := "invalid payload"
		if e := w.finalizer.failOrRetryAfterAttempt(intent.ID, adap.ProviderName(), nil, &msg, now, 0); e != nil {
			w.log.Error("notification_finalize_failure", "intentId", intent.ID, "error", e.Error())
		}
		w.observeDelivery(intent.Channel, DeliveryOutcomeInvalidPayload)
		return
	}
	if skip, reason := w.shouldPreSendSkip(intent, payload); skip {
		w.log.Info("appointment_notification_skip",
			"intentId", intent.ID,
			"appointmentId", intent.AppointmentID,
			"kind", intent.Kind,
			"reason", reason,
		)
		msg := reason
		if _, e := w.finalizer.FinalizeNotificationSkipped(intent.ID, adap.ProviderName(), &msg); e != nil {
			w.log.Error("notification_finalize_skipped", "intentId", intent.ID, "error", e.Error())
		}
		// Outcome = skip branch selected (not proof finalize TX committed).
		w.observeDelivery(intent.Channel, DeliveryOutcomeSkipped)
		return
	}

	res, sendErr := adap.Send(ctx, intent, payload)
	if sendErr != nil {
		w.finalizeSendError(intent.Channel, intent.ID, adap.ProviderName(), sendErr, now)
		return
	}
	if res.Skipped {
		msg := strings.TrimSpace(res.SkipReason)
		if msg == "" {
			msg = "adapter skipped"
		}
		if _, e := w.finalizer.FinalizeNotificationSkipped(intent.ID, adap.ProviderName(), &msg); e != nil {
			w.log.Error("notification_finalize_skipped", "intentId", intent.ID, "error", e.Error())
		}
		w.observeDelivery(intent.Channel, DeliveryOutcomeSkipped)
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
	if _, e := w.finalizer.FinalizeNotificationSent(intent.ID, adap.ProviderName(), pmid); e != nil {
		w.log.Error("notification_finalize_sent", "intentId", intent.ID, "error", e.Error())
		// Do not record outcome=sent when durable SENT transition failed.
		return
	}
	w.observeDelivery(intent.Channel, DeliveryOutcomeSent)
}

// observeDelivery emits one delivery-attempt outcome when channel maps to a 5C label.
func (w *NotificationWorker) observeDelivery(domainChannel string, outcome DeliveryOutcome) {
	ch, ok := MetricChannelFromDomain(domainChannel)
	if !ok {
		return
	}
	w.observer.ObserveDeliveryAttempt(ch, outcome)
}

// shouldPreSendSkip uses production preSendShouldSkip unless tests override preSendCheck.
func (w *NotificationWorker) shouldPreSendSkip(intent *AppointmentNotificationIntent, payload NotificationPayload) (bool, string) {
	if w.preSendCheck != nil {
		return w.preSendCheck(intent, payload)
	}
	return w.preSendShouldSkip(intent, payload)
}

// notificationAmbiguousDeliveryReason is the privacy-safe attempt error persisted
// for ErrAmbiguousDelivery (no recipient, body, clinical content, or provider payload).
const notificationAmbiguousDeliveryReason = "delivery outcome ambiguous"

func (w *NotificationWorker) finalizeSendError(domainChannel string, intentID uint, provider string, sendErr error, now time.Time) {
	// Ambiguous before context: a transport may wrap ErrAmbiguousDelivery around a
	// deadline/cancel after dispatch. That must be terminal FAILED, not leave-PROCESSING.
	if errors.Is(sendErr, email.ErrAmbiguousDelivery) {
		msg := notificationAmbiguousDeliveryReason
		_, e := w.finalizer.FinalizeNotificationFailedTerminal(intentID, provider, nil, &msg)
		if e != nil {
			w.log.Error("notification_finalize_failure", "intentId", intentID, "error", e.Error())
		}
		w.observeDelivery(domainChannel, DeliveryOutcomeAmbiguous)
		return
	}
	if errors.Is(sendErr, context.Canceled) || errors.Is(sendErr, context.DeadlineExceeded) {
		// Leave PROCESSING; stale lease recovery will reclaim.
		// Generic context failures are NOT auto-promoted to ambiguous (LOT 26H-2):
		// pre-dispatch cancel must not become systematic message loss.
		w.observeDelivery(domainChannel, DeliveryOutcomeCanceled)
		return
	}
	msg := sendErr.Error()
	if len(msg) > 500 {
		msg = msg[:500]
	}
	switch {
	case errors.Is(sendErr, email.ErrPermanent):
		_, e := w.finalizer.FinalizeNotificationFailedTerminal(intentID, provider, nil, &msg)
		if e != nil {
			w.log.Error("notification_finalize_failure", "intentId", intentID, "error", e.Error())
		}
		w.observeDelivery(domainChannel, DeliveryOutcomePermanent)
		return
	case errors.Is(sendErr, email.ErrInvalidMessage):
		_, e := w.finalizer.FinalizeNotificationFailedTerminal(intentID, provider, nil, &msg)
		if e != nil {
			w.log.Error("notification_finalize_failure", "intentId", intentID, "error", e.Error())
		}
		w.observeDelivery(domainChannel, DeliveryOutcomeInvalidMessage)
		return
	case errors.Is(sendErr, email.ErrNotConfigured):
		_, e := w.finalizer.FinalizeNotificationFailedTerminal(intentID, provider, nil, &msg)
		if e != nil {
			w.log.Error("notification_finalize_failure", "intentId", intentID, "error", e.Error())
		}
		w.observeDelivery(domainChannel, DeliveryOutcomeNotConfigured)
		return
	default:
		// Transient, unknown, and ErrTransient → existing retry/backoff.
		// Provider Retry-After (if any) is a floor only (LOT 26H-4).
		hint, _ := email.RetryAfter(sendErr)
		e := w.finalizer.failOrRetryAfterAttempt(intentID, provider, nil, &msg, now, hint)
		if e != nil {
			w.log.Error("notification_finalize_failure", "intentId", intentID, "error", e.Error())
		}
		w.observeDelivery(domainChannel, DeliveryOutcomeTransient)
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
