package grc

import (
	"errors"
	"strings"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"gorm.io/gorm"
)

// evidenceExpiredSQL mirrors EvidenceExpired; now is bound as a parameter so
// the filter and the computed field share one instant.
const evidenceExpiredSQL = "(status = ? AND expires_at IS NOT NULL AND expires_at < ?)"

func optionalIDChanged(previous, next *uint) bool {
	if previous == nil || next == nil {
		return previous != next
	}
	return *previous != *next
}

func (s *GRCService) SaveEvidence(id, actor uint, r EvidenceRequest) (*Evidence, error) {
	normalizeEvidenceRequest(&r)
	if err := validateEvidenceRequest(r); err != nil {
		return nil, err
	}

	var evidence Evidence
	var oldAuditValue string
	action := auditActionCreate

	if id > 0 {
		if err := s.db.First(&evidence, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, coreerrors.NotFound("GRC_EVIDENCE")
			}
			return nil, err
		}
		if evidence.Status == EvidenceStatusRevoked {
			return nil, coreerrors.Conflict("Une preuve révoquée ne peut plus être modifiée")
		}

		// Serialized before any mutation of evidence.
		action = auditActionUpdate
		oldAuditValue = evidenceAuditValue(evidence)
	} else {
		evidence.CreatedBy = actor
	}

	// Relations are validated when assigned only, so a historical evidence
	// stays editable after its owner or service has been deactivated.
	if id == 0 || evidence.ControlImplementationID != r.ControlImplementationID {
		if err := s.validateEvidenceControlImplementation(r.ControlImplementationID); err != nil {
			return nil, err
		}
	}
	if id == 0 || optionalIDChanged(evidence.OwnerProfileID, r.OwnerProfileID) {
		if err := s.validateRiskProfile(r.OwnerProfileID, "Le propriétaire de la preuve est inactif"); err != nil {
			return nil, err
		}
	}
	if id == 0 || optionalIDChanged(evidence.OwningServiceID, r.OwningServiceID) {
		if err := s.validateOwningService(r.OwningServiceID); err != nil {
			return nil, err
		}
	}

	evidence.Reference = r.Reference
	evidence.Title = r.Title
	evidence.Description = r.Description
	evidence.ControlImplementationID = r.ControlImplementationID
	evidence.Type = r.Type
	evidence.Status = r.Status
	evidence.ExternalReference = r.ExternalReference
	evidence.CollectedAt = r.CollectedAt
	evidence.ValidFrom = r.ValidFrom
	evidence.ExpiresAt = r.ExpiresAt
	evidence.OwnerProfileID = r.OwnerProfileID
	evidence.OwningServiceID = r.OwningServiceID
	evidence.UpdatedBy = actor

	if err := s.saveWithAudit(func(tx *gorm.DB) error {
		return tx.Select("*").Save(&evidence).Error
	}, auditModuleGRCEvidence, action, func() uint { return evidence.ID }, actor, oldAuditValue, func() string { return evidenceAuditValue(evidence) }); err != nil {
		if isDuplicateError(err) {
			return nil, coreerrors.Conflict("Référence de preuve déjà utilisée")
		}
		return nil, err
	}

	return s.FindEvidence(evidence.ID)
}

func (s *GRCService) validateEvidenceControlImplementation(id uint) error {
	if err := s.db.First(&ControlImplementation{}, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return coreerrors.NotFound("GRC_CONTROL_IMPLEMENTATION")
		}
		return err
	}
	return nil
}

func preloadEvidenceRelations(q *gorm.DB) *gorm.DB {
	return q.
		Preload("ControlImplementation").
		Preload("ControlImplementation.Control").
		Preload("ControlImplementation.Asset").
		Preload("OwnerProfile").
		Preload("OwningService")
}

func (s *GRCService) FindEvidence(id uint) (*Evidence, error) {
	return s.FindEvidenceWithWindow(id, DashboardDefaultWindowDays)
}

