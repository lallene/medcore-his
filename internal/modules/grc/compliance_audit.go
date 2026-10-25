package grc

import (
	"encoding/json"
	"time"
)

const (
	auditModuleGRCFramework             = "GRC_FRAMEWORK"
	auditModuleGRCRequirement           = "GRC_REQUIREMENT"
	auditModuleGRCControl               = "GRC_CONTROL"
	auditModuleGRCControlImplementation = "GRC_CONTROL_IMPLEMENTATION"
	auditModuleGRCRequirementControl    = "GRC_REQUIREMENT_CONTROL"
	auditModuleGRCControlRisk           = "GRC_CONTROL_RISK"

	auditActionLink   = "LINK"
	auditActionUnlink = "UNLINK"
)

// Snapshots list the audited fields explicitly: free-text fields
// (descriptions, sources, implementation details) are never included.

type frameworkAuditSnapshot struct {
	ID          uint       `json:"id"`
	Code        string     `json:"code"`
	Name        string     `json:"name"`
	Version     string     `json:"version"`
	Status      string     `json:"status"`
	PublishedAt *time.Time `json:"publishedAt,omitempty"`
}

type requirementAuditSnapshot struct {
	ID                  uint   `json:"id"`
	FrameworkID         uint   `json:"frameworkId"`
	Code                string `json:"code"`
	Title               string `json:"title"`
	ParentRequirementID *uint  `json:"parentRequirementId,omitempty"`
	Status              string `json:"status"`
}

type controlAuditSnapshot struct {
	ID              uint   `json:"id"`
	Code            string `json:"code"`
	Name            string `json:"name"`
	Category        string `json:"category"`
	OwnerProfileID  *uint  `json:"ownerProfileId,omitempty"`
	OwningServiceID *uint  `json:"owningServiceId,omitempty"`
	Status          string `json:"status"`
}

type controlImplementationAuditSnapshot struct {
	ID              uint       `json:"id"`
	ControlID       uint       `json:"controlId"`
	AssetID         uint       `json:"assetId"`
	OwningServiceID *uint      `json:"owningServiceId,omitempty"`
	OwnerProfileID  *uint      `json:"ownerProfileId,omitempty"`
	Status          string     `json:"status"`
	ImplementedAt   *time.Time `json:"implementedAt,omitempty"`
	LastReviewedAt  *time.Time `json:"lastReviewedAt,omitempty"`
}

type requirementControlAuditSnapshot struct {
	ID            uint `json:"id"`
	RequirementID uint `json:"requirementId"`
	ControlID     uint `json:"controlId"`
}

type controlRiskAuditSnapshot struct {
	ID        uint `json:"id"`
	ControlID uint `json:"controlId"`
	RiskID    uint `json:"riskId"`
}

func auditJSON(snapshot any) string {
	value, err := json.Marshal(snapshot)
	if err != nil {
		return ""
	}
	return string(value)
}

func frameworkAuditValue(framework Framework) string {
	return auditJSON(frameworkAuditSnapshot{
		ID:          framework.ID,
		Code:        framework.Code,
		Name:        framework.Name,
		Version:     framework.Version,
		Status:      framework.Status,
		PublishedAt: framework.PublishedAt,
	})
}

func requirementAuditValue(requirement Requirement) string {
	return auditJSON(requirementAuditSnapshot{
		ID:                  requirement.ID,
		FrameworkID:         requirement.FrameworkID,
		Code:                requirement.Code,
		Title:               requirement.Title,
		ParentRequirementID: requirement.ParentRequirementID,
		Status:              requirement.Status,
	})
}

func controlAuditValue(control Control) string {
	return auditJSON(controlAuditSnapshot{
		ID:              control.ID,
		Code:            control.Code,
		Name:            control.Name,
		Category:        control.Category,
		OwnerProfileID:  control.OwnerProfileID,
		OwningServiceID: control.OwningServiceID,
		Status:          control.Status,
	})
}

func controlImplementationAuditValue(implementation ControlImplementation) string {
	return auditJSON(controlImplementationAuditSnapshot{
		ID:              implementation.ID,
		ControlID:       implementation.ControlID,
		AssetID:         implementation.AssetID,
		OwningServiceID: implementation.OwningServiceID,
		OwnerProfileID:  implementation.OwnerProfileID,
		Status:          implementation.Status,
		ImplementedAt:   implementation.ImplementedAt,
		LastReviewedAt:  implementation.LastReviewedAt,
	})
}

func requirementControlAuditValue(link RequirementControl) string {
	return auditJSON(requirementControlAuditSnapshot{
		ID:            link.ID,
		RequirementID: link.RequirementID,
		ControlID:     link.ControlID,
	})
}

func controlRiskAuditValue(link ControlRisk) string {
	return auditJSON(controlRiskAuditSnapshot{
		ID:        link.ID,
		ControlID: link.ControlID,
		RiskID:    link.RiskID,
	})
}
