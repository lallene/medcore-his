package grc

import (
	"time"
	"unicode/utf8"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
)

const (
	securityIncidentReferenceMaxLength = 60
	securityIncidentTitleMaxLength     = 220
)

var (
	validSecurityIncidentSeverities = map[string]bool{
		SecurityIncidentSeverityLow:      true,
		SecurityIncidentSeverityMedium:   true,
		SecurityIncidentSeverityHigh:     true,
		SecurityIncidentSeverityCritical: true,
	}
	validSecurityIncidentStatuses = map[string]bool{
		SecurityIncidentStatusOpen:          true,
		SecurityIncidentStatusInvestigating: true,
		SecurityIncidentStatusContained:     true,
		SecurityIncidentStatusResolved:      true,
		SecurityIncidentStatusClosed:        true,
	}
	// securityIncidentTransitions is the official lifecycle. CLOSED is
	// terminal. Reaching CLOSED is not a generic update: the handler and the
	// service refuse that target even when the transition table allows it
	// from RESOLVED.
	securityIncidentTransitions = map[string]map[string]bool{
		SecurityIncidentStatusOpen: {
			SecurityIncidentStatusOpen:          true,
			SecurityIncidentStatusInvestigating: true,
		},
		SecurityIncidentStatusInvestigating: {
			SecurityIncidentStatusInvestigating: true,
			SecurityIncidentStatusContained:     true,
			SecurityIncidentStatusResolved:      true,
		},
		SecurityIncidentStatusContained: {
			SecurityIncidentStatusContained:     true,
			SecurityIncidentStatusInvestigating: true,
			SecurityIncidentStatusResolved:      true,
		},
		SecurityIncidentStatusResolved: {
			SecurityIncidentStatusResolved:      true,
			SecurityIncidentStatusInvestigating: true,
			SecurityIncidentStatusClosed:        true,
		},
		SecurityIncidentStatusClosed: {
			SecurityIncidentStatusClosed: true,
		},
	}
)

func normalizeSecurityIncidentRequest(r *SecurityIncidentRequest) {
	r.Reference = normalizeCode(r.Reference)
	r.Title = normalizeText(r.Title)
	r.Description = normalizeText(r.Description)
	r.Severity = normalizeEnum(r.Severity)
	r.Status = normalizeEnum(r.Status)
	if r.OccurredAt != nil {
		utc := r.OccurredAt.UTC()
		r.OccurredAt = &utc
	}
	if !r.DetectedAt.IsZero() {
		r.DetectedAt = r.DetectedAt.UTC()
	}
}

// validateSecurityIncidentRequest checks a normalized request. now is the
// instant supplied by the service; this function never reads the clock.
// creating forces no status check: the caller has already set OPEN.
func validateSecurityIncidentRequest(r SecurityIncidentRequest, now time.Time, creating bool) error {
	if r.Reference == "" || r.Title == "" {
		return coreerrors.BadRequest("Référence et titre de l'incident obligatoires")
	}
	if utf8.RuneCountInString(r.Reference) > securityIncidentReferenceMaxLength {
		return coreerrors.BadRequest("Référence de l'incident trop longue")
	}
	if utf8.RuneCountInString(r.Title) > securityIncidentTitleMaxLength {
		return coreerrors.BadRequest("Titre de l'incident trop long")
	}
	if !validSecurityIncidentSeverities[r.Severity] {
		return coreerrors.BadRequest("Sévérité d'incident invalide")
	}
	if !creating && !validSecurityIncidentStatuses[r.Status] {
		return coreerrors.BadRequest("Statut d'incident invalide")
	}
	if r.DetectedAt.IsZero() {
		return coreerrors.BadRequest("La date de détection est obligatoire")
	}
	if r.DetectedAt.After(now) {
		return coreerrors.BadRequest("La détection ne peut pas être postérieure à l'instant courant")
	}
	if r.OccurredAt != nil && r.OccurredAt.After(r.DetectedAt) {
		return coreerrors.BadRequest("La survenue ne peut pas être postérieure à la détection")
	}
	if !isPositiveOptionalID(r.RiskID) || !isPositiveOptionalID(r.AssetID) ||
		!isPositiveOptionalID(r.OwningServiceID) || !isPositiveOptionalID(r.OwnerProfileID) {
		return coreerrors.BadRequest("Identifiant de relation invalide")
	}
	return nil
}

func validateSecurityIncidentTransition(from, to string) error {
	if securityIncidentTransitions[from][to] {
		return nil
	}
	return coreerrors.Conflict("Transition d'incident de sécurité invalide")
}

// securityIncidentTimestamps derives the server-owned resolution instant.
// Leaving RESOLVED clears it. A later resolution records a new instant.
// closedAt is never set here: only CloseSecurityIncident writes it, and a
// non-closed status always clears it.
func securityIncidentTimestamps(from, to string, resolvedAt *time.Time, now time.Time) (*time.Time, *time.Time) {
	switch to {
	case SecurityIncidentStatusResolved:
		if from != SecurityIncidentStatusResolved || resolvedAt == nil {
			instant := now.UTC()
			resolvedAt = &instant
		}
		return resolvedAt, nil
	default:
		return nil, nil
	}
}

func normalizeSecurityIncidentFilter(f *SecurityIncidentFilter) {
	f.Search = normalizeText(f.Search)
	f.Status = normalizeEnum(f.Status)
	f.Severity = normalizeEnum(f.Severity)
}

func validateSecurityIncidentFilter(f SecurityIncidentFilter) error {
	if f.Status != "" && !validSecurityIncidentStatuses[f.Status] {
		return coreerrors.BadRequest("Statut d'incident invalide")
	}
	if f.Severity != "" && !validSecurityIncidentSeverities[f.Severity] {
		return coreerrors.BadRequest("Sévérité d'incident invalide")
	}
	return nil
}
