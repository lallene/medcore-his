package pharmacy

import (
	"errors"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
)

var (
	ErrFamilyNotFound        = errors.New("famille thérapeutique introuvable")
	ErrMedicationNotFound    = errors.New("médicament introuvable")
	ErrPresentationNotFound  = errors.New("présentation pharmaceutique introuvable")
	ErrStockNotFound         = errors.New("stock pharmacie introuvable")
	ErrStockAlreadyExists    = errors.New("un stock existe déjà pour cette présentation")
	ErrInvalidExpirationDate = errors.New("date d'expiration invalide")
	ErrInsufficientStock     = errors.New("stock insuffisant")
	ErrStockNotManaged       = errors.New("stock non géré par la pharmacie")

	ErrPrescriptionNotFound             = errors.New("prescription introuvable")
	ErrPrescriptionRequired             = errors.New("une prescription MedCore interne est obligatoire")
	ErrPrescriptionPatientMismatch      = errors.New("la prescription ne correspond pas au patient")
	ErrPrescriptionPresentationMismatch = errors.New("la présentation ne correspond pas à la prescription")
	ErrPrescriptionQuantityExceeded     = errors.New("quantité délivrée supérieure au reste à délivrer")
	ErrInvalidPrescriptionStatus        = errors.New("statut de prescription invalide")
)

type Service struct {
	repo *Repository
}

const (
	PrescriptionDispensationStatusPending   = "PENDING"
	PrescriptionDispensationStatusPartial   = "PARTIAL"
	PrescriptionDispensationStatusCompleted = "COMPLETED"
)

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) GetFamilies() ([]MedicationFamily, error) {
	return s.repo.FindAllFamilies()
}

func (s *Service) CreateFamily(req CreateMedicationFamilyRequest) (*MedicationFamily, error) {
	family := MedicationFamily{
		Code:        req.Code,
		Name:        req.Name,
		Description: req.Description,
		IsActive:    true,
	}

	if err := s.repo.CreateFamily(&family); err != nil {
		return nil, err
	}

	return &family, nil
}

func (s *Service) UpdateFamily(
	id uint,
	req UpdateMedicationFamilyRequest,
) (*MedicationFamily, error) {
	if _, err := s.repo.FindFamilyByID(id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrFamilyNotFound
		}

		return nil, err
	}

	updates := map[string]interface{}{}

	if req.Code != nil {
		updates["code"] = *req.Code
	}

	if req.Name != nil {
		updates["name"] = *req.Name
	}

	if req.Description != nil {
		updates["description"] = *req.Description
	}

	if req.IsActive != nil {
		updates["is_active"] = *req.IsActive
	}

	if len(updates) > 0 {
		if err := s.repo.UpdateFamily(id, updates); err != nil {
			return nil, err
		}
	}

	return s.repo.FindFamilyByID(id)
}

func (s *Service) DeleteFamily(id uint) error {
	if _, err := s.repo.FindFamilyByID(id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrFamilyNotFound
		}

		return err
	}

	return s.repo.DeleteFamily(id)
}

func (s *Service) GetMedications() ([]Medication, error) {
	return s.repo.FindAllMedications()
}

func (s *Service) CreateMedication(req CreateMedicationRequest) (*Medication, error) {
	if _, err := s.repo.FindFamilyByID(req.FamilyID); err != nil {
		return nil, ErrFamilyNotFound
	}

	medication := Medication{
		FamilyID:     req.FamilyID,
		Code:         req.Code,
		Name:         req.Name,
		GenericName:  req.GenericName,
		Manufacturer: req.Manufacturer,
		Description:  req.Description,
		IsActive:     true,
	}

	if err := s.repo.CreateMedication(&medication); err != nil {
		return nil, err
	}

	return s.repo.FindMedicationByID(medication.ID)
}

func (s *Service) UpdateMedication(id uint, req UpdateMedicationRequest) (*Medication, error) {
	if _, err := s.repo.FindMedicationByID(id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrMedicationNotFound
		}

		return nil, err
	}

	updates := map[string]interface{}{}

	if req.FamilyID != nil {
		if _, err := s.repo.FindFamilyByID(*req.FamilyID); err != nil {
			return nil, ErrFamilyNotFound
		}

		updates["family_id"] = *req.FamilyID
	}

	if req.Code != nil {
		updates["code"] = *req.Code
	}

	if req.Name != nil {
		updates["name"] = *req.Name
	}
	if req.GenericName != nil {
		updates["generic_name"] = *req.GenericName
	}
	if req.Manufacturer != nil {
		updates["manufacturer"] = *req.Manufacturer
	}

	if req.Description != nil {
		updates["description"] = *req.Description
	}

	if req.IsActive != nil {
		updates["is_active"] = *req.IsActive
	}

	if len(updates) > 0 {
		if err := s.repo.UpdateMedication(id, updates); err != nil {
			return nil, err
		}
	}

	return s.repo.FindMedicationByID(id)
}

