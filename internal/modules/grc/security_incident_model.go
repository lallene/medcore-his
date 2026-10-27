package grc

import (
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/organization"
	"github.com/lallene/medcore-his/backend/internal/modules/staff"
)

// SecurityIncident is a GRC information-security incident. It is not an
// ITIL support ticket and has no relation to ticketing.Ticket.
const (
	SecurityIncidentSeverityLow      = "LOW"
	SecurityIncidentSeverityMedium   = "MEDIUM"
	SecurityIncidentSeverityHigh     = "HIGH"
	SecurityIncidentSeverityCritical = "CRITICAL"

	SecurityIncidentStatusOpen          = "OPEN"
	SecurityIncidentStatusInvestigating = "INVESTIGATING"
	SecurityIncidentStatusContained     = "CONTAINED"
	SecurityIncidentStatusResolved      = "RESOLVED"
	SecurityIncidentStatusClosed        = "CLOSED"
)

type SecurityIncident struct {
	ID          uint   `gorm:"primaryKey" json:"id"`
	Reference   string `gorm:"size:60;not null;uniqueIndex:uidx_grc_security_incidents_reference" json:"reference"`
	Title       string `gorm:"size:220;not null" json:"title"`
	Description string `gorm:"type:text;not null" json:"description"`
	Severity    string `gorm:"size:20;not null;index;check:grc_security_incident_severity_valid,severity IN ('LOW','MEDIUM','HIGH','CRITICAL')" json:"severity"`
	Status      string `gorm:"size:20;not null;index;check:grc_security_incident_status_valid,status IN ('OPEN','INVESTIGATING','CONTAINED','RESOLVED','CLOSED')" json:"status"`

	DetectedAt time.Time  `gorm:"not null;index" json:"detectedAt"`
	OccurredAt *time.Time `gorm:"check:grc_security_incident_occurred,occurred_at IS NULL OR occurred_at <= detected_at" json:"occurredAt"`
	ResolvedAt *time.Time `gorm:"check:grc_security_incident_timestamps,(status = 'RESOLVED' AND resolved_at IS NOT NULL AND closed_at IS NULL) OR (status = 'CLOSED' AND resolved_at IS NOT NULL AND closed_at IS NOT NULL) OR (status NOT IN ('RESOLVED','CLOSED') AND resolved_at IS NULL AND closed_at IS NULL)" json:"resolvedAt"`
	ClosedAt   *time.Time `json:"closedAt"`

	RiskID *uint `gorm:"index" json:"riskId"`
	Risk   *Risk `gorm:"foreignKey:RiskID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"risk,omitempty"`

	AssetID *uint  `gorm:"index" json:"assetId"`
	Asset   *Asset `gorm:"foreignKey:AssetID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"asset,omitempty"`

	OwningServiceID *uint                 `gorm:"index" json:"owningServiceId"`
	OwningService   *organization.Service `gorm:"foreignKey:OwningServiceID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"owningService,omitempty"`

	OwnerProfileID *uint          `gorm:"index" json:"ownerProfileId"`
	OwnerProfile   *staff.Profile `gorm:"foreignKey:OwnerProfileID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"ownerProfile,omitempty"`

	CreatedBy uint      `gorm:"not null;index" json:"createdBy"`
	UpdatedBy uint      `gorm:"not null;index" json:"updatedBy"`
	CreatedAt time.Time `gorm:"not null" json:"createdAt"`
	UpdatedAt time.Time `gorm:"not null" json:"updatedAt"`
}

func (SecurityIncident) TableName() string { return "grc_security_incidents" }

// SecurityIncidentRequest is the full replacement document of a create or
// update. Omitted and JSON null relation ids both clear the link, which is
// the same pattern as the other GRC registries. Status, resolvedAt and
// closedAt are not accepted on create.
type SecurityIncidentRequest struct {
	Reference       string     `json:"reference"`
	Title           string     `json:"title"`
	Description     string     `json:"description"`
	Severity        string     `json:"severity"`
	Status          string     `json:"status"`
	DetectedAt      time.Time  `json:"detectedAt"`
	OccurredAt      *time.Time `json:"occurredAt"`
	RiskID          *uint      `json:"riskId"`
	AssetID         *uint      `json:"assetId"`
	OwningServiceID *uint      `json:"owningServiceId"`
	OwnerProfileID  *uint      `json:"ownerProfileId"`
}

type SecurityIncidentFilter struct {
	Search          string
	Status          string
	Severity        string
	RiskID          *uint
	AssetID         *uint
	OwningServiceID *uint
	OwnerProfileID  *uint
	Page            int
	Limit           int
}

type SecurityIncidentPage struct {
	Items      []SecurityIncident `json:"items"`
	Page       int                `json:"page"`
	Limit      int                `json:"limit"`
	Total      int64              `json:"total"`
	TotalPages int                `json:"totalPages"`
}
