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
	ID             uint      `gorm:"primaryKey" json:"id"`
	CashRegisterID uint      `gorm:"not null;index" json:"cashRegisterId"`
	OpenedBy       uint      `gorm:"not null;index" json:"openedBy"`
	OpenedAt       time.Time `gorm:"not null;index" json:"openedAt"`
	OpeningFloat   int64     `gorm:"not null;check:cash_session_opening_nonnegative,opening_float >= 0" json:"openingFloat"`
	OpeningNote    string    `gorm:"type:text" json:"openingNote"`
	// OpenIdempotencyKey namespaces OPEN intents (distinct from billing/close keys).
	OpenIdempotencyKey string     `gorm:"size:120;uniqueIndex" json:"openIdempotencyKey,omitempty"`
	Status             string     `gorm:"size:20;not null;index" json:"status"`
	ClosedBy           *uint      `gorm:"index" json:"closedBy"`
	ClosedAt           *time.Time `json:"closedAt"`
	ExpectedCashAmount *int64     `json:"expectedCashAmount"`
	CountedCashAmount  *int64     `json:"countedCashAmount"`
	CashDifference     *int64     `json:"cashDifference"`
	ClosingNote        string     `gorm:"type:text" json:"closingNote"`
	// CloseIdempotencyKey set only when CLOSED; unique when present.
	CloseIdempotencyKey *string   `gorm:"size:120;uniqueIndex" json:"closeIdempotencyKey,omitempty"`
	CreatedAt           time.Time `json:"createdAt"`
	UpdatedAt           time.Time `json:"updatedAt"`
	Register            Register  `gorm:"foreignKey:CashRegisterID" json:"register"`
}

func (Session) TableName() string { return "cash_sessions" }

type Receipt struct {
	ID            uint   `gorm:"primaryKey" json:"id"`
	ReceiptNumber string `gorm:"size:30;not null;uniqueIndex" json:"receiptNumber"`
	PaymentID     uint   `gorm:"not null;uniqueIndex" json:"paymentId"`
	InvoiceID     uint   `gorm:"not null;index" json:"invoiceId"`
	PatientID     uint   `gorm:"not null;index" json:"patientId"`
	// CashSessionID is set for cash-session collection; nil for sessionless billing collection (LOT29D-B).
	CashSessionID     *uint     `gorm:"index" json:"cashSessionId,omitempty"`
	Amount            int64     `gorm:"not null;check:cash_receipt_amount_positive,amount > 0" json:"amount"`
	PaymentMethod     string    `gorm:"size:30;not null" json:"paymentMethod"`
	ExternalReference string    `gorm:"size:120" json:"externalReference"`
	MobileOperator    string    `gorm:"size:80" json:"mobileOperator"`
	IssuedBy          uint      `gorm:"not null;index" json:"issuedBy"`
	IssuedAt          time.Time `gorm:"not null;index" json:"issuedAt"`
	InvoiceNumber     string    `gorm:"size:30;not null" json:"invoiceNumber"`
	PatientName       string    `gorm:"size:250;not null" json:"patientName"`
	PatientCode       string    `gorm:"size:50" json:"patientCode"`
	CashierName       string    `gorm:"size:150;not null" json:"cashierName"`
	// RegisterCode/RegisterName are cash-register snapshots; empty for sessionless billing receipts.
	RegisterCode       string `gorm:"size:50;not null" json:"registerCode"`
	RegisterName       string `gorm:"size:150;not null" json:"registerName"`
	InvoiceGrossAmount int64  `json:"invoiceGrossAmount"`
	InsuranceAmount    int64  `json:"insuranceAmount"`
	PatientAmount      int64  `json:"patientAmount"`
	PaidBefore         int64  `json:"paidBefore"`
	BalanceAfter       int64  `json:"balanceAfter"`
	// LOT29F-H-B: payer snapshot at receipt issue (empty for historical receipts).
	PayerDisplayName  string    `gorm:"size:250" json:"payerDisplayName,omitempty"`
	PayerKind         string    `gorm:"size:20" json:"payerKind,omitempty"`
	PayerPhone        string    `gorm:"size:50" json:"payerPhone,omitempty"`
	PayerRelationship string    `gorm:"size:80" json:"payerRelationship,omitempty"`
	PayerIsPatient    bool      `gorm:"not null;default:false" json:"payerIsPatient"`
	PayerProvenance   string    `gorm:"size:30" json:"payerProvenance,omitempty"`
	CreatedAt         time.Time `json:"createdAt"`
	// LOT29D-C: underlying payment was fully reversed (decorated on read).
	PaymentReversed   bool       `json:"paymentReversed" gorm:"-"`
	PaymentReversedAt *time.Time `json:"paymentReversedAt,omitempty" gorm:"-"`
	// LOT29F-E′: derived when reversal timestamp is after session ClosedAt.
	PostCloseCorrection bool `json:"postCloseCorrection,omitempty" gorm:"-"`
	// LOT29F-F: physical correction executed later on a host OPEN session.
	CashCorrectionExecuted      bool       `json:"cashCorrectionExecuted,omitempty" gorm:"-"`
	CashCorrectionExecutionID   *uint      `json:"cashCorrectionExecutionId,omitempty" gorm:"-"`
	CashCorrectionExecutedAt    *time.Time `json:"cashCorrectionExecutedAt,omitempty" gorm:"-"`
	CashCorrectionHostSessionID *uint      `json:"cashCorrectionHostSessionId,omitempty" gorm:"-"`
}

