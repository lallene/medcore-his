package grc

import (
	"errors"
	"strings"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"gorm.io/gorm"
)

func normalizePageFilter(page, limit int) (int, int) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}

	return page, limit
}

func totalPages(total int64, limit int) int {
	if total == 0 {
		return 0
	}

	return int((total + int64(limit) - 1) / int64(limit))
}

func (s *GRCService) SaveFramework(id, actor uint, r FrameworkRequest) (*Framework, error) {
	r.Code = normalizeCode(r.Code)
	r.Name = normalizeText(r.Name)
	r.Version = normalizeText(r.Version)
	r.Description = normalizeText(r.Description)
	r.Source = normalizeText(r.Source)
	r.Status = normalizeEnum(r.Status)

	if r.Code == "" || r.Name == "" || r.Version == "" {
		return nil, coreerrors.BadRequest("Code, nom et version du référentiel obligatoires")
	}
	if !isValidFrameworkStatus(r.Status) {
		return nil, coreerrors.BadRequest("Statut du référentiel invalide")
	}

	var framework Framework
	var oldAuditValue string
	action := auditActionCreate

	if id > 0 {
		if err := s.db.First(&framework, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, coreerrors.NotFound("GRC_FRAMEWORK")
			}
			return nil, err
		}

		action = auditActionUpdate
		oldAuditValue = frameworkAuditValue(framework)
	} else {
		framework.CreatedBy = actor
	}

	framework.Code = r.Code
	framework.Name = r.Name
	framework.Version = r.Version
	framework.Description = r.Description
	framework.Source = r.Source
	framework.Status = r.Status
	framework.PublishedAt = r.PublishedAt
	framework.UpdatedBy = actor

	if err := s.saveWithAudit(func(tx *gorm.DB) error {
		return tx.Select("*").Save(&framework).Error
	}, auditModuleGRCFramework, action, func() uint { return framework.ID }, actor, oldAuditValue, func() string { return frameworkAuditValue(framework) }); err != nil {
		if isDuplicateError(err) {
			return nil, coreerrors.Conflict("Code et version de référentiel déjà utilisés")
		}
		return nil, err
	}

	return s.FindFramework(framework.ID)
}

func (s *GRCService) FindFramework(id uint) (*Framework, error) {
	var framework Framework

	err := s.db.First(&framework, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, coreerrors.NotFound("GRC_FRAMEWORK")
	}
	if err != nil {
		return nil, err
	}

	return &framework, nil
}

func (s *GRCService) ListFrameworks(f FrameworkFilter) (*FrameworkPage, error) {
	f.Page, f.Limit = normalizePageFilter(f.Page, f.Limit)
	f.Code = normalizeCode(f.Code)
	f.Status = normalizeEnum(f.Status)

	if f.Status != "" && !isValidFrameworkStatus(f.Status) {
		return nil, coreerrors.BadRequest("Statut du référentiel invalide")
	}

	q := s.db.Model(&Framework{})

	if search := normalizeText(f.Search); search != "" {
		like := "%" + strings.ToLower(search) + "%"
		q = q.Where(
			"(LOWER(code) LIKE ? OR LOWER(name) LIKE ? OR LOWER(version) LIKE ?)",
			like,
			like,
			like,
		)
	}
	if f.Code != "" {
		q = q.Where("code = ?", f.Code)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, err
	}

	frameworks := []Framework{}
	if err := q.
		Order("code, version, id").
		Offset((f.Page - 1) * f.Limit).
		Limit(f.Limit).
		Find(&frameworks).
		Error; err != nil {
		return nil, err
	}

	return &FrameworkPage{
		Items:      frameworks,
		Page:       f.Page,
		Limit:      f.Limit,
		Total:      total,
		TotalPages: totalPages(total, f.Limit),
	}, nil
}

