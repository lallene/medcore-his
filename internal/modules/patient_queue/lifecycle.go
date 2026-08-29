package patient_queue

import (
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
)

// Lifecycle operations (LOT 23E).
const (
	LifecycleOpReschedule = "RESCHEDULE"
	LifecycleOpCancel     = "CANCEL"
	LifecycleOpNoShow     = "NO_SHOW"
)

// canTransitionAppointment documents the LOT 23E state machine.
// Operational statuses (ARRIVED/CHECKED_IN/IN_PROGRESS) and terminals reject lifecycle mutations.
func canTransitionAppointment(status string, op string) error {
	switch op {
	case LifecycleOpReschedule:
		switch status {
		case ApptScheduled:
			return nil
		case ApptArrived, ApptCheckedIn:
			return coreerrors.Conflict("Reschedule interdit: parcours opérationnel commencé")
		case ApptInProgress:
			return coreerrors.Conflict("Reschedule interdit: rendez-vous en cours")
		case ApptCompleted:
			return coreerrors.Conflict("Reschedule interdit: rendez-vous terminé")
		case ApptCancelled:
			return coreerrors.Conflict("Reschedule interdit: rendez-vous annulé")
		case ApptNoShow:
			return coreerrors.Conflict("Reschedule interdit: rendez-vous en no-show")
		default:
			return coreerrors.Conflict("Reschedule interdit pour ce statut")
		}
	case LifecycleOpCancel:
		switch status {
		case ApptScheduled:
			return nil
		case ApptCancelled:
			return nil // idempotent handled by caller
		case ApptArrived, ApptCheckedIn:
			return coreerrors.Conflict("Annulation interdite: parcours opérationnel commencé")
		case ApptInProgress:
			return coreerrors.Conflict("Annulation interdite: rendez-vous en cours")
		case ApptCompleted:
			return coreerrors.Conflict("Annulation interdite: rendez-vous terminé")
		case ApptNoShow:
			return coreerrors.Conflict("Annulation interdite: rendez-vous en no-show")
		default:
			return coreerrors.Conflict("Annulation interdite pour ce statut")
		}
	case LifecycleOpNoShow:
		switch status {
		case ApptScheduled, ApptArrived:
			return nil
		case ApptNoShow:
			return nil // idempotent handled by caller
		case ApptCheckedIn:
			return coreerrors.Conflict("No-show interdit: rendez-vous déjà check-in")
		case ApptInProgress:
			return coreerrors.Conflict("No-show interdit: rendez-vous en cours")
		case ApptCompleted:
			return coreerrors.Conflict("No-show interdit: rendez-vous terminé")
		case ApptCancelled:
			return coreerrors.Conflict("No-show interdit: rendez-vous annulé")
		default:
			return coreerrors.Conflict("No-show interdit pour ce statut")
		}
	default:
		return coreerrors.BadRequest("opération cycle de vie inconnue")
	}
}

// assertNoActiveQueueLink rejects lifecycle ops that would require queue mutation.
func assertNoActiveQueueLink(appt Appointment) error {
	if appt.QueueTicketID != nil {
		return coreerrors.Conflict("Opération interdite: rendez-vous lié à un ticket file actif")
	}
	switch appt.Status {
	case ApptCheckedIn, ApptInProgress:
		return coreerrors.Conflict("Opération interdite: parcours file commencé")
	}
	return nil
}

// noShowEligibleTime: scheduled start must have been reached (not strictly future).
// Uses configured scheduling location for "now" comparison on wall-clock start.
func noShowEligibleTime(scheduledAt, now time.Time) bool {
	return !scheduledAt.UTC().After(now.UTC())
}