func (Receipt) TableName() string { return "cash_receipts" }

// LOT29F-C CashMovement directions / V1 types (amount always > 0; direction carries sign).
const (
	MovementIn  = "IN"
	MovementOut = "OUT"

	MovementManualIn  = "MANUAL_IN"
	MovementManualOut = "MANUAL_OUT"
	// LOT29F-D: system-generated drawer effect of an OPEN CASH PaymentReversal (not MANUAL_OUT).
	MovementPaymentReversal = "PAYMENT_REVERSAL"
	// LOT29F-F: system OUT on OPEN host session for post-close physical correction (PCE1).
	// Defined in correction_execution.go as MovementPostCloseCorrectionOut.

	MovementRefPaymentReversal = "PAYMENT_REVERSAL"
)

// CashMovement is an append-only physical cash journal entry on an OPEN CashSession.
// It is not a Payment, PaymentReversal, Refund, CreditNote, or Receipt.
type CashMovement struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	CashSessionID  uint      `gorm:"not null;index" json:"cashSessionId"`
	Direction      string    `gorm:"size:10;not null;index" json:"direction"`
	Type           string    `gorm:"size:30;not null;index" json:"type"`
	Amount         int64     `gorm:"not null;check:cash_movement_amount_positive,amount > 0" json:"amount"`
	Reason         string    `gorm:"size:500;not null" json:"reason"`
	ReferenceType  string    `gorm:"size:40" json:"referenceType,omitempty"`
	ReferenceID    *uint     `gorm:"index" json:"referenceId,omitempty"`
	CreatedBy      uint      `gorm:"not null;index" json:"createdBy"`
	OccurredAt     time.Time `gorm:"not null;index" json:"occurredAt"`
	IdempotencyKey string    `gorm:"size:120;not null;uniqueIndex" json:"idempotencyKey"`
	CreatedAt      time.Time `json:"createdAt"`
}

func (CashMovement) TableName() string { return "cash_movements" }

// CashMovementAudit is the mandatory financial evidence row for a movement (same TX).
type CashMovementAudit struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	MovementID uint      `gorm:"not null;uniqueIndex" json:"movementId"`
	EventType  string    `gorm:"size:40;not null" json:"eventType"`
	SessionID  uint      `gorm:"not null;index" json:"sessionId"`
	Direction  string    `gorm:"size:10;not null" json:"direction"`
	Type       string    `gorm:"size:30;not null" json:"type"`
	Amount     int64     `gorm:"not null" json:"amount"`
	Reason     string    `gorm:"size:500;not null" json:"reason"`
	ActorID    uint      `gorm:"not null;index" json:"actorId"`
	CreatedAt  time.Time `json:"createdAt"`
}

func (CashMovementAudit) TableName() string { return "cash_movement_audits" }

// VarianceKinds for CLOSED final reconciliation (server-authored semantics).
const (
	VarianceBalanced = "BALANCED"
	VarianceShortage = "SHORTAGE"
	VarianceSurplus  = "SURPLUS"
)

// SessionSummary is the single backend-authoritative financial projection for a CashSession.
// Collection totals come from billing_payments attached to the session (not receipts).
// OpeningFloat is never part of TotalCollected.
// ExpectedCash OPEN: OpeningFloat + CASH collected + movement IN − movement OUT.
// ExpectedCash CLOSED: persisted ExpectedCashAmount snapshot.
// LOT29E-D: Closed CashSession remains the reconciliation aggregate — no second CashReconciliation entity.
// LOT29F-C: CashMovement totals are explicit and never part of CashCollected / TotalCollected.
type SessionSummary struct {
	Session Session `json:"session"`
	// Canonical collection fields (LOT29E-C).
	CashCollected    int64 `json:"cashCollected"`
	NonCashCollected int64 `json:"nonCashCollected"`
	TotalCollected   int64 `json:"totalCollected"`
	// Method breakdown (authoritative; FE must not regroup from journal).
	CashPayments         int64 `json:"cashPayments"`
	CardPayments         int64 `json:"cardPayments"`
	MobileMoneyPayments  int64 `json:"mobileMoneyPayments"`
	BankTransferPayments int64 `json:"bankTransferPayments"`
	CheckPayments        int64 `json:"checkPayments"`
	// TotalPayments mirrors TotalCollected for compatibility with pre-29E-C clients.
	TotalPayments  int64 `json:"totalPayments"`
	OperationCount int64 `json:"operationCount"`
	ExpectedCash   int64 `json:"expectedCash"`
	// LOT29F-C movement totals (not revenue). CashCollected remains gross CASH payments.
	CashMovementIn  int64 `json:"cashMovementIn"`
	CashMovementOut int64 `json:"cashMovementOut"`
	NetCashMovement int64 `json:"netCashMovement"`
	// LOT29F-D / F OUT breakdown (same CashMovementOut authority; informational only).
	CashMovementManualOut              int64 `json:"cashMovementManualOut"`
	CashMovementReversalOut            int64 `json:"cashMovementReversalOut"`
	CashMovementPostCloseCorrectionOut int64 `json:"cashMovementPostCloseCorrectionOut"`
	// LOT29E-D reconciliation metadata (projection over CashSession; not a second aggregate).
	ClosingProofComplete bool   `json:"closingProofComplete"`
	FinalReconciliation  bool   `json:"finalReconciliation"`
	RecoveryClose        bool   `json:"recoveryClose"`
	VarianceKind         string `json:"varianceKind,omitempty"`
}
