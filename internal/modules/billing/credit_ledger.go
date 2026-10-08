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

// Credit ledger entry types (extensible for H-D / Refund).
const (
	CreditEntryCredit = "CREDIT"
	CreditEntryApply  = "APPLY"
	CreditEntryRefund = "REFUND"
)

const (
	CreditSourceCreditNote = "CREDIT_NOTE"
)

const (
	CodeCreditHolderAmbiguous       = "CREDIT_HOLDER_AMBIGUOUS"
	CodeCreditLegacyPayerUnresolved = "CREDIT_LEGACY_PAYER_UNRESOLVED"
	CodeCreditReversalBlocked       = "CREDIT_REVERSAL_BLOCKED"
	CodeCreditNoteAmountInvalid     = "CREDIT_NOTE_AMOUNT_INVALID"
)

// CreditLedgerEntry is an append-only financial-value event (LOT29F-H-C).
// Holder = FinancialParty; clinical scope = Patient. Not a Payment, Refund, or CashMovement.
type CreditLedgerEntry struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	HolderPartyID  uint      `gorm:"not null;index;index:idx_credit_holder_patient,priority:1" json:"holderPartyId"`
	PatientID      uint      `gorm:"not null;index;index:idx_credit_holder_patient,priority:2" json:"patientId"`
	EntryType      string    `gorm:"size:20;not null;index" json:"entryType"` // CREDIT|APPLY|REFUND
	Amount         int64     `gorm:"not null;check:billing_credit_ledger_amount_positive,amount > 0" json:"amount"`
	SourceType     string    `gorm:"size:40;not null;uniqueIndex:ux_credit_ledger_source" json:"sourceType"`
	SourceID       uint      `gorm:"not null;uniqueIndex:ux_credit_ledger_source" json:"sourceId"`
	Reason         string    `gorm:"size:500" json:"reason,omitempty"`
	CreatedBy      uint      `gorm:"not null;index" json:"createdBy"`
	CreatedAt      time.Time `json:"createdAt"`
	IdempotencyKey string    `gorm:"size:120;not null;uniqueIndex" json:"idempotencyKey"`
}

func (CreditLedgerEntry) TableName() string { return "billing_credit_ledger_entries" }

// CreditSummary is the backend-authoritative available-credit projection (LOT29F-I-A).
// LedgerAvailable = CREDIT − APPLY − REFUND(executed).
// ReservedForRefund = Σ REQUESTED+APPROVED refund amounts.
// SpendableCredit / AvailableCredit = max(0, LedgerAvailable − ReservedForRefund).
// AvailableCredit remains the apply/UX spendable field (backward-compatible name).
type CreditSummary struct {
	HolderPartyID     uint  `json:"holderPartyId"`
	PatientID         uint  `json:"patientId"`
	TotalCredited     int64 `json:"totalCredited"`
	TotalApplied      int64 `json:"totalApplied"`
	TotalRefunded     int64 `json:"totalRefunded"`
	LedgerAvailable   int64 `json:"ledgerAvailable"`
	ReservedForRefund int64 `json:"reservedForRefund"`
	SpendableCredit   int64 `json:"spendableCredit"`
	AvailableCredit   int64 `json:"availableCredit"` // == SpendableCredit (apply ceiling)
}

func CorrectedPatientObligation(patientAmount, credited int64) int64 {
	v := patientAmount - credited
	if v < 0 {
		return 0
	}
	return v
}

func CustomerCreditFromExcess(effectivePaid, correctedObligation int64) int64 {
	v := effectivePaid - correctedObligation
	if v < 0 {
		return 0
	}
	return v
}

func RemainingReceivable(correctedObligation, effectivePaid int64) int64 {
	v := correctedObligation - effectivePaid
	if v < 0 {
		return 0
	}
	return v
}

// AvailableCredit derives spendable credit for holder+patient (ledger − refund reservations).
func AvailableCredit(tx *gorm.DB, holderPartyID, patientID uint) (int64, error) {
	sum, err := CreditSummaryFor(tx, holderPartyID, patientID)
	if err != nil {
		return 0, err
	}
	return sum.SpendableCredit, nil
}

