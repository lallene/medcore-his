package performed_acts

import (
	"errors"
	"strings"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/modules/act_catalog"
	"gorm.io/gorm"
)

func normalizeSourceType(raw string) string {
	return strings.ToUpper(strings.TrimSpace(raw))
}

func normalizeClinicalKey(sourceType, raw string) (string, error) {
	key := strings.TrimSpace(raw)
	switch sourceType {
	case SourceConsultation:
		if key != ConsultationClinicalKey {
			return "", coreerrors.BadRequest("CONSULTATION clinicalKey doit être la clé canonique vide")
		}
		return ConsultationClinicalKey, nil
	case SourceLaboratory, SourceImaging:
		if key == "" {
			return "", coreerrors.BadRequest("clinicalKey (medical_exams.code) obligatoire")
		}
		return key, nil
	default:
		return "", coreerrors.BadRequest("Type de source producteur invalide")
	}
}

func (s *Service) ListProducerMaps() ([]ProducerMap, error) {
	var rows []ProducerMap
	if err := s.db.Order("source_type ASC, clinical_key ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *Service) GetProducerMap(id uint) (*ProducerMap, error) {
	var row ProducerMap
	if err := s.db.First(&row, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, coreerrors.NotFound("PRODUCER_MAP")
		}
		return nil, err
	}
	return &row, nil
}

func (s *Service) UpsertProducerMap(req UpsertProducerMapRequest, actorID uint) (*ProducerMap, error) {
	sourceType := normalizeSourceType(req.SourceType)
	if !ValidSourceTypes[sourceType] {
		return nil, coreerrors.BadRequest("Type de source producteur invalide")
	}
	clinicalKey, err := normalizeClinicalKey(sourceType, req.ClinicalKey)
	if err != nil {
		return nil, err
	}
	if req.ActCatalogEntryID == 0 {
		return nil, coreerrors.BadRequest("Acte catalogue obligatoire")
	}
	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}

	if err := s.validateCatalogTarget(req.ActCatalogEntryID, isActive); err != nil {
		return nil, err
	}

	var existing ProducerMap
	err = s.db.Where("source_type = ? AND clinical_key = ?", sourceType, clinicalKey).First(&existing).Error
	if err == nil {
		existing.ActCatalogEntryID = req.ActCatalogEntryID
		existing.IsActive = isActive
		existing.UpdatedBy = actorID
		if err := s.db.Save(&existing).Error; err != nil {
			return nil, err
		}
		return &existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	row := ProducerMap{
		SourceType:        sourceType,
		ClinicalKey:       clinicalKey,
		ActCatalogEntryID: req.ActCatalogEntryID,
		IsActive:          isActive,
		CreatedBy:         actorID,
		UpdatedBy:         actorID,
	}
	if err := s.db.Create(&row).Error; err != nil {
		if isDuplicateKey(err) {
			return nil, coreerrors.Conflict("Correspondance producteur déjà existante")
		}
		return nil, err
	}
	return &row, nil
}

func (s *Service) UpdateProducerMap(id uint, req UpdateProducerMapRequest, actorID uint) (*ProducerMap, error) {
	row, err := s.GetProducerMap(id)
	if err != nil {
		return nil, err
	}
	if req.ActCatalogEntryID == 0 {
		return nil, coreerrors.BadRequest("Acte catalogue obligatoire")
	}
	isActive := row.IsActive
	if req.IsActive != nil {
		isActive = *req.IsActive
	}
	if err := s.validateCatalogTarget(req.ActCatalogEntryID, isActive); err != nil {
		return nil, err
	}
	row.ActCatalogEntryID = req.ActCatalogEntryID
	row.IsActive = isActive
	row.UpdatedBy = actorID
	if err := s.db.Save(row).Error; err != nil {
		return nil, err
	}
	return row, nil
}

func (s *Service) DeactivateProducerMap(id uint, actorID uint) (*ProducerMap, error) {
	row, err := s.GetProducerMap(id)
	if err != nil {
		return nil, err
	}
	inactive := false
	return s.UpdateProducerMap(id, UpdateProducerMapRequest{
		ActCatalogEntryID: row.ActCatalogEntryID,
		IsActive:          &inactive,
	}, actorID)
}

func (s *Service) validateCatalogTarget(entryID uint, mapActive bool) error {
	var catalog act_catalog.Entry
	if err := s.db.First(&catalog, entryID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return coreerrors.NotFound("ACT_CATALOG")
		}
		return err
	}
	if mapActive && !catalog.IsActive {
		return coreerrors.Conflict("Impossible d'activer une correspondance vers un acte catalogue inactif")
	}
	return nil
}
