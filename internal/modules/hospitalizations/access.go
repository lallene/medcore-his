package hospitalizations

import (
	"errors"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"gorm.io/gorm"
)

// Access carries the authenticated actor and trusted RBAC permissions from the handler.
// Organizational service scope is derived from staff assignments; "*" may bypass it.
type Access struct {
	UserID      uint
	Permissions map[string]bool
}

func (a Access) Has(p string) bool {
	return a.Permissions["*"] || a.Permissions[p]
}

// UnrestrictedAccess is for trusted internal callers / tests (permission "*").
func UnrestrictedAccess(userID uint) Access {
	return Access{UserID: userID, Permissions: map[string]bool{"*": true}}
}

func containsServiceID(ids []uint, serviceID uint) bool {
	for _, id := range ids {
		if id == serviceID {
			return true
		}
	}
	return false
}

// assignedServiceIDs returns (unrestricted, ids, err).
// unrestricted is true only for trusted "*".
// Non-"*" actors with missing staff tables or no assignments get an empty id set (fail closed).
func (s *Service) assignedServiceIDs(a Access) (unrestricted bool, ids []uint, err error) {
	if a.Has("*") {
		return true, nil, nil
	}
	db := s.db
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

// assertCanAccessHospitalization enforces Hospitalization.ServiceID membership.
// Out-of-scope and missing/zero ServiceID return NotFound (anti-enumeration), matching consultations.
func (s *Service) assertCanAccessHospitalization(h *Hospitalization, a Access) error {
	if a.Has("*") {
		return nil
	}
	unrestricted, ids, err := s.assignedServiceIDs(a)
	if err != nil {
		return err
	}
	return assertHospServiceMembership(h, unrestricted, ids)
}

func assertHospServiceMembership(h *Hospitalization, unrestricted bool, ids []uint) error {
	if unrestricted {
		return nil
	}
	if h == nil || h.ServiceID == nil || *h.ServiceID == 0 {
		return coreerrors.NotFound("HOSPITALIZATION")
	}
	if containsServiceID(ids, *h.ServiceID) {
		return nil
	}
	return coreerrors.NotFound("HOSPITALIZATION")
}

func (s *Service) loadHospitalizationForAccess(id uint, a Access) (*Hospitalization, error) {
	item, err := s.repo.FindByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, coreerrors.NotFound("HOSPITALIZATION")
		}
		return nil, err
	}
	if err := s.assertCanAccessHospitalization(item, a); err != nil {
		return nil, err
	}
	return item, nil
}
