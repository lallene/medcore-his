package patient_queue

import "time"

// MetricChannel is a bounded Prometheus channel label (LOT 26I-5C / 5D).
type MetricChannel string

const (
	MetricChannelLog   MetricChannel = "log"
	MetricChannelEmail MetricChannel = "email"
	MetricChannelSMS   MetricChannel = "sms"
)

// MetricProvider is a bounded Prometheus provider label (LOT 26I-5C).
type MetricProvider string

const (
	MetricProviderLog          MetricProvider = "log"
	MetricProviderMicrosoft365 MetricProvider = "microsoft365"
)

// DeliveryOutcome is the worker delivery-attempt handling class (LOT 26I-5C).
// Distinct from durable intent status (e.g. transient may leave PENDING or FAILED).
type DeliveryOutcome string

const (
	DeliveryOutcomeSent               DeliveryOutcome = "sent"
	DeliveryOutcomeSkipped            DeliveryOutcome = "skipped"
	DeliveryOutcomePermanent          DeliveryOutcome = "permanent"
	DeliveryOutcomeInvalidMessage     DeliveryOutcome = "invalid_message"
	DeliveryOutcomeNotConfigured      DeliveryOutcome = "not_configured"
	DeliveryOutcomeAmbiguous          DeliveryOutcome = "ambiguous"
	DeliveryOutcomeTransient          DeliveryOutcome = "transient"
	DeliveryOutcomeCanceled           DeliveryOutcome = "canceled"
	DeliveryOutcomeInvalidPayload     DeliveryOutcome = "invalid_payload"
	DeliveryOutcomeAdapterUnavailable DeliveryOutcome = "adapter_unavailable"
)

// MetricChannelFromDomain maps domain channels for delivery/provider metrics (LOT 26I-5C).
// SMS is excluded (no delivery adapter). Unknown → ok=false (drop).
func MetricChannelFromDomain(domainChannel string) (MetricChannel, bool) {
	switch domainChannel {
	case NotifChannelLog:
		return MetricChannelLog, true
	case NotifChannelEmail:
		return MetricChannelEmail, true
	default:
		return "", false
	}
}

// QueueMetricChannelFromDomain maps domain channels for queue gauges (LOT 26I-5D).
// Allows LOG/EMAIL/SMS. Unknown → ok=false (drop).
func QueueMetricChannelFromDomain(domainChannel string) (MetricChannel, bool) {
	switch domainChannel {
	case NotifChannelLog:
		return MetricChannelLog, true
	case NotifChannelEmail:
		return MetricChannelEmail, true
	case NotifChannelSMS:
		return MetricChannelSMS, true
	default:
		return "", false
	}
}

// WorkerLoopObserver records worker Tick / claim / stale / delivery signals (LOT 26I-5B / 5C).
// Implementations must not return errors or panic; observation never fails business work.
// No Prometheus types, IDs, error strings, or unbounded labels.
type WorkerLoopObserver interface {
	// ObserveTick records one Tick wall-clock duration and result (err==nil → success).
	ObserveTick(duration time.Duration, err error)
	// ObserveClaimed records intents successfully transitioned to PROCESSING (n may be 0).
	ObserveClaimed(n int)
	// ObserveStaleRecovered records successful stale recovery transitions (n may be 0;
	// include partial n when recovery returns (n, err)).
	ObserveStaleRecovered(n int)
	// ObserveDeliveryAttempt records one claimed-intent handling branch (LOT 26I-5C).
	ObserveDeliveryAttempt(channel MetricChannel, outcome DeliveryOutcome)
	// ObserveProviderDuration records provider/transport Send wall time (LOT 26I-5C).
	ObserveProviderDuration(channel MetricChannel, provider MetricProvider, duration time.Duration)
}

// noopWorkerLoopObserver discards all observations.
type noopWorkerLoopObserver struct{}

func (noopWorkerLoopObserver) ObserveTick(time.Duration, error)                      {}
func (noopWorkerLoopObserver) ObserveClaimed(int)                                    {}
func (noopWorkerLoopObserver) ObserveStaleRecovered(int)                             {}
func (noopWorkerLoopObserver) ObserveDeliveryAttempt(MetricChannel, DeliveryOutcome) {}
func (noopWorkerLoopObserver) ObserveProviderDuration(MetricChannel, MetricProvider, time.Duration) {
}
