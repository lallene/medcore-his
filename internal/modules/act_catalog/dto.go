package act_catalog

// CreateRequest creates a catalogue entry. Code is normalized to uppercase.
type CreateRequest struct {
	Code              string `json:"code" binding:"required"`
	Label             string `json:"label" binding:"required"`
	Description       string `json:"description"`
	Category          string `json:"category" binding:"required"`
	BasePrice         int64  `json:"basePrice"`
	Currency          string `json:"currency"`
	Billable          *bool  `json:"billable"`
	InsuranceEligible *bool  `json:"insuranceEligible"`
	IsActive          *bool  `json:"isActive"`
}

// UpdateRequest mutates mutable fields only. Code is never accepted.
type UpdateRequest struct {
	Label             string `json:"label" binding:"required"`
	Description       string `json:"description"`
	Category          string `json:"category" binding:"required"`
	BasePrice         int64  `json:"basePrice"`
	Currency          string `json:"currency"`
	Billable          *bool  `json:"billable"`
	InsuranceEligible *bool  `json:"insuranceEligible"`
	IsActive          *bool  `json:"isActive"`
}

// ListFilter bounds list/search.
type ListFilter struct {
	Search   string
	Category string
	Active   *bool
	Page     int
	Limit    int
}

// Page is the paginated list response.
type Page struct {
	Data       []Entry `json:"data"`
	Page       int     `json:"page"`
	Limit      int     `json:"limit"`
	Total      int64   `json:"total"`
	TotalPages int     `json:"totalPages"`
}
