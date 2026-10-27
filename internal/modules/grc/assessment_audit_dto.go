package grc

import "time"

// Server-owned fields (ID, CreatedBy/UpdatedBy, timestamps, CompletedAt,
// ResolvedAt, ClosedAt and computed fields) are never accepted from clients.

type AssessmentRequest struct {
	Reference               string     `json:"reference"`
	Title                   string     `json:"title"`
	ControlImplementationID uint       `json:"controlImplementationId"`
	Type                    string     `json:"type"`
	Status                  string     `json:"status"`
	Result                  *string    `json:"result"`
	AssessorProfileID       *uint      `json:"assessorProfileId"`
	AssessedAt              *time.Time `json:"assessedAt"`
	NextAssessmentAt        *time.Time `json:"nextAssessmentAt"`
	Summary                 string     `json:"summary"`
}

type AssessmentFilter struct {
	Search                  string
	ControlImplementationID *uint
	// EvidenceID keeps the assessments that examined this evidence
	// (grc_assessment_evidence).
	EvidenceID         *uint
	AssessorProfileID  *uint
	Type               string
	Status             string
	Result             string
	ReassessmentDue    *bool
	HasInvalidEvidence *bool
	AssessedAfter      *time.Time
	AssessedBefore     *time.Time
	Page               int
	Limit              int
}

type AssessmentPage struct {
	Items      []Assessment `json:"items"`
	Page       int          `json:"page"`
	Limit      int          `json:"limit"`
	Total      int64        `json:"total"`
	TotalPages int          `json:"totalPages"`
}

// AssessmentEvidenceRequest links an evidence to the assessment of the route.
type AssessmentEvidenceRequest struct {
	EvidenceID uint `json:"evidenceId"`
}

type AuditEngagementRequest struct {
	Reference            string     `json:"reference"`
	Title                string     `json:"title"`
	Description          string     `json:"description"`
	Scope                string     `json:"scope"`
	Type                 string     `json:"type"`
	Status               string     `json:"status"`
	LeadAuditorProfileID *uint      `json:"leadAuditorProfileId"`
	OwningServiceID      *uint      `json:"owningServiceId"`
	PlannedStartAt       *time.Time `json:"plannedStartAt"`
	PlannedEndAt         *time.Time `json:"plannedEndAt"`
	StartedAt            *time.Time `json:"startedAt"`
}

type AuditEngagementFilter struct {
	Search               string
	Type                 string
	Status               string
	LeadAuditorProfileID *uint
	OwningServiceID      *uint
	PlannedStartAfter    *time.Time
	PlannedStartBefore   *time.Time
	Page                 int
	Limit                int
}

type AuditEngagementPage struct {
	Items      []AuditEngagement `json:"items"`
	Page       int               `json:"page"`
	Limit      int               `json:"limit"`
	Total      int64             `json:"total"`
	TotalPages int               `json:"totalPages"`
}

type FindingRequest struct {
	Reference               string     `json:"reference"`
	Title                   string     `json:"title"`
	Description             string     `json:"description"`
	AuditEngagementID       *uint      `json:"auditEngagementId"`
	AssessmentID            *uint      `json:"assessmentId"`
	Category                string     `json:"category"`
	Severity                string     `json:"severity"`
	Status                  string     `json:"status"`
	ControlImplementationID *uint      `json:"controlImplementationId"`
	RiskID                  *uint      `json:"riskId"`
	OwnerProfileID          *uint      `json:"ownerProfileId"`
	OwningServiceID         *uint      `json:"owningServiceId"`
	DueAt                   *time.Time `json:"dueAt"`
	ClosureNote             string     `json:"closureNote"`
}

type FindingFilter struct {
	Search                  string
	AuditEngagementID       *uint
	AssessmentID            *uint
	ControlImplementationID *uint
	RiskID                  *uint
	OwnerProfileID          *uint
	OwningServiceID         *uint
	Category                string
	Severity                string
	Status                  string
	Overdue                 *bool
	DueBefore               *time.Time
	Page                    int
	Limit                   int
}

type FindingPage struct {
	Items      []Finding `json:"items"`
	Page       int       `json:"page"`
	Limit      int       `json:"limit"`
	Total      int64     `json:"total"`
	TotalPages int       `json:"totalPages"`
}
