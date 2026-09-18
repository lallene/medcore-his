package consultations

import (
	"errors"

	"gorm.io/gorm"
)

// Access carries the authenticated actor and trusted RBAC permissions from the handler.
// Organizational service scope is derived from staff assignments; "*" may bypass it.
// authorID on mutation APIs remains audit/traceability identity only.
type Access struct {
	UserID      uint
	Permissions map[string]bool
}

func (a Access) Has(p string) bool {
	return a.Permissions["*"] || a.Permissions[p]
}

// assignedServiceIDs returns (unrestricted, ids, err).
// unrestricted is true only for trusted "*".
// Non-"*" actors with missing staff tables or no assignments get an empty id set (fail closed).
func (s *Service) assignedServiceIDs(a Access) (unrestricted bool, ids []uint, err error) {
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

func containsServiceID(ids []uint, serviceID uint) bool {
	for _, id := range ids {
		if id == serviceID {
			return true
		}
	}
	return false
}

// assertCanAccessConsultation enforces Consultation.ServiceID membership.
// Out-of-scope and missing/zero ServiceID return ErrConsultationNotFound (anti-enumeration).
func (s *Service) assertCanAccessConsultation(c *Consultation, a Access) error {
	if a.Has("*") {
		return nil
	}
	if c.ServiceID == nil || *c.ServiceID == 0 {
		return ErrConsultationNotFound
	}
	_, ids, err := s.assignedServiceIDs(a)
	if err != nil {
		return err
	}
	if containsServiceID(ids, *c.ServiceID) {
		return nil
	}
	return ErrConsultationNotFound
}

func (s *Service) loadConsultationForAccess(id uint, a Access) (*Consultation, error) {
	consultation, err := s.repo.FindByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrConsultationNotFound
		}
		return nil, err
	}
	if err := s.assertCanAccessConsultation(consultation, a); err != nil {
		return nil, err
	}
	return consultation, nil
}
