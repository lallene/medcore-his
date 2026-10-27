package grc

import (
	"errors"
	"strings"
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// assessmentInvalidEvidenceCondition matches linked evidence that is no
// longer VALID or has expired at the bound instant.
const assessmentInvalidEvidenceCondition = "(e.status <> ? OR (e.expires_at IS NOT NULL AND e.expires_at < ?))"

const (
	assessmentReassessmentDueSQL    = "(status = ? AND next_assessment_at IS NOT NULL AND next_assessment_at < ?)"
	assessmentNotReassessmentDueSQL = "(status <> ? OR next_assessment_at IS NULL OR next_assessment_at >= ?)"
	assessmentHasInvalidEvidenceSQL = "EXISTS (SELECT 1 FROM grc_assessment_evidence ae JOIN grc_evidence e ON e.id = ae.evidence_id " +
		"WHERE ae.assessment_id = grc_assessments.id AND " + assessmentInvalidEvidenceCondition + ")"
)

func (s *GRCService) SaveAssessment(id, actor uint, r AssessmentRequest) (*Assessment, error) {
	normalizeAssessmentRequest(&r)
	if err := validateAssessmentRequest(r); err != nil {
		return nil, err
	}

	now := s.currentTime()
	if r.AssessedAt != nil && r.AssessedAt.After(now) {
		return nil, coreerrors.BadRequest("La date d'évaluation ne peut pas être dans le futur")
	}

	var assessment Assessment
	var oldAuditValue string
	action := auditActionCreate

	if id > 0 {
		if err := s.db.First(&assessment, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, coreerrors.NotFound("GRC_ASSESSMENT")
			}
			return nil, err
		}
		if err := validateAssessmentTransition(assessment.Status, r.Status); err != nil {
			return nil, err
		}

		// Serialized before any mutation of assessment.
		action = auditActionUpdate
		oldAuditValue = assessmentAuditValue(assessment)
	} else {
		if err := validateAssessmentInitialStatus(r.Status); err != nil {
			return nil, err
		}
		assessment.CreatedBy = actor
	}

	if id == 0 || assessment.ControlImplementationID != r.ControlImplementationID {
		if _, err := s.loadRemediationActionImplementation(r.ControlImplementationID); err != nil {
			return nil, err
		}
		if id > 0 {
			var links int64
			if err := s.db.Model(&AssessmentEvidence{}).Where("assessment_id = ?", id).Count(&links).Error; err != nil {
				return nil, err
			}
			if links > 0 {
				return nil, coreerrors.Conflict("Impossible de changer l'implémentation d'une évaluation ayant des preuves liées")
			}
		}
	}
	if id == 0 || optionalIDChanged(assessment.AssessorProfileID, r.AssessorProfileID) {
		if err := s.validateRiskProfile(r.AssessorProfileID, "L'évaluateur est inactif"); err != nil {
			return nil, err
		}
	}

	completing := r.Status == AssessmentStatusCompleted && assessment.Status != AssessmentStatusCompleted

	assessment.Reference = r.Reference
	assessment.Title = r.Title
	assessment.ControlImplementationID = r.ControlImplementationID
	assessment.Type = r.Type
	assessment.Status = r.Status
	assessment.Result = r.Result
	assessment.AssessorProfileID = r.AssessorProfileID
	assessment.AssessedAt = r.AssessedAt
	assessment.NextAssessmentAt = r.NextAssessmentAt
	assessment.Summary = r.Summary
	assessment.UpdatedBy = actor

	var err error
	if completing {
		err = s.completeAssessment(&assessment, now, func(tx *gorm.DB) error {
			return writeGRCAudit(tx, auditModuleGRCAssessment, action, assessment.ID, actor, oldAuditValue, assessmentAuditValue(assessment))
		})
	} else {
		err = s.saveWithAudit(func(tx *gorm.DB) error {
			return tx.Select("*").Save(&assessment).Error
		}, auditModuleGRCAssessment, action, func() uint { return assessment.ID }, actor, oldAuditValue, func() string { return assessmentAuditValue(assessment) })
	}
	if err != nil {
		if isDuplicateError(err) {
			return nil, coreerrors.Conflict("Référence d'évaluation déjà utilisée")
		}
		return nil, err
	}

	return s.FindAssessment(assessment.ID)
}

