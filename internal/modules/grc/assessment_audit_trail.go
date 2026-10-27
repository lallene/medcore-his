package grc

import "time"

const (
	auditModuleGRCAssessment         = "GRC_ASSESSMENT"
	auditModuleGRCAssessmentEvidence = "GRC_ASSESSMENT_EVIDENCE"
	auditModuleGRCAuditEngagement    = "GRC_AUDIT_ENGAGEMENT"
	auditModuleGRCFinding            = "GRC_FINDING"
)

// Allowlisted snapshots of the 01D entities. A status transition is an UPDATE
// whose old and new snapshots carry both statuses and the server timestamps.
// Free text (summary, description, scope, closure note), computed fields,
// server metadata and relations are never audited.

type assessmentAuditSnapshot struct {
	Reference               string     `json:"reference"`
	Title                   string     `json:"title"`
	ControlImplementationID uint       `json:"controlImplementationId"`
	Type                    string     `json:"type"`
	Status                  string     `json:"status"`
	Result                  *string    `json:"result"`
	AssessorProfileID       *uint      `json:"assessorProfileId"`
	AssessedAt              *time.Time `json:"assessedAt"`
	CompletedAt             *time.Time `json:"completedAt"`
	NextAssessmentAt        *time.Time `json:"nextAssessmentAt"`
}

type assessmentEvidenceAuditSnapshot struct {
	AssessmentID uint `json:"assessmentId"`
	EvidenceID   uint `json:"evidenceId"`
}

type auditEngagementAuditSnapshot struct {
	Reference            string     `json:"reference"`
	Title                string     `json:"title"`
	Type                 string     `json:"type"`
	Status               string     `json:"status"`
	LeadAuditorProfileID *uint      `json:"leadAuditorProfileId"`
	OwningServiceID      *uint      `json:"owningServiceId"`
	PlannedStartAt       *time.Time `json:"plannedStartAt"`
	PlannedEndAt         *time.Time `json:"plannedEndAt"`
	StartedAt            *time.Time `json:"startedAt"`
	CompletedAt          *time.Time `json:"completedAt"`
}

type findingAuditSnapshot struct {
	Reference               string     `json:"reference"`
	Title                   string     `json:"title"`
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
	ResolvedAt              *time.Time `json:"resolvedAt"`
	ClosedAt                *time.Time `json:"closedAt"`
}

func assessmentAuditValue(a Assessment) string {
	return auditJSON(assessmentAuditSnapshot{
		Reference:               a.Reference,
		Title:                   a.Title,
		ControlImplementationID: a.ControlImplementationID,
		Type:                    a.Type,
		Status:                  a.Status,
		Result:                  a.Result,
		AssessorProfileID:       a.AssessorProfileID,
		AssessedAt:              a.AssessedAt,
		CompletedAt:             a.CompletedAt,
		NextAssessmentAt:        a.NextAssessmentAt,
	})
}

func assessmentEvidenceAuditValue(link AssessmentEvidence) string {
	return auditJSON(assessmentEvidenceAuditSnapshot{
		AssessmentID: link.AssessmentID,
		EvidenceID:   link.EvidenceID,
	})
}

func auditEngagementAuditValue(a AuditEngagement) string {
	return auditJSON(auditEngagementAuditSnapshot{
		Reference:            a.Reference,
		Title:                a.Title,
		Type:                 a.Type,
		Status:               a.Status,
		LeadAuditorProfileID: a.LeadAuditorProfileID,
		OwningServiceID:      a.OwningServiceID,
		PlannedStartAt:       a.PlannedStartAt,
		PlannedEndAt:         a.PlannedEndAt,
		StartedAt:            a.StartedAt,
		CompletedAt:          a.CompletedAt,
	})
}

func findingAuditValue(f Finding) string {
	return auditJSON(findingAuditSnapshot{
		Reference:               f.Reference,
		Title:                   f.Title,
		AuditEngagementID:       f.AuditEngagementID,
		AssessmentID:            f.AssessmentID,
		Category:                f.Category,
		Severity:                f.Severity,
		Status:                  f.Status,
		ControlImplementationID: f.ControlImplementationID,
		RiskID:                  f.RiskID,
		OwnerProfileID:          f.OwnerProfileID,
		OwningServiceID:         f.OwningServiceID,
		DueAt:                   f.DueAt,
		ResolvedAt:              f.ResolvedAt,
		ClosedAt:                f.ClosedAt,
	})
}
