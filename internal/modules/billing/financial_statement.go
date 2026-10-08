package billing

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
	"gorm.io/gorm"
)

// Financial event types (Refund request lifecycle added in LOT29F-I-A; EXECUTED is I-B).
const (
	FinEventInvoiceIssued             = "INVOICE_ISSUED"
	FinEventPaymentReceived           = "PAYMENT_RECEIVED"
	FinEventPaymentReversed           = "PAYMENT_REVERSED"
	FinEventCreditNoteIssued          = "CREDIT_NOTE_ISSUED"
	FinEventCreditEarned              = "CREDIT_EARNED"
	FinEventCreditApplied             = "CREDIT_APPLIED"
	FinEventCreditApplicationReversed = "CREDIT_APPLICATION_REVERSED"
	FinEventRefundRequested           = "REFUND_REQUESTED"
	FinEventRefundApproved            = "REFUND_APPROVED"
	FinEventRefundRejected            = "REFUND_REJECTED"
	FinEventRefundCancelled           = "REFUND_CANCELLED"
	FinEventRefundExecuted            = "REFUND_EXECUTED"
)

// FinancialStatement is a read-only projection over authoritative domain records (LOT29F-H-E).
// It is NOT a mutable PatientBalance authority and never writes financial state.
type FinancialStatement struct {
	PatientID   uint                      `json:"patientId"`
	PatientCode string                    `json:"patientCode"`
	PatientName string                    `json:"patientName"`
	AsOf        time.Time                 `json:"asOf"`
	Summary     FinancialStatementSummary `json:"summary"`
	Invoices    []FinancialInvoiceLine    `json:"invoices"`
	Holders     []FinancialHolderCredit   `json:"holders"`
	// PatientCreditTotalAvailable is informational Σ holder available (ownership stays per-holder).
	PatientCreditTotalAvailable int64 `json:"patientCreditTotalAvailable"`
}

type FinancialStatementSummary struct {
	GrossPatientObligation     int64 `json:"grossPatientObligation"`
	CreditNoteReduction        int64 `json:"creditNoteReduction"`
	CorrectedPatientObligation int64 `json:"correctedPatientObligation"`
	EffectiveMoneyPaid         int64 `json:"effectiveMoneyPaid"`
	CreditApplied              int64 `json:"creditApplied"`
	TotalSettled               int64 `json:"totalSettled"`
	ReceivableOutstanding      int64 `json:"receivableOutstanding"`
	CreditEarned               int64 `json:"creditEarned"`
	CreditRestored             int64 `json:"creditRestored"`
	CreditUsed                 int64 `json:"creditUsed"`
	// CreditAvailable = spendable (ledger − reserved). Pending refunds are not money returned.
	CreditAvailable       int64 `json:"creditAvailable"`
	LedgerCreditAvailable int64 `json:"ledgerCreditAvailable"`
	ReservedForRefund     int64 `json:"reservedForRefund"`
	SpendableCredit       int64 `json:"spendableCredit"`
}

type FinancialInvoiceLine struct {
	InvoiceID              uint       `json:"invoiceId"`
	Number                 string     `json:"number"`
	Status                 string     `json:"status"`
	IssuedAt               *time.Time `json:"issuedAt,omitempty"`
	CreatedAt              time.Time  `json:"createdAt"`
	GrossPatientObligation int64      `json:"grossPatientObligation"`
	CreditNoteReduction    int64      `json:"creditNoteReduction"`
	CorrectedObligation    int64      `json:"correctedObligation"`
	EffectiveMoneyPaid     int64      `json:"effectiveMoneyPaid"`
	CreditApplied          int64      `json:"creditApplied"`
	TotalSettled           int64      `json:"totalSettled"`
	RemainingReceivable    int64      `json:"remainingReceivable"`
	CreditNoteID           *uint      `json:"creditNoteId,omitempty"`
	CreditNoteNumber       string     `json:"creditNoteNumber,omitempty"`
}

