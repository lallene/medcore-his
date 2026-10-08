package billing

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/modules/medical_records"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Refund lifecycle (LOT29F-I-A). REQUESTED/APPROVED reserve spendable credit; no money leaves the clinic.
const (
	RefundStatusRequested = "REQUESTED"
	RefundStatusApproved  = "APPROVED"
	RefundStatusRejected  = "REJECTED"
	RefundStatusCancelled = "CANCELLED"
	// Future I-B: RefundStatusExecuted = "EXECUTED"
)

const (
	RefundReasonDuplicateOrOverpayment      = "DUPLICATE_OR_OVERPAYMENT"
	RefundReasonServiceCancelledOrNotPerf   = "SERVICE_CANCELLED_OR_NOT_PERFORMED"
	RefundReasonInvoiceCorrection           = "INVOICE_CORRECTION"
	RefundReasonUnusedAdvance               = "UNUSED_ADVANCE"
	RefundReasonInsuranceAfterPayment       = "INSURANCE_COVERAGE_AFTER_PAYMENT"
	RefundReasonTransferOrDeathBeforeService = "TRANSFER_OR_DEATH_BEFORE_SERVICE"
	RefundReasonOther                       = "OTHER"
)

const (
	RefundBeneficiaryHolder    = "HOLDER"
	RefundBeneficiaryAlternate = "ALTERNATE"
)

const (
	RefundMethodUnspecified = "UNSPECIFIED"
	RefundMethodCash        = "CASH"
	RefundMethodCard        = "CARD"
	RefundMethodMobileMoney = "MOBILE_MONEY"
	RefundMethodTransfer    = "TRANSFER"
)

const (
	CodeRefundInsufficientSpendable = "REFUND_INSUFFICIENT_SPENDABLE"
	CodeRefundInvalidStatus         = "REFUND_INVALID_STATUS"
	CodeRefundSoDViolation          = "REFUND_SOD_VIOLATION"
	CodeRefundAttestationRequired   = "REFUND_ATTESTATION_REQUIRED"
	CodeRefundConsentRequired       = "REFUND_CONSENT_REQUIRED"
	CodeRefundOtherRequiresManager  = "REFUND_OTHER_REQUIRES_MANAGER"
	CodeRefundOrgCashForbidden      = "REFUND_ORG_CASH_FORBIDDEN"
)