func CreditSummaryFor(tx *gorm.DB, holderPartyID, patientID uint) (*CreditSummary, error) {
	type row struct {
		EntryType string
		Total     int64
	}
	var rows []row
	e := tx.Model(&CreditLedgerEntry{}).
		Select("entry_type, COALESCE(SUM(amount),0) AS total").
		Where("holder_party_id=? AND patient_id=?", holderPartyID, patientID).
		Group("entry_type").
		Scan(&rows).Error
	if e != nil {
		return nil, e
	}
	out := &CreditSummary{HolderPartyID: holderPartyID, PatientID: patientID}
	for _, r := range rows {
		switch r.EntryType {
		case CreditEntryCredit:
			out.TotalCredited = r.Total
		case CreditEntryApply:
			out.TotalApplied = r.Total
		case CreditEntryRefund:
			out.TotalRefunded = r.Total
		}
	}
	ledger := out.TotalCredited - out.TotalApplied - out.TotalRefunded
	if ledger < 0 {
		ledger = 0
	}
	out.LedgerAvailable = ledger
	reserved, e := ReservedForRefund(tx, holderPartyID, patientID)
	if e != nil {
		return nil, e
	}
	out.ReservedForRefund = reserved
	spendable := ledger - reserved
	if spendable < 0 {
		spendable = 0
	}
	out.SpendableCredit = spendable
	out.AvailableCredit = spendable
	return out, nil
}

func CreditLedgerAmountForSource(tx *gorm.DB, sourceType string, sourceID uint) (int64, error) {
	var amt int64
	e := tx.Model(&CreditLedgerEntry{}).
		Where("source_type=? AND source_id=? AND entry_type=?", sourceType, sourceID, CreditEntryCredit).
		Select("COALESCE(SUM(amount),0)").Scan(&amt).Error
	return amt, e
}

// InvoiceHasPositiveCustomerCredit reports whether a CreditNote on this invoice produced CREDIT.
func InvoiceHasPositiveCustomerCredit(tx *gorm.DB, invoiceID uint) (bool, error) {
	var n int64
	e := tx.Raw(`
		SELECT COUNT(*)
		FROM billing_credit_ledger_entries e
		INNER JOIN billing_credit_notes cn ON cn.id = e.source_id AND e.source_type = ?
		WHERE cn.invoice_id = ? AND e.entry_type = ? AND e.amount > 0
	`, CreditSourceCreditNote, invoiceID, CreditEntryCredit).Scan(&n).Error
	return n > 0, e
}

// ResolveCreditHolderForInvoice picks the unique confirmed FinancialParty among effective payments.
// Fail-closed when creditAmount > 0 and payer authority is legacy/missing/ambiguous.
func ResolveCreditHolderForInvoice(tx *gorm.DB, invoiceID uint, patientID uint, creditAmount int64) (uint, error) {
	if creditAmount <= 0 {
		return 0, nil
	}
	var pays []Payment
	if e := tx.Raw(`
		SELECT p.*
		FROM billing_payments p
		LEFT JOIN billing_payment_reversals r ON r.original_payment_id = p.id
		WHERE p.invoice_id = ? AND r.id IS NULL
		ORDER BY p.id
	`, invoiceID).Scan(&pays).Error; e != nil {
		return 0, e
	}
	if len(pays) == 0 {
		return 0, creditNoteConflict(CodeCreditLegacyPayerUnresolved,
			"Crédit client impossible — aucun encaissement confirmé pour attribuer le titulaire")
	}
	var holder uint
	for _, p := range pays {
		if p.PayerProvenance != PayerProvenanceCaptured || p.PayerPartyID == nil || *p.PayerPartyID == 0 {
			return 0, creditNoteConflict(CodeCreditLegacyPayerUnresolved,
				"Crédit client impossible — payeur historique non confirmé (LEGACY_UNCONFIRMED)")
		}
		if holder == 0 {
			holder = *p.PayerPartyID
			continue
		}
		if holder != *p.PayerPartyID {
			return 0, creditNoteConflict(CodeCreditHolderAmbiguous,
				"Crédit client impossible — plusieurs payeurs distincts sur la facture")
		}
	}
	var party FinancialParty
	if e := tx.First(&party, holder).Error; e != nil {
		if errors.Is(e, gorm.ErrRecordNotFound) {
			return 0, creditNoteConflict(CodeCreditLegacyPayerUnresolved, "Titulaire financier introuvable")
		}
		return 0, e
	}
	if party.Kind == PartyKindIndividual && party.PatientID != nil && *party.PatientID != patientID {
		return 0, creditNoteConflict(CodeCreditHolderAmbiguous, "Titulaire financier incohérent avec le patient")
	}
	return holder, nil
}