func (s *Service) DeleteMedication(id uint) error {
	if _, err := s.repo.FindMedicationByID(id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrMedicationNotFound
		}

		return err
	}

	return s.repo.DeleteMedication(id)
}

func (s *Service) GetPresentations() ([]MedicationPresentation, error) {
	return s.repo.FindAllPresentations()
}

func (s *Service) GetPresentationAvailability() ([]PresentationAvailabilityResponse, error) {
	presentations, err := s.repo.FindAllPresentations()
	if err != nil {
		return nil, err
	}
	stocks, err := s.repo.FindAllStocks()
	if err != nil {
		return nil, err
	}
	stockByPresentation := make(map[uint]PharmacyStock, len(stocks))
	for _, stock := range stocks {
		stockByPresentation[stock.PresentationID] = stock
	}
	dispensable, err := s.repo.FindDispensableQuantities(time.Now())
	if err != nil {
		return nil, err
	}
	result := make([]PresentationAvailabilityResponse, 0, len(presentations))
	for _, p := range presentations {
		stock, ok := stockByPresentation[p.ID]
		availableQuantity := dispensable[p.ID]
		status := "OUT_OF_STOCK"
		if ok && p.IsActive && p.Medication.IsActive && availableQuantity > 0 {
			status = "AVAILABLE"
			if availableQuantity <= stock.AlertThreshold {
				status = "LOW_STOCK"
			}
		}
		result = append(result, PresentationAvailabilityResponse{
			PresentationID: p.ID, CommercialName: p.Medication.Name, GenericName: p.Medication.GenericName,
			Family: p.Medication.Family.Name, Dosage: p.Dosage, Form: p.Form, Route: p.Route, Unit: p.Unit,
			Packaging: p.Packaging, AvailableQuantity: availableQuantity, AlertThreshold: stock.AlertThreshold,
			StockStatus: status, IsActive: p.IsActive && p.Medication.IsActive,
		})
	}
	rank := func(v string) int {
		if v == "AVAILABLE" {
			return 0
		}
		if v == "LOW_STOCK" {
			return 1
		}
		return 2
	}
	sort.SliceStable(result, func(i, j int) bool {
		if rank(result[i].StockStatus) != rank(result[j].StockStatus) {
			return rank(result[i].StockStatus) < rank(result[j].StockStatus)
		}
		return strings.ToLower(result[i].CommercialName) < strings.ToLower(result[j].CommercialName)
	})
	return result, nil
}

func (s *Service) CreatePresentation(
	req CreateMedicationPresentationRequest,
) (*MedicationPresentation, error) {
	if _, err := s.repo.FindMedicationByID(req.MedicationID); err != nil {
		return nil, ErrMedicationNotFound
	}

	presentation := MedicationPresentation{
		MedicationID: req.MedicationID,
		Code:         req.Code,
		Dosage:       req.Dosage,
		Form:         req.Form,
		Route:        req.Route,
		Unit:         req.Unit,
		Packaging:    req.Packaging,
		IsActive:     true,
	}

	if err := s.repo.CreatePresentation(&presentation); err != nil {
		return nil, err
	}

	return s.repo.FindPresentationByID(presentation.ID)
}

func (s *Service) UpdatePresentation(
	id uint,
	req UpdateMedicationPresentationRequest,
) (*MedicationPresentation, error) {
	if _, err := s.repo.FindPresentationByID(id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrPresentationNotFound
		}

		return nil, err
	}

	updates := map[string]interface{}{}

	if req.MedicationID != nil {
		if _, err := s.repo.FindMedicationByID(*req.MedicationID); err != nil {
			return nil, ErrMedicationNotFound
		}

		updates["medication_id"] = *req.MedicationID
	}

	if req.Code != nil {
		updates["code"] = *req.Code
	}

	if req.Dosage != nil {
		updates["dosage"] = *req.Dosage
	}

	if req.Form != nil {
		updates["form"] = *req.Form
	}

	if req.Route != nil {
		updates["route"] = *req.Route
	}

	if req.Unit != nil {
		updates["unit"] = *req.Unit
	}
	if req.Packaging != nil {
		updates["packaging"] = *req.Packaging
	}

	if req.IsActive != nil {
		updates["is_active"] = *req.IsActive
	}

	if len(updates) > 0 {
		if err := s.repo.UpdatePresentation(id, updates); err != nil {
			return nil, err
		}
	}

	return s.repo.FindPresentationByID(id)
}

