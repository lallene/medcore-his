package pharmacy

import (
	"errors"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/auth"
	"gorm.io/gorm"
)

type Repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) FindAllFamilies() ([]MedicationFamily, error) {
	var families []MedicationFamily

	err := r.db.
		Order("name ASC").
		Find(&families).Error

	return families, err
}

func (r *Repository) FindFamilyByID(id uint) (*MedicationFamily, error) {
	var family MedicationFamily

	if err := r.db.First(&family, id).Error; err != nil {
		return nil, err
	}

	return &family, nil
}

func (r *Repository) CreateFamily(family *MedicationFamily) error {
	return r.db.Create(family).Error
}

func (r *Repository) UpdateFamily(id uint, updates map[string]interface{}) error {
	return r.db.
		Model(&MedicationFamily{}).
		Where("id = ?", id).
		Updates(updates).Error
}

func (r *Repository) DeleteFamily(id uint) error {
	return r.db.
		Model(&MedicationFamily{}).
		Where("id = ?", id).
		Update("is_active", false).Error
}

func (r *Repository) FindAllMedications() ([]Medication, error) {
	var medications []Medication

	err := r.db.
		Preload("Family").
		Order("name ASC").
		Find(&medications).Error

	return medications, err
}

func (r *Repository) FindMedicationByID(id uint) (*Medication, error) {
	var medication Medication

	if err := r.db.
		Preload("Family").
		First(&medication, id).Error; err != nil {
		return nil, err
	}

	return &medication, nil
}

func (r *Repository) CreateMedication(medication *Medication) error {
	return r.db.Create(medication).Error
}

func (r *Repository) UpdateMedication(id uint, updates map[string]interface{}) error {
	return r.db.
		Model(&Medication{}).
		Where("id = ?", id).
		Updates(updates).Error
}

func (r *Repository) DeleteMedication(id uint) error {
	return r.db.
		Model(&Medication{}).
		Where("id = ?", id).
		Update("is_active", false).Error
}

func (r *Repository) FindAllPresentations() ([]MedicationPresentation, error) {
	var presentations []MedicationPresentation

	err := r.db.
		Preload("Medication").
		Preload("Medication.Family").
		Order("created_at DESC").
		Find(&presentations).Error

	return presentations, err
}

func (r *Repository) FindPresentationByID(id uint) (*MedicationPresentation, error) {
	var presentation MedicationPresentation

	if err := r.db.
		Preload("Medication").
		Preload("Medication.Family").
		First(&presentation, id).Error; err != nil {
		return nil, err
	}

	return &presentation, nil
}

func (r *Repository) CreatePresentation(presentation *MedicationPresentation) error {
	return r.db.Create(presentation).Error
}

func (r *Repository) UpdatePresentation(id uint, updates map[string]interface{}) error {
	return r.db.
		Model(&MedicationPresentation{}).
		Where("id = ?", id).
		Updates(updates).Error
}

func (r *Repository) DeletePresentation(id uint) error {
	return r.db.
		Model(&MedicationPresentation{}).
		Where("id = ?", id).
		Update("is_active", false).Error
}

func (r *Repository) FindAllStocks() ([]PharmacyStock, error) {
	var stocks []PharmacyStock

	err := r.db.
		Preload("Presentation").
		Preload("Presentation.Medication").
		Preload("Presentation.Medication.Family").
		Order("created_at DESC").
		Find(&stocks).Error

	return stocks, err
}

func (r *Repository) FindStockByID(id uint) (*PharmacyStock, error) {
	var stock PharmacyStock

	if err := r.db.
		Preload("Presentation").
		Preload("Presentation.Medication").
		Preload("Presentation.Medication.Family").
		First(&stock, id).Error; err != nil {
		return nil, err
	}

	return &stock, nil
}

func (r *Repository) FindStockByPresentationID(
	presentationID uint,
) (*PharmacyStock, error) {
	var stock PharmacyStock

	if err := r.db.
		Where("presentation_id = ?", presentationID).
		First(&stock).Error; err != nil {
		return nil, err
	}

	return &stock, nil
}

func (r *Repository) CreateStock(stock *PharmacyStock) error {
	return r.db.Create(stock).Error
}

func (r *Repository) UpdateStock(
	id uint,
	updates map[string]interface{},
) error {
	return r.db.
		Model(&PharmacyStock{}).
		Where("id = ?", id).
		Updates(updates).Error
}

