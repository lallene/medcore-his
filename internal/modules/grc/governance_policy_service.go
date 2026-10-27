package grc

import (
	"errors"
	"strings"
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *GRCService) validateGovernancePolicyProfiles(current *GovernancePolicy, r GovernancePolicyRequest, creating bool) error {
	var serviceID, ownerID, approverID *uint
	if !creating && current != nil {
		serviceID, ownerID, approverID = current.OwningServiceID, current.OwnerProfileID, current.ApproverProfileID
	}
	if creating || optionalIDChanged(serviceID, r.OwningServiceID) {
		if err := s.validateOwningService(r.OwningServiceID); err != nil {
			return err
		}
	}
	if creating || optionalIDChanged(ownerID, r.OwnerProfileID) {
		if err := s.validateRiskProfile(r.OwnerProfileID, "Le propriétaire de la politique est inactif"); err != nil {
			return err
		}
	}
	if creating || optionalIDChanged(approverID, r.ApproverProfileID) {
		if err := s.validateRiskProfile(r.ApproverProfileID, "L'approbateur de la politique est inactif"); err != nil {
			return err
		}
	}
	return nil
}

func (s *GRCService) CreateGovernancePolicy(actor uint, r GovernancePolicyRequest) (*GovernancePolicy, error) {
	r.Status = GovernancePolicyStatusDraft
	normalizeGovernancePolicyRequest(&r)
	if err := validateGovernancePolicyRequest(r, true); err != nil {
		return nil, err
	}
	if err := s.validateGovernancePolicyProfiles(nil, r, true); err != nil {
		return nil, err
	}
	frameworkIDs, _, err := normalizePolicyIDList(r.FrameworkIDs)
	if err != nil {
		return nil, err
	}
	controlIDs, _, err := normalizePolicyIDList(r.ControlIDs)
	if err != nil {
		return nil, err
	}
	if frameworkIDs == nil {
		frameworkIDs = []uint{}
	}
	if controlIDs == nil {
		controlIDs = []uint{}
	}

	var saved GovernancePolicy
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := requirePolicyFrameworks(tx, frameworkIDs); err != nil {
			return err
		}
		if err := requirePolicyControls(tx, controlIDs); err != nil {
			return err
		}
		item := GovernancePolicy{
			Reference:         r.Reference,
			Title:             r.Title,
			Description:       r.Description,
			Version:           r.Version,
			Status:            GovernancePolicyStatusDraft,
			EffectiveFrom:     r.EffectiveFrom,
			EffectiveTo:       r.EffectiveTo,
			OwningServiceID:   r.OwningServiceID,
			OwnerProfileID:    r.OwnerProfileID,
			ApproverProfileID: r.ApproverProfileID,
			CreatedBy:         actor,
			UpdatedBy:         actor,
		}
		if err := tx.Create(&item).Error; err != nil {
			if isDuplicateError(err) {
				return coreerrors.Conflict("Référence de politique déjà utilisée")
			}
			return err
		}
		if err := insertPolicyFrameworks(tx, item.ID, actor, frameworkIDs); err != nil {
			return err
		}
		if err := insertPolicyControls(tx, item.ID, actor, controlIDs); err != nil {
			return err
		}
		saved = item
		return writeGRCAudit(tx, auditModuleGRCGovernancePolicy, auditActionCreate, saved.ID, actor, "", governancePolicyAuditValue(saved, frameworkIDs, controlIDs))
	})
	if err != nil {
		return nil, err
	}
	return s.FindGovernancePolicy(saved.ID)
}

