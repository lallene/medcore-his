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

// LockOpenSessionForReversal locks CashSession FOR UPDATE and requires OPEN.
// Caller-owned TX — no nested transaction. Lock order: session before payment/invoice.
func LockOpenSessionForReversal(tx *gorm.DB, sessionID uint) error {
	var session Session
	if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&session, sessionID).Error; e != nil {
		if errors.Is(e, gorm.ErrRecordNotFound) {
			return coreerrors.NotFound("CASH_SESSION")
		}
		return e
	}
	if session.Status != SessionOpen {
		return coreerrors.Conflict("PAYMENT_REVERSAL_CASH_SESSION_CLOSED: La session de caisse est fermée — contrepassation impossible")
	}
	return nil
}

// CreatePaymentReversalMovement writes the system OUT movement for an OPEN CASH PaymentReversal.
// Same TX as the reversal. Does not require cash.movement.create — authorized by billing.payment.reverse.
func CreatePaymentReversalMovement(tx *gorm.DB, sessionID uint, pay billing.Payment, rev billing.PaymentReversal, user uint) error {
	if pay.CashSessionID == nil || *pay.CashSessionID != sessionID {
		return coreerrors.Conflict("Session de caisse incohérente pour la contrepassation")
	}
	if pay.PaymentMethod != "CASH" {
		return coreerrors.Conflict("PAYMENT_REVERSAL_SESSION_METHOD_UNSUPPORTED: Seuls les encaissements espèces de session ouverte peuvent être contrepassés")
	}
	if rev.ID == 0 || rev.Amount != pay.Amount || rev.Amount <= 0 {
		return coreerrors.Conflict("Mouvement de contrepassation incohérent")
	}

	// Idempotent by reference / derived key (one reversal → one movement).
	key := fmt.Sprintf("cash-payment-reversal-%d", pay.ID)
	var prior CashMovement
	if e := tx.Where("idempotency_key=?", key).First(&prior).Error; e == nil {
		if prior.Type != MovementPaymentReversal || prior.CashSessionID != sessionID || prior.Amount != pay.Amount {
			return coreerrors.Conflict("Clé d'idempotence déjà utilisée")
		}
		if prior.ReferenceType == MovementRefPaymentReversal && prior.ReferenceID != nil && *prior.ReferenceID == rev.ID {
			return nil
		}
		return nil
	} else if !errors.Is(e, gorm.ErrRecordNotFound) {
		return e
	}

	payTotals, e := loadSessionPaymentTotals(tx, sessionID)
	if e != nil {
		return e
	}
	movTotals, e := loadSessionMovementTotals(tx, sessionID)
	if e != nil {
		return e
	}
	var session Session
	if e := tx.Select("id", "opening_float", "status").First(&session, sessionID).Error; e != nil {
		return e
	}
	if session.Status != SessionOpen {
		return coreerrors.Conflict("PAYMENT_REVERSAL_CASH_SESSION_CLOSED: La session de caisse est fermée — contrepassation impossible")
	}
	expected := liveExpectedCash(session.OpeningFloat, payTotals.Cash, movTotals.In, movTotals.Out)
	if rev.Amount > expected {
		return coreerrors.Conflict("Sortie supérieure aux espèces attendues en caisse")
	}

	reason := fmt.Sprintf("Contrepassation encaissement #%d", pay.ID)
	if len(reason) > MaxMovementReasonLen {
		reason = reason[:MaxMovementReasonLen]
	}
	refID := rev.ID
	now := time.Now()
	m := CashMovement{
		CashSessionID:  sessionID,
		Direction:      MovementOut,
		Type:           MovementPaymentReversal,
		Amount:         rev.Amount,
		Reason:         reason,
		ReferenceType:  MovementRefPaymentReversal,
		ReferenceID:    &refID,
		CreatedBy:      user,
		OccurredAt:     now,
		IdempotencyKey: key,
	}
	if e := tx.Exec("SAVEPOINT payment_reversal_movement").Error; e != nil {
		return e
	}
	if e := tx.Create(&m).Error; e != nil {
		_ = tx.Exec("ROLLBACK TO SAVEPOINT payment_reversal_movement").Error
		if isMovementIdempotencyUniqueViolation(e) {
			var raced CashMovement
			if load := tx.Where("idempotency_key=?", key).First(&raced).Error; load == nil {
				return nil
			}
			if load := tx.Where("reference_type=? AND reference_id=?", MovementRefPaymentReversal, rev.ID).First(&raced).Error; load == nil {
				return nil
			}
		}
		return e
	}
	if e := tx.Exec("RELEASE SAVEPOINT payment_reversal_movement").Error; e != nil {
		return e
	}
	audit := CashMovementAudit{
		MovementID: m.ID,
		EventType:  MovementAuditCreated,
		SessionID:  sessionID,
		Direction:  MovementOut,
		Type:       MovementPaymentReversal,
		Amount:     rev.Amount,
		Reason:     reason,
		ActorID:    user,
		CreatedAt:  now,
	}
	return tx.Create(&audit).Error
}

func init() {
	billing.RegisterCashSessionReversalHooks(LockOpenSessionForReversal, CreatePaymentReversalMovement)
}
