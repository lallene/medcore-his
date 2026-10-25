package grc

import (
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/organization"
	"github.com/lallene/medcore-his/backend/internal/modules/staff"
)

const (
	FrameworkStatusDraft    = "DRAFT"
	FrameworkStatusActive   = "ACTIVE"
	FrameworkStatusArchived = "ARCHIVED"

	RequirementStatusActive   = "ACTIVE"
	RequirementStatusInactive = "INACTIVE"

	ControlStatusDraft    = "DRAFT"
	ControlStatusActive   = "ACTIVE"
	ControlStatusInactive = "INACTIVE"
	ControlStatusRetired  = "RETIRED"

	ControlImplementationStatusPlanned        = "PLANNED"
	ControlImplementationStatusImplemented    = "IMPLEMENTED"
	ControlImplementationStatusPartial        = "PARTIAL"
	ControlImplementationStatusNotImplemented = "NOT_IMPLEMENTED"
	ControlImplementationStatusNotApplicable  = "NOT_APPLICABLE"
)

type Framework struct {
	ID          uint       `gorm:"primaryKey" json:"id"`
	Code        string     `gorm:"size:60;not null;uniqueIndex:idx_grc_frameworks_code_version,priority:1" json:"code"`
	Name        string     `gorm:"size:180;not null" json:"name"`
	Version     string     `gorm:"size:60;not null;uniqueIndex:idx_grc_frameworks_code_version,priority:2" json:"version"`
	Description string     `gorm:"type:text" json:"description"`
	Source      string     `gorm:"size:500" json:"source"`
	Status      string     `gorm:"size:20;not null;index;check:grc_framework_status_valid,status IN ('DRAFT','ACTIVE','ARCHIVED')" json:"status"`
	PublishedAt *time.Time `json:"publishedAt"`
	CreatedBy   uint       `gorm:"not null;index" json:"createdBy"`
	UpdatedBy   uint       `gorm:"not null;index" json:"updatedBy"`
	CreatedAt   time.Time  `gorm:"not null" json:"createdAt"`
	UpdatedAt   time.Time  `gorm:"not null" json:"updatedAt"`
}

func (Framework) TableName() string { return "grc_frameworks" }

type Requirement struct {
	ID                  uint         `gorm:"primaryKey" json:"id"`
	FrameworkID         uint         `gorm:"not null;index;uniqueIndex:idx_grc_requirements_framework_code,priority:1" json:"frameworkId"`
	Framework           *Framework   `gorm:"foreignKey:FrameworkID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"framework,omitempty"`
	Code                string       `gorm:"size:100;not null;uniqueIndex:idx_grc_requirements_framework_code,priority:2" json:"code"`
	Title               string       `gorm:"size:220;not null" json:"title"`
	Description         string       `gorm:"type:text" json:"description"`
	ParentRequirementID *uint        `gorm:"index" json:"parentRequirementId"`
	ParentRequirement   *Requirement `gorm:"foreignKey:ParentRequirementID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"parentRequirement,omitempty"`
	Status              string       `gorm:"size:20;not null;index;check:grc_requirement_status_valid,status IN ('ACTIVE','INACTIVE')" json:"status"`
	CreatedBy           uint         `gorm:"not null;index" json:"createdBy"`
	UpdatedBy           uint         `gorm:"not null;index" json:"updatedBy"`
	CreatedAt           time.Time    `gorm:"not null" json:"createdAt"`
	UpdatedAt           time.Time    `gorm:"not null" json:"updatedAt"`
}

func (Requirement) TableName() string { return "grc_requirements" }