func (s *GRCService) SaveRequirement(id, actor uint, r RequirementRequest) (*Requirement, error) {
	r.Code = normalizeCode(r.Code)
	r.Title = normalizeText(r.Title)
	r.Description = normalizeText(r.Description)
	r.Status = normalizeEnum(r.Status)

	if r.FrameworkID == 0 {
		return nil, coreerrors.BadRequest("Référentiel de l'exigence obligatoire")
	}
	if r.Code == "" || r.Title == "" {
		return nil, coreerrors.BadRequest("Code et titre de l'exigence obligatoires")
	}
	if !isValidRequirementStatus(r.Status) {
		return nil, coreerrors.BadRequest("Statut de l'exigence invalide")
	}

	if _, err := s.FindFramework(r.FrameworkID); err != nil {
		return nil, err
	}

	var requirement Requirement
	var oldAuditValue string
	action := auditActionCreate

	if id > 0 {
		if err := s.db.First(&requirement, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, coreerrors.NotFound("GRC_REQUIREMENT")
			}
			return nil, err
		}

		action = auditActionUpdate
		oldAuditValue = requirementAuditValue(requirement)

		if requirement.FrameworkID != r.FrameworkID {
			var children int64
			if err := s.db.Model(&Requirement{}).
				Where("parent_requirement_id = ?", id).
				Count(&children).
				Error; err != nil {
				return nil, err
			}
			if children > 0 {
				return nil, coreerrors.Conflict("Impossible de changer le référentiel d'une exigence ayant des sous-exigences")
			}
		}
	} else {
		requirement.CreatedBy = actor
	}

	if err := s.validateRequirementParent(id, r.FrameworkID, r.ParentRequirementID); err != nil {
		return nil, err
	}

	requirement.FrameworkID = r.FrameworkID
	requirement.Code = r.Code
	requirement.Title = r.Title
	requirement.Description = r.Description
	requirement.ParentRequirementID = r.ParentRequirementID
	requirement.Status = r.Status
	requirement.UpdatedBy = actor

	if err := s.saveWithAudit(func(tx *gorm.DB) error {
		return tx.Select("*").Save(&requirement).Error
	}, auditModuleGRCRequirement, action, func() uint { return requirement.ID }, actor, oldAuditValue, func() string { return requirementAuditValue(requirement) }); err != nil {
		if isDuplicateError(err) {
			return nil, coreerrors.Conflict("Code d'exigence déjà utilisé dans ce référentiel")
		}
		return nil, err
	}

	return s.FindRequirement(requirement.ID)
}

// validateRequirementParent checks that the parent exists in the same
// framework and that attaching it would not create a hierarchy cycle.
func (s *GRCService) validateRequirementParent(id, frameworkID uint, parentID *uint) error {
	if parentID == nil {
		return nil
	}
	if id > 0 && *parentID == id {
		return coreerrors.Conflict("Une exigence ne peut pas être son propre parent")
	}

	var parent Requirement
	if err := s.db.First(&parent, *parentID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return coreerrors.NotFound("GRC_REQUIREMENT")
		}
		return err
	}
	if parent.FrameworkID != frameworkID {
		return coreerrors.Conflict("L'exigence parente doit appartenir au même référentiel")
	}

	// A new requirement has no descendants, so it cannot close a cycle.
	if id == 0 {
		return nil
	}

	// Walk up from the parent: reaching id means id would become its own
	// ancestor. The visited set bounds the walk to the rows actually stored.
	visited := map[uint]bool{}
	current := parent.ParentRequirementID
	for current != nil {
		if *current == id {
			return coreerrors.Conflict("La hiérarchie des exigences ne peut pas contenir de cycle")
		}
		if visited[*current] {
			return coreerrors.Conflict("La hiérarchie des exigences existante contient un cycle")
		}
		visited[*current] = true

		var ancestor Requirement
		if err := s.db.
			Select("id", "parent_requirement_id").
			First(&ancestor, *current).
			Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return coreerrors.NotFound("GRC_REQUIREMENT")
			}
			return err
		}
		current = ancestor.ParentRequirementID
	}

	return nil
}