func (s *Service) DeletePresentation(id uint) error {
	if _, err := s.repo.FindPresentationByID(id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrPresentationNotFound
		}

		return err
	}

	return s.repo.DeletePresentation(id)
}

func toStockResponse(stock *PharmacyStock) *PharmacyStockResponse {
	return &PharmacyStockResponse{
		ID:                stock.ID,
		PresentationID:    stock.PresentationID,
		Presentation:      stock.Presentation,
		QuantityAvailable: stock.QuantityAvailable,
		AlertThreshold:    stock.AlertThreshold,
		IsStockManaged:    stock.IsStockManaged,
		Status:            stock.Status(),
		CreatedAt:         stock.CreatedAt,
		UpdatedAt:         stock.UpdatedAt,
	}
}

func (s *Service) GetStocks() ([]PharmacyStockResponse, error) {
	stocks, err := s.repo.FindAllStocks()
	if err != nil {
		return nil, err
	}
	dispensable, err := s.repo.FindDispensableQuantities(time.Now())
	if err != nil {
		return nil, err
	}

	responses := make([]PharmacyStockResponse, 0, len(stocks))

	for i := range stocks {
		response := *toStockResponse(&stocks[i])
		response.QuantityAvailable = dispensable[stocks[i].PresentationID]
		response.Status = "out_of_stock"
		if !stocks[i].IsStockManaged {
			response.Status = "not_managed"
		} else if response.QuantityAvailable > 0 && response.QuantityAvailable <= response.AlertThreshold {
			response.Status = "low_stock"
		} else if response.QuantityAvailable > response.AlertThreshold {
			response.Status = "available"
		}
		responses = append(responses, response)
	}

	return responses, nil
}

func (s *Service) CreateStock(
	req CreatePharmacyStockRequest,
) (*PharmacyStockResponse, error) {
	if _, err := s.repo.FindPresentationByID(req.PresentationID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrPresentationNotFound
		}

		return nil, err
	}

	if _, err := s.repo.FindStockByPresentationID(req.PresentationID); err == nil {
		return nil, ErrStockAlreadyExists
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	stock := PharmacyStock{
		PresentationID:    req.PresentationID,
		QuantityAvailable: req.QuantityAvailable,
		AlertThreshold:    req.AlertThreshold,
		IsStockManaged:    req.IsStockManaged,
	}

	if err := s.repo.CreateStock(&stock); err != nil {
		return nil, err
	}

	created, err := s.repo.FindStockByID(stock.ID)
	if err != nil {
		return nil, err
	}

	return toStockResponse(created), nil
}

func (s *Service) UpdateStock(
	id uint,
	req UpdatePharmacyStockRequest,
) (*PharmacyStockResponse, error) {
	if _, err := s.repo.FindStockByID(id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrStockNotFound
		}

		return nil, err
	}

	updates := map[string]interface{}{}

	if req.QuantityAvailable != nil {
		updates["quantity_available"] = *req.QuantityAvailable
	}

	if req.AlertThreshold != nil {
		updates["alert_threshold"] = *req.AlertThreshold
	}

	if req.IsStockManaged != nil {
		updates["is_stock_managed"] = *req.IsStockManaged
	}

	if len(updates) > 0 {
		if err := s.repo.UpdateStock(id, updates); err != nil {
			return nil, err
		}
	}

	stock, err := s.repo.FindStockByID(id)
	if err != nil {
		return nil, err
	}

	return toStockResponse(stock), nil
}

func toBatchResponse(batch *PharmacyBatch) *PharmacyBatchResponse {
	return &PharmacyBatchResponse{
		ID:                batch.ID,
		PresentationID:    batch.PresentationID,
		Presentation:      batch.Presentation,
		BatchNumber:       batch.BatchNumber,
		QuantityReceived:  batch.QuantityReceived,
		QuantityRemaining: batch.QuantityRemaining,
		ExpirationDate:    batch.ExpirationDate,
		Supplier:          batch.Supplier,
		PurchasePrice:     batch.PurchasePrice,
		IsActive:          batch.IsActive,
		CreatedAt:         batch.CreatedAt,
		UpdatedAt:         batch.UpdatedAt,
	}
}

