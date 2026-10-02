package consultations

import (
	"errors"

	"github.com/lallene/medcore-his/backend/internal/core/logger"
	"gorm.io/gorm"
)

// LOT28E-B2-C1 — consultation_created is a post-commit best-effort
// longitudinal / UX chronology signal. Consultation persistence is authoritative;
// timeline failure must not fail the create API after a successful commit.

const (
	consultationCreatedTimelineOperation = "consultation_created_timeline"
	consultationCreatedTimelineEventType = "consultation_created"
)

// Bounded error classes for consultation_created Warn logs.
// Classification never uses err.Error() text (may contain SQL/driver detail).
const (
	consultationCreatedTimelineErrorClassNone     = "none"
	consultationCreatedTimelineErrorClassDatabase = "database"
	consultationCreatedTimelineErrorClassPersist  = "timeline_persist_failed"
)

func classifyConsultationCreatedTimelineError(err error) string {
	if err == nil {
		return consultationCreatedTimelineErrorClassNone
	}
	switch {
	case errors.Is(err, gorm.ErrInvalidDB),
		errors.Is(err, gorm.ErrInvalidTransaction),
		errors.Is(err, gorm.ErrDuplicatedKey):
		return consultationCreatedTimelineErrorClassDatabase
	default:
		return consultationCreatedTimelineErrorClassPersist
	}
}

// logConsultationCreatedTimelineFailure emits a non-fatal WARN after consultation
// commit when the best-effort consultation_created timeline insert fails.
// Allowed fields only: operation, event_type, consultation_id, error_class.
// Never logs PHI, request bodies, raw SQL, DSN, or err.Error().
func logConsultationCreatedTimelineFailure(consultationID uint, err error) {
	logger.Warn(
		"consultation_created timeline event persistence failed",
		"operation", consultationCreatedTimelineOperation,
		"event_type", consultationCreatedTimelineEventType,
		"consultation_id", consultationID,
		"error_class", classifyConsultationCreatedTimelineError(err),
	)
}
