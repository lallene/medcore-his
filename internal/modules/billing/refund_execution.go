package billing

import (
	"errors"
	"fmt"
	"strings"
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	CreditSourceRefund = "REFUND"
)

const (
	CodeRefundExecInvalidStatus   = "REFUND_EXEC_INVALID_STATUS"
	CodeRefundExecSoDViolation    = "REFUND_EXEC_SOD_VIOLATION"
	CodeRefundExecMethodMismatch  = "REFUND_EXEC_METHOD_MISMATCH"
	CodeRefundExecCashRequired    = "REFUND_EXEC_CASH_SESSION_REQUIRED"
	CodeRefundExecInsufficientCash = "REFUND_EXEC_INSUFFICIENT_CASH"
	CodeRefundExecExternalRefReq  = "REFUND_EXEC_EXTERNAL_REF_REQUIRED"
	CodeRefundExecOrgCashForbidden = "REFUND_ORG_CASH_FORBIDDEN"
	CodeRefundExecAlreadyDone     = "REFUND_EXEC_ALREADY_EXECUTED"
	CodeRefundExecIdempotencyConflict = "REFUND_EXEC_IDEMPOTENCY_CONFLICT"
)

// RefundExecution is the immutable 1:1 financial execution of an APPROVED Refund (LOT29F-I-B).
// CASH: creates REFUND_OUT CashMovement. EXTERNAL: records completed off-system restitution.
type RefundExecution struct {
	ID     uint `gorm:"primaryKey" json:"id"`
	RefundID uint `gorm:"not null;uniqueIndex" json:"refundId"`

	Method string `gorm:"size:30;not null;index" json:"method"` // CASH|CARD|MOBILE_MONEY|TRANSFER

	ExecutedBy uint      `gorm:"not null;index" json:"executedBy"`
	ExecutedAt time.Time `gorm:"not null;index" json:"executedAt"`

	ExternalReference string `gorm:"size:200" json:"externalReference,omitempty"`
	EvidenceReference string `gorm:"size:200" json:"evidenceReference,omitempty"`
	// BeneficiaryRailRef is account/phone/reference for external rails (permission-gated reads).
	BeneficiaryRailRef string `gorm:"size:200" json:"beneficiaryRailRef,omitempty"`

	CashSessionID  *uint `json:"cashSessionId,omitempty"`
	CashRegisterID *uint `json:"cashRegisterId,omitempty"`
	CashMovementID *uint `gorm:"uniqueIndex" json:"cashMovementId,omitempty"`

	// Snapshot copy of approved beneficiary (immutable at execution).
	BeneficiaryMode        string `gorm:"size:20;not null" json:"beneficiaryMode"`
	BeneficiaryDisplayName string `gorm:"size:200;not null" json:"beneficiaryDisplayName"`
	BeneficiaryKind        string `gorm:"size:30;not null" json:"beneficiaryKind"`

	IdempotencyKey string    `gorm:"size:120;not null;uniqueIndex" json:"idempotencyKey"`
	CreatedAt      time.Time `json:"createdAt"`
}

func (RefundExecution) TableName() string { return "billing_refund_executions" }

// RefundExecuteRequest is the client command for POST /billing/refunds/:id/execute.
// Never accepts amount/holder/patient/beneficiary/approvedBy/executedBy overrides.
type RefundExecuteRequest struct {
	Method             string `json:"method"`
	ExternalReference  string `json:"externalReference"`
	EvidenceReference  string `json:"evidenceReference"`
	BeneficiaryRailRef string `json:"beneficiaryRailRef"`
	// HostSessionID optional for CASH — defaults to executor's current OPEN session.
	HostSessionID  *uint  `json:"hostSessionId"`
	IdempotencyKey string `json:"idempotencyKey"`
}

// RefundWithExecution is the read projection after I-B.
type RefundWithExecution struct {
	Refund
	Execution *RefundExecution `json:"execution,omitempty"`
}

// Test hooks for failure-injection (nil in production).
var (
	refundExecFailAfterExecutionInsert func() error
	refundExecFailAfterLedgerInsert    func() error
	refundExecFailAfterCashMovement    func() error
	refundExecFailBeforeCommit         func() error
)

// SetRefundExecFailAfterLedger is test-only failure injection after ledger REFUND insert.
func SetRefundExecFailAfterLedger(fn func() error) { refundExecFailAfterLedgerInsert = fn }