type FinancialHolderCredit struct {
	HolderPartyID     uint   `json:"holderPartyId"`
	Kind              string `json:"kind"`
	DisplayName       string `json:"displayName"`
	Phone             string `json:"phone,omitempty"`
	CreditEarned      int64  `json:"creditEarned"`
	CreditRestored    int64  `json:"creditRestored"`
	CreditUsed        int64  `json:"creditUsed"`
	CreditRefunded    int64  `json:"creditRefunded"`
	LedgerAvailable   int64  `json:"ledgerAvailable"`
	ReservedForRefund int64  `json:"reservedForRefund"`
	SpendableCredit   int64  `json:"spendableCredit"`
	AvailableCredit   int64  `json:"availableCredit"` // spendable
}

type FinancialHistoryEvent struct {
	EventType       string    `json:"eventType"`
	OccurredAt      time.Time `json:"occurredAt"`
	Amount          int64     `json:"amount"`
	SourceType      string    `json:"sourceType"`
	SourceID        uint      `json:"sourceId"`
	InvoiceID       *uint     `json:"invoiceId,omitempty"`
	InvoiceNumber   string    `json:"invoiceNumber,omitempty"`
	DocumentNumber  string    `json:"documentNumber,omitempty"` // e.g. RMB-YYYY-XXXXXX for REFUND_EXECUTED
	HolderPartyID   *uint     `json:"holderPartyId,omitempty"`
	PaymentID       *uint     `json:"paymentId,omitempty"`
	CreditNoteID    *uint     `json:"creditNoteId,omitempty"`
	ApplicationID   *uint     `json:"creditApplicationId,omitempty"`
	PayerDisplay    string    `json:"payerDisplay,omitempty"`
	PayerProvenance string    `json:"payerProvenance,omitempty"`
	PayerUnknown    bool      `json:"payerUnknown,omitempty"`
	Label           string    `json:"label"`
	SortKey         string    `json:"sortKey"`
}

type FinancialHistoryPage struct {
	Data       []FinancialHistoryEvent `json:"data"`
	Page       int                     `json:"page"`
	Limit      int                     `json:"limit"`
	Total      int64                   `json:"total"`
	TotalPages int                     `json:"totalPages"`
}

type FinancialHistoryFilter struct {
	PatientID  uint
	Page       int
	Limit      int
	DateFrom   string // YYYY-MM-DD inclusive
	DateTo     string
	EventType  string
	InvoiceID  uint
	HolderID   uint
	IncludePII bool // billing.payer.read
}

// GetFinancialStatement builds a consistent read-only projection in one REPEATABLE READ snapshot.
func (s *Service) GetFinancialStatement(patientID uint, includePayerPII bool) (*FinancialStatement, error) {
	if patientID == 0 {
		return nil, coreerrors.BadRequest("Patient requis")
	}
	var out *FinancialStatement
	e := s.db.Transaction(func(tx *gorm.DB) error {
		if err := setRepeatableRead(tx); err != nil {
			return err
		}
		var p patients.Patient
		if e := tx.Select("id", "code_patient", "nom", "prenoms").First(&p, patientID).Error; e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return coreerrors.NotFound("PATIENT")
			}
			return e
		}
		stmt, err := buildFinancialStatement(tx, p, includePayerPII)
		if err != nil {
			return err
		}
		out = stmt
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if e != nil {
		return nil, e
	}
	return out, nil
}

func setRepeatableRead(tx *gorm.DB) error {
	// GORM TxOptions already set isolation; no-op keep for clarity / non-PG drivers.
	_ = tx
	return nil
}