func (s *Service) GetBatches() ([]PharmacyBatchResponse, error) {
	batches, err := s.repo.FindAllBatches()
	if err != nil {
		return nil, err
	}

	responses := make([]PharmacyBatchResponse, 0, len(batches))

	for i := range batches {
		responses = append(responses, *toBatchResponse(&batches[i]))
	}

	return responses, nil
}

func (s *Service) CreateBatch(req CreatePharmacyBatchRequest) (*PharmacyBatchResponse, error) {
	if _, err := s.repo.FindPresentationByID(req.PresentationID); err != nil {
		return nil, ErrPresentationNotFound
	}

	var expirationDate *time.Time

	if req.ExpirationDate != "" {
		parsed, err := time.Parse("2006-01-02", req.ExpirationDate)
		if err != nil {
			return nil, ErrInvalidExpirationDate
		}

		expirationDate = &parsed
	}

	batch := PharmacyBatch{
		PresentationID:    req.PresentationID,
		BatchNumber:       req.BatchNumber,
		QuantityReceived:  req.QuantityReceived,
		QuantityRemaining: req.QuantityReceived,
		ExpirationDate:    expirationDate,
		Supplier:          req.Supplier,
		PurchasePrice:     req.PurchasePrice,
		IsActive:          true,
	}

	if err := s.repo.CreateBatchAndIncreaseStock(&batch); err != nil {
		return nil, err
	}

	created, err := s.repo.FindBatchByID(batch.ID)
	if err != nil {
		return nil, err
	}

	return toBatchResponse(created), nil
}

func (s *Service) GetStockMovements() ([]StockMovement, error) {
	return s.repo.FindAllStockMovements()
}

func (s *Service) GetPresentationStockMovements(
	presentationID uint,
) ([]StockMovement, error) {
	if _, err := s.repo.FindPresentationByID(presentationID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrPresentationNotFound
		}

		return nil, err
	}

	return s.repo.FindStockMovementsByPresentationID(presentationID)
}

func (s *Service) GetDispensations() ([]PharmacyDispensation, error) {
	return s.repo.FindAllDispensations()
}

func (s *Service) CreateDispensation(
	req CreateDispensationRequest,
	userID uint,
) (*PharmacyDispensation, error) {
	if req.PrescriptionID == nil {
		return nil, ErrPrescriptionRequired
	}
	if req.IdempotencyKey != "" {
		if existing, err := s.repo.FindDispensationByIdempotencyKey(req.IdempotencyKey); err == nil {
			return existing, nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	if _, err := s.repo.FindPresentationByID(req.PresentationID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrPresentationNotFound
		}

		return nil, err
	}

	user, err := s.repo.FindUserByID(userID)
	if err != nil {
		return nil, err
	}

	dispensation := PharmacyDispensation{
		PresentationID:  req.PresentationID,
		Quantity:        req.Quantity,
		Status:          DispensationStatusCompleted,
		PatientID:       req.PatientID,
		ReferenceType:   "",
		ReferenceID:     nil,
		Notes:           req.Notes,
		IdempotencyKey:  req.IdempotencyKey,
		DispensedByID:   &user.ID,
		DispensedByName: user.Name,
	}

	if req.PrescriptionID != nil {
		prescription, err := s.repo.FindConsultationPrescriptionByID(*req.PrescriptionID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, ErrPrescriptionNotFound
			}

			return nil, err
		}

		if prescription.PresentationID == nil ||
			*prescription.PresentationID != req.PresentationID {
			return nil, ErrPrescriptionPresentationMismatch
		}
		context, err := s.repo.FindPrescriptionContext(*req.PrescriptionID)
		if err != nil {
			return nil, err
		}
		if req.PatientID != nil && *req.PatientID != context.PatientID {
			return nil, ErrPrescriptionPatientMismatch
		}
		dispensation.PatientID = &context.PatientID

		alreadyDispensed, err :=
			s.repo.SumDispensedQuantityForPrescription(*req.PrescriptionID)
		if err != nil {
			return nil, err
		}

		remaining := prescription.Quantity - alreadyDispensed

		if req.Quantity > remaining {
			return nil, ErrPrescriptionQuantityExceeded
		}

		dispensation.ReferenceType = "CONSULTATION_PRESCRIPTION"
		dispensation.ReferenceID = req.PrescriptionID
	}

	if err := s.repo.Dispense(&dispensation); err != nil {
		return nil, err
	}

	return s.repo.FindDispensationByID(dispensation.ID)
}

