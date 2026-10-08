package billing

import (
	"errors"
	"fmt"
	"strings"
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"gorm.io/gorm"
)

const (
	CodeRefundVoucherNotAvailable = "REFUND_VOUCHER_NOT_AVAILABLE"
	RefundClinicHeader            = "MEDCORE HIS"
)

// RefundVoucher is a READ-ONLY documentary projection of an EXECUTED Refund.
// It is not a monetary authority and never allocates numbers or mutates financial state.
type RefundVoucher struct {
	RefundID     uint   `json:"refundId"`
	RefundNumber string `json:"refundNumber"`
	ClinicHeader string `json:"clinicHeader"`

	Status string `json:"status"`
	Amount int64  `json:"amount"`

	PatientID   uint   `json:"patientId"`
	PatientCode string `json:"patientCode,omitempty"`
	PatientName string `json:"patientName,omitempty"`

	HolderPartyID uint   `json:"holderPartyId"`
	HolderDisplay string `json:"holderDisplay"`
	HolderKind    string `json:"holderKind"`

	BeneficiaryMode         string `json:"beneficiaryMode"`
	BeneficiaryDisplayName  string `json:"beneficiaryDisplayName"`
	BeneficiaryKind         string `json:"beneficiaryKind"`
	BeneficiaryRelationship string `json:"beneficiaryRelationship,omitempty"`
	HolderConsentRef        string `json:"holderConsentRef,omitempty"`

	ReasonCode             string `json:"reasonCode"`
	ReasonComment          string `json:"reasonComment,omitempty"`
	ClinicalAttestationRef string `json:"clinicalAttestationRef,omitempty"`

	Method      string     `json:"method"`
	ExecutedAt  time.Time  `json:"executedAt"`
	ExecutedBy  uint       `json:"executedBy"`
	ApprovedBy  uint       `json:"approvedBy"`
	ApprovedAt  *time.Time `json:"approvedAt,omitempty"`
	RequestedBy uint       `json:"requestedBy"`
	RequestedAt time.Time  `json:"requestedAt"`

	CashSessionID  *uint  `json:"cashSessionId,omitempty"`
	CashRegisterID *uint  `json:"cashRegisterId,omitempty"`
	RegisterCode   string `json:"registerCode,omitempty"`
	RegisterName   string `json:"registerName,omitempty"`

	ExternalReference  string `json:"externalReference,omitempty"`
	EvidenceReference  string `json:"evidenceReference,omitempty"`
	BeneficiaryRailRef string `json:"beneficiaryRailRef,omitempty"` // masked unless IncludeRailRef

	CopyLabels      []string `json:"copyLabels"`
	SignatureZones  []string `json:"signatureZones"`
	DocumentaryNote string   `json:"documentaryNote"`
}

// GetRefundVoucher returns the canonical documentary voucher for an EXECUTED refund.
// Calling this never allocates an RMB number or mutates financial state.
func (s *Service) GetRefundVoucher(id uint, includeRailRef bool) (*RefundVoucher, error) {
	row, e := s.GetRefund(id)
	if e != nil {
		return nil, e
	}
	if row.Status != RefundStatusExecuted || strings.TrimSpace(row.RefundNumber) == "" {
		return nil, refundConflict(CodeRefundVoucherNotAvailable,
			"Bon de remboursement disponible uniquement après exécution")
	}
	var exec RefundExecution
	if e := s.db.Where("refund_id=?", id).First(&exec).Error; e != nil {
		if errors.Is(e, gorm.ErrRecordNotFound) {
			return nil, coreerrors.Internal("Exécution introuvable pour un remboursement EXECUTED")
		}
		return nil, e
	}

	var party FinancialParty
	_ = s.db.First(&party, row.HolderPartyID)

	patientCode, patientName := "", ""
	type patientRow struct {
		CodePatient string
		Nom         string
		Prenoms     string
	}
	var p patientRow
	if e := s.db.Table("patients").Select("code_patient, nom, prenoms").Where("id=?", row.PatientID).Scan(&p).Error; e == nil {
		patientCode = p.CodePatient
		patientName = strings.TrimSpace(p.Prenoms + " " + p.Nom)
	}

	regCode, regName := "", ""
	if exec.CashRegisterID != nil && *exec.CashRegisterID > 0 {
		type regRow struct {
			Code string
			Name string
		}
		var r regRow
		if e := s.db.Table("cash_registers").Select("code, name").Where("id=?", *exec.CashRegisterID).Scan(&r).Error; e == nil {
			regCode, regName = r.Code, r.Name
		}
	}

	approvedBy := uint(0)
	if row.ApprovedBy != nil {
		approvedBy = *row.ApprovedBy
	}

	rail := exec.BeneficiaryRailRef
	if !includeRailRef {
		rail = maskRailRef(rail)
	}

	return &RefundVoucher{
		RefundID:                row.ID,
		RefundNumber:            row.RefundNumber,
		ClinicHeader:            RefundClinicHeader,
		Status:                  row.Status,
		Amount:                  row.Amount,
		PatientID:               row.PatientID,
		PatientCode:             patientCode,
		PatientName:             patientName,
		HolderPartyID:           row.HolderPartyID,
		HolderDisplay:           party.DisplayName,
		HolderKind:              party.Kind,
		BeneficiaryMode:         row.BeneficiaryMode,
		BeneficiaryDisplayName:  row.BeneficiaryDisplayName,
		BeneficiaryKind:         row.BeneficiaryKind,
		BeneficiaryRelationship: row.BeneficiaryRelationship,
		HolderConsentRef:        row.HolderConsentRef,
		ReasonCode:              row.ReasonCode,
		ReasonComment:           row.ReasonComment,
		ClinicalAttestationRef:  row.ClinicalAttestationRef,
		Method:                  exec.Method,
		ExecutedAt:              exec.ExecutedAt,
		ExecutedBy:              exec.ExecutedBy,
		ApprovedBy:              approvedBy,
		ApprovedAt:              row.ApprovedAt,
		RequestedBy:             row.RequestedBy,
		RequestedAt:             row.RequestedAt,
		CashSessionID:           exec.CashSessionID,
		CashRegisterID:          exec.CashRegisterID,
		RegisterCode:            regCode,
		RegisterName:            regName,
		ExternalReference:       exec.ExternalReference,
		EvidenceReference:       exec.EvidenceReference,
		BeneficiaryRailRef:      rail,
		CopyLabels: []string{
			"EXEMPLAIRE BÉNÉFICIAIRE",
			"EXEMPLAIRE CAISSE / CLINIQUE",
		},
		SignatureZones: []string{
			"Signature du bénéficiaire",
			"Signature du caissier / exécutant",
		},
		DocumentaryNote: fmt.Sprintf(
			"Document justificatif du remboursement %s. Ne constitue pas une autorité monétaire distincte.",
			row.RefundNumber,
		),
	}, nil
}

func maskRailRef(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if len(s) <= 4 {
		return "****"
	}
	return strings.Repeat("*", len(s)-4) + s[len(s)-4:]
}
