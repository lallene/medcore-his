package staff

import (
	"errors"
	"sort"
	"strings"
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Service struct{ db *gorm.DB }

func NewService(db *gorm.DB) *Service { return &Service{db: db} }

func activeCodes[T Function | Specialty | Capability](db *gorm.DB, profileID uint, out *[]string) error {
	return db.Model(new(T)).Where("profile_id=? AND active", profileID).Order("code").Pluck("code", out).Error
}

func EffectivePermissions(db *gorm.DB, userID uint, legacyRole string) ([]string, error) {
	if legacyRole == "admin" {
		return []string{"*"}, nil
	}
	var profile Profile
	if e := db.Where("user_id=? AND active", userID).First(&profile).Error; errors.Is(e, gorm.ErrRecordNotFound) {
		return MergePermissions(legacyRole, nil, nil), nil
	} else if e != nil {
		return nil, e
	}
	var functions, specialties []string
	if e := activeCodes[Function](db, profile.ID, &functions); e != nil {
		return nil, e
	}
	if e := activeCodes[Specialty](db, profile.ID, &specialties); e != nil {
		return nil, e
	}
	return MergePermissions(legacyRole, functions, specialties), nil
}

func (s *Service) view(profile Profile) (*View, error) {
	v := &View{Profile: profile, Functions: []string{}, Specialties: []string{}, Capabilities: []string{}}
	var identity struct {
		Name       string
		Email      string
		LegacyRole string
	}
	if e := s.db.Table("users").Select("name,email,role legacy_role").Where("id=?", profile.UserID).Scan(&identity).Error; e != nil {
		return nil, e
	}
	v.Name, v.Email, v.LegacyRole = identity.Name, identity.Email, identity.LegacyRole
	if e := activeCodes[Function](s.db, profile.ID, &v.Functions); e != nil {
		return nil, e
	}
	if e := activeCodes[Specialty](s.db, profile.ID, &v.Specialties); e != nil {
		return nil, e
	}
	if e := activeCodes[Capability](s.db, profile.ID, &v.Capabilities); e != nil {
		return nil, e
	}
	v.EffectivePermissions = MergePermissions(v.LegacyRole, v.Functions, v.Specialties)
	// Apply matrix overlays + user GRANT/DENY (same rules as access module / auth middleware).
	var matrixRows []struct {
		FunctionCode string
		Permission   string
		Effect       string
	}
	_ = s.db.Table("rbac_matrix_overrides").Select("function_code, permission, effect").Where("active").Scan(&matrixRows)
	overlays := make([]rbac.MatrixOverlay, 0, len(matrixRows))
	for _, r := range matrixRows {
		overlays = append(overlays, rbac.MatrixOverlay{FunctionCode: r.FunctionCode, Permission: r.Permission, Effect: r.Effect})
	}
	var ovRows []struct {
		Permission string
		Effect     string
	}
	_ = s.db.Table("staff_permission_overrides").Select("permission, effect").Where("user_id=? AND active", profile.UserID).Scan(&ovRows)
	overrides := make([]rbac.UserOverride, 0, len(ovRows))
	for _, r := range ovRows {
		overrides = append(overrides, rbac.UserOverride{Permission: r.Permission, Effect: r.Effect})
	}
	v.EffectivePermissions = rbac.EffectiveStaffPermissionsFull(v.LegacyRole, v.Functions, v.Specialties, overlays, overrides)
	if e := s.db.Table("staff_service_assignments a").Select("a.service_id,a.is_primary,a.active,s.code,s.name").Joins("JOIN organization_services s ON s.id=a.service_id").Where("a.profile_id=? AND a.active", profile.ID).Order("a.is_primary DESC,s.name").Scan(&v.ServiceAssignments).Error; e != nil {
		return nil, e
	}
	return v, nil
}

func (s *Service) Get(id uint) (*View, error) {
	var p Profile
	if e := s.db.First(&p, id).Error; e != nil {
		return nil, coreerrors.NotFound("STAFF_PROFILE")
	}
	return s.view(p)
}

func (s *Service) List(f Filter) (*Page, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.Limit < 1 || f.Limit > 100 {
		f.Limit = 20
	}
	q := s.db.Model(&Profile{}).Joins("JOIN users u ON u.id=staff_profiles.user_id")
	if x := strings.TrimSpace(f.Search); x != "" {
		like := "%" + strings.ToLower(x) + "%"
		q = q.Where("LOWER(CONCAT(u.name,' ',u.email,' ',staff_profiles.employee_code)) LIKE ?", like)
	}
	if f.Active == "true" {
		q = q.Where("staff_profiles.active")
	} else if f.Active == "false" {
		q = q.Where("NOT staff_profiles.active")
	}
	if f.Function != "" {
		q = q.Where("EXISTS (SELECT 1 FROM staff_functions sf WHERE sf.profile_id=staff_profiles.id AND sf.active AND sf.code=?)", f.Function)
	}
	if f.Specialty != "" {
		q = q.Where("EXISTS (SELECT 1 FROM staff_specialties ss WHERE ss.profile_id=staff_profiles.id AND ss.active AND ss.code=?)", f.Specialty)
	}
	if f.ServiceID != nil {
		q = q.Where("EXISTS (SELECT 1 FROM staff_service_assignments sa WHERE sa.profile_id=staff_profiles.id AND sa.active AND sa.service_id=?)", *f.ServiceID)
	}
	var total int64
	if e := q.Count(&total).Error; e != nil {
		return nil, e
	}
	var profiles []Profile
	if e := q.Order("u.name,staff_profiles.id").Offset((f.Page - 1) * f.Limit).Limit(f.Limit).Find(&profiles).Error; e != nil {
		return nil, e
	}
	out := &Page{Items: []View{}, Page: f.Page, Limit: f.Limit, Total: total, TotalPages: int((total + int64(f.Limit) - 1) / int64(f.Limit))}
	for _, p := range profiles {
		v, e := s.view(p)
		if e != nil {
			return nil, e
		}
		out.Items = append(out.Items, *v)
	}
	return out, nil
}

func normalize(values []string, valid func(string) bool) ([]string, error) {
	set := map[string]bool{}
	for _, raw := range values {
		c := strings.ToUpper(strings.TrimSpace(raw))
		if !valid(c) {
			return nil, coreerrors.BadRequest("Valeur Staff invalide: " + c)
		}
		set[c] = true
	}
	out := make([]string, 0, len(set))
	for c := range set {
		out = append(out, c)
	}
	sort.Strings(out)
	return out, nil
}

func syncAssignments[T Function | Specialty | Capability](tx *gorm.DB, profileID, actor uint, dimension string, wanted []string) error {
	now := time.Now()
	var current []T
	if e := tx.Where("profile_id=?", profileID).Find(&current).Error; e != nil {
		return e
	}
	type state struct {
		ID     uint
		Code   string
		Active bool
	}
	states := map[string]state{}
	for _, x := range current {
		var s state
		switch v := any(x).(type) {
		case Function:
			s = state{v.ID, v.Code, v.Active}
		case Specialty:
			s = state{v.ID, v.Code, v.Active}
		case Capability:
			s = state{v.ID, v.Code, v.Active}
		}
		states[s.Code] = s
	}
	want := map[string]bool{}
	for _, c := range wanted {
		want[c] = true
		if prior, ok := states[c]; !ok {
			var x any
			switch any(*new(T)).(type) {
			case Function:
				x = &Function{ProfileID: profileID, Code: c, Active: true, AssignedBy: actor, AssignedAt: now}
			case Specialty:
				x = &Specialty{ProfileID: profileID, Code: c, Active: true, AssignedBy: actor, AssignedAt: now}
			case Capability:
				x = &Capability{ProfileID: profileID, Code: c, Active: true, AssignedBy: actor, AssignedAt: now}
			}
			if e := tx.Create(x).Error; e != nil {
				return e
			}
			tx.Create(&AuditEvent{ProfileID: profileID, ActorID: actor, Action: "ASSIGNED", Dimension: dimension, Value: c, CreatedAt: now})
		} else if !prior.Active {
			if e := tx.Model(new(T)).Where("id=?", prior.ID).Updates(map[string]any{"active": true, "assigned_by": actor, "assigned_at": now, "removed_by": nil, "removed_at": nil}).Error; e != nil {
				return e
			}
			tx.Create(&AuditEvent{ProfileID: profileID, ActorID: actor, Action: "ASSIGNED", Dimension: dimension, Value: c, CreatedAt: now})
		}
	}
	for c, prior := range states {
		if prior.Active && !want[c] {
			if e := tx.Model(new(T)).Where("id=?", prior.ID).Updates(map[string]any{"active": false, "removed_by": actor, "removed_at": now}).Error; e != nil {
				return e
			}
			tx.Create(&AuditEvent{ProfileID: profileID, ActorID: actor, Action: "REMOVED", Dimension: dimension, Value: c, CreatedAt: now})
		}
	}
	return nil
}

func (s *Service) Upsert(id uint, r UpsertRequest, actor uint) (*View, error) {
	functions, e := normalize(r.Functions, ValidFunction)
	if e != nil {
		return nil, e
	}
	specialties, e := normalize(r.Specialties, ValidSpecialty)
	if e != nil {
		return nil, e
	}
	capabilities, e := normalize(r.Capabilities, ValidCapability)
	if e != nil {
		return nil, e
	}
	if r.UserID == 0 || strings.TrimSpace(r.EmployeeCode) == "" {
		return nil, coreerrors.BadRequest("Utilisateur et code agent obligatoires")
	}
	var profileID uint
	e = s.db.Transaction(func(tx *gorm.DB) error {
		var users int64
		if x := tx.Table("users").Where("id=?", r.UserID).Count(&users).Error; x != nil {
			return x
		}
		if users == 0 {
			return coreerrors.NotFound("USER")
		}
		var p Profile
		if id > 0 {
			if x := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&p, id).Error; x != nil {
				return coreerrors.NotFound("STAFF_PROFILE")
			}
			if p.UserID != r.UserID {
				return coreerrors.Conflict("Le compte utilisateur d'un profil ne peut pas être remplacé")
			}
		} else {
			p = Profile{UserID: r.UserID, Active: true}
			if x := tx.Where("user_id=?", r.UserID).First(&p).Error; x == nil {
				return coreerrors.Conflict("Un profil existe déjà pour cet utilisateur")
			} else if !errors.Is(x, gorm.ErrRecordNotFound) {
				return x
			}
		}
		oldActive := p.Active
		p.EmployeeCode = strings.ToUpper(strings.TrimSpace(r.EmployeeCode))
		p.JobTitle = strings.TrimSpace(r.JobTitle)
		p.PrimaryDepartment = strings.TrimSpace(r.PrimaryDepartment)
		if r.PrimaryServiceID != nil {
			var service struct {
				ID     uint
				Name   string
				Active bool
			}
			if x := tx.Table("organization_services").Select("id,name,active").Where("id=?", *r.PrimaryServiceID).Scan(&service).Error; x != nil {
				return x
			}
			if service.ID == 0 {
				return coreerrors.NotFound("SERVICE")
			}
			if !service.Active {
				return coreerrors.Conflict("Le service principal est inactif")
			}
			p.PrimaryServiceID = r.PrimaryServiceID
			p.PrimaryDepartment = service.Name
		}
		p.ProfessionalNumber = strings.TrimSpace(r.ProfessionalNumber)
		if r.Active != nil {
			p.Active = *r.Active
		}
		if p.ID == 0 {
			if x := tx.Create(&p).Error; x != nil {
				return x
			}
			tx.Create(&AuditEvent{ProfileID: p.ID, ActorID: actor, Action: "CREATED", Dimension: "PROFILE", Value: p.EmployeeCode, CreatedAt: time.Now()})
		} else if x := tx.Save(&p).Error; x != nil {
			return x
		}
		if oldActive != p.Active {
			action := "ACTIVATED"
			if !p.Active {
				action = "DEACTIVATED"
			}
			tx.Create(&AuditEvent{ProfileID: p.ID, ActorID: actor, Action: action, Dimension: "PROFILE", Value: p.EmployeeCode, CreatedAt: time.Now()})
			if x := tx.Table("users").Where("id=?", p.UserID).Update("is_active", p.Active).Error; x != nil {
				return x
			}
		}
		if x := syncAssignments[Function](tx, p.ID, actor, "FUNCTION", functions); x != nil {
			return x
		}
		if x := syncAssignments[Specialty](tx, p.ID, actor, "SPECIALTY", specialties); x != nil {
			return x
		}
		if x := syncAssignments[Capability](tx, p.ID, actor, "CAPABILITY", capabilities); x != nil {
			return x
		}
		if r.PrimaryServiceID != nil {
			wanted := map[uint]bool{*r.PrimaryServiceID: true}
			for _, sid := range r.SecondaryServiceIDs {
				wanted[sid] = true
			}
			var existing []struct {
				ID, ServiceID uint
				Active        bool
			}
			if x := tx.Table("staff_service_assignments").Where("profile_id=?", p.ID).Scan(&existing).Error; x != nil {
				return x
			}
			for sid := range wanted {
				var svc struct {
					ID     uint
					Active bool
				}
				tx.Table("organization_services").Select("id,active").Where("id=?", sid).Scan(&svc)
				if svc.ID == 0 || !svc.Active {
					return coreerrors.Conflict("Service secondaire invalide ou inactif")
				}
				assignment := map[string]any{"profile_id": p.ID, "service_id": sid, "is_primary": sid == *r.PrimaryServiceID, "active": true, "created_by": actor, "created_at": time.Now(), "updated_at": time.Now()}
				var count int64
				tx.Table("staff_service_assignments").Where("profile_id=? AND service_id=?", p.ID, sid).Count(&count)
				if count == 0 {
					if x := tx.Table("staff_service_assignments").Create(assignment).Error; x != nil {
						return x
					}
				} else {
					if x := tx.Table("staff_service_assignments").Where("profile_id=? AND service_id=?", p.ID, sid).Updates(map[string]any{"is_primary": sid == *r.PrimaryServiceID, "active": true, "updated_at": time.Now()}).Error; x != nil {
						return x
					}
				}
			}
			for _, a := range existing {
				if a.Active && !wanted[a.ServiceID] {
					if x := tx.Table("staff_service_assignments").Where("id=?", a.ID).Updates(map[string]any{"active": false, "is_primary": false, "updated_at": time.Now()}).Error; x != nil {
						return x
					}
				}
			}
		}
		profileID = p.ID
		if AfterProfileChangeValidate != nil {
			if x := AfterProfileChangeValidate(tx, p.UserID, p.Active, functions, specialties); x != nil {
				return x
			}
		}
		// Keep login/middleware gate aligned with staff profile activity.
		if x := tx.Table("users").Where("id=?", p.UserID).Update("is_active", p.Active).Error; x != nil {
			return x
		}
		return nil
	})
	if e != nil {
		if strings.Contains(e.Error(), "duplicate key") {
			return nil, coreerrors.Conflict("Code agent ou affectation déjà utilisé")
		}
		return nil, e
	}
	return s.Get(profileID)
}

func (s *Service) Audit(id uint) ([]AuditEvent, error) {
	if _, e := s.Get(id); e != nil {
		return nil, e
	}
	var xs []AuditEvent
	e := s.db.Where("profile_id=?", id).Order("created_at DESC,id DESC").Find(&xs).Error
	return xs, e
}
func CatalogData() Catalog {
	rows := make([]MatrixRow, 0, len(FunctionLabels))
	for c, l := range FunctionLabels {
		p := append([]string(nil), FunctionPermissions[c]...)
		sort.Strings(p)
		rows = append(rows, MatrixRow{Code: c, Label: l, Permissions: p})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Label < rows[j].Label })
	return Catalog{Functions: FunctionLabels, Specialties: SpecialtyLabels, Capabilities: CapabilityLabels, Matrix: rows}
}

func (s *Service) Users() ([]UserOption, error) {
	var out []UserOption
	e := s.db.Table("users u").Select("u.id,u.name,u.email,(sp.id IS NOT NULL) has_profile").Joins("LEFT JOIN staff_profiles sp ON sp.user_id=u.id").Where("u.role<>'admin'").Order("u.name,u.id").Scan(&out).Error
	return out, e
}
