package grc

import (
	"errors"
	"strings"
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/modules/organization"
	"github.com/lallene/medcore-his/backend/internal/modules/staff"
	"gorm.io/gorm"
)

type GRCService struct {
	db  *gorm.DB
	now func() time.Time
}

func NewService(db *gorm.DB) *GRCService {
	return &GRCService{db: db, now: time.Now}
}

// currentTime falls back to time.Now when the service was built without
// NewService.
func (s *GRCService) currentTime() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

func (s *GRCService) SaveAsset(id, actor uint, r AssetRequest) (*Asset, error) {
	r.Code = normalizeCode(r.Code)
	r.Name = normalizeText(r.Name)
	r.Type = normalizeEnum(r.Type)
	r.Description = normalizeText(r.Description)
	r.Criticality = normalizeEnum(r.Criticality)
	r.Status = normalizeEnum(r.Status)

	if r.Code == "" || r.Name == "" {
		return nil, coreerrors.BadRequest("Code et nom de l'actif obligatoires")
	}
	if !isValidAssetType(r.Type) {
		return nil, coreerrors.BadRequest("Type d'actif invalide")
	}
	if !isValidCriticality(r.Criticality) {
		return nil, coreerrors.BadRequest("Criticité de l'actif invalide")
	}
	if !isValidAssetStatus(r.Status) {
		return nil, coreerrors.BadRequest("Statut de l'actif invalide")
	}

	if err := s.validateOwningService(r.OwningServiceID); err != nil {
		return nil, err
	}
	if err := s.validateOwnerProfile(r.OwnerProfileID); err != nil {
		return nil, err
	}

	var asset Asset
	var oldAuditValue string
	action := auditActionCreate

	if id > 0 {
		if err := s.db.First(&asset, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, coreerrors.NotFound("GRC_ASSET")
			}
			return nil, err
		}

		action = auditActionUpdate
		oldAuditValue = assetAuditValue(asset)
	} else {
		asset.CreatedBy = actor
	}

	asset.Code = r.Code
	asset.Name = r.Name
	asset.Type = r.Type
	asset.Description = r.Description
	asset.Criticality = r.Criticality
	asset.OwningServiceID = r.OwningServiceID
	asset.OwnerProfileID = r.OwnerProfileID
	asset.Status = r.Status
	asset.UpdatedBy = actor

	if err := s.db.Select("*").Save(&asset).Error; err != nil {
		if isDuplicateError(err) {
			return nil, coreerrors.Conflict("Code actif déjà utilisé")
		}
		return nil, err
	}

	publishGRCAudit(
		auditModuleGRCAsset,
		action,
		asset.ID,
		actor,
		oldAuditValue,
		assetAuditValue(asset),
	)

	return s.FindAsset(asset.ID)
}

func (s *GRCService) FindAsset(id uint) (*Asset, error) {
	var asset Asset

	err := s.db.
		Preload("OwningService").
		Preload("OwnerProfile").
		First(&asset, id).
		Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, coreerrors.NotFound("GRC_ASSET")
	}
	if err != nil {
		return nil, err
	}

	return &asset, nil
}

func (s *GRCService) validateOwningService(id *uint) error {
	if id == nil {
		return nil
	}

	var service organization.Service
	if err := s.db.First(&service, *id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return coreerrors.NotFound("SERVICE")
		}
		return err
	}
	if !service.Active {
		return coreerrors.Conflict("Le service propriétaire est inactif")
	}

	return nil
}

func (s *GRCService) validateOwnerProfile(id *uint) error {
	if id == nil {
		return nil
	}

	var profile staff.Profile
	if err := s.db.First(&profile, *id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return coreerrors.NotFound("STAFF_PROFILE")
		}
		return err
	}
	if !profile.Active {
		return coreerrors.Conflict("Le propriétaire de l'actif est inactif")
	}

	return nil
}

func isDuplicateError(err error) bool {
	if err == nil {
		return false
	}

	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique constraint") ||
		strings.Contains(message, "duplicate")
}