func (s *GRCService) UpdateGovernancePolicy(id, actor uint, r GovernancePolicyRequest) (*GovernancePolicy, error) {
	normalizeGovernancePolicyRequest(&r)
	// Reserved targets are refused before any other check, including for a
	// wildcard caller. Staying APPROVED or PUBLISHED is a metadata update and
	// sends no status.
	if err := governancePolicyReservedStatus(r.Status); err != nil {
		return nil, err
	}
	if err := validateGovernancePolicyRequest(r, false); err != nil {
		return nil, err
	}
	frameworkIDs, frameworksSet, err := normalizePolicyIDList(r.FrameworkIDs)
	if err != nil {
		return nil, err
	}
	controlIDs, controlsSet, err := normalizePolicyIDList(r.ControlIDs)
	if err != nil {
		return nil, err
	}

	var saved GovernancePolicy
	var oldValue, newValue string
	err = s.db.Transaction(func(tx *gorm.DB) error {
		current, err := lockGovernancePolicy(tx, id)
		if err != nil {
			return err
		}
		if current.Status == GovernancePolicyStatusRetired {
			return coreerrors.Conflict("Une politique retirée est immuable")
		}
		if err := s.validateGovernancePolicyProfiles(&current, r, false); err != nil {
			return err
		}
		status := r.Status
		if status == "" {
			status = current.Status
		}
		if err := validateGovernancePolicyTransition(current.Status, status); err != nil {
			return err
		}

		oldFrameworks, err := policyRelationIDs(tx, "grc_governance_policy_frameworks", "framework_id", id)
		if err != nil {
			return err
		}
		oldControls, err := policyRelationIDs(tx, "grc_governance_policy_controls", "control_id", id)
		if err != nil {
			return err
		}
		nextFrameworks := oldFrameworks
		if frameworksSet {
			nextFrameworks, err = replacePolicyFrameworks(tx, id, actor, oldFrameworks, frameworkIDs)
			if err != nil {
				return err
			}
		}
		nextControls := oldControls
		if controlsSet {
			nextControls, err = replacePolicyControls(tx, id, actor, oldControls, controlIDs)
			if err != nil {
				return err
			}
		}

		oldValue = governancePolicyAuditValue(current, oldFrameworks, oldControls)
		current.Reference = r.Reference
		current.Title = r.Title
		current.Description = r.Description
		current.Version = r.Version
		current.Status = status
		current.EffectiveFrom = r.EffectiveFrom
		current.EffectiveTo = r.EffectiveTo
		current.OwningServiceID = r.OwningServiceID
		current.OwnerProfileID = r.OwnerProfileID
		current.ApproverProfileID = r.ApproverProfileID
		applyGovernancePolicyTimestamps(&current, status)
		current.UpdatedBy = actor
		if err := tx.Select("*").Save(&current).Error; err != nil {
			if isDuplicateError(err) {
				return coreerrors.Conflict("Référence de politique déjà utilisée")
			}
			return err
		}
		saved = current
		newValue = governancePolicyAuditValue(saved, nextFrameworks, nextControls)
		return writeGRCAudit(tx, auditModuleGRCGovernancePolicy, auditActionUpdate, saved.ID, actor, oldValue, newValue)
	})
	if err != nil {
		return nil, err
	}
	return s.FindGovernancePolicy(saved.ID)
}

// applyGovernancePolicyTimestamps keeps server-owned instants coherent with
// the target status. Generic update never writes APPROVED, PUBLISHED or
// RETIRED, so those branches only preserve an instant the dedicated route
// already set. Returning from APPROVED to IN_REVIEW clears the approval.
func applyGovernancePolicyTimestamps(item *GovernancePolicy, status string) {
	switch status {
	case GovernancePolicyStatusApproved, GovernancePolicyStatusPublished:
		item.RetiredAt = nil
	default:
		item.ApprovedAt = nil
		item.PublishedAt = nil
		item.RetiredAt = nil
	}
}

func (s *GRCService) ApproveGovernancePolicy(id, actor uint) (*GovernancePolicy, error) {
	return s.mutateGovernancePolicy(id, actor, func(current *GovernancePolicy, now time.Time) error {
		if current.Status != GovernancePolicyStatusInReview {
			return coreerrors.Conflict("Seule une politique en revue peut être approuvée")
		}
		if current.ApproverProfileID == nil {
			return coreerrors.Conflict("Un approbateur est obligatoire pour approuver la politique")
		}
		current.Status = GovernancePolicyStatusApproved
		current.ApprovedAt = &now
		current.PublishedAt = nil
		current.RetiredAt = nil
		return nil
	})
}

