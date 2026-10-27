package grc

import (
	"errors"
	"strings"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *GRCService) validateSecurityIncidentRelations(current *SecurityIncident, r SecurityIncidentRequest, creating bool) error {
	riskChanged := creating || optionalIDChanged(current.RiskID, r.RiskID)
	if riskChanged && r.RiskID != nil {
		if err := s.db.First(&Risk{}, *r.RiskID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return coreerrors.NotFound("GRC_RISK")
			}
			return err
		}
	}
	if creating || optionalIDChanged(current.AssetID, r.AssetID) {
		if err := s.validateRiskAsset(r.AssetID); err != nil {
			return err
		}
	}
	if creating || optionalIDChanged(current.OwningServiceID, r.OwningServiceID) {
		if err := s.validateOwningService(r.OwningServiceID); err != nil {
			return err
		}
	}
	if creating || optionalIDChanged(current.OwnerProfileID, r.OwnerProfileID) {
		if err := s.validateRiskProfile(r.OwnerProfileID, "Le propriétaire de l'incident est inactif"); err != nil {
			return err
		}
	}
	return nil
}

func (s *GRCService) CreateSecurityIncident(actor uint, r SecurityIncidentRequest) (*SecurityIncident, error) {
	r.Status = SecurityIncidentStatusOpen
	normalizeSecurityIncidentRequest(&r)
	if err := validateSecurityIncidentRequest(r, s.currentTime(), true); err != nil {
		return nil, err
	}
	if err := s.validateSecurityIncidentRelations(nil, r, true); err != nil {
		return nil, err
	}

	item := SecurityIncident{
		Reference:       r.Reference,
		Title:           r.Title,
		Description:     r.Description,
		Severity:        r.Severity,
		Status:          SecurityIncidentStatusOpen,
		DetectedAt:      r.DetectedAt,
		OccurredAt:      r.OccurredAt,
		RiskID:          r.RiskID,
		AssetID:         r.AssetID,
		OwningServiceID: r.OwningServiceID,
		OwnerProfileID:  r.OwnerProfileID,
		CreatedBy:       actor,
		UpdatedBy:       actor,
	}
	if err := s.saveWithAudit(func(tx *gorm.DB) error {
		return tx.Create(&item).Error
	}, auditModuleGRCSecurityIncident, auditActionCreate, func() uint { return item.ID }, actor, "", func() string { return securityIncidentAuditValue(item) }); err != nil {
		if isDuplicateError(err) {
			return nil, coreerrors.Conflict("Référence d'incident déjà utilisée")
		}
		return nil, err
	}
	return s.FindSecurityIncident(item.ID)
}

func (s *GRCService) UpdateSecurityIncident(id, actor uint, r SecurityIncidentRequest) (*SecurityIncident, error) {
	normalizeSecurityIncidentRequest(&r)
	if err := validateSecurityIncidentRequest(r, s.currentTime(), false); err != nil {
		return nil, err
	}
	// Generic update never closes, even when the caller holds grc.incident.update
	// or the wildcard. The transition table still records RESOLVED -> CLOSED
	// so the dedicated route can allow it.
	if r.Status == SecurityIncidentStatusClosed {
		return nil, coreerrors.Forbidden("La clôture d'un incident de sécurité passe par la route dédiée /close")
	}

	now := s.currentTime()
	var saved SecurityIncident
	var oldValue string
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var current SecurityIncident
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return coreerrors.NotFound("GRC_SECURITY_INCIDENT")
			}
			return err
		}
		if err := validateSecurityIncidentTransition(current.Status, r.Status); err != nil {
			return err
		}
		if err := s.validateSecurityIncidentRelations(&current, r, false); err != nil {
			return err
		}

		resolvedAt, closedAt := securityIncidentTimestamps(current.Status, r.Status, current.ResolvedAt, now)
		oldValue = securityIncidentAuditValue(current)
		current.Reference = r.Reference
		current.Title = r.Title
		current.Description = r.Description
		current.Severity = r.Severity
		current.Status = r.Status
		current.DetectedAt = r.DetectedAt
		current.OccurredAt = r.OccurredAt
		current.ResolvedAt = resolvedAt
		current.ClosedAt = closedAt
		current.RiskID = r.RiskID
		current.AssetID = r.AssetID
		current.OwningServiceID = r.OwningServiceID
		current.OwnerProfileID = r.OwnerProfileID
		current.UpdatedBy = actor
		if err := tx.Select("*").Save(&current).Error; err != nil {
			if isDuplicateError(err) {
				return coreerrors.Conflict("Référence d'incident déjà utilisée")
			}
			return err
		}
		saved = current
		return writeGRCAudit(tx, auditModuleGRCSecurityIncident, auditActionUpdate, saved.ID, actor, oldValue, securityIncidentAuditValue(saved))
	})
	if err != nil {
		return nil, err
	}
	return s.FindSecurityIncident(saved.ID)
}

