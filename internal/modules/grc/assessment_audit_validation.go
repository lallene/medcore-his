package grc

import (
	"time"
	"unicode/utf8"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
)

// Pure validation for GRC-01D: no database access and no system clock.
// Rules depending on the current time (assessed_at not in the future, ...)
// or on stored relations belong to the services; PostgreSQL CHECK
// constraints remain the last integrity guard.

var validAssessmentTypes = map[string]bool{
	AssessmentTypeDesign:                 true,
	AssessmentTypeOperatingEffectiveness: true,
	AssessmentTypeCompliance:             true,
}

var validAssessmentStatuses = map[string]bool{
	AssessmentStatusDraft:     true,
	AssessmentStatusCompleted: true,
	AssessmentStatusCancelled: true,
}

var validAssessmentResults = map[string]bool{
	AssessmentResultEffective:          true,
	AssessmentResultPartiallyEffective: true,
	AssessmentResultIneffective:        true,
	AssessmentResultNotApplicable:      true,
}

var validAuditEngagementTypes = map[string]bool{
	AuditEngagementTypeInternal: true,
	AuditEngagementTypeExternal: true,
	AuditEngagementTypeVendor:   true,
}

var validAuditEngagementStatuses = map[string]bool{
	AuditEngagementStatusPlanned:    true,
	AuditEngagementStatusInProgress: true,
	AuditEngagementStatusCompleted:  true,
	AuditEngagementStatusCancelled:  true,
}

var validFindingCategories = map[string]bool{
	FindingCategoryNonConformity:     true,
	FindingCategoryControlDeficiency: true,
	FindingCategoryObservation:       true,
	FindingCategoryImprovement:       true,
}

var validFindingSeverities = map[string]bool{
	FindingSeverityLow:      true,
	FindingSeverityMedium:   true,
	FindingSeverityHigh:     true,
	FindingSeverityCritical: true,
}

var validFindingStatuses = map[string]bool{
	FindingStatusOpen:          true,
	FindingStatusInRemediation: true,
	FindingStatusResolved:      true,
	FindingStatusClosed:        true,
	FindingStatusAccepted:      true,
}

// Allowed status transitions, keyed by current status. Staying in a
// non-terminal status (editing other fields) is an allowed transition;
// terminal statuses have no entry.
var assessmentTransitions = map[string]map[string]bool{
	AssessmentStatusDraft: {
		AssessmentStatusDraft:     true,
		AssessmentStatusCompleted: true,
		AssessmentStatusCancelled: true,
	},
}

var auditEngagementTransitions = map[string]map[string]bool{
	AuditEngagementStatusPlanned: {
		AuditEngagementStatusPlanned:    true,
		AuditEngagementStatusInProgress: true,
		AuditEngagementStatusCancelled:  true,
	},
	AuditEngagementStatusInProgress: {
		AuditEngagementStatusInProgress: true,
		AuditEngagementStatusCompleted:  true,
		AuditEngagementStatusCancelled:  true,
	},
}

var findingTransitions = map[string]map[string]bool{
	FindingStatusOpen: {
		FindingStatusOpen:          true,
		FindingStatusInRemediation: true,
		FindingStatusResolved:      true,
		FindingStatusAccepted:      true,
	},
	FindingStatusInRemediation: {
		FindingStatusInRemediation: true,
		FindingStatusResolved:      true,
		FindingStatusAccepted:      true,
	},
	FindingStatusResolved: {
		FindingStatusResolved: true,
		FindingStatusOpen:     true,
		FindingStatusClosed:   true,
		FindingStatusAccepted: true,
	},
}

func isValidAssessmentType(value string) bool   { return validAssessmentTypes[value] }
func isValidAssessmentStatus(value string) bool { return validAssessmentStatuses[value] }
func isValidAssessmentResult(value string) bool { return validAssessmentResults[value] }

func isValidAuditEngagementType(value string) bool   { return validAuditEngagementTypes[value] }
func isValidAuditEngagementStatus(value string) bool { return validAuditEngagementStatuses[value] }

