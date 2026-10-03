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

// Structured credit-note conflict codes (LOT29F-B).
const (
	CodeCreditNoteAlreadyExists               = "CREDIT_NOTE_ALREADY_EXISTS"
	CodeCreditNoteInvoiceNotEligible          = "CREDIT_NOTE_INVOICE_NOT_ELIGIBLE"
	CodeCreditNotePaymentReversalRequired     = "CREDIT_NOTE_PAYMENT_REVERSAL_REQUIRED"
	CodeCreditNoteCashCorrectionRequired      = "CREDIT_NOTE_CASH_CORRECTION_REQUIRED"
	CodeCreditNoteInsuranceCorrectionRequired = "CREDIT_NOTE_INSURANCE_CORRECTION_REQUIRED"
	CodeCreditNotePatientCreditPolicyRequired = "CREDIT_NOTE_PATIENT_CREDIT_POLICY_REQUIRED"
	CodeCreditNoteIdempotencyConflict         = "IDEMPOTENCY_CONFLICT"
)

type CreditNoteRequest struct {
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

func creditNoteFingerprintMatch(cn CreditNote, invoiceID uint, reason string) bool {
	return cn.InvoiceID == invoiceID && cn.Reason == reason
}

func creditNoteFingerprintConflict(cn CreditNote, invoiceID uint, reason string) error {
	if creditNoteFingerprintMatch(cn, invoiceID, reason) {
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

func applyCreditDecorations(inv *Invoice, credited int64, cn *CreditNote) {
	if inv == nil {
		return
	}
	inv.CreditedAmount = credited
	effPatient := inv.PatientAmount - credited
	if effPatient < 0 {
		effPatient = 0
	}
	inv.EffectivePatientAmount = effPatient
	effBal := effPatient - inv.PaidAmount
	if effBal < 0 {
		effBal = 0
	}
	inv.EffectiveBalanceAmount = effBal
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
		applyCreditDecorations(x, 0, nil)
		return
	}
	if e != nil {
		applyCreditDecorations(x, 0, nil)
		return
	}
	applyCreditDecorations(x, cn.Amount, &cn)
}

func hasActiveInsuranceAllocation(tx *gorm.DB, invoiceID uint) (bool, error) {
	var n int64
	e := tx.Model(&AuthorizationAllocation{}).
		Where("invoice_line_id IN (?)", tx.Model(&InvoiceLine{}).Select("id").Where("invoice_id=?", invoiceID)).
		Count(&n).Error
	return n > 0, e
}

func hasEffectiveCashSessionPayment(tx *gorm.DB, invoiceID uint) (bool, error) {
	var n int64
	e := tx.Raw(`
		SELECT COUNT(*)
		FROM billing_payments p
		LEFT JOIN billing_payment_reversals r ON r.original_payment_id = p.id
		WHERE p.invoice_id = ? AND r.id IS NULL AND p.cash_session_id IS NOT NULL
	`, invoiceID).Scan(&n).Error
	return n > 0, e
}

// IssueCreditNote creates an immutable full-invoice credit note for a safe V1 subset:
// unpaid ISSUED, patient-only (no insurance), no existing credit note.
// Does not reverse payments, refund money, mutate insurance, or create patient credit.
func (s *Service) IssueCreditNote(invoiceID uint, req CreditNoteRequest, user uint) (*Invoice, error) {
	reason, err := NormalizeCreditNoteReason(req.Reason)
	if err != nil {
		return nil, err
	}
	key, err := NormalizePaymentIdempotencyKey(req.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	var outID uint
	e := s.db.Transaction(func(tx *gorm.DB) error {
		var priorByKey CreditNote
		if e := tx.Where("idempotency_key=?", key).First(&priorByKey).Error; e == nil {
			if e := creditNoteFingerprintConflict(priorByKey, invoiceID, reason); e != nil {
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

		effectivePaid, e := EffectivePaidOnInvoice(tx, inv.ID)
		if e != nil {
			return e
		}
		if effectivePaid > 0 {
			cashPay, e := hasEffectiveCashSessionPayment(tx, inv.ID)
			if e != nil {
				return e
			}
			if cashPay {
				return creditNoteConflict(CodeCreditNoteCashCorrectionRequired, "Un encaissement de session de caisse empêche l'avoir tant que la correction caisse n'existe pas")
			}
			// Paid / partially paid full credit would invent patient credit / refund (PC5 / R3).
			// Sessionless remediation: reverse payments first, then cancel or issue avoir while unpaid.
			return creditNoteConflict(CodeCreditNotePaymentReversalRequired, "Contrepassation des encaissements requise avant avoir — un avoir sur facture encaissée créerait un crédit patient (politique requise)")
		}

		// V1: unpaid ISSUED only (Cancel remains available; avoir preserves issued invoice + corrective document).
		if inv.Status != InvoiceIssued {
			return creditNoteConflict(CodeCreditNoteInvoiceNotEligible, "Seul un avoir sur facture émise non encaissée (patient seul) est supporté en V1")
		}
		if inv.PatientAmount <= 0 {
			return creditNoteConflict(CodeCreditNoteInvoiceNotEligible, "Montant patient nul — avoir inutile")
		}

		amount := inv.PatientAmount // backend-authoritative full credit
		cn := CreditNote{
			InvoiceID:      inv.ID,
			Amount:         amount,
			Reason:         reason,
			IssuedBy:       user,
			IssuedAt:       time.Now(),
			IdempotencyKey: key,
			Number:         fmt.Sprintf("TMP-CN-%d", time.Now().UnixNano()),
		}
		if e := tx.Create(&cn).Error; e != nil {
			if isCreditNoteUniqueViolation(e) {
				var raced CreditNote
				if load := tx.Where("idempotency_key=?", key).First(&raced).Error; load == nil {
					if e := creditNoteFingerprintConflict(raced, invoiceID, reason); e != nil {
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
		cn.Number = fmt.Sprintf("CN-%06d", cn.ID)
		if e := tx.Model(&cn).Update("number", cn.Number).Error; e != nil {
			return e
		}

		// Project balance to zero without rewriting historical patient/gross/insurance amounts.
		inv.BalanceAmount = 0
		inv.UpdatedBy = user
		if e := tx.Save(&inv).Error; e != nil {
			return e
		}
		if e := s.timeline(tx, &inv, "credit_note_issued", "Avoir émis", user); e != nil {
			return e
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
	view := &CreditNoteView{
		CreditNotePublic:   toCreditNotePublic(cn),
		InvoiceNumber:      inv.Number,
		InvoiceGrossAmount: inv.GrossAmount,
		PatientAmount:      inv.PatientAmount,
		PatientID:          inv.PatientID,
		PatientName:        inv.PatientName,
		PatientCode:        inv.PatientCode,
	}
	var issuer struct{ Name string }
	s.db.Table("users").Select("name").Where("id=?", cn.IssuedBy).Scan(&issuer)
	view.IssuerName = strings.TrimSpace(issuer.Name)
	return view, nil
}
