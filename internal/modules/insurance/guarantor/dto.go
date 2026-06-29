package guarantor

type CreateGuarantorRequest struct {
	CompanyID           uint    `json:"companyId" binding:"required"`
	Code                string  `json:"code" binding:"required,min=2,max=50"`
	Name                string  `json:"name" binding:"required,min=2,max=150"`
	Description         string  `json:"description"`
	DefaultCoverageRate float64 `json:"defaultCoverageRate" binding:"gte=0,lte=100"`
	PaymentDelayDays    int     `json:"paymentDelayDays" binding:"gte=0,lte=365"`
}

type UpdateGuarantorRequest struct {
	CompanyID           uint    `json:"companyId"`
	Code                string  `json:"code" binding:"omitempty,min=2,max=50"`
	Name                string  `json:"name" binding:"omitempty,min=2,max=150"`
	Description         string  `json:"description"`
	DefaultCoverageRate float64 `json:"defaultCoverageRate" binding:"gte=0,lte=100"`
	PaymentDelayDays    int     `json:"paymentDelayDays" binding:"gte=0,lte=365"`
	IsActive            *bool   `json:"isActive"`
}

type GuarantorResponse struct {
	ID                  uint    `json:"id"`
	UUID                string  `json:"uuid"`
	CompanyID           uint    `json:"companyId"`
	CompanyName         string  `json:"companyName,omitempty"`
	Code                string  `json:"code"`
	Name                string  `json:"name"`
	Description         string  `json:"description"`
	DefaultCoverageRate float64 `json:"defaultCoverageRate"`
	PaymentDelayDays    int     `json:"paymentDelayDays"`
	IsActive            bool    `json:"isActive"`
}

type GuarantorSummary struct {
	ID                  uint    `json:"id"`
	CompanyID           uint    `json:"companyId"`
	CompanyName         string  `json:"companyName,omitempty"`
	Code                string  `json:"code"`
	Name                string  `json:"name"`
	DefaultCoverageRate float64 `json:"defaultCoverageRate"`
	IsActive            bool    `json:"isActive"`
}
