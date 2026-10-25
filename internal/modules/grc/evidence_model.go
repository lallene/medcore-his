package grc

import (
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/organization"
	"github.com/lallene/medcore-his/backend/internal/modules/staff"
)

const (
	EvidenceTypeDocument      = "DOCUMENT"
	EvidenceTypeConfiguration = "CONFIGURATION"
	EvidenceTypeLog           = "LOG"
	EvidenceTypeScreenshot    = "SCREENSHOT"
	EvidenceTypeReport        = "REPORT"
	EvidenceTypeTestResult    = "TEST_RESULT"
	EvidenceTypeCertificate   = "CERTIFICATE"
	EvidenceTypeOther         = "OTHER"

	// Expiry is derived from ExpiresAt at read time, never stored as a status.
	EvidenceStatusDraft   = "DRAFT"
	EvidenceStatusValid   = "VALID"
	EvidenceStatusRevoked = "REVOKED"

	RemediationActionPriorityLow      = "LOW"
	RemediationActionPriorityMedium   = "MEDIUM"
	RemediationActionPriorityHigh     = "HIGH"
	RemediationActionPriorityCritical = "CRITICAL"

	RemediationActionStatusOpen       = "OPEN"
	RemediationActionStatusInProgress = "IN_PROGRESS"
	RemediationActionStatusBlocked    = "BLOCKED"
	RemediationActionStatusDone       = "DONE"
	RemediationActionStatusCancelled  = "CANCELLED"
)

// Evidence holds the metadata of a compliance proof. It never stores the
// proof itself (files, secrets, raw logs or configuration).
type Evidence struct {
	ID                      uint                   `gorm:"primaryKey" json:"id"`
	Reference               string                 `gorm:"size:60;not null;uniqueIndex" json:"reference"`
	Title                   string                 `gorm:"size:220;not null" json:"title"`
	Description             string                 `gorm:"type:text" json:"description"`
	ControlImplementationID uint                   `gorm:"not null;index" json:"controlImplementationId"`
	ControlImplementation   *ControlImplementation `gorm:"foreignKey:ControlImplementationID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"controlImplementation,omitempty"`
	Type                    string                 `gorm:"size:30;not null;index;check:grc_evidence_type_valid,type IN ('DOCUMENT','CONFIGURATION','LOG','SCREENSHOT','REPORT','TEST_RESULT','CERTIFICATE','OTHER')" json:"type"`
	Status                  string                 `gorm:"size:20;not null;index;check:grc_evidence_status_valid,status IN ('DRAFT','VALID','REVOKED')" json:"status"`
	ExternalReference       string                 `gorm:"size:500" json:"externalReference"`
	CollectedAt             *time.Time             `json:"collectedAt"`
	ValidFrom               *time.Time             `json:"validFrom"`
	ExpiresAt               *time.Time             `gorm:"index;check:grc_evidence_validity_range,expires_at IS NULL OR valid_from IS NULL OR expires_at >= valid_from" json:"expiresAt"`
	OwnerProfileID          *uint                  `gorm:"index" json:"ownerProfileId"`
	OwnerProfile            *staff.Profile         `gorm:"foreignKey:OwnerProfileID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"ownerProfile,omitempty"`
	OwningServiceID         *uint                  `gorm:"index" json:"owningServiceId"`
	OwningService           *organization.Service  `gorm:"foreignKey:OwningServiceID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"owningService,omitempty"`
	CreatedBy               uint                   `gorm:"not null;index" json:"createdBy"`
	UpdatedBy               uint                   `gorm:"not null;index" json:"updatedBy"`
	CreatedAt               time.Time              `gorm:"not null" json:"createdAt"`
	UpdatedAt               time.Time              `gorm:"not null" json:"updatedAt"`

	Expired bool `gorm:"-" json:"expired"`
}

func (Evidence) TableName() string { return "grc_evidence" }

type RemediationAction struct {
	ID                      uint                   `gorm:"primaryKey" json:"id"`
	Reference               string                 `gorm:"size:60;not null;uniqueIndex" json:"reference"`
	Title                   string                 `gorm:"size:220;not null" json:"title"`
	Description             string                 `gorm:"type:text" json:"description"`
	ControlImplementationID *uint                  `gorm:"index" json:"controlImplementationId"`
	ControlImplementation   *ControlImplementation `gorm:"foreignKey:ControlImplementationID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"controlImplementation,omitempty"`
	RiskID                  *uint                  `gorm:"index;check:grc_remediation_action_target_present,control_implementation_id IS NOT NULL OR risk_id IS NOT NULL" json:"riskId"`
	Risk                    *Risk                  `gorm:"foreignKey:RiskID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"risk,omitempty"`
	AssigneeProfileID       *uint                  `gorm:"index" json:"assigneeProfileId"`
	AssigneeProfile         *staff.Profile         `gorm:"foreignKey:AssigneeProfileID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"assigneeProfile,omitempty"`
	OwningServiceID         *uint                  `gorm:"index" json:"owningServiceId"`
	OwningService           *organization.Service  `gorm:"foreignKey:OwningServiceID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"owningService,omitempty"`
	Priority                string                 `gorm:"size:20;not null;index;check:grc_remediation_action_priority_valid,priority IN ('LOW','MEDIUM','HIGH','CRITICAL')" json:"priority"`
	Status                  string                 `gorm:"size:20;not null;index;check:grc_remediation_action_status_valid,status IN ('OPEN','IN_PROGRESS','BLOCKED','DONE','CANCELLED')" json:"status"`
	DueAt                   *time.Time             `gorm:"index" json:"dueAt"`
	CompletedAt             *time.Time             `gorm:"check:grc_remediation_action_completion,(status = 'DONE') = (completed_at IS NOT NULL)" json:"completedAt"`
	CreatedBy               uint                   `gorm:"not null;index" json:"createdBy"`
	UpdatedBy               uint                   `gorm:"not null;index" json:"updatedBy"`
	CreatedAt               time.Time              `gorm:"not null" json:"createdAt"`
	UpdatedAt               time.Time              `gorm:"not null" json:"updatedAt"`

	Overdue bool `gorm:"-" json:"overdue"`
}

func (RemediationAction) TableName() string { return "grc_remediation_actions" }
