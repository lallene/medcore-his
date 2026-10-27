package grc

import (
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
)

// Evidence validity — the only definition, shared by the evidence registry
// and the GRC dashboard. It is derived at read time from the status, the
// expiry and one instant; it is never stored. Exactly one state applies:
//   - INVALID: status is not VALID (DRAFT or REVOKED);
//   - EXPIRED: VALID and expires_at < now (an expiry exactly at now is not
//     expired yet, see EvidenceExpired);
//   - EXPIRING_SOON: VALID and now <= expires_at <= now + window;
//   - VALID: VALID and no expiry, or expires_at > now + window.
//
// The dashboard figures map onto these states: expired = EXPIRED,
// expiringSoon = EXPIRING_SOON, valid = VALID + EXPIRING_SOON and
// invalid = INVALID + EXPIRED (as Assessment.HasInvalidEvidence).
const (
	EvidenceValidityValid        = "VALID"
	EvidenceValidityExpiringSoon = "EXPIRING_SOON"
	EvidenceValidityExpired      = "EXPIRED"
	EvidenceValidityInvalid      = "INVALID"
)

var evidenceValidityStates = []string{
	EvidenceValidityValid, EvidenceValidityExpiringSoon, EvidenceValidityExpired, EvidenceValidityInvalid,
}

// evidenceExpiringSoonSQL mirrors the EXPIRING_SOON state; bind
// (EvidenceStatusValid, now, now + window).
const evidenceExpiringSoonSQL = "(status = ? AND expires_at IS NOT NULL AND expires_at >= ? AND expires_at <= ?)"

// EvidenceValidity classifies one evidence at now with the given window end.
func EvidenceValidity(status string, expiresAt *time.Time, now, soonLimit time.Time) string {
	switch {
	case status != EvidenceStatusValid:
		return EvidenceValidityInvalid
	case EvidenceExpired(status, expiresAt, now):
		return EvidenceValidityExpired
	case expiresAt != nil && !expiresAt.After(soonLimit):
		return EvidenceValidityExpiringSoon
	default:
		return EvidenceValidityValid
	}
}

// evidenceValidityCondition returns the SQL condition of one state, with the
// same boundaries as EvidenceValidity.
func evidenceValidityCondition(state string, now, soonLimit time.Time) (string, []any, error) {
	switch state {
	case EvidenceValidityInvalid:
		return "status <> ?", []any{EvidenceStatusValid}, nil
	case EvidenceValidityExpired:
		return evidenceExpiredSQL, []any{EvidenceStatusValid, now}, nil
	case EvidenceValidityExpiringSoon:
		return evidenceExpiringSoonSQL, []any{EvidenceStatusValid, now, soonLimit}, nil
	case EvidenceValidityValid:
		return "(status = ? AND (expires_at IS NULL OR expires_at > ?))", []any{EvidenceStatusValid, soonLimit}, nil
	default:
		return "", nil, coreerrors.BadRequest("Validité de preuve invalide")
	}
}

// evidenceSoonLimit validates the window (same bounds as the dashboard) and
// returns its end.
func evidenceSoonLimit(now time.Time, days int) (time.Time, error) {
	if err := validateDashboardWindow(days, "expiringSoonDays"); err != nil {
		return time.Time{}, err
	}
	return now.AddDate(0, 0, days), nil
}

func fillEvidenceComputed(e *Evidence, now, soonLimit time.Time) {
	e.Expired = EvidenceExpired(e.Status, e.ExpiresAt, now)
	e.Validity = EvidenceValidity(e.Status, e.ExpiresAt, now, soonLimit)
}