// completeAssessment is the business boundary of an assessment: in one
// transaction it locks the row, checks that every linked evidence belongs to
// the same control implementation and is VALID and not expired at now, then
// records CompletedAt. Later evidence changes never rewrite the assessment.
func (s *GRCService) completeAssessment(assessment *Assessment, now time.Time, record func(tx *gorm.DB) error) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		var current Assessment
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, assessment.ID).Error; err != nil {
			return err
		}
		if err := validateAssessmentTransition(current.Status, AssessmentStatusCompleted); err != nil {
			return err
		}

		var evidence []Evidence
		if err := tx.Model(&Evidence{}).
			Joins("JOIN grc_assessment_evidence ae ON ae.evidence_id = grc_evidence.id").
			Where("ae.assessment_id = ?", assessment.ID).
			Find(&evidence).
			Error; err != nil {
			return err
		}
		for _, item := range evidence {
			if item.ControlImplementationID != assessment.ControlImplementationID {
				return coreerrors.Conflict("Une preuve liée n'appartient pas à l'implémentation évaluée")
			}
			if item.Status != EvidenceStatusValid || EvidenceExpired(item.Status, item.ExpiresAt, now) {
				return coreerrors.Conflict("Toutes les preuves liées doivent être valides et non expirées à la clôture")
			}
		}

		assessment.CompletedAt = &now
		if err := tx.Select("*").Save(assessment).Error; err != nil {
			return err
		}
		return record(tx)
	})
}

func preloadAssessmentRelations(q *gorm.DB) *gorm.DB {
	return q.
		Preload("ControlImplementation").
		Preload("ControlImplementation.Control").
		Preload("ControlImplementation.Asset").
		Preload("AssessorProfile")
}

// assessmentIDsWithInvalidEvidence returns, among ids, the assessments that
// rely on evidence no longer VALID or expired at now (one query per page).
func (s *GRCService) assessmentIDsWithInvalidEvidence(ids []uint, now time.Time) (map[uint]bool, error) {
	invalid := map[uint]bool{}
	if len(ids) == 0 {
		return invalid, nil
	}

	var rows []uint
	if err := s.db.Table("grc_assessment_evidence ae").
		Joins("JOIN grc_evidence e ON e.id = ae.evidence_id").
		Where("ae.assessment_id IN ?", ids).
		Where(assessmentInvalidEvidenceCondition, EvidenceStatusValid, now).
		Distinct().
		Pluck("ae.assessment_id", &rows).
		Error; err != nil {
		return nil, err
	}
	for _, id := range rows {
		invalid[id] = true
	}
	return invalid, nil
}

func (s *GRCService) FindAssessment(id uint) (*Assessment, error) {
	if id == 0 {
		return nil, coreerrors.BadRequest("Identifiant d'évaluation invalide")
	}

	var assessment Assessment
	err := preloadAssessmentRelations(s.db).First(&assessment, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, coreerrors.NotFound("GRC_ASSESSMENT")
	}
	if err != nil {
		return nil, err
	}

	now := s.currentTime()
	invalid, err := s.assessmentIDsWithInvalidEvidence([]uint{assessment.ID}, now)
	if err != nil {
		return nil, err
	}
	assessment.ReassessmentDue = AssessmentReassessmentDue(assessment.Status, assessment.NextAssessmentAt, now)
	assessment.HasInvalidEvidence = invalid[assessment.ID]
	return &assessment, nil
}

func (s *GRCService) ListAssessments(f AssessmentFilter) (*AssessmentPage, error) {
	f.Page, f.Limit = normalizePageFilter(f.Page, f.Limit)
	normalizeAssessmentFilter(&f)
	if err := validateAssessmentFilter(f); err != nil {
		return nil, err
	}

	// One instant for the filters and the computed fields.
	now := s.currentTime()

	q := s.db.Model(&Assessment{})

	if search := normalizeText(f.Search); search != "" {
		like := "%" + strings.ToLower(search) + "%"
		q = q.Where("(LOWER(reference) LIKE ? OR LOWER(title) LIKE ?)", like, like)
	}
	if f.ControlImplementationID != nil {
		q = q.Where("control_implementation_id = ?", *f.ControlImplementationID)
	}
	if f.EvidenceID != nil {
		q = q.Where("EXISTS (SELECT 1 FROM grc_assessment_evidence aef WHERE aef.assessment_id = grc_assessments.id AND aef.evidence_id = ?)", *f.EvidenceID)
	}
	if f.AssessorProfileID != nil {
		q = q.Where("assessor_profile_id = ?", *f.AssessorProfileID)
	}
	if f.Type != "" {
		q = q.Where("type = ?", f.Type)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.Result != "" {
		q = q.Where("result = ?", f.Result)
	}
	if f.AssessedAfter != nil {
		q = q.Where("assessed_at >= ?", *f.AssessedAfter)
	}
	if f.AssessedBefore != nil {
		q = q.Where("assessed_at <= ?", *f.AssessedBefore)
	}
	if f.ReassessmentDue != nil {
		if *f.ReassessmentDue {
			q = q.Where(assessmentReassessmentDueSQL, AssessmentStatusCompleted, now)
		} else {
			q = q.Where(assessmentNotReassessmentDueSQL, AssessmentStatusCompleted, now)
		}
	}
	if f.HasInvalidEvidence != nil {
		if *f.HasInvalidEvidence {
			q = q.Where(assessmentHasInvalidEvidenceSQL, EvidenceStatusValid, now)
		} else {
			q = q.Where("NOT "+assessmentHasInvalidEvidenceSQL, EvidenceStatusValid, now)
		}
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, err
	}

	items := []Assessment{}
	if err := preloadAssessmentRelations(q).
		Order("reference, id").
		Offset((f.Page - 1) * f.Limit).
		Limit(f.Limit).
		Find(&items).
		Error; err != nil {
		return nil, err
	}

	ids := make([]uint, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	invalid, err := s.assessmentIDsWithInvalidEvidence(ids, now)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].ReassessmentDue = AssessmentReassessmentDue(items[i].Status, items[i].NextAssessmentAt, now)
		items[i].HasInvalidEvidence = invalid[items[i].ID]
	}

	return &AssessmentPage{
		Items:      items,
		Page:       f.Page,
		Limit:      f.Limit,
		Total:      total,
		TotalPages: totalPages(total, f.Limit),
	}, nil
}

