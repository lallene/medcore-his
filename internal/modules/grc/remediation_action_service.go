package grc

import (
	"errors"
	"strings"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"gorm.io/gorm"
)

// remediationActionOverdueSQL mirrors RemediationActionOverdue and
// remediationActionNotOverdueSQL is its exact NULL-safe complement. Both bind
// now as a parameter so the filter and the computed field share one instant.
const (
	remediationActionOverdueSQL    = "(due_at IS NOT NULL AND due_at < ? AND status NOT IN (?, ?))"
	remediationActionNotOverdueSQL = "(due_at IS NULL OR due_at >= ? OR status IN (?, ?))"
	remediationActionOrderSQL      = "CASE WHEN due_at IS NULL THEN 1 ELSE 0 END, due_at, id"
)

func (s *GRCService) SaveRemediationAction(id, actor uint, r RemediationActionRequest) (*RemediationAction, error) {
	normalizeRemediationActionRequest(&r)
	if err := validateRemediationActionRequest(r); err != nil {
		return nil, err
	}

	var action RemediationAction
	var oldAuditValue string
	auditAction := auditActionCreate

	if id > 0 {
		if err := s.db.First(&action, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, coreerrors.NotFound("GRC_REMEDIATION_ACTION")
			}
			return nil, err
		}

		// Serialized before any mutation of action.
		auditAction = auditActionUpdate
		oldAuditValue = remediationActionAuditValue(action)
	} else {
		action.CreatedBy = actor
	}

	// Relations are validated when assigned only, so a historical action stays
	// editable after a target link, assignee or service has changed.
	implementationChanged := id == 0 || optionalIDChanged(action.ControlImplementationID, r.ControlImplementationID)
	riskChanged := id == 0 || optionalIDChanged(action.RiskID, r.RiskID)

	var implementation *ControlImplementation
	if r.ControlImplementationID != nil && (implementationChanged || (riskChanged && r.RiskID != nil)) {
		loaded, err := s.loadRemediationActionImplementation(*r.ControlImplementationID)
		if err != nil {
			return nil, err
		}
		implementation = loaded
	}
	if riskChanged && r.RiskID != nil {
		if err := s.db.First(&Risk{}, *r.RiskID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, coreerrors.NotFound("GRC_RISK")
			}
			return nil, err
		}
	}
	if implementation != nil && r.RiskID != nil {
		if err := s.validateRemediationActionControlRisk(implementation.ControlID, *r.RiskID); err != nil {
			return nil, err
		}
	}

	if id == 0 || optionalIDChanged(action.AssigneeProfileID, r.AssigneeProfileID) {
		if err := s.validateRiskProfile(r.AssigneeProfileID, "L'assigné de l'action est inactif"); err != nil {
			return nil, err
		}
	}
	if id == 0 || optionalIDChanged(action.OwningServiceID, r.OwningServiceID) {
		if err := s.validateOwningService(r.OwningServiceID); err != nil {
			return nil, err
		}
	}

	// The model sent to Save must satisfy grc_remediation_action_completion:
	// (status = 'DONE') = (completed_at IS NOT NULL).
	if r.Status == RemediationActionStatusDone {
		now := s.currentTime()
		switch {
		case r.CompletedAt == nil && action.Status == RemediationActionStatusDone && action.CompletedAt != nil:
			// Editing an already completed action keeps its completion date.
			r.CompletedAt = action.CompletedAt
		case r.CompletedAt == nil:
			r.CompletedAt = &now
		case r.CompletedAt.After(now):
			return nil, coreerrors.BadRequest("La date de réalisation de l'action ne peut pas être dans le futur")
		}
	} else {
		r.CompletedAt = nil
	}

	action.Reference = r.Reference
	action.Title = r.Title
	action.Description = r.Description
	action.ControlImplementationID = r.ControlImplementationID
	action.RiskID = r.RiskID
	action.AssigneeProfileID = r.AssigneeProfileID
	action.OwningServiceID = r.OwningServiceID
	action.Priority = r.Priority
	action.Status = r.Status
	action.DueAt = r.DueAt
	action.CompletedAt = r.CompletedAt
	action.UpdatedBy = actor

	if err := s.db.Select("*").Save(&action).Error; err != nil {
		if isDuplicateError(err) {
			return nil, coreerrors.Conflict("Référence d'action déjà utilisée")
		}
		return nil, err
	}

	publishGRCAudit(
		auditModuleGRCRemediationAction,
		auditAction,
		action.ID,
		actor,
		oldAuditValue,
		remediationActionAuditValue(action),
	)

	return s.FindRemediationAction(action.ID)
}