func buildFinancialStatement(tx *gorm.DB, p patients.Patient, includePayerPII bool) (*FinancialStatement, error) {
	var invoices []Invoice
	if e := tx.Where("patient_id=? AND status <> ?", p.ID, InvoiceDraft).
		Order("id ASC").Find(&invoices).Error; e != nil {
		return nil, e
	}
	invIDs := make([]uint, 0, len(invoices))
	for _, inv := range invoices {
		invIDs = append(invIDs, inv.ID)
	}

	creditedMap, err := creditedByInvoice(tx, invIDs)
	if err != nil {
		return nil, err
	}
	moneyMap, err := moneyPaidByInvoice(tx, invIDs)
	if err != nil {
		return nil, err
	}
	appliedMap, err := creditAppliedByInvoice(tx, invIDs)
	if err != nil {
		return nil, err
	}
	cnByInv, err := creditNotesByInvoice(tx, invIDs)
	if err != nil {
		return nil, err
	}

	lines := make([]FinancialInvoiceLine, 0, len(invoices))
	var sum FinancialStatementSummary
	for _, inv := range invoices {
		if inv.Status == InvoiceCancelled {
			// Cancelled invoices stay visible in history; exclude from obligation/receivable aggregates.
			continue
		}
		credited := creditedMap[inv.ID]
		money := moneyMap[inv.ID]
		applied := appliedMap[inv.ID]
		corrected := CorrectedPatientObligation(inv.PatientAmount, credited)
		receivable := RemainingReceivableAfterSettlement(corrected, money, applied)
		line := FinancialInvoiceLine{
			InvoiceID:              inv.ID,
			Number:                 inv.Number,
			Status:                 inv.Status,
			IssuedAt:               inv.IssuedAt,
			CreatedAt:              inv.CreatedAt,
			GrossPatientObligation: inv.PatientAmount,
			CreditNoteReduction:    credited,
			CorrectedObligation:    corrected,
			EffectiveMoneyPaid:     money,
			CreditApplied:          applied,
			TotalSettled:           money + applied,
			RemainingReceivable:    receivable,
		}
		if cn, ok := cnByInv[inv.ID]; ok {
			id := cn.ID
			line.CreditNoteID = &id
			line.CreditNoteNumber = cn.Number
		}
		lines = append(lines, line)
		sum.GrossPatientObligation += inv.PatientAmount
		sum.CreditNoteReduction += credited
		sum.CorrectedPatientObligation += corrected
		sum.EffectiveMoneyPaid += money
		sum.CreditApplied += applied
		sum.TotalSettled += money + applied
		sum.ReceivableOutstanding += receivable
	}

	holders, err := holderCreditsForPatient(tx, p.ID, includePayerPII)
	if err != nil {
		return nil, err
	}
	var totalSpendable, totalLedger, totalReserved, earned, restored, used int64
	for _, h := range holders {
		totalSpendable += h.SpendableCredit
		totalLedger += h.LedgerAvailable
		totalReserved += h.ReservedForRefund
		earned += h.CreditEarned
		restored += h.CreditRestored
		used += h.CreditUsed
	}
	sum.CreditEarned = earned
	sum.CreditRestored = restored
	sum.CreditUsed = used
	sum.LedgerCreditAvailable = totalLedger
	sum.ReservedForRefund = totalReserved
	sum.SpendableCredit = totalSpendable
	sum.CreditAvailable = totalSpendable

	name := strings.TrimSpace(strings.TrimSpace(p.Prenoms) + " " + strings.TrimSpace(p.Nom))
	return &FinancialStatement{
		PatientID:                   p.ID,
		PatientCode:                 p.CodePatient,
		PatientName:                 name,
		AsOf:                        time.Now().UTC(),
		Summary:                     sum,
		Invoices:                    lines,
		Holders:                     holders,
		PatientCreditTotalAvailable: totalSpendable,
	}, nil
}

func creditedByInvoice(tx *gorm.DB, invIDs []uint) (map[uint]int64, error) {
	out := map[uint]int64{}
	if len(invIDs) == 0 {
		return out, nil
	}
	type row struct {
		InvoiceID uint
		Credited  int64
	}
	var rows []row
	e := tx.Raw(`SELECT invoice_id, COALESCE(SUM(amount),0) AS credited FROM billing_credit_notes WHERE invoice_id IN ? GROUP BY invoice_id`, invIDs).Scan(&rows).Error
	for _, r := range rows {
		out[r.InvoiceID] = r.Credited
	}
	return out, e
}

func moneyPaidByInvoice(tx *gorm.DB, invIDs []uint) (map[uint]int64, error) {
	out := map[uint]int64{}
	if len(invIDs) == 0 {
		return out, nil
	}
	type row struct {
		InvoiceID uint
		Paid      int64
	}
	var rows []row
	e := tx.Raw(`
		SELECT p.invoice_id, COALESCE(SUM(p.amount),0) AS paid
		FROM billing_payments p
		LEFT JOIN billing_payment_reversals r ON r.original_payment_id = p.id
		WHERE p.invoice_id IN ? AND r.id IS NULL
		GROUP BY p.invoice_id
	`, invIDs).Scan(&rows).Error
	for _, r := range rows {
		out[r.InvoiceID] = r.Paid
	}
	return out, e
}

