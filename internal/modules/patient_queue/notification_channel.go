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

// LogDeliveryAdapter logs non-PHI metadata only (id, kind, channel, scheduledAt from payload).
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
	// Never log telephone, email, reason, or full payload JSON.
	a.logger.Info("appointment_notification_intent",
		"intentId", intent.ID,
		"appointmentId", intent.AppointmentID,
		"kind", intent.Kind,
		"channel", intent.Channel,
		"scheduledAt", payload.ScheduledAt,
	)
	return DeliveryResult{ProviderMessageID: "log"}, nil
}
