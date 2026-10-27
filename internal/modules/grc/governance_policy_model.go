package grc

import (
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/organization"
	"github.com/lallene/medcore-his/backend/internal/modules/staff"
)

// GovernancePolicy is an internal governance, security or compliance policy.
// It does not replace a framework, a control, a risk or an incident.
const (
	GovernancePolicyStatusDraft     = "DRAFT"
	GovernancePolicyStatusInReview  = "IN_REVIEW"
	GovernancePolicyStatusApproved  = "APPROVED"
	GovernancePolicyStatusPublished = "PUBLISHED"
	GovernancePolicyStatusRetired   = "RETIRED"
)

type GovernancePolicy struct {
	ID          uint   `gorm:"primaryKey" json:"id"`
	Reference   string `gorm:"size:60;not null;uniqueIndex:uidx_grc_governance_policies_reference" json:"reference"`
	Title       string `gorm:"size:220;not null" json:"title"`
	Description string `gorm:"type:text;not null" json:"description"`
	Version     string `gorm:"size:40;not null" json:"version"`
	Status      string `gorm:"size:20;not null;index;check:grc_governance_policy_status_valid,status IN ('DRAFT','IN_REVIEW','APPROVED','PUBLISHED','RETIRED')" json:"status"`

	EffectiveFrom *time.Time `json:"effectiveFrom"`
	EffectiveTo   *time.Time `gorm:"check:grc_governance_policy_effective,effective_to IS NULL OR (effective_from IS NOT NULL AND effective_to >= effective_from)" json:"effectiveTo"`

	OwningServiceID   *uint                 `gorm:"index" json:"owningServiceId"`
	OwningService     *organization.Service `gorm:"foreignKey:OwningServiceID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"owningService,omitempty"`
	OwnerProfileID    *uint                 `gorm:"index" json:"ownerProfileId"`
	OwnerProfile      *staff.Profile        `gorm:"foreignKey:OwnerProfileID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"ownerProfile,omitempty"`
	ApproverProfileID *uint                 `gorm:"index" json:"approverProfileId"`
	ApproverProfile   *staff.Profile        `gorm:"foreignKey:ApproverProfileID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"approverProfile,omitempty"`

	// Server-owned. The check matches every legal status, including the
	// clearing of approved_at when an approved policy returns to review.
	ApprovedAt  *time.Time `gorm:"check:grc_governance_policy_timestamps,(status IN ('DRAFT','IN_REVIEW') AND approved_at IS NULL AND published_at IS NULL AND retired_at IS NULL) OR (status = 'APPROVED' AND approved_at IS NOT NULL AND published_at IS NULL AND retired_at IS NULL) OR (status = 'PUBLISHED' AND approved_at IS NOT NULL AND published_at IS NOT NULL AND retired_at IS NULL) OR (status = 'RETIRED' AND approved_at IS NOT NULL AND published_at IS NOT NULL AND retired_at IS NOT NULL)" json:"approvedAt"`
	PublishedAt *time.Time `json:"publishedAt"`
	RetiredAt   *time.Time `json:"retiredAt"`

	CreatedBy uint      `gorm:"not null;index" json:"createdBy"`
	UpdatedBy uint      `gorm:"not null;index" json:"updatedBy"`
	CreatedAt time.Time `gorm:"not null" json:"createdAt"`
	UpdatedAt time.Time `gorm:"not null" json:"updatedAt"`

	// Computed. The list carries the counts only; the detail also carries the links.
	Frameworks     []GovernancePolicyLink `gorm:"-" json:"frameworks,omitempty"`
	Controls       []GovernancePolicyLink `gorm:"-" json:"controls,omitempty"`
	FrameworkCount int64                  `gorm:"-" json:"frameworkCount"`
	ControlCount   int64                  `gorm:"-" json:"controlCount"`
}

func (GovernancePolicy) TableName() string { return "grc_governance_policies" }

// GovernancePolicyLink is the identity of a framework or control linked to a
// policy. It is not a second copy of those aggregates.
type GovernancePolicyLink struct {
	ID     uint   `json:"id"`
	Code   string `json:"code"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

// GovernancePolicyFramework is the policy-framework join.
// ON DELETE RESTRICT follows grc_requirement_controls: GRC registries have no
// business DELETE, so removing a parent that is still linked is rejected.
type GovernancePolicyFramework struct {
	ID          uint              `gorm:"primaryKey" json:"id"`
	PolicyID    uint              `gorm:"not null;index;uniqueIndex:uidx_grc_governance_policy_frameworks,priority:1" json:"policyId"`
	Policy      *GovernancePolicy `gorm:"foreignKey:PolicyID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	FrameworkID uint              `gorm:"not null;index;uniqueIndex:uidx_grc_governance_policy_frameworks,priority:2" json:"frameworkId"`
	Framework   *Framework        `gorm:"foreignKey:FrameworkID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	CreatedBy   uint              `gorm:"not null;index" json:"createdBy"`
	CreatedAt   time.Time         `gorm:"not null" json:"createdAt"`
}

func (GovernancePolicyFramework) TableName() string { return "grc_governance_policy_frameworks" }

// GovernancePolicyControl is the policy-control join, with the same
// RESTRICT rule as GovernancePolicyFramework.
type GovernancePolicyControl struct {
	ID        uint              `gorm:"primaryKey" json:"id"`
	PolicyID  uint              `gorm:"not null;index;uniqueIndex:uidx_grc_governance_policy_controls,priority:1" json:"policyId"`
	Policy    *GovernancePolicy `gorm:"foreignKey:PolicyID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	ControlID uint              `gorm:"not null;index;uniqueIndex:uidx_grc_governance_policy_controls,priority:2" json:"controlId"`
	Control   *Control          `gorm:"foreignKey:ControlID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	CreatedBy uint              `gorm:"not null;index" json:"createdBy"`
	CreatedAt time.Time         `gorm:"not null" json:"createdAt"`
}

func (GovernancePolicyControl) TableName() string { return "grc_governance_policy_controls" }

// GovernancePolicyRequest is the create and update document.
// FrameworkIDs and ControlIDs use a pointer so an omitted field (or JSON null)
// is distinct from an explicit empty list: omitted leaves the links unchanged,
// [] clears them. Scalar relations stay full-replacement, like the other GRC
// registries. Status is ignored on create. approvedAt, publishedAt and retiredAt
// are never accepted.
type GovernancePolicyRequest struct {
	Reference         string     `json:"reference"`
	Title             string     `json:"title"`
	Description       string     `json:"description"`
	Version           string     `json:"version"`
	Status            string     `json:"status"`
	EffectiveFrom     *time.Time `json:"effectiveFrom"`
	EffectiveTo       *time.Time `json:"effectiveTo"`
	OwningServiceID   *uint      `json:"owningServiceId"`
	OwnerProfileID    *uint      `json:"ownerProfileId"`
	ApproverProfileID *uint      `json:"approverProfileId"`
	FrameworkIDs      *[]uint    `json:"frameworkIds"`
	ControlIDs        *[]uint    `json:"controlIds"`
}

type GovernancePolicyFilter struct {
	Search            string
	Status            string
	OwningServiceID   *uint
	OwnerProfileID    *uint
	ApproverProfileID *uint
	FrameworkID       *uint
	ControlID         *uint
	Page              int
	Limit             int
}

type GovernancePolicyPage struct {
	Items      []GovernancePolicy `json:"items"`
	Page       int                `json:"page"`
	Limit      int                `json:"limit"`
	Total      int64              `json:"total"`
	TotalPages int                `json:"totalPages"`
}
