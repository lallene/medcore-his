package patient_queue

import (
	"strings"
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"gorm.io/gorm"
)

// EnsureAppointmentIndexes adds composite indexes used by schedule/availability/booking queries.
// PostgreSQL EXCLUDE on intervals is not added: legacy NULL ends + status filtering make a
// partial EXCLUDE brittle; application advisory locks remain the authority (LOT 23D).
func EnsureAppointmentIndexes(db *gorm.DB) error {
	statements := []string{
		`CREATE INDEX IF NOT EXISTS idx_pq_appt_service_scheduled ON patient_queue_appointments (service_id, scheduled_at)`,
		`CREATE INDEX IF NOT EXISTS idx_pq_appt_doctor_scheduled ON patient_queue_appointments (expected_doctor_id, scheduled_at)`,
		`CREATE INDEX IF NOT EXISTS idx_pq_appt_patient_scheduled ON patient_queue_appointments (patient_id, scheduled_at)`,
		`CREATE INDEX IF NOT EXISTS idx_pq_appt_status_scheduled ON patient_queue_appointments (status, scheduled_at)`,
		`CREATE INDEX IF NOT EXISTS idx_pq_appt_type_scheduled ON patient_queue_appointments (appointment_type_id, scheduled_at)`,
		// Replace global idempotency unique with caller-scoped unique (created_by, idempotency_key).
		`DROP INDEX IF EXISTS ux_pq_appt_idempotency`,
		`CREATE UNIQUE INDEX IF NOT EXISTS ux_pq_appt_idempotency_caller ON patient_queue_appointments (created_by, idempotency_key) WHERE idempotency_key IS NOT NULL`,
	}
	for _, sql := range statements {
		if err := db.Exec(sql).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) writeAppointmentHistory(tx *gorm.DB, appointmentID, actor uint, event, fromStatus, toStatus, reason, payload string) error {
	h := AppointmentHistory{
		AppointmentID: appointmentID,
		ActorUserID:   actor,
		EventType:     event,
		FromStatus:    fromStatus,
		ToStatus:      toStatus,
		Reason:        reason,
		Payload:       payload,
		CreatedAt:     time.Now().UTC(),
	}
	return tx.Create(&h).Error
}

// CreateAppointmentType creates a scheduling catalog entry (not a clinical reason).
func (s *Service) CreateAppointmentType(r CreateAppointmentTypeRequest, a Access) (*AppointmentType, error) {
	if !s.has(a, "queue.checkin") && !s.has(a, "organization.manage") && !s.has(a, "*") {
		return nil, coreerrors.Forbidden("Permission création type de rendez-vous refusée")
	}
	code := strings.ToUpper(strings.TrimSpace(r.Code))
	name := strings.TrimSpace(r.Name)
	if code == "" || name == "" {
		return nil, coreerrors.BadRequest("code et nom requis")
	}
	if r.DefaultDurationMinutes <= 0 {
		return nil, coreerrors.BadRequest("durée par défaut doit être > 0")
	}
	if r.ServiceID != nil {
		if err := s.assertServiceInScope(*r.ServiceID, a); err != nil {
			return nil, err
		}
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
	if err := s.db.Create(&t).Error; err != nil {
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "unique") || strings.Contains(msg, "duplicate") {
			return nil, coreerrors.Conflict("Code de type de rendez-vous déjà utilisé")
		}
		return nil, coreerrors.Internal(err.Error())
	}
	return &t, nil
}

func (s *Service) resolveAppointmentInterval(r CreateAppointmentRequest) (time.Time, *time.Time, *uint, error) {
	start := r.ScheduledAt.UTC()
	var end *time.Time
	var typeID *uint

	if r.AppointmentTypeID != nil {
		var at AppointmentType
		if err := s.db.First(&at, *r.AppointmentTypeID).Error; err != nil {
			return start, nil, nil, coreerrors.NotFound("Type de rendez-vous")
		}
		if !at.Active {
			return start, nil, nil, coreerrors.BadRequest("Type de rendez-vous inactif")
		}
		if at.DefaultDurationMinutes <= 0 {
			return start, nil, nil, coreerrors.BadRequest("durée du type invalide")
		}
		if at.ServiceID != nil && *at.ServiceID != r.ServiceID {
			return start, nil, nil, coreerrors.BadRequest("Type de rendez-vous hors service")
		}
		typeID = &at.ID
		if r.ScheduledEndAt == nil {
			e := start.Add(time.Duration(at.DefaultDurationMinutes) * time.Minute)
			end = &e
		}
	}

	if r.ScheduledEndAt != nil {
		e := r.ScheduledEndAt.UTC()
		end = &e
	}
	if end != nil && !end.After(start) {
		return start, nil, nil, coreerrors.BadRequest("scheduledEndAt doit être strictement après scheduledAt ([start, end))")
	}
	return start, end, typeID, nil
}