// lockDraftAssessment locks the assessment row: evidence links can only
// change while it is DRAFT.
func lockDraftAssessment(tx *gorm.DB, id uint) (*Assessment, error) {
	var assessment Assessment
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&assessment, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, coreerrors.NotFound("GRC_ASSESSMENT")
		}
		return nil, err
	}
	if assessment.Status != AssessmentStatusDraft {
		return nil, coreerrors.Conflict("Les preuves d'une évaluation terminée ou annulée ne peuvent plus être modifiées")
	}
	return &assessment, nil
}

func (s *GRCService) LinkAssessmentEvidence(actor, assessmentID uint, r AssessmentEvidenceRequest) (*AssessmentEvidence, error) {
	if assessmentID == 0 {
		return nil, coreerrors.BadRequest("Identifiant d'évaluation invalide")
	}
	if err := validateAssessmentEvidenceRequest(r); err != nil {
		return nil, err
	}

	now := s.currentTime()
	link := AssessmentEvidence{AssessmentID: assessmentID, EvidenceID: r.EvidenceID, CreatedBy: actor}

	err := s.db.Transaction(func(tx *gorm.DB) error {
		assessment, err := lockDraftAssessment(tx, assessmentID)
		if err != nil {
			return err
		}

		var evidence Evidence
		if err := tx.First(&evidence, r.EvidenceID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return coreerrors.NotFound("GRC_EVIDENCE")
			}
			return err
		}
		if evidence.ControlImplementationID != assessment.ControlImplementationID {
			return coreerrors.Conflict("La preuve n'appartient pas à l'implémentation évaluée")
		}
		if evidence.Status != EvidenceStatusValid || EvidenceExpired(evidence.Status, evidence.ExpiresAt, now) {
			return coreerrors.Conflict("Seule une preuve valide et non expirée peut être liée")
		}

		if err := tx.Create(&link).Error; err != nil {
			return err
		}
		return writeGRCAudit(tx, auditModuleGRCAssessmentEvidence, auditActionLink, link.ID, actor, "", assessmentEvidenceAuditValue(link))
	})
	if err != nil {
		if isDuplicateError(err) {
			return nil, coreerrors.Conflict("Cette preuve est déjà liée à l'évaluation")
		}
		return nil, err
	}

	return &link, nil
}

func (s *GRCService) UnlinkAssessmentEvidence(actor, assessmentID, evidenceID uint) error {
	if assessmentID == 0 || evidenceID == 0 {
		return coreerrors.BadRequest("Évaluation et preuve obligatoires")
	}

	// The link is read before deletion so the audit keeps its real ID.
	var link AssessmentEvidence
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if _, err := lockDraftAssessment(tx, assessmentID); err != nil {
			return err
		}

		if err := tx.Where("assessment_id = ? AND evidence_id = ?", assessmentID, evidenceID).First(&link).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return coreerrors.NotFound("GRC_ASSESSMENT_EVIDENCE")
			}
			return err
		}
		result := tx.Delete(&AssessmentEvidence{}, link.ID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return coreerrors.NotFound("GRC_ASSESSMENT_EVIDENCE")
		}
		return writeGRCAudit(tx, auditModuleGRCAssessmentEvidence, auditActionUnlink, link.ID, actor, assessmentEvidenceAuditValue(link), "")
	})
	return err
}

func (s *GRCService) ListAssessmentEvidence(assessmentID uint) ([]AssessmentEvidence, error) {
	if _, err := s.FindAssessment(assessmentID); err != nil {
		return nil, err
	}

	links := []AssessmentEvidence{}
	if err := s.db.Preload("Evidence").
		Where("assessment_id = ?", assessmentID).
		Order("id").
		Find(&links).
		Error; err != nil {
		return nil, err
	}
	return links, nil
}
