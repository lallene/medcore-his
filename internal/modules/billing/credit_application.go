package billing

import (
	"errors"
	"fmt"
	"hash/fnv"
	"strings"
	"time"
	"unicode/utf8"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	CreditSourceCreditApplication         = "CREDIT_APPLICATION"
	CreditSourceCreditApplicationReversal = "CREDIT_APPLICATION_REVERSAL"

	CodeCreditApplicationInsufficient        = "CREDIT_APPLICATION_INSUFFICIENT"
	CodeCreditApplicationInvoiceIneligible   = "CREDIT_APPLICATION_INVOICE_INELIGIBLE"
	CodeCreditApplicationExceedsBalance      = "CREDIT_APPLICATION_EXCEEDS_BALANCE"
	CodeCreditApplicationIdempotencyConflict = "IDEMPOTENCY_CONFLICT"
	CodeCreditApplicationAlreadyReversed     = "CREDIT_APPLICATION_ALREADY_REVERSED"
	CodeCreditApplicationNotFound            = "CREDIT_APPLICATION_NOT_FOUND"

	MaxCreditApplicationReasonLen = 500
)

// CreditApplication is an immutable allocation of customer credit to an invoice (LOT29F-H-D).
// Not a Payment, CashMovement, Refund, or CreditNote.
type CreditApplication struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	HolderPartyID  uint      `gorm:"not null;index" json:"holderPartyId"`
	PatientID      uint      `gorm:"not null;index" json:"patientId"`
	InvoiceID      uint      `gorm:"not null;index" json:"invoiceId"`
	Amount         int64     `gorm:"not null;check:billing_credit_application_amount_positive,amount > 0" json:"amount"`
	Reason         string    `gorm:"size:500" json:"reason,omitempty"`
	IdempotencyKey string    `gorm:"size:120;not null;uniqueIndex" json:"idempotencyKey"`
	CreatedBy      uint      `gorm:"not null;index" json:"createdBy"`
	CreatedAt      time.Time `json:"createdAt"`
}

func (CreditApplication) TableName() string { return "billing_credit_applications" }

// CreditApplicationReversal restores previously applied credit (append-only correction).
type CreditApplicationReversal struct {
	ID                    uint      `gorm:"primaryKey" json:"id"`
	OriginalApplicationID uint      `gorm:"not null;uniqueIndex" json:"originalApplicationId"`
	Amount                int64     `gorm:"not null;check:billing_credit_app_rev_amount_positive,amount > 0" json:"amount"`
	Reason                string    `gorm:"size:500;not null" json:"reason"`
	IdempotencyKey        string    `gorm:"size:120;not null;uniqueIndex" json:"idempotencyKey"`
	CreatedBy             uint      `gorm:"not null;index" json:"createdBy"`
	CreatedAt             time.Time `json:"createdAt"`
}

func (CreditApplicationReversal) TableName() string { return "billing_credit_application_reversals" }

type CreditApplicationRequest struct {
	HolderPartyID  uint   `json:"holderPartyId" binding:"required"`
	Amount         int64  `json:"amount" binding:"required"`
	Reason         string `json:"reason"`
	IdempotencyKey string `json:"idempotencyKey"`
}

type CreditApplicationReversalRequest struct {
	Reason         string `json:"reason" binding:"required"`
	IdempotencyKey string `json:"idempotencyKey"`
}

type CreditApplicationResult struct {
	Application            CreditApplication `json:"application"`
	AmountApplied          int64             `json:"amountApplied"`
	RemainingAvailable     int64             `json:"remainingAvailableCredit"`
	RemainingReceivable    int64             `json:"remainingReceivable"`
	InvoiceStatus          string            `json:"invoiceStatus"`
	InvoiceID              uint              `json:"invoiceId"`
	HolderPartyID          uint              `json:"holderPartyId"`
	PatientID              uint              `json:"patientId"`
	CreditAppliedOnInvoice int64             `json:"creditAppliedOnInvoice"`
	MoneyPaidOnInvoice     int64             `json:"moneyPaidOnInvoice"`
}

const (
	EffectiveCreditAppliedSubquery = `
SELECT a.invoice_id, SUM(a.amount) AS applied
FROM billing_credit_applications a
LEFT JOIN billing_credit_application_reversals r ON r.original_application_id = a.id
WHERE r.id IS NULL
GROUP BY a.invoice_id
`
)

