package grc

import (
	"encoding/json"
	"time"
)

const auditModuleGRCSecurityIncident = "GRC_SECURITY_INCIDENT"

// securityIncidentAuditSnapshot is the maximum audit allowlist. Description
// is intentionally absent: it is free text and is not part of the technical
// snapshot. Pointers are marshaled even when nil so the key set is stable.
type securityIncidentAuditSnapshot struct {
	ID              uint       `json:"id"`
	Reference       string     `json:"reference"`
	Title           string     `json:"title"`
	Severity        string     `json:"severity"`
	Status          string     `json:"status"`
	DetectedAt      time.Time  `json:"detectedAt"`
	OccurredAt      *time.Time `json:"occurredAt"`
	ResolvedAt      *time.Time `json:"resolvedAt"`
	ClosedAt        *time.Time `json:"closedAt"`
	RiskID          *uint      `json:"riskId"`
	AssetID         *uint      `json:"assetId"`
	OwningServiceID *uint      `json:"owningServiceId"`
	OwnerProfileID  *uint      `json:"ownerProfileId"`
}

func securityIncidentAuditValue(item SecurityIncident) string {
	value, err := json.Marshal(securityIncidentAuditSnapshot{
		ID:              item.ID,
		Reference:       item.Reference,
		Title:           item.Title,
		Severity:        item.Severity,
		Status:          item.Status,
		DetectedAt:      item.DetectedAt,
		OccurredAt:      item.OccurredAt,
		ResolvedAt:      item.ResolvedAt,
		ClosedAt:        item.ClosedAt,
		RiskID:          item.RiskID,
		AssetID:         item.AssetID,
		OwningServiceID: item.OwningServiceID,
		OwnerProfileID:  item.OwnerProfileID,
	})
	if err != nil {
		return ""
	}
	return string(value)
}
