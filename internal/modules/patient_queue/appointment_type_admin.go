package patient_queue

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/core/scheduling"
	"gorm.io/gorm"
)

func (s *Service) canManageAppointmentType(a Access) bool {
	return s.has(a, "appointment_type.manage") || s.has(a, "*")
}

func (s *Service) assertCanManageAppointmentType(a Access) error {
	if !s.canManageAppointmentType(a) {
		return coreerrors.Forbidden("Permission gestion type de rendez-vous refusée")
	}
	return nil
}

func (s *Service) validateAppointmentTypeDuration(minutes int) error {
	if minutes < scheduling.MinDurationMinutes || minutes > scheduling.MaxDurationMinutes {
		return coreerrors.BadRequest(fmt.Sprintf(
			"durée hors limites (%d–%d min)", scheduling.MinDurationMinutes, scheduling.MaxDurationMinutes,
		))
	}
	return nil
}

func (s *Service) normalizeAppointmentTypeCode(code string) (string, error) {
	out := strings.ToUpper(strings.TrimSpace(code))
	if out == "" {
		return "", coreerrors.BadRequest("code requis")
	}
	if len(out) > 64 {
		return "", coreerrors.BadRequest("code trop long")
	}
	return out, nil
}

func (s *Service) normalizeAppointmentTypeName(name string) (string, error) {
	out := strings.TrimSpace(name)
	if out == "" {
		return "", coreerrors.BadRequest("nom requis")
	}
	if len(out) > 160 {
		return "", coreerrors.BadRequest("nom trop long")
	}
	return out, nil
}

// validateAppointmentTypeServiceID requires an ACTIVE organization service when set.
func (s *Service) validateAppointmentTypeServiceID(serviceID *uint) error {
	if serviceID == nil {
		return nil
	}
	if *serviceID == 0 {
		return coreerrors.BadRequest("serviceId invalide")
	}
	return s.assertServiceExists(*serviceID)
}

func appointmentTypeConflict(err error) error {
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "unique") || strings.Contains(msg, "duplicate") {
		return coreerrors.Conflict("Code de type de rendez-vous déjà utilisé")
	}
	return coreerrors.Internal("échec persistance type de rendez-vous")
}

func (s *Service) writeAppointmentTypeAudit(tx *gorm.DB, actor uint, event string, t AppointmentType, reason, payload string) error {
	svcID := uint(0)
	if t.ServiceID != nil {
		svcID = *t.ServiceID
	}
	return s.writeScheduleAudit(tx, actor, event, EntityAppointmentType, t.ID, 0, svcID, reason, payload)
}

// CreateAppointmentType creates a scheduling catalog entry (not a clinical reason).
// Authorization: appointment_type.manage | * only (NOT queue.checkin / schedule.* / appointment.create.*).
func (s *Service) CreateAppointmentType(r CreateAppointmentTypeRequest, a Access) (*AppointmentType, error) {
	if err := s.assertCanManageAppointmentType(a); err != nil {
		return nil, err
	}
	code, err := s.normalizeAppointmentTypeCode(r.Code)
	if err != nil {
		return nil, err
	}
	name, err := s.normalizeAppointmentTypeName(r.Name)
	if err != nil {
		return nil, err
	}
	if err := s.validateAppointmentTypeDuration(r.DefaultDurationMinutes); err != nil {
		return nil, err
	}
	if err := s.validateAppointmentTypeServiceID(r.ServiceID); err != nil {
		return nil, err
	}
	active := true
	if r.Active != nil {
		active = *r.Active
	}
	now := time.Now().UTC()
	t := AppointmentType{
		Code:                   code,
		Name:                   name,
		DefaultDurationMinutes: r.DefaultDurationMinutes,
		ServiceID:              r.ServiceID,
		Active:                 active,
		CreatedAt:              now,
		UpdatedAt:              now,
	}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if e := tx.Create(&t).Error; e != nil {
			return appointmentTypeConflict(e)
		}
		payload, _ := json.Marshal(map[string]any{
			"code": t.Code, "name": t.Name, "defaultDurationMinutes": t.DefaultDurationMinutes,
			"serviceId": t.ServiceID, "active": t.Active,
		})
		return s.writeAppointmentTypeAudit(tx, a.UserID, ApptTypeAuditCreated, t, "", string(payload))
	})
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// UpdateAppointmentType patches mutable fields. Code is immutable identity.
func (s *Service) UpdateAppointmentType(id uint, r UpdateAppointmentTypeRequest, a Access) (*AppointmentType, error) {
	if err := s.assertCanManageAppointmentType(a); err != nil {
		return nil, err
	}
	if id == 0 {
		return nil, coreerrors.BadRequest("identifiant invalide")
	}
	var out *AppointmentType
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var t AppointmentType
		if e := tx.First(&t, id).Error; e != nil {
			if e == gorm.ErrRecordNotFound {
				return coreerrors.NotFound("Type de rendez-vous")
			}
			return coreerrors.Internal("échec lecture type de rendez-vous")
		}
		changed := false
		if r.Name != nil {
			name, e := s.normalizeAppointmentTypeName(*r.Name)
			if e != nil {
				return e
			}
			if name != t.Name {
				t.Name = name
				changed = true
			}
		}
		if r.DefaultDurationMinutes != nil {
			if e := s.validateAppointmentTypeDuration(*r.DefaultDurationMinutes); e != nil {
				return e
			}
			if *r.DefaultDurationMinutes != t.DefaultDurationMinutes {
				t.DefaultDurationMinutes = *r.DefaultDurationMinutes
				changed = true
			}
		}
		if r.ClearServiceID {
			if t.ServiceID != nil {
				t.ServiceID = nil
				changed = true
			}
		} else if r.ServiceID != nil {
			if e := s.validateAppointmentTypeServiceID(r.ServiceID); e != nil {
				return e
			}
			if t.ServiceID == nil || *t.ServiceID != *r.ServiceID {
				sid := *r.ServiceID
				t.ServiceID = &sid
				changed = true
			}
		}
		if r.Active != nil && *r.Active != t.Active {
			// Reactivation must not leave an ACTIVE type bound to an inactive organization service
			// when serviceId is left unchanged (LOT 23M-A integrity).
			if *r.Active && t.ServiceID != nil {
				if e := s.validateAppointmentTypeServiceID(t.ServiceID); e != nil {
					return e
				}
			}
			t.Active = *r.Active
			changed = true
		}
		if !changed {
			out = &t
			return nil
		}
		t.UpdatedAt = time.Now().UTC()
		if e := tx.Save(&t).Error; e != nil {
			return appointmentTypeConflict(e)
		}
		payload, _ := json.Marshal(map[string]any{
			"code": t.Code, "name": t.Name, "defaultDurationMinutes": t.DefaultDurationMinutes,
			"serviceId": t.ServiceID, "active": t.Active,
		})
		event := ApptTypeAuditUpdated
		if r.Active != nil && !*r.Active {
			event = ApptTypeAuditDisabled
		}
		if e := s.writeAppointmentTypeAudit(tx, a.UserID, event, t, "", string(payload)); e != nil {
			return e
		}
		out = &t
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// DisableAppointmentType soft-deactivates (active=false). Historical appointments remain readable.
func (s *Service) DisableAppointmentType(id uint, a Access) (*AppointmentType, error) {
	active := false
	return s.UpdateAppointmentType(id, UpdateAppointmentTypeRequest{Active: &active}, a)
}