func creditAppliedByInvoice(tx *gorm.DB, invIDs []uint) (map[uint]int64, error) {
	out := map[uint]int64{}
	if len(invIDs) == 0 {
		return out, nil
	}
	type row struct {
		InvoiceID uint
		Applied   int64
	}
	var rows []row
	e := tx.Raw(`
		SELECT a.invoice_id, COALESCE(SUM(a.amount),0) AS applied
		FROM billing_credit_applications a
		LEFT JOIN billing_credit_application_reversals r ON r.original_application_id = a.id
		WHERE a.invoice_id IN ? AND r.id IS NULL
		GROUP BY a.invoice_id
	`, invIDs).Scan(&rows).Error
	for _, r := range rows {
		out[r.InvoiceID] = r.Applied
	}
	return out, e
}

func creditNotesByInvoice(tx *gorm.DB, invIDs []uint) (map[uint]CreditNote, error) {
	out := map[uint]CreditNote{}
	if len(invIDs) == 0 {
		return out, nil
	}
	var notes []CreditNote
	if e := tx.Where("invoice_id IN ?", invIDs).Find(&notes).Error; e != nil {
		return nil, e
	}
	for _, n := range notes {
		out[n.InvoiceID] = n
	}
	return out, nil
}

func holderCreditsForPatient(tx *gorm.DB, patientID uint, includePayerPII bool) ([]FinancialHolderCredit, error) {
	type holderRow struct {
		HolderPartyID uint
	}
	var holders []holderRow
	if e := tx.Model(&CreditLedgerEntry{}).
		Select("DISTINCT holder_party_id AS holder_party_id").
		Where("patient_id=?", patientID).
		Scan(&holders).Error; e != nil {
		return nil, e
	}
	out := make([]FinancialHolderCredit, 0, len(holders))
	for _, h := range holders {
		var party FinancialParty
		if e := tx.First(&party, h.HolderPartyID).Error; e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				continue
			}
			return nil, e
		}
		// Split CREDIT: earned from CN vs restored from application reversal.
		type typed struct {
			EntryType  string
			SourceType string
			Total      int64
		}
		var rows []typed
		if e := tx.Model(&CreditLedgerEntry{}).
			Select("entry_type, source_type, COALESCE(SUM(amount),0) AS total").
			Where("holder_party_id=? AND patient_id=?", h.HolderPartyID, patientID).
			Group("entry_type, source_type").
			Scan(&rows).Error; e != nil {
			return nil, e
		}
		var earned, restored, used, refunded int64
		for _, r := range rows {
			switch r.EntryType {
			case CreditEntryCredit:
				if r.SourceType == CreditSourceCreditApplicationReversal {
					restored += r.Total
				} else {
					earned += r.Total
				}
			case CreditEntryApply:
				used += r.Total
			case CreditEntryRefund:
				refunded += r.Total
			}
		}
		ledger := earned + restored - used - refunded
		if ledger < 0 {
			ledger = 0
		}
		reserved, e := ReservedForRefund(tx, h.HolderPartyID, patientID)
		if e != nil {
			return nil, e
		}
		spendable := ledger - reserved
		if spendable < 0 {
			spendable = 0
		}
		phone := ""
		if includePayerPII {
			phone = party.Phone
		}
		out = append(out, FinancialHolderCredit{
			HolderPartyID:     party.ID,
			Kind:              party.Kind,
			DisplayName:       party.DisplayName,
			Phone:             phone,
			CreditEarned:      earned,
			CreditRestored:    restored,
			CreditUsed:        used,
			CreditRefunded:    refunded,
			LedgerAvailable:   ledger,
			ReservedForRefund: reserved,
			SpendableCredit:   spendable,
			AvailableCredit:   spendable,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].HolderPartyID < out[j].HolderPartyID })
	return out, nil
}

