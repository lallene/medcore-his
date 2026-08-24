package billing

type TariffRequest struct {
	ActType       string `json:"actType" binding:"required"`
	ReferenceID   *uint  `json:"referenceId"`
	Code          string `json:"code" binding:"required"`
	Label         string `json:"label" binding:"required"`
	UnitPrice     int64  `json:"unitPrice" binding:"required"`
	EffectiveFrom string `json:"effectiveFrom"`
	EffectiveTo   string `json:"effectiveTo"`
	IsActive      *bool  `json:"isActive"`
}
type InvoiceLineRequest struct {
	ActType     string `json:"actType" binding:"required"`
	ReferenceID uint   `json:"referenceId" binding:"required"`
	TariffID    uint   `json:"tariffId" binding:"required"`
}
type CreateInvoiceRequest struct {
	PatientID uint                 `json:"patientId" binding:"required"`
	Lines     []InvoiceLineRequest `json:"lines" binding:"required,min=1"`
}
type PaymentRequest struct {
	Amount         int64  `json:"amount" binding:"required"`
	PaymentMethod  string `json:"paymentMethod" binding:"required"`
	Reference      string `json:"reference"`
	IdempotencyKey string `json:"idempotencyKey" binding:"required"`
	MobileOperator string `json:"mobileOperator"`
}
type CancelRequest struct {
	Reason string `json:"reason" binding:"required"`
}
type ListFilter struct {
	PatientID      uint
	Status, Search string
	Page, Limit    int
}
type Page struct {
	Data       []Invoice `json:"data"`
	Page       int       `json:"page"`
	Limit      int       `json:"limit"`
	Total      int64     `json:"total"`
	TotalPages int       `json:"totalPages"`
}
type BillableAct struct {
	ActType             string  `json:"actType"`
	ReferenceID         uint    `json:"referenceId"`
	BillableKey         string  `json:"billableKey"`
	Label               string  `json:"label"`
	Date                string  `json:"date"`
	Quantity            float64 `json:"quantity"`
	Tariff              *Tariff `json:"tariff"`
	CoverageResolution  string  `json:"coverageResolution"`
	AuthorizationNumber string  `json:"authorizationNumber"`
	AlreadyBilled       bool    `json:"alreadyBilled"`
}
type KPIs struct {
	PendingInvoices   int64 `json:"pendingInvoices"`
	PatientReceivable int64 `json:"patientReceivable"`
	PaidInvoices      int64 `json:"paidInvoices"`
	InsuranceExpected int64 `json:"insuranceExpected"`
}
type ActBillingStatus struct {
	Billed        bool   `json:"billed"`
	InvoiceID     uint   `json:"invoiceId,omitempty"`
	InvoiceNumber string `json:"invoiceNumber,omitempty"`
	InvoiceStatus string `json:"invoiceStatus,omitempty"`
}
