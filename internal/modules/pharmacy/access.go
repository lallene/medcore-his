package pharmacy

import (
	"errors"

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

// prescriptionConsultationServiceID returns consultations.service_id for the prescription's consultation.
// ok is false when the prescription/consultation join yields no row.
func (s *Service) prescriptionConsultationServiceID(prescriptionID uint) (serviceID *uint, ok bool, err error) {
	var row struct {
		ServiceID *uint
		Found     int
	}
	err = s.repo.db.Raw(`
		SELECT c.service_id AS service_id, 1 AS found
		FROM consultation_prescriptions cp
		JOIN consultations c ON c.id = cp.consultation_id
		WHERE cp.id = ?
	`, prescriptionID).Scan(&row).Error
	if err != nil {
		return nil, false, err
	}
	if row.Found == 0 {
		return nil, false, nil
	}
	return row.ServiceID, true, nil
}

// assertCanAccessPrescriptionService enforces consultation.ServiceID membership for a prescription.
// Out-of-scope, missing prescription, and missing/zero ServiceID return ErrPrescriptionNotFound (anti-enumeration).
func (s *Service) assertCanAccessPrescriptionService(prescriptionID uint, a Access) error {
	if a.Has("*") {
		return nil
	}
	serviceID, ok, err := s.prescriptionConsultationServiceID(prescriptionID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrPrescriptionNotFound
		}
		return err
	}
	if !ok || serviceID == nil || *serviceID == 0 {
		return ErrPrescriptionNotFound
	}
	_, ids, err := s.assignedServiceIDs(a)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if id == *serviceID {
			return nil
		}
	}
	return ErrPrescriptionNotFound
}