func (s *GRCService) PublishGovernancePolicy(id, actor uint) (*GovernancePolicy, error) {
	return s.mutateGovernancePolicy(id, actor, func(current *GovernancePolicy, now time.Time) error {
		if current.Status != GovernancePolicyStatusApproved || current.ApprovedAt == nil {
			return coreerrors.Conflict("Seule une politique approuvée peut être publiée")
		}
		if current.EffectiveFrom == nil {
			return coreerrors.Conflict("Un début d'effet est obligatoire pour publier la politique")
		}
		current.Status = GovernancePolicyStatusPublished
		current.PublishedAt = &now
		current.RetiredAt = nil
		return nil
	})
}

func (s *GRCService) RetireGovernancePolicy(id, actor uint) (*GovernancePolicy, error) {
	return s.mutateGovernancePolicy(id, actor, func(current *GovernancePolicy, now time.Time) error {
		if current.Status != GovernancePolicyStatusPublished {
			return coreerrors.Conflict("Seule une politique publiée peut être retirée")
		}
		current.Status = GovernancePolicyStatusRetired
		current.RetiredAt = &now
		return nil
	})
}

func (s *GRCService) mutateGovernancePolicy(id, actor uint, apply func(*GovernancePolicy, time.Time) error) (*GovernancePolicy, error) {
	now := s.currentTime().UTC()
	var saved GovernancePolicy
	var oldValue, newValue string
	err := s.db.Transaction(func(tx *gorm.DB) error {
		current, err := lockGovernancePolicy(tx, id)
		if err != nil {
			return err
		}
		frameworks, err := policyRelationIDs(tx, "grc_governance_policy_frameworks", "framework_id", id)
		if err != nil {
			return err
		}
		controls, err := policyRelationIDs(tx, "grc_governance_policy_controls", "control_id", id)
		if err != nil {
			return err
		}
		oldValue = governancePolicyAuditValue(current, frameworks, controls)
		if err := apply(&current, now); err != nil {
			return err
		}
		current.UpdatedBy = actor
		if err := tx.Select("*").Save(&current).Error; err != nil {
			return err
		}
		saved = current
		newValue = governancePolicyAuditValue(saved, frameworks, controls)
		return writeGRCAudit(tx, auditModuleGRCGovernancePolicy, auditActionUpdate, saved.ID, actor, oldValue, newValue)
	})
	if err != nil {
		return nil, err
	}
	return s.FindGovernancePolicy(saved.ID)
}

