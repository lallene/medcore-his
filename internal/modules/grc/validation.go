package grc

import (
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
)

var validAssetTypes = map[string]bool{
	AssetTypeApplication:     true,
	AssetTypeInfrastructure:  true,
	AssetTypeServer:          true,
	AssetTypeDatabase:        true,
	AssetTypeNetwork:         true,
	AssetTypeData:            true,
	AssetTypeBusinessService: true,
	AssetTypeVendor:          true,
	AssetTypeProcess:         true,
	AssetTypeSite:            true,
}

var validCriticalities = map[string]bool{
	AssetCriticalityLow:      true,
	AssetCriticalityMedium:   true,
	AssetCriticalityHigh:     true,
	AssetCriticalityCritical: true,
}

var validAssetStatuses = map[string]bool{
	AssetStatusActive:   true,
	AssetStatusInactive: true,
	AssetStatusRetired:  true,
}

func normalizeCode(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func normalizeEnum(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func normalizeText(value string) string {
	return strings.TrimSpace(value)
}

// Lengths mirror the grc_assets columns, so PostgreSQL never truncates or
// rejects a value the service accepted.
const (
	assetCodeMaxLength = 60
	assetNameMaxLength = 180
)

func normalizeAssetRequest(r *AssetRequest) {
	r.Code = normalizeCode(r.Code)
	r.Name = normalizeText(r.Name)
	r.Type = normalizeEnum(r.Type)
	r.Description = normalizeText(r.Description)
	r.Criticality = normalizeEnum(r.Criticality)
	r.Status = normalizeEnum(r.Status)
}

// validateAssetRequest checks a normalized request without any database
// access: relation existence and activity are checked by the service.
func validateAssetRequest(r AssetRequest) error {
	if r.Code == "" || r.Name == "" {
		return coreerrors.BadRequest("Code et nom de l'actif obligatoires")
	}
	if utf8.RuneCountInString(r.Code) > assetCodeMaxLength {
		return coreerrors.BadRequest("Code de l'actif trop long")
	}
	if utf8.RuneCountInString(r.Name) > assetNameMaxLength {
		return coreerrors.BadRequest("Nom de l'actif trop long")
	}
	if !isValidAssetType(r.Type) {
		return coreerrors.BadRequest("Type d'actif invalide")
	}
	if !isValidCriticality(r.Criticality) {
		return coreerrors.BadRequest("Criticité de l'actif invalide")
	}
	if !isValidAssetStatus(r.Status) {
		return coreerrors.BadRequest("Statut de l'actif invalide")
	}
	if !isPositiveOptionalID(r.OwningServiceID) || !isPositiveOptionalID(r.OwnerProfileID) {
		return coreerrors.BadRequest("Identifiant de relation de l'actif invalide")
	}
	return nil
}

func isValidAssetType(value string) bool {
	return validAssetTypes[value]
}

func isValidCriticality(value string) bool {
	return validCriticalities[value]
}

func isValidAssetStatus(value string) bool {
	return validAssetStatuses[value]
}

var validRiskTreatments = map[string]bool{
	RiskTreatmentReduce:   true,
	RiskTreatmentAccept:   true,
	RiskTreatmentAvoid:    true,
	RiskTreatmentTransfer: true,
}

var validRiskStatuses = map[string]bool{
	RiskStatusDraft:       true,
	RiskStatusOpen:        true,
	RiskStatusInTreatment: true,
	RiskStatusAccepted:    true,
	RiskStatusClosed:      true,
}

func isValidRiskTreatment(value string) bool {
	return validRiskTreatments[value]
}

func isValidRiskStatus(value string) bool {
	return validRiskStatuses[value]
}

var validFrameworkStatuses = map[string]bool{
	FrameworkStatusDraft:    true,
	FrameworkStatusActive:   true,
	FrameworkStatusArchived: true,
}

var validRequirementStatuses = map[string]bool{
	RequirementStatusActive:   true,
	RequirementStatusInactive: true,
}

var validControlStatuses = map[string]bool{
	ControlStatusDraft:    true,
	ControlStatusActive:   true,
	ControlStatusInactive: true,
	ControlStatusRetired:  true,
}

var validControlImplementationStatuses = map[string]bool{
	ControlImplementationStatusPlanned:        true,
	ControlImplementationStatusImplemented:    true,
	ControlImplementationStatusPartial:        true,
	ControlImplementationStatusNotImplemented: true,
	ControlImplementationStatusNotApplicable:  true,
}

func isValidFrameworkStatus(value string) bool {
	return validFrameworkStatuses[value]
}

func isValidRequirementStatus(value string) bool {
	return validRequirementStatuses[value]
}

func isValidControlStatus(value string) bool {
	return validControlStatuses[value]
}

func isValidControlImplementationStatus(value string) bool {
	return validControlImplementationStatuses[value]
}

const evidenceExternalReferenceMaxLength = 500

// Lengths mirror the grc_evidence and grc_remediation_actions columns.
const (
	evidenceReferenceMaxLength          = 60
	evidenceTitleMaxLength              = 220
	remediationActionReferenceMaxLength = 60
	remediationActionTitleMaxLength     = 220
)

var validEvidenceTypes = map[string]bool{
	EvidenceTypeDocument:      true,
	EvidenceTypeConfiguration: true,
	EvidenceTypeLog:           true,
	EvidenceTypeScreenshot:    true,
	EvidenceTypeReport:        true,
	EvidenceTypeTestResult:    true,
	EvidenceTypeCertificate:   true,
	EvidenceTypeOther:         true,
}

var validEvidenceStatuses = map[string]bool{
	EvidenceStatusDraft:   true,
	EvidenceStatusValid:   true,
	EvidenceStatusRevoked: true,
}

var validRemediationActionPriorities = map[string]bool{
	RemediationActionPriorityLow:      true,
	RemediationActionPriorityMedium:   true,
	RemediationActionPriorityHigh:     true,
	RemediationActionPriorityCritical: true,
}

var validRemediationActionStatuses = map[string]bool{
	RemediationActionStatusOpen:       true,
	RemediationActionStatusInProgress: true,
	RemediationActionStatusBlocked:    true,
	RemediationActionStatusDone:       true,
	RemediationActionStatusCancelled:  true,
}

func isValidEvidenceType(value string) bool {
	return validEvidenceTypes[value]
}

func isValidEvidenceStatus(value string) bool {
	return validEvidenceStatuses[value]
}

func isValidRemediationActionPriority(value string) bool {
	return validRemediationActionPriorities[value]
}

func isValidRemediationActionStatus(value string) bool {
	return validRemediationActionStatuses[value]
}

// normalizeEvidenceRequest trims and upper-cases text fields; timestamps are
// left untouched.
func normalizeEvidenceRequest(r *EvidenceRequest) {
	r.Reference = normalizeCode(r.Reference)
	r.Title = normalizeText(r.Title)
	r.Description = normalizeText(r.Description)
	r.Type = normalizeEnum(r.Type)
	r.Status = normalizeEnum(r.Status)
	r.ExternalReference = normalizeText(r.ExternalReference)
}

func normalizeRemediationActionRequest(r *RemediationActionRequest) {
	r.Reference = normalizeCode(r.Reference)
	r.Title = normalizeText(r.Title)
	r.Description = normalizeText(r.Description)
	r.Priority = normalizeEnum(r.Priority)
	r.Status = normalizeEnum(r.Status)
}

func isPositiveOptionalID(id *uint) bool {
	return id == nil || *id > 0
}

// validateEvidenceRequest checks a normalized request without any database
// access: relation existence and activity are checked by the service.
func validateEvidenceRequest(r EvidenceRequest) error {
	if r.Reference == "" || r.Title == "" {
		return coreerrors.BadRequest("Référence et titre de la preuve obligatoires")
	}
	if utf8.RuneCountInString(r.Reference) > evidenceReferenceMaxLength {
		return coreerrors.BadRequest("Référence de la preuve trop longue")
	}
	if utf8.RuneCountInString(r.Title) > evidenceTitleMaxLength {
		return coreerrors.BadRequest("Titre de la preuve trop long")
	}
	if r.ControlImplementationID == 0 {
		return coreerrors.BadRequest("Implémentation de contrôle de la preuve obligatoire")
	}
	if !isValidEvidenceType(r.Type) {
		return coreerrors.BadRequest("Type de preuve invalide")
	}
	if !isValidEvidenceStatus(r.Status) {
		return coreerrors.BadRequest("Statut de la preuve invalide")
	}
	if !isPositiveOptionalID(r.OwnerProfileID) || !isPositiveOptionalID(r.OwningServiceID) {
		return coreerrors.BadRequest("Identifiant de relation de la preuve invalide")
	}
	if r.ValidFrom != nil && r.ExpiresAt != nil && r.ExpiresAt.Before(*r.ValidFrom) {
		return coreerrors.BadRequest("La date d'expiration de la preuve précède sa date de validité")
	}

	return validateEvidenceExternalReference(r.ExternalReference)
}

// validateEvidenceExternalReference accepts opaque references (for example
// DOC-SEC-2026-0042) and URIs without credentials, query or fragment. The value
// itself is never included in errors or logs.
func validateEvidenceExternalReference(value string) error {
	if value == "" {
		return nil
	}
	if utf8.RuneCountInString(value) > evidenceExternalReferenceMaxLength {
		return coreerrors.BadRequest("Référence externe de la preuve trop longue")
	}
	if strings.ContainsAny(value, "\r\n") {
		return coreerrors.BadRequest("La référence externe de la preuve ne doit pas contenir de saut de ligne")
	}

	parsed, err := url.Parse(value)
	if err != nil {
		if strings.Contains(value, "://") {
			return coreerrors.BadRequest("Référence externe de la preuve invalide")
		}
		return nil
	}
	if parsed.Scheme == "" {
		return nil
	}
	if parsed.User != nil {
		return coreerrors.BadRequest("La référence externe de la preuve ne doit pas contenir d'identifiants")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return coreerrors.BadRequest("La référence externe de la preuve ne doit pas contenir de paramètres ni de fragment")
	}

	return nil
}

// validateRemediationActionRequest checks a normalized request without any
// database access. A DONE action may omit CompletedAt: the service fills it
// before saving, as required by grc_remediation_action_completion.
func validateRemediationActionRequest(r RemediationActionRequest) error {
	if r.Reference == "" || r.Title == "" {
		return coreerrors.BadRequest("Référence et titre de l'action obligatoires")
	}
	if utf8.RuneCountInString(r.Reference) > remediationActionReferenceMaxLength {
		return coreerrors.BadRequest("Référence de l'action trop longue")
	}
	if utf8.RuneCountInString(r.Title) > remediationActionTitleMaxLength {
		return coreerrors.BadRequest("Titre de l'action trop long")
	}
	if !isValidRemediationActionPriority(r.Priority) {
		return coreerrors.BadRequest("Priorité de l'action invalide")
	}
	if !isValidRemediationActionStatus(r.Status) {
		return coreerrors.BadRequest("Statut de l'action invalide")
	}
	// Same rule as grc_remediation_action_target_present_v2.
	if r.ControlImplementationID == nil && r.RiskID == nil && r.FindingID == nil {
		return coreerrors.BadRequest("L'action doit cibler une implémentation de contrôle, un risque ou un constat")
	}
	for _, id := range []*uint{r.ControlImplementationID, r.RiskID, r.FindingID, r.AssigneeProfileID, r.OwningServiceID} {
		if !isPositiveOptionalID(id) {
			return coreerrors.BadRequest("Identifiant de relation de l'action invalide")
		}
	}
	if r.Status != RemediationActionStatusDone && r.CompletedAt != nil {
		return coreerrors.BadRequest("Seule une action terminée peut avoir une date de réalisation")
	}

	return nil
}

// EvidenceExpired reports whether a valid evidence has passed its expiry.
// An evidence expiring exactly at now is not expired yet.
func EvidenceExpired(status string, expiresAt *time.Time, now time.Time) bool {
	return status == EvidenceStatusValid && expiresAt != nil && expiresAt.Before(now)
}

// RemediationActionOverdue reports whether a non-terminal action has passed
// its due date. A due date exactly at now is not overdue yet.
func RemediationActionOverdue(status string, dueAt *time.Time, now time.Time) bool {
	return dueAt != nil &&
		dueAt.Before(now) &&
		status != RemediationActionStatusDone &&
		status != RemediationActionStatusCancelled
}
