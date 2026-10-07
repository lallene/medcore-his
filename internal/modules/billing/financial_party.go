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
	Kind        string `gorm:"size:20;not null;index" json:"kind"`
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

// PreviewPayerFingerprint validates payer payload and returns fingerprint without creating parties.
func PreviewPayerFingerprint(tx *gorm.DB, patientID uint, req *PayerRequest) (string, error) {
	if req == nil {
		return "", coreerrors.BadRequest("Payeur obligatoire pour un nouvel encaissement")
	}
	mode := strings.ToUpper(strings.TrimSpace(req.Mode))
	switch mode {
	case PayerModePatient:
		var patient struct {
			ID                      uint
			Nom, Prenoms, Telephone string
		}
		if e := tx.Table("patients").Select("id,nom,prenoms,telephone").Where("id=?", patientID).Scan(&patient).Error; e != nil {
			return "", e
		}
		if patient.ID == 0 {
			return "", coreerrors.NotFound("PATIENT")
		}
		name := strings.TrimSpace(patient.Prenoms + " " + patient.Nom)
		if name == "" {
			name = strings.TrimSpace(patient.Nom)
		}
		if name == "" {
			return "", coreerrors.Conflict("Identité patient insuffisante pour le payeur")
		}
		phone, err := NormalizePayerPhone(patient.Telephone)
		if err != nil {
			return "", err
		}
		return payerFingerprint(ResolvedPayerSnapshot{
			Kind: PartyKindIndividual, DisplayName: name, Phone: phone,
			IsPatient: true, Provenance: PayerProvenanceCaptured,
		}), nil
	case PayerModeIndividual:
		name, err := NormalizePayerName(req.DisplayName)
		if err != nil {
			return "", err
		}
		phone, err := NormalizePayerPhone(req.Phone)
		if err != nil {
			return "", err
		}
		if phone == "" {
			return "", coreerrors.BadRequest("Téléphone du payeur obligatoire")
		}
		rel, err := NormalizePayerRelationship(req.Relationship)
		if err != nil {
			return "", err
		}
		if rel == "" {
			return "", coreerrors.BadRequest("Lien avec le patient obligatoire")
		}
		if req.PartyID != nil && *req.PartyID > 0 {
			var party FinancialParty
			if e := tx.First(&party, *req.PartyID).Error; e != nil {
				if errors.Is(e, gorm.ErrRecordNotFound) {
					return "", coreerrors.NotFound("FINANCIAL_PARTY")
				}
				return "", e
			}
			if party.Kind != PartyKindIndividual {
				return "", coreerrors.Conflict("Type de payeur incohérent avec la fiche référencée")
			}
		}
		return payerFingerprint(ResolvedPayerSnapshot{
			Kind: PartyKindIndividual, DisplayName: name, Phone: phone, Relationship: rel,
			IsPatient: false, Provenance: PayerProvenanceCaptured,
		}), nil
	case PayerModeOrganization:
		name, err := NormalizePayerName(req.DisplayName)
		if err != nil {
			return "", err
		}
		phone, err := NormalizePayerPhone(req.Phone)
		if err != nil {
			return "", err
		}
		if req.PartyID != nil && *req.PartyID > 0 {
			var party FinancialParty
			if e := tx.First(&party, *req.PartyID).Error; e != nil {
				if errors.Is(e, gorm.ErrRecordNotFound) {
					return "", coreerrors.NotFound("FINANCIAL_PARTY")
				}
				return "", e
			}
			if party.Kind != PartyKindOrganization {
				return "", coreerrors.Conflict("Le payeur référencé n'est pas une organisation")
			}
		}
		return payerFingerprint(ResolvedPayerSnapshot{
			Kind: PartyKindOrganization, DisplayName: name, Phone: phone,
			IsPatient: false, Provenance: PayerProvenanceCaptured,
		}), nil
	default:
		return "", coreerrors.BadRequest("Type de payeur non supporté")
	}
}