func (s *GRCService) FindRequirement(id uint) (*Requirement, error) {
	var requirement Requirement

	err := s.db.
		Preload("Framework").
		Preload("ParentRequirement").
		First(&requirement, id).
		Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, coreerrors.NotFound("GRC_REQUIREMENT")
	}
	if err != nil {
		return nil, err
	}
	counts, err := s.requirementMappedControls(s.db, []uint{requirement.ID})
	if err != nil {
		return nil, err
	}
	requirement.MappedControls = counts[requirement.ID]

	return &requirement, nil
}

func (s *GRCService) ListRequirements(f RequirementFilter) (*RequirementPage, error) {
	f.Page, f.Limit = normalizePageFilter(f.Page, f.Limit)
	f.Status = normalizeEnum(f.Status)

	if f.Status != "" && !isValidRequirementStatus(f.Status) {
		return nil, coreerrors.BadRequest("Statut de l'exigence invalide")
	}

	q := s.db.Model(&Requirement{})

	if search := normalizeText(f.Search); search != "" {
		like := "%" + strings.ToLower(search) + "%"
		q = q.Where("(LOWER(code) LIKE ? OR LOWER(title) LIKE ?)", like, like)
	}
	if f.FrameworkID != nil {
		q = q.Where("framework_id = ?", *f.FrameworkID)
	}
	if f.ParentRequirementID != nil {
		q = q.Where("parent_requirement_id = ?", *f.ParentRequirementID)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.Mapped != nil {
		if *f.Mapped {
			q = q.Where(requirementMappedSQL)
		} else {
			q = q.Where("NOT " + requirementMappedSQL)
		}
	}
	if f.ControlID != nil {
		q = q.Where("EXISTS (SELECT 1 FROM grc_requirement_controls rcf WHERE rcf.requirement_id = grc_requirements.id AND rcf.control_id = ?)", *f.ControlID)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, err
	}

	requirements := []Requirement{}
	if err := q.
		Preload("Framework").
		Preload("ParentRequirement").
		Order("framework_id, code, id").
		Offset((f.Page - 1) * f.Limit).
		Limit(f.Limit).
		Find(&requirements).
		Error; err != nil {
		return nil, err
	}

	ids := make([]uint, len(requirements))
	for i := range requirements {
		ids[i] = requirements[i].ID
	}
	counts, err := s.requirementMappedControls(s.db, ids)
	if err != nil {
		return nil, err
	}
	for i := range requirements {
		requirements[i].MappedControls = counts[requirements[i].ID]
	}

	return &RequirementPage{
		Items:      requirements,
		Page:       f.Page,
		Limit:      f.Limit,
		Total:      total,
		TotalPages: totalPages(total, f.Limit),
	}, nil
}

func (s *GRCService) SaveControl(id, actor uint, r ControlRequest) (*Control, error) {
	normalizeControlRequest(&r)
	if err := validateControlRequest(r); err != nil {
		return nil, err
	}

	var control Control
	var oldAuditValue string
	action := auditActionCreate

	if id > 0 {
		if err := s.db.First(&control, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, coreerrors.NotFound("GRC_CONTROL")
			}
			return nil, err
		}

		action = auditActionUpdate
		oldAuditValue = controlAuditValue(control)
	} else {
		control.CreatedBy = actor
	}

	// Relations are validated when assigned only, so a control stays editable
	// after its owner or service was deactivated.
	if id == 0 || optionalIDChanged(control.OwningServiceID, r.OwningServiceID) {
		if err := s.validateOwningService(r.OwningServiceID); err != nil {
			return nil, err
		}
	}
	if id == 0 || optionalIDChanged(control.OwnerProfileID, r.OwnerProfileID) {
		if err := s.validateRiskProfile(r.OwnerProfileID, "Le propriétaire du contrôle est inactif"); err != nil {
			return nil, err
		}
	}

	control.Code = r.Code
	control.Name = r.Name
	control.Description = r.Description
	control.Category = r.Category
	control.OwnerProfileID = r.OwnerProfileID
	control.OwningServiceID = r.OwningServiceID
	control.Status = r.Status
	control.UpdatedBy = actor

	if err := s.saveWithAudit(func(tx *gorm.DB) error {
		return tx.Select("*").Save(&control).Error
	}, auditModuleGRCControl, action, func() uint { return control.ID }, actor, oldAuditValue, func() string { return controlAuditValue(control) }); err != nil {
		if isDuplicateError(err) {
			return nil, coreerrors.Conflict("Code contrôle déjà utilisé")
		}
		return nil, err
	}

	return s.FindControl(control.ID)
}

