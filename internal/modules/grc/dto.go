package grc

import "time"

type AssetRequest struct {
	Code            string `json:"code"`
	Name            string `json:"name"`
	Type            string `json:"type"`
	Description     string `json:"description"`
	Criticality     string `json:"criticality"`
	OwningServiceID *uint  `json:"owningServiceId"`
	OwnerProfileID  *uint  `json:"ownerProfileId"`
	Status          string `json:"status"`
}

type RiskRequest struct {
	Reference          string     `json:"reference"`
	Title              string     `json:"title"`
	Description        string     `json:"description"`
	AssetID            *uint      `json:"assetId"`
	OwningServiceID    *uint      `json:"owningServiceId"`
	OwnerProfileID     *uint      `json:"ownerProfileId"`
	ApproverProfileID  *uint      `json:"approverProfileId"`
	Threat             string     `json:"threat"`
	Vulnerability      string     `json:"vulnerability"`
	Likelihood         int        `json:"likelihood"`
	Impact             int        `json:"impact"`
	ResidualLikelihood int        `json:"residualLikelihood"`
	ResidualImpact     int        `json:"residualImpact"`
	Treatment          string     `json:"treatment"`
	TreatmentDueAt     *time.Time `json:"treatmentDueAt"`
	Status             string     `json:"status"`
}

type AssetFilter struct {
	Search         string
	Type           string
	Criticality    string
	Status         string
	ServiceID      *uint
	OwnerProfileID *uint
	Page           int
	Limit          int
}

type AssetPage struct {
	Items      []Asset `json:"items"`
	Page       int     `json:"page"`
	Limit      int     `json:"limit"`
	Total      int64   `json:"total"`
	TotalPages int     `json:"totalPages"`
}

type RiskFilter struct {
	Search            string
	Status            string
	Treatment         string
	AssetID           *uint
	ServiceID         *uint
	OwnerProfileID    *uint
	ApproverProfileID *uint
	// ControlID keeps the risks linked to a control (grc_control_risks).
	ControlID     *uint
	InherentLevel string
	ResidualLevel string
	Page          int
	Limit         int
}

type RiskPage struct {
	Items      []Risk `json:"items"`
	Page       int    `json:"page"`
	Limit      int    `json:"limit"`
	Total      int64  `json:"total"`
	TotalPages int    `json:"totalPages"`
}

type FrameworkRequest struct {
	Code        string     `json:"code"`
	Name        string     `json:"name"`
	Version     string     `json:"version"`
	Description string     `json:"description"`
	Source      string     `json:"source"`
	Status      string     `json:"status"`
	PublishedAt *time.Time `json:"publishedAt"`
}

type FrameworkFilter struct {
	Search string
	Code   string
	Status string
	Page   int
	Limit  int
}

type FrameworkPage struct {
	Items      []Framework `json:"items"`
	Page       int         `json:"page"`
	Limit      int         `json:"limit"`
	Total      int64       `json:"total"`
	TotalPages int         `json:"totalPages"`
}

type RequirementRequest struct {
	FrameworkID         uint   `json:"frameworkId"`
	Code                string `json:"code"`
	Title               string `json:"title"`
	Description         string `json:"description"`
	ParentRequirementID *uint  `json:"parentRequirementId"`
	Status              string `json:"status"`
}

type RequirementFilter struct {
	Search              string
	FrameworkID         *uint
	ParentRequirementID *uint
	Status              string
	// Mapped keeps requirements with (true) or without (false) any control
	// link; ControlID keeps the requirements linked to that control.
	Mapped    *bool
	ControlID *uint
	Page      int
	Limit     int
}

type RequirementPage struct {
	Items      []Requirement `json:"items"`
	Page       int           `json:"page"`
	Limit      int           `json:"limit"`
	Total      int64         `json:"total"`
	TotalPages int           `json:"totalPages"`
}