// ListFinancialHistory returns a deterministic paginated event projection.
// Ordering: occurred_at ASC, event_type ASC, source_type ASC, source_id ASC (stable, no skip/dup under equal timestamps).
func (s *Service) ListFinancialHistory(f FinancialHistoryFilter) (*FinancialHistoryPage, error) {
	if f.PatientID == 0 {
		return nil, coreerrors.BadRequest("Patient requis")
	}
	if f.Page < 1 {
		f.Page = 1
	}
	if f.Limit < 1 || f.Limit > 100 {
		f.Limit = 20
	}
	var page *FinancialHistoryPage
	e := s.db.Transaction(func(tx *gorm.DB) error {
		if err := setRepeatableRead(tx); err != nil {
			return err
		}
		var p patients.Patient
		if e := tx.Select("id").First(&p, f.PatientID).Error; e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return coreerrors.NotFound("PATIENT")
			}
			return e
		}
		events, err := collectFinancialEvents(tx, f)
		if err != nil {
			return err
		}
		total := int64(len(events))
		start := (f.Page - 1) * f.Limit
		if start > len(events) {
			start = len(events)
		}
		end := start + f.Limit
		if end > len(events) {
			end = len(events)
		}
		slice := events[start:end]
		tp := int((total + int64(f.Limit) - 1) / int64(f.Limit))
		if tp == 0 && total == 0 {
			tp = 0
		}
		page = &FinancialHistoryPage{Data: slice, Page: f.Page, Limit: f.Limit, Total: total, TotalPages: tp}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if e != nil {
		return nil, e
	}
	return page, nil
}