func (s *GRCService) FindControl(id uint) (*Control, error) {
	var control Control

	err := s.db.
		Preload("OwningService").
		Preload("OwnerProfile").
		First(&control, id).
		Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, coreerrors.NotFound("GRC_CONTROL")
	}
	if err != nil {
		return nil, err
	}
	single := []Control{control}
	if err := s.fillControlCounts(single); err != nil {
		return nil, err
	}

	return &single[0], nil
}

func (s *GRCService) ListControls(f ControlFilter) (*ControlPage, error) {
	f.Page, f.Limit = normalizePageFilter(f.Page, f.Limit)
	f.Category = normalizeText(f.Category)
	f.Status = normalizeEnum(f.Status)

	if f.Status != "" && !isValidControlStatus(f.Status) {
		return nil, coreerrors.BadRequest("Statut du contrôle invalide")
	}

	q := s.db.Model(&Control{})

	if search := normalizeText(f.Search); search != "" {
		like := "%" + strings.ToLower(search) + "%"
		q = q.Where(
			"(LOWER(code) LIKE ? OR LOWER(name) LIKE ? OR LOWER(category) LIKE ?)",
			like,
			like,
			like,
		)
	}
	if f.Category != "" {
		q = q.Where("category = ?", f.Category)
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
	if f.RiskID != nil {
		q = q.Where("EXISTS (SELECT 1 FROM grc_control_risks cr WHERE cr.control_id = grc_controls.id AND cr.risk_id = ?)", *f.RiskID)
	}
	if f.RequirementID != nil {
		q = q.Where("EXISTS (SELECT 1 FROM grc_requirement_controls rc WHERE rc.control_id = grc_controls.id AND rc.requirement_id = ?)", *f.RequirementID)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, err
	}

	controls := []Control{}
	if err := q.
		Preload("OwningService").
		Preload("OwnerProfile").
		Order("code, id").
		Offset((f.Page - 1) * f.Limit).
		Limit(f.Limit).
		Find(&controls).
		Error; err != nil {
		return nil, err
	}
	if err := s.fillControlCounts(controls); err != nil {
		return nil, err
	}

	return &ControlPage{
		Items:      controls,
		Page:       f.Page,
		Limit:      f.Limit,
		Total:      total,
		TotalPages: totalPages(total, f.Limit),
	}, nil
}

func (s *GRCService) SaveControlImplementation(id, actor uint, r ControlImplementationRequest) (*ControlImplementation, error) {
	normalizeControlImplementationRequest(&r)
	if err := validateControlImplementationRequest(r); err != nil {
		return nil, err
	}

	var implementation ControlImplementation
	var oldAuditValue string
	action := auditActionCreate

	if id > 0 {
		if err := s.db.First(&implementation, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, coreerrors.NotFound("GRC_CONTROL_IMPLEMENTATION")
			}
			return nil, err
		}

		action = auditActionUpdate
		oldAuditValue = controlImplementationAuditValue(implementation)
	} else {
		implementation.CreatedBy = actor
	}

	// Assignment rules apply to new links only, so an existing implementation
	// stays editable after its control or asset has been deactivated.
	if id == 0 || implementation.ControlID != r.ControlID {
		if err := s.validateAssignableControl(r.ControlID); err != nil {
			return nil, err
		}
	}
	if id == 0 || implementation.AssetID != r.AssetID {
		if err := s.validateAssignableAsset(r.AssetID); err != nil {
			return nil, err
		}
	}

	if id == 0 || optionalIDChanged(implementation.OwningServiceID, r.OwningServiceID) {
		if err := s.validateOwningService(r.OwningServiceID); err != nil {
			return nil, err
		}
	}
	if id == 0 || optionalIDChanged(implementation.OwnerProfileID, r.OwnerProfileID) {
		if err := s.validateRiskProfile(r.OwnerProfileID, "Le propriétaire de l'implémentation est inactif"); err != nil {
			return nil, err
		}
	}

	implementation.ControlID = r.ControlID
	implementation.AssetID = r.AssetID
	implementation.OwningServiceID = r.OwningServiceID
	implementation.OwnerProfileID = r.OwnerProfileID
	implementation.Status = r.Status
	implementation.ImplementationDetails = r.ImplementationDetails
	implementation.ImplementedAt = r.ImplementedAt
	implementation.LastReviewedAt = r.LastReviewedAt
	implementation.UpdatedBy = actor

	if err := s.saveWithAudit(func(tx *gorm.DB) error {
		return tx.Select("*").Save(&implementation).Error
	}, auditModuleGRCControlImplementation, action, func() uint { return implementation.ID }, actor, oldAuditValue, func() string { return controlImplementationAuditValue(implementation) }); err != nil {
		if isDuplicateError(err) {
			return nil, coreerrors.Conflict("Ce contrôle est déjà implémenté sur cet actif")
		}
		return nil, err
	}

	return s.FindControlImplementation(implementation.ID)
}

