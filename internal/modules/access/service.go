package access

import (
	"errors"
	"fmt"
	"strings"
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"github.com/lallene/medcore-his/backend/internal/modules/staff"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Service struct{ db *gorm.DB }

func NewService(db *gorm.DB) *Service { return &Service{db: db} }

func (s *Service) loadMatrixOverlays() ([]rbac.MatrixOverlay, error) {
	var rows []MatrixOverride
	if e := s.db.Where("active").Find(&rows).Error; e != nil {
		return nil, e
	}
	out := make([]rbac.MatrixOverlay, 0, len(rows))
	for _, r := range rows {
		out = append(out, rbac.MatrixOverlay{FunctionCode: r.FunctionCode, Permission: r.Permission, Effect: r.Effect})
	}
	return out, nil
}

func (s *Service) loadUserOverrides(userID uint) ([]rbac.UserOverride, error) {
	var rows []PermissionOverride
	if e := s.db.Where("user_id=? AND active", userID).Find(&rows).Error; e != nil {
		return nil, e
	}
	out := make([]rbac.UserOverride, 0, len(rows))
	for _, r := range rows {
		out = append(out, rbac.UserOverride{Permission: r.Permission, Effect: r.Effect})
	}
	return out, nil
}

func (s *Service) userRoleFunctions(userID uint) (role string, functions, specialties []string, profileID uint, active bool, err error) {
	var u struct {
		Role     string
		IsActive bool
	}
	if e := s.db.Table("users").Select("role, is_active").Where("id=?", userID).Scan(&u).Error; e != nil {
		return "", nil, nil, 0, false, e
	}
	role = u.Role
	active = u.IsActive
	var profile staff.Profile
	if e := s.db.Where("user_id=?", userID).First(&profile).Error; errors.Is(e, gorm.ErrRecordNotFound) {
		return role, nil, nil, 0, active, nil
	} else if e != nil {
		return "", nil, nil, 0, false, e
	}
	profileID = profile.ID
	active = active && profile.Active
	_ = s.db.Model(&staff.Function{}).Where("profile_id=? AND active", profile.ID).Order("code").Pluck("code", &functions)
	_ = s.db.Model(&staff.Specialty{}).Where("profile_id=? AND active", profile.ID).Order("code").Pluck("code", &specialties)
	return role, functions, specialties, profileID, active, nil
}

// ComputeEffectivePermissions is the single source of truth used by auth middleware and ACC.
// Inactive users (users.is_active=false) or inactive staff profiles yield no permissions.
func (s *Service) ComputeEffectivePermissions(userID uint) ([]string, error) {
	role, functions, specialties, _, active, err := s.userRoleFunctions(userID)
	if err != nil {
		return nil, err
	}
	if !active {
		return []string{}, nil
	}
	overlays, err := s.loadMatrixOverlays()
	if err != nil {
		return nil, err
	}
	overrides, err := s.loadUserOverrides(userID)
	if err != nil {
		return nil, err
	}
	return rbac.EffectiveStaffPermissionsFull(role, functions, specialties, overlays, overrides), nil
}

func (s *Service) Explain(userID uint) ([]rbac.EffectiveEntry, error) {
	role, functions, specialties, _, _, err := s.userRoleFunctions(userID)
	if err != nil {
		return nil, err
	}
	overlays, err := s.loadMatrixOverlays()
	if err != nil {
		return nil, err
	}
	overrides, err := s.loadUserOverrides(userID)
	if err != nil {
		return nil, err
	}
	return rbac.ExplainEffective(role, functions, specialties, overlays, overrides), nil
}

func (s *Service) writeAudit(target, actor uint, action, permission, oldV, newV, function, reason string, serviceID *uint) error {
	ev := AccessAuditEvent{
		TargetUserID: target, ActorUserID: actor, Action: action, Permission: permission,
		OldValue: oldV, NewValue: newV, FunctionCode: function, ServiceID: serviceID, Reason: reason,
		CreatedAt: time.Now().UTC(),
	}
	return s.db.Create(&ev).Error
}

func (s *Service) countRBACAdmins(excludeUserID uint) (int64, error) {
	overlays, err := s.loadMatrixOverlays()
	if err != nil {
		return 0, err
	}
	var users []struct {
		ID   uint
		Role string
	}
	if e := s.db.Table("users").Select("id, role").Where("is_active").Find(&users).Error; e != nil {
		return 0, e
	}
	var n int64
	for _, u := range users {
		if u.ID == excludeUserID {
			continue
		}
		if u.Role == "admin" {
			n++
			continue
		}
		var profile staff.Profile
		if e := s.db.Where("user_id=? AND active", u.ID).First(&profile).Error; e != nil {
			continue
		}
		var functions, specialties []string
		_ = s.db.Model(&staff.Function{}).Where("profile_id=? AND active", profile.ID).Pluck("code", &functions)
		_ = s.db.Model(&staff.Specialty{}).Where("profile_id=? AND active", profile.ID).Pluck("code", &specialties)
		overrides, _ := s.loadUserOverrides(u.ID)
		perms := rbac.EffectiveStaffPermissionsFull(u.Role, functions, specialties, overlays, overrides)
		if rbac.CanAdministerRBAC(perms) {
			n++
		}
	}
	return n, nil
}

func (s *Service) assertNotLastAdmin(targetUserID uint, nextPerms []string) error {
	stillAdmin := rbac.CanAdministerRBAC(nextPerms)
	var role string
	_ = s.db.Table("users").Select("role").Where("id=?", targetUserID).Scan(&role)
	if role == "admin" {
		stillAdmin = true
	}
	if stillAdmin {
		return nil
	}
	n, err := s.countRBACAdmins(targetUserID)
	if err != nil {
		return err
	}
	if n < 1 {
		return coreerrors.Conflict("Impossible de retirer le dernier administrateur des accès (anti-lockout)")
	}
	return nil
}

func (s *Service) KPIs() (*KPIs, error) {
	k := &KPIs{}
	s.db.Table("users").Where("role<>?", "admin").Count(&k.Users)
	s.db.Table("staff_profiles").Where("active").Count(&k.Active)
	s.db.Table("staff_profiles").Where("NOT active").Count(&k.Disabled)
	var privileged int64
	overlays, _ := s.loadMatrixOverlays()
	var profiles []staff.Profile
	s.db.Where("active").Find(&profiles)
	for _, p := range profiles {
		var role string
		_ = s.db.Table("users").Select("role").Where("id=?", p.UserID).Scan(&role)
		var functions, specialties []string
		_ = s.db.Model(&staff.Function{}).Where("profile_id=? AND active", p.ID).Pluck("code", &functions)
		_ = s.db.Model(&staff.Specialty{}).Where("profile_id=? AND active", p.ID).Pluck("code", &specialties)
		overrides, _ := s.loadUserOverrides(p.UserID)
		perms := rbac.EffectiveStaffPermissionsFull(role, functions, specialties, overlays, overrides)
		if rbac.CanAdministerRBAC(perms) || rbac.HasAnyPermission(perms, "queue.read.all", "ticket.read.all") {
			privileged++
		}
	}
	k.Privileged = privileged
	s.db.Raw(`SELECT COUNT(*) FROM staff_profiles sp WHERE sp.active AND NOT EXISTS (
		SELECT 1 FROM staff_service_assignments sa WHERE sa.profile_id=sp.id AND sa.active)`).Scan(&k.WithoutService)
	s.db.Model(&PermissionOverride{}).Where("active").Distinct("user_id").Count(&k.WithOverrides)
	return k, nil
}

type listFilter struct {
	Search, Function, Status, Privilege string
	ServiceID                           *uint
	HasOverrides                        bool
	Page, Limit                         int
}

func (s *Service) ListUsers(f listFilter) (*UserList, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.Limit < 1 || f.Limit > 100 {
		f.Limit = 20
	}
	q := s.db.Model(&staff.Profile{}).Joins("JOIN users u ON u.id=staff_profiles.user_id")
	if x := strings.TrimSpace(f.Search); x != "" {
		like := "%" + strings.ToLower(x) + "%"
		q = q.Where("LOWER(CONCAT(u.name,' ',u.email,' ',staff_profiles.employee_code)) LIKE ?", like)
	}
	if f.Function != "" {
		q = q.Where("EXISTS (SELECT 1 FROM staff_functions sf WHERE sf.profile_id=staff_profiles.id AND sf.active AND sf.code=?)", f.Function)
	}
	if f.Status == "active" {
		q = q.Where("staff_profiles.active AND u.is_active")
	} else if f.Status == "disabled" {
		q = q.Where("NOT staff_profiles.active OR NOT u.is_active")
	}
	if f.ServiceID != nil {
		q = q.Where("EXISTS (SELECT 1 FROM staff_service_assignments sa WHERE sa.profile_id=staff_profiles.id AND sa.active AND sa.service_id=?)", *f.ServiceID)
	}
	if f.HasOverrides {
		q = q.Where("EXISTS (SELECT 1 FROM staff_permission_overrides o WHERE o.user_id=staff_profiles.user_id AND o.active)")
	}
	var total int64
	if e := q.Count(&total).Error; e != nil {
		return nil, coreerrors.Internal(e.Error())
	}
	var profiles []staff.Profile
	if e := q.Order("u.name").Offset((f.Page - 1) * f.Limit).Limit(f.Limit).Find(&profiles).Error; e != nil {
		return nil, coreerrors.Internal(e.Error())
	}
	items := make([]UserSummary, 0, len(profiles))
	overlays, _ := s.loadMatrixOverlays()
	for _, p := range profiles {
		sum, err := s.summarizeProfile(p, overlays)
		if err != nil {
			return nil, err
		}
		if f.Privilege == "privileged" && !sum.Privileged {
			continue
		}
		if f.Privilege == "standard" && sum.Privileged {
			continue
		}
		items = append(items, *sum)
	}
	pages := int(total) / f.Limit
	if int(total)%f.Limit > 0 {
		pages++
	}
	return &UserList{Items: items, Page: f.Page, Limit: f.Limit, Total: total, TotalPages: pages}, nil
}

func (s *Service) summarizeProfile(p staff.Profile, overlays []rbac.MatrixOverlay) (*UserSummary, error) {
	var identity struct {
		Name, Email, Role string
		IsActive          bool
	}
	if e := s.db.Table("users").Select("name,email,role,is_active").Where("id=?", p.UserID).Scan(&identity).Error; e != nil {
		return nil, e
	}
	var functions, specialties []string
	_ = s.db.Model(&staff.Function{}).Where("profile_id=? AND active", p.ID).Order("code").Pluck("code", &functions)
	_ = s.db.Model(&staff.Specialty{}).Where("profile_id=? AND active", p.ID).Order("code").Pluck("code", &specialties)
	overrides, _ := s.loadUserOverrides(p.UserID)
	perms := rbac.EffectiveStaffPermissionsFull(identity.Role, functions, specialties, overlays, overrides)
	var services []ServiceRef
	_ = s.db.Table("staff_service_assignments a").Select("a.service_id id, a.is_primary, s.code, s.name").
		Joins("JOIN organization_services s ON s.id=a.service_id").
		Where("a.profile_id=? AND a.active", p.ID).Order("a.is_primary DESC, s.name").Scan(&services)
	var ovCount int64
	s.db.Model(&PermissionOverride{}).Where("user_id=? AND active", p.UserID).Count(&ovCount)
	level := "STANDARD"
	if identity.Role == "admin" || rbac.HasAnyPermission(perms, "*") {
		level = "WILDCARD"
	} else if rbac.CanAdministerRBAC(perms) {
		level = "RBAC_ADMIN"
	} else if rbac.HasAnyPermission(perms, "queue.read.all", "ticket.read.all") {
		level = "ELEVATED"
	}
	return &UserSummary{
		ProfileID: p.ID, UserID: p.UserID, Name: identity.Name, Email: identity.Email,
		EmployeeCode: p.EmployeeCode, Active: p.Active && identity.IsActive,
		Functions: functions, Specialties: specialties, Services: services,
		AccessLevel: level, OverrideCount: int(ovCount),
		Privileged: level != "STANDARD", UpdatedAt: p.UpdatedAt.UTC().Format(time.RFC3339),
	}, nil
}

func (s *Service) GetUser(profileID uint) (*UserDetail, error) {
	var p staff.Profile
	if e := s.db.First(&p, profileID).Error; e != nil {
		return nil, coreerrors.NotFound("Utilisateur")
	}
	overlays, err := s.loadMatrixOverlays()
	if err != nil {
		return nil, err
	}
	sum, err := s.summarizeProfile(p, overlays)
	if err != nil {
		return nil, err
	}
	entries, err := s.Explain(p.UserID)
	if err != nil {
		return nil, err
	}
	var ovRows []PermissionOverride
	s.db.Where("user_id=? AND active", p.UserID).Order("permission").Find(&ovRows)
	allowed := make([]string, 0)
	for _, e := range entries {
		if e.Allowed {
			allowed = append(allowed, e.Permission)
		}
	}
	return &UserDetail{
		UserSummary: *sum, JobTitle: p.JobTitle, PrimaryDepartment: p.PrimaryDepartment,
		ProfessionalNumber: p.ProfessionalNumber, Effective: entries, EffectiveCodes: allowed,
		Overrides: ovRows, PrimaryServiceID: p.PrimaryServiceID,
	}, nil
}

type OverrideRequest struct {
	Permission string `json:"permission" binding:"required"`
	Effect     string `json:"effect" binding:"required"`
	Reason     string `json:"reason"`
}

func (s *Service) SetOverride(profileID, actorID uint, req OverrideRequest) (*UserDetail, error) {
	if req.Effect != rbac.OverrideGrant && req.Effect != rbac.OverrideDeny {
		return nil, coreerrors.BadRequest("Effect doit être GRANT ou DENY")
	}
	if req.Permission == "*" {
		return nil, coreerrors.BadRequest("Le wildcard * ne peut pas être attribué via override")
	}
	var p staff.Profile
	if e := s.db.First(&p, profileID).Error; e != nil {
		return nil, coreerrors.NotFound("Utilisateur")
	}
	var role string
	_ = s.db.Table("users").Select("role").Where("id=?", p.UserID).Scan(&role)
	if role == "admin" {
		return nil, coreerrors.BadRequest("Les overrides ne s'appliquent pas au compte admin technique")
	}

	var existing PermissionOverride
	found := s.db.Where("user_id=? AND permission=?", p.UserID, req.Permission).First(&existing).Error == nil
	old := ""
	if found {
		old = existing.Effect
	}
	now := time.Now().UTC()
	if found {
		existing.Effect, existing.Reason, existing.Active = req.Effect, req.Reason, true
		existing.UpdatedBy, existing.UpdatedAt = actorID, now
		existing.RemovedBy, existing.RemovedAt = nil, nil
		if err := s.db.Save(&existing).Error; err != nil {
			return nil, coreerrors.Internal(err.Error())
		}
	} else {
		row := PermissionOverride{
			UserID: p.UserID, Permission: req.Permission, Effect: req.Effect, Reason: req.Reason,
			Active: true, CreatedBy: actorID, UpdatedBy: actorID, CreatedAt: now, UpdatedAt: now,
		}
		if err := s.db.Create(&row).Error; err != nil {
			return nil, coreerrors.Internal(err.Error())
		}
	}

	next, err := s.ComputeEffectivePermissions(p.UserID)
	if err != nil {
		return nil, err
	}
	if err := s.assertNotLastAdmin(p.UserID, next); err != nil {
		if found {
			_ = s.db.Model(&PermissionOverride{}).Where("user_id=? AND permission=?", p.UserID, req.Permission).
				Updates(map[string]any{"effect": old, "active": true})
		} else {
			_ = s.db.Where("user_id=? AND permission=?", p.UserID, req.Permission).Delete(&PermissionOverride{})
		}
		return nil, err
	}
	action := "DIRECT_GRANT"
	if req.Effect == rbac.OverrideDeny {
		action = "DIRECT_DENY"
	}
	_ = s.writeAudit(p.UserID, actorID, action, req.Permission, old, req.Effect, "", req.Reason, nil)
	return s.GetUser(profileID)
}

func (s *Service) ClearOverride(profileID, actorID uint, permission, reason string) (*UserDetail, error) {
	var p staff.Profile
	if e := s.db.First(&p, profileID).Error; e != nil {
		return nil, coreerrors.NotFound("Utilisateur")
	}
	var row PermissionOverride
	if e := s.db.Where("user_id=? AND permission=? AND active", p.UserID, permission).First(&row).Error; e != nil {
		return nil, coreerrors.NotFound("Override")
	}
	now := time.Now().UTC()
	prev := row.Effect
	row.Active = false
	row.RemovedBy = &actorID
	row.RemovedAt = &now
	row.UpdatedBy = actorID
	row.UpdatedAt = now
	if e := s.db.Save(&row).Error; e != nil {
		return nil, coreerrors.Internal(e.Error())
	}
	next, _ := s.ComputeEffectivePermissions(p.UserID)
	if err := s.assertNotLastAdmin(p.UserID, next); err != nil {
		row.Active = true
		row.RemovedBy, row.RemovedAt = nil, nil
		_ = s.db.Save(&row)
		return nil, err
	}
	_ = s.writeAudit(p.UserID, actorID, "OVERRIDE_CLEARED", permission, prev, "", "", reason, nil)
	return s.GetUser(profileID)
}

type ActiveRequest struct {
	Active bool   `json:"active"`
	Reason string `json:"reason"`
}

func (s *Service) SetActive(profileID, actorID uint, req ActiveRequest) (*UserDetail, error) {
	var p staff.Profile
	if e := s.db.First(&p, profileID).Error; e != nil {
		return nil, coreerrors.NotFound("Utilisateur")
	}
	if !req.Active {
		if err := s.assertNotLastAdmin(p.UserID, nil); err != nil {
			return nil, err
		}
		var role string
		_ = s.db.Table("users").Select("role").Where("id=?", p.UserID).Scan(&role)
		if role == "admin" {
			var other int64
			s.db.Table("users").Where("role=? AND is_active AND id<>?", "admin", p.UserID).Count(&other)
			if other < 1 {
				return nil, coreerrors.Conflict("Impossible de désactiver le dernier compte admin technique")
			}
		}
	}
	old := p.Active
	p.Active = req.Active
	if e := s.db.Save(&p).Error; e != nil {
		return nil, coreerrors.Internal(e.Error())
	}
	if e := s.db.Table("users").Where("id=?", p.UserID).Update("is_active", req.Active).Error; e != nil {
		p.Active = old
		_ = s.db.Save(&p)
		return nil, coreerrors.Internal(e.Error())
	}
	action := "ACTIVATED"
	if !req.Active {
		action = "DEACTIVATED"
	}
	_ = s.writeAudit(p.UserID, actorID, action, "", fmt.Sprintf("%v", old), fmt.Sprintf("%v", req.Active), "", req.Reason, nil)
	return s.GetUser(profileID)
}

type FunctionsRequest struct {
	Functions []string `json:"functions"`
	Reason    string   `json:"reason"`
}

func (s *Service) SetFunctions(profileID, actorID uint, req FunctionsRequest) (*UserDetail, error) {
	staffSvc := staff.NewService(s.db)
	view, err := staffSvc.Get(profileID)
	if err != nil {
		return nil, err
	}
	active := view.Active
	payload := staff.UpsertRequest{
		UserID: view.UserID, EmployeeCode: view.EmployeeCode, JobTitle: view.JobTitle,
		PrimaryDepartment: view.PrimaryDepartment, PrimaryServiceID: view.PrimaryServiceID,
		ProfessionalNumber: view.ProfessionalNumber, Active: &active,
		Functions: req.Functions, Specialties: view.Specialties, Capabilities: view.Capabilities,
	}
	for _, a := range view.ServiceAssignments {
		if !a.IsPrimary {
			payload.SecondaryServiceIDs = append(payload.SecondaryServiceIDs, a.ServiceID)
		}
	}
	old := strings.Join(view.Functions, ",")
	if _, err := staffSvc.Upsert(profileID, payload, actorID); err != nil {
		return nil, err
	}
	next, _ := s.ComputeEffectivePermissions(view.UserID)
	if err := s.assertNotLastAdmin(view.UserID, next); err != nil {
		payload.Functions = view.Functions
		_, _ = staffSvc.Upsert(profileID, payload, actorID)
		return nil, err
	}
	_ = s.writeAudit(view.UserID, actorID, "FUNCTIONS_UPDATED", "", old, strings.Join(req.Functions, ","), "", req.Reason, nil)
	return s.GetUser(profileID)
}

type ServicesRequest struct {
	PrimaryServiceID    *uint  `json:"primaryServiceId"`
	SecondaryServiceIDs []uint `json:"secondaryServiceIds"`
	Reason              string `json:"reason"`
}

func (s *Service) SetServices(profileID, actorID uint, req ServicesRequest) (*UserDetail, error) {
	staffSvc := staff.NewService(s.db)
	view, err := staffSvc.Get(profileID)
	if err != nil {
		return nil, err
	}
	active := view.Active
	payload := staff.UpsertRequest{
		UserID: view.UserID, EmployeeCode: view.EmployeeCode, JobTitle: view.JobTitle,
		PrimaryDepartment: view.PrimaryDepartment, PrimaryServiceID: req.PrimaryServiceID,
		SecondaryServiceIDs: req.SecondaryServiceIDs, ProfessionalNumber: view.ProfessionalNumber,
		Active: &active, Functions: view.Functions, Specialties: view.Specialties, Capabilities: view.Capabilities,
	}
	oldPrimary := ""
	if view.PrimaryServiceID != nil {
		oldPrimary = fmt.Sprintf("%d", *view.PrimaryServiceID)
	}
	newPrimary := ""
	if req.PrimaryServiceID != nil {
		newPrimary = fmt.Sprintf("%d", *req.PrimaryServiceID)
	}
	if _, err := staffSvc.Upsert(profileID, payload, actorID); err != nil {
		return nil, err
	}
	_ = s.writeAudit(view.UserID, actorID, "SERVICES_UPDATED", "", oldPrimary, newPrimary, "", req.Reason, req.PrimaryServiceID)
	return s.GetUser(profileID)
}

type MatrixToggleRequest struct {
	FunctionCode string `json:"functionCode" binding:"required"`
	Permission   string `json:"permission" binding:"required"`
	Effect       string `json:"effect" binding:"required"` // GRANT | DENY | CLEAR
	Reason       string `json:"reason"`
}

func (s *Service) assertAnyRBACAdminRemains() error {
	n, err := s.countRBACAdmins(0)
	if err != nil {
		return err
	}
	if n < 1 {
		return coreerrors.Conflict("Cette modification de matrice supprimerait le dernier administrateur des accès (anti-lockout)")
	}
	return nil
}

func (s *Service) ToggleMatrix(actorID uint, req MatrixToggleRequest) error {
	if req.Permission == "*" {
		return coreerrors.BadRequest("Wildcard * interdit dans la matrice fonctionnelle")
	}
	if _, ok := rbac.StaffFunctionPermissions[req.FunctionCode]; !ok {
		return coreerrors.BadRequest("Fonction inconnue")
	}
	now := time.Now().UTC()
	if req.Effect == "CLEAR" {
		var row MatrixOverride
		if e := s.db.Where("function_code=? AND permission=? AND active", req.FunctionCode, req.Permission).First(&row).Error; e != nil {
			return nil
		}
		prevEffect := row.Effect
		row.Active = false
		row.RemovedBy = &actorID
		row.RemovedAt = &now
		row.UpdatedBy = actorID
		row.UpdatedAt = now
		if e := s.db.Save(&row).Error; e != nil {
			return coreerrors.Internal(e.Error())
		}
		if err := s.assertAnyRBACAdminRemains(); err != nil {
			row.Active = true
			row.RemovedBy, row.RemovedAt = nil, nil
			row.Effect = prevEffect
			_ = s.db.Save(&row)
			return err
		}
		_ = s.writeAudit(0, actorID, "MATRIX_CLEARED", req.Permission, prevEffect, "", req.FunctionCode, req.Reason, nil)
		return nil
	}
	if req.Effect != rbac.OverrideGrant && req.Effect != rbac.OverrideDeny {
		return coreerrors.BadRequest("Effect matrice invalide")
	}
	var existing MatrixOverride
	found := s.db.Where("function_code=? AND permission=?", req.FunctionCode, req.Permission).First(&existing).Error == nil
	old := ""
	var createdID uint
	if found {
		old = existing.Effect
		existing.Effect, existing.Reason, existing.Active = req.Effect, req.Reason, true
		existing.UpdatedBy, existing.UpdatedAt = actorID, now
		existing.RemovedBy, existing.RemovedAt = nil, nil
		if e := s.db.Save(&existing).Error; e != nil {
			return coreerrors.Internal(e.Error())
		}
	} else {
		row := MatrixOverride{
			FunctionCode: req.FunctionCode, Permission: req.Permission, Effect: req.Effect, Reason: req.Reason,
			Active: true, CreatedBy: actorID, UpdatedBy: actorID, CreatedAt: now, UpdatedAt: now,
		}
		if e := s.db.Create(&row).Error; e != nil {
			return coreerrors.Internal(e.Error())
		}
		createdID = row.ID
	}
	if err := s.assertAnyRBACAdminRemains(); err != nil {
		if found {
			_ = s.db.Model(&MatrixOverride{}).Where("function_code=? AND permission=?", req.FunctionCode, req.Permission).
				Updates(map[string]any{"effect": old, "active": old != ""})
			if old == "" {
				_ = s.db.Where("function_code=? AND permission=?", req.FunctionCode, req.Permission).Delete(&MatrixOverride{})
			}
		} else if createdID > 0 {
			_ = s.db.Delete(&MatrixOverride{}, createdID)
		}
		return err
	}
	_ = s.writeAudit(0, actorID, "MATRIX_UPDATED", req.Permission, old, req.Effect, req.FunctionCode, req.Reason, nil)
	return nil
}

func (s *Service) Matrix() (*MatrixResponse, error) {
	overlays, err := s.loadMatrixOverlays()
	if err != nil {
		return nil, err
	}
	funcs := make([]string, 0, len(rbac.StaffFunctionPermissions))
	for code := range rbac.StaffFunctionPermissions {
		funcs = append(funcs, code)
	}
	// stable order
	ordered := []string{
		"ACCUEIL", "AIDE_SOIGNANT", "INFIRMIER", "SAGE_FEMME", "BIOLOGISTE", "RADIOLOGIE",
		"CAISSIER", "FACTURATION", "COMPTABLE", "SUPPORT_AGENT", "SUPPORT_MANAGER",
		"DIRECTEUR_MEDICAL", "DIRECTEUR_ADMINISTRATIF",
	}
	seen := map[string]bool{}
	finalFuncs := make([]string, 0, len(funcs))
	for _, c := range ordered {
		if _, ok := rbac.StaffFunctionPermissions[c]; ok {
			finalFuncs = append(finalFuncs, c)
			seen[c] = true
		}
	}
	for _, c := range funcs {
		if !seen[c] {
			finalFuncs = append(finalFuncs, c)
		}
	}

	permSet := map[string]bool{}
	for _, list := range rbac.StaffFunctionPermissions {
		for _, p := range list {
			permSet[p] = true
		}
	}
	for _, p := range rbac.StaffPhysicianPermissions {
		permSet[p] = true
	}
	for _, o := range overlays {
		permSet[o.Permission] = true
	}
	permissions := make([]string, 0, len(permSet))
	for p := range permSet {
		permissions = append(permissions, p)
	}
	// sort
	for i := 0; i < len(permissions); i++ {
		for j := i + 1; j < len(permissions); j++ {
			if permissions[j] < permissions[i] {
				permissions[i], permissions[j] = permissions[j], permissions[i]
			}
		}
	}

	cells := make([]MatrixCell, 0)
	for _, f := range finalFuncs {
		resolved := map[string]bool{}
		for _, p := range rbac.FunctionPermissionsResolved(f, overlays) {
			resolved[p] = true
		}
		for _, p := range permissions {
			cells = append(cells, MatrixCell{FunctionCode: f, Permission: p, Allowed: resolved[p]})
		}
	}
	return &MatrixResponse{Functions: finalFuncs, Permissions: permissions, Cells: cells, Overlays: overlays}, nil
}

func (s *Service) PermissionsCatalog() []PermissionCatalogItem {
	metas := rbac.AllPermissionMetas()
	overlays, _ := s.loadMatrixOverlays()
	out := make([]PermissionCatalogItem, 0, len(metas))
	for _, m := range metas {
		funcs := []string{}
		for code := range rbac.StaffFunctionPermissions {
			for _, p := range rbac.FunctionPermissionsResolved(code, overlays) {
				if p == m.Key {
					funcs = append(funcs, code)
					break
				}
			}
		}
		for i := 0; i < len(funcs); i++ {
			for j := i + 1; j < len(funcs); j++ {
				if funcs[j] < funcs[i] {
					funcs[i], funcs[j] = funcs[j], funcs[i]
				}
			}
		}
		out = append(out, PermissionCatalogItem{
			Key: m.Key, Label: m.Label, Domain: m.Domain, ScopeHint: m.ScopeHint,
			Sensitive: m.Sensitive, Functions: funcs,
		})
	}
	return out
}

func (s *Service) Audit(targetUserID uint, limit int) ([]AccessAuditEvent, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	var rows []AccessAuditEvent
	q := s.db.Order("created_at DESC").Limit(limit)
	if targetUserID > 0 {
		q = q.Where("target_user_id=?", targetUserID)
	}
	if e := q.Find(&rows).Error; e != nil {
		return nil, coreerrors.Internal(e.Error())
	}
	return rows, nil
}

func (s *Service) Simulate(profileID uint) (*Simulation, error) {
	detail, err := s.GetUser(profileID)
	if err != nil {
		return nil, err
	}
	perms := detail.EffectiveCodes
	has := func(codes ...string) bool { return rbac.HasAnyPermission(perms, codes...) }
	nav := []SimNavItem{
		{Title: "Dashboard", Href: "/dashboard", Visible: has("dashboard.read")},
		{Title: "Patients", Href: "/patients", Visible: has("patients:read")},
		{Title: "File patients", Href: "/queue", Visible: has("queue.reception.read", "queue.triage.read", "queue.doctor.read", "queue.read.service", "queue.read.all")},
		{Title: "Accueil", Href: "/queue/reception", Visible: has("queue.reception.read")},
		{Title: "Pré-triage", Href: "/queue/triage", Visible: has("queue.triage.read")},
		{Title: "File médecin", Href: "/queue/doctor", Visible: has("queue.doctor.read")},
		{Title: "Facturation", Href: "/billing", Visible: has("billing.read")},
		{Title: "Caisse", Href: "/cash", Visible: has("cash.session.read", "cash.payment.create")},
		{Title: "Personnel", Href: "/admin/staff", Visible: has("staff.read")},
		{Title: "Centre d'accès", Href: "/admin/access", Visible: has("rbac.read", "staff.manage", "staff.read")},
		{Title: "Organisation", Href: "/admin/organization", Visible: has("organization.read")},
		{Title: "Automated QA", Href: "/admin/qa", Visible: has("qa.read")},
		{Title: "Service Desk", Href: "/support/tickets", Visible: has("ticket.read.service", "ticket.read.all")},
	}
	actions := []SimAction{
		{Code: "queue.doctor.take", Label: "Prendre en charge (file médecin)", Allowed: has("queue.doctor.take")},
		{Code: "queue.checkin", Label: "Check-in patient", Allowed: has("queue.checkin")},
		{Code: "queue.triage.update", Label: "Valider triage", Allowed: has("queue.triage.update")},
		{Code: "staff.manage", Label: "Gérer le personnel", Allowed: has("staff.manage")},
		{Code: "rbac.override.manage", Label: "Gérer exceptions d'accès", Allowed: has("rbac.override.manage", "staff.manage")},
		{Code: "billing.cancel", Label: "Annuler une facture", Allowed: has("billing.cancel")},
	}
	return &Simulation{
		UserID: detail.UserID, ProfileID: detail.ProfileID, Name: detail.Name,
		Navigation: nav, Actions: actions, Services: detail.Services,
		Permissions: perms, Note: "Simulation informative read-only — pas d'impersonation JWT.",
	}, nil
}

// Ensure unused clause import still compiles if OnConflict unused — keep for future.
var _ = clause.OnConflict{}
