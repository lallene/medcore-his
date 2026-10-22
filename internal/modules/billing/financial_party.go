package billing

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"gorm.io/gorm"
)

const (
	PartyKindIndividual   = "INDIVIDUAL"
	PartyKindOrganization = "ORGANIZATION"

	PayerModePatient      = "PATIENT"
	PayerModeIndividual   = "INDIVIDUAL"
	PayerModeOrganization = "ORGANIZATION"

	PayerProvenanceCaptured          = "CAPTURED"
	PayerProvenanceLegacyUnconfirmed = "LEGACY_UNCONFIRMED"

	MaxPayerNameLen         = 250
	MaxPayerPhoneLen        = 50
	MaxPayerRelationshipLen = 80
)

// FinancialParty is the durable financial holder identity (LOT29F-H-B).
// Not a Patient, not a cashier, not an insurance company, not a CRM.
type FinancialParty struct {
	ID          uint   `gorm:"primaryKey" json:"id"`
	Kind        string `gorm:"size:20;not null;index;check:billing_financial_party_kind_valid,kind IN ('INDIVIDUAL','ORGANIZATION')" json:"kind"`
	DisplayName string `gorm:"size:250;not null" json:"displayName"`
	Phone       string `gorm:"size:50" json:"phone,omitempty"`
	// PatientID set when this party represents that patient as financial holder.
	PatientID *uint     `gorm:"uniqueIndex" json:"patientId,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (FinancialParty) TableName() string { return "billing_financial_parties" }

// PayerRequest is the command payload for explicit payer authority on NEW payments.
type PayerRequest struct {
	Mode         string `json:"mode"` // PATIENT | INDIVIDUAL | ORGANIZATION
	PartyID      *uint  `json:"partyId,omitempty"`
	DisplayName  string `json:"displayName,omitempty"`
	Phone        string `json:"phone,omitempty"`
	Relationship string `json:"relationship,omitempty"`
}

// ResolvedPayerSnapshot is the immutable payment-time payer authority to persist.
type ResolvedPayerSnapshot struct {
	PartyID      uint
	Kind         string
	DisplayName  string
	Phone        string
	Relationship string
	IsPatient    bool
	Provenance   string
	Fingerprint  string
}

func NormalizePayerPhone(raw string) (string, error) {
	phone := strings.TrimSpace(raw)
	if phone == "" {
		return "", nil
	}
	if utf8.RuneCountInString(phone) < 8 || utf8.RuneCountInString(phone) > MaxPayerPhoneLen {
		return "", coreerrors.BadRequest("Téléphone du payeur invalide")
	}
	return phone, nil
}

func NormalizePayerName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", coreerrors.BadRequest("Nom du payeur obligatoire")
	}
	if utf8.RuneCountInString(name) > MaxPayerNameLen {
		return "", coreerrors.BadRequest("Nom du payeur trop long")
	}
	return name, nil
}

func NormalizePayerRelationship(raw string) (string, error) {
	rel := strings.TrimSpace(raw)
	if utf8.RuneCountInString(rel) > MaxPayerRelationshipLen {
		return "", coreerrors.BadRequest("Lien avec le patient trop long")
	}
	return rel, nil
}

// payerFingerprint excludes PartyID so idempotent retries without partyId still match.
func payerFingerprint(s ResolvedPayerSnapshot) string {
	return strings.Join([]string{
		s.Provenance,
		s.Kind,
		strings.ToUpper(s.DisplayName),
		s.Phone,
		s.Relationship,
		boolStr(s.IsPatient),
	}, "|")
}

func boolStr(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

// Payer authority conflict codes (LOT30-A′).
const (
	CodePayerPartyMismatch         = "PAYER_PARTY_MISMATCH"
	CodePayerPartyPatientMismatch  = "PAYER_PARTY_PATIENT_MISMATCH"
	CodePayerPartyRelationRequired = "PAYER_PARTY_RELATION_REQUIRED"
)

func payerConflict(code, message string) error {
	return coreerrors.New(409, code, message, nil)
}

// validatedPayer is the server-validated payer identity before persistence.
// existing != nil → snapshot comes from the FinancialParty master (server authority);
// existing == nil → a new third-party FinancialParty will be created from client values.
type validatedPayer struct {
	mode     string
	existing *FinancialParty
	snap     ResolvedPayerSnapshot
}

// validatePayer applies payer authority rules without creating rows (LOT29F-H-B, LOT30-A′):
//   - PATIENT: identity from the invoice patient; a supplied partyId must be that patient's party.
//   - INDIVIDUAL / ORGANIZATION with partyId: party must exist with the same kind; display name /
//     phone come from the master and conflicting client values are rejected (never rewritten).
//     An INDIVIDUAL party must already be related to this patient (prior captured payment or
//     credit for the same patient); an ORGANIZATION may pay for several patients.
//   - Without partyId: client identity creates a new party (no fuzzy merge on name/phone).
func validatePayer(tx *gorm.DB, patientID uint, req *PayerRequest) (*validatedPayer, error) {
	if req == nil {
		return nil, coreerrors.BadRequest("Payeur obligatoire pour un nouvel encaissement")
	}
	mode := strings.ToUpper(strings.TrimSpace(req.Mode))
	partyID := uint(0)
	if req.PartyID != nil {
		partyID = *req.PartyID
	}
	switch mode {
	case PayerModePatient:
		return validatePatientPayer(tx, patientID, partyID)
	case PayerModeIndividual:
		rel, err := NormalizePayerRelationship(req.Relationship)
		if err != nil {
			return nil, err
		}
		if rel == "" {
			return nil, coreerrors.BadRequest("Lien avec le patient obligatoire")
		}
		out := &validatedPayer{mode: mode, snap: ResolvedPayerSnapshot{
			Kind: PartyKindIndividual, Relationship: rel, Provenance: PayerProvenanceCaptured,
		}}
		if partyID > 0 {
			party, err := loadExistingThirdParty(tx, partyID, PartyKindIndividual, patientID, req)
			if err != nil {
				return nil, err
			}
			out.existing = party
			out.snap.DisplayName, out.snap.Phone = party.DisplayName, party.Phone
			return out, nil
		}
		name, err := NormalizePayerName(req.DisplayName)
		if err != nil {
			return nil, err
		}
		phone, err := NormalizePayerPhone(req.Phone)
		if err != nil {
			return nil, err
		}
		if phone == "" {
			return nil, coreerrors.BadRequest("Téléphone du payeur obligatoire")
		}
		out.snap.DisplayName, out.snap.Phone = name, phone
		return out, nil
	case PayerModeOrganization:
		out := &validatedPayer{mode: mode, snap: ResolvedPayerSnapshot{
			Kind: PartyKindOrganization, Provenance: PayerProvenanceCaptured,
		}}
		if partyID > 0 {
			party, err := loadExistingThirdParty(tx, partyID, PartyKindOrganization, patientID, req)
			if err != nil {
				return nil, err
			}
			out.existing = party
			out.snap.DisplayName, out.snap.Phone = party.DisplayName, party.Phone
			return out, nil
		}
		name, err := NormalizePayerName(req.DisplayName)
		if err != nil {
			return nil, err
		}
		phone, err := NormalizePayerPhone(req.Phone)
		if err != nil {
			return nil, err
		}
		out.snap.DisplayName, out.snap.Phone = name, phone
		return out, nil
	default:
		return nil, coreerrors.BadRequest("Type de payeur non supporté")
	}
}

func validatePatientPayer(tx *gorm.DB, patientID, partyID uint) (*validatedPayer, error) {
	var patient struct {
		ID                      uint
		Nom, Prenoms, Telephone string
	}
	if e := tx.Table("patients").Select("id,nom,prenoms,telephone").Where("id=?", patientID).Scan(&patient).Error; e != nil {
		return nil, e
	}
	if patient.ID == 0 {
		return nil, coreerrors.NotFound("PATIENT")
	}
	if partyID > 0 {
		var party FinancialParty
		if e := tx.First(&party, partyID).Error; e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return nil, coreerrors.NotFound("FINANCIAL_PARTY")
			}
			return nil, e
		}
		if party.PatientID == nil || *party.PatientID != patientID {
			return nil, payerConflict(CodePayerPartyPatientMismatch, "Ce titulaire financier ne représente pas le patient de la facture")
		}
	}
	name := strings.TrimSpace(patient.Prenoms + " " + patient.Nom)
	if name == "" {
		name = strings.TrimSpace(patient.Nom)
	}
	if name == "" {
		return nil, coreerrors.Conflict("Identité patient insuffisante pour le payeur")
	}
	phone, err := NormalizePayerPhone(patient.Telephone)
	if err != nil {
		return nil, err
	}
	return &validatedPayer{mode: PayerModePatient, snap: ResolvedPayerSnapshot{
		Kind: PartyKindIndividual, DisplayName: name, Phone: phone,
		IsPatient: true, Provenance: PayerProvenanceCaptured,
	}}, nil
}

// loadExistingThirdParty loads a referenced INDIVIDUAL / ORGANIZATION party and enforces
// kind, patient-representation, client-value consistency and (INDIVIDUAL) patient relation.
func loadExistingThirdParty(tx *gorm.DB, partyID uint, kind string, patientID uint, req *PayerRequest) (*FinancialParty, error) {
	var party FinancialParty
	if e := tx.First(&party, partyID).Error; e != nil {
		if errors.Is(e, gorm.ErrRecordNotFound) {
			return nil, coreerrors.NotFound("FINANCIAL_PARTY")
		}
		return nil, e
	}
	if party.Kind != kind {
		if kind == PartyKindOrganization {
			return nil, coreerrors.Conflict("Le payeur référencé n'est pas une organisation")
		}
		return nil, coreerrors.Conflict("Type de payeur incohérent avec la fiche référencée")
	}
	if party.PatientID != nil {
		return nil, coreerrors.Conflict("Ce payeur représente déjà le patient — utilisez le mode patient")
	}
	if name := strings.TrimSpace(req.DisplayName); name != "" && !strings.EqualFold(name, strings.TrimSpace(party.DisplayName)) {
		return nil, payerConflict(CodePayerPartyMismatch, "Le nom saisi ne correspond pas au titulaire financier référencé")
	}
	if phone := strings.TrimSpace(req.Phone); phone != "" && phone != strings.TrimSpace(party.Phone) {
		return nil, payerConflict(CodePayerPartyMismatch, "Le téléphone saisi ne correspond pas au titulaire financier référencé")
	}
	if kind == PartyKindIndividual {
		related, err := partyRelatedToPatient(tx, party.ID, patientID)
		if err != nil {
			return nil, err
		}
		if !related {
			return nil, payerConflict(CodePayerPartyRelationRequired, "Ce payeur n'a aucune relation financière avec ce patient")
		}
	}
	return &party, nil
}

// partyRelatedToPatient: a captured payment for this patient's invoices, or credit held for this patient.
func partyRelatedToPatient(tx *gorm.DB, partyID, patientID uint) (bool, error) {
	var n int64
	if e := tx.Raw(`
		SELECT COUNT(*) FROM billing_payments p
		JOIN billing_invoices i ON i.id = p.invoice_id
		WHERE p.payer_party_id = ? AND p.payer_provenance = ? AND i.patient_id = ?
	`, partyID, PayerProvenanceCaptured, patientID).Scan(&n).Error; e != nil {
		return false, e
	}
	if n > 0 {
		return true, nil
	}
	if e := tx.Model(&CreditLedgerEntry{}).Where("holder_party_id=? AND patient_id=?", partyID, patientID).Count(&n).Error; e != nil {
		return false, e
	}
	return n > 0, nil
}

// PreviewPayerFingerprint validates payer payload and returns fingerprint without creating parties.
func PreviewPayerFingerprint(tx *gorm.DB, patientID uint, req *PayerRequest) (string, error) {
	v, err := validatePayer(tx, patientID, req)
	if err != nil {
		return "", err
	}
	return payerFingerprint(v.snap), nil
}

// ResolvePayerForPayment builds/loads FinancialParty and immutable snapshot for a NEW payment.
// patientID is the invoice patient (clinical subject); never inferred as payer without Mode=PATIENT.
func ResolvePayerForPayment(tx *gorm.DB, patientID uint, req *PayerRequest) (*ResolvedPayerSnapshot, error) {
	v, err := validatePayer(tx, patientID, req)
	if err != nil {
		return nil, err
	}
	snap := v.snap
	switch {
	case v.mode == PayerModePatient:
		party, err := ensurePatientParty(tx, patientID, snap.DisplayName, snap.Phone)
		if err != nil {
			return nil, err
		}
		snap.PartyID = party.ID
	case v.existing != nil:
		snap.PartyID = v.existing.ID
	default:
		party := FinancialParty{Kind: snap.Kind, DisplayName: snap.DisplayName, Phone: snap.Phone}
		if e := tx.Create(&party).Error; e != nil {
			return nil, e
		}
		snap.PartyID = party.ID
	}
	snap.Fingerprint = payerFingerprint(snap)
	return &snap, nil
}

func ensurePatientParty(tx *gorm.DB, patientID uint, name, phone string) (*FinancialParty, error) {
	var party FinancialParty
	e := tx.Where("patient_id=?", patientID).First(&party).Error
	if e == nil {
		// Master may update; payment snapshot stays frozen at pay time.
		_ = tx.Model(&party).Updates(map[string]any{
			"display_name": name,
			"phone":        phone,
			"kind":         PartyKindIndividual,
		}).Error
		party.DisplayName = name
		party.Phone = phone
		return &party, nil
	}
	if !errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, e
	}
	pid := patientID
	party = FinancialParty{
		Kind:        PartyKindIndividual,
		DisplayName: name,
		Phone:       phone,
		PatientID:   &pid,
	}
	if e := tx.Create(&party).Error; e != nil {
		// Race: unique patient_id — recover.
		var raced FinancialParty
		if load := tx.Where("patient_id=?", patientID).First(&raced).Error; load == nil {
			return &raced, nil
		}
		return nil, e
	}
	return &party, nil
}

// ApplyPayerSnapshot copies resolved payer onto a Payment row (immutable after create).
func ApplyPayerSnapshot(p *Payment, s ResolvedPayerSnapshot) {
	id := s.PartyID
	p.PayerPartyID = &id
	p.PayerKind = s.Kind
	p.PayerDisplayName = s.DisplayName
	p.PayerPhone = s.Phone
	p.PayerRelationship = s.Relationship
	p.PayerIsPatient = s.IsPatient
	p.PayerProvenance = s.Provenance
	p.PayerFingerprint = s.Fingerprint
}

func paymentPayerFingerprintMatch(p Payment, snap *ResolvedPayerSnapshot) bool {
	if snap == nil {
		return p.PayerProvenance == PayerProvenanceLegacyUnconfirmed || p.PayerProvenance == ""
	}
	return p.PayerFingerprint == snap.Fingerprint &&
		p.PayerProvenance == PayerProvenanceCaptured
}

// PatientPayerRequest is the canonical PATIENT-mode payload (tests + FE default).
func PatientPayerRequest() *PayerRequest {
	return &PayerRequest{Mode: PayerModePatient}
}

// RedactPayerContacts strips payer phone (PII) when caller lacks billing.payer.read.
func RedactPayerContacts(inv *Invoice) {
	if inv == nil {
		return
	}
	for i := range inv.Payments {
		inv.Payments[i].PayerPhone = ""
	}
}

// RedactPaymentPayerContact strips phone on a single payment.
func RedactPaymentPayerContact(p *Payment) {
	if p != nil {
		p.PayerPhone = ""
	}
}