func (s *GRCService) validateAssignableControl(id uint) error {
	control, err := s.FindControl(id)
	if err != nil {
		return err
	}
	if control.Status == ControlStatusInactive || control.Status == ControlStatusRetired {
		return coreerrors.Conflict("Le contrôle n'est pas assignable")
	}

	return nil
}

func (s *GRCService) validateAssignableAsset(id uint) error {
	var asset Asset
	if err := s.db.First(&asset, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return coreerrors.NotFound("GRC_ASSET")
		}
		return err
	}
	if asset.Status != AssetStatusActive {
		return coreerrors.Conflict("L'actif de l'implémentation est inactif")
	}

	return nil
}

func (s *GRCService) FindControlImplementation(id uint) (*ControlImplementation, error) {
	var implementation ControlImplementation

	err := s.db.
		Preload("Control").
		Preload("Asset").
		Preload("OwningService").
		Preload("OwnerProfile").
		First(&implementation, id).
		Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, coreerrors.NotFound("GRC_CONTROL_IMPLEMENTATION")
	}
	if err != nil {
		return nil, err
	}

	return &implementation, nil
}

func (s *GRCService) ListControlImplementations(f ControlImplementationFilter) (*ControlImplementationPage, error) {
	f.Page, f.Limit = normalizePageFilter(f.Page, f.Limit)
	f.Status = normalizeEnum(f.Status)

	if f.Status != "" && !isValidControlImplementationStatus(f.Status) {
		return nil, coreerrors.BadRequest("Statut de l'implémentation invalide")
	}

	q := s.db.Model(&ControlImplementation{})

	if f.ControlID != nil {
		q = q.Where("control_id = ?", *f.ControlID)
	}
	if f.AssetID != nil {
		q = q.Where("asset_id = ?", *f.AssetID)
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

	implementations := []ControlImplementation{}
	if err := q.
		Preload("Control").
		Preload("Asset").
		Preload("OwningService").
		Preload("OwnerProfile").
		Order("control_id, asset_id, id").
		Offset((f.Page - 1) * f.Limit).
		Limit(f.Limit).
		Find(&implementations).
		Error; err != nil {
		return nil, err
	}

	return &ControlImplementationPage{
		Items:      implementations,
		Page:       f.Page,
		Limit:      f.Limit,
		Total:      total,
		TotalPages: totalPages(total, f.Limit),
	}, nil
}

func (s *GRCService) LinkRequirementControl(actor uint, r RequirementControlRequest) (*RequirementControl, error) {
	if r.RequirementID == 0 || r.ControlID == 0 {
		return nil, coreerrors.BadRequest("Exigence et contrôle obligatoires")
	}

	if err := s.db.First(&Requirement{}, r.RequirementID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, coreerrors.NotFound("GRC_REQUIREMENT")
		}
		return nil, err
	}
	if err := s.db.First(&Control{}, r.ControlID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, coreerrors.NotFound("GRC_CONTROL")
		}
		return nil, err
	}

	link := RequirementControl{
		RequirementID: r.RequirementID,
		ControlID:     r.ControlID,
		CreatedBy:     actor,
	}
	if err := s.saveWithAudit(func(tx *gorm.DB) error {
		return tx.Create(&link).Error
	}, auditModuleGRCRequirementControl, auditActionLink, func() uint { return link.ID }, actor, "", func() string { return requirementControlAuditValue(link) }); err != nil {
		if isDuplicateError(err) {
			return nil, coreerrors.Conflict("Ce contrôle est déjà lié à cette exigence")
		}
		return nil, err
	}

	return &link, nil
}

