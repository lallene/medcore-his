package cash

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/modules/billing"
	"github.com/lallene/medcore-his/backend/internal/modules/medical_records"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	// MovementPostCloseCorrectionOut is the system OUT for PCE1 physical execution on a host OPEN session.
	// Distinct from PAYMENT_REVERSAL (same-session OPEN reverse).
	MovementPostCloseCorrectionOut = "POST_CLOSE_CORRECTION_OUT"
	MovementRefCashCorrectionExec  = "CASH_CORRECTION_EXECUTION"

	CorrectionAuditExecuted = "cash_correction_executed"
	MaxExecutionNoteLen     = 500
)

// CashCorrectionExecution is the durable 1:1 physical OUT for a post-close PaymentReversal (LOT29F-F PCE1).
// Not a Refund. Never appended to the original CLOSED session.
type CashCorrectionExecution struct {
	ID                    uint `gorm:"primaryKey" json:"id"`
	PaymentReversalID     uint `gorm:"not null;uniqueIndex" json:"paymentReversalId"`
	OriginalPaymentID     uint `gorm:"not null;index" json:"originalPaymentId"`
	OriginalCashSessionID uint `gorm:"not null;index" json:"originalCashSessionId"`
	HostCashSessionID     uint `gorm:"not null;index" json:"hostCashSessionId"`
	CashRegisterID        uint `gorm:"not null;index" json:"cashRegisterId"`
	// Pointer so execution can be inserted before movement in the same TX; committed rows always set.
	CashMovementID *uint     `gorm:"uniqueIndex" json:"cashMovementId"`
	Amount         int64     `gorm:"not null;check:cash_correction_execution_amount_positive,amount > 0" json:"amount"`
	Note           string    `gorm:"size:500" json:"note,omitempty"`
	ExecutedBy     uint      `gorm:"not null;index" json:"executedBy"`
	ExecutedAt     time.Time `gorm:"not null;index" json:"executedAt"`
	IdempotencyKey string    `gorm:"size:120;not null;uniqueIndex" json:"idempotencyKey"`
	CreatedAt      time.Time `json:"createdAt"`
}

func (CashCorrectionExecution) TableName() string { return "cash_correction_executions" }

// CashCorrectionExecutionAudit is mandatory financial evidence (same TX as execution).
type CashCorrectionExecutionAudit struct {
	ID                uint      `gorm:"primaryKey" json:"id"`
	ExecutionID       uint      `gorm:"not null;uniqueIndex" json:"executionId"`
	EventType         string    `gorm:"size:40;not null" json:"eventType"`
	PaymentReversalID uint      `gorm:"not null;index" json:"paymentReversalId"`
	OriginalPaymentID uint      `gorm:"not null;index" json:"originalPaymentId"`
	OriginalSessionID uint      `gorm:"not null;index" json:"originalSessionId"`
	HostSessionID     uint      `gorm:"not null;index" json:"hostSessionId"`
	CashRegisterID    uint      `gorm:"not null;index" json:"cashRegisterId"`
	Amount            int64     `gorm:"not null" json:"amount"`
	ActorID           uint      `gorm:"not null;index" json:"actorId"`
	CreatedAt         time.Time `json:"createdAt"`
}

func (CashCorrectionExecutionAudit) TableName() string { return "cash_correction_execution_audits" }

type ExecuteCorrectionRequest struct {
	PaymentReversalID uint   `json:"paymentReversalId" binding:"required"`
	HostSessionID     *uint  `json:"hostSessionId"`
	Note              string `json:"note"`
	IdempotencyKey    string `json:"idempotencyKey"`
}

type CorrectionEligibility struct {
	Eligible            bool                     `json:"eligible"`
	UnavailableReason   string                   `json:"unavailableReason,omitempty"`
	PaymentID           uint                     `json:"paymentId"`
	PaymentReversalID   uint                     `json:"paymentReversalId,omitempty"`
	Amount              int64                    `json:"amount"`
	OriginalSessionID   uint                     `json:"originalSessionId,omitempty"`
	CashRegisterID      uint                     `json:"cashRegisterId,omitempty"`
	HostSession         *SessionSummary          `json:"hostSession,omitempty"`
	AlreadyExecuted     bool                     `json:"alreadyExecuted"`
	Execution           *CashCorrectionExecution `json:"execution,omitempty"`
	PostCloseCorrection bool                     `json:"postCloseCorrection"`
}

func NormalizeExecutionNote(raw string) (string, error) {
	note := strings.TrimSpace(raw)
	if note == "" {
		return "", nil
	}
	if utf8.RuneCountInString(note) > MaxExecutionNoteLen {
		return "", coreerrors.BadRequest("Note d'exécution trop longue")
	}
	return note, nil
}