// ResolvePayerForPayment builds/loads FinancialParty and immutable snapshot for a NEW payment.
// patientID is the invoice patient (clinical subject); never inferred as payer without Mode=PATIENT.
func ResolvePayerForPayment(tx *gorm.DB, patientID uint, req *PayerRequest) (*ResolvedPayerSnapshot, error) {
	if req == nil {
		return nil, coreerrors.BadRequest("Payeur obligatoire pour un nouvel encaissement")
	}
	mode := strings.ToUpper(strings.TrimSpace(req.Mode))
	switch mode {
	case PayerModePatient:
		return resolvePatientAsPayer(tx, patientID)
	case PayerModeIndividual:
		return resolveThirdPartyIndividual(tx, patientID, req)
	case PayerModeOrganization:
		return resolveOrganizationPayer(tx, req)
	default:
		return nil, coreerrors.BadRequest("Type de payeur non supporté")
	}
}

func resolvePatientAsPayer(tx *gorm.DB, patientID uint) (*ResolvedPayerSnapshot, error) {
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
	party, err := ensurePatientParty(tx, patientID, name, phone)
	if err != nil {
		return nil, err
	}
	snap := ResolvedPayerSnapshot{
		PartyID:     party.ID,
		Kind:        PartyKindIndividual,
		DisplayName: name,
		Phone:       phone,
		IsPatient:   true,
		Provenance:  PayerProvenanceCaptured,
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

func resolveThirdPartyIndividual(tx *gorm.DB, patientID uint, req *PayerRequest) (*ResolvedPayerSnapshot, error) {
	_ = patientID // association is via relationship text only in H-B
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
	rel, err := NormalizePayerRelationship(req.Relationship)
	if err != nil {
		return nil, err
	}
	if rel == "" {
		return nil, coreerrors.BadRequest("Lien avec le patient obligatoire")
	}
	party, err := loadOrCreateParty(tx, req.PartyID, PartyKindIndividual, name, phone)
	if err != nil {
		return nil, err
	}
	if party.PatientID != nil {
		return nil, coreerrors.Conflict("Ce payeur représente déjà le patient — utilisez le mode patient")
	}
	snap := ResolvedPayerSnapshot{
		PartyID:      party.ID,
		Kind:         PartyKindIndividual,
		DisplayName:  name,
		Phone:        phone,
		Relationship: rel,
		IsPatient:    false,
		Provenance:   PayerProvenanceCaptured,
	}
	snap.Fingerprint = payerFingerprint(snap)
	return &snap, nil
}

func resolveOrganizationPayer(tx *gorm.DB, req *PayerRequest) (*ResolvedPayerSnapshot, error) {
	name, err := NormalizePayerName(req.DisplayName)
	if err != nil {
		return nil, err
	}
	phone, err := NormalizePayerPhone(req.Phone)
	if err != nil {
		return nil, err
	}
	party, err := loadOrCreateParty(tx, req.PartyID, PartyKindOrganization, name, phone)
	if err != nil {
		return nil, err
	}
	if party.Kind != PartyKindOrganization {
		return nil, coreerrors.Conflict("Le payeur référencé n'est pas une organisation")
	}
	snap := ResolvedPayerSnapshot{
		PartyID:     party.ID,
		Kind:        PartyKindOrganization,
		DisplayName: name,
		Phone:       phone,
		IsPatient:   false,
		Provenance:  PayerProvenanceCaptured,
	}
	snap.Fingerprint = payerFingerprint(snap)
	return &snap, nil
}

func loadOrCreateParty(tx *gorm.DB, partyID *uint, kind, name, phone string) (*FinancialParty, error) {
	if partyID != nil && *partyID > 0 {
		var party FinancialParty
		if e := tx.First(&party, *partyID).Error; e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return nil, coreerrors.NotFound("FINANCIAL_PARTY")
			}
			return nil, e
		}
		if party.Kind != kind {
			return nil, coreerrors.Conflict("Type de payeur incohérent avec la fiche référencée")
		}
		return &party, nil
	}
	party := FinancialParty{Kind: kind, DisplayName: name, Phone: phone}
	if e := tx.Create(&party).Error; e != nil {
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
