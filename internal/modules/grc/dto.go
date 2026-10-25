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
	Page              int
	Limit             int
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
	Page                int
	Limit               int
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
	Page           int
	Limit          int
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
