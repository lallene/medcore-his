package cash

import (
	"errors"
	"fmt"
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/modules/billing"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	// MovementRefundOut is the system OUT for genuine CASH Refund execution (LOT29F-I-B).
	// Distinct from PAYMENT_REVERSAL and POST_CLOSE_CORRECTION_OUT.
	MovementRefundOut      = "REFUND_OUT"
	MovementRefRefundExec  = "REFUND_EXECUTION"
)

// ResolveOpenSessionForRefund locks an OPEN CashSession for the executor.
// Prefer preferredSessionID when provided and OPEN; else executor's current OPEN session.
// Does NOT require same register as original payment (genuine refund ≠ PCE).
func ResolveOpenSessionForRefund(tx *gorm.DB, user uint, preferredSessionID *uint) (sessionID, registerID uint, err error) {
	var session Session
	if preferredSessionID != nil && *preferredSessionID > 0 {
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&session, *preferredSessionID).Error; e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return 0, 0, coreerrors.NotFound("CASH_SESSION")
			}
			return 0, 0, e
		}
		if session.Status != SessionOpen {
			return 0, 0, coreerrors.Conflict("La session de caisse doit être ouverte pour un remboursement espèces")
		}
		// Authorized: session opener or any executor with billing.refund.execute (route-gated).
		return session.ID, session.CashRegisterID, nil
	}
	if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("status=? AND opened_by=?", SessionOpen, user).
		Order("opened_at DESC").
		First(&session).Error; e != nil {
		if errors.Is(e, gorm.ErrRecordNotFound) {
			return 0, 0, coreerrors.Conflict("REFUND_EXEC_CASH_SESSION_REQUIRED: Aucune session de caisse ouverte pour l'exécutant")
		}
		return 0, 0, e
	}
	return session.ID, session.CashRegisterID, nil
}

// CreateRefundOutMovement writes the system REFUND_OUT on an OPEN session.
// Authorized by billing.refund.execute — does not require cash.movement.create.
func CreateRefundOutMovement(tx *gorm.DB, sessionID uint, exec billing.RefundExecution, refund billing.Refund, user uint) (uint, error) {
	if exec.ID == 0 || refund.ID == 0 || refund.Amount <= 0 {
		return 0, coreerrors.Conflict("Mouvement de remboursement incohérent")
	}
	if exec.CashSessionID == nil || *exec.CashSessionID != sessionID {
		return 0, coreerrors.Conflict("Session de caisse incohérente pour le remboursement")
	}

	var session Session
	if e := tx.Select("id", "opening_float", "status", "cash_register_id").First(&session, sessionID).Error; e != nil {
		return 0, e
	}
	if session.Status != SessionOpen {
		return 0, coreerrors.Conflict("La session de caisse doit être ouverte pour un remboursement espèces")
	}

	key := fmt.Sprintf("cash-refund-out-%d", refund.ID)
	var prior CashMovement
	if e := tx.Where("idempotency_key=?", key).First(&prior).Error; e == nil {
		if prior.Type != MovementRefundOut || prior.CashSessionID != sessionID || prior.Amount != refund.Amount {
			return 0, coreerrors.Conflict("Clé d'idempotence déjà utilisée")
		}
		return prior.ID, nil
	} else if !errors.Is(e, gorm.ErrRecordNotFound) {
		return 0, e
	}

	payTotals, e := loadSessionPaymentTotals(tx, sessionID)
	if e != nil {
		return 0, e
	}
	movTotals, e := loadSessionMovementTotals(tx, sessionID)
	if e != nil {
		return 0, e
	}
	expected := liveExpectedCash(session.OpeningFloat, payTotals.Cash, movTotals.In, movTotals.Out)
	if refund.Amount > expected {
		return 0, coreerrors.Conflict("REFUND_EXEC_INSUFFICIENT_CASH: Espèces insuffisantes en caisse pour ce remboursement")
	}

	reason := fmt.Sprintf("Remboursement client #%d", refund.ID)
	if len(reason) > MaxMovementReasonLen {
		reason = reason[:MaxMovementReasonLen]
	}
	refID := exec.ID
	now := time.Now().UTC()
	m := CashMovement{
		CashSessionID:  sessionID,
		Direction:      MovementOut,
		Type:           MovementRefundOut,
		Amount:         refund.Amount,
		Reason:         reason,
		ReferenceType:  MovementRefRefundExec,
		ReferenceID:    &refID,
		CreatedBy:      user,
		OccurredAt:     now,
		IdempotencyKey: key,
	}
	if e := tx.Exec("SAVEPOINT refund_out_movement").Error; e != nil {
		return 0, e
	}
	if e := tx.Create(&m).Error; e != nil {
		_ = tx.Exec("ROLLBACK TO SAVEPOINT refund_out_movement").Error
		if isMovementIdempotencyUniqueViolation(e) {
			var raced CashMovement
			if load := tx.Where("idempotency_key=?", key).First(&raced).Error; load == nil {
				return raced.ID, nil
			}
			if load := tx.Where("reference_type=? AND reference_id=?", MovementRefRefundExec, exec.ID).First(&raced).Error; load == nil {
				return raced.ID, nil
			}
		}
		return 0, e
	}
	if e := tx.Exec("RELEASE SAVEPOINT refund_out_movement").Error; e != nil {
		return 0, e
	}
	audit := CashMovementAudit{
		MovementID: m.ID,
		EventType:  MovementAuditCreated,
		SessionID:  sessionID,
		Direction:  MovementOut,
		Type:       MovementRefundOut,
		Amount:     refund.Amount,
		Reason:     reason,
		ActorID:    user,
		CreatedAt:  now,
	}
	if e := tx.Create(&audit).Error; e != nil {
		return 0, e
	}
	return m.ID, nil
}

func init() {
	billing.RegisterCashRefundHooks(ResolveOpenSessionForRefund, CreateRefundOutMovement)
}