func (r *Repository) FindAllBatches() ([]PharmacyBatch, error) {
	var batches []PharmacyBatch

	err := r.db.
		Preload("Presentation").
		Preload("Presentation.Medication").
		Preload("Presentation.Medication.Family").
		Order("created_at DESC").
		Find(&batches).Error

	return batches, err
}

func (r *Repository) FindBatchByID(id uint) (*PharmacyBatch, error) {
	var batch PharmacyBatch

	if err := r.db.
		Preload("Presentation").
		Preload("Presentation.Medication").
		Preload("Presentation.Medication.Family").
		First(&batch, id).Error; err != nil {
		return nil, err
	}

	return &batch, nil
}

func (r *Repository) CreateBatchAndIncreaseStock(batch *PharmacyBatch) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var stock PharmacyStock

		err := tx.
			Where("presentation_id = ?", batch.PresentationID).
			First(&stock).Error

		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		stockBefore := float64(0)

		if err == nil {
			stockBefore = stock.QuantityAvailable
		}

		if err := tx.Create(batch).Error; err != nil {
			return err
		}

		stockAfter := stockBefore + batch.QuantityRemaining

		if errors.Is(err, gorm.ErrRecordNotFound) {
			stock = PharmacyStock{
				PresentationID:    batch.PresentationID,
				QuantityAvailable: stockAfter,
				AlertThreshold:    0,
				IsStockManaged:    true,
			}

			if err := tx.Create(&stock).Error; err != nil {
				return err
			}
		} else {
			if err := tx.
				Model(&PharmacyStock{}).
				Where("id = ?", stock.ID).
				Updates(map[string]interface{}{
					"quantity_available": stockAfter,
					"is_stock_managed":   true,
				}).Error; err != nil {
				return err
			}
		}

		movement := StockMovement{
			PresentationID: batch.PresentationID,
			BatchID:        &batch.ID,
			Type:           StockMovementBatchEntry,
			Quantity:       batch.QuantityReceived,
			StockBefore:    stockBefore,
			StockAfter:     stockAfter,
			ReferenceType:  "PHARMACY_BATCH",
			ReferenceID:    &batch.ID,
			Reason:         "Entrée d'un nouveau lot en pharmacie",
		}

		return tx.Create(&movement).Error
	})
}

func (r *Repository) FindAllStockMovements() ([]StockMovement, error) {
	var movements []StockMovement

	err := r.db.
		Preload("Presentation").
		Preload("Presentation.Medication").
		Preload("Presentation.Medication.Family").
		Preload("Batch.Presentation").
		Preload("Batch.Presentation.Medication").
		Preload("Batch.Presentation.Medication.Family").
		Preload("Batch").
		Order("created_at DESC").
		Find(&movements).Error

	return movements, err
}

func (r *Repository) FindStockMovementsByPresentationID(
	presentationID uint,
) ([]StockMovement, error) {
	var movements []StockMovement

	err := r.db.
		Preload("Presentation").
		Preload("Presentation.Medication").
		Preload("Batch.Presentation").
		Preload("Batch.Presentation.Medication").
		Preload("Batch.Presentation.Medication.Family").
		Preload("Batch").
		Where("presentation_id = ?", presentationID).
		Order("created_at DESC").
		Find(&movements).Error

	return movements, err
}

func (r *Repository) FindAllDispensations() ([]PharmacyDispensation, error) {
	var dispensations []PharmacyDispensation

	err := r.db.
		Preload("Presentation").
		Preload("Presentation.Medication").
		Preload("Presentation.Medication.Family").
		Preload("Items.Batch.Presentation").
		Preload("Items.Batch.Presentation.Medication").
		Preload("Items.Batch.Presentation.Medication.Family").
		Preload("Items").
		Preload("Items.Batch").
		Order("created_at DESC").
		Find(&dispensations).Error

	return dispensations, err
}

func (r *Repository) FindDispensationByID(
	id uint,
) (*PharmacyDispensation, error) {
	var dispensation PharmacyDispensation

	if err := r.db.
		Preload("Presentation").
		Preload("Presentation.Medication").
		Preload("Presentation.Medication.Family").
		Preload("Items").
		Preload("Items.Batch").
		Preload("Items.Batch.Presentation").
		Preload("Items.Batch.Presentation.Medication").
		Preload("Items.Batch.Presentation.Medication.Family").
		First(&dispensation, id).Error; err != nil {
		return nil, err
	}

	return &dispensation, nil
}