func EffectiveCreditAppliedOnInvoice(tx *gorm.DB, invoiceID uint) (int64, error) {
	var applied int64
	e := tx.Raw(`
		SELECT COALESCE(SUM(a.amount), 0)
		FROM billing_credit_applications a
		LEFT JOIN billing_credit_application_reversals r ON r.original_application_id = a.id
		WHERE a.invoice_id = ? AND r.id IS NULL
	`, invoiceID).Scan(&applied).Error
	return applied, e
}

func RemainingReceivableAfterSettlement(correctedObligation, moneyPaid, creditApplied int64) int64 {
	return RemainingReceivable(correctedObligation, moneyPaid+creditApplied)
}

func creditAppConflict(code, message string) error {
	return coreerrors.New(409, code, message, nil)
}

func NormalizeCreditApplicationReason(raw string) (string, error) {
	reason := strings.TrimSpace(raw)
	if utf8.RuneCountInString(reason) > MaxCreditApplicationReasonLen {
		return "", coreerrors.BadRequest("Motif trop long")
	}
	return reason, nil
}

func creditApplicationFingerprintMatch(a CreditApplication, invoiceID, holderPartyID uint, amount int64) bool {
	return a.InvoiceID == invoiceID && a.HolderPartyID == holderPartyID && a.Amount == amount
}

func creditApplicationFingerprintConflict(a CreditApplication, invoiceID, holderPartyID uint, amount int64) error {
	if creditApplicationFingerprintMatch(a, invoiceID, holderPartyID, amount) {
		return nil
	}
	return creditAppConflict(CodeCreditApplicationIdempotencyConflict, "Clé d'idempotence déjà utilisée")
}

func isCreditApplicationUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, fragment := range []string{
		"duplicate key", "unique constraint", "sqlstate 23505",
		"idempotency_key", "billing_credit_applications", "ux_credit_ledger_source",
	} {
		if strings.Contains(msg, fragment) {
			return true
		}
	}
	return false
}

// lockCreditAccount takes a transaction-scoped advisory lock for holder+patient credit authority.
// Lock order (H-D): invoice FOR UPDATE → credit advisory → mutations.
func lockCreditAccount(tx *gorm.DB, holderPartyID, patientID uint) error {
	h := fnv.New64a()
	_, _ = fmt.Fprintf(h, "medcore-credit:%d:%d", holderPartyID, patientID)
	key := int64(h.Sum64())
	return tx.Exec("SELECT pg_advisory_xact_lock(?)", key).Error
}

func insertLedgerApplyForApplication(tx *gorm.DB, app CreditApplication, user uint) error {
	entry := CreditLedgerEntry{
		HolderPartyID:  app.HolderPartyID,
		PatientID:      app.PatientID,
		EntryType:      CreditEntryApply,
		Amount:         app.Amount,
		SourceType:     CreditSourceCreditApplication,
		SourceID:       app.ID,
		Reason:         app.Reason,
		CreatedBy:      user,
		CreatedAt:      time.Now(),
		IdempotencyKey: fmt.Sprintf("credit-apply-%s", app.IdempotencyKey),
	}
	if e := tx.Exec("SAVEPOINT credit_apply_ledger").Error; e != nil {
		return e
	}
	if e := tx.Create(&entry).Error; e != nil {
		_ = tx.Exec("ROLLBACK TO SAVEPOINT credit_apply_ledger").Error
		if isCreditLedgerUniqueViolation(e) {
			var raced CreditLedgerEntry
			if load := tx.Where("source_type=? AND source_id=?", CreditSourceCreditApplication, app.ID).First(&raced).Error; load == nil {
				if raced.Amount == app.Amount && raced.EntryType == CreditEntryApply {
					return nil
				}
				return creditAppConflict(CodeCreditApplicationIdempotencyConflict, "Débit crédit déjà produit avec un autre effet")
			}
		}
		return e
	}
	return tx.Exec("RELEASE SAVEPOINT credit_apply_ledger").Error
}