// CloseSecurityIncident is the only writer of CLOSED and closedAt. The
// incident must already be RESOLVED; resolvedAt is left unchanged.
func (s *GRCService) CloseSecurityIncident(id, actor uint) (*SecurityIncident, error) {
	now := s.currentTime().UTC()
	var saved SecurityIncident
	var oldValue string
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var current SecurityIncident
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return coreerrors.NotFound("GRC_SECURITY_INCIDENT")
			}
			return err
		}
		if current.Status != SecurityIncidentStatusResolved || current.ResolvedAt == nil {
			return coreerrors.Conflict("Seul un incident résolu peut être clôturé")
		}
		oldValue = securityIncidentAuditValue(current)
		current.Status = SecurityIncidentStatusClosed
		current.ClosedAt = &now
		current.UpdatedBy = actor
		if err := tx.Select("*").Save(&current).Error; err != nil {
			return err
		}
		saved = current
		return writeGRCAudit(tx, auditModuleGRCSecurityIncident, auditActionUpdate, saved.ID, actor, oldValue, securityIncidentAuditValue(saved))
	})
	if err != nil {
		return nil, err
	}
	return s.FindSecurityIncident(saved.ID)
}

func (s *GRCService) FindSecurityIncident(id uint) (*SecurityIncident, error) {
	if id == 0 {
		return nil, coreerrors.BadRequest("Identifiant d'incident invalide")
	}

	var item SecurityIncident
	err := s.preloadSecurityIncident(s.db).First(&item, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, coreerrors.NotFound("GRC_SECURITY_INCIDENT")
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *GRCService) preloadSecurityIncident(db *gorm.DB) *gorm.DB {
	return db.Preload("Risk").Preload("Asset").Preload("OwningService").Preload("OwnerProfile")
}

func (s *GRCService) ListSecurityIncidents(f SecurityIncidentFilter) (*SecurityIncidentPage, error) {
	f.Page, f.Limit = normalizePageFilter(f.Page, f.Limit)
	normalizeSecurityIncidentFilter(&f)
	if err := validateSecurityIncidentFilter(f); err != nil {
		return nil, err
	}

	query := s.db.Model(&SecurityIncident{})
	if f.Search != "" {
		like := "%" + strings.ToLower(f.Search) + "%"
		query = query.Where(
			"(LOWER(grc_security_incidents.reference) LIKE ? OR LOWER(grc_security_incidents.title) LIKE ?)",
			like, like,
		)
	}
	if f.Status != "" {
		query = query.Where("grc_security_incidents.status = ?", f.Status)
	}
	if f.Severity != "" {
		query = query.Where("grc_security_incidents.severity = ?", f.Severity)
	}
	if f.RiskID != nil {
		query = query.Where("grc_security_incidents.risk_id = ?", *f.RiskID)
	}
	if f.AssetID != nil {
		query = query.Where("grc_security_incidents.asset_id = ?", *f.AssetID)
	}
	if f.OwningServiceID != nil {
		query = query.Where("grc_security_incidents.owning_service_id = ?", *f.OwningServiceID)
	}
	if f.OwnerProfileID != nil {
		query = query.Where("grc_security_incidents.owner_profile_id = ?", *f.OwnerProfileID)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}

	items := []SecurityIncident{}
	if err := s.preloadSecurityIncident(query).
		Order("grc_security_incidents.detected_at DESC, grc_security_incidents.id DESC").
		Offset((f.Page - 1) * f.Limit).
		Limit(f.Limit).
		Find(&items).Error; err != nil {
		return nil, err
	}

	return &SecurityIncidentPage{
		Items:      items,
		Page:       f.Page,
		Limit:      f.Limit,
		Total:      total,
		TotalPages: totalPages(total, f.Limit),
	}, nil
}