func (s *GRCService) UnlinkRequirementControl(actor, requirementID, controlID uint) error {
	if requirementID == 0 || controlID == 0 {
		return coreerrors.BadRequest("Exigence et contrôle obligatoires")
	}

	// Read the link first so the audit keeps the snapshot of what was removed.
	var link RequirementControl
	if err := s.db.
		Where("requirement_id = ? AND control_id = ?", requirementID, controlID).
		First(&link).
		Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return coreerrors.NotFound("GRC_REQUIREMENT_CONTROL")
		}
		return err
	}

	if err := s.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Delete(&RequirementControl{}, link.ID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return coreerrors.NotFound("GRC_REQUIREMENT_CONTROL")
		}
		return writeGRCAudit(tx, auditModuleGRCRequirementControl, auditActionUnlink, link.ID, actor, requirementControlAuditValue(link), "")
	}); err != nil {
		return err
	}

	return nil
}

func (s *GRCService) LinkControlRisk(actor uint, r ControlRiskRequest) (*ControlRisk, error) {
	if r.ControlID == 0 || r.RiskID == 0 {
		return nil, coreerrors.BadRequest("Contrôle et risque obligatoires")
	}

	if err := s.db.First(&Control{}, r.ControlID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, coreerrors.NotFound("GRC_CONTROL")
		}
		return nil, err
	}
	if err := s.db.First(&Risk{}, r.RiskID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, coreerrors.NotFound("GRC_RISK")
		}
		return nil, err
	}

	link := ControlRisk{
		ControlID: r.ControlID,
		RiskID:    r.RiskID,
		CreatedBy: actor,
	}
	if err := s.saveWithAudit(func(tx *gorm.DB) error {
		return tx.Create(&link).Error
	}, auditModuleGRCControlRisk, auditActionLink, func() uint { return link.ID }, actor, "", func() string { return controlRiskAuditValue(link) }); err != nil {
		if isDuplicateError(err) {
			return nil, coreerrors.Conflict("Ce contrôle est déjà lié à ce risque")
		}
		return nil, err
	}

	return &link, nil
}

func (s *GRCService) UnlinkControlRisk(actor, controlID, riskID uint) error {
	if controlID == 0 || riskID == 0 {
		return coreerrors.BadRequest("Contrôle et risque obligatoires")
	}

	// Read the link first so the audit keeps the snapshot of what was removed.
	var link ControlRisk
	if err := s.db.
		Where("control_id = ? AND risk_id = ?", controlID, riskID).
		First(&link).
		Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return coreerrors.NotFound("GRC_CONTROL_RISK")
		}
		return err
	}

	if err := s.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Delete(&ControlRisk{}, link.ID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return coreerrors.NotFound("GRC_CONTROL_RISK")
		}
		return writeGRCAudit(tx, auditModuleGRCControlRisk, auditActionUnlink, link.ID, actor, controlRiskAuditValue(link), "")
	}); err != nil {
		return err
	}

	return nil
}