func insertCreditLedgerFromCreditNote(tx *gorm.DB, cn CreditNote, inv Invoice, amount int64, holderPartyID, user uint, idemKey string) error {
	if amount <= 0 {
		return nil
	}
	entry := CreditLedgerEntry{
		HolderPartyID:  holderPartyID,
		PatientID:      inv.PatientID,
		EntryType:      CreditEntryCredit,
		Amount:         amount,
		SourceType:     CreditSourceCreditNote,
		SourceID:       cn.ID,
		Reason:         cn.Reason,
		CreatedBy:      user,
		CreatedAt:      time.Now(),
		IdempotencyKey: fmt.Sprintf("cn-credit-%s", idemKey),
	}
	if e := tx.Exec("SAVEPOINT credit_ledger_insert").Error; e != nil {
		return e
	}
	if e := tx.Create(&entry).Error; e != nil {
		_ = tx.Exec("ROLLBACK TO SAVEPOINT credit_ledger_insert").Error
		if isCreditLedgerUniqueViolation(e) {
			var raced CreditLedgerEntry
			if load := tx.Where("source_type=? AND source_id=?", CreditSourceCreditNote, cn.ID).First(&raced).Error; load == nil {
				if raced.Amount == amount && raced.HolderPartyID == holderPartyID && raced.PatientID == inv.PatientID {
					return nil
				}
				return creditNoteConflict(CodeCreditNoteIdempotencyConflict, "Crédit déjà produit pour cet avoir avec un autre effet")
			}
			if load := tx.Where("idempotency_key=?", entry.IdempotencyKey).First(&raced).Error; load == nil {
				return nil
			}
		}
		return e
	}
	return tx.Exec("RELEASE SAVEPOINT credit_ledger_insert").Error
}

func isCreditLedgerUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, fragment := range []string{
		"duplicate key", "unique constraint", "sqlstate 23505", "ux_credit_ledger_source",
		"billing_credit_ledger_entries",
	} {
		if strings.Contains(msg, fragment) {
			return true
		}
	}
	return false
}

// LockEffectivePaymentsForInvoice locks non-reversed payment rows for CN vs Pay/Reverse races.
func LockEffectivePaymentsForInvoice(tx *gorm.DB, invoiceID uint) error {
	var pays []Payment
	return tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where(`id IN (
			SELECT p.id FROM billing_payments p
			LEFT JOIN billing_payment_reversals r ON r.original_payment_id = p.id
			WHERE p.invoice_id = ? AND r.id IS NULL
		)`, invoiceID).
		Order("id ASC").
		Find(&pays).Error
}

// GetCreditSummary is the read projection for authorized financial users.
func (s *Service) GetCreditSummary(holderPartyID, patientID uint) (*CreditSummary, error) {
	if holderPartyID == 0 || patientID == 0 {
		return nil, coreerrors.BadRequest("Titulaire et patient requis")
	}
	var party FinancialParty
	if e := s.db.First(&party, holderPartyID).Error; e != nil {
		if errors.Is(e, gorm.ErrRecordNotFound) {
			return nil, coreerrors.NotFound("FINANCIAL_PARTY")
		}
		return nil, e
	}
	return CreditSummaryFor(s.db, holderPartyID, patientID)
}

// ListCreditLedger returns append-only history for holder+patient (authorized read).
func (s *Service) ListCreditLedger(holderPartyID, patientID uint) ([]CreditLedgerEntry, error) {
	if holderPartyID == 0 || patientID == 0 {
		return nil, coreerrors.BadRequest("Titulaire et patient requis")
	}
	var rows []CreditLedgerEntry
	e := s.db.Where("holder_party_id=? AND patient_id=?", holderPartyID, patientID).
		Order("id ASC").Find(&rows).Error
	return rows, e
}
