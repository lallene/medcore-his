package performed_acts

import (
	"errors"
	"strings"
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/modules/act_catalog"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func isDuplicateKey(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, fragment := range []string{"duplicate key", "unique constraint", "sqlstate 23505", "ux_performed_acts_source"} {
		if strings.Contains(msg, fragment) {
			return true
		}
	}
	return false
}

// EnsureFromProducer creates a PerformedAct for a clinical producer event inside tx.
// Idempotent on (source_type, source_id): concurrent callers converge on one row.
// Does not require performed_acts.create RBAC — caller already authorized the clinical transition.
func (s *Service) EnsureFromProducer(tx *gorm.DB, req ProducerCreateRequest) (*Act, error) {
	if tx == nil {
		return nil, coreerrors.Internal("performed_acts producer: nil transaction")
	}
	if req.SourceID == 0 || req.PatientID == 0 || req.ActorID == 0 {
		return nil, coreerrors.BadRequest("Producteur: patient, source et acteur obligatoires")
	}
	if !ValidSourceTypes[req.SourceType] {
		return nil, coreerrors.BadRequest("Type de source producteur invalide")
	}

	qty := req.Quantity
	if qty == 0 {
		qty = 1
	}
	if qty <= 0 {
		return nil, coreerrors.BadRequest("La quantité doit être strictement positive")
	}
	if qty > 1_000_000 {
		return nil, coreerrors.BadRequest("La quantité dépasse la limite autorisée")
	}

	// Serialize with CreateInvoice(legacy) on the same clinical row when present.
	if err := lockProducerClinicalSource(tx, req.SourceType, req.SourceID); err != nil {
		return nil, err
	}

	performedAt := time.Now()
	if req.PerformedAt != nil {
		performedAt = *req.PerformedAt
	}

	// Fast path: already recorded.
	var existing Act
	err := tx.Where("source_type = ? AND source_id = ?", req.SourceType, req.SourceID).First(&existing).Error
	if err == nil {
		return acceptExistingProducerAct(&existing, req.PatientID)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	var mapping ProducerMap
	err = tx.Where(
		"source_type = ? AND clinical_key = ? AND is_active = ?",
		req.SourceType, req.ClinicalKey, true,
	).First(&mapping).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, coreerrors.Conflict("Aucune correspondance catalogue active pour ce producteur clinique")
		}
		return nil, err
	}

	var patient patients.Patient
	if err := tx.Select("id").First(&patient, req.PatientID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, coreerrors.NotFound("PATIENT")
		}
		return nil, err
	}

	var catalog act_catalog.Entry
	if err := tx.First(&catalog, mapping.ActCatalogEntryID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, coreerrors.NotFound("ACT_CATALOG")
		}
		return nil, err
	}
	if !catalog.IsActive {
		return nil, coreerrors.Conflict("L'acte catalogue mappé est inactif")
	}

	if err := s.validateOptionalContextTx(tx, req.PatientID, CreateRequest{
		PatientID:         req.PatientID,
		ConsultationID:    req.ConsultationID,
		AppointmentID:     req.AppointmentID,
		HospitalizationID: req.HospitalizationID,
	}); err != nil {
		return nil, err
	}

	sourceID := req.SourceID
	item := Act{
		PatientID:         req.PatientID,
		Quantity:          qty,
		PerformedAt:       performedAt,
		PerformedBy:       req.ActorID,
		Status:            StatusPerformed,
		ConsultationID:    req.ConsultationID,
		AppointmentID:     req.AppointmentID,
		HospitalizationID: req.HospitalizationID,
		SourceType:        req.SourceType,
		SourceID:          &sourceID,
		CreatedBy:         req.ActorID,
		UpdatedBy:         req.ActorID,
	}
	applyCatalogSnapshot(&item, catalog)

	useSavepoint := tx.Dialector.Name() == "postgres"
	if useSavepoint {
		if err := tx.Exec("SAVEPOINT ensure_performed_act").Error; err != nil {
			return nil, err
		}
	}
	if err := tx.Create(&item).Error; err != nil {
		if useSavepoint {
			_ = tx.Exec("ROLLBACK TO SAVEPOINT ensure_performed_act").Error
		}
		if isDuplicateKey(err) {
			var raced Act
			if findErr := tx.Where("source_type = ? AND source_id = ?", req.SourceType, req.SourceID).First(&raced).Error; findErr != nil {
				return nil, findErr
			}
			return acceptExistingProducerAct(&raced, req.PatientID)
		}
		return nil, err
	}
	if useSavepoint {
		_ = tx.Exec("RELEASE SAVEPOINT ensure_performed_act").Error
	}
	return &item, nil
}

func acceptExistingProducerAct(existing *Act, patientID uint) (*Act, error) {
	if existing == nil {
		return nil, coreerrors.Internal("performed_acts producer: nil existing act")
	}
	if existing.PatientID != patientID {
		return nil, coreerrors.Conflict("Acte réalisé existant pour une autre source patient")
	}
	if existing.Status != StatusPerformed {
		return nil, coreerrors.Conflict("Un acte réalisé VOIDED existe déjà pour cette source clinique")
	}
	return existing, nil
}

// lockProducerClinicalSource takes FOR UPDATE on the clinical source row when it
// exists so CreateInvoice(legacy) and producer PA creation cannot both commit.
// Missing source rows or missing tables (unit fixtures) are ignored — no lock needed.
func lockProducerClinicalSource(tx *gorm.DB, sourceType string, sourceID uint) error {
	table := ""
	switch sourceType {
	case SourceConsultation:
		table = "consultations"
	case SourceLaboratory:
		table = "laboratory_orders"
	case SourceImaging:
		table = "imaging_orders"
	default:
		return nil
	}
	useSavepoint := tx.Dialector.Name() == "postgres"
	if useSavepoint {
		if err := tx.Exec("SAVEPOINT pa_lock_clinical_source").Error; err != nil {
			return err
		}
	}
	var id uint
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Table(table).Select("id").Where("id = ?", sourceID).Take(&id).Error
	if err == nil {
		if useSavepoint {
			_ = tx.Exec("RELEASE SAVEPOINT pa_lock_clinical_source").Error
		}
		return nil
	}
	if useSavepoint {
		_ = tx.Exec("ROLLBACK TO SAVEPOINT pa_lock_clinical_source").Error
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "no such table") || strings.Contains(msg, "does not exist") {
		return nil
	}
	return err
}

func (s *Service) validateOptionalContextTx(tx *gorm.DB, patientID uint, req CreateRequest) error {
	if req.ConsultationID != nil {
		var n int64
		if err := tx.Table("consultations").Where("id = ? AND patient_id = ?", *req.ConsultationID, patientID).Count(&n).Error; err != nil {
			return err
		}
		if n == 0 {
			return coreerrors.NotFound("CONSULTATION")
		}
	}
	if req.AppointmentID != nil {
		var n int64
		if err := tx.Table("patient_queue_appointments").Where("id = ? AND patient_id = ?", *req.AppointmentID, patientID).Count(&n).Error; err != nil {
			return err
		}
		if n == 0 {
			return coreerrors.NotFound("APPOINTMENT")
		}
	}
	if req.HospitalizationID != nil {
		var n int64
		if err := tx.Table("hospitalizations").Where("id = ? AND patient_id = ?", *req.HospitalizationID, patientID).Count(&n).Error; err != nil {
			return err
		}
		if n == 0 {
			return coreerrors.NotFound("HOSPITALIZATION")
		}
	}
	return nil
}
