package patient_queue

import (
	"context"
	"log/slog"
)

// DeliveryResult is the adapter outcome without PHI.
type DeliveryResult struct {
	ProviderMessageID string
	Skipped           bool
	// SkipReason is a generic technical reason when Skipped is true (no PHI / no addresses).
	// Consumed by the worker in LOT 26F-3; adapters may set it in 26F-2.
	SkipReason string
}

// NotificationDeliveryAdapter sends an intent via a channel. 23N-A: no external I/O.
type NotificationDeliveryAdapter interface {
	Channel() string
	ProviderName() string
	Send(ctx context.Context, intent *AppointmentNotificationIntent, payload NotificationPayload) (DeliveryResult, error)
}

// NoopDeliveryAdapter acknowledges without side effects.
type NoopDeliveryAdapter struct {
	channel string
}

func NewNoopDeliveryAdapter(channel string) *NoopDeliveryAdapter {
	if channel == "" {
		channel = NotifChannelLog
	}
	return &NoopDeliveryAdapter{channel: channel}
}

func (a *NoopDeliveryAdapter) Channel() string      { return a.channel }
func (a *NoopDeliveryAdapter) ProviderName() string { return "noop" }

func (a *NoopDeliveryAdapter) Send(_ context.Context, intent *AppointmentNotificationIntent, _ NotificationPayload) (DeliveryResult, error) {
	_ = intent // intentional: no PHI fields logged
	return DeliveryResult{Skipped: true}, nil
}

// LogDeliveryAdapter acknowledges LOG-channel delivery without emitting identifying
// application logs (LOT 26I-5E). Routine outcomes are observed via 5C metrics.
// Never logs telephone, email, reason, payload JSON, intent/appointment IDs, or scheduledAt.
type LogDeliveryAdapter struct {
	channel string
	logger  *slog.Logger
}

func NewLogDeliveryAdapter(channel string, logger *slog.Logger) *LogDeliveryAdapter {
	if channel == "" {
		channel = NotifChannelLog
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &LogDeliveryAdapter{channel: channel, logger: logger}
}

func (a *LogDeliveryAdapter) Channel() string      { return a.channel }
func (a *LogDeliveryAdapter) ProviderName() string { return "log" }

func (a *LogDeliveryAdapter) Send(_ context.Context, intent *AppointmentNotificationIntent, payload NotificationPayload) (DeliveryResult, error) {
	// Business success for LOG channel; no per-delivery application Info (metrics-first).
	_ = intent
	_ = payload
	_ = a.logger
	return DeliveryResult{ProviderMessageID: "log"}, nil
}
