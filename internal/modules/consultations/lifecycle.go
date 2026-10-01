package consultations

import (
	"errors"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/performed_acts"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrQueueLinkedCancelBlocked is returned when cancelling an in_progress consultation
// that still has an ACTIVE queue ticket (LOT28A / LOT28B fail-closed).
var ErrQueueLinkedCancelBlocked = errors.New(
	"annulation impossible: parcours file actif lié",
)

// TransitionOpts configures TransitionStatusTx (LOT28B canonical clinical authority).
type TransitionOpts struct {
	ToStatus                string
	ExpectedVersion         *int // nil = internal/queue mode (status predicate only)
	AuthorID                uint
	CancellationReason      string
	AppointmentID           *uint
	PerformedActs           *performed_acts.Service
	AllowIdempotentComplete bool // already completed → success without second timeline
}

// TransitionResult describes a successful clinical status transition attempt.
type TransitionResult struct {
	OldStatus string
	NewStatus string
	PatientID uint
	Version   int
	Changed   bool // false when idempotent complete / cancelled skip
}

// TransitionStatusTx applies a lifecycle transition under the caller transaction.
// Atomic OCC: FOR UPDATE then UPDATE … WHERE id AND status [=version when ExpectedVersion set].
// Version increments exactly once when Changed.
func TransitionStatusTx(tx *gorm.DB, id uint, opts TransitionOpts) (*TransitionResult, error) {
	if opts.ToStatus == "" {
		return nil, ErrInvalidTransition
	}

	var locked Consultation
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrConsultationNotFound
		}
		return nil, err
	}

	result := &TransitionResult{
		OldStatus: locked.Status,
		NewStatus: locked.Status,
		PatientID: locked.PatientID,
		Version:   locked.Version,
	}

	// Idempotent complete (queue Complete after clinical already done).
	if opts.AllowIdempotentComplete &&
		opts.ToStatus == ConsultationStatusCompleted &&
		locked.Status == ConsultationStatusCompleted {
		if err := ensureProducerOnComplete(tx, locked, opts); err != nil {
			return nil, err
		}
		return result, nil
	}

	// Queue historically no-ops clinical work when consultation is cancelled.
	if opts.AllowIdempotentComplete &&
		opts.ToStatus == ConsultationStatusCompleted &&
		locked.Status == ConsultationStatusCancelled {
		return result, nil
	}

	if !canTransitionConsultationStatus(locked.Status, opts.ToStatus) {
		return nil, ErrInvalidTransition
	}

	if opts.ExpectedVersion != nil {
		if *opts.ExpectedVersion < 1 || locked.Version != *opts.ExpectedVersion {
			return nil, ErrConsultationVersionConflict
		}
	}

	if opts.ToStatus == ConsultationStatusCancelled {
		if opts.CancellationReason == "" {
			return nil, ErrCancellationReasonRequired
		}
		if locked.Status == ConsultationStatusInProgress {
			linked, err := hasActiveQueueTicketTx(tx, id)
			if err != nil {
				return nil, err
			}
			if linked {
				return nil, ErrQueueLinkedCancelBlocked
			}
		}
	}

	now := time.Now().UTC()
	updates := map[string]any{
		"status":     opts.ToStatus,
		"updated_at": now,
		"version":    gorm.Expr("version + 1"),
	}
	switch opts.ToStatus {
	case ConsultationStatusInProgress:
		updates["started_at"] = now
	case ConsultationStatusCompleted:
		updates["completed_at"] = now
	case ConsultationStatusCancelled:
		updates["cancelled_at"] = now
		updates["cancellation_reason"] = opts.CancellationReason
	}

	q := tx.Model(&Consultation{}).Where("id = ? AND status = ?", id, locked.Status)
	if opts.ExpectedVersion != nil {
		q = q.Where("version = ?", *opts.ExpectedVersion)
	}
	res := q.Updates(updates)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected != 1 {
		return nil, ErrConsultationVersionConflict
	}

	result.NewStatus = opts.ToStatus
	result.Version = locked.Version + 1
	result.Changed = true

	if opts.ToStatus == ConsultationStatusCompleted {
		locked.Status = ConsultationStatusCompleted
		if err := ensureProducerOnComplete(tx, locked, opts); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// CompleteConsultationInTx is the queue-facing clinical completion helper (LOT28B).
// No client expectedVersion; concurrency via status = in_progress predicate.
func CompleteConsultationInTx(
	tx *gorm.DB,
	id uint,
	authorID uint,
	appointmentID *uint,
	pa *performed_acts.Service,
) (*TransitionResult, error) {
	return TransitionStatusTx(tx, id, TransitionOpts{
		ToStatus:                ConsultationStatusCompleted,
		AuthorID:                authorID,
		AppointmentID:           appointmentID,
		PerformedActs:           pa,
		AllowIdempotentComplete: true,
	})
}

// ActivateDraftInTx moves draft → in_progress under caller TX (TakeDoctor activation).
func ActivateDraftInTx(tx *gorm.DB, id uint, authorID uint) (*TransitionResult, error) {
	return TransitionStatusTx(tx, id, TransitionOpts{
		ToStatus: ConsultationStatusInProgress,
		AuthorID: authorID,
	})
}

func hasActiveQueueTicketTx(tx *gorm.DB, consultationID uint) (bool, error) {
	var n int64
	if err := tx.Raw(`
		SELECT COUNT(*) FROM patient_queue_tickets
		WHERE consultation_id = ? AND status = 'ACTIVE'`, consultationID).Scan(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

func ensureProducerOnComplete(tx *gorm.DB, c Consultation, opts TransitionOpts) error {
	if opts.PerformedActs == nil {
		return nil
	}
	now := time.Now().UTC()
	cid := c.ID
	appointmentID := opts.AppointmentID
	if appointmentID == nil {
		var appt uint
		if err := tx.Raw(`
			SELECT appointment_id FROM patient_queue_tickets
			WHERE consultation_id = ? AND appointment_id IS NOT NULL
			LIMIT 1`, c.ID).Scan(&appt).Error; err == nil && appt > 0 {
			appointmentID = &appt
		}
	}
	_, err := opts.PerformedActs.EnsureFromProducer(tx, performed_acts.ProducerCreateRequest{
		SourceType:     performed_acts.SourceConsultation,
		SourceID:       c.ID,
		PatientID:      c.PatientID,
		ClinicalKey:    performed_acts.ConsultationClinicalKey,
		ConsultationID: &cid,
		AppointmentID:  appointmentID,
		PerformedAt:    &now,
		ActorID:        opts.AuthorID,
	})
	return err
}
