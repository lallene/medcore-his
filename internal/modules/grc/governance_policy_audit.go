package grc

import (
	"encoding/json"
	"time"
)

const auditModuleGRCGovernancePolicy = "GRC_GOVERNANCE_POLICY"

// governancePolicyAuditSnapshot is the maximum allowlist. Description is
// free text and is excluded. Relation ids are sorted so the snapshot does not
// depend on insertion order. Nil slices are written as empty arrays.
type governancePolicyAuditSnapshot struct {
	ID                uint       `json:"id"`
	Reference         string     `json:"reference"`
	Title             string     `json:"title"`
	Version           string     `json:"version"`
	Status            string     `json:"status"`
	EffectiveFrom     *time.Time `json:"effectiveFrom"`
	EffectiveTo       *time.Time `json:"effectiveTo"`
	OwningServiceID   *uint      `json:"owningServiceId"`
	OwnerProfileID    *uint      `json:"ownerProfileId"`
	ApproverProfileID *uint      `json:"approverProfileId"`
	ApprovedAt        *time.Time `json:"approvedAt"`
	PublishedAt       *time.Time `json:"publishedAt"`
	RetiredAt         *time.Time `json:"retiredAt"`
	FrameworkIDs      []uint     `json:"frameworkIds"`
	ControlIDs        []uint     `json:"controlIds"`
}

func governancePolicyAuditValue(item GovernancePolicy, frameworkIDs, controlIDs []uint) string {
	if frameworkIDs == nil {
		frameworkIDs = []uint{}
	}
	if controlIDs == nil {
		controlIDs = []uint{}
	}
	value, err := json.Marshal(governancePolicyAuditSnapshot{
		ID:                item.ID,
		Reference:         item.Reference,
		Title:             item.Title,
		Version:           item.Version,
		Status:            item.Status,
		EffectiveFrom:     item.EffectiveFrom,
		EffectiveTo:       item.EffectiveTo,
		OwningServiceID:   item.OwningServiceID,
		OwnerProfileID:    item.OwnerProfileID,
		ApproverProfileID: item.ApproverProfileID,
		ApprovedAt:        item.ApprovedAt,
		PublishedAt:       item.PublishedAt,
		RetiredAt:         item.RetiredAt,
		FrameworkIDs:      frameworkIDs,
		ControlIDs:        controlIDs,
	})
	if err != nil {
		return ""
	}
	return string(value)
}
