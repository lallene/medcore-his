package patient_queue

import (
	"errors"
	"strings"
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"gorm.io/gorm"
)

// canInspectNotificationAdmin — LOT 23N-C1 gate (route + service). schedule.read.* alone is insufficient.
func (s *Service) canInspectNotificationAdmin(a Access) bool {
	return a.Has("*") || a.Has("schedule.manage.all") || a.Has("schedule.manage.service")
}

// notificationAdminScopeServiceIDs — nil means unrestricted (manage.all / *).
// manage.service → assigned staff services only. Never expands via schedule.read.*.
func (s *Service) notificationAdminScopeServiceIDs(a Access) ([]uint, error) {
	if !s.canInspectNotificationAdmin(a) {
		return nil, coreerrors.Forbidden("Consultation des notifications non autorisée")
	}
	if s.canManageAllSchedules(a) {
		return nil, nil
	}
	return s.assignedStaffServiceIDs(a)
}

func (s *Service) assertNotificationAdminAuthorized(a Access) error {
	if !s.canInspectNotificationAdmin(a) {
		return coreerrors.Forbidden("Consultation des notifications non autorisée")
	}
	return nil
}

func validateNotificationAdminListFilter(f *NotificationAdminListFilter) error {
	if f.Status != "" {
		if err := ValidateNotificationStatus(f.Status); err != nil {
			return err
		}
	}
	if f.Kind != "" {
		if err := ValidateNotificationKind(f.Kind); err != nil {
			return err
		}
	}
	if f.Channel != "" {
		if err := ValidateNotificationChannel(f.Channel); err != nil {
			return err
		}
	}
	if f.AppointmentID != nil && *f.AppointmentID == 0 {
		return coreerrors.BadRequest("appointmentId invalide")
	}
	if f.SendAfterFrom != nil && f.SendAfterTo != nil && f.SendAfterFrom.After(*f.SendAfterTo) {
		return coreerrors.BadRequest("sendAfterFrom doit être ≤ sendAfterTo")
	}
	if f.CreatedAtFrom != nil && f.CreatedAtTo != nil && f.CreatedAtFrom.After(*f.CreatedAtTo) {
		return coreerrors.BadRequest("createdAtFrom doit être ≤ createdAtTo")
	}
	// Pagination defaults belong on the HTTP parser; service rejects invalid explicit values.
	if f.Page < 1 {
		return coreerrors.BadRequest("page doit être ≥ 1")
	}
	if f.Limit < 1 {
		return coreerrors.BadRequest("limit doit être ≥ 1")
	}
	if f.Limit > 100 {
		return coreerrors.BadRequest("limit doit être ≤ 100")
	}
	return nil
}

// notificationAdminScopedIntents joins appointments for service isolation.
// scopeIDs nil = unrestricted (manage.all / *); non-nil = service IN list.
func (s *Service) notificationAdminScopedIntents(scopeIDs []uint) *gorm.DB {
	q := s.db.Table("appointment_notification_intents AS i").
		Joins("INNER JOIN patient_queue_appointments AS a ON a.id = i.appointment_id")
	if scopeIDs != nil {
		q = q.Where("a.service_id IN ?", scopeIDs)
	}
	return q
}

func (s *Service) notificationAdminSelectQuery(scopeIDs []uint) *gorm.DB {
	return s.notificationAdminScopedIntents(scopeIDs).
		Select(`i.id, i.appointment_id, i.patient_id, i.kind, i.channel, i.occurrence_key,
			i.send_after, i.status, i.payload_json, i.created_at, i.updated_at, i.cancelled_at, i.sent_at,
			i.processing_started_at,
			(SELECT COUNT(1)::int FROM appointment_notification_attempts att WHERE att.intent_id = i.id) AS attempt_count`)
}

func applyNotificationAdminFilters(q *gorm.DB, f NotificationAdminListFilter) *gorm.DB {
	if f.Status != "" {
		q = q.Where("i.status = ?", f.Status)
	}
	if f.Kind != "" {
		q = q.Where("i.kind = ?", f.Kind)
	}
	if f.Channel != "" {
		q = q.Where("i.channel = ?", f.Channel)
	}
	if f.AppointmentID != nil {
		q = q.Where("i.appointment_id = ?", *f.AppointmentID)
	}
	if f.SendAfterFrom != nil {
		q = q.Where("i.send_after >= ?", f.SendAfterFrom.UTC())
	}
	if f.SendAfterTo != nil {
		q = q.Where("i.send_after <= ?", f.SendAfterTo.UTC())
	}
	if f.CreatedAtFrom != nil {
		q = q.Where("i.created_at >= ?", f.CreatedAtFrom.UTC())
	}
	if f.CreatedAtTo != nil {
		q = q.Where("i.created_at <= ?", f.CreatedAtTo.UTC())
	}
	return q
}

type notificationAdminScanRow struct {
	ID                  uint       `gorm:"column:id"`
	AppointmentID       uint       `gorm:"column:appointment_id"`
	PatientID           uint       `gorm:"column:patient_id"`
	Kind                string     `gorm:"column:kind"`
	Channel             string     `gorm:"column:channel"`
	OccurrenceKey       string     `gorm:"column:occurrence_key"`
	SendAfter           time.Time  `gorm:"column:send_after"`
	Status              string     `gorm:"column:status"`
	PayloadJSON         string     `gorm:"column:payload_json"`
	CreatedAt           time.Time  `gorm:"column:created_at"`
	UpdatedAt           time.Time  `gorm:"column:updated_at"`
	CancelledAt         *time.Time `gorm:"column:cancelled_at"`
	SentAt              *time.Time `gorm:"column:sent_at"`
	ProcessingStartedAt *time.Time `gorm:"column:processing_started_at"`
	AttemptCount        int        `gorm:"column:attempt_count"`
}

