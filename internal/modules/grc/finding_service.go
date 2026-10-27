package grc

import (
	"errors"
	"strings"
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// findingOverdueSQL mirrors FindingOverdue; findingNotOverdueSQL is its exact
// NULL-safe complement. now is bound so filter and field share one instant.
const (
	findingOverdueSQL    = "(due_at IS NOT NULL AND due_at < ? AND status IN (?, ?))"
	findingNotOverdueSQL = "(due_at IS NULL OR due_at >= ? OR status NOT IN (?, ?))"
	findingOrderSQL      = "CASE WHEN due_at IS NULL THEN 1 ELSE 0 END, due_at, id"
)

func (s *GRCService) SaveFinding(id, actor uint, r FindingRequest) (*Finding, error) {
	normalizeFindingRequest(&r)
	if err := validateFindingRequest(r); err != nil {
		return nil, err
	}

	now := s.currentTime()

	var finding Finding
	var oldAuditValue string
	action := auditActionCreate

	if id > 0 {
		if err := s.db.First(&finding, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, coreerrors.NotFound("GRC_FINDING")
			}
			return nil, err
		}
		if err := validateFindingTransition(finding.Status, r.Status); err != nil {
			return nil, err
		}

		// Serialized before any mutation of finding.
		action = auditActionUpdate
		oldAuditValue = findingAuditValue(finding)
		if optionalIDChanged(finding.AuditEngagementID, r.AuditEngagementID) ||
			optionalIDChanged(finding.AssessmentID, r.AssessmentID) {
			return nil, coreerrors.Conflict("La source d'un constat ne peut pas être modifiée")
		}
	} else {
		if err := validateFindingInitialStatus(r.Status); err != nil {
			return nil, err
		}
		finding.CreatedBy = actor
	}

	implementationChanged := id == 0 || optionalIDChanged(finding.ControlImplementationID, r.ControlImplementationID)
	riskChanged := id == 0 || optionalIDChanged(finding.RiskID, r.RiskID)

	if err := s.validateFindingSource(id == 0, implementationChanged, &r); err != nil {
		return nil, err
	}

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

	if id == 0 || optionalIDChanged(finding.OwnerProfileID, r.OwnerProfileID) {
		if err := s.validateRiskProfile(r.OwnerProfileID, "Le propriétaire du constat est inactif"); err != nil {
			return nil, err
		}
	}
	if id == 0 || optionalIDChanged(finding.OwningServiceID, r.OwningServiceID) {
		if err := s.validateOwningService(r.OwningServiceID); err != nil {
			return nil, err
		}
	}

	resolvedAt, closedAt, err := findingTimestamps(finding.Status, r.Status, finding.ResolvedAt, finding.ClosedAt, now)
	if err != nil {
		return nil, err
	}

	finding.Reference = r.Reference
	finding.Title = r.Title
	finding.Description = r.Description
	finding.AuditEngagementID = r.AuditEngagementID
	finding.AssessmentID = r.AssessmentID
	finding.Category = r.Category
	finding.Severity = r.Severity
	finding.ControlImplementationID = r.ControlImplementationID
	finding.RiskID = r.RiskID
	finding.OwnerProfileID = r.OwnerProfileID
	finding.OwningServiceID = r.OwningServiceID
	finding.DueAt = r.DueAt
	finding.ClosureNote = r.ClosureNote
	finding.ResolvedAt = resolvedAt
	finding.ClosedAt = closedAt
	previousStatus := finding.Status
	finding.Status = r.Status
	finding.UpdatedBy = actor

	if r.Status == FindingStatusClosed && previousStatus != FindingStatusClosed {
		err = s.closeFinding(&finding, func(tx *gorm.DB) error {
			return writeGRCAudit(tx, auditModuleGRCFinding, action, finding.ID, actor, oldAuditValue, findingAuditValue(finding))
		})
	} else {
		err = s.saveWithAudit(func(tx *gorm.DB) error {
			return tx.Select("*").Save(&finding).Error
		}, auditModuleGRCFinding, action, func() uint { return finding.ID }, actor, oldAuditValue, func() string { return findingAuditValue(finding) })
	}
	if err != nil {
		if isDuplicateError(err) {
			return nil, coreerrors.Conflict("Référence de constat déjà utilisée")
		}
		return nil, err
	}

	return s.FindFinding(finding.ID)
}

// validateFindingSource checks the source on creation (an audit in progress,
// or a completed assessment) and keeps the finding on the assessed control
// implementation. The source itself is immutable after creation.
func (s *GRCService) validateFindingSource(creating, implementationChanged bool, r *FindingRequest) error {
	if r.AuditEngagementID != nil && creating {
		var audit AuditEngagement
		if err := s.db.First(&audit, *r.AuditEngagementID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return coreerrors.NotFound("GRC_AUDIT_ENGAGEMENT")
			}
			return err
		}
		if audit.Status != AuditEngagementStatusInProgress {
			return coreerrors.Conflict("Un constat d'audit ne peut être créé que pendant un audit en cours")
		}
	}

	if r.AssessmentID == nil || (!creating && !implementationChanged) {
		return nil
	}

	var assessment Assessment
	if err := s.db.First(&assessment, *r.AssessmentID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return coreerrors.NotFound("GRC_ASSESSMENT")
		}
		return err
	}
	if creating && assessment.Status != AssessmentStatusCompleted {
		return coreerrors.Conflict("Un constat d'évaluation exige une évaluation terminée")
	}
	if creating && r.ControlImplementationID == nil {
		r.ControlImplementationID = &assessment.ControlImplementationID
	}
	if r.ControlImplementationID == nil || *r.ControlImplementationID != assessment.ControlImplementationID {
		return coreerrors.Conflict("Le constat doit porter sur l'implémentation de son évaluation")
	}
	return nil
}