func collectFinancialEvents(tx *gorm.DB, f FinancialHistoryFilter) ([]FinancialHistoryEvent, error) {
	var invoices []Invoice
	q := tx.Where("patient_id=?", f.PatientID)
	if f.InvoiceID > 0 {
		q = q.Where("id=?", f.InvoiceID)
	}
	if e := q.Order("id ASC").Find(&invoices).Error; e != nil {
		return nil, e
	}
	invByID := map[uint]Invoice{}
	invIDs := make([]uint, 0, len(invoices))
	for _, inv := range invoices {
		invByID[inv.ID] = inv
		invIDs = append(invIDs, inv.ID)
	}

	events := make([]FinancialHistoryEvent, 0, 64)

	for _, inv := range invoices {
		if inv.Status == InvoiceDraft {
			continue
		}
		occurred := inv.CreatedAt
		if inv.IssuedAt != nil {
			occurred = *inv.IssuedAt
		}
		id := inv.ID
		events = append(events, FinancialHistoryEvent{
			EventType:     FinEventInvoiceIssued,
			OccurredAt:    occurred,
			Amount:        inv.PatientAmount,
			SourceType:    "INVOICE",
			SourceID:      inv.ID,
			InvoiceID:     &id,
			InvoiceNumber: inv.Number,
			Label:         "Facture émise",
		})
	}

	if len(invIDs) > 0 {
		var pays []Payment
		if e := tx.Where("invoice_id IN ?", invIDs).Order("id ASC").Find(&pays).Error; e != nil {
			return nil, e
		}
		var revs []PaymentReversal
		payIDs := make([]uint, 0, len(pays))
		for _, p := range pays {
			payIDs = append(payIDs, p.ID)
		}
		if len(payIDs) > 0 {
			if e := tx.Where("original_payment_id IN ?", payIDs).Order("id ASC").Find(&revs).Error; e != nil {
				return nil, e
			}
		}
		revByPay := map[uint]PaymentReversal{}
		for _, r := range revs {
			revByPay[r.OriginalPaymentID] = r
		}
		for _, p := range pays {
			inv := invByID[p.InvoiceID]
			pid := p.InvoiceID
			payID := p.ID
			unknown := p.PayerProvenance == PayerProvenanceLegacyUnconfirmed || p.PayerProvenance == ""
			display := p.PayerDisplayName
			if unknown {
				display = "Payeur non renseigné (historique)"
			} else if !f.IncludePII {
				// keep display name; phone never on history events
			}
			ev := FinancialHistoryEvent{
				EventType:       FinEventPaymentReceived,
				OccurredAt:      p.PaidAt,
				Amount:          p.Amount,
				SourceType:      "PAYMENT",
				SourceID:        p.ID,
				InvoiceID:       &pid,
				InvoiceNumber:   inv.Number,
				PaymentID:       &payID,
				PayerDisplay:    display,
				PayerProvenance: p.PayerProvenance,
				PayerUnknown:    unknown,
				Label:           "Encaissement",
			}
			if p.PayerPartyID != nil {
				h := *p.PayerPartyID
				ev.HolderPartyID = &h
			}
			events = append(events, ev)
			if r, ok := revByPay[p.ID]; ok {
				events = append(events, FinancialHistoryEvent{
					EventType:     FinEventPaymentReversed,
					OccurredAt:    r.ReversedAt,
					Amount:        r.Amount,
					SourceType:    "PAYMENT_REVERSAL",
					SourceID:      r.ID,
					InvoiceID:     &pid,
					InvoiceNumber: inv.Number,
					PaymentID:     &payID,
					Label:         "Paiement contrepassé",
				})
			}
		}

		var notes []CreditNote
		if e := tx.Where("invoice_id IN ?", invIDs).Order("id ASC").Find(&notes).Error; e != nil {
			return nil, e
		}
		for _, cn := range notes {
			inv := invByID[cn.InvoiceID]
			pid := cn.InvoiceID
			cnID := cn.ID
			events = append(events, FinancialHistoryEvent{
				EventType:     FinEventCreditNoteIssued,
				OccurredAt:    cn.IssuedAt,
				Amount:        cn.Amount,
				SourceType:    "CREDIT_NOTE",
				SourceID:      cn.ID,
				InvoiceID:     &pid,
				InvoiceNumber: inv.Number,
				CreditNoteID:  &cnID,
				Label:         "Avoir émis",
			})
		}

		var apps []CreditApplication
		aq := tx.Where("patient_id=?", f.PatientID)
		if f.InvoiceID > 0 {
			aq = aq.Where("invoice_id=?", f.InvoiceID)
		}
		if f.HolderID > 0 {
			aq = aq.Where("holder_party_id=?", f.HolderID)
		}
		if e := aq.Order("id ASC").Find(&apps).Error; e != nil {
			return nil, e
		}
		appIDs := make([]uint, 0, len(apps))
		for _, a := range apps {
			appIDs = append(appIDs, a.ID)
		}
		var appRevs []CreditApplicationReversal
		if len(appIDs) > 0 {
			if e := tx.Where("original_application_id IN ?", appIDs).Order("id ASC").Find(&appRevs).Error; e != nil {
				return nil, e
			}
		}
		revByApp := map[uint]CreditApplicationReversal{}
		for _, r := range appRevs {
			revByApp[r.OriginalApplicationID] = r
		}
		for _, a := range apps {
			inv := invByID[a.InvoiceID]
			pid := a.InvoiceID
			hid := a.HolderPartyID
			aid := a.ID
			events = append(events, FinancialHistoryEvent{
				EventType:     FinEventCreditApplied,
				OccurredAt:    a.CreatedAt,
				Amount:        a.Amount,
				SourceType:    "CREDIT_APPLICATION",
				SourceID:      a.ID,
				InvoiceID:     &pid,
				InvoiceNumber: inv.Number,
				HolderPartyID: &hid,
				ApplicationID: &aid,
				Label:         "Crédit utilisé",
			})
			if r, ok := revByApp[a.ID]; ok {
				events = append(events, FinancialHistoryEvent{
					EventType:     FinEventCreditApplicationReversed,
					OccurredAt:    r.CreatedAt,
					Amount:        r.Amount,
					SourceType:    "CREDIT_APPLICATION_REVERSAL",
					SourceID:      r.ID,
					InvoiceID:     &pid,
					InvoiceNumber: inv.Number,
					HolderPartyID: &hid,
					ApplicationID: &aid,
					Label:         "Utilisation de crédit annulée",
				})
			}
		}
	}

	// CREDIT_EARNED from ledger (CN-produced CREDIT only — not application restorals).
	var ledger []CreditLedgerEntry
	lq := tx.Where("patient_id=? AND entry_type=? AND source_type=?", f.PatientID, CreditEntryCredit, CreditSourceCreditNote)
	if f.HolderID > 0 {
		lq = lq.Where("holder_party_id=?", f.HolderID)
	}
	if e := lq.Order("id ASC").Find(&ledger).Error; e != nil {
		return nil, e
	}
	for _, e := range ledger {
		hid := e.HolderPartyID
		cnID := e.SourceID
		var invID *uint
		var invNum string
		var cn CreditNote
		if tx.First(&cn, e.SourceID).Error == nil {
			id := cn.InvoiceID
			invID = &id
			if inv, ok := invByID[cn.InvoiceID]; ok {
				invNum = inv.Number
			}
			if f.InvoiceID > 0 && cn.InvoiceID != f.InvoiceID {
				continue
			}
		}
		events = append(events, FinancialHistoryEvent{
			EventType:     FinEventCreditEarned,
			OccurredAt:    e.CreatedAt,
			Amount:        e.Amount,
			SourceType:    "CREDIT_LEDGER",
			SourceID:      e.ID,
			InvoiceID:     invID,
			InvoiceNumber: invNum,
			HolderPartyID: &hid,
			CreditNoteID:  &cnID,
			Label:         "Crédit acquis",
		})
	}

	// Refund workflow + execution events (EXECUTED only when money left / external proof recorded).
	var refunds []Refund
	rq := tx.Where("patient_id=?", f.PatientID)
	if f.HolderID > 0 {
		rq = rq.Where("holder_party_id=?", f.HolderID)
	}
	if e := rq.Order("id ASC").Find(&refunds).Error; e != nil {
		return nil, e
	}
	var execByRefund = map[uint]RefundExecution{}
	{
		var execs []RefundExecution
		if e := tx.Where("refund_id IN (SELECT id FROM billing_refunds WHERE patient_id=?)", f.PatientID).Find(&execs).Error; e != nil {
			return nil, e
		}
		for _, x := range execs {
			execByRefund[x.RefundID] = x
		}
	}
	for _, r := range refunds {
		hid := r.HolderPartyID
		appendRefund := func(eventType, label string, at time.Time, sourceID uint, docNumber string) {
			events = append(events, FinancialHistoryEvent{
				EventType:      eventType,
				OccurredAt:     at,
				Amount:         r.Amount,
				SourceType:     "REFUND",
				SourceID:       sourceID,
				DocumentNumber: docNumber,
				HolderPartyID:  &hid,
				Label:          label,
			})
		}
		appendRefund(FinEventRefundRequested, "Demande de remboursement", r.RequestedAt, r.ID, "")
		if r.ApprovedAt != nil {
			appendRefund(FinEventRefundApproved, "Remboursement autorisé", *r.ApprovedAt, r.ID, "")
		}
		if r.RejectedAt != nil {
			appendRefund(FinEventRefundRejected, "Demande de remboursement rejetée", *r.RejectedAt, r.ID, "")
		}
		if r.CancelledAt != nil {
			appendRefund(FinEventRefundCancelled, "Demande de remboursement annulée", *r.CancelledAt, r.ID, "")
		}
		if x, ok := execByRefund[r.ID]; ok && r.Status == RefundStatusExecuted {
			label := "Remboursement effectué"
			if r.RefundNumber != "" {
				label = "Remboursement effectué " + r.RefundNumber
			}
			appendRefund(FinEventRefundExecuted, label, x.ExecutedAt, x.ID, r.RefundNumber)
		}
	}

	// Filters
	filtered := make([]FinancialHistoryEvent, 0, len(events))
	for _, ev := range events {
		if f.EventType != "" && ev.EventType != strings.ToUpper(strings.TrimSpace(f.EventType)) {
			continue
		}
		if f.DateFrom != "" {
			if ev.OccurredAt.Format("2006-01-02") < f.DateFrom {
				continue
			}
		}
		if f.DateTo != "" {
			if ev.OccurredAt.Format("2006-01-02") > f.DateTo {
				continue
			}
		}
		if f.HolderID > 0 {
			if ev.HolderPartyID == nil || *ev.HolderPartyID != f.HolderID {
				// Invoice-issued without holder still allowed only when no holder filter? Filter requires holder match.
				continue
			}
		}
		ev.SortKey = financialEventSortKey(ev)
		filtered = append(filtered, ev)
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		return filtered[i].SortKey < filtered[j].SortKey
	})
	return filtered, nil
}

func financialEventSortKey(ev FinancialHistoryEvent) string {
	// Lexicographic: time(RFC3339Nano) | eventType | sourceType | sourceId(zero-padded)
	return fmt.Sprintf("%s|%s|%s|%020d",
		ev.OccurredAt.UTC().Format(time.RFC3339Nano),
		ev.EventType,
		ev.SourceType,
		ev.SourceID,
	)
}

// Parse uint helper for handlers.
func parseUintParam(s string) (uint, error) {
	v, err := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	if err != nil || v == 0 {
		return 0, fmt.Errorf("invalid")
	}
	return uint(v), nil
}
