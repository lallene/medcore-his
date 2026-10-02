package authorization

import (
	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
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

// assertCanAccessAuthorization enforces InsuranceAuthorization.ServiceID when present.
//
// NULL/zero ServiceID is SERVICE_AUTHORITY_UNRESOLVED in product terms (reference had no
// resolvable organizational service). Do not invent deny/allow: preserve permission-only access.
// Non-null ServiceID requires staff assignment membership (404 anti-enumeration).
//
// Callers that run inside a DB transaction must resolve assignedServiceIDs BEFORE opening
// the TX (authorizationDB tests use MaxOpenConns(1); HasTable/Raw on s.db inside TX deadlocks).
func (s *Service) assertCanAccessAuthorization(item *InsuranceAuthorization, a Access) error {
	if a.Has("*") {
		return nil
	}
	if item == nil || item.ServiceID == nil || *item.ServiceID == 0 {
		return nil
	}
	_, ids, err := s.assignedServiceIDs(a)
	if err != nil {
		return err
	}
	return assertServiceMembership(item, false, ids)
}

func assertServiceMembership(item *InsuranceAuthorization, unrestricted bool, ids []uint) error {
	if unrestricted {
		return nil
	}
	if item == nil || item.ServiceID == nil || *item.ServiceID == 0 {
		return nil
	}
	if containsServiceID(ids, *item.ServiceID) {
		return nil
	}
	return coreerrors.NotFound("INSURANCE_AUTHORIZATION")
}