// Refund is the genuine restitution workflow aggregate (request → decision). I-A never executes money.
type Refund struct {
	ID     uint `gorm:"primaryKey" json:"id"`
	PatientID     uint `gorm:"not null;index" json:"patientId"`
	HolderPartyID uint `gorm:"not null;index;index:idx_refund_holder_patient,priority:1" json:"holderPartyId"`

	Amount int64  `gorm:"not null;check:billing_refund_amount_positive,amount > 0" json:"amount"`
	Status string `gorm:"size:20;not null;index" json:"status"`

	ReasonCode    string `gorm:"size:60;not null;index" json:"reasonCode"`
	ReasonComment string `gorm:"size:1000" json:"reasonComment,omitempty"`

	BeneficiaryMode         string `gorm:"size:20;not null" json:"beneficiaryMode"`
	BeneficiaryDisplayName  string `gorm:"size:200;not null" json:"beneficiaryDisplayName"`
	BeneficiaryKind         string `gorm:"size:30;not null" json:"beneficiaryKind"`
	BeneficiaryPhone        string `gorm:"size:50" json:"beneficiaryPhone,omitempty"`
	BeneficiaryRelationship string `gorm:"size:80" json:"beneficiaryRelationship,omitempty"`
	HolderConsentRef        string `gorm:"size:200" json:"holderConsentRef,omitempty"`

	IntendedMethod        string `gorm:"size:30;not null" json:"intendedMethod"`
	MethodOverrideReason  string `gorm:"size:500" json:"methodOverrideReason,omitempty"`
	ClinicalAttestationRef string `gorm:"size:200" json:"clinicalAttestationRef,omitempty"`

	RequestedBy uint      `gorm:"not null;index" json:"requestedBy"`
	RequestedAt time.Time `gorm:"not null;index" json:"requestedAt"`

	ApprovedBy *uint      `json:"approvedBy,omitempty"`
	ApprovedAt *time.Time `json:"approvedAt,omitempty"`

	RejectedBy       *uint      `json:"rejectedBy,omitempty"`
	RejectedAt       *time.Time `json:"rejectedAt,omitempty"`
	RejectionReason  string     `gorm:"size:1000" json:"rejectionReason,omitempty"`

	CancelledBy         *uint      `json:"cancelledBy,omitempty"`
	CancelledAt         *time.Time `json:"cancelledAt,omitempty"`
	CancellationReason  string     `gorm:"size:1000" json:"cancellationReason,omitempty"`

	IdempotencyKey string `gorm:"size:120;not null;uniqueIndex" json:"idempotencyKey"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

func (Refund) TableName() string { return "billing_refunds" }

func refundConflict(code, msg string) error {
	return coreerrors.New(409, code, msg, nil)
}

func IsActiveRefundReservationStatus(status string) bool {
	return status == RefundStatusRequested || status == RefundStatusApproved
}

// ReservedForRefund sums active REQUESTED+APPROVED refund amounts for holder+patient.
func ReservedForRefund(tx *gorm.DB, holderPartyID, patientID uint) (int64, error) {
	var total int64
	e := tx.Model(&Refund{}).
		Select("COALESCE(SUM(amount),0)").
		Where("holder_party_id=? AND patient_id=? AND status IN ?", holderPartyID, patientID,
			[]string{RefundStatusRequested, RefundStatusApproved}).
		Scan(&total).Error
	return total, e
}

// SpendableCredit = ledgerAvailable − reservedForRefund (never negative).
func SpendableCredit(tx *gorm.DB, holderPartyID, patientID uint) (int64, error) {
	sum, err := CreditSummaryFor(tx, holderPartyID, patientID)
	if err != nil {
		return 0, err
	}
	return sum.SpendableCredit, nil
}

func NormalizeRefundReasonCode(code string) (string, error) {
	c := strings.TrimSpace(strings.ToUpper(code))
	switch c {
	case RefundReasonDuplicateOrOverpayment,
		RefundReasonServiceCancelledOrNotPerf,
		RefundReasonInvoiceCorrection,
		RefundReasonUnusedAdvance,
		RefundReasonInsuranceAfterPayment,
		RefundReasonTransferOrDeathBeforeService,
		RefundReasonOther:
		return c, nil
	default:
		return "", coreerrors.BadRequest("Code motif de remboursement invalide")
	}
}

func NormalizeRefundMethod(method string) (string, error) {
	m := strings.TrimSpace(strings.ToUpper(method))
	if m == "" {
		return RefundMethodUnspecified, nil
	}
	switch m {
	case RefundMethodUnspecified, RefundMethodCash, RefundMethodCard, RefundMethodMobileMoney, RefundMethodTransfer:
		return m, nil
	default:
		return "", coreerrors.BadRequest("Mode de remboursement prévu invalide")
	}
}

// RefundRequest is the client command for creating a refund request (no status/actor fields).
type RefundRequest struct {
	PatientID              uint   `json:"patientId"`
	HolderPartyID          uint   `json:"holderPartyId"`
	Amount                 int64  `json:"amount"`
	ReasonCode             string `json:"reasonCode"`
	ReasonComment          string `json:"reasonComment"`
	BeneficiaryMode        string `json:"beneficiaryMode"`
	BeneficiaryDisplayName string `json:"beneficiaryDisplayName"`
	BeneficiaryRelationship string `json:"beneficiaryRelationship"`
	HolderConsentRef       string `json:"holderConsentRef"`
	IntendedMethod         string `json:"intendedMethod"`
	MethodOverrideReason   string `json:"methodOverrideReason"`
	ClinicalAttestationRef string `json:"clinicalAttestationRef"`
	IdempotencyKey         string `json:"idempotencyKey"`
}

type RefundDecisionRequest struct {
	Reason         string `json:"reason"`
	IdempotencyKey string `json:"idempotencyKey"`
	// ManagerialApproval acknowledges OTHER requires management validation (I-A structural flag).
	ManagerialApproval bool `json:"managerialApproval"`
}

type RefundListFilter struct {
	Page          int
	Limit         int
	Status        string
	PatientID     uint
	HolderPartyID uint
	ReasonCode    string
	DateFrom      string
	DateTo        string
	IncludePII    bool
}

type RefundPage struct {
	Data       []Refund `json:"data"`
	Page       int      `json:"page"`
	Limit      int      `json:"limit"`
	Total      int64    `json:"total"`
	TotalPages int      `json:"totalPages"`
}

func (s *Service) RequestRefund(req RefundRequest, user uint) (*Refund, error) {
	if user == 0 {
		return nil, coreerrors.Unauthorized("Utilisateur non authentifié")
	}
	if req.PatientID == 0 || req.HolderPartyID == 0 {
		return nil, coreerrors.BadRequest("Patient et titulaire financier obligatoires")
	}
	if req.Amount <= 0 {
		return nil, coreerrors.BadRequest("Montant de remboursement obligatoire et strictement positif")
	}
	reason, err := NormalizeRefundReasonCode(req.ReasonCode)
	if err != nil {
		return nil, err
	}
	comment := strings.TrimSpace(req.ReasonComment)
	if reason == RefundReasonOther && comment == "" {
		return nil, coreerrors.BadRequest("Commentaire obligatoire pour le motif OTHER")
	}
	if len(comment) > 1000 {
		return nil, coreerrors.BadRequest("Commentaire trop long")
	}
	method, err := NormalizeRefundMethod(req.IntendedMethod)
	if err != nil {
		return nil, err
	}
	override := strings.TrimSpace(req.MethodOverrideReason)
	key, err := NormalizePaymentIdempotencyKey(req.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	attest := strings.TrimSpace(req.ClinicalAttestationRef)
	if reason == RefundReasonServiceCancelledOrNotPerf && attest == "" {
		// No authoritative clinical non-performance subsystem yet — fail closed without attestation ref.
		return nil, refundConflict(CodeRefundAttestationRequired,
			"Attestation clinique de non-réalisation requise pour ce motif")
	}

	benMode := strings.TrimSpace(strings.ToUpper(req.BeneficiaryMode))
	if benMode == "" {
		benMode = RefundBeneficiaryHolder
	}
	if benMode != RefundBeneficiaryHolder && benMode != RefundBeneficiaryAlternate {
		return nil, coreerrors.BadRequest("Mode bénéficiaire invalide")
	}

	var out *Refund
	e := s.db.Transaction(func(tx *gorm.DB) error {
		var prior Refund
		if e := tx.Where("idempotency_key=?", key).First(&prior).Error; e == nil {
			if prior.PatientID != req.PatientID || prior.HolderPartyID != req.HolderPartyID || prior.Amount != req.Amount || prior.ReasonCode != reason {
				return coreerrors.New(409, "IDEMPOTENCY_CONFLICT", "Clé d'idempotence déjà utilisée avec un autre payload", nil)
			}
			out = &prior
			return nil
		} else if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}

		if e := lockCreditAccount(tx, req.HolderPartyID, req.PatientID); e != nil {
			return e
		}

		var party FinancialParty
		if e := tx.First(&party, req.HolderPartyID).Error; e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return coreerrors.NotFound("FINANCIAL_PARTY")
			}
			return e
		}
		// Holder must be scoped to patient when PatientID is set on party; ORGANIZATION may be shared.
		if party.PatientID != nil && *party.PatientID != req.PatientID {
			return coreerrors.BadRequest("Titulaire non rattaché à ce patient")
		}

		if party.Kind == PartyKindOrganization && method == RefundMethodCash {
			return refundConflict(CodeRefundOrgCashForbidden,
				"Remboursement organisation : le mode espèces n'est pas autorisé")
		}

		spendable, e := SpendableCredit(tx, req.HolderPartyID, req.PatientID)
		if e != nil {
			return e
		}
		if req.Amount > spendable {
			return refundConflict(CodeRefundInsufficientSpendable, "Crédit utilisable insuffisant pour cette demande")
		}

		benName := strings.TrimSpace(party.DisplayName)
		benKind := party.Kind
		benPhone := party.Phone
		benRel := ""
		consent := ""
		if benMode == RefundBeneficiaryAlternate {
			consent = strings.TrimSpace(req.HolderConsentRef)
			if consent == "" {
				return refundConflict(CodeRefundConsentRequired,
					"Consentement écrit du payeur requis pour un bénéficiaire alternatif")
			}
			benName = strings.TrimSpace(req.BeneficiaryDisplayName)
			if benName == "" {
				return coreerrors.BadRequest("Identité du bénéficiaire alternatif obligatoire")
			}
			benRel = strings.TrimSpace(req.BeneficiaryRelationship)
			if benRel == "" {
				return coreerrors.BadRequest("Lien du bénéficiaire alternatif obligatoire")
			}
			benKind = "ALTERNATE"
			benPhone = ""
		}

		now := time.Now().UTC()
		row := Refund{
			PatientID:               req.PatientID,
			HolderPartyID:           req.HolderPartyID,
			Amount:                  req.Amount,
			Status:                  RefundStatusRequested,
			ReasonCode:              reason,
			ReasonComment:           comment,
			BeneficiaryMode:         benMode,
			BeneficiaryDisplayName:  benName,
			BeneficiaryKind:         benKind,
			BeneficiaryPhone:        benPhone,
			BeneficiaryRelationship: benRel,
			HolderConsentRef:        consent,
			IntendedMethod:          method,
			MethodOverrideReason:    override,
			ClinicalAttestationRef:  attest,
			RequestedBy:             user,
			RequestedAt:             now,
			IdempotencyKey:          key,
			CreatedAt:               now,
			UpdatedAt:               now,
		}
		if e := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; e != nil {
			return e
		}
		if row.ID == 0 {
			var again Refund
			if e := tx.Where("idempotency_key=?", key).First(&again).Error; e != nil {
				return e
			}
			out = &again
			return nil
		}
		if e := s.timelineRefund(tx, &row, "refund_requested", "Demande de remboursement", user); e != nil {
			return e
		}
		out = &row
		return nil
	})
	if e != nil {
		return nil, e
	}
	return out, nil
}

func (s *Service) ApproveRefund(id uint, req RefundDecisionRequest, user uint) (*Refund, error) {
	if user == 0 {
		return nil, coreerrors.Unauthorized("Utilisateur non authentifié")
	}
	var out *Refund
	e := s.db.Transaction(func(tx *gorm.DB) error {
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
		if row.Status != RefundStatusRequested {
			return refundConflict(CodeRefundInvalidStatus, "Seule une demande REQUESTED peut être autorisée")
		}
		if row.RequestedBy == user {
			return refundConflict(CodeRefundSoDViolation, "Le demandeur ne peut pas autoriser sa propre demande")
		}
		if row.ReasonCode == RefundReasonOther && !req.ManagerialApproval {
			return refundConflict(CodeRefundOtherRequiresManager,
				"Le motif OTHER exige une validation managériale explicite")
		}
		// Re-check spendable excluding this row's own reservation (still REQUESTED).
		spendable, e := SpendableCredit(tx, row.HolderPartyID, row.PatientID)
		if e != nil {
			return e
		}
		// Reservation already includes this amount; spendable may be 0 while this request is valid.
		_ = spendable
		reserved, e := ReservedForRefund(tx, row.HolderPartyID, row.PatientID)
		if e != nil {
			return e
		}
		ledger, e := ledgerAvailableOnly(tx, row.HolderPartyID, row.PatientID)
		if e != nil {
			return e
		}
		if reserved > ledger {
			return refundConflict(CodeRefundInsufficientSpendable, "Réservation de remboursement incohérente avec le crédit disponible")
		}

		now := time.Now().UTC()
		row.Status = RefundStatusApproved
		row.ApprovedBy = &user
		row.ApprovedAt = &now
		row.UpdatedAt = now
		if e := tx.Save(&row).Error; e != nil {
			return e
		}
		if e := s.timelineRefund(tx, &row, "refund_approved", "Remboursement autorisé", user); e != nil {
			return e
		}
		out = &row
		return nil
	})
	if e != nil {
		return nil, e
	}
	return out, nil
}

func (s *Service) RejectRefund(id uint, req RefundDecisionRequest, user uint) (*Refund, error) {
	if user == 0 {
		return nil, coreerrors.Unauthorized("Utilisateur non authentifié")
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		return nil, coreerrors.BadRequest("Motif de rejet obligatoire")
	}
	if len(reason) > 1000 {
		return nil, coreerrors.BadRequest("Motif de rejet trop long")
	}
	var out *Refund
	e := s.db.Transaction(func(tx *gorm.DB) error {
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
		if row.Status != RefundStatusRequested {
			return refundConflict(CodeRefundInvalidStatus, "Seule une demande REQUESTED peut être rejetée")
		}
		if row.RequestedBy == user {
			return refundConflict(CodeRefundSoDViolation, "Le demandeur ne peut pas rejeter sa propre demande")
		}
		now := time.Now().UTC()
		row.Status = RefundStatusRejected
		row.RejectedBy = &user
		row.RejectedAt = &now
		row.RejectionReason = reason
		row.UpdatedAt = now
		if e := tx.Save(&row).Error; e != nil {
			return e
		}
		if e := s.timelineRefund(tx, &row, "refund_rejected", "Demande de remboursement rejetée", user); e != nil {
			return e
		}
		out = &row
		return nil
	})
	if e != nil {
		return nil, e
	}
	return out, nil
}

func (s *Service) CancelRefund(id uint, req RefundDecisionRequest, user uint) (*Refund, error) {
	if user == 0 {
		return nil, coreerrors.Unauthorized("Utilisateur non authentifié")
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		return nil, coreerrors.BadRequest("Motif d'annulation obligatoire")
	}
	if len(reason) > 1000 {
		return nil, coreerrors.BadRequest("Motif d'annulation trop long")
	}
	var out *Refund
	e := s.db.Transaction(func(tx *gorm.DB) error {
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
		switch row.Status {
		case RefundStatusRequested:
			// Requester or financial decision-maker may cancel (permission gated at handler).
		case RefundStatusApproved:
			// Pre-execution cancellation of authorization.
		default:
			return refundConflict(CodeRefundInvalidStatus, "Cette demande ne peut plus être annulée")
		}
		now := time.Now().UTC()
		row.Status = RefundStatusCancelled
		row.CancelledBy = &user
		row.CancelledAt = &now
		row.CancellationReason = reason
		row.UpdatedAt = now
		if e := tx.Save(&row).Error; e != nil {
			return e
		}
		if e := s.timelineRefund(tx, &row, "refund_cancelled", "Demande de remboursement annulée", user); e != nil {
			return e
		}
		out = &row
		return nil
	})
	if e != nil {
		return nil, e
	}
	return out, nil
}

func (s *Service) GetRefund(id uint) (*Refund, error) {
	var row Refund
	if e := s.db.First(&row, id).Error; e != nil {
		if errors.Is(e, gorm.ErrRecordNotFound) {
			return nil, coreerrors.NotFound("REFUND")
		}
		return nil, e
	}
	return &row, nil
}

func (s *Service) ListRefunds(f RefundListFilter) (*RefundPage, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.Limit < 1 || f.Limit > 100 {
		f.Limit = 20
	}
	q := s.db.Model(&Refund{})
	if f.Status != "" {
		q = q.Where("status=?", strings.TrimSpace(strings.ToUpper(f.Status)))
	}
	if f.PatientID > 0 {
		q = q.Where("patient_id=?", f.PatientID)
	}
	if f.HolderPartyID > 0 {
		q = q.Where("holder_party_id=?", f.HolderPartyID)
	}
	if f.ReasonCode != "" {
		q = q.Where("reason_code=?", strings.TrimSpace(strings.ToUpper(f.ReasonCode)))
	}
	if f.DateFrom != "" {
		if t, e := time.Parse("2006-01-02", f.DateFrom); e == nil {
			q = q.Where("requested_at >= ?", t.UTC())
		}
	}
	if f.DateTo != "" {
		if t, e := time.Parse("2006-01-02", f.DateTo); e == nil {
			q = q.Where("requested_at < ?", t.UTC().Add(24*time.Hour))
		}
	}
	var total int64
	if e := q.Count(&total).Error; e != nil {
		return nil, e
	}
	var rows []Refund
	if e := q.Order("requested_at DESC, id DESC").
		Offset((f.Page - 1) * f.Limit).Limit(f.Limit).
		Find(&rows).Error; e != nil {
		return nil, e
	}
	if !f.IncludePII {
		for i := range rows {
			rows[i].BeneficiaryPhone = ""
		}
	}
	tp := int(math.Ceil(float64(total) / float64(f.Limit)))
	if tp == 0 {
		tp = 1
	}
	return &RefundPage{Data: rows, Page: f.Page, Limit: f.Limit, Total: total, TotalPages: tp}, nil
}

func ledgerAvailableOnly(tx *gorm.DB, holderPartyID, patientID uint) (int64, error) {
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
		return 0, e
	}
	var credited, applied, refunded int64
	for _, r := range rows {
		switch r.EntryType {
		case CreditEntryCredit:
			credited = r.Total
		case CreditEntryApply:
			applied = r.Total
		case CreditEntryRefund:
			refunded = r.Total
		}
	}
	v := credited - applied - refunded
	if v < 0 {
		return 0, nil
	}
	return v, nil
}

func (s *Service) timelineRefund(tx *gorm.DB, r *Refund, eventType, title string, user uint) error {
	var rec medical_records.MedicalRecord
	if e := tx.Where("patient_id=?", r.PatientID).Order("id ASC").First(&rec).Error; e != nil {
		if errors.Is(e, gorm.ErrRecordNotFound) {
			return nil
		}
		return e
	}
	ref := r.ID
	desc := fmt.Sprintf("refund:%d amount:%d status:%s reason:%s holder:%d",
		r.ID, r.Amount, r.Status, r.ReasonCode, r.HolderPartyID)
	return tx.Create(&medical_records.MedicalTimelineEvent{
		MedicalRecordID: rec.ID,
		PatientID:       r.PatientID,
		EventType:       eventType,
		Category:        "billing",
		Title:           title,
		Description:     desc,
		ReferenceType:   "billing_refund",
		ReferenceID:     &ref,
		Severity:        "info",
		EventDate:       time.Now().UTC(),
		CreatedBy:       user,
	}).Error
}
