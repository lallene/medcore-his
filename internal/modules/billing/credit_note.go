package billing

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	MaxCreditNoteReasonLen = 500
	MinCreditNoteReasonLen = 3
)

// Structured credit-note conflict codes (LOT29F-B / H-C).
const (
	CodeCreditNoteAlreadyExists               = "CREDIT_NOTE_ALREADY_EXISTS"
	CodeCreditNoteInvoiceNotEligible          = "CREDIT_NOTE_INVOICE_NOT_ELIGIBLE"
	CodeCreditNotePaymentReversalRequired     = "CREDIT_NOTE_PAYMENT_REVERSAL_REQUIRED"
	CodeCreditNoteCashCorrectionRequired      = "CREDIT_NOTE_CASH_CORRECTION_REQUIRED"
	CodeCreditNoteInsuranceCorrectionRequired = "CREDIT_NOTE_INSURANCE_CORRECTION_REQUIRED"
	CodeCreditNotePatientCreditPolicyRequired = "CREDIT_NOTE_PATIENT_CREDIT_POLICY_REQUIRED"
	CodeCreditNoteIdempotencyConflict         = "IDEMPOTENCY_CONFLICT"
)

// CreditNoteRequest — client supplies reduction amount + reason; never resulting credit.
type CreditNoteRequest struct {
	Amount         int64  `json:"amount"`
	Reason         string `json:"reason" binding:"required"`
	IdempotencyKey string `json:"idempotencyKey"`
}

type CreditNoteView struct {
	CreditNotePublic
	InvoiceNumber      string `json:"invoiceNumber"`
	InvoiceGrossAmount int64  `json:"invoiceGrossAmount"`
	PatientAmount      int64  `json:"patientAmount"`
	PatientID          uint   `json:"patientId"`
	PatientName        string `json:"patientName"`
	PatientCode        string `json:"patientCode"`
	IssuerName         string `json:"issuerName,omitempty"`
	CustomerCredit     int64  `json:"customerCredit"`
	HolderPartyID      *uint  `json:"holderPartyId,omitempty"`
}

func creditNoteConflict(code, message string) error {
	return coreerrors.New(409, code, message, nil)
}

func NormalizeCreditNoteReason(raw string) (string, error) {
	reason := strings.TrimSpace(raw)
	if reason == "" {
		return "", coreerrors.BadRequest("Motif obligatoire")
	}
	if utf8.RuneCountInString(reason) < MinCreditNoteReasonLen {
		return "", coreerrors.BadRequest("Motif trop court")
	}
	if utf8.RuneCountInString(reason) > MaxCreditNoteReasonLen {
		return "", coreerrors.BadRequest("Motif trop long")
	}
	return reason, nil
}

func creditNoteFingerprintMatch(cn CreditNote, invoiceID uint, reason string, amount int64) bool {
	return cn.InvoiceID == invoiceID && cn.Reason == reason && cn.Amount == amount
}

func creditNoteFingerprintConflict(cn CreditNote, invoiceID uint, reason string, amount int64) error {
	if creditNoteFingerprintMatch(cn, invoiceID, reason, amount) {
		return nil
	}
	return creditNoteConflict(CodeCreditNoteIdempotencyConflict, "Clé d'idempotence déjà utilisée")
}

func isCreditNoteUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, fragment := range []string{
		"duplicate key", "unique constraint", "sqlstate 23505",
		"invoice_id", "idempotency_key", "billing_credit_notes",
	} {
		if strings.Contains(msg, fragment) {
			return true
		}
	}
	return false
}

func toCreditNotePublic(cn CreditNote) CreditNotePublic {
	return CreditNotePublic{
		ID: cn.ID, Number: cn.Number, InvoiceID: cn.InvoiceID, Amount: cn.Amount,
		Reason: cn.Reason, IssuedBy: cn.IssuedBy, IssuedAt: cn.IssuedAt, CreatedAt: cn.CreatedAt,
	}
}

func applyCreditDecorations(inv *Invoice, credited int64, cn *CreditNote, customerCredit int64, holderPartyID *uint) {
	if inv == nil {
		return
	}
	inv.CreditedAmount = credited
	effPatient := CorrectedPatientObligation(inv.PatientAmount, credited)
	inv.EffectivePatientAmount = effPatient
	effBal := RemainingReceivable(effPatient, inv.PaidAmount)
	inv.EffectiveBalanceAmount = effBal
	inv.CustomerCreditAmount = customerCredit
	inv.CreditHolderPartyID = holderPartyID
	if cn != nil {
		pub := toCreditNotePublic(*cn)
		inv.CreditNote = &pub
	}
}