// toNotificationPayloadAdminDTO copies allow-listed fields only — never re-emits raw JSON keys.
func toNotificationPayloadAdminDTO(raw string, appointmentID uint) NotificationPayloadAdminDTO {
	p, err := ParseNotificationPayload(raw)
	if err != nil {
		// Corrupt / unsafe stored JSON must not leak; expose appointmentId from the intent row only.
		return NotificationPayloadAdminDTO{AppointmentID: appointmentID}
	}
	return NotificationPayloadAdminDTO{
		AppointmentID:       p.AppointmentID,
		ScheduledAt:         p.ScheduledAt,
		AppointmentTypeName: p.AppointmentTypeName,
		ServiceName:         p.ServiceName,
		ClinicLabel:         p.ClinicLabel,
	}
}

func toNotificationIntentAdminDTO(row notificationAdminScanRow) NotificationIntentAdminDTO {
	return NotificationIntentAdminDTO{
		ID:                  row.ID,
		AppointmentID:       row.AppointmentID,
		PatientID:           row.PatientID,
		Kind:                row.Kind,
		Channel:             row.Channel,
		Status:              row.Status,
		OccurrenceKey:       row.OccurrenceKey,
		SendAfter:           row.SendAfter,
		ProcessingStartedAt: row.ProcessingStartedAt,
		SentAt:              row.SentAt,
		CancelledAt:         row.CancelledAt,
		CreatedAt:           row.CreatedAt,
		UpdatedAt:           row.UpdatedAt,
		AttemptCount:        row.AttemptCount,
		Payload:             toNotificationPayloadAdminDTO(row.PayloadJSON, row.AppointmentID),
	}
}

// ListNotificationIntentsAdmin — paginated, filtered, service-scoped intent list (LOT 23N-C1).
func (s *Service) ListNotificationIntentsAdmin(f NotificationAdminListFilter, a Access) (*NotificationIntentAdminListResponse, error) {
	if err := s.assertNotificationAdminAuthorized(a); err != nil {
		return nil, err
	}
	if err := validateNotificationAdminListFilter(&f); err != nil {
		return nil, err
	}
	scopeIDs, err := s.notificationAdminScopeServiceIDs(a)
	if err != nil {
		return nil, err
	}

	countQ := applyNotificationAdminFilters(s.notificationAdminScopedIntents(scopeIDs), f)
	var total int64
	if err := countQ.Count(&total).Error; err != nil {
		return nil, coreerrors.Internal("échec comptage intents notification")
	}

	listQ := applyNotificationAdminFilters(s.notificationAdminSelectQuery(scopeIDs), f).
		Order("i.created_at DESC, i.id DESC").
		Offset((f.Page - 1) * f.Limit).
		Limit(f.Limit)

	var rows []notificationAdminScanRow
	if err := listQ.Scan(&rows).Error; err != nil {
		return nil, coreerrors.Internal("échec liste intents notification")
	}

	items := make([]NotificationIntentAdminDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, toNotificationIntentAdminDTO(row))
	}
	return &NotificationIntentAdminListResponse{
		Items: items,
		Total: total,
		Page:  f.Page,
		Limit: f.Limit,
	}, nil
}

// GetNotificationIntentAdmin — detail with service isolation (out-of-scope → NotFound).
func (s *Service) GetNotificationIntentAdmin(id uint, a Access) (*NotificationIntentAdminDTO, error) {
	if err := s.assertNotificationAdminAuthorized(a); err != nil {
		return nil, err
	}
	if id == 0 {
		return nil, coreerrors.BadRequest("Identifiant invalide")
	}
	scopeIDs, err := s.notificationAdminScopeServiceIDs(a)
	if err != nil {
		return nil, err
	}

	var row notificationAdminScanRow
	err = s.notificationAdminSelectQuery(scopeIDs).Where("i.id = ?", id).Take(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, coreerrors.NotFound("Notification")
		}
		return nil, coreerrors.Internal("échec lecture intent notification")
	}
	dto := toNotificationIntentAdminDTO(row)
	return &dto, nil
}

// ListNotificationAttemptsAdmin — attempts for an in-scope intent, ordered by attempt_no ASC.
func (s *Service) ListNotificationAttemptsAdmin(intentID uint, a Access) ([]NotificationAttemptAdminDTO, error) {
	if _, err := s.GetNotificationIntentAdmin(intentID, a); err != nil {
		return nil, err
	}

	var attempts []AppointmentNotificationAttempt
	if err := s.db.Where("intent_id = ?", intentID).
		Order("attempt_no ASC, id ASC").
		Find(&attempts).Error; err != nil {
		return nil, coreerrors.Internal("échec liste tentatives notification")
	}

	out := make([]NotificationAttemptAdminDTO, 0, len(attempts))
	for _, att := range attempts {
		out = append(out, NotificationAttemptAdminDTO{
			ID:                att.ID,
			IntentID:          att.IntentID,
			AttemptNo:         att.AttemptNo,
			Provider:          att.Provider,
			ProviderMessageID: att.ProviderMessageID,
			Error:             att.Error,
			CreatedAt:         att.CreatedAt,
		})
	}
	return out, nil
}

// trimOptionalQuery is a tiny helper for handlers (empty → omit).
func trimOptionalQuery(raw string) string {
	return strings.TrimSpace(raw)
}
