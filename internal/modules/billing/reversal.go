package billing

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	MaxReversalReasonLen = 500
	MinReversalReasonLen = 3
)

type ReversePaymentRequest struct {
	Reason         string `json:"reason" binding:"required"`
	IdempotencyKey string `json:"idempotencyKey"`
}

func NormalizeReversalReason(raw string) (string, error) {
	reason := strings.TrimSpace(raw)
	if reason == "" {
		return "", coreerrors.BadRequest("Motif obligatoire")
	}
	if utf8.RuneCountInString(reason) < MinReversalReasonLen {
		return "", coreerrors.BadRequest("Motif trop court")
	}
	if utf8.RuneCountInString(reason) > MaxReversalReasonLen {
		return "", coreerrors.BadRequest("Motif trop long")
	}
	return reason, nil
}

func reversalFingerprintMatch(r PaymentReversal, paymentID uint, reason string) bool {
	return r.OriginalPaymentID == paymentID && r.Reason == reason
}

func reversalFingerprintConflict(r PaymentReversal, paymentID uint, reason string) error {
	if reversalFingerprintMatch(r, paymentID, reason) {
		return nil
	}
	return coreerrors.Conflict("Clé d'idempotence déjà utilisée")
}

func isReversalUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, fragment := range []string{
		"duplicate key", "unique constraint", "sqlstate 23505",
		"original_payment_id", "idempotency_key", "billing_payment_reversals",
	} {
		if strings.Contains(msg, fragment) {
			return true
		}
	}
	return false
}

func recomputeInvoiceStatus(patientAmount, paidEffective int64) (paid, balance int64, status string) {
	paid = paidEffective
	balance = patientAmount - paidEffective
	if balance < 0 {
		balance = 0
	}
	switch {
	case paidEffective <= 0:
		return 0, patientAmount, InvoiceIssued
	case balance == 0:
		return paidEffective, 0, InvoicePaid
	default:
		return paidEffective, balance, InvoicePartiallyPaid
	}
}

// ReversePayment creates a V1 full reversal counter-entry for a sessionless billing payment.
func (s *Service) ReversePayment(paymentID uint, req ReversePaymentRequest, user uint) (*Invoice, error) {
	reason, err := NormalizeReversalReason(req.Reason)
	if err != nil {
		return nil, err
	}
	key, err := NormalizePaymentIdempotencyKey(req.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	var invoiceID uint
	e := s.db.Transaction(func(tx *gorm.DB) error {
		var priorByKey PaymentReversal
		if e := tx.Where("idempotency_key=?", key).First(&priorByKey).Error; e == nil {
			if e := reversalFingerprintConflict(priorByKey, paymentID, reason); e != nil {
				return e
			}
			var pay Payment
			if e := tx.First(&pay, priorByKey.OriginalPaymentID).Error; e != nil {
				return e
			}
			invoiceID = pay.InvoiceID
			return nil
		} else if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}

		var pay Payment
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&pay, paymentID).Error; e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return coreerrors.NotFound("PAYMENT")
			}
			return e
		}
		if pay.CashSessionID != nil {
			// Cash journal/session totals SUM billing_payments without a correction entry.
			return coreerrors.Conflict("Les encaissements de session de caisse ne peuvent pas être contrepassés ici")
		}

		var existing PaymentReversal
		if e := tx.Where("original_payment_id=?", pay.ID).First(&existing).Error; e == nil {
			return coreerrors.Conflict("Ce paiement a déjà été contrepassé")
		} else if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}

		var inv Invoice
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&inv, pay.InvoiceID).Error; e != nil {
			return coreerrors.NotFound("INVOICE")
		}
		if inv.Status == InvoiceCancelled || inv.Status == InvoiceDraft {
			return coreerrors.Conflict("La facture n'accepte pas de contrepassation")
		}

		effectiveBefore, e := EffectivePaidOnInvoice(tx, inv.ID)
		if e != nil {
			return e
		}
		if effectiveBefore < pay.Amount {
			return coreerrors.Conflict("État financier incohérent pour la contrepassation")
		}

		rev := PaymentReversal{
			OriginalPaymentID: pay.ID,
			Amount:            pay.Amount,
			Reason:            reason,
			ReversedBy:        user,
			ReversedAt:        time.Now(),
			IdempotencyKey:    key,
		}
		if e := tx.Create(&rev).Error; e != nil {
			if isReversalUniqueViolation(e) {
				var raced PaymentReversal
				if load := tx.Where("idempotency_key=?", key).First(&raced).Error; load == nil {
					if e := reversalFingerprintConflict(raced, paymentID, reason); e != nil {
						return e
					}
					invoiceID = pay.InvoiceID
					return nil
				}
				if load := tx.Where("original_payment_id=?", pay.ID).First(&raced).Error; load == nil {
					return coreerrors.Conflict("Ce paiement a déjà été contrepassé")
				}
			}
			return e
		}

		effectiveAfter := effectiveBefore - pay.Amount
		paid, balance, status := recomputeInvoiceStatus(inv.PatientAmount, effectiveAfter)
		if paid != effectiveAfter || paid < 0 || balance < 0 || balance > inv.PatientAmount {
			return coreerrors.Conflict("État financier incohérent après contrepassation")
		}
		inv.PaidAmount = paid
		inv.BalanceAmount = balance
		inv.Status = status
		inv.UpdatedBy = user
		if e := tx.Save(&inv).Error; e != nil {
			return e
		}
		if e := s.timeline(tx, &inv, "payment_reversed", "Encaissement contrepassé", user); e != nil {
			return e
		}
		invoiceID = inv.ID
		return nil
	})
	if e != nil {
		return nil, e
	}
	if invoiceID == 0 {
		var pay Payment
		if e := s.db.First(&pay, paymentID).Error; e != nil {
			return nil, coreerrors.NotFound("PAYMENT")
		}
		invoiceID = pay.InvoiceID
	}
	return s.GetInvoice(invoiceID)
}
