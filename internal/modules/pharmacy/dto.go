package pharmacy

import "time"

type CreateMedicationFamilyRequest struct {
	Code        string `json:"code" binding:"required"`
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
}

type UpdateMedicationFamilyRequest struct {
	Code        *string `json:"code"`
	Name        *string `json:"name"`
	Description *string `json:"description"`
	IsActive    *bool   `json:"isActive"`
}

type CreateMedicationRequest struct {
	FamilyID     uint   `json:"familyId" binding:"required"`
	Code         string `json:"code" binding:"required"`
	Name         string `json:"name" binding:"required"`
	GenericName  string `json:"genericName"`
	Manufacturer string `json:"manufacturer"`
	Description  string `json:"description"`
}

type UpdateMedicationRequest struct {
	FamilyID     *uint   `json:"familyId"`
	Code         *string `json:"code"`
	Name         *string `json:"name"`
	GenericName  *string `json:"genericName"`
	Manufacturer *string `json:"manufacturer"`
	Description  *string `json:"description"`
	IsActive     *bool   `json:"isActive"`
}

type CreateMedicationPresentationRequest struct {
	MedicationID uint   `json:"medicationId" binding:"required"`
	Code         string `json:"code" binding:"required"`
	Dosage       string `json:"dosage" binding:"required"`
	Form         string `json:"form" binding:"required"`
	Route        string `json:"route" binding:"required"`
	Unit         string `json:"unit"`
	Packaging    string `json:"packaging"`
}

type UpdateMedicationPresentationRequest struct {
	MedicationID *uint   `json:"medicationId"`
	Code         *string `json:"code"`
	Dosage       *string `json:"dosage"`
	Form         *string `json:"form"`
	Route        *string `json:"route"`
	Unit         *string `json:"unit"`
	Packaging    *string `json:"packaging"`
	IsActive     *bool   `json:"isActive"`
}

type CreatePharmacyStockRequest struct {
	PresentationID    uint    `json:"presentationId" binding:"required"`
	QuantityAvailable float64 `json:"quantityAvailable" binding:"gte=0"`
	AlertThreshold    float64 `json:"alertThreshold" binding:"gte=0"`
	IsStockManaged    bool    `json:"isStockManaged"`
}

type UpdatePharmacyStockRequest struct {
	QuantityAvailable *float64 `json:"quantityAvailable" binding:"omitempty,gte=0"`
	AlertThreshold    *float64 `json:"alertThreshold" binding:"omitempty,gte=0"`
	IsStockManaged    *bool    `json:"isStockManaged"`
}

type PharmacyStockResponse struct {
	ID                uint                   `json:"id"`
	PresentationID    uint                   `json:"presentationId"`
	Presentation      MedicationPresentation `json:"presentation"`
	QuantityAvailable float64                `json:"quantityAvailable"`
	AlertThreshold    float64                `json:"alertThreshold"`
	IsStockManaged    bool                   `json:"isStockManaged"`
	Status            string                 `json:"status"`
	CreatedAt         time.Time              `json:"createdAt"`
	UpdatedAt         time.Time              `json:"updatedAt"`
}

type CreatePharmacyBatchRequest struct {
	PresentationID   uint    `json:"presentationId" binding:"required"`
	BatchNumber      string  `json:"batchNumber" binding:"required"`
	QuantityReceived float64 `json:"quantityReceived" binding:"required,gt=0"`
	ExpirationDate   string  `json:"expirationDate"`
	Supplier         string  `json:"supplier"`
	PurchasePrice    float64 `json:"purchasePrice"`
}

type PharmacyBatchResponse struct {
	ID                uint                   `json:"id"`
	PresentationID    uint                   `json:"presentationId"`
	Presentation      MedicationPresentation `json:"presentation"`
	BatchNumber       string                 `json:"batchNumber"`
	QuantityReceived  float64                `json:"quantityReceived"`
	QuantityRemaining float64                `json:"quantityRemaining"`
	ExpirationDate    *time.Time             `json:"expirationDate"`
	Supplier          string                 `json:"supplier"`
	PurchasePrice     float64                `json:"purchasePrice"`
	IsActive          bool                   `json:"isActive"`
	CreatedAt         time.Time              `json:"createdAt"`
	UpdatedAt         time.Time              `json:"updatedAt"`
}

type CreateDispensationRequest struct {
	PresentationID uint    `json:"presentationId" binding:"required"`
	Quantity       float64 `json:"quantity" binding:"required,gt=0"`

	PatientID      *uint `json:"patientId"`
	PrescriptionID *uint `json:"prescriptionId"`

	Notes          string `json:"notes"`
	IdempotencyKey string `json:"idempotencyKey" binding:"omitempty,max=100"`
}

type PresentationAvailabilityResponse struct {
	PresentationID    uint    `json:"presentationId"`
	CommercialName    string  `json:"commercialName"`
	GenericName       string  `json:"genericName"`
	Family            string  `json:"family"`
	Dosage            string  `json:"dosage"`
	Form              string  `json:"form"`
	Route             string  `json:"route"`
	Unit              string  `json:"unit"`
	Packaging         string  `json:"packaging"`
	AvailableQuantity float64 `json:"availableQuantity"`
	AlertThreshold    float64 `json:"alertThreshold"`
	StockStatus       string  `json:"stockStatus"`
	IsActive          bool    `json:"isActive"`
}

type PrescriptionDispensationStatusResponse struct {
	PrescriptionID     uint    `json:"prescriptionId"`
	PresentationID     *uint   `json:"presentationId"`
	PrescribedQuantity float64 `json:"prescribedQuantity"`
	DispensedQuantity  float64 `json:"dispensedQuantity"`
	RemainingQuantity  float64 `json:"remainingQuantity"`
	IsFullyDispensed   bool    `json:"isFullyDispensed"`
}

type PharmacyPrescriptionQueueItem struct {
	PrescriptionID uint  `json:"prescriptionId"`
	ConsultationID uint  `json:"consultationId"`
	PresentationID *uint `json:"presentationId"`

	MedicationName    string    `json:"medicationName"`
	GenericName       string    `json:"genericName"`
	Family            string    `json:"family"`
	PatientID         uint      `json:"patientId"`
	PatientName       string    `json:"patientName"`
	PatientCode       string    `json:"patientCode"`
	DoctorName        string    `json:"doctorName"`
	Service           string    `json:"service"`
	PrescribedAt      time.Time `json:"prescribedAt"`
	AvailableQuantity float64   `json:"availableQuantity"`
	StockStatus       string    `json:"stockStatus"`
	Dosage            string    `json:"dosage"`
	Form              string    `json:"form"`
	Route             string    `json:"route"`

	PrescribedQuantity float64 `json:"prescribedQuantity"`
	DispensedQuantity  float64 `json:"dispensedQuantity"`
	RemainingQuantity  float64 `json:"remainingQuantity"`

	Duration     string `json:"duration"`
	Instructions string `json:"instructions"`

	Status string `json:"status"`
}
