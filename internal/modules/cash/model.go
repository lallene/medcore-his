package cash

import (
	"github.com/lallene/medcore-his/backend/internal/modules/organization"
	"time"
)

const (
	SessionOpen   = "OPEN"
	SessionClosed = "CLOSED"
)

type Register struct {
	ID        uint                  `gorm:"primaryKey" json:"id"`
	Code      string                `gorm:"size:50;not null;uniqueIndex" json:"code"`
	Name      string                `gorm:"size:150;not null" json:"name"`
	Location  string                `gorm:"size:200" json:"location"`
	ServiceID *uint                 `gorm:"index" json:"serviceId"`
	Service   *organization.Service `gorm:"foreignKey:ServiceID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"service,omitempty"`
	Active    bool                  `gorm:"not null;default:true;index" json:"active"`
	CreatedBy uint                  `gorm:"not null;index" json:"createdBy"`
	UpdatedBy uint                  `gorm:"not null;index" json:"updatedBy"`
	CreatedAt time.Time             `json:"createdAt"`
	UpdatedAt time.Time             `json:"updatedAt"`
}

func (Register) TableName() string { return "cash_registers" }

type Session struct {
	ID                 uint       `gorm:"primaryKey" json:"id"`
	CashRegisterID     uint       `gorm:"not null;index" json:"cashRegisterId"`
	OpenedBy           uint       `gorm:"not null;index" json:"openedBy"`
	OpenedAt           time.Time  `gorm:"not null;index" json:"openedAt"`
	OpeningFloat       int64      `gorm:"not null;check:cash_session_opening_nonnegative,opening_float >= 0" json:"openingFloat"`
	OpeningNote        string     `gorm:"type:text" json:"openingNote"`
	Status             string     `gorm:"size:20;not null;index" json:"status"`
	ClosedBy           *uint      `gorm:"index" json:"closedBy"`
	ClosedAt           *time.Time `json:"closedAt"`
	ExpectedCashAmount *int64     `json:"expectedCashAmount"`
	CountedCashAmount  *int64     `json:"countedCashAmount"`
	CashDifference     *int64     `json:"cashDifference"`
	ClosingNote        string     `gorm:"type:text" json:"closingNote"`
	CreatedAt          time.Time  `json:"createdAt"`
	UpdatedAt          time.Time  `json:"updatedAt"`
	Register           Register   `gorm:"foreignKey:CashRegisterID" json:"register"`
}

func (Session) TableName() string { return "cash_sessions" }

type Receipt struct {
	ID                 uint      `gorm:"primaryKey" json:"id"`
	ReceiptNumber      string    `gorm:"size:30;not null;uniqueIndex" json:"receiptNumber"`
	PaymentID          uint      `gorm:"not null;uniqueIndex" json:"paymentId"`
	InvoiceID          uint      `gorm:"not null;index" json:"invoiceId"`
	PatientID          uint      `gorm:"not null;index" json:"patientId"`
	CashSessionID      uint      `gorm:"not null;index" json:"cashSessionId"`
	Amount             int64     `gorm:"not null;check:cash_receipt_amount_positive,amount > 0" json:"amount"`
	PaymentMethod      string    `gorm:"size:30;not null" json:"paymentMethod"`
	ExternalReference  string    `gorm:"size:120" json:"externalReference"`
	MobileOperator     string    `gorm:"size:80" json:"mobileOperator"`
	IssuedBy           uint      `gorm:"not null;index" json:"issuedBy"`
	IssuedAt           time.Time `gorm:"not null;index" json:"issuedAt"`
	InvoiceNumber      string    `gorm:"size:30;not null" json:"invoiceNumber"`
	PatientName        string    `gorm:"size:250;not null" json:"patientName"`
	PatientCode        string    `gorm:"size:50" json:"patientCode"`
	CashierName        string    `gorm:"size:150;not null" json:"cashierName"`
	RegisterCode       string    `gorm:"size:50;not null" json:"registerCode"`
	RegisterName       string    `gorm:"size:150;not null" json:"registerName"`
	InvoiceGrossAmount int64     `json:"invoiceGrossAmount"`
	InsuranceAmount    int64     `json:"insuranceAmount"`
	PatientAmount      int64     `json:"patientAmount"`
	PaidBefore         int64     `json:"paidBefore"`
	BalanceAfter       int64     `json:"balanceAfter"`
	CreatedAt          time.Time `json:"createdAt"`
}

func (Receipt) TableName() string { return "cash_receipts" }

type SessionSummary struct {
	Session              Session `json:"session"`
	CashPayments         int64   `json:"cashPayments"`
	CardPayments         int64   `json:"cardPayments"`
	MobileMoneyPayments  int64   `json:"mobileMoneyPayments"`
	BankTransferPayments int64   `json:"bankTransferPayments"`
	CheckPayments        int64   `json:"checkPayments"`
	TotalPayments        int64   `json:"totalPayments"`
	OperationCount       int64   `json:"operationCount"`
	ExpectedCash         int64   `json:"expectedCash"`
}
