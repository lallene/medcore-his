package grc

import "time"

// Minimal bodies of the dedicated transition routes. They only carry the
// business data a sensitive transition may change, so a holder of the
// transition permission cannot edit other fields guarded by *.update.

// AssessmentCompleteRequest completes a DRAFT assessment; omitted values keep
// the current draft value.
type AssessmentCompleteRequest struct {
	Result            *string    `json:"result"`
	AssessorProfileID *uint      `json:"assessorProfileId"`
	AssessedAt        *time.Time `json:"assessedAt"`
	NextAssessmentAt  *time.Time `json:"nextAssessmentAt"`
	Summary           *string    `json:"summary"`
}

// FindingAcceptRequest accepts a finding without remediation; the
// justification is mandatory (checked by the pure validation).
type FindingAcceptRequest struct {
	ClosureNote string `json:"closureNote"`
}