func isValidFindingCategory(value string) bool { return validFindingCategories[value] }
func isValidFindingSeverity(value string) bool { return validFindingSeverities[value] }
func isValidFindingStatus(value string) bool   { return validFindingStatuses[value] }

// normalizeOptionalEnum trims and upper-cases an optional enum; an empty
// value becomes nil.
func normalizeOptionalEnum(value *string) *string {
	if value == nil {
		return nil
	}
	normalized := normalizeEnum(*value)
	if normalized == "" {
		return nil
	}
	return &normalized
}

func allPositiveOptionalIDs(ids ...*uint) bool {
	for _, id := range ids {
		if !isPositiveOptionalID(id) {
			return false
		}
	}
	return true
}

func validateOptionalTimeRange(from, to *time.Time, message string) error {
	if from != nil && to != nil && to.Before(*from) {
		return coreerrors.BadRequest(message)
	}
	return nil
}

func validateTransition(transitions map[string]map[string]bool, from, to, terminalMessage, forbiddenMessage string) error {
	allowed, ok := transitions[from]
	if !ok {
		return coreerrors.Conflict(terminalMessage)
	}
	if !allowed[to] {
		return coreerrors.Conflict(forbiddenMessage)
	}
	return nil
}

// --- Assessment ---

func normalizeAssessmentRequest(r *AssessmentRequest) {
	r.Reference = normalizeCode(r.Reference)
	r.Title = normalizeText(r.Title)
	r.Type = normalizeEnum(r.Type)
	r.Status = normalizeEnum(r.Status)
	r.Result = normalizeOptionalEnum(r.Result)
	r.Summary = normalizeText(r.Summary)
}

func validateAssessmentRequest(r AssessmentRequest) error {
	if r.Reference == "" || r.Title == "" {
		return coreerrors.BadRequest("Référence et titre de l'évaluation obligatoires")
	}
	if r.ControlImplementationID == 0 {
		return coreerrors.BadRequest("Implémentation de contrôle de l'évaluation obligatoire")
	}
	if !isValidAssessmentType(r.Type) {
		return coreerrors.BadRequest("Type d'évaluation invalide")
	}
	if !isValidAssessmentStatus(r.Status) {
		return coreerrors.BadRequest("Statut de l'évaluation invalide")
	}
	if r.Result != nil && !isValidAssessmentResult(*r.Result) {
		return coreerrors.BadRequest("Résultat de l'évaluation invalide")
	}
	if !isPositiveOptionalID(r.AssessorProfileID) {
		return coreerrors.BadRequest("Identifiant d'évaluateur invalide")
	}
	if r.Status == AssessmentStatusCompleted &&
		(r.Result == nil || r.AssessorProfileID == nil || r.AssessedAt == nil) {
		return coreerrors.BadRequest("Une évaluation terminée exige un résultat, un évaluateur et une date d'évaluation")
	}
	if r.Result != nil && *r.Result == AssessmentResultNotApplicable && r.Summary == "" {
		return coreerrors.BadRequest("Une évaluation non applicable exige une justification")
	}
	return validateOptionalTimeRange(r.AssessedAt, r.NextAssessmentAt,
		"La prochaine évaluation précède la date d'évaluation")
}

// validateAssessmentInitialStatus: an assessment is always created as DRAFT;
// completion and cancellation are transitions.
func validateAssessmentInitialStatus(status string) error {
	if status != AssessmentStatusDraft {
		return coreerrors.BadRequest("Une évaluation est créée au statut brouillon")
	}
	return nil
}

func validateAssessmentTransition(from, to string) error {
	return validateTransition(assessmentTransitions, from, to,
		"Une évaluation terminée ou annulée ne peut plus être modifiée",
		"Transition de statut de l'évaluation interdite")
}

func validateAssessmentEvidenceRequest(r AssessmentEvidenceRequest) error {
	if r.EvidenceID == 0 {
		return coreerrors.BadRequest("Preuve de l'évaluation obligatoire")
	}
	return nil
}

func normalizeAssessmentFilter(f *AssessmentFilter) {
	f.Type = normalizeEnum(f.Type)
	f.Status = normalizeEnum(f.Status)
	f.Result = normalizeEnum(f.Result)
}