func (s *GRCService) loadRemediationActionImplementation(id uint) (*ControlImplementation, error) {
	var implementation ControlImplementation
	if err := s.db.First(&implementation, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, coreerrors.NotFound("GRC_CONTROL_IMPLEMENTATION")
		}
		return nil, err
	}
	return &implementation, nil
}

// validateRemediationActionControlRisk requires an existing control-risk link
// when an action targets both a control implementation and a risk.
func (s *GRCService) validateRemediationActionControlRisk(controlID, riskID uint) error {
	var links int64
	if err := s.db.Model(&ControlRisk{}).
		Where("control_id = ? AND risk_id = ?", controlID, riskID).
		Count(&links).
		Error; err != nil {
		return err
	}
	if links == 0 {
		return coreerrors.Conflict("Le contrôle de l'implémentation n'est pas lié à ce risque")
	}
	return nil
}

func preloadRemediationActionRelations(q *gorm.DB) *gorm.DB {
	return q.
		Preload("ControlImplementation").
		Preload("ControlImplementation.Control").
		Preload("ControlImplementation.Asset").
		Preload("Risk").
		Preload("AssigneeProfile").
		Preload("OwningService")
}

func (s *GRCService) FindRemediationAction(id uint) (*RemediationAction, error) {
	if id == 0 {
		return nil, coreerrors.BadRequest("Identifiant d'action invalide")
	}

	var action RemediationAction
	err := preloadRemediationActionRelations(s.db).First(&action, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, coreerrors.NotFound("GRC_REMEDIATION_ACTION")
	}
	if err != nil {
		return nil, err
	}

	action.Overdue = RemediationActionOverdue(action.Status, action.DueAt, s.currentTime())
	return &action, nil
}

func (s *GRCService) ListRemediationActions(f RemediationActionFilter) (*RemediationActionPage, error) {
	f.Page, f.Limit = normalizePageFilter(f.Page, f.Limit)
	f.Priority = normalizeEnum(f.Priority)
	f.Status = normalizeEnum(f.Status)

	if f.Priority != "" && !isValidRemediationActionPriority(f.Priority) {
		return nil, coreerrors.BadRequest("Priorité de l'action invalide")
	}
	if f.Status != "" && !isValidRemediationActionStatus(f.Status) {
		return nil, coreerrors.BadRequest("Statut de l'action invalide")
	}
	for _, id := range []*uint{f.ControlImplementationID, f.RiskID, f.AssigneeProfileID, f.OwningServiceID} {
		if !isPositiveOptionalID(id) {
			return nil, coreerrors.BadRequest("Identifiant de filtre invalide")
		}
	}

	// One instant for the whole operation: the Overdue filter and the
	// computed Overdue field must agree.
	now := s.currentTime()

	q := s.db.Model(&RemediationAction{})

	if search := normalizeText(f.Search); search != "" {
		like := "%" + strings.ToLower(search) + "%"
		q = q.Where("(LOWER(reference) LIKE ? OR LOWER(title) LIKE ?)", like, like)
	}
	if f.ControlImplementationID != nil {
		q = q.Where("control_implementation_id = ?", *f.ControlImplementationID)
	}
	if f.RiskID != nil {
		q = q.Where("risk_id = ?", *f.RiskID)
	}
	if f.AssigneeProfileID != nil {
		q = q.Where("assignee_profile_id = ?", *f.AssigneeProfileID)
	}
	if f.OwningServiceID != nil {
		q = q.Where("owning_service_id = ?", *f.OwningServiceID)
	}
	if f.Priority != "" {
		q = q.Where("priority = ?", f.Priority)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.DueBefore != nil {
		q = q.Where("due_at <= ?", *f.DueBefore)
	}
	if f.Overdue != nil {
		if *f.Overdue {
			q = q.Where(remediationActionOverdueSQL, now, RemediationActionStatusDone, RemediationActionStatusCancelled)
		} else {
			q = q.Where(remediationActionNotOverdueSQL, now, RemediationActionStatusDone, RemediationActionStatusCancelled)
		}
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, err
	}

	items := []RemediationAction{}
	if err := preloadRemediationActionRelations(q).
		Order(remediationActionOrderSQL).
		Offset((f.Page - 1) * f.Limit).
		Limit(f.Limit).
		Find(&items).
		Error; err != nil {
		return nil, err
	}

	for i := range items {
		items[i].Overdue = RemediationActionOverdue(items[i].Status, items[i].DueAt, now)
	}

	return &RemediationActionPage{
		Items:      items,
		Page:       f.Page,
		Limit:      f.Limit,
		Total:      total,
		TotalPages: totalPages(total, f.Limit),
	}, nil
}