func executionFingerprintMatch(x CashCorrectionExecution, revID, hostID uint, note string) bool {
	return x.PaymentReversalID == revID &&
		x.HostCashSessionID == hostID &&
		x.Note == note
}

func executionFingerprintConflict(x CashCorrectionExecution, revID, hostID uint, note string) error {
	if executionFingerprintMatch(x, revID, hostID, note) {
		return nil
	}
	return coreerrors.Conflict("Clé d'idempotence déjà utilisée")
}

func isExecutionUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, fragment := range []string{
		"duplicate key", "unique constraint", "sqlstate 23505",
		"payment_reversal_id", "idempotency_key", "cash_correction_executions",
	} {
		if strings.Contains(msg, fragment) {
			return true
		}
	}
	return false
}

// CorrectionEligibilityForPayment returns FE authority for post-close physical execution.
func (s *Service) CorrectionEligibilityForPayment(paymentID uint) (*CorrectionEligibility, error) {
	var pay billing.Payment
	if e := s.db.First(&pay, paymentID).Error; e != nil {
		if errors.Is(e, gorm.ErrRecordNotFound) {
			return nil, coreerrors.NotFound("PAYMENT")
		}
		return nil, e
	}
	out := &CorrectionEligibility{PaymentID: pay.ID, Amount: pay.Amount}
	var rev billing.PaymentReversal
	if e := s.db.Where("original_payment_id=?", pay.ID).First(&rev).Error; e != nil {
		if errors.Is(e, gorm.ErrRecordNotFound) {
			out.UnavailableReason = "Contrepassation requise avant exécution physique"
			return out, nil
		}
		return nil, e
	}
	out.PaymentReversalID = rev.ID
	var prior CashCorrectionExecution
	if e := s.db.Where("payment_reversal_id=?", rev.ID).First(&prior).Error; e == nil {
		out.AlreadyExecuted = true
		out.Execution = &prior
		out.Eligible = false
		out.UnavailableReason = "Correction de caisse déjà exécutée"
		return out, nil
	} else if !errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, e
	}
	if pay.PaymentMethod != "CASH" {
		out.UnavailableReason = "Seuls les encaissements espèces peuvent faire l'objet d'une exécution physique"
		return out, nil
	}
	if pay.CashSessionID == nil {
		out.UnavailableReason = "Encaissement hors session — exécution physique non applicable"
		return out, nil
	}
	out.OriginalSessionID = *pay.CashSessionID
	var s1 Session
	if e := s.db.Preload("Register").First(&s1, *pay.CashSessionID).Error; e != nil {
		return nil, e
	}
	out.CashRegisterID = s1.CashRegisterID
	if s1.Status != SessionClosed || s1.ClosedAt == nil {
		out.UnavailableReason = "La session d'origine doit être clôturée"
		return out, nil
	}
	if !rev.ReversedAt.After(*s1.ClosedAt) {
		out.UnavailableReason = "Contrepassation hors correction postérieure à la clôture"
		return out, nil
	}
	out.PostCloseCorrection = true
	host, e := s.findOpenSessionForRegister(s1.CashRegisterID)
	if e != nil {
		return nil, e
	}
	if host == nil {
		out.UnavailableReason = "CASH_EXECUTION_OPEN_SESSION_REQUIRED: Aucune session ouverte sur la même caisse"
		return out, nil
	}
	sum, e := s.Get(host.ID)
	if e != nil {
		return nil, e
	}
	out.HostSession = sum
	if sum.ExpectedCash < pay.Amount {
		out.UnavailableReason = "CASH_EXECUTION_INSUFFICIENT_CASH: Espèces attendues insuffisantes sur la session hôte"
		return out, nil
	}
	out.Eligible = true
	return out, nil
}

func (s *Service) findOpenSessionForRegister(registerID uint) (*Session, error) {
	var host Session
	e := s.db.Where("cash_register_id=? AND status=?", registerID, SessionOpen).First(&host).Error
	if e == nil {
		return &host, nil
	}
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return nil, e
}

func (s *Service) GetCorrectionExecution(id uint) (*CashCorrectionExecution, error) {
	var x CashCorrectionExecution
	if e := s.db.First(&x, id).Error; e != nil {
		if errors.Is(e, gorm.ErrRecordNotFound) {
			return nil, coreerrors.NotFound("CASH_CORRECTION_EXECUTION")
		}
		return nil, e
	}
	return &x, nil
}

