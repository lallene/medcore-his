package patient_queue

import "time"

// NotificationQueueChannelSnapshot is one channel's queue backlog at asOf (LOT 26I-5D).
// Domain channel strings are LOG/EMAIL/SMS (or other persisted values to be dropped by metrics).
type NotificationQueueChannelSnapshot struct {
	Channel         string
	Pending         int64
	Due             int64
	Processing      int64
	StaleProcessing int64
	OldestDueAge    time.Duration // >= 0; 0 when no due row
}

// NotificationQueueSnapshot is a read-only aggregate of PENDING/PROCESSING intents.
type NotificationQueueSnapshot struct {
	AsOf     time.Time
	Channels []NotificationQueueChannelSnapshot
}

// NotificationQueueSnapshotObserver receives successful queue snapshots (LOT 26I-5D).
// Implementations must not return errors; observation never fails business work.
type NotificationQueueSnapshotObserver interface {
	ObserveQueueSnapshot(NotificationQueueSnapshot)
}

type noopQueueSnapshotObserver struct{}

func (noopQueueSnapshotObserver) ObserveQueueSnapshot(NotificationQueueSnapshot) {}

// oldestDueAgeFromSendAfter converts oldest due send_after to a non-negative age at asOf.
// Uses send_after only (not created_at/updated_at). No wall-clock call.
func oldestDueAgeFromSendAfter(asOf time.Time, oldestDueSendAfter *time.Time) time.Duration {
	if oldestDueSendAfter == nil {
		return 0
	}
	age := asOf.Sub(oldestDueSendAfter.UTC())
	if age < 0 {
		return 0
	}
	return age
}
