package patient_queue

import (
	"strings"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/modules/consultations"
	"gorm.io/gorm"
)

// IsActiveVisit reports whether a ticket is an active patient visit (LOT28A).
// Predicate: status == ACTIVE. ON_HOLD is intentionally excluded (matches check-in
// duplicate guards and ux_pq_tickets_patient_active).
func IsActiveVisit(t Ticket) bool {
	return t.Status == StatusActive
}

// clinicalCareStarted reports whether clinical encounter has begun for this visit.
// Care start = doctor take / DOCTOR_IN_PROGRESS / linked consultation in_progress.
// Triage alone does NOT commit care for cancel purposes.
func clinicalCareStarted(t Ticket, consultationStatus string) bool {
	if t.DoctorTakenBy != nil {
		return true
	}
	if t.Stage == StageDoctorInProgress {
		return true
	}
	if t.ConsultationID != nil && consultationStatus == consultations.ConsultationStatusInProgress {
		return true
	}
	return false
}

const errActiveVisitConflict = "Le patient a déjà un parcours actif"
const errCareStartedCancel = "Annulation impossible: prise en charge clinique commencée"
const errApptOperationalCancel = "Annulation impossible: rendez-vous déjà en parcours clinique"

// isActiveVisitUniqueViolation detects Postgres unique hits on ux_pq_tickets_patient_active.
func isActiveVisitUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "unique") && !strings.Contains(msg, "duplicate") {
		return false
	}
	return strings.Contains(msg, strings.ToLower(ticketPatientActiveUniqueIndex)) ||
		strings.Contains(msg, "patient_id")
}

func conflictActiveVisit() error {
	return coreerrors.Conflict(errActiveVisitConflict)
}

// countActiveVisitsTx counts ACTIVE tickets for a patient inside a transaction.
func countActiveVisitsTx(tx *gorm.DB, patientID uint) (int64, error) {
	var n int64
	if err := tx.Model(&Ticket{}).Where("patient_id=? AND status=?", patientID, StatusActive).Count(&n).Error; err != nil {
		return 0, coreerrors.Internal(err.Error())
	}
	return n, nil
}