func (s *Service) attachCreditNote(x *Invoice) {
	if x == nil {
		return
	}
	var cn CreditNote
	e := s.db.Where("invoice_id=?", x.ID).First(&cn).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		applyCreditDecorations(x, 0, nil, 0, nil)
		return
	}
	if e != nil {
		applyCreditDecorations(x, 0, nil, 0, nil)
		return
	}
	creditAmt, _ := CreditLedgerAmountForSource(s.db, CreditSourceCreditNote, cn.ID)
	var holder *uint
	if creditAmt > 0 {
		var entry CreditLedgerEntry
		if s.db.Where("source_type=? AND source_id=?", CreditSourceCreditNote, cn.ID).First(&entry).Error == nil {
			h := entry.HolderPartyID
			holder = &h
		}
	}
	applyCreditDecorations(x, cn.Amount, &cn, creditAmt, holder)
}

func hasActiveInsuranceAllocation(tx *gorm.DB, invoiceID uint) (bool, error) {
	var n int64
	e := tx.Model(&AuthorizationAllocation{}).
		Where("invoice_line_id IN (?)", tx.Model(&InvoiceLine{}).Select("id").Where("invoice_id=?", invoiceID)).
		Count(&n).Error
	return n > 0, e
}

// IssueCreditNote creates an immutable CreditNote (one per invoice) with authoritative amount.
// LOT29F-H-C: paid/partial invoices may produce customer credit = max(effectivePaid - correctedObligation, 0).
// Retains one CreditNote per invoice (unique invoice_id); amount may be partial.
func (s *Service) IssueCreditNote(invoiceID uint, req CreditNoteRequest, user uint) (*Invoice, error) {
	reason, err := NormalizeCreditNoteReason(req.Reason)
	if err != nil {
		return nil, err
	}
	key, err := NormalizePaymentIdempotencyKey(req.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	if req.Amount <= 0 {
		return nil, coreerrors.BadRequest("Montant d'avoir obligatoire et strictement positif")
	}
	amount := req.Amount
	var outID uint
	e := s.db.Transaction(func(tx *gorm.DB) error {
		var priorByKey CreditNote
		if e := tx.Where("idempotency_key=?", key).First(&priorByKey).Error; e == nil {
			if e := creditNoteFingerprintConflict(priorByKey, invoiceID, reason, amount); e != nil {
				return e
			}
			outID = priorByKey.InvoiceID
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
		if e := LockEffectivePaymentsForInvoice(tx, inv.ID); e != nil {
			return e
		}

		var existing CreditNote
		if e := tx.Where("invoice_id=?", inv.ID).First(&existing).Error; e == nil {
			return creditNoteConflict(CodeCreditNoteAlreadyExists, "Un avoir existe déjà pour cette facture")
		} else if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}

		if inv.Status == InvoiceDraft {
			return creditNoteConflict(CodeCreditNoteInvoiceNotEligible, "Une facture brouillon se corrige par édition ou annulation, pas par avoir")
		}
		if inv.Status == InvoiceCancelled {
			return creditNoteConflict(CodeCreditNoteInvoiceNotEligible, "Une facture annulée ne peut pas recevoir d'avoir")
		}
		if inv.CoveragePending {
			return creditNoteConflict(CodeCreditNoteInvoiceNotEligible, "Une facture avec PEC en attente ne peut pas recevoir d'avoir")
		}
		if inv.Status != InvoiceIssued && inv.Status != InvoicePartiallyPaid && inv.Status != InvoicePaid {
			return creditNoteConflict(CodeCreditNoteInvoiceNotEligible, "Cette facture ne peut pas recevoir d'avoir")
		}

		if inv.InsuranceAmount > 0 {
			return creditNoteConflict(CodeCreditNoteInsuranceCorrectionRequired, "Une facture avec part assurance nécessite une correction assurance (LOT30)")
		}
		insured, e := hasActiveInsuranceAllocation(tx, inv.ID)
		if e != nil {
			return e
		}
		if insured {
			return creditNoteConflict(CodeCreditNoteInsuranceCorrectionRequired, "Une allocation assurance active nécessite une correction assurance (LOT30)")
		}

		if inv.PatientAmount <= 0 {
			return creditNoteConflict(CodeCreditNoteInvoiceNotEligible, "Montant patient nul — avoir inutile")
		}
		alreadyCredited, e := CreditedOnInvoice(tx, inv.ID)
		if e != nil {
			return e
		}
		remainingCorrectable := CorrectedPatientObligation(inv.PatientAmount, alreadyCredited)
		if amount > remainingCorrectable {
			return creditNoteConflict(CodeCreditNoteAmountInvalid,
				fmt.Sprintf("Montant d'avoir supérieur à l'obligation patient corrigible (%d)", remainingCorrectable))
		}

		effectivePaid, e := EffectivePaidOnInvoice(tx, inv.ID)
		if e != nil {
			return e
		}
		corrected := CorrectedPatientObligation(inv.PatientAmount, alreadyCredited+amount)
		customerCredit := CustomerCreditFromExcess(effectivePaid, corrected)

		holderID, e := ResolveCreditHolderForInvoice(tx, inv.ID, inv.PatientID, customerCredit)
		if e != nil {
			return e
		}

		cn := CreditNote{
			InvoiceID:      inv.ID,
			Amount:         amount,
			Reason:         reason,
			IssuedBy:       user,
			IssuedAt:       time.Now(),
			IdempotencyKey: key,
			Number:         fmt.Sprintf("TMP-CN-%d", time.Now().UnixNano()),
		}
		if e := tx.Exec("SAVEPOINT cn_idempotency").Error; e != nil {
			return e
		}
		if e := tx.Create(&cn).Error; e != nil {
			_ = tx.Exec("ROLLBACK TO SAVEPOINT cn_idempotency").Error
			if isCreditNoteUniqueViolation(e) {
				var raced CreditNote
				if load := tx.Where("idempotency_key=?", key).First(&raced).Error; load == nil {
					if e := creditNoteFingerprintConflict(raced, invoiceID, reason, amount); e != nil {
						return e
					}
					outID = raced.InvoiceID
					return nil
				}
				if load := tx.Where("invoice_id=?", inv.ID).First(&raced).Error; load == nil {
					return creditNoteConflict(CodeCreditNoteAlreadyExists, "Un avoir existe déjà pour cette facture")
				}
			}
			return e
		}
		if e := tx.Exec("RELEASE SAVEPOINT cn_idempotency").Error; e != nil {
			return e
		}
		cn.Number = fmt.Sprintf("CN-%06d", cn.ID)
		if e := tx.Model(&cn).Update("number", cn.Number).Error; e != nil {
			return e
		}

		if e := insertCreditLedgerFromCreditNote(tx, cn, inv, customerCredit, holderID, user, key); e != nil {
			return e
		}

		paid, balance, status := recomputeInvoiceStatus(inv.PatientAmount, alreadyCredited+amount, effectivePaid)
		inv.PaidAmount = paid
		inv.BalanceAmount = balance
		inv.Status = status
		inv.UpdatedBy = user
		if e := tx.Save(&inv).Error; e != nil {
			return e
		}
		if e := s.timeline(tx, &inv, "credit_note_issued", "Avoir émis", user); e != nil {
			return e
		}
		if customerCredit > 0 {
			if e := s.timeline(tx, &inv, "customer_credit_created", "Crédit financier créé", user); e != nil {
				return e
			}
		}
		outID = inv.ID
		return nil
	})
	if e != nil {
		return nil, e
	}
	return s.GetInvoice(outID)
}

func (s *Service) GetCreditNote(id uint) (*CreditNoteView, error) {
	var cn CreditNote
	if e := s.db.First(&cn, id).Error; e != nil {
		if errors.Is(e, gorm.ErrRecordNotFound) {
			return nil, coreerrors.NotFound("CREDIT_NOTE")
		}
		return nil, e
	}
	var inv Invoice
	if e := s.db.First(&inv, cn.InvoiceID).Error; e != nil {
		return nil, coreerrors.NotFound("INVOICE")
	}
	s.decorate(&inv)
	creditAmt, _ := CreditLedgerAmountForSource(s.db, CreditSourceCreditNote, cn.ID)
	view := &CreditNoteView{
		CreditNotePublic:   toCreditNotePublic(cn),
		InvoiceNumber:      inv.Number,
		InvoiceGrossAmount: inv.GrossAmount,
		PatientAmount:      inv.PatientAmount,
		PatientID:          inv.PatientID,
		PatientName:        inv.PatientName,
		PatientCode:        inv.PatientCode,
		CustomerCredit:     creditAmt,
		HolderPartyID:      inv.CreditHolderPartyID,
	}
	var issuer struct{ Name string }
	s.db.Table("users").Select("name").Where("id=?", cn.IssuedBy).Scan(&issuer)
	view.IssuerName = strings.TrimSpace(issuer.Name)
	return view, nil
}