// FindEvidenceWithWindow derives the validity with the given EXPIRING_SOON
// window, in days.
func (s *GRCService) FindEvidenceWithWindow(id uint, soonDays int) (*Evidence, error) {
	now := s.currentTime()
	soonLimit, err := evidenceSoonLimit(now, soonDays)
	if err != nil {
		return nil, err
	}
	if id == 0 {
		return nil, coreerrors.BadRequest("Identifiant de preuve invalide")
	}

	var evidence Evidence
	err = preloadEvidenceRelations(s.db).First(&evidence, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, coreerrors.NotFound("GRC_EVIDENCE")
	}
	if err != nil {
		return nil, err
	}

	fillEvidenceComputed(&evidence, now, soonLimit)
	return &evidence, nil
}

func (s *GRCService) ListEvidence(f EvidenceFilter) (*EvidencePage, error) {
	f.Page, f.Limit = normalizePageFilter(f.Page, f.Limit)
	f.Type = normalizeEnum(f.Type)
	f.Status = normalizeEnum(f.Status)

	if f.Type != "" && !isValidEvidenceType(f.Type) {
		return nil, coreerrors.BadRequest("Type de preuve invalide")
	}
	if f.Status != "" && !isValidEvidenceStatus(f.Status) {
		return nil, coreerrors.BadRequest("Statut de la preuve invalide")
	}
	for _, id := range []*uint{f.ControlImplementationID, f.OwnerProfileID, f.OwningServiceID} {
		if !isPositiveOptionalID(id) {
			return nil, coreerrors.BadRequest("Identifiant de filtre invalide")
		}
	}

	// One instant for the whole operation: the Expired/Validity filters and
	// the computed fields must agree.
	now := s.currentTime()
	if f.ExpiringSoonDays == 0 {
		f.ExpiringSoonDays = DashboardDefaultWindowDays
	}
	soonLimit, err := evidenceSoonLimit(now, f.ExpiringSoonDays)
	if err != nil {
		return nil, err
	}
	f.Validity = normalizeEnum(f.Validity)

	q := s.db.Model(&Evidence{})

	if search := normalizeText(f.Search); search != "" {
		like := "%" + strings.ToLower(search) + "%"
		q = q.Where("(LOWER(reference) LIKE ? OR LOWER(title) LIKE ?)", like, like)
	}
	if f.ControlImplementationID != nil {
		q = q.Where("control_implementation_id = ?", *f.ControlImplementationID)
	}
	if f.OwnerProfileID != nil {
		q = q.Where("owner_profile_id = ?", *f.OwnerProfileID)
	}
	if f.OwningServiceID != nil {
		q = q.Where("owning_service_id = ?", *f.OwningServiceID)
	}
	if f.Type != "" {
		q = q.Where("type = ?", f.Type)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.ExpiresBefore != nil {
		q = q.Where("expires_at <= ?", *f.ExpiresBefore)
	}
	if f.Expired != nil {
		if *f.Expired {
			q = q.Where(evidenceExpiredSQL, EvidenceStatusValid, now)
		} else {
			q = q.Where("NOT "+evidenceExpiredSQL, EvidenceStatusValid, now)
		}
	}
	if f.Validity != "" {
		condition, args, err := evidenceValidityCondition(f.Validity, now, soonLimit)
		if err != nil {
			return nil, err
		}
		q = q.Where(condition, args...)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, err
	}

	items := []Evidence{}
	if err := preloadEvidenceRelations(q).
		Order("reference, id").
		Offset((f.Page - 1) * f.Limit).
		Limit(f.Limit).
		Find(&items).
		Error; err != nil {
		return nil, err
	}

	for i := range items {
		fillEvidenceComputed(&items[i], now, soonLimit)
	}

	return &EvidencePage{
		Items:      items,
		Page:       f.Page,
		Limit:      f.Limit,
		Total:      total,
		TotalPages: totalPages(total, f.Limit),
	}, nil
}
