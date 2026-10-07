package cash

type RegisterRequest struct {
	Code     string `json:"code" binding:"required"`
	Name     string `json:"name" binding:"required"`
	Location string `json:"location"`
	Active   *bool  `json:"active"`
}

// SessionListFilter supports historical session listing (status/register/date + pagination).
type SessionListFilter struct {
	Status         string
	CashRegisterID uint
	DateFrom       string // opened_at >= date (YYYY-MM-DD)
	DateTo         string // opened_at < date+1 day
	Page           int
	Limit          int
}

// SessionListPage is a paginated list of CashSession rows (no journal / no payment sums).
type SessionListPage struct {
	Items      []Session `json:"items"`
	Page       int       `json:"page"`
	Limit      int       `json:"limit"`
	Total      int64     `json:"total"`
	TotalPages int       `json:"totalPages"`
}
type OpenRequest struct {
	CashRegisterID uint   `json:"cashRegisterId" binding:"required"`
	OpeningFloat   int64  `json:"openingFloat"`
	Note           string `json:"note"`
	// IdempotencyKey may be supplied in JSON and/or Idempotency-Key header.
	IdempotencyKey string `json:"idempotencyKey"`
}
type CloseRequest struct {
	CountedCashAmount int64  `json:"countedCashAmount"`
	Note              string `json:"note"`
	// IdempotencyKey may be supplied in JSON and/or Idempotency-Key header.
	IdempotencyKey string `json:"idempotencyKey"`
}
type PaymentRequest struct {
	InvoiceID         uint   `json:"invoiceId" binding:"required"`
	Amount            int64  `json:"amount" binding:"required"`
	PaymentMethod     string `json:"paymentMethod" binding:"required"`
	ExternalReference string `json:"externalReference"`
	MobileOperator    string `json:"mobileOperator"`
	// IdempotencyKey may be supplied in JSON and/or Idempotency-Key header (handler merges).
	IdempotencyKey string `json:"idempotencyKey"`
	// LOT29F-H-B: required for NEW payments (mapped to billing.PayerRequest).
	Payer *PayerRequest `json:"payer"`
}

// PayerRequest mirrors billing.PayerRequest for cash collection commands.
type PayerRequest struct {
	Mode         string `json:"mode"`
	PartyID      *uint  `json:"partyId,omitempty"`
	DisplayName  string `json:"displayName,omitempty"`
	Phone        string `json:"phone,omitempty"`
	Relationship string `json:"relationship,omitempty"`
}