type Control struct {
	ID              uint                  `gorm:"primaryKey" json:"id"`
	Code            string                `gorm:"size:60;not null;uniqueIndex" json:"code"`
	Name            string                `gorm:"size:220;not null" json:"name"`
	Description     string                `gorm:"type:text" json:"description"`
	Category        string                `gorm:"size:100" json:"category"`
	OwnerProfileID  *uint                 `gorm:"index" json:"ownerProfileId"`
	OwnerProfile    *staff.Profile        `gorm:"foreignKey:OwnerProfileID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"ownerProfile,omitempty"`
	OwningServiceID *uint                 `gorm:"index" json:"owningServiceId"`
	OwningService   *organization.Service `gorm:"foreignKey:OwningServiceID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"owningService,omitempty"`
	Status          string                `gorm:"size:20;not null;index;check:grc_control_status_valid,status IN ('DRAFT','ACTIVE','INACTIVE','RETIRED')" json:"status"`
	CreatedBy       uint                  `gorm:"not null;index" json:"createdBy"`
	UpdatedBy       uint                  `gorm:"not null;index" json:"updatedBy"`
	CreatedAt       time.Time             `gorm:"not null" json:"createdAt"`
	UpdatedAt       time.Time             `gorm:"not null" json:"updatedAt"`
}

func (Control) TableName() string { return "grc_controls" }

type RequirementControl struct {
	ID            uint         `gorm:"primaryKey" json:"id"`
	RequirementID uint         `gorm:"not null;index;uniqueIndex:idx_grc_requirement_controls_requirement_control,priority:1" json:"requirementId"`
	Requirement   *Requirement `gorm:"foreignKey:RequirementID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"requirement,omitempty"`
	ControlID     uint         `gorm:"not null;index;uniqueIndex:idx_grc_requirement_controls_requirement_control,priority:2" json:"controlId"`
	Control       *Control     `gorm:"foreignKey:ControlID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"control,omitempty"`
	CreatedBy     uint         `gorm:"not null;index" json:"createdBy"`
	CreatedAt     time.Time    `gorm:"not null" json:"createdAt"`
}

func (RequirementControl) TableName() string { return "grc_requirement_controls" }

type ControlRisk struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	ControlID uint      `gorm:"not null;index;uniqueIndex:idx_grc_control_risks_control_risk,priority:1" json:"controlId"`
	Control   *Control  `gorm:"foreignKey:ControlID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"control,omitempty"`
	RiskID    uint      `gorm:"not null;index;uniqueIndex:idx_grc_control_risks_control_risk,priority:2" json:"riskId"`
	Risk      *Risk     `gorm:"foreignKey:RiskID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"risk,omitempty"`
	CreatedBy uint      `gorm:"not null;index" json:"createdBy"`
	CreatedAt time.Time `gorm:"not null" json:"createdAt"`
}

func (ControlRisk) TableName() string { return "grc_control_risks" }

type ControlImplementation struct {
	ID                    uint                  `gorm:"primaryKey" json:"id"`
	ControlID             uint                  `gorm:"not null;index;uniqueIndex:idx_grc_control_implementations_control_asset,priority:1" json:"controlId"`
	Control               *Control              `gorm:"foreignKey:ControlID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"control,omitempty"`
	AssetID               uint                  `gorm:"not null;index;uniqueIndex:idx_grc_control_implementations_control_asset,priority:2" json:"assetId"`
	Asset                 *Asset                `gorm:"foreignKey:AssetID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"asset,omitempty"`
	OwningServiceID       *uint                 `gorm:"index" json:"owningServiceId"`
	OwningService         *organization.Service `gorm:"foreignKey:OwningServiceID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"owningService,omitempty"`
	OwnerProfileID        *uint                 `gorm:"index" json:"ownerProfileId"`
	OwnerProfile          *staff.Profile        `gorm:"foreignKey:OwnerProfileID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"ownerProfile,omitempty"`
	Status                string                `gorm:"size:24;not null;index;check:grc_control_implementation_status_valid,status IN ('PLANNED','IMPLEMENTED','PARTIAL','NOT_IMPLEMENTED','NOT_APPLICABLE')" json:"status"`
	ImplementationDetails string                `gorm:"type:text" json:"implementationDetails"`
	ImplementedAt         *time.Time            `json:"implementedAt"`
	LastReviewedAt        *time.Time            `json:"lastReviewedAt"`
	CreatedBy             uint                  `gorm:"not null;index" json:"createdBy"`
	UpdatedBy             uint                  `gorm:"not null;index" json:"updatedBy"`
	CreatedAt             time.Time             `gorm:"not null" json:"createdAt"`
	UpdatedAt             time.Time             `gorm:"not null" json:"updatedAt"`
}

func (ControlImplementation) TableName() string { return "grc_control_implementations" }
