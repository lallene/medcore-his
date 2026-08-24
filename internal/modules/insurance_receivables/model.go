package insurance_receivables

import "time"

const (
	SettlementDraft     = "DRAFT"
	SettlementPosted    = "POSTED"
	SettlementCancelled = "CANCELLED"
	BatchDraft          = "DRAFT"
	BatchSubmitted      = "SUBMITTED"
	BatchAcknowledged   = "ACKNOWLEDGED"
	BatchClosed         = "CLOSED"
)

type Settlement struct {
	ID                 uint       `gorm:"primaryKey" json:"id"`
	SettlementNumber   string     `gorm:"size:30;not null;uniqueIndex" json:"settlementNumber"`
	InsuranceCompanyID uint       `gorm:"not null;index;uniqueIndex:ux_ins_settlement_external" json:"insuranceCompanyId"`
	GuarantorID        *uint      `gorm:"index" json:"guarantorId"`
	CoverageReference  string     `gorm:"size:120" json:"coverageReference"`
	ExternalReference  string     `gorm:"size:150;not null;uniqueIndex:ux_ins_settlement_external" json:"externalReference"`
	ReceivedAt         time.Time  `gorm:"not null;index" json:"receivedAt"`
	TotalAmount        int64      `gorm:"not null;check:insurance_settlement_amount_positive,total_amount > 0" json:"totalAmount"`
	PaymentMethod      string     `gorm:"size:30;not null" json:"paymentMethod"`
	BankReference      string     `gorm:"size:150" json:"bankReference"`
	Comment            string     `gorm:"type:text" json:"comment"`
	Status             string     `gorm:"size:20;not null;index" json:"status"`
	IdempotencyKey     string     `gorm:"size:150;not null;uniqueIndex" json:"idempotencyKey"`
	CreatedBy          uint       `gorm:"not null;index" json:"createdBy"`
	PostedBy           *uint      `gorm:"index" json:"postedBy"`
	PostedAt           *time.Time `json:"postedAt"`
	CreatedAt          time.Time  `json:"createdAt"`
	UpdatedAt          time.Time  `json:"updatedAt"`
}

func (Settlement) TableName() string { return "insurance_settlements" }

type SettlementAllocation struct {
	ID                       uint      `gorm:"primaryKey" json:"id"`
	SettlementID             uint      `gorm:"not null;index;uniqueIndex:ux_ins_settlement_line" json:"settlementId"`
	InvoiceID                uint      `gorm:"not null;index" json:"invoiceId"`
	InvoiceLineID            uint      `gorm:"not null;index;uniqueIndex:ux_ins_settlement_line" json:"invoiceLineId"`
	InsuranceAuthorizationID *uint     `gorm:"index" json:"insuranceAuthorizationId"`
	Amount                   int64     `gorm:"not null;check:insurance_settlement_allocation_positive,amount > 0" json:"amount"`
	CreatedBy                uint      `gorm:"not null;index" json:"createdBy"`
	CreatedAt                time.Time `json:"createdAt"`
}

func (SettlementAllocation) TableName() string { return "insurance_settlement_allocations" }

type ReceivableMetadata struct {
	ID            uint       `gorm:"primaryKey" json:"id"`
	InvoiceLineID uint       `gorm:"not null;uniqueIndex" json:"invoiceLineId"`
	DueDate       *time.Time `gorm:"type:date;index" json:"dueDate"`
	Note          string     `gorm:"type:text" json:"note"`
	UpdatedBy     uint       `gorm:"not null;index" json:"updatedBy"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
}

func (ReceivableMetadata) TableName() string { return "insurance_receivable_metadata" }

type FollowUp struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	InvoiceLineID uint      `gorm:"not null;index" json:"invoiceLineId"`
	Type          string    `gorm:"size:30;not null" json:"type"`
	Note          string    `gorm:"type:text;not null" json:"note"`
	FollowedUpAt  time.Time `gorm:"not null;index" json:"followedUpAt"`
	CreatedBy     uint      `gorm:"not null;index" json:"createdBy"`
	CreatedAt     time.Time `json:"createdAt"`
}

func (FollowUp) TableName() string { return "insurance_receivable_followups" }

type SubmissionBatch struct {
	ID                 uint       `gorm:"primaryKey" json:"id"`
	BatchNumber        string     `gorm:"size:40;not null;uniqueIndex" json:"batchNumber"`
	InsuranceCompanyID uint       `gorm:"not null;index" json:"insuranceCompanyId"`
	PeriodFrom         *time.Time `gorm:"type:date" json:"periodFrom"`
	PeriodTo           *time.Time `gorm:"type:date" json:"periodTo"`
	ExternalReference  string     `gorm:"size:150" json:"externalReference"`
	Comment            string     `gorm:"type:text" json:"comment"`
	Status             string     `gorm:"size:20;not null;index" json:"status"`
	SubmittedBy        *uint      `gorm:"index" json:"submittedBy"`
	SubmittedAt        *time.Time `json:"submittedAt"`
	CreatedBy          uint       `gorm:"not null;index" json:"createdBy"`
	CreatedAt          time.Time  `json:"createdAt"`
	UpdatedAt          time.Time  `json:"updatedAt"`
}

func (SubmissionBatch) TableName() string { return "insurance_submission_batches" }

type SubmissionBatchItem struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	BatchID       uint      `gorm:"not null;index;uniqueIndex:ux_ins_batch_line" json:"batchId"`
	InvoiceID     uint      `gorm:"not null;index" json:"invoiceId"`
	InvoiceLineID uint      `gorm:"not null;index;uniqueIndex:ux_ins_batch_line" json:"invoiceLineId"`
	Amount        int64     `gorm:"not null;check:insurance_batch_item_positive,amount > 0" json:"amount"`
	CreatedAt     time.Time `json:"createdAt"`
}

func (SubmissionBatchItem) TableName() string { return "insurance_submission_batch_items" }