func insertLedgerCreditForApplicationReversal(tx *gorm.DB, rev CreditApplicationReversal, app CreditApplication, user uint) error {
	entry := CreditLedgerEntry{
		HolderPartyID:  app.HolderPartyID,
		PatientID:      app.PatientID,
		EntryType:      CreditEntryCredit,
		Amount:         rev.Amount,
		SourceType:     CreditSourceCreditApplicationReversal,
		SourceID:       rev.ID,
		Reason:         rev.Reason,
		CreatedBy:      user,
		CreatedAt:      time.Now(),
		IdempotencyKey: fmt.Sprintf("credit-apply-rev-%s", rev.IdempotencyKey),
	}
	if e := tx.Exec("SAVEPOINT credit_apply_rev_ledger").Error; e != nil {
		return e
	}
	if e := tx.Create(&entry).Error; e != nil {
		_ = tx.Exec("ROLLBACK TO SAVEPOINT credit_apply_rev_ledger").Error
		if isCreditLedgerUniqueViolation(e) {
			var raced CreditLedgerEntry
			if load := tx.Where("source_type=? AND source_id=?", CreditSourceCreditApplicationReversal, rev.ID).First(&raced).Error; load == nil {
				return nil
			}
		}
		return e
	}
	return tx.Exec("RELEASE SAVEPOINT credit_apply_rev_ledger").Error
}

func (s *Service) buildApplicationResult(tx *gorm.DB, app CreditApplication, inv Invoice) (*CreditApplicationResult, error) {
	avail, e := AvailableCredit(tx, app.HolderPartyID, app.PatientID)
	if e != nil {
		return nil, e
	}
	money, e := EffectivePaidOnInvoice(tx, inv.ID)
	if e != nil {
		return nil, e
	}
	applied, e := EffectiveCreditAppliedOnInvoice(tx, inv.ID)
	if e != nil {
		return nil, e
	}
	credited, e := CreditedOnInvoice(tx, inv.ID)
	if e != nil {
		return nil, e
	}
	corrected := CorrectedPatientObligation(inv.PatientAmount, credited)
	return &CreditApplicationResult{
		Application:            app,
		AmountApplied:          app.Amount,
		RemainingAvailable:     avail,
		RemainingReceivable:    RemainingReceivableAfterSettlement(corrected, money, applied),
		InvoiceStatus:          inv.Status,
		InvoiceID:              inv.ID,
		HolderPartyID:          app.HolderPartyID,
		PatientID:              app.PatientID,
		CreditAppliedOnInvoice: applied,
		MoneyPaidOnInvoice:     money,
	}, nil
}