func (s *GRCService) FindGovernancePolicy(id uint) (*GovernancePolicy, error) {
	if id == 0 {
		return nil, coreerrors.BadRequest("Identifiant de politique invalide")
	}
	var item GovernancePolicy
	err := s.db.Preload("OwningService").Preload("OwnerProfile").Preload("ApproverProfile").First(&item, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, coreerrors.NotFound("GRC_GOVERNANCE_POLICY")
	}
	if err != nil {
		return nil, err
	}
	if err := s.fillGovernancePolicyLinks(&item); err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *GRCService) ListGovernancePolicies(f GovernancePolicyFilter) (*GovernancePolicyPage, error) {
	f.Page, f.Limit = normalizePageFilter(f.Page, f.Limit)
	normalizeGovernancePolicyFilter(&f)
	if err := validateGovernancePolicyFilter(f); err != nil {
		return nil, err
	}

	query := s.db.Model(&GovernancePolicy{})
	if f.Search != "" {
		like := "%" + strings.ToLower(f.Search) + "%"
		query = query.Where(
			"(LOWER(grc_governance_policies.reference) LIKE ? OR LOWER(grc_governance_policies.title) LIKE ? OR LOWER(grc_governance_policies.version) LIKE ?)",
			like, like, like,
		)
	}
	if f.Status != "" {
		query = query.Where("grc_governance_policies.status = ?", f.Status)
	}
	if f.OwningServiceID != nil {
		query = query.Where("grc_governance_policies.owning_service_id = ?", *f.OwningServiceID)
	}
	if f.OwnerProfileID != nil {
		query = query.Where("grc_governance_policies.owner_profile_id = ?", *f.OwnerProfileID)
	}
	if f.ApproverProfileID != nil {
		query = query.Where("grc_governance_policies.approver_profile_id = ?", *f.ApproverProfileID)
	}
	if f.FrameworkID != nil {
		query = query.Where(
			"EXISTS (SELECT 1 FROM grc_governance_policy_frameworks pf WHERE pf.policy_id = grc_governance_policies.id AND pf.framework_id = ?)",
			*f.FrameworkID,
		)
	}
	if f.ControlID != nil {
		query = query.Where(
			"EXISTS (SELECT 1 FROM grc_governance_policy_controls pc WHERE pc.policy_id = grc_governance_policies.id AND pc.control_id = ?)",
			*f.ControlID,
		)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}
	items := []GovernancePolicy{}
	if err := query.Preload("OwningService").Preload("OwnerProfile").Preload("ApproverProfile").
		Order("grc_governance_policies.reference, grc_governance_policies.id").
		Offset((f.Page - 1) * f.Limit).
		Limit(f.Limit).
		Find(&items).Error; err != nil {
		return nil, err
	}
	if err := s.fillGovernancePolicyCounts(items); err != nil {
		return nil, err
	}
	return &GovernancePolicyPage{
		Items:      items,
		Page:       f.Page,
		Limit:      f.Limit,
		Total:      total,
		TotalPages: totalPages(total, f.Limit),
	}, nil
}

func lockGovernancePolicy(tx *gorm.DB, id uint) (GovernancePolicy, error) {
	var current GovernancePolicy
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return current, coreerrors.NotFound("GRC_GOVERNANCE_POLICY")
	}
	return current, err
}

func policyRelationIDs(tx *gorm.DB, table, column string, policyID uint) ([]uint, error) {
	ids := []uint{}
	err := tx.Table(table).Where("policy_id = ?", policyID).Order(column).Pluck(column, &ids).Error
	return ids, err
}

func requirePolicyFrameworks(tx *gorm.DB, ids []uint) error {
	return requireExistingIDs(tx, &Framework{}, ids, "GRC_FRAMEWORK")
}

func requirePolicyControls(tx *gorm.DB, ids []uint) error {
	return requireExistingIDs(tx, &Control{}, ids, "GRC_CONTROL")
}

func requireExistingIDs(tx *gorm.DB, model any, ids []uint, code string) error {
	if len(ids) == 0 {
		return nil
	}
	var found int64
	if err := tx.Model(model).Where("id IN ?", ids).Count(&found).Error; err != nil {
		return err
	}
	if found != int64(len(ids)) {
		return coreerrors.NotFound(code)
	}
	return nil
}

func replacePolicyFrameworks(tx *gorm.DB, policyID, actor uint, current, next []uint) ([]uint, error) {
	if samePolicyIDs(current, next) {
		return current, nil
	}
	if err := requirePolicyFrameworks(tx, newPolicyIDs(current, next)); err != nil {
		return nil, err
	}
	if err := tx.Where("policy_id = ?", policyID).Delete(&GovernancePolicyFramework{}).Error; err != nil {
		return nil, err
	}
	if err := insertPolicyFrameworks(tx, policyID, actor, next); err != nil {
		return nil, err
	}
	return next, nil
}

func replacePolicyControls(tx *gorm.DB, policyID, actor uint, current, next []uint) ([]uint, error) {
	if samePolicyIDs(current, next) {
		return current, nil
	}
	if err := requirePolicyControls(tx, newPolicyIDs(current, next)); err != nil {
		return nil, err
	}
	if err := tx.Where("policy_id = ?", policyID).Delete(&GovernancePolicyControl{}).Error; err != nil {
		return nil, err
	}
	if err := insertPolicyControls(tx, policyID, actor, next); err != nil {
		return nil, err
	}
	return next, nil
}