func (s *GRCService) ListAssets(f AssetFilter) (*AssetPage, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.Limit < 1 || f.Limit > 100 {
		f.Limit = 20
	}

	f.Type = normalizeEnum(f.Type)
	f.Criticality = normalizeEnum(f.Criticality)
	f.Status = normalizeEnum(f.Status)

	if f.Type != "" && !isValidAssetType(f.Type) {
		return nil, coreerrors.BadRequest("Type d'actif invalide")
	}
	if f.Criticality != "" && !isValidCriticality(f.Criticality) {
		return nil, coreerrors.BadRequest("Criticité de l'actif invalide")
	}
	if f.Status != "" && !isValidAssetStatus(f.Status) {
		return nil, coreerrors.BadRequest("Statut de l'actif invalide")
	}

	q := s.db.Model(&Asset{})

	if search := strings.TrimSpace(f.Search); search != "" {
		like := "%" + strings.ToLower(search) + "%"
		q = q.Where(
			"(LOWER(code) LIKE ? OR LOWER(name) LIKE ? OR LOWER(description) LIKE ?)",
			like,
			like,
			like,
		)
	}

	if f.Type != "" {
		q = q.Where("type = ?", f.Type)
	}
	if f.Criticality != "" {
		q = q.Where("criticality = ?", f.Criticality)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.ServiceID != nil {
		q = q.Where("owning_service_id = ?", *f.ServiceID)
	}
	if f.OwnerProfileID != nil {
		q = q.Where("owner_profile_id = ?", *f.OwnerProfileID)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, err
	}

	assets := []Asset{}
	if err := q.
		Preload("OwningService").
		Preload("OwnerProfile").
		Order("name, id").
		Offset((f.Page - 1) * f.Limit).
		Limit(f.Limit).
		Find(&assets).
		Error; err != nil {
		return nil, err
	}

	return &AssetPage{
		Items:      assets,
		Page:       f.Page,
		Limit:      f.Limit,
		Total:      total,
		TotalPages: int((total + int64(f.Limit) - 1) / int64(f.Limit)),
	}, nil
}

func (s *GRCService) SaveRisk(id, actor uint, r RiskRequest) (*Risk, error) {
	r.Reference = normalizeCode(r.Reference)
	r.Title = normalizeText(r.Title)
	r.Description = normalizeText(r.Description)
	r.Threat = normalizeText(r.Threat)
	r.Vulnerability = normalizeText(r.Vulnerability)
	r.Treatment = normalizeEnum(r.Treatment)
	r.Status = normalizeEnum(r.Status)

	if r.Reference == "" || r.Title == "" {
		return nil, coreerrors.BadRequest("Référence et titre du risque obligatoires")
	}
	if !isValidRiskTreatment(r.Treatment) {
		return nil, coreerrors.BadRequest("Traitement du risque invalide")
	}
	if !isValidRiskStatus(r.Status) {
		return nil, coreerrors.BadRequest("Statut du risque invalide")
	}

	if err := s.validateRiskAsset(r.AssetID); err != nil {
		return nil, err
	}
	if err := s.validateOwningService(r.OwningServiceID); err != nil {
		return nil, err
	}
	if err := s.validateRiskProfile(r.OwnerProfileID, "Le propriétaire du risque est inactif"); err != nil {
		return nil, err
	}
	if err := s.validateRiskProfile(r.ApproverProfileID, "L'approbateur du risque est inactif"); err != nil {
		return nil, err
	}

	var risk Risk
	var oldAuditValue string
	action := auditActionCreate

	if id > 0 {
		if err := s.db.First(&risk, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, coreerrors.NotFound("GRC_RISK")
			}
			return nil, err
		}

		action = auditActionUpdate
		oldAuditValue = riskAuditValue(risk)
	} else {
		risk.CreatedBy = actor
	}

	risk.Reference = r.Reference
	risk.Title = r.Title
	risk.Description = r.Description
	risk.AssetID = r.AssetID
	risk.OwningServiceID = r.OwningServiceID
	risk.OwnerProfileID = r.OwnerProfileID
	risk.ApproverProfileID = r.ApproverProfileID
	risk.Threat = r.Threat
	risk.Vulnerability = r.Vulnerability
	risk.Likelihood = r.Likelihood
	risk.Impact = r.Impact
	risk.ResidualLikelihood = r.ResidualLikelihood
	risk.ResidualImpact = r.ResidualImpact
	risk.Treatment = r.Treatment
	risk.TreatmentDueAt = r.TreatmentDueAt
	risk.Status = r.Status
	risk.UpdatedBy = actor

	if err := risk.RecalculateScores(); err != nil {
		return nil, coreerrors.BadRequest(err.Error())
	}

	if err := s.db.Select("*").Save(&risk).Error; err != nil {
		if isDuplicateError(err) {
			return nil, coreerrors.Conflict("Référence risque déjà utilisée")
		}
		return nil, err
	}

	publishGRCAudit(
		auditModuleGRCRisk,
		action,
		risk.ID,
		actor,
		oldAuditValue,
		riskAuditValue(risk),
	)

	return s.FindRisk(risk.ID)
}