func validateAssessmentFilter(f AssessmentFilter) error {
	if f.Type != "" && !isValidAssessmentType(f.Type) {
		return coreerrors.BadRequest("Type d'évaluation invalide")
	}
	if f.Status != "" && !isValidAssessmentStatus(f.Status) {
		return coreerrors.BadRequest("Statut de l'évaluation invalide")
	}
	if f.Result != "" && !isValidAssessmentResult(f.Result) {
		return coreerrors.BadRequest("Résultat de l'évaluation invalide")
	}
	if !allPositiveOptionalIDs(f.ControlImplementationID, f.AssessorProfileID) {
		return coreerrors.BadRequest("Identifiant de filtre invalide")
	}
	return validateOptionalTimeRange(f.AssessedAfter, f.AssessedBefore, "Période d'évaluation invalide")
}

// AssessmentReassessmentDue reports whether a completed assessment has passed
// its next assessment date. A date exactly at now is not due yet.
func AssessmentReassessmentDue(status string, nextAssessmentAt *time.Time, now time.Time) bool {
	return status == AssessmentStatusCompleted && nextAssessmentAt != nil && nextAssessmentAt.Before(now)
}

// --- AuditEngagement ---

func normalizeAuditEngagementRequest(r *AuditEngagementRequest) {
	r.Reference = normalizeCode(r.Reference)
	r.Title = normalizeText(r.Title)
	r.Description = normalizeText(r.Description)
	r.Scope = normalizeText(r.Scope)
	r.Type = normalizeEnum(r.Type)
	r.Status = normalizeEnum(r.Status)
}

func validateAuditEngagementRequest(r AuditEngagementRequest) error {
	if r.Reference == "" || r.Title == "" {
		return coreerrors.BadRequest("Référence et titre de l'audit obligatoires")
	}
	if utf8.RuneCountInString(r.Reference) > auditEngagementReferenceMaxLength {
		return coreerrors.BadRequest("Référence de l'audit trop longue")
	}
	if utf8.RuneCountInString(r.Title) > auditEngagementTitleMaxLength {
		return coreerrors.BadRequest("Titre de l'audit trop long")
	}
	if !isValidAuditEngagementType(r.Type) {
		return coreerrors.BadRequest("Type d'audit invalide")
	}
	if !isValidAuditEngagementStatus(r.Status) {
		return coreerrors.BadRequest("Statut de l'audit invalide")
	}
	if !allPositiveOptionalIDs(r.LeadAuditorProfileID, r.OwningServiceID) {
		return coreerrors.BadRequest("Identifiant de relation de l'audit invalide")
	}
	return validateOptionalTimeRange(r.PlannedStartAt, r.PlannedEndAt,
		"La fin prévue de l'audit précède son début prévu")
}

// validateAuditEngagementInitialStatus: an audit is created as planned, or
// directly in progress when it is recorded once fieldwork has started.
func validateAuditEngagementInitialStatus(status string) error {
	if status != AuditEngagementStatusPlanned && status != AuditEngagementStatusInProgress {
		return coreerrors.BadRequest("Un audit est créé au statut planifié ou en cours")
	}
	return nil
}

func validateAuditEngagementTransition(from, to string) error {
	return validateTransition(auditEngagementTransitions, from, to,
		"Un audit terminé ou annulé ne peut plus être modifié",
		"Transition de statut de l'audit interdite")
}

func normalizeAuditEngagementFilter(f *AuditEngagementFilter) {
	f.Type = normalizeEnum(f.Type)
	f.Status = normalizeEnum(f.Status)
}

func validateAuditEngagementFilter(f AuditEngagementFilter) error {
	if f.Type != "" && !isValidAuditEngagementType(f.Type) {
		return coreerrors.BadRequest("Type d'audit invalide")
	}
	if f.Status != "" && !isValidAuditEngagementStatus(f.Status) {
		return coreerrors.BadRequest("Statut de l'audit invalide")
	}
	if !allPositiveOptionalIDs(f.LeadAuditorProfileID, f.OwningServiceID) {
		return coreerrors.BadRequest("Identifiant de filtre invalide")
	}
	return validateOptionalTimeRange(f.PlannedStartAfter, f.PlannedStartBefore, "Période de début prévu invalide")
}