func (r *Repository) Dispense(
	dispensation *PharmacyDispensation,
) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var stock PharmacyStock

		if err := tx.
			Where("presentation_id = ?", dispensation.PresentationID).
			First(&stock).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrStockNotFound
			}

			return err
		}

		if !stock.IsStockManaged {
			return ErrStockNotManaged
		}

		if stock.QuantityAvailable < dispensation.Quantity {
			return ErrInsufficientStock
		}

		now := time.Now()

		var batches []PharmacyBatch

		if err := tx.
			Where(
				"presentation_id = ? AND is_active = ? AND quantity_remaining > 0",
				dispensation.PresentationID,
				true,
			).
			Where(
				"expiration_date IS NULL OR expiration_date >= ?",
				now,
			).
			Order("expiration_date ASC NULLS LAST").
			Order("created_at ASC").
			Find(&batches).Error; err != nil {
			return err
		}

		availableInBatches := float64(0)

		for _, batch := range batches {
			availableInBatches += batch.QuantityRemaining
		}

		if availableInBatches < dispensation.Quantity {
			return ErrInsufficientStock
		}

		if err := tx.Create(dispensation).Error; err != nil {
			return err
		}

		remainingToDispense := dispensation.Quantity
		currentStock := stock.QuantityAvailable

		for i := range batches {
			if remainingToDispense <= 0 {
				break
			}

			batch := &batches[i]

			quantityFromBatch := remainingToDispense

			if batch.QuantityRemaining < quantityFromBatch {
				quantityFromBatch = batch.QuantityRemaining
			}

			batchRemainingAfter := batch.QuantityRemaining - quantityFromBatch
			stockBefore := currentStock
			stockAfter := currentStock - quantityFromBatch

			if err := tx.
				Model(&PharmacyBatch{}).
				Where("id = ?", batch.ID).
				Update(
					"quantity_remaining",
					batchRemainingAfter,
				).Error; err != nil {
				return err
			}

			item := PharmacyDispensationItem{
				DispensationID: dispensation.ID,
				BatchID:        batch.ID,
				Quantity:       quantityFromBatch,
			}

			if err := tx.Create(&item).Error; err != nil {
				return err
			}

			movement := StockMovement{
				PresentationID:  dispensation.PresentationID,
				BatchID:         &batch.ID,
				Type:            StockMovementDispensation,
				Quantity:        quantityFromBatch,
				StockBefore:     stockBefore,
				StockAfter:      stockAfter,
				ReferenceType:   "PHARMACY_DISPENSATION",
				ReferenceID:     &dispensation.ID,
				Reason:          "Délivrance de médicament par la pharmacie",
				PerformedByID:   dispensation.DispensedByID,
				PerformedByName: dispensation.DispensedByName,
			}

			if err := tx.Create(&movement).Error; err != nil {
				return err
			}

			currentStock = stockAfter
			remainingToDispense -= quantityFromBatch
		}

		if err := tx.
			Model(&PharmacyStock{}).
			Where("id = ?", stock.ID).
			Update("quantity_available", currentStock).Error; err != nil {
			return err
		}

		return nil
	})
}

func (r *Repository) FindUserByID(id uint) (*auth.User, error) {
	var user auth.User

	if err := r.db.First(&user, id).Error; err != nil {
		return nil, err
	}

	return &user, nil
}

func (r *Repository) SumDispensedQuantityForPrescription(
	prescriptionID uint,
) (float64, error) {
	var total float64

	err := r.db.
		Model(&PharmacyDispensation{}).
		Where("reference_type = ?", "CONSULTATION_PRESCRIPTION").
		Where("reference_id = ?", prescriptionID).
		Select("COALESCE(SUM(quantity), 0)").
		Scan(&total).Error

	return total, err
}

func (r *Repository) FindConsultationPrescriptionByID(
	id uint,
) (*ConsultationPrescriptionRef, error) {
	var prescription ConsultationPrescriptionRef

	if err := r.db.First(&prescription, id).Error; err != nil {
		return nil, err
	}

	return &prescription, nil
}

func (r *Repository) FindAllConsultationPrescriptions() ([]ConsultationPrescriptionRef, error) {
	var prescriptions []ConsultationPrescriptionRef

	err := r.db.
		Where("presentation_id IS NOT NULL").
		Where("quantity > 0").
		Order("id DESC").
		Find(&prescriptions).Error

	return prescriptions, err
}
