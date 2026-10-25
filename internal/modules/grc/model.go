package grc

import (
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/organization"
	"github.com/lallene/medcore-his/backend/internal/modules/staff"
)

const (
	AssetTypeApplication     = "APPLICATION"
	AssetTypeInfrastructure  = "INFRASTRUCTURE"
	AssetTypeServer          = "SERVER"
	AssetTypeDatabase        = "DATABASE"
	AssetTypeNetwork         = "NETWORK"
	AssetTypeData            = "DATA"
	AssetTypeBusinessService = "BUSINESS_SERVICE"
	AssetTypeVendor          = "VENDOR"
	AssetTypeProcess         = "PROCESS"
	AssetTypeSite            = "SITE"

	AssetCriticalityLow      = "LOW"
	AssetCriticalityMedium   = "MEDIUM"
	AssetCriticalityHigh     = "HIGH"
	AssetCriticalityCritical = "CRITICAL"

	AssetStatusActive   = "ACTIVE"
	AssetStatusInactive = "INACTIVE"
	AssetStatusRetired  = "RETIRED"

	RiskTreatmentReduce   = "REDUCE"
	RiskTreatmentAccept   = "ACCEPT"
	RiskTreatmentAvoid    = "AVOID"
	RiskTreatmentTransfer = "TRANSFER"

	RiskStatusDraft       = "DRAFT"
	RiskStatusOpen        = "OPEN"
	RiskStatusInTreatment = "IN_TREATMENT"
	RiskStatusAccepted    = "ACCEPTED"
	RiskStatusClosed      = "CLOSED"
)

type Asset struct {
	ID              uint                  `gorm:"primaryKey" json:"id"`
	Code            string                `gorm:"size:60;not null;uniqueIndex" json:"code"`
	Name            string                `gorm:"size:180;not null" json:"name"`
	Type            string                `gorm:"size:40;not null;index;check:grc_asset_type_valid,type IN ('APPLICATION','INFRASTRUCTURE','SERVER','DATABASE','NETWORK','DATA','BUSINESS_SERVICE','VENDOR','PROCESS','SITE')" json:"type"`
	Description     string                `gorm:"type:text" json:"description"`
	Criticality     string                `gorm:"size:20;not null;index;check:grc_asset_criticality_valid,criticality IN ('LOW','MEDIUM','HIGH','CRITICAL')" json:"criticality"`
	OwningServiceID *uint                 `gorm:"index" json:"owningServiceId"`
	OwningService   *organization.Service `gorm:"foreignKey:OwningServiceID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"owningService,omitempty"`
	OwnerProfileID  *uint                 `gorm:"index" json:"ownerProfileId"`
	OwnerProfile    *staff.Profile        `gorm:"foreignKey:OwnerProfileID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"ownerProfile,omitempty"`
	Status          string                `gorm:"size:20;not null;index;check:grc_asset_status_valid,status IN ('ACTIVE','INACTIVE','RETIRED')" json:"status"`
	CreatedBy       uint                  `gorm:"not null;index" json:"createdBy"`
	UpdatedBy       uint                  `gorm:"not null;index" json:"updatedBy"`
	CreatedAt       time.Time             `gorm:"not null" json:"createdAt"`
	UpdatedAt       time.Time             `gorm:"not null" json:"updatedAt"`
}

func (Asset) TableName() string { return "grc_assets" }

type Risk struct {
	ID                 uint                  `gorm:"primaryKey" json:"id"`
	Reference          string                `gorm:"size:60;not null;uniqueIndex" json:"reference"`
	Title              string                `gorm:"size:220;not null" json:"title"`
	Description        string                `gorm:"type:text;not null" json:"description"`
	AssetID            *uint                 `gorm:"index" json:"assetId"`
	Asset              *Asset                `gorm:"foreignKey:AssetID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"asset,omitempty"`
	OwningServiceID    *uint                 `gorm:"index" json:"owningServiceId"`
	OwningService      *organization.Service `gorm:"foreignKey:OwningServiceID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"owningService,omitempty"`
	OwnerProfileID     *uint                 `gorm:"index" json:"ownerProfileId"`
	OwnerProfile       *staff.Profile        `gorm:"foreignKey:OwnerProfileID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"ownerProfile,omitempty"`
	ApproverProfileID  *uint                 `gorm:"index" json:"approverProfileId"`
	ApproverProfile    *staff.Profile        `gorm:"foreignKey:ApproverProfileID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"approverProfile,omitempty"`
	Threat             string                `gorm:"type:text" json:"threat"`
	Vulnerability      string                `gorm:"type:text" json:"vulnerability"`
	Likelihood         int                   `gorm:"not null;check:grc_risk_likelihood_valid,likelihood BETWEEN 1 AND 5" json:"likelihood"`
	Impact             int                   `gorm:"not null;check:grc_risk_impact_valid,impact BETWEEN 1 AND 5" json:"impact"`
	InherentScore      int                   `gorm:"not null;check:grc_risk_inherent_score_valid,inherent_score BETWEEN 1 AND 25" json:"inherentScore"`
	ResidualLikelihood int                   `gorm:"not null;check:grc_risk_residual_likelihood_valid,residual_likelihood BETWEEN 1 AND 5" json:"residualLikelihood"`
	ResidualImpact     int                   `gorm:"not null;check:grc_risk_residual_impact_valid,residual_impact BETWEEN 1 AND 5" json:"residualImpact"`
	ResidualScore      int                   `gorm:"not null;check:grc_risk_residual_score_valid,residual_score BETWEEN 1 AND 25" json:"residualScore"`
	Treatment          string                `gorm:"size:20;not null;index;check:grc_risk_treatment_valid,treatment IN ('REDUCE','ACCEPT','AVOID','TRANSFER')" json:"treatment"`
	TreatmentDueAt     *time.Time            `gorm:"index" json:"treatmentDueAt"`
	Status             string                `gorm:"size:24;not null;index;check:grc_risk_status_valid,status IN ('DRAFT','OPEN','IN_TREATMENT','ACCEPTED','CLOSED')" json:"status"`
	CreatedBy          uint                  `gorm:"not null;index" json:"createdBy"`
	UpdatedBy          uint                  `gorm:"not null;index" json:"updatedBy"`
	CreatedAt          time.Time             `gorm:"not null" json:"createdAt"`
	UpdatedAt          time.Time             `gorm:"not null" json:"updatedAt"`
}

func (Risk) TableName() string { return "grc_risks" }