type ControlRequest struct {
	Code            string `json:"code"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	Category        string `json:"category"`
	OwnerProfileID  *uint  `json:"ownerProfileId"`
	OwningServiceID *uint  `json:"owningServiceId"`
	Status          string `json:"status"`
}

type ControlFilter struct {
	Search         string
	Category       string
	Status         string
	ServiceID      *uint
	OwnerProfileID *uint
	// RiskID keeps the controls linked to a risk (grc_control_risks).
	RiskID *uint
	// RequirementID keeps the controls mapped to a requirement
	// (grc_requirement_controls).
	RequirementID *uint
	Page          int
	Limit         int
}

type ControlPage struct {
	Items      []Control `json:"items"`
	Page       int       `json:"page"`
	Limit      int       `json:"limit"`
	Total      int64     `json:"total"`
	TotalPages int       `json:"totalPages"`
}

type ControlImplementationRequest struct {
	ControlID             uint       `json:"controlId"`
	AssetID               uint       `json:"assetId"`
	OwningServiceID       *uint      `json:"owningServiceId"`
	OwnerProfileID        *uint      `json:"ownerProfileId"`
	Status                string     `json:"status"`
	ImplementationDetails string     `json:"implementationDetails"`
	ImplementedAt         *time.Time `json:"implementedAt"`
	LastReviewedAt        *time.Time `json:"lastReviewedAt"`
}

type ControlImplementationFilter struct {
	ControlID      *uint
	AssetID        *uint
	Status         string
	ServiceID      *uint
	OwnerProfileID *uint
	Page           int
	Limit          int
}

type ControlImplementationPage struct {
	Items      []ControlImplementation `json:"items"`
	Page       int                     `json:"page"`
	Limit      int                     `json:"limit"`
	Total      int64                   `json:"total"`
	TotalPages int                     `json:"totalPages"`
}

type RequirementControlRequest struct {
	RequirementID uint `json:"requirementId"`
	ControlID     uint `json:"controlId"`
}

type ControlRiskRequest struct {
	ControlID uint `json:"controlId"`
	RiskID    uint `json:"riskId"`
}

type EvidenceRequest struct {
	Reference               string     `json:"reference"`
	Title                   string     `json:"title"`
	Description             string     `json:"description"`
	ControlImplementationID uint       `json:"controlImplementationId"`
	Type                    string     `json:"type"`
	Status                  string     `json:"status"`
	ExternalReference       string     `json:"externalReference"`
	CollectedAt             *time.Time `json:"collectedAt"`
	ValidFrom               *time.Time `json:"validFrom"`
	ExpiresAt               *time.Time `json:"expiresAt"`
	OwnerProfileID          *uint      `json:"ownerProfileId"`
	OwningServiceID         *uint      `json:"owningServiceId"`
}

type EvidenceFilter struct {
	Search                  string
	ControlImplementationID *uint
	OwnerProfileID          *uint
	OwningServiceID         *uint
	Type                    string
	Status                  string
	Expired                 *bool
	ExpiresBefore           *time.Time
	// Validity is one of the EvidenceValidity states; ExpiringSoonDays is the
	// window of EXPIRING_SOON (0 means the default 30 days).
	Validity         string
	ExpiringSoonDays int
	Page             int
	Limit            int
}

type EvidencePage struct {
	Items      []Evidence `json:"items"`
	Page       int        `json:"page"`
	Limit      int        `json:"limit"`
	Total      int64      `json:"total"`
	TotalPages int        `json:"totalPages"`
}

type RemediationActionRequest struct {
	Reference               string     `json:"reference"`
	Title                   string     `json:"title"`
	Description             string     `json:"description"`
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

type RemediationActionFilter struct {
	Search                  string
	ControlImplementationID *uint
	RiskID                  *uint
	FindingID               *uint
	AssigneeProfileID       *uint
	OwningServiceID         *uint
	Priority                string
	Status                  string
	Overdue                 *bool
	DueBefore               *time.Time
	// DueState is one of the RemediationActionDueState states; DueSoonDays is
	// the window of DUE_SOON (0 means the default 30 days).
	DueState    string
	DueSoonDays int
	Page        int
	Limit       int
}

type RemediationActionPage struct {
	Items      []RemediationAction `json:"items"`
	Page       int                 `json:"page"`
	Limit      int                 `json:"limit"`
	Total      int64               `json:"total"`
	TotalPages int                 `json:"totalPages"`
}
