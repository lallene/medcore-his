package billing

import "time"

const (
	InvoiceDraft         = "DRAFT"
	InvoiceIssued        = "ISSUED"
	InvoicePartiallyPaid = "PARTIALLY_PAID"
	InvoicePaid          = "PAID"
	InvoiceCancelled     = "CANCELLED"
)

type Tariff struct {
	ID            uint       `gorm:"primaryKey" json:"id"`
	ActType       string     `gorm:"size:30;not null;index:idx_tariff_lookup" json:"actType"`
	ReferenceID   *uint      `gorm:"index:idx_tariff_lookup" json:"referenceId"`
	Code          string     `gorm:"size:60;not null;uniqueIndex" json:"code"`
	Label         string     `gorm:"size:200;not null" json:"label"`
	UnitPrice     int64      `gorm:"not null;check:billing_tariff_unit_price_nonnegative,unit_price >= 0" json:"unitPrice"`
	Currency      string     `gorm:"size:3;not null;default:XOF" json:"currency"`
	EffectiveFrom time.Time  `gorm:"not null;index" json:"effectiveFrom"`
	EffectiveTo   *time.Time `gorm:"index" json:"effectiveTo"`
	IsActive      bool       `gorm:"not null;default:true;index" json:"isActive"`
	CreatedBy     uint       `gorm:"not null;index" json:"createdBy"`
	UpdatedBy     uint       `gorm:"not null;index" json:"updatedBy"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
}

func (Tariff) TableName() string { return "billing_tariffs" }

type Invoice struct {
	ID                 uint          `gorm:"primaryKey" json:"id"`
	Number             string        `gorm:"size:30;not null;uniqueIndex" json:"number"`
	PatientID          uint          `gorm:"not null;index" json:"patientId"`
	MedicalRecordID    *uint         `gorm:"index" json:"medicalRecordId"`
	Status             string        `gorm:"size:30;not null;index" json:"status"`
	GrossAmount        int64         `gorm:"not null;check:billing_invoice_gross_nonnegative,gross_amount >= 0" json:"grossAmount"`
	InsuranceAmount    int64         `gorm:"not null;check:billing_invoice_insurance_nonnegative,insurance_amount >= 0" json:"insuranceAmount"`
	PatientAmount      int64         `gorm:"not null;check:billing_invoice_patient_nonnegative,patient_amount >= 0" json:"patientAmount"`
	PaidAmount         int64         `gorm:"not null;check:billing_invoice_paid_nonnegative,paid_amount >= 0" json:"paidAmount"`
	BalanceAmount      int64         `gorm:"not null;check:billing_invoice_balance_nonnegative,balance_amount >= 0" json:"balanceAmount"`
	CoveragePending    bool          `gorm:"not null;default:false" json:"coveragePending"`
	IssuedAt           *time.Time    `json:"issuedAt"`
	IssuedBy           *uint         `gorm:"index" json:"issuedBy"`
	CancelledAt        *time.Time    `json:"cancelledAt"`
	CancelledBy        *uint         `gorm:"index" json:"cancelledBy"`
	CancellationReason string        `gorm:"type:text" json:"cancellationReason"`
	CreatedBy          uint          `gorm:"not null;index" json:"createdBy"`
	UpdatedBy          uint          `gorm:"not null;index" json:"updatedBy"`
	CreatedAt          time.Time     `gorm:"index" json:"createdAt"`
	UpdatedAt          time.Time     `json:"updatedAt"`
	Lines              []InvoiceLine `gorm:"foreignKey:InvoiceID" json:"lines,omitempty"`
	Payments           []Payment     `gorm:"foreignKey:InvoiceID" json:"payments,omitempty"`
	PatientName        string        `gorm:"-" json:"patientName"`
	PatientCode        string        `gorm:"-" json:"patientCode"`
	// LOT29F-B credit-note decorations (not columns).
	CreditedAmount         int64             `json:"creditedAmount" gorm:"-"`
	EffectivePatientAmount int64             `json:"effectivePatientAmount" gorm:"-"`
	EffectiveBalanceAmount int64             `json:"effectiveBalanceAmount" gorm:"-"`
	CreditNote             *CreditNotePublic `json:"creditNote,omitempty" gorm:"-"`
}

func (Invoice) TableName() string { return "billing_invoices" }

type InvoiceLine struct {
	ID                  uint      `gorm:"primaryKey" json:"id"`
	InvoiceID           uint      `gorm:"not null;index" json:"invoiceId"`
	TariffID            uint      `gorm:"not null;index" json:"tariffId"`
	ActType             string    `gorm:"size:30;not null;index" json:"actType"`
	ReferenceID         uint      `gorm:"not null;index" json:"referenceId"`
	ClinicalReferenceID uint      `gorm:"not null;index" json:"clinicalReferenceId"`
	BillableKey         string    `gorm:"size:100;not null;index" json:"billableKey"`
	Description         string    `gorm:"size:240;not null" json:"description"`
	Quantity            float64   `gorm:"type:decimal(12,2);not null;check:billing_line_quantity_positive,quantity > 0" json:"quantity"`
	UnitPrice           int64     `gorm:"not null;check:billing_line_unit_price_nonnegative,unit_price >= 0" json:"unitPrice"`
	GrossAmount         int64     `gorm:"not null;check:billing_line_gross_nonnegative,gross_amount >= 0" json:"grossAmount"`
	InsuranceAmount     int64     `gorm:"not null;check:billing_line_insurance_nonnegative,insurance_amount >= 0" json:"insuranceAmount"`
	PatientAmount       int64     `gorm:"not null;check:billing_line_patient_nonnegative,patient_amount >= 0" json:"patientAmount"`
	AuthorizationID     *uint     `gorm:"index" json:"authorizationId"`
	AuthorizationNumber string    `gorm:"size:30" json:"authorizationNumber"`
	CoverageResolution  string    `gorm:"size:20;not null" json:"coverageResolution"`
	CoverageStatus      string    `gorm:"size:30" json:"coverageStatus"`
	CoveragePending     bool      `gorm:"not null;default:false" json:"coveragePending"`
	IsActive            bool      `gorm:"not null;default:true;index" json:"isActive"`
	CreatedAt           time.Time `json:"createdAt"`
}

func (InvoiceLine) TableName() string { return "billing_invoice_lines" }

type AuthorizationAllocation struct {
	ID              uint  `gorm:"primaryKey"`
	AuthorizationID uint  `gorm:"not null;index"`
	InvoiceLineID   uint  `gorm:"not null;uniqueIndex"`
	Amount          int64 `gorm:"not null;check:billing_allocation_amount_nonnegative,amount >= 0"`
	CreatedAt       time.Time
}

func (AuthorizationAllocation) TableName() string { return "billing_authorization_allocations" }

type Payment struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	InvoiceID      uint      `gorm:"not null;index" json:"invoiceId"`
	Amount         int64     `gorm:"not null;check:billing_payment_amount_positive,amount > 0" json:"amount"`
	PaymentMethod  string    `gorm:"size:30;not null" json:"paymentMethod"`
	Reference      string    `gorm:"size:120" json:"reference"`
	IdempotencyKey string    `gorm:"size:120;not null;uniqueIndex" json:"idempotencyKey"`
	PaidAt         time.Time `gorm:"not null;index" json:"paidAt"`
	ReceivedBy     uint      `gorm:"not null;index" json:"receivedBy"`
	CashSessionID  *uint     `gorm:"index" json:"cashSessionId,omitempty"`
	MobileOperator string    `gorm:"size:80" json:"mobileOperator,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	// Canonical cash_receipts identity (decorated on read; not a column).
	ReceiptID     *uint  `json:"receiptId,omitempty" gorm:"-"`
	ReceiptNumber string `json:"receiptNumber,omitempty" gorm:"-"`
	// LOT29F-D: OPEN/CLOSED of linked cash session when present (read decoration).
	CashSessionStatus string `json:"cashSessionStatus,omitempty" gorm:"-"`
	// LOT29D-C full reversal decoration (not columns).
	Reversed       bool       `json:"reversed" gorm:"-"`
	ReversalID     *uint      `json:"reversalId,omitempty" gorm:"-"`
	ReversedAt     *time.Time `json:"reversedAt,omitempty" gorm:"-"`
	ReversalReason string     `json:"reversalReason,omitempty" gorm:"-"`
	ReversedBy     *uint      `json:"reversedBy,omitempty" gorm:"-"`
}

