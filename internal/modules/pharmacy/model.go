package pharmacy

import "time"

type MedicationFamily struct {
	ID          uint   `gorm:"primaryKey" json:"id"`
	Code        string `gorm:"size:50;uniqueIndex;not null" json:"code"`
	Name        string `gorm:"size:150;not null" json:"name"`
	Description string `gorm:"type:text" json:"description"`
	IsActive    bool   `gorm:"default:true;index" json:"isActive"`
}

type Medication struct {
	ID uint `gorm:"primaryKey" json:"id"`

	FamilyID uint             `gorm:"not null;index" json:"familyId"`
	Family   MedicationFamily `gorm:"foreignKey:FamilyID" json:"family"`

	Code        string `gorm:"size:50;uniqueIndex;not null" json:"code"`
	Name        string `gorm:"size:200;not null;index" json:"name"`
	Description string `gorm:"type:text" json:"description"`
	IsActive    bool   `gorm:"default:true;index" json:"isActive"`
}

type MedicationPresentation struct {
	ID uint `gorm:"primaryKey" json:"id"`

	MedicationID uint       `gorm:"not null;index" json:"medicationId"`
	Medication   Medication `gorm:"foreignKey:MedicationID" json:"medication"`

	Code string `gorm:"size:100;uniqueIndex;not null" json:"code"`

	Dosage string `gorm:"size:100;not null" json:"dosage"`
	Form   string `gorm:"size:100;not null" json:"form"`
	Route  string `gorm:"size:100;not null" json:"route"`
	Unit   string `gorm:"size:50" json:"unit"`

	IsActive bool `gorm:"default:true;index" json:"isActive"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type PharmacyStock struct {
	ID uint `gorm:"primaryKey" json:"id"`

	PresentationID uint                   `gorm:"uniqueIndex;not null" json:"presentationId"`
	Presentation   MedicationPresentation `gorm:"foreignKey:PresentationID" json:"presentation"`

	QuantityAvailable float64 `gorm:"type:decimal(12,2);default:0" json:"quantityAvailable"`
	AlertThreshold    float64 `gorm:"type:decimal(12,2);default:0" json:"alertThreshold"`
	IsStockManaged    bool    `gorm:"default:false;index" json:"isStockManaged"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (s PharmacyStock) Status() string {
	if !s.IsStockManaged {
		return "not_managed"
	}

	if s.QuantityAvailable <= 0 {
		return "out_of_stock"
	}

	if s.QuantityAvailable <= s.AlertThreshold {
		return "low_stock"
	}

	return "available"
}

type PharmacyBatch struct {
	ID uint `gorm:"primaryKey" json:"id"`

	PresentationID uint                   `gorm:"not null;index" json:"presentationId"`
	Presentation   MedicationPresentation `gorm:"foreignKey:PresentationID" json:"presentation"`

	BatchNumber string `gorm:"size:100;not null;index" json:"batchNumber"`

	QuantityReceived  float64 `gorm:"type:decimal(12,2);default:0" json:"quantityReceived"`
	QuantityRemaining float64 `gorm:"type:decimal(12,2);default:0" json:"quantityRemaining"`

	ExpirationDate *time.Time `json:"expirationDate"`

	Supplier      string  `gorm:"size:150" json:"supplier"`
	PurchasePrice float64 `gorm:"type:decimal(12,2);default:0" json:"purchasePrice"`

	IsActive bool `gorm:"default:true;index" json:"isActive"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

const (
	StockMovementBatchEntry    = "BATCH_ENTRY"
	StockMovementDispensation  = "DISPENSATION"
	StockMovementLoss          = "LOSS"
	StockMovementDamage        = "DAMAGE"
	StockMovementExpired       = "EXPIRED"
	StockMovementAdjustmentIn  = "ADJUSTMENT_IN"
	StockMovementAdjustmentOut = "ADJUSTMENT_OUT"
)

type StockMovement struct {
	ID uint `gorm:"primaryKey" json:"id"`

	PresentationID uint                   `gorm:"not null;index" json:"presentationId"`
	Presentation   MedicationPresentation `gorm:"foreignKey:PresentationID" json:"presentation"`

	BatchID *uint          `gorm:"index" json:"batchId"`
	Batch   *PharmacyBatch `gorm:"foreignKey:BatchID" json:"batch,omitempty"`

	Type string `gorm:"size:50;not null;index" json:"type"`

	Quantity float64 `gorm:"type:decimal(12,2);not null" json:"quantity"`

	StockBefore float64 `gorm:"type:decimal(12,2);not null" json:"stockBefore"`
	StockAfter  float64 `gorm:"type:decimal(12,2);not null" json:"stockAfter"`

	ReferenceType string `gorm:"size:100;index" json:"referenceType"`
	ReferenceID   *uint  `gorm:"index" json:"referenceId"`

	Reason string `gorm:"type:text" json:"reason"`

	PerformedByID   *uint  `gorm:"index" json:"performedById"`
	PerformedByName string `gorm:"size:150" json:"performedByName"`

	CreatedAt time.Time `gorm:"index" json:"createdAt"`
}

const (
	DispensationStatusCompleted = "COMPLETED"
)

type PharmacyDispensation struct {
	ID uint `gorm:"primaryKey" json:"id"`

	PresentationID uint                   `gorm:"not null;index" json:"presentationId"`
	Presentation   MedicationPresentation `gorm:"foreignKey:PresentationID" json:"presentation"`

	Quantity float64 `gorm:"type:decimal(12,2);not null" json:"quantity"`

	Status string `gorm:"size:50;not null;index" json:"status"`

	PatientID *uint `gorm:"index" json:"patientId"`

	ReferenceType string `gorm:"size:100;index" json:"referenceType"`
	ReferenceID   *uint  `gorm:"index" json:"referenceId"`

	Notes string `gorm:"type:text" json:"notes"`

	DispensedByID   *uint  `gorm:"index" json:"dispensedById"`
	DispensedByName string `gorm:"size:150" json:"dispensedByName"`

	Items []PharmacyDispensationItem `gorm:"foreignKey:DispensationID" json:"items"`

	CreatedAt time.Time `gorm:"index" json:"createdAt"`
}

type PharmacyDispensationItem struct {
	ID uint `gorm:"primaryKey" json:"id"`

	DispensationID uint          `gorm:"not null;index" json:"dispensationId"`
	BatchID        uint          `gorm:"not null;index" json:"batchId"`
	Batch          PharmacyBatch `gorm:"foreignKey:BatchID" json:"batch"`

	Quantity float64 `gorm:"type:decimal(12,2);not null" json:"quantity"`

	CreatedAt time.Time `json:"createdAt"`
}

type ConsultationPrescriptionRef struct {
	ID             uint `gorm:"primaryKey"`
	ConsultationID uint
	PresentationID *uint

	MedicationName string
	Dosage         string
	Form           string
	Route          string

	Quantity float64

	Duration     string
	Instructions string
}

func (ConsultationPrescriptionRef) TableName() string {
	return "consultation_prescriptions"
}
