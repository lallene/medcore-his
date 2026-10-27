package grc

import (
	"errors"
	"strings"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"gorm.io/gorm"
)

const auditEngagementOrderSQL = "CASE WHEN planned_start_at IS NULL THEN 1 ELSE 0 END, planned_start_at, id"

func (s *GRCService) SaveAuditEngagement(id, actor uint, r AuditEngagementRequest) (*AuditEngagement, error) {
	normalizeAuditEngagementRequest(&r)
	if err := validateAuditEngagementRequest(r); err != nil {
		return nil, err
	}

	now := s.currentTime()
	if r.StartedAt != nil && r.StartedAt.After(now) {
		return nil, coreerrors.BadRequest("La date de début réelle de l'audit ne peut pas être dans le futur")
	}
	if r.Status == AuditEngagementStatusPlanned && r.StartedAt != nil {
		return nil, coreerrors.BadRequest("Un audit planifié n'a pas encore de date de début réelle")
	}

	var audit AuditEngagement
	var oldAuditValue string
	action := auditActionCreate

	if id > 0 {
		if err := s.db.First(&audit, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, coreerrors.NotFound("GRC_AUDIT_ENGAGEMENT")
			}
			return nil, err
		}
		if err := validateAuditEngagementTransition(audit.Status, r.Status); err != nil {
			return nil, err
		}

		// Serialized before any mutation of audit.
		action = auditActionUpdate
		oldAuditValue = auditEngagementAuditValue(audit)
	} else {
		if err := validateAuditEngagementInitialStatus(r.Status); err != nil {
			return nil, err
		}
		audit.CreatedBy = actor
	}

	if id == 0 || optionalIDChanged(audit.LeadAuditorProfileID, r.LeadAuditorProfileID) {
		if err := s.validateRiskProfile(r.LeadAuditorProfileID, "Le responsable de l'audit est inactif"); err != nil {
			return nil, err
		}
	}
	if id == 0 || optionalIDChanged(audit.OwningServiceID, r.OwningServiceID) {
		if err := s.validateOwningService(r.OwningServiceID); err != nil {
			return nil, err
		}
	}

	// StartedAt is a business date: kept when omitted, defaulted to now once
	// fieldwork starts. CompletedAt is server-owned and set on completion only.
	startedAt := r.StartedAt
	if startedAt == nil && r.Status != AuditEngagementStatusPlanned {
		startedAt = audit.StartedAt
	}
	if startedAt == nil &&
		(r.Status == AuditEngagementStatusInProgress || r.Status == AuditEngagementStatusCompleted) {
		startedAt = &now
	}
	completedAt := audit.CompletedAt
	if r.Status == AuditEngagementStatusCompleted && audit.Status != AuditEngagementStatusCompleted {
		completedAt = &now
	}

	audit.Reference = r.Reference
	audit.Title = r.Title
	audit.Description = r.Description
	audit.Scope = r.Scope
	audit.Type = r.Type
	audit.Status = r.Status
	audit.LeadAuditorProfileID = r.LeadAuditorProfileID
	audit.OwningServiceID = r.OwningServiceID
	audit.PlannedStartAt = r.PlannedStartAt
	audit.PlannedEndAt = r.PlannedEndAt
	audit.StartedAt = startedAt
	audit.CompletedAt = completedAt
	audit.UpdatedBy = actor

	if err := s.saveWithAudit(func(tx *gorm.DB) error {
		return tx.Select("*").Save(&audit).Error
	}, auditModuleGRCAuditEngagement, action, func() uint { return audit.ID }, actor, oldAuditValue, func() string { return auditEngagementAuditValue(audit) }); err != nil {
		if isDuplicateError(err) {
			return nil, coreerrors.Conflict("Référence d'audit déjà utilisée")
		}
		return nil, err
	}

	return s.FindAuditEngagement(audit.ID)
}

func preloadAuditEngagementRelations(q *gorm.DB) *gorm.DB {
	return q.Preload("LeadAuditorProfile").Preload("OwningService")
}

func (s *GRCService) FindAuditEngagement(id uint) (*AuditEngagement, error) {
	if id == 0 {
		return nil, coreerrors.BadRequest("Identifiant d'audit invalide")
	}

	var audit AuditEngagement
	err := preloadAuditEngagementRelations(s.db).First(&audit, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, coreerrors.NotFound("GRC_AUDIT_ENGAGEMENT")
	}
	if err != nil {
		return nil, err
	}
	single := []AuditEngagement{audit}
	if err := s.fillAuditFindingCounts(single, s.currentTime()); err != nil {
		return nil, err
	}
	return &single[0], nil
}

func (s *GRCService) ListAuditEngagements(f AuditEngagementFilter) (*AuditEngagementPage, error) {
	f.Page, f.Limit = normalizePageFilter(f.Page, f.Limit)
	normalizeAuditEngagementFilter(&f)
	if err := validateAuditEngagementFilter(f); err != nil {
		return nil, err
	}

	q := s.db.Model(&AuditEngagement{})

	if search := normalizeText(f.Search); search != "" {
		like := "%" + strings.ToLower(search) + "%"
		q = q.Where("(LOWER(reference) LIKE ? OR LOWER(title) LIKE ?)", like, like)
	}
	if f.Type != "" {
		q = q.Where("type = ?", f.Type)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.LeadAuditorProfileID != nil {
		q = q.Where("lead_auditor_profile_id = ?", *f.LeadAuditorProfileID)
	}
	if f.OwningServiceID != nil {
		q = q.Where("owning_service_id = ?", *f.OwningServiceID)
	}
	if f.PlannedStartAfter != nil {
		q = q.Where("planned_start_at >= ?", *f.PlannedStartAfter)
	}
	if f.PlannedStartBefore != nil {
		q = q.Where("planned_start_at <= ?", *f.PlannedStartBefore)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, err
	}

	items := []AuditEngagement{}
	if err := preloadAuditEngagementRelations(q).
		Order(auditEngagementOrderSQL).
		Offset((f.Page - 1) * f.Limit).
		Limit(f.Limit).
		Find(&items).
		Error; err != nil {
		return nil, err
	}
	if err := s.fillAuditFindingCounts(items, s.currentTime()); err != nil {
		return nil, err
	}

	return &AuditEngagementPage{
		Items:      items,
		Page:       f.Page,
		Limit:      f.Limit,
		Total:      total,
		TotalPages: totalPages(total, f.Limit),
	}, nil
}