func newPolicyIDs(current, next []uint) []uint {
	have := map[uint]struct{}{}
	for _, id := range current {
		have[id] = struct{}{}
	}
	added := make([]uint, 0)
	for _, id := range next {
		if _, ok := have[id]; !ok {
			added = append(added, id)
		}
	}
	return added
}

func insertPolicyFrameworks(tx *gorm.DB, policyID, actor uint, ids []uint) error {
	if len(ids) == 0 {
		return nil
	}
	rows := make([]GovernancePolicyFramework, len(ids))
	for i, id := range ids {
		rows[i] = GovernancePolicyFramework{PolicyID: policyID, FrameworkID: id, CreatedBy: actor}
	}
	if err := tx.Create(&rows).Error; err != nil {
		if isForeignKeyError(err) {
			return coreerrors.NotFound("GRC_FRAMEWORK")
		}
		return err
	}
	return nil
}

func insertPolicyControls(tx *gorm.DB, policyID, actor uint, ids []uint) error {
	if len(ids) == 0 {
		return nil
	}
	rows := make([]GovernancePolicyControl, len(ids))
	for i, id := range ids {
		rows[i] = GovernancePolicyControl{PolicyID: policyID, ControlID: id, CreatedBy: actor}
	}
	if err := tx.Create(&rows).Error; err != nil {
		if isForeignKeyError(err) {
			return coreerrors.NotFound("GRC_CONTROL")
		}
		return err
	}
	return nil
}

func isForeignKeyError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "foreign key")
}

func (s *GRCService) fillGovernancePolicyLinks(item *GovernancePolicy) error {
	item.Frameworks = []GovernancePolicyLink{}
	item.Controls = []GovernancePolicyLink{}
	var frameworks []Framework
	if err := s.db.Model(&Framework{}).
		Joins("JOIN grc_governance_policy_frameworks j ON j.framework_id = grc_frameworks.id").
		Where("j.policy_id = ?", item.ID).
		Order("grc_frameworks.id").
		Find(&frameworks).Error; err != nil {
		return err
	}
	for _, framework := range frameworks {
		item.Frameworks = append(item.Frameworks, GovernancePolicyLink{
			ID: framework.ID, Code: framework.Code, Name: framework.Name, Status: framework.Status,
		})
	}
	var controls []Control
	if err := s.db.Model(&Control{}).
		Joins("JOIN grc_governance_policy_controls j ON j.control_id = grc_controls.id").
		Where("j.policy_id = ?", item.ID).
		Order("grc_controls.id").
		Find(&controls).Error; err != nil {
		return err
	}
	for _, control := range controls {
		item.Controls = append(item.Controls, GovernancePolicyLink{
			ID: control.ID, Code: control.Code, Name: control.Name, Status: control.Status,
		})
	}
	item.FrameworkCount = int64(len(item.Frameworks))
	item.ControlCount = int64(len(item.Controls))
	return nil
}

func (s *GRCService) fillGovernancePolicyCounts(items []GovernancePolicy) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]uint, len(items))
	for i := range items {
		ids[i] = items[i].ID
	}
	frameworks, err := countByPolicy(s.db, &GovernancePolicyFramework{}, ids)
	if err != nil {
		return err
	}
	controls, err := countByPolicy(s.db, &GovernancePolicyControl{}, ids)
	if err != nil {
		return err
	}
	for i := range items {
		items[i].FrameworkCount = frameworks[items[i].ID]
		items[i].ControlCount = controls[items[i].ID]
	}
	return nil
}

func countByPolicy(db *gorm.DB, model any, policyIDs []uint) (map[uint]int64, error) {
	out := map[uint]int64{}
	var rows []struct {
		PolicyID uint
		Total    int64
	}
	if err := db.Model(model).
		Select("policy_id, COUNT(*) AS total").
		Where("policy_id IN ?", policyIDs).
		Group("policy_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.PolicyID] = row.Total
	}
	return out, nil
}