func (Payment) TableName() string { return "billing_payments" }

// PaymentReversal is the V1 linked counter-entry for a full payment reversal (LOT29D-C).
// Original Payment rows remain immutable.
type PaymentReversal struct {
	ID                uint      `gorm:"primaryKey" json:"id"`
	OriginalPaymentID uint      `gorm:"not null;uniqueIndex" json:"originalPaymentId"`
	Amount            int64     `gorm:"not null;check:billing_payment_reversal_amount_positive,amount > 0" json:"amount"`
	Reason            string    `gorm:"size:500;not null" json:"reason"`
	ReversedBy        uint      `gorm:"not null;index" json:"reversedBy"`
	ReversedAt        time.Time `gorm:"not null;index" json:"reversedAt"`
	IdempotencyKey    string    `gorm:"size:120;not null;uniqueIndex" json:"idempotencyKey"`
	CreatedAt         time.Time `json:"createdAt"`
}

func (PaymentReversal) TableName() string { return "billing_payment_reversals" }

// CreditNote is the immutable full-invoice accounting correction document (LOT29F-B).
// It is not a payment, payment reversal, refund, or cash movement.
type CreditNote struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	Number         string    `gorm:"size:30;not null;uniqueIndex" json:"number"`
	InvoiceID      uint      `gorm:"not null;uniqueIndex" json:"invoiceId"`
	Amount         int64     `gorm:"not null;check:billing_credit_note_amount_positive,amount > 0" json:"amount"`
	Reason         string    `gorm:"size:500;not null" json:"reason"`
	IssuedBy       uint      `gorm:"not null;index" json:"issuedBy"`
	IssuedAt       time.Time `gorm:"not null;index" json:"issuedAt"`
	IdempotencyKey string    `gorm:"size:120;not null;uniqueIndex" json:"idempotencyKey"`
	CreatedAt      time.Time `json:"createdAt"`
}

func (CreditNote) TableName() string { return "billing_credit_notes" }

// CreditNotePublic is the invoice-embedded / document projection.
type CreditNotePublic struct {
	ID        uint      `json:"id"`
	Number    string    `json:"number"`
	InvoiceID uint      `json:"invoiceId"`
	Amount    int64     `json:"amount"`
	Reason    string    `json:"reason"`
	IssuedBy  uint      `json:"issuedBy"`
	IssuedAt  time.Time `json:"issuedAt"`
	CreatedAt time.Time `json:"createdAt"`
}
