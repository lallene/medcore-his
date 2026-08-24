package receivables

import "time"

type Filter struct {
	Search, Status, Due, DateFrom, DateTo string
	PatientID                             uint
	MinAmount, MaxAmount                  int64
	Page, Limit                           int
}
type DueDateRequest struct {
	DueDate *string `json:"dueDate"`
}
type FollowUpRequest struct {
	ActionType          string  `json:"actionType"`
	Note                string  `json:"note"`
	PromisedPaymentDate *string `json:"promisedPaymentDate"`
	PromisedAmount      *int64  `json:"promisedAmount"`
}
type Line struct {
	Description     string `json:"description"`
	ActType         string `json:"actType"`
	GrossAmount     int64  `json:"grossAmount"`
	InsuranceAmount int64  `json:"insuranceAmount"`
	PatientAmount   int64  `json:"patientAmount"`
}
type Payment struct {
	ID            uint      `json:"id"`
	Amount        int64     `json:"amount"`
	PaymentMethod string    `json:"paymentMethod"`
	Reference     string    `json:"reference"`
	PaidAt        time.Time `json:"paidAt"`
	ReceiptID     uint      `json:"receiptId"`
	ReceiptNumber string    `json:"receiptNumber"`
}
type Item struct {
	InvoiceID       uint       `json:"invoiceId"`
	InvoiceNumber   string     `json:"invoiceNumber"`
	PatientID       uint       `json:"patientId"`
	PatientName     string     `json:"patientName"`
	PatientCode     string     `json:"patientCode"`
	InvoiceDate     time.Time  `json:"invoiceDate"`
	InvoiceStatus   string     `json:"invoiceStatus"`
	GrossAmount     int64      `json:"grossAmount"`
	InsuranceAmount int64      `json:"insuranceAmount"`
	PatientDue      int64      `json:"patientDue"`
	PatientPaid     int64      `json:"patientPaid"`
	PatientBalance  int64      `json:"patientBalance"`
	DueDate         *time.Time `json:"dueDate"`
	Status          string     `json:"status"`
	LastPaymentAt   string     `json:"lastPaymentAt"`
	CoveragePending bool       `json:"coveragePending"`
	Descriptions    string     `json:"descriptions"`
}
type Page struct {
	Items      []Item `json:"items"`
	Page       int    `json:"page"`
	Limit      int    `json:"limit"`
	Total      int64  `json:"total"`
	TotalPages int    `json:"totalPages"`
}
type KPIs struct {
	TotalReceivables      int64 `json:"totalReceivables"`
	OverdueReceivables    int64 `json:"overdueReceivables"`
	NonOverdueReceivables int64 `json:"nonOverdueReceivables"`
	CollectedAmount       int64 `json:"collectedAmount"`
	DebtorPatients        int64 `json:"debtorPatients"`
	UnpaidInvoices        int64 `json:"unpaidInvoices"`
}
type Detail struct {
	Item
	Lines     []Line     `json:"lines"`
	Payments  []Payment  `json:"payments"`
	FollowUps []FollowUp `json:"followUps"`
}
