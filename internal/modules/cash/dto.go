package cash

type RegisterRequest struct {
	Code     string `json:"code" binding:"required"`
	Name     string `json:"name" binding:"required"`
	Location string `json:"location"`
	Active   *bool  `json:"active"`
}
type OpenRequest struct {
	CashRegisterID uint   `json:"cashRegisterId" binding:"required"`
	OpeningFloat   int64  `json:"openingFloat"`
	Note           string `json:"note"`
}
type CloseRequest struct {
	CountedCashAmount int64  `json:"countedCashAmount"`
	Note              string `json:"note"`
}
type PaymentRequest struct {
	InvoiceID         uint   `json:"invoiceId" binding:"required"`
	Amount            int64  `json:"amount" binding:"required"`
	PaymentMethod     string `json:"paymentMethod" binding:"required"`
	ExternalReference string `json:"externalReference"`
	MobileOperator    string `json:"mobileOperator"`
	IdempotencyKey    string `json:"idempotencyKey" binding:"required"`
}
