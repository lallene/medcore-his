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