// ApplyCredit allocates available holder/patient credit to settle invoice receivable.
func (s *Service) ApplyCredit(invoiceID uint, req CreditApplicationRequest, user uint) (*CreditApplicationResult, error) {
	if req.HolderPartyID == 0 {
		return nil, coreerrors.BadRequest("Titulaire financier obligatoire")
	}
	if req.Amount <= 0 {
		return nil, coreerrors.BadRequest("Montant d'application obligatoire et strictement positif")
	}
	reason, err := NormalizeCreditApplicationReason(req.Reason)
	if err != nil {
		return nil, err
	}
	key, err := NormalizePaymentIdempotencyKey(req.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	var out *CreditApplicationResult
	e := s.db.Transaction(func(tx *gorm.DB) error {
		var prior CreditApplication
		if e := tx.Where("idempotency_key=?", key).First(&prior).Error; e == nil {
			if e := creditApplicationFingerprintConflict(prior, invoiceID, req.HolderPartyID, req.Amount); e != nil {
				return e
			}
			var inv Invoice
			if e := tx.First(&inv, prior.InvoiceID).Error; e != nil {
				return e
			}
			res, e := s.buildApplicationResult(tx, prior, inv)
			if e != nil {
				return e
			}
			out = res
			return nil
		} else if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}

		var inv Invoice
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&inv, invoiceID).Error; e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return coreerrors.NotFound("INVOICE")
			}
			return e
		}
		if e := lockCreditAccount(tx, req.HolderPartyID, inv.PatientID); e != nil {
			return e
		}

		if inv.Status == InvoiceDraft || inv.Status == InvoiceCancelled {
			return creditAppConflict(CodeCreditApplicationInvoiceIneligible, "Cette facture n'accepte pas d'application de crédit")
		}
		if inv.CoveragePending {
			return creditAppConflict(CodeCreditApplicationInvoiceIneligible, "PEC en attente — application de crédit impossible")
		}
		if inv.InsuranceAmount > 0 {
			return creditAppConflict(CodeCreditApplicationInvoiceIneligible, "Facture avec part assurance — application de crédit non supportée")
		}
		if inv.Status != InvoiceIssued && inv.Status != InvoicePartiallyPaid && inv.Status != InvoicePaid {
			return creditAppConflict(CodeCreditApplicationInvoiceIneligible, "État de facture incompatible avec l'application de crédit")
		}

		var party FinancialParty
		if e := tx.First(&party, req.HolderPartyID).Error; e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return coreerrors.NotFound("FINANCIAL_PARTY")
			}
			return e
		}

		// I-A: applications consume spendable credit (ledger − active refund reservations).
		avail, e := SpendableCredit(tx, req.HolderPartyID, inv.PatientID)
		if e != nil {
			return e
		}
		if req.Amount > avail {
			return creditAppConflict(CodeCreditApplicationInsufficient, "Crédit utilisable insuffisant")
		}

		credited, e := CreditedOnInvoice(tx, inv.ID)
		if e != nil {
			return e
		}
		moneyPaid, e := EffectivePaidOnInvoice(tx, inv.ID)
		if e != nil {
			return e
		}
		alreadyApplied, e := EffectiveCreditAppliedOnInvoice(tx, inv.ID)
		if e != nil {
			return e
		}
		corrected := CorrectedPatientObligation(inv.PatientAmount, credited)
		receivable := RemainingReceivableAfterSettlement(corrected, moneyPaid, alreadyApplied)
		if receivable <= 0 {
			return creditAppConflict(CodeCreditApplicationInvoiceIneligible, "Aucun solde patient à régler")
		}
		if req.Amount > receivable {
			return creditAppConflict(CodeCreditApplicationExceedsBalance, "Montant supérieur au reste dû patient")
		}

		app := CreditApplication{
			HolderPartyID:  req.HolderPartyID,
			PatientID:      inv.PatientID,
			InvoiceID:      inv.ID,
			Amount:         req.Amount,
			Reason:         reason,
			IdempotencyKey: key,
			CreatedBy:      user,
			CreatedAt:      time.Now(),
		}
		if e := tx.Exec("SAVEPOINT credit_apply_idempotency").Error; e != nil {
			return e
		}
		if e := tx.Create(&app).Error; e != nil {
			_ = tx.Exec("ROLLBACK TO SAVEPOINT credit_apply_idempotency").Error
			if isCreditApplicationUniqueViolation(e) {
				var raced CreditApplication
				if load := tx.Where("idempotency_key=?", key).First(&raced).Error; load == nil {
					if e := creditApplicationFingerprintConflict(raced, invoiceID, req.HolderPartyID, req.Amount); e != nil {
						return e
					}
					res, e := s.buildApplicationResult(tx, raced, inv)
					if e != nil {
						return e
					}
					out = res
					return nil
				}
			}
			return e
		}
		if e := tx.Exec("RELEASE SAVEPOINT credit_apply_idempotency").Error; e != nil {
			return e
		}
		if e := insertLedgerApplyForApplication(tx, app, user); e != nil {
			return e
		}

		appliedAfter := alreadyApplied + req.Amount
		paid, balance, status := recomputeInvoiceStatus(inv.PatientAmount, credited, moneyPaid, appliedAfter)
		inv.PaidAmount = paid
		inv.BalanceAmount = balance
		inv.Status = status
		inv.UpdatedBy = user
		if e := tx.Save(&inv).Error; e != nil {
			return e
		}
		if e := s.timeline(tx, &inv, "credit_applied", "Crédit appliqué", user); e != nil {
			return e
		}
		res, e := s.buildApplicationResult(tx, app, inv)
		if e != nil {
			return e
		}
		out = res
		return nil
	})
	if e != nil {
		return nil, e
	}
	return out, nil
}

