package laboratory

import "gorm.io/gorm"

// Access carries the authenticated actor and trusted RBAC permissions from the handler.
// Organizational scope is derived from staff assignments; "*" may bypass it.
type Access struct {
	UserID      uint
	Permissions map[string]bool
}

func (a Access) Has(p string) bool {
	return a.Permissions["*"] || a.Permissions[p]
}

func (s *Service) assignedExecutingServiceIDs(a Access) (unrestricted bool, ids []uint, err error) {
	if a.Has("*") {
		return true, nil, nil
	}
	db := s.repo.db
	if !db.Migrator().HasTable("staff_profiles") || !db.Migrator().HasTable("staff_service_assignments") {
		return false, nil, nil
	}
	var assigned []uint
	if err := db.Raw(`SELECT ssa.service_id FROM staff_service_assignments ssa
		JOIN staff_profiles sp ON sp.id = ssa.profile_id
		WHERE sp.user_id = ? AND sp.active AND ssa.active`, a.UserID).Scan(&assigned).Error; err != nil {
		return false, nil, err
	}
	return false, assigned, nil
}

// assertCanAccessExecutingOrder enforces Order.ExecutingServiceID membership.
// Out-of-scope and missing execution scope return ErrRecordNotFound (anti-enumeration).
func (s *Service) assertCanAccessExecutingOrder(o *Order, a Access) error {
	if a.Has("*") {
		return nil
	}
	if o.ExecutingServiceID == nil || *o.ExecutingServiceID == 0 {
		return gorm.ErrRecordNotFound
	}
	_, ids, err := s.assignedExecutingServiceIDs(a)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if id == *o.ExecutingServiceID {
			return nil
		}
	}
	return gorm.ErrRecordNotFound
}