// ExecutePostCloseCorrection records physical cash leaving the OPEN same-register host drawer (PCE1).
// Explicit command — never automatic after PaymentReversal. Does not mutate Invoice/Receivable/Payment/S1.
func (s *Service) ExecutePostCloseCorrection(r ExecuteCorrectionRequest, user uint) (*CashCorrectionExecution, error) {
	if r.PaymentReversalID == 0 {
		return nil, coreerrors.BadRequest("Contrepassation requise")
	}
	note, err := NormalizeExecutionNote(r.Note)
	if err != nil {
		return nil, err
	}
	key, err := NormalizeSessionCommandKey(r.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	var outID uint
	e := s.db.Transaction(func(tx *gorm.DB) error {
		var priorByKey CashCorrectionExecution
		if e := tx.Where("idempotency_key=?", key).First(&priorByKey).Error; e == nil {
			hostID := priorByKey.HostCashSessionID
			if r.HostSessionID != nil {
				hostID = *r.HostSessionID
			}
			if e := executionFingerprintConflict(priorByKey, r.PaymentReversalID, hostID, note); e != nil {
				return e
			}
			// Allow replay when host omitted (resolved) matches stored host.
			if r.HostSessionID == nil || *r.HostSessionID == priorByKey.HostCashSessionID {
				if priorByKey.PaymentReversalID == r.PaymentReversalID && priorByKey.Note == note {
					outID = priorByKey.ID
					return nil
				}
			}
			return coreerrors.Conflict("Clé d'idempotence déjà utilisée")
		} else if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}

		var rev billing.PaymentReversal
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&rev, r.PaymentReversalID).Error; e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return coreerrors.NotFound("PAYMENT_REVERSAL")
			}
			return e
		}
		var existing CashCorrectionExecution
		if e := tx.Where("payment_reversal_id=?", rev.ID).First(&existing).Error; e == nil {
			return coreerrors.Conflict("Correction de caisse déjà exécutée pour cette contrepassation")
		} else if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}

		var pay billing.Payment
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&pay, rev.OriginalPaymentID).Error; e != nil {
			return coreerrors.NotFound("PAYMENT")
		}
		if pay.PaymentMethod != "CASH" {
			return coreerrors.Conflict("Seuls les encaissements espèces peuvent faire l'objet d'une exécution physique")
		}
		if pay.CashSessionID == nil {
			return coreerrors.Conflict("Encaissement hors session — exécution physique non applicable")
		}
		if rev.Amount != pay.Amount || rev.Amount <= 0 {
			return coreerrors.Conflict("Montant de contrepassation incohérent")
		}

		var s1 Session
		if e := tx.First(&s1, *pay.CashSessionID).Error; e != nil {
			return e
		}
		if s1.Status != SessionClosed || s1.ClosedAt == nil {
			return coreerrors.Conflict("La session d'origine doit être clôturée")
		}
		if !rev.ReversedAt.After(*s1.ClosedAt) {
			return coreerrors.Conflict("Contrepassation hors correction postérieure à la clôture")
		}

		var host Session
		if r.HostSessionID != nil {
			if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&host, *r.HostSessionID).Error; e != nil {
				if errors.Is(e, gorm.ErrRecordNotFound) {
					return coreerrors.NotFound("CASH_SESSION")
				}
				return e
			}
		} else {
			if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("cash_register_id=? AND status=?", s1.CashRegisterID, SessionOpen).
				First(&host).Error; e != nil {
				if errors.Is(e, gorm.ErrRecordNotFound) {
					return coreerrors.Conflict("CASH_EXECUTION_OPEN_SESSION_REQUIRED: Aucune session ouverte sur la même caisse")
				}
				return e
			}
		}
		if host.Status != SessionOpen {
			return coreerrors.Conflict("CASH_EXECUTION_OPEN_SESSION_REQUIRED: La session hôte doit être ouverte")
		}
		if host.CashRegisterID != s1.CashRegisterID {
			return coreerrors.Conflict("CASH_EXECUTION_REGISTER_MISMATCH: La session hôte doit appartenir à la même caisse")
		}

		payTotals, e := loadSessionPaymentTotals(tx, host.ID)
		if e != nil {
			return e
		}
		movTotals, e := loadSessionMovementTotals(tx, host.ID)
		if e != nil {
			return e
		}
		expected := liveExpectedCash(host.OpeningFloat, payTotals.Cash, movTotals.In, movTotals.Out)
		if pay.Amount > expected {
			return coreerrors.Conflict("CASH_EXECUTION_INSUFFICIENT_CASH: Espèces attendues insuffisantes sur la session hôte")
		}

		now := time.Now()
		reason := fmt.Sprintf("Correction de caisse postérieure — paiement #%d", pay.ID)
		if note != "" {
			reason = fmt.Sprintf("%s — %s", reason, note)
		}
		if len(reason) > MaxMovementReasonLen {
			reason = reason[:MaxMovementReasonLen]
		}
		movKey := fmt.Sprintf("cash-correction-exec-%d", rev.ID)

		exec := CashCorrectionExecution{
			PaymentReversalID:     rev.ID,
			OriginalPaymentID:     pay.ID,
			OriginalCashSessionID: s1.ID,
			HostCashSessionID:     host.ID,
			CashRegisterID:        s1.CashRegisterID,
			Amount:                pay.Amount,
			Note:                  note,
			ExecutedBy:            user,
			ExecutedAt:            now,
			IdempotencyKey:        key,
		}
		if e := tx.Exec("SAVEPOINT cash_correction_execution").Error; e != nil {
			return e
		}
		if e := tx.Create(&exec).Error; e != nil {
			_ = tx.Exec("ROLLBACK TO SAVEPOINT cash_correction_execution").Error
			if isExecutionUniqueViolation(e) {
				var raced CashCorrectionExecution
				if load := tx.Where("idempotency_key=?", key).First(&raced).Error; load == nil {
					hostID := host.ID
					if e := executionFingerprintConflict(raced, r.PaymentReversalID, hostID, note); e != nil {
						return e
					}
					outID = raced.ID
					return nil
				}
				if load := tx.Where("payment_reversal_id=?", rev.ID).First(&raced).Error; load == nil {
					return coreerrors.Conflict("Correction de caisse déjà exécutée pour cette contrepassation")
				}
			}
			return e
		}
		if e := tx.Exec("RELEASE SAVEPOINT cash_correction_execution").Error; e != nil {
			return e
		}

		refID := exec.ID
		m := CashMovement{
			CashSessionID:  host.ID,
			Direction:      MovementOut,
			Type:           MovementPostCloseCorrectionOut,
			Amount:         pay.Amount,
			Reason:         reason,
			ReferenceType:  MovementRefCashCorrectionExec,
			ReferenceID:    &refID,
			CreatedBy:      user,
			OccurredAt:     now,
			IdempotencyKey: movKey,
		}
		if e := tx.Create(&m).Error; e != nil {
			return e
		}
		movID := m.ID
		if e := tx.Model(&exec).Update("cash_movement_id", movID).Error; e != nil {
			return e
		}
		exec.CashMovementID = &movID
		audit := CashMovementAudit{
			MovementID: m.ID,
			EventType:  MovementAuditCreated,
			SessionID:  host.ID,
			Direction:  MovementOut,
			Type:       MovementPostCloseCorrectionOut,
			Amount:     pay.Amount,
			Reason:     reason,
			ActorID:    user,
			CreatedAt:  now,
		}
		if e := tx.Create(&audit).Error; e != nil {
			return e
		}
		execAudit := CashCorrectionExecutionAudit{
			ExecutionID:       exec.ID,
			EventType:         CorrectionAuditExecuted,
			PaymentReversalID: rev.ID,
			OriginalPaymentID: pay.ID,
			OriginalSessionID: s1.ID,
			HostSessionID:     host.ID,
			CashRegisterID:    s1.CashRegisterID,
			Amount:            pay.Amount,
			ActorID:           user,
			CreatedAt:         now,
		}
		if e := tx.Create(&execAudit).Error; e != nil {
			return e
		}
		if e := correctionTimeline(tx, pay.InvoiceID, user, exec); e != nil {
			return e
		}
		outID = exec.ID
		return nil
	})
	if e != nil {
		return nil, e
	}
	return s.GetCorrectionExecution(outID)
}

func correctionTimeline(tx *gorm.DB, invoiceID, user uint, exec CashCorrectionExecution) error {
	var inv billing.Invoice
	if e := tx.Select("id", "patient_id", "medical_record_id", "number").First(&inv, invoiceID).Error; e != nil {
		return e
	}
	if inv.MedicalRecordID == nil {
		return nil
	}
	ref := exec.ID
	title := fmt.Sprintf("Correction de caisse exécutée — session hôte #%d", exec.HostCashSessionID)
	return tx.Create(&medical_records.MedicalTimelineEvent{
		MedicalRecordID: *inv.MedicalRecordID,
		PatientID:       inv.PatientID,
		EventType:       CorrectionAuditExecuted,
		Category:        "billing",
		Title:           title,
		Description:     inv.Number,
		ReferenceType:   "cash_correction_execution",
		ReferenceID:     &ref,
		Severity:        "info",
		EventDate:       exec.ExecutedAt,
		CreatedBy:       user,
	}).Error
}