// SetRefundExecFailAfterExecution is test-only failure injection after execution row insert.
func SetRefundExecFailAfterExecution(fn func() error) { refundExecFailAfterExecutionInsert = fn }

// SetRefundExecFailAfterCashMovement is test-only failure injection after REFUND_OUT.
func SetRefundExecFailAfterCashMovement(fn func() error) { refundExecFailAfterCashMovement = fn }

// CashRefundHooks — wired by cash package (avoids import cycle).
type CashRefundSessionResolverFn func(tx *gorm.DB, user uint, preferredSessionID *uint) (sessionID, registerID uint, err error)
type CashRefundOutFn func(tx *gorm.DB, sessionID uint, exec RefundExecution, refund Refund, user uint) (movementID uint, err error)

var (
	cashRefundResolveSessionFn CashRefundSessionResolverFn
	cashRefundCreateOutFn      CashRefundOutFn
)

// RegisterCashRefundHooks wires OPEN-session resolution + REFUND_OUT creation.
func RegisterCashRefundHooks(resolve CashRefundSessionResolverFn, createOut CashRefundOutFn) {
	cashRefundResolveSessionFn = resolve
	cashRefundCreateOutFn = createOut
}

func resolveExecutionMethod(intended, requested string) (string, error) {
	req := strings.TrimSpace(strings.ToUpper(requested))
	intend := strings.TrimSpace(strings.ToUpper(intended))
	if intend == "" || intend == RefundMethodUnspecified {
		if req == "" {
			return "", coreerrors.BadRequest("Mode d'exécution obligatoire")
		}
		switch req {
		case RefundMethodCash, RefundMethodCard, RefundMethodMobileMoney, RefundMethodTransfer:
			return req, nil
		default:
			return "", coreerrors.BadRequest("Mode d'exécution invalide")
		}
	}
	if req != "" && req != intend {
		return "", refundConflict(CodeRefundExecMethodMismatch,
			"Le mode d'exécution doit correspondre au mode autorisé")
	}
	switch intend {
	case RefundMethodCash, RefundMethodCard, RefundMethodMobileMoney, RefundMethodTransfer:
		return intend, nil
	default:
		return "", coreerrors.BadRequest("Mode autorisé non exécutable")
	}
}

func isExternalRefundMethod(method string) bool {
	switch method {
	case RefundMethodCard, RefundMethodMobileMoney, RefundMethodTransfer:
		return true
	default:
		return false
	}
}

func executionFingerprintMatch(x RefundExecution, refundID uint, method, extRef, evidence, rail string, sessionID *uint) bool {
	if x.RefundID != refundID || x.Method != method {
		return false
	}
	if x.ExternalReference != extRef || x.EvidenceReference != evidence || x.BeneficiaryRailRef != rail {
		return false
	}
	if method == RefundMethodCash {
		if sessionID == nil || x.CashSessionID == nil || *x.CashSessionID != *sessionID {
			return false
		}
	}
	return true
}

func isRefundExecUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, fragment := range []string{
		"duplicate key", "unique constraint", "sqlstate 23505",
		"refund_id", "idempotency_key", "billing_refund_executions",
		"ux_credit_ledger_source",
	} {
		if strings.Contains(msg, fragment) {
			return true
		}
	}
	return false
}

func insertLedgerRefund(tx *gorm.DB, refund Refund, user uint) error {
	entry := CreditLedgerEntry{
		HolderPartyID:  refund.HolderPartyID,
		PatientID:      refund.PatientID,
		EntryType:      CreditEntryRefund,
		Amount:         refund.Amount,
		SourceType:     CreditSourceRefund,
		SourceID:       refund.ID,
		Reason:         fmt.Sprintf("Remboursement #%d", refund.ID),
		CreatedBy:      user,
		CreatedAt:      time.Now().UTC(),
		IdempotencyKey: fmt.Sprintf("credit-refund-%d", refund.ID),
	}
	if e := tx.Exec("SAVEPOINT credit_refund_ledger").Error; e != nil {
		return e
	}
	if e := tx.Create(&entry).Error; e != nil {
		_ = tx.Exec("ROLLBACK TO SAVEPOINT credit_refund_ledger").Error
		if isRefundExecUniqueViolation(e) {
			var raced CreditLedgerEntry
			if load := tx.Where("source_type=? AND source_id=?", CreditSourceRefund, refund.ID).First(&raced).Error; load == nil {
				if raced.Amount == refund.Amount && raced.EntryType == CreditEntryRefund {
					return nil
				}
				return refundConflict(CodeRefundExecIdempotencyConflict, "Débit remboursement déjà produit avec un autre effet")
			}
		}
		return e
	}
	return tx.Exec("RELEASE SAVEPOINT credit_refund_ledger").Error
}