func (s *Service) GetPrescriptionDispensationStatus(
	prescriptionID uint,
) (*PrescriptionDispensationStatusResponse, error) {
	prescription, err := s.repo.FindConsultationPrescriptionByID(prescriptionID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrPrescriptionNotFound
		}

		return nil, err
	}

	dispensedQuantity, err := s.repo.SumDispensedQuantityForPrescription(
		prescriptionID,
	)
	if err != nil {
		return nil, err
	}

	remainingQuantity := prescription.Quantity - dispensedQuantity

	if remainingQuantity < 0 {
		remainingQuantity = 0
	}

	return &PrescriptionDispensationStatusResponse{
		PrescriptionID:     prescription.ID,
		PresentationID:     prescription.PresentationID,
		PrescribedQuantity: prescription.Quantity,
		DispensedQuantity:  dispensedQuantity,
		RemainingQuantity:  remainingQuantity,
		IsFullyDispensed:   remainingQuantity <= 0,
	}, nil
}

func (s *Service) GetPrescriptionQueue(
	statusFilter string,
) ([]PharmacyPrescriptionQueueItem, error) {
	if statusFilter != "" {
		switch statusFilter {
		case PrescriptionDispensationStatusPending,
			PrescriptionDispensationStatusPartial,
			PrescriptionDispensationStatusCompleted:
		default:
			return nil, ErrInvalidPrescriptionStatus
		}
	}

	prescriptions, err := s.repo.FindAllConsultationPrescriptions()
	if err != nil {
		return nil, err
	}

	items := make([]PharmacyPrescriptionQueueItem, 0, len(prescriptions))
	dispensable, err := s.repo.FindDispensableQuantities(time.Now())
	if err != nil {
		return nil, err
	}

	for _, prescription := range prescriptions {
		context, err := s.repo.FindPrescriptionContext(prescription.ID)
		if err != nil {
			return nil, err
		}
		presentation, err := s.repo.FindPresentationByID(*prescription.PresentationID)
		if err != nil {
			return nil, err
		}
		stock, stockErr := s.repo.FindStockByPresentationID(*prescription.PresentationID)
		stockStatus, available := "OUT_OF_STOCK", float64(0)
		if stockErr == nil && presentation.IsActive && presentation.Medication.IsActive {
			available = dispensable[*prescription.PresentationID]
			if available > stock.AlertThreshold {
				stockStatus = "AVAILABLE"
			} else if available > 0 {
				stockStatus = "LOW_STOCK"
			}
		}
		dispensedQuantity, err := s.repo.SumDispensedQuantityForPrescription(
			prescription.ID,
		)
		if err != nil {
			return nil, err
		}

		remainingQuantity := prescription.Quantity - dispensedQuantity
		if remainingQuantity < 0 {
			remainingQuantity = 0
		}

		status := PrescriptionDispensationStatusPending

		if dispensedQuantity > 0 && remainingQuantity > 0 {
			status = PrescriptionDispensationStatusPartial
		}

		if remainingQuantity <= 0 {
			status = PrescriptionDispensationStatusCompleted
		}

		if statusFilter != "" && status != statusFilter {
			continue
		}

		items = append(items, PharmacyPrescriptionQueueItem{
			PrescriptionID:     prescription.ID,
			ConsultationID:     prescription.ConsultationID,
			PresentationID:     prescription.PresentationID,
			MedicationName:     presentation.Medication.Name,
			GenericName:        presentation.Medication.GenericName,
			Family:             presentation.Medication.Family.Name,
			PatientID:          context.PatientID,
			PatientName:        context.PatientName,
			PatientCode:        context.PatientCode,
			DoctorName:         context.DoctorName,
			Service:            context.Service,
			PrescribedAt:       context.CreatedAt,
			AvailableQuantity:  available,
			StockStatus:        stockStatus,
			Dosage:             prescription.Dosage,
			Form:               prescription.Form,
			Route:              prescription.Route,
			PrescribedQuantity: prescription.Quantity,
			DispensedQuantity:  dispensedQuantity,
			RemainingQuantity:  remainingQuantity,
			Duration:           prescription.Duration,
			Instructions:       prescription.Instructions,
			Status:             status,
		})
	}

	return items, nil
}
