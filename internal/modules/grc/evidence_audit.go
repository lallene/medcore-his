package grc

import "time"

const (
	auditModuleGRCEvidence          = "GRC_EVIDENCE"
	auditModuleGRCRemediationAction = "GRC_REMEDIATION_ACTION"
)

// Snapshots are explicit allowlists: Description, ExternalReference, computed
// fields (Expired, Overdue), server metadata and relations are never audited.
// The record ID is carried by the audit event itself.

type evidenceAuditSnapshot struct {
	Reference               string     `json:"reference"`
	Title                   string     `json:"title"`
	ControlImplementationID uint       `json:"controlImplementationId"`
	Type                    string     `json:"type"`
	Status                  string     `json:"status"`
	CollectedAt             *time.Time `json:"collectedAt"`
	ValidFrom               *time.Time `json:"validFrom"`
	ExpiresAt               *time.Time `json:"expiresAt"`
	OwnerProfileID          *uint      `json:"ownerProfileId"`
	OwningServiceID         *uint      `json:"owningServiceId"`
}

type remediationActionAuditSnapshot struct {
	Reference               string     `json:"reference"`
	Title                   string     `json:"title"`
	ControlImplementationID *uint      `json:"controlImplementationId"`
	RiskID                  *uint      `json:"riskId"`
	FindingID               *uint      `json:"findingId"`
	AssigneeProfileID       *uint      `json:"assigneeProfileId"`
	OwningServiceID         *uint      `json:"owningServiceId"`
	Priority                string     `json:"priority"`
	Status                  string     `json:"status"`
	DueAt                   *time.Time `json:"dueAt"`
	CompletedAt             *time.Time `json:"completedAt"`
}

func evidenceAuditValue(evidence Evidence) string {
	return auditJSON(evidenceAuditSnapshot{
		Reference:               evidence.Reference,
		Title:                   evidence.Title,
		ControlImplementationID: evidence.ControlImplementationID,
		Type:                    evidence.Type,
		Status:                  evidence.Status,
		CollectedAt:             evidence.CollectedAt,
		ValidFrom:               evidence.ValidFrom,
		ExpiresAt:               evidence.ExpiresAt,
		OwnerProfileID:          evidence.OwnerProfileID,
		OwningServiceID:         evidence.OwningServiceID,
	})
}

func remediationActionAuditValue(action RemediationAction) string {
	return auditJSON(remediationActionAuditSnapshot{
		Reference:               action.Reference,
		Title:                   action.Title,
		ControlImplementationID: action.ControlImplementationID,
		RiskID:                  action.RiskID,
		FindingID:               action.FindingID,
		AssigneeProfileID:       action.AssigneeProfileID,
		OwningServiceID:         action.OwningServiceID,
		Priority:                action.Priority,
		Status:                  action.Status,
		DueAt:                   action.DueAt,
		CompletedAt:             action.CompletedAt,
	})
}
