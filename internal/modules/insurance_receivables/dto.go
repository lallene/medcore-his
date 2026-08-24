package insurance_receivables

import "time"

type Filter struct {
	Search, Status, DateFrom, DateTo, Overdue string
	CompanyID, PatientID, BatchID             uint
	Page, Limit                               int
}
type Item struct {
	InvoiceLineID       uint       `json:"invoiceLineId"`
	InvoiceID           uint       `json:"invoiceId"`
	PatientID           uint       `json:"patientId"`
	InsuranceCompanyID  uint       `json:"insuranceCompanyId"`
	InvoiceNumber       string     `json:"invoiceNumber"`
	PatientName         string     `json:"patientName"`
	PatientCode         string     `json:"patientCode"`
	CompanyName         string     `json:"companyName"`
	AuthorizationID     *uint      `json:"authorizationId"`
	AuthorizationNumber string     `json:"authorizationNumber"`
	CoverageResolution  string     `json:"coverageResolution"`
	ActType             string     `json:"actType"`
	Description         string     `json:"description"`
	InvoiceDate         time.Time  `json:"invoiceDate"`
	GrossAmount         int64      `json:"grossAmount"`
	InsuranceDue        int64      `json:"insuranceDue"`
	InsurancePaid       int64      `json:"insurancePaid"`
	InsuranceBalance    int64      `json:"insuranceBalance"`
	DueDate             *time.Time `json:"dueDate"`
	Status              string     `json:"status"`
	BatchNumber         string     `json:"batchNumber"`
}
type Page struct {
	Items      []Item `json:"items"`
	Page       int    `json:"page"`
	Limit      int    `json:"limit"`
	Total      int64  `json:"total"`
	TotalPages int    `json:"totalPages"`
}
type KPIs struct {
	TotalReceivables  int64 `json:"totalReceivables"`
	SettledAmount     int64 `json:"settledAmount"`
	OverdueAmount     int64 `json:"overdueAmount"`
	UnallocatedAmount int64 `json:"unallocatedAmount"`
	PendingInvoices   int64 `json:"pendingInvoices"`
	DebtorCompanies   int64 `json:"debtorCompanies"`
}
type CompanySummary struct {
	InsuranceCompanyID uint   `json:"insuranceCompanyId"`
	CompanyName        string `json:"companyName"`
	Billed             int64  `json:"billed"`
	Paid               int64  `json:"paid"`
	Balance            int64  `json:"balance"`
	Unallocated        int64  `json:"unallocated"`
	Invoices           int64  `json:"invoices"`
	Patients           int64  `json:"patients"`
}
type SettlementRequest struct {
	InsuranceCompanyID uint   `json:"insuranceCompanyId"`
	GuarantorID        *uint  `json:"guarantorId"`
	CoverageReference  string `json:"coverageReference"`
	ExternalReference  string `json:"externalReference"`
	ReceivedAt         string `json:"receivedAt"`
	TotalAmount        int64  `json:"totalAmount"`
	PaymentMethod      string `json:"paymentMethod"`
	BankReference      string `json:"bankReference"`
	Comment            string `json:"comment"`
	IdempotencyKey     string `json:"idempotencyKey"`
}
type AllocationRequest struct {
	InvoiceLineID uint  `json:"invoiceLineId"`
	Amount        int64 `json:"amount"`
}
type SettlementView struct {
	Settlement
	CompanyName       string                 `json:"companyName"`
	AllocatedAmount   int64                  `json:"allocatedAmount"`
	UnallocatedAmount int64                  `json:"unallocatedAmount"`
	Allocations       []SettlementAllocation `json:"allocations"`
}
type DueDateRequest struct {
	DueDate *string `json:"dueDate"`
	Note    string  `json:"note"`
}
type FollowUpRequest struct {
	Type string `json:"type"`
	Note string `json:"note"`
}
type ReceivableView struct {
	Item
	FollowUps   []FollowUp             `json:"followUps"`
	Allocations []SettlementAllocation `json:"allocations"`
}
type BatchRequest struct {
	InsuranceCompanyID uint    `json:"insuranceCompanyId"`
	PeriodFrom         *string `json:"periodFrom"`
	PeriodTo           *string `json:"periodTo"`
	ExternalReference  string  `json:"externalReference"`
	Comment            string  `json:"comment"`
	InvoiceLineIDs     []uint  `json:"invoiceLineIds"`
}
type BatchView struct {
	SubmissionBatch
	CompanyName  string                `json:"companyName"`
	InvoiceCount int64                 `json:"invoiceCount"`
	TotalAmount  int64                 `json:"totalAmount"`
	Items        []SubmissionBatchItem `json:"items,omitempty"`
}
