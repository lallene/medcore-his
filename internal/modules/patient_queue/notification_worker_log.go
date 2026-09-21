package patient_queue

import (
	"context"
	"errors"

	"github.com/lallene/medcore-his/backend/internal/shared/email"
	"gorm.io/gorm"
)

// Bounded application-log operation names (LOT 26I-5E). Static literals only.
const (
	WorkerLogOpTick                 = "tick"
	WorkerLogOpQueueSnapshotRefresh = "queue_snapshot_refresh"
	WorkerLogOpFinalizeSent         = "finalize_sent"
	WorkerLogOpFinalizeSkipped      = "finalize_skipped"
	WorkerLogOpFinalizeFailure      = "finalize_failure"
)

// Bounded application-log error classes (LOT 26I-5E). Never derived from err.Error().
const (
	WorkerErrorClassCanceled       = "canceled"
	WorkerErrorClassAmbiguous      = "ambiguous"
	WorkerErrorClassPermanent      = "permanent"
	WorkerErrorClassInvalidMessage = "invalid_message"
	WorkerErrorClassNotConfigured  = "not_configured"
	WorkerErrorClassTransient      = "transient"
	WorkerErrorClassDatabase       = "database"
	WorkerErrorClassInternal       = "internal"
)

// ClassifyWorkerLogError maps err to a bounded error_class for application slog.
// Classification uses errors.Is only — never err.Error() text.
// Email sentinels are checked before context: AmbiguousDelivery may wrap
// Canceled/DeadlineExceeded after Graph dispatch (same precedence as finalizeSendError).
func ClassifyWorkerLogError(err error) string {
	if err == nil {
		return WorkerErrorClassInternal
	}
	switch {
	case errors.Is(err, email.ErrAmbiguousDelivery):
		return WorkerErrorClassAmbiguous
	case errors.Is(err, email.ErrPermanent):
		return WorkerErrorClassPermanent
	case errors.Is(err, email.ErrInvalidMessage):
		return WorkerErrorClassInvalidMessage
	case errors.Is(err, email.ErrNotConfigured):
		return WorkerErrorClassNotConfigured
	case errors.Is(err, email.ErrTransient):
		return WorkerErrorClassTransient
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return WorkerErrorClassCanceled
	case errors.Is(err, gorm.ErrRecordNotFound),
		errors.Is(err, gorm.ErrInvalidDB),
		errors.Is(err, gorm.ErrInvalidTransaction):
		return WorkerErrorClassDatabase
	default:
		return WorkerErrorClassInternal
	}
}

// logWorkerOpError emits a production-safe ERROR with operation + error_class only.
// Extra attrs must be bounded non-identifying fields (e.g. channel, provider).
// Never pass raw errors, IDs, SQL, or free-text diagnostics.
func (w *NotificationWorker) logWorkerOpError(operation string, err error, attrs ...any) {
	if w == nil || w.log == nil {
		return
	}
	args := make([]any, 0, 4+len(attrs))
	args = append(args, "operation", operation, "error_class", ClassifyWorkerLogError(err))
	args = append(args, attrs...)
	w.log.Error("notification worker operation failed", args...)
}
