package medical_records

import (
	"errors"

	"github.com/lallene/medcore-his/backend/internal/core/logger"
	"gorm.io/gorm"
)

// LOT28E-B2-A — common_medical_record_updated is a post-commit best-effort
// longitudinal UX / fallback chronology signal. It is intentionally NOT
// same-TX fail-closed like B1 document_added / document_archived.

const (
	genericCMRTimelineOperation = "common_medical_record_updated_timeline"
	genericCMRTimelineEventType = "common_medical_record_updated"
)

// Bounded error classes for generic CMR timeline Warn logs.
// Classification never uses err.Error() text (may contain SQL/driver detail).
const (
	genericCMRTimelineErrorClassNone     = "none"
	genericCMRTimelineErrorClassDatabase = "database"
	genericCMRTimelineErrorClassPersist  = "timeline_persist_failed"
)

func classifyGenericCMRTimelineError(err error) string {
	if err == nil {
		return genericCMRTimelineErrorClassNone
	}
	switch {
	case errors.Is(err, gorm.ErrInvalidDB),
		errors.Is(err, gorm.ErrInvalidTransaction),
		errors.Is(err, gorm.ErrDuplicatedKey):
		return genericCMRTimelineErrorClassDatabase
	default:
		return genericCMRTimelineErrorClassPersist
	}
}

// logGenericCMRTimelineFailure emits a non-fatal WARN after CMR commit when
// the best-effort generic timeline insert fails. Allowed fields only:
// operation, event_type, medical_record_id, error_class.
// Never logs PHI, request bodies, raw SQL, DSN, or err.Error().
func logGenericCMRTimelineFailure(medicalRecordID uint, err error) {
	logger.Warn(
		"generic CMR timeline event persistence failed",
		"operation", genericCMRTimelineOperation,
		"event_type", genericCMRTimelineEventType,
		"medical_record_id", medicalRecordID,
		"error_class", classifyGenericCMRTimelineError(err),
	)
}