// ExecuteRefund atomically converts APPROVED reservation → EXECUTED + CreditLedger REFUND
// (+ REFUND_OUT for CASH). Exactly one economic effect per Refund.
func (s *Service) ExecuteRefund(id uint, req RefundExecuteRequest, user uint) (*RefundWithExecution, error) {
	if user == 0 {
		return nil, coreerrors.Unauthorized("Utilisateur non authentifié")
	}
	key, err := NormalizePaymentIdempotencyKey(req.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	extRef := strings.TrimSpace(req.ExternalReference)
	evidence := strings.TrimSpace(req.EvidenceReference)
	rail := strings.TrimSpace(req.BeneficiaryRailRef)

	var out *RefundWithExecution
	e := s.db.Transaction(func(tx *gorm.DB) error {
		// Idempotency replay by key first.
		var priorExec RefundExecution
		if e := tx.Where("idempotency_key=?", key).First(&priorExec).Error; e == nil {
			var row Refund
			if e := tx.First(&row, priorExec.RefundID).Error; e != nil {
				return e
			}
			if priorExec.RefundID != id {
				return refundConflict(CodeRefundExecIdempotencyConflict, "Clé d'idempotence déjà utilisée pour un autre remboursement")
			}
			method, methErr := resolveExecutionMethod(row.IntendedMethod, req.Method)
			if methErr != nil {
				return methErr
			}
			var sessPtr *uint
			if method == RefundMethodCash {
				// Fingerprint session from prior or request.
				sessPtr = priorExec.CashSessionID
				if req.HostSessionID != nil {
					sessPtr = req.HostSessionID
				}
			}
			if !executionFingerprintMatch(priorExec, id, method, extRef, evidence, rail, sessPtr) &&
				!executionFingerprintMatch(priorExec, id, method, extRef, evidence, rail, priorExec.CashSessionID) {
				return refundConflict(CodeRefundExecIdempotencyConflict, "Clé d'idempotence déjà utilisée avec un autre payload")
			}
			out = &RefundWithExecution{Refund: row, Execution: &priorExec}
			return nil
		} else if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}

		var row Refund
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, id).Error; e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return coreerrors.NotFound("REFUND")
			}
			return e
		}
		if e := lockCreditAccount(tx, row.HolderPartyID, row.PatientID); e != nil {
			return e
		}

		if row.Status == RefundStatusExecuted {
			return refundConflict(CodeRefundExecAlreadyDone, "Ce remboursement est déjà exécuté")
		}
		if row.Status != RefundStatusApproved {
			return refundConflict(CodeRefundExecInvalidStatus, "Seul un remboursement APPROVED peut être exécuté")
		}
		if row.ApprovedBy == nil {
			return refundConflict(CodeRefundExecInvalidStatus, "Autorisation manquante")
		}
		if *row.ApprovedBy == user {
			return refundConflict(CodeRefundExecSoDViolation, "L'autorisateur ne peut pas exécuter le remboursement")
		}
		if row.Amount <= 0 {
			return coreerrors.BadRequest("Montant de remboursement invalide")
		}

		method, methErr := resolveExecutionMethod(row.IntendedMethod, req.Method)
		if methErr != nil {
			return methErr
		}

		var party FinancialParty
		if e := tx.First(&party, row.HolderPartyID).Error; e != nil {
			return e
		}
		if party.Kind == PartyKindOrganization {
			if method == RefundMethodCash {
				return refundConflict(CodeRefundExecOrgCashForbidden,
					"Remboursement organisation : le mode espèces n'est pas autorisé")
			}
			if method != RefundMethodTransfer {
				return refundConflict(CodeRefundExecMethodMismatch,
					"Remboursement organisation : virement bancaire requis")
			}
		}

		if isExternalRefundMethod(method) {
			if extRef == "" {
				return refundConflict(CodeRefundExecExternalRefReq, "Référence externe obligatoire")
			}
			if rail == "" {
				return coreerrors.BadRequest("Référence bénéficiaire (compte/numéro) obligatoire pour un remboursement externe")
			}
		}

		// Reservation must still be active (APPROVED).
		if !IsActiveRefundReservationStatus(row.Status) {
			return refundConflict(CodeRefundExecInvalidStatus, "Réservation inactive")
		}

		now := time.Now().UTC()
		exec := RefundExecution{
			RefundID:               row.ID,
			Method:                 method,
			ExecutedBy:             user,
			ExecutedAt:             now,
			ExternalReference:      extRef,
			EvidenceReference:      evidence,
			BeneficiaryRailRef:     rail,
			BeneficiaryMode:        row.BeneficiaryMode,
			BeneficiaryDisplayName: row.BeneficiaryDisplayName,
			BeneficiaryKind:        row.BeneficiaryKind,
			IdempotencyKey:         key,
			CreatedAt:              now,
		}

		if method == RefundMethodCash {
			if cashRefundResolveSessionFn == nil || cashRefundCreateOutFn == nil {
				return coreerrors.Internal("Exécution espèces indisponible")
			}
			sid, rid, e := cashRefundResolveSessionFn(tx, user, req.HostSessionID)
			if e != nil {
				return e
			}
			exec.CashSessionID = &sid
			exec.CashRegisterID = &rid
		}

		if e := tx.Exec("SAVEPOINT refund_execution_insert").Error; e != nil {
			return e
		}
		if e := tx.Create(&exec).Error; e != nil {
			_ = tx.Exec("ROLLBACK TO SAVEPOINT refund_execution_insert").Error
			if isRefundExecUniqueViolation(e) {
				var raced RefundExecution
				if load := tx.Where("refund_id=?", row.ID).First(&raced).Error; load == nil {
					var r2 Refund
					_ = tx.First(&r2, row.ID).Error
					out = &RefundWithExecution{Refund: r2, Execution: &raced}
					return nil
				}
				if load := tx.Where("idempotency_key=?", key).First(&raced).Error; load == nil {
					return refundConflict(CodeRefundExecIdempotencyConflict, "Clé d'idempotence déjà utilisée")
				}
			}
			return e
		}
		if e := tx.Exec("RELEASE SAVEPOINT refund_execution_insert").Error; e != nil {
			return e
		}
		if refundExecFailAfterExecutionInsert != nil {
			if e := refundExecFailAfterExecutionInsert(); e != nil {
				return e
			}
		}

		if e := insertLedgerRefund(tx, row, user); e != nil {
			return e
		}
		if refundExecFailAfterLedgerInsert != nil {
			if e := refundExecFailAfterLedgerInsert(); e != nil {
				return e
			}
		}

		if method == RefundMethodCash {
			movID, e := cashRefundCreateOutFn(tx, *exec.CashSessionID, exec, row, user)
			if e != nil {
				return e
			}
			exec.CashMovementID = &movID
			if e := tx.Model(&exec).Update("cash_movement_id", movID).Error; e != nil {
				return e
			}
			if refundExecFailAfterCashMovement != nil {
				if e := refundExecFailAfterCashMovement(); e != nil {
					return e
				}
			}
		}

		row.Status = RefundStatusExecuted
		row.UpdatedAt = now
		if e := tx.Save(&row).Error; e != nil {
			return e
		}
		if e := s.timelineRefund(tx, &row, "refund_executed", "Remboursement effectué", user); e != nil {
			return e
		}
		if refundExecFailBeforeCommit != nil {
			if e := refundExecFailBeforeCommit(); e != nil {
				return e
			}
		}
		out = &RefundWithExecution{Refund: row, Execution: &exec}
		return nil
	})
	if e != nil {
		return nil, e
	}
	return out, nil
}

// GetRefundWithExecution returns refund + optional execution (PII rail ref gated by caller).
func (s *Service) GetRefundWithExecution(id uint, includeRailRef bool) (*RefundWithExecution, error) {
	row, e := s.GetRefund(id)
	if e != nil {
		return nil, e
	}
	out := &RefundWithExecution{Refund: *row}
	var exec RefundExecution
	if e := s.db.Where("refund_id=?", id).First(&exec).Error; e == nil {
		if !includeRailRef {
			exec.BeneficiaryRailRef = ""
		}
		out.Execution = &exec
	} else if !errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, e
	}
	return out, nil
}