// --- Finding ---

func normalizeFindingRequest(r *FindingRequest) {
	r.Reference = normalizeCode(r.Reference)
	r.Title = normalizeText(r.Title)
	r.Description = normalizeText(r.Description)
	r.Category = normalizeEnum(r.Category)
	r.Severity = normalizeEnum(r.Severity)
	r.Status = normalizeEnum(r.Status)
	r.ClosureNote = normalizeText(r.ClosureNote)
}

func validateFindingRequest(r FindingRequest) error {
	if r.Reference == "" || r.Title == "" {
		return coreerrors.BadRequest("Référence et titre du constat obligatoires")
	}
	if utf8.RuneCountInString(r.Reference) > findingReferenceMaxLength {
		return coreerrors.BadRequest("Référence du constat trop longue")
	}
	if utf8.RuneCountInString(r.Title) > findingTitleMaxLength {
		return coreerrors.BadRequest("Titre du constat trop long")
	}
	if (r.AuditEngagementID == nil) == (r.AssessmentID == nil) {
		return coreerrors.BadRequest("Un constat provient d'un audit ou d'une évaluation, exactement")
	}
	if !isValidFindingCategory(r.Category) {
		return coreerrors.BadRequest("Catégorie du constat invalide")
	}
	if !isValidFindingSeverity(r.Severity) {
		return coreerrors.BadRequest("Sévérité du constat invalide")
	}
	if !isValidFindingStatus(r.Status) {
		return coreerrors.BadRequest("Statut du constat invalide")
	}
	if !allPositiveOptionalIDs(r.AuditEngagementID, r.AssessmentID, r.ControlImplementationID,
		r.RiskID, r.OwnerProfileID, r.OwningServiceID) {
		return coreerrors.BadRequest("Identifiant de relation du constat invalide")
	}
	if r.Status == FindingStatusAccepted && r.ClosureNote == "" {
		return coreerrors.BadRequest("Un constat accepté exige une justification")
	}
	return nil
}

func validateFindingInitialStatus(status string) error {
	if status != FindingStatusOpen {
		return coreerrors.BadRequest("Un constat est créé au statut ouvert")
	}
	return nil
}

func validateFindingTransition(from, to string) error {
	return validateTransition(findingTransitions, from, to,
		"Un constat clôturé ou accepté ne peut plus être modifié",
		"Transition de statut du constat interdite")
}

func normalizeFindingFilter(f *FindingFilter) {
	f.Category = normalizeEnum(f.Category)
	f.Severity = normalizeEnum(f.Severity)
	f.Status = normalizeEnum(f.Status)
}

func validateFindingFilter(f FindingFilter) error {
	if f.Category != "" && !isValidFindingCategory(f.Category) {
		return coreerrors.BadRequest("Catégorie du constat invalide")
	}
	if f.Severity != "" && !isValidFindingSeverity(f.Severity) {
		return coreerrors.BadRequest("Sévérité du constat invalide")
	}
	if f.Status != "" && !isValidFindingStatus(f.Status) {
		return coreerrors.BadRequest("Statut du constat invalide")
	}
	if !allPositiveOptionalIDs(f.AuditEngagementID, f.AssessmentID, f.ControlImplementationID,
		f.RiskID, f.OwnerProfileID, f.OwningServiceID) {
		return coreerrors.BadRequest("Identifiant de filtre invalide")
	}
	return nil
}

// FindingOverdue reports whether a finding still awaiting remediation (OPEN
// or IN_REMEDIATION) has passed its due date. A due date exactly at now is
// not overdue yet; a RESOLVED finding awaits verification and is not overdue.
func FindingOverdue(status string, dueAt *time.Time, now time.Time) bool {
	return dueAt != nil && dueAt.Before(now) &&
		(status == FindingStatusOpen || status == FindingStatusInRemediation)
}