// findingTimestamps derives the server-owned ResolvedAt and ClosedAt of a
// transition. Reopening (RESOLVED -> OPEN) clears ResolvedAt so that a later
// resolution records a fresh instant.
func findingTimestamps(from, to string, resolvedAt, closedAt *time.Time, now time.Time) (*time.Time, *time.Time, error) {
	switch to {
	case FindingStatusOpen, FindingStatusInRemediation:
		return nil, nil, nil
	case FindingStatusResolved:
		if from != FindingStatusResolved || resolvedAt == nil {
			resolvedAt = &now
		}
		return resolvedAt, nil, nil
	case FindingStatusClosed, FindingStatusAccepted:
		if from == to {
			return resolvedAt, closedAt, nil
		}
		if from != FindingStatusResolved {
			resolvedAt = nil
		}
		if resolvedAt != nil && now.Before(*resolvedAt) {
			return nil, nil, coreerrors.Conflict("La clôture du constat ne peut pas précéder sa résolution")
		}
		return resolvedAt, &now, nil
	}
	return resolvedAt, closedAt, nil
}

// closeFinding verifies, under a row lock, that the finding is still RESOLVED
// and that none of its remediation actions is still open. Action writes that
// target a finding take the same finding lock before they commit, so a new or
// reopened action cannot land between this check and the close.
func (s *GRCService) closeFinding(finding *Finding, record func(tx *gorm.DB) error) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		var current Finding
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, finding.ID).Error; err != nil {
			return err
		}
		if err := validateFindingTransition(current.Status, FindingStatusClosed); err != nil {
			return err
		}

		var open int64
		if err := tx.Model(&RemediationAction{}).
			Where("finding_id = ? AND status NOT IN (?, ?)", finding.ID,
				RemediationActionStatusDone, RemediationActionStatusCancelled).
			Count(&open).
			Error; err != nil {
			return err
		}
		if open > 0 {
			return coreerrors.Conflict("Un constat ne peut être clôturé tant que des actions restent ouvertes")
		}

		if err := tx.Select("*").Save(finding).Error; err != nil {
			return err
		}
		return record(tx)
	})
}

func preloadFindingRelations(q *gorm.DB) *gorm.DB {
	return q.
		Preload("AuditEngagement").
		Preload("Assessment").
		Preload("ControlImplementation").
		Preload("ControlImplementation.Control").
		Preload("ControlImplementation.Asset").
		Preload("Risk").
		Preload("OwnerProfile").
		Preload("OwningService")
}

func (s *GRCService) FindFinding(id uint) (*Finding, error) {
	if id == 0 {
		return nil, coreerrors.BadRequest("Identifiant de constat invalide")
	}

	var finding Finding
	err := preloadFindingRelations(s.db).First(&finding, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, coreerrors.NotFound("GRC_FINDING")
	}
	if err != nil {
		return nil, err
	}

	finding.Overdue = FindingOverdue(finding.Status, finding.DueAt, s.currentTime())
	return &finding, nil
}

func (s *GRCService) ListFindings(f FindingFilter) (*FindingPage, error) {
	f.Page, f.Limit = normalizePageFilter(f.Page, f.Limit)
	normalizeFindingFilter(&f)
	if err := validateFindingFilter(f); err != nil {
		return nil, err
	}

	now := s.currentTime()
	q := s.db.Model(&Finding{})

	if search := normalizeText(f.Search); search != "" {
		like := "%" + strings.ToLower(search) + "%"
		q = q.Where("(LOWER(reference) LIKE ? OR LOWER(title) LIKE ?)", like, like)
	}
	for column, value := range map[string]*uint{
		"audit_engagement_id":       f.AuditEngagementID,
		"assessment_id":             f.AssessmentID,
		"control_implementation_id": f.ControlImplementationID,
		"risk_id":                   f.RiskID,
		"owner_profile_id":          f.OwnerProfileID,
		"owning_service_id":         f.OwningServiceID,
	} {
		if value != nil {
			q = q.Where(column+" = ?", *value)
		}
	}
	if f.Category != "" {
		q = q.Where("category = ?", f.Category)
	}
	if f.Severity != "" {
		q = q.Where("severity = ?", f.Severity)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.DueBefore != nil {
		q = q.Where("due_at <= ?", *f.DueBefore)
	}
	if f.Overdue != nil {
		if *f.Overdue {
			q = q.Where(findingOverdueSQL, now, FindingStatusOpen, FindingStatusInRemediation)
		} else {
			q = q.Where(findingNotOverdueSQL, now, FindingStatusOpen, FindingStatusInRemediation)
		}
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, err
	}

	items := []Finding{}
	if err := preloadFindingRelations(q).
		Order(findingOrderSQL).
		Offset((f.Page - 1) * f.Limit).
		Limit(f.Limit).
		Find(&items).
		Error; err != nil {
		return nil, err
	}
	for i := range items {
		items[i].Overdue = FindingOverdue(items[i].Status, items[i].DueAt, now)
	}

	return &FindingPage{
		Items:      items,
		Page:       f.Page,
		Limit:      f.Limit,
		Total:      total,
		TotalPages: totalPages(total, f.Limit),
	}, nil
}