// ReverseCreditApplication restores available credit and invoice receivable (no Payment/Cash).
func (s *Service) ReverseCreditApplication(applicationID uint, req CreditApplicationReversalRequest, user uint) (*CreditApplicationResult, error) {
	reason, err := NormalizeCreditNoteReason(req.Reason)
	if err != nil {
		return nil, err
	}
	key, err := NormalizePaymentIdempotencyKey(req.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	var out *CreditApplicationResult
	e := s.db.Transaction(func(tx *gorm.DB) error {
		var priorRev CreditApplicationReversal
		if e := tx.Where("idempotency_key=?", key).First(&priorRev).Error; e == nil {
			var app CreditApplication
			if e := tx.First(&app, priorRev.OriginalApplicationID).Error; e != nil {
				return e
			}
			var inv Invoice
			if e := tx.First(&inv, app.InvoiceID).Error; e != nil {
				return e
			}
			res, e := s.buildApplicationResult(tx, app, inv)
			if e != nil {
				return e
			}
			out = res
			return nil
		} else if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}

		var app CreditApplication
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&app, applicationID).Error; e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return coreerrors.NotFound("CREDIT_APPLICATION")
			}
			return e
		}
		var inv Invoice
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&inv, app.InvoiceID).Error; e != nil {
			return coreerrors.NotFound("INVOICE")
		}
		if e := lockCreditAccount(tx, app.HolderPartyID, app.PatientID); e != nil {
			return e
		}
		var existing CreditApplicationReversal
		if e := tx.Where("original_application_id=?", app.ID).First(&existing).Error; e == nil {
			return creditAppConflict(CodeCreditApplicationAlreadyReversed, "Cette application de crédit a déjà été annulée")
		} else if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}

		rev := CreditApplicationReversal{
			OriginalApplicationID: app.ID,
			Amount:                app.Amount,
			Reason:                reason,
			IdempotencyKey:        key,
			CreatedBy:             user,
			CreatedAt:             time.Now(),
		}
		if e := tx.Create(&rev).Error; e != nil {
			if isCreditApplicationUniqueViolation(e) {
				return creditAppConflict(CodeCreditApplicationAlreadyReversed, "Cette application de crédit a déjà été annulée")
			}
			return e
		}
		if e := insertLedgerCreditForApplicationReversal(tx, rev, app, user); e != nil {
			return e
		}

		credited, e := CreditedOnInvoice(tx, inv.ID)
		if e != nil {
			return e
		}
		moneyPaid, e := EffectivePaidOnInvoice(tx, inv.ID)
		if e != nil {
			return e
		}
		applied, e := EffectiveCreditAppliedOnInvoice(tx, inv.ID)
		if e != nil {
			return e
		}
		paid, balance, status := recomputeInvoiceStatus(inv.PatientAmount, credited, moneyPaid, applied)
		inv.PaidAmount = paid
		inv.BalanceAmount = balance
		inv.Status = status
		inv.UpdatedBy = user
		if e := tx.Save(&inv).Error; e != nil {
			return e
		}
		if e := s.timeline(tx, &inv, "credit_application_reversed", "Application de crédit annulée", user); e != nil {
			return e
		}
		res, e := s.buildApplicationResult(tx, app, inv)
		if e != nil {
			return e
		}
		out = res
		return nil
	})
	if e != nil {
		return nil, e
	}
	return out, nil
}

// ListPatientCreditBalances returns holder summaries with available credit for a patient.
func (s *Service) ListPatientCreditBalances(patientID uint) ([]CreditSummary, error) {
	if patientID == 0 {
		return nil, coreerrors.BadRequest("Patient requis")
	}
	type row struct {
		HolderPartyID uint
	}
	var holders []row
	if e := s.db.Model(&CreditLedgerEntry{}).
		Select("DISTINCT holder_party_id AS holder_party_id").
		Where("patient_id=?", patientID).
		Scan(&holders).Error; e != nil {
		return nil, e
	}
	out := make([]CreditSummary, 0, len(holders))
	for _, h := range holders {
		sum, e := CreditSummaryFor(s.db, h.HolderPartyID, patientID)
		if e != nil {
			return nil, e
		}
		if sum.LedgerAvailable > 0 || sum.ReservedForRefund > 0 || sum.TotalCredited > 0 {
			out = append(out, *sum)
		}
	}
	return out, nil
}

func (s *Service) ListCreditApplicationsForInvoice(invoiceID uint) ([]CreditApplication, error) {
	var rows []CreditApplication
	e := s.db.Where("invoice_id=?", invoiceID).Order("id ASC").Find(&rows).Error
	return rows, e
}