func (s *GRCService) FindRisk(id uint) (*Risk, error) {
	var risk Risk

	err := s.db.
		Preload("Asset").
		Preload("OwningService").
		Preload("OwnerProfile").
		Preload("ApproverProfile").
		First(&risk, id).
		Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, coreerrors.NotFound("GRC_RISK")
	}
	if err != nil {
		return nil, err
	}

	return &risk, nil
}

func (s *GRCService) validateRiskAsset(id *uint) error {
	if id == nil {
		return nil
	}

	var asset Asset
	if err := s.db.First(&asset, *id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return coreerrors.NotFound("GRC_ASSET")
		}
		return err
	}

	if asset.Status != AssetStatusActive {
		return coreerrors.Conflict("L'actif associé au risque est inactif")
	}

	return nil
}

func (s *GRCService) validateRiskProfile(id *uint, inactiveMessage string) error {
	if id == nil {
		return nil
	}

	var profile staff.Profile
	if err := s.db.First(&profile, *id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return coreerrors.NotFound("STAFF_PROFILE")
		}
		return err
	}

	if !profile.Active {
		return coreerrors.Conflict(inactiveMessage)
	}

	return nil
}

func (s *GRCService) ListRisks(filter RiskFilter) (RiskPage, error) {
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.Limit < 1 || filter.Limit > 100 {
		filter.Limit = 20
	}

	filter.Status = normalizeEnum(filter.Status)
	filter.Treatment = normalizeEnum(filter.Treatment)

	if filter.Status != "" && !isValidRiskStatus(filter.Status) {
		return RiskPage{}, coreerrors.BadRequest("Statut du risque invalide")
	}
	if filter.Treatment != "" && !isValidRiskTreatment(filter.Treatment) {
		return RiskPage{}, coreerrors.BadRequest("Traitement du risque invalide")
	}

	query := s.db.Model(&Risk{})

	if search := normalizeText(filter.Search); search != "" {
		pattern := "%" + strings.ToLower(search) + "%"
		query = query.Where(
			`LOWER(reference) LIKE ? OR
			 LOWER(title) LIKE ? OR
			 LOWER(description) LIKE ? OR
			 LOWER(threat) LIKE ? OR
			 LOWER(vulnerability) LIKE ?`,
			pattern,
			pattern,
			pattern,
			pattern,
			pattern,
		)
	}

	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.Treatment != "" {
		query = query.Where("treatment = ?", filter.Treatment)
	}
	if filter.AssetID != nil {
		query = query.Where("asset_id = ?", *filter.AssetID)
	}
	if filter.ServiceID != nil {
		query = query.Where("owning_service_id = ?", *filter.ServiceID)
	}
	if filter.OwnerProfileID != nil {
		query = query.Where("owner_profile_id = ?", *filter.OwnerProfileID)
	}
	if filter.ApproverProfileID != nil {
		query = query.Where("approver_profile_id = ?", *filter.ApproverProfileID)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return RiskPage{}, err
	}

	var risks []Risk
	if err := query.
		Preload("Asset").
		Preload("OwningService").
		Preload("OwnerProfile").
		Preload("ApproverProfile").
		Order("reference, id").
		Offset((filter.Page - 1) * filter.Limit).
		Limit(filter.Limit).
		Find(&risks).
		Error; err != nil {
		return RiskPage{}, err
	}

	totalPages := 0
	if total > 0 {
		totalPages = int((total + int64(filter.Limit) - 1) / int64(filter.Limit))
	}

	return RiskPage{
		Items:      risks,
		Page:       filter.Page,
		Limit:      filter.Limit,
		Total:      total,
		TotalPages: totalPages,
	}, nil
}
