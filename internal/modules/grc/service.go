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
	normalizeAssetRequest(&r)
	if err := validateAssetRequest(r); err != nil {
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

	// Relations are validated when assigned only, so an asset stays editable
	// (for example to retire it) after its owner or service was deactivated.
	if id == 0 || optionalIDChanged(asset.OwningServiceID, r.OwningServiceID) {
		if err := s.validateOwningService(r.OwningServiceID); err != nil {
			return nil, err
		}
	}
	if id == 0 || optionalIDChanged(asset.OwnerProfileID, r.OwnerProfileID) {
		if err := s.validateOwnerProfile(r.OwnerProfileID); err != nil {
			return nil, err
		}
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

	if err := s.saveWithAudit(func(tx *gorm.DB) error {
		return tx.Select("*").Save(&asset).Error
	}, auditModuleGRCAsset, action, func() uint { return asset.ID }, actor, oldAuditValue, func() string { return assetAuditValue(asset) }); err != nil {
		if isDuplicateError(err) {
			return nil, coreerrors.Conflict("Code actif déjà utilisé")
		}
		return nil, err
	}

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
	f.Page, f.Limit = normalizePageFilter(f.Page, f.Limit)

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
		TotalPages: totalPages(total, f.Limit),
	}, nil
}

func (s *GRCService) SaveRisk(id, actor uint, r RiskRequest) (*Risk, error) {
	normalizeRiskRequest(&r)
	if err := validateRiskRequest(r); err != nil {
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

	// Relations are validated when assigned only, so a risk stays editable
	// after its asset was retired or its owner, approver or service was
	// deactivated.
	if id == 0 || optionalIDChanged(risk.AssetID, r.AssetID) {
		if err := s.validateRiskAsset(r.AssetID); err != nil {
			return nil, err
		}
	}
	if id == 0 || optionalIDChanged(risk.OwningServiceID, r.OwningServiceID) {
		if err := s.validateOwningService(r.OwningServiceID); err != nil {
			return nil, err
		}
	}
	if id == 0 || optionalIDChanged(risk.OwnerProfileID, r.OwnerProfileID) {
		if err := s.validateRiskProfile(r.OwnerProfileID, "Le propriétaire du risque est inactif"); err != nil {
			return nil, err
		}
	}
	if id == 0 || optionalIDChanged(risk.ApproverProfileID, r.ApproverProfileID) {
		if err := s.validateRiskProfile(r.ApproverProfileID, "L'approbateur du risque est inactif"); err != nil {
			return nil, err
		}
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

	if err := s.saveWithAudit(func(tx *gorm.DB) error {
		return tx.Select("*").Save(&risk).Error
	}, auditModuleGRCRisk, action, func() uint { return risk.ID }, actor, oldAuditValue, func() string { return riskAuditValue(risk) }); err != nil {
		if isDuplicateError(err) {
			return nil, coreerrors.Conflict("Référence risque déjà utilisée")
		}
		return nil, err
	}

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
	if err := fillRiskLevels(&risk); err != nil {
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
	filter.Page, filter.Limit = normalizePageFilter(filter.Page, filter.Limit)

	filter.Status = normalizeEnum(filter.Status)
	filter.Treatment = normalizeEnum(filter.Treatment)
	filter.InherentLevel = normalizeEnum(filter.InherentLevel)
	filter.ResidualLevel = normalizeEnum(filter.ResidualLevel)

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
			`(LOWER(reference) LIKE ? OR
			 LOWER(title) LIKE ? OR
			 LOWER(description) LIKE ? OR
			 LOWER(threat) LIKE ? OR
			 LOWER(vulnerability) LIKE ?)`,
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
	if filter.ControlID != nil {
		query = query.Where("EXISTS (SELECT 1 FROM grc_control_risks crf WHERE crf.risk_id = grc_risks.id AND crf.control_id = ?)", *filter.ControlID)
	}
	for column, level := range map[string]string{"inherent_score": filter.InherentLevel, "residual_score": filter.ResidualLevel} {
		if level == "" {
			continue
		}
		scores, err := riskLevelScores(level)
		if err != nil {
			return RiskPage{}, err
		}
		query = query.Where(column+" IN ?", scores)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return RiskPage{}, err
	}

	risks := []Risk{}
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

	for i := range risks {
		if err := fillRiskLevels(&risks[i]); err != nil {
			return RiskPage{}, err
		}
	}

	return RiskPage{
		Items:      risks,
		Page:       filter.Page,
		Limit:      filter.Limit,
		Total:      total,
		TotalPages: totalPages(total, filter.Limit),
	}, nil
}
