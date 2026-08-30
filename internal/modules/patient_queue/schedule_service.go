package patient_queue

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"gorm.io/gorm"
)

// --- DTOs ---

type CreateWorkingScheduleRequest struct {
	PractitionerID uint       `json:"practitionerId" binding:"required"`
	ServiceID      uint       `json:"serviceId" binding:"required"`
	Weekday        int        `json:"weekday"`
	StartTime      string     `json:"startTime" binding:"required"`
	EndTime        string     `json:"endTime" binding:"required"`
	ValidFrom      time.Time  `json:"validFrom" binding:"required"`
	ValidUntil     *time.Time `json:"validUntil"`
}

type UpdateWorkingScheduleRequest struct {
	ServiceID  *uint      `json:"serviceId"`
	Weekday    *int       `json:"weekday"`
	StartTime  *string    `json:"startTime"`
	EndTime    *string    `json:"endTime"`
	ValidFrom  *time.Time `json:"validFrom"`
	ValidUntil *time.Time `json:"validUntil"`
	ClearUntil bool       `json:"clearUntil"` // set ValidUntil to null
	Active     *bool      `json:"active"`
}

type CreateScheduleExceptionRequest struct {
	PractitionerID uint      `json:"practitionerId" binding:"required"`
	ServiceID      uint      `json:"serviceId" binding:"required"`
	Type           string    `json:"type" binding:"required"`
	StartAt        time.Time `json:"startAt" binding:"required"`
	EndAt          time.Time `json:"endAt" binding:"required"`
	Reason         string    `json:"reason"`
}

type UpdateScheduleExceptionRequest struct {
	ServiceID *uint      `json:"serviceId"`
	Type      *string    `json:"type"`
	StartAt   *time.Time `json:"startAt"`
	EndAt     *time.Time `json:"endAt"`
	Reason    *string    `json:"reason"`
	Active    *bool      `json:"active"`
}

type ScheduleFilter struct {
	PractitionerID *uint
	ServiceID      *uint
	Weekday        *int
	Active         *bool
	ValidOn        *time.Time // schedules whose validity covers this date
	Page           int
	Limit          int
}

type ExceptionFilter struct {
	PractitionerID *uint
	ServiceID      *uint
	Type           *string
	Active         *bool
	From           *time.Time
	To             *time.Time
	Page           int
	Limit          int
}

// WorkingWindow is a domain projection for LOT 23C (recurring wall-clock window).
type WorkingWindow struct {
	ID             uint
	PractitionerID uint
	ServiceID      uint
	Weekday        int
	StartTime      string // HH:MM:SS local
	EndTime        string
	ValidFrom      time.Time
	ValidUntil     *time.Time
}

// --- Auth helpers (schedule-specific) ---

func (s *Service) canReadAllSchedules(a Access) bool {
	return a.Has("*") || a.Has("schedule.read.all")
}

func (s *Service) canManageAllSchedules(a Access) bool {
	return a.Has("*") || a.Has("schedule.manage.all")
}

// assignedStaffServiceIDs returns staff_service_assignments (+ JWT ServiceID).
// Never globalizes via schedule.read.all or queue.read.all (LOT 23I).
func (s *Service) assignedStaffServiceIDs(a Access) ([]uint, error) {
	ids := map[uint]bool{}
	if a.ServiceID != nil {
		ids[*a.ServiceID] = true
	}
	var assigned []uint
	s.db.Raw(`SELECT service_id FROM staff_service_assignments WHERE profile_id IN (
		SELECT id FROM staff_profiles WHERE user_id=? AND active) AND active`, a.UserID).Scan(&assigned)
	for _, id := range assigned {
		ids[id] = true
	}
	if len(ids) == 0 {
		return nil, coreerrors.Forbidden("Aucun service assigné pour les plannings")
	}
	out := make([]uint, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	return out, nil
}

// scheduleScopeServiceIDs — READ list scope. Global only for schedule.read.all / manage.all / *.
func (s *Service) scheduleScopeServiceIDs(a Access) ([]uint, error) {
	if s.canReadAllSchedules(a) || s.canManageAllSchedules(a) {
		return nil, nil
	}
	return s.assignedStaffServiceIDs(a)
}

// scheduleManageScopeServiceIDs — MUTATION scope. Global only for schedule.manage.all / *.
// schedule.read.all must NOT expand manage.service (LOT 23I RBAC-01).
func (s *Service) scheduleManageScopeServiceIDs(a Access) ([]uint, error) {
	if s.canManageAllSchedules(a) {
		return nil, nil
	}
	return s.assignedStaffServiceIDs(a)
}

func (s *Service) assertCanReadSchedule(practitionerID, serviceID uint, a Access) error {
	if s.canReadAllSchedules(a) {
		return nil
	}
	if a.Has("schedule.read.own") && practitionerID == a.UserID {
		return nil
	}
	if a.Has("schedule.read.service") || a.Has("schedule.manage.service") || a.Has("schedule.manage.all") {
		return s.assertScheduleServiceInReadScope(serviceID, a)
	}
	return coreerrors.Forbidden("Lecture planning non autorisée")
}

func (s *Service) assertCanManageSchedule(practitionerID, serviceID uint, a Access) error {
	if s.canManageAllSchedules(a) {
		return nil
	}
	if a.Has("schedule.manage.own") && practitionerID == a.UserID {
		// Own manage still requires assignment to the service
		return s.assertPractitionerAssignedToService(practitionerID, serviceID)
	}
	if a.Has("schedule.manage.service") {
		return s.assertScheduleServiceInManageScope(serviceID, a)
	}
	return coreerrors.Forbidden("Gestion planning non autorisée")
}

// assertScheduleServiceInReadScope — used for READ paths. read.all / manage.all bypass.
func (s *Service) assertScheduleServiceInReadScope(serviceID uint, a Access) error {
	if s.canReadAllSchedules(a) || s.canManageAllSchedules(a) {
		return nil
	}
	ids, err := s.assignedStaffServiceIDs(a)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if id == serviceID {
			return nil
		}
	}
	return coreerrors.NotFound("Planning")
}

// assertScheduleServiceInManageScope — used for MUTATIONS. Only manage.all / * bypass.
// schedule.read.all must not enlarge manage.service (LOT 23I RBAC-01).
func (s *Service) assertScheduleServiceInManageScope(serviceID uint, a Access) error {
	if s.canManageAllSchedules(a) {
		return nil
	}
	ids, err := s.scheduleManageScopeServiceIDs(a)
	if err != nil {
		return err
	}
	if ids == nil {
		return nil
	}
	for _, id := range ids {
		if id == serviceID {
			return nil
		}
	}
	return coreerrors.NotFound("Planning")
}

// assertScheduleServiceInScope keeps the historical name for READ callers (availability, agenda filters).
func (s *Service) assertScheduleServiceInScope(serviceID uint, a Access) error {
	return s.assertScheduleServiceInReadScope(serviceID, a)
}

func (s *Service) assertServiceExists(serviceID uint) error {
	var n int64
	if err := s.db.Raw(`SELECT COUNT(1) FROM organization_services WHERE id=?`, serviceID).Scan(&n).Error; err != nil {
		return coreerrors.Internal(err.Error())
	}
	if n == 0 {
		return coreerrors.BadRequest("Service inexistant")
	}
	return nil
}

func (s *Service) assertPractitionerIsStaff(practitionerID uint) error {
	var n int64
	if err := s.db.Raw(`SELECT COUNT(1) FROM staff_profiles WHERE user_id=? AND active`, practitionerID).Scan(&n).Error; err != nil {
		return coreerrors.Internal(err.Error())
	}
	if n == 0 {
		return coreerrors.BadRequest("Praticien invalide (profil staff actif requis)")
	}
	return nil
}

func (s *Service) assertPractitionerAssignedToService(practitionerID, serviceID uint) error {
	if err := s.assertPractitionerIsStaff(practitionerID); err != nil {
		return err
	}
	if err := s.assertServiceExists(serviceID); err != nil {
		return err
	}
	var n int64
	err := s.db.Raw(`SELECT COUNT(1) FROM staff_service_assignments ssa
		JOIN staff_profiles sp ON sp.id = ssa.profile_id
		WHERE sp.user_id=? AND sp.active AND ssa.service_id=? AND ssa.active`,
		practitionerID, serviceID).Scan(&n).Error
	if err != nil {
		return coreerrors.Internal(err.Error())
	}
	if n == 0 {
		return coreerrors.BadRequest("Praticien non affecté à ce service")
	}
	return nil
}

func (s *Service) writeScheduleAudit(tx *gorm.DB, actor uint, event, entityType string, entityID, practitionerID, serviceID uint, reason, payload string) error {
	ev := ScheduleAuditEvent{
		ActorUserID:    actor,
		EventType:      event,
		EntityType:     entityType,
		EntityID:       entityID,
		PractitionerID: practitionerID,
		ServiceID:      serviceID,
		Reason:         reason,
		Payload:        payload,
		CreatedAt:      time.Now().UTC(),
	}
	return tx.Create(&ev).Error
}

func schedulePayload(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func (s *Service) advisoryLockSchedule(tx *gorm.DB, practitionerID, serviceID uint, weekday int) error {
	// Transaction-scoped advisory lock keyed by practitioner+service+weekday.
	key1 := int64(practitionerID)
	key2 := int64(serviceID)<<8 | int64(weekday&0xFF)
	if err := tx.Exec(`SELECT pg_advisory_xact_lock(?, ?)`, key1, key2).Error; err != nil {
		return coreerrors.Internal("verrou planning: " + err.Error())
	}
	return nil
}

func (s *Service) findOverlappingSchedules(tx *gorm.DB, excludeID, practitionerID, serviceID uint, weekday int, startSecs, endSecs int, validFrom time.Time, validUntil *time.Time) ([]StaffWorkingSchedule, error) {
	var rows []StaffWorkingSchedule
	q := tx.Where("practitioner_id = ? AND service_id = ? AND weekday = ? AND active = true", practitionerID, serviceID, weekday)
	if excludeID > 0 {
		q = q.Where("id <> ?", excludeID)
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	var out []StaffWorkingSchedule
	for _, r := range rows {
		if !validityRangesOverlap(validFrom, validUntil, r.ValidFrom, r.ValidUntil) {
			continue
		}
		rs, _, e1 := ParseWallClockSeconds(r.StartTime)
		re, _, e2 := ParseWallClockSeconds(r.EndTime)
		if e1 != nil || e2 != nil {
			continue
		}
		if timeWindowsOverlapHalfOpen(startSecs, endSecs, rs, re) {
			out = append(out, r)
		}
	}
	return out, nil
}

func validateWorkingWindowFields(weekday int, start, end string, validFrom time.Time, validUntil *time.Time) (startSecs, endSecs int, startN, endN string, err error) {
	if e := validateWeekday(weekday); e != nil {
		return 0, 0, "", "", coreerrors.BadRequest(e.Error())
	}
	startSecs, startN, e := ParseWallClockSeconds(start)
	if e != nil {
		return 0, 0, "", "", coreerrors.BadRequest(e.Error())
	}
	endSecs, endN, e = ParseWallClockSeconds(end)
	if e != nil {
		return 0, 0, "", "", coreerrors.BadRequest(e.Error())
	}
	if startSecs >= endSecs {
		return 0, 0, "", "", coreerrors.BadRequest("La fenêtre horaire doit satisfaire start < end (intervalle [start,end))")
	}
	vf := dateOnlyUTC(validFrom)
	if validUntil != nil {
		vu := dateOnlyUTC(*validUntil)
		if vu.Before(vf) {
			return 0, 0, "", "", coreerrors.BadRequest("validUntil doit être >= validFrom")
		}
	}
	return startSecs, endSecs, startN, endN, nil
}

// CreateWorkingSchedule creates a recurring weekly window with overlap protection.
func (s *Service) CreateWorkingSchedule(req CreateWorkingScheduleRequest, a Access) (*StaffWorkingSchedule, error) {
	if err := s.assertCanManageSchedule(req.PractitionerID, req.ServiceID, a); err != nil {
		return nil, err
	}
	if err := s.assertPractitionerAssignedToService(req.PractitionerID, req.ServiceID); err != nil {
		return nil, err
	}
	startSecs, endSecs, startN, endN, err := validateWorkingWindowFields(req.Weekday, req.StartTime, req.EndTime, req.ValidFrom, req.ValidUntil)
	if err != nil {
		return nil, err
	}
	vf := dateOnlyUTC(req.ValidFrom)
	var vu *time.Time
	if req.ValidUntil != nil {
		x := dateOnlyUTC(*req.ValidUntil)
		vu = &x
	}

	var created *StaffWorkingSchedule
	txErr := s.db.Transaction(func(tx *gorm.DB) error {
		if err := s.advisoryLockSchedule(tx, req.PractitionerID, req.ServiceID, req.Weekday); err != nil {
			return err
		}
		overlap, err := s.findOverlappingSchedules(tx, 0, req.PractitionerID, req.ServiceID, req.Weekday, startSecs, endSecs, vf, vu)
		if err != nil {
			return coreerrors.Internal(err.Error())
		}
		if len(overlap) > 0 {
			return coreerrors.Conflict("Chevauchement de plages horaires pour ce praticien/service/jour")
		}
		now := time.Now().UTC()
		row := StaffWorkingSchedule{
			PractitionerID: req.PractitionerID,
			ServiceID:      req.ServiceID,
			Weekday:        req.Weekday,
			StartTime:      startN,
			EndTime:        endN,
			ValidFrom:      vf,
			ValidUntil:     vu,
			Active:         true,
			CreatedBy:      a.UserID,
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		if err := tx.Create(&row).Error; err != nil {
			return coreerrors.Internal(err.Error())
		}
		if err := s.writeScheduleAudit(tx, a.UserID, SchedAuditCreated, EntitySchedule, row.ID, row.PractitionerID, row.ServiceID, "", schedulePayload(row)); err != nil {
			return coreerrors.Internal(err.Error())
		}
		created = &row
		return nil
	})
	if txErr != nil {
		return nil, txErr
	}
	return created, nil
}

// UpdateWorkingSchedule loads → authorizes → validates → mutates (soft disable via active=false).
func (s *Service) UpdateWorkingSchedule(id uint, req UpdateWorkingScheduleRequest, a Access) (*StaffWorkingSchedule, error) {
	var existing StaffWorkingSchedule
	if err := s.db.First(&existing, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, coreerrors.NotFound("Planning")
		}
		return nil, coreerrors.Internal(err.Error())
	}
	if err := s.assertCanManageSchedule(existing.PractitionerID, existing.ServiceID, a); err != nil {
		return nil, err
	}
	targetService := existing.ServiceID
	if req.ServiceID != nil && *req.ServiceID != existing.ServiceID {
		targetService = *req.ServiceID
		if err := s.assertCanManageSchedule(existing.PractitionerID, targetService, a); err != nil {
			return nil, err
		}
		if err := s.assertPractitionerAssignedToService(existing.PractitionerID, targetService); err != nil {
			return nil, err
		}
	}

	weekday := existing.Weekday
	if req.Weekday != nil {
		weekday = *req.Weekday
	}
	start := existing.StartTime
	if req.StartTime != nil {
		start = *req.StartTime
	}
	end := existing.EndTime
	if req.EndTime != nil {
		end = *req.EndTime
	}
	vf := existing.ValidFrom
	if req.ValidFrom != nil {
		vf = *req.ValidFrom
	}
	vu := existing.ValidUntil
	if req.ClearUntil {
		vu = nil
	} else if req.ValidUntil != nil {
		vu = req.ValidUntil
	}

	startSecs, endSecs, startN, endN, err := validateWorkingWindowFields(weekday, start, end, vf, vu)
	if err != nil {
		return nil, err
	}
	vf = dateOnlyUTC(vf)
	if vu != nil {
		x := dateOnlyUTC(*vu)
		vu = &x
	}
	active := existing.Active
	if req.Active != nil {
		active = *req.Active
	}

	var updated *StaffWorkingSchedule
	txErr := s.db.Transaction(func(tx *gorm.DB) error {
		if err := s.advisoryLockSchedule(tx, existing.PractitionerID, targetService, weekday); err != nil {
			return err
		}
		// Also lock old key if weekday/service moved
		if targetService != existing.ServiceID || weekday != existing.Weekday {
			if err := s.advisoryLockSchedule(tx, existing.PractitionerID, existing.ServiceID, existing.Weekday); err != nil {
				return err
			}
		}
		if active {
			overlap, err := s.findOverlappingSchedules(tx, existing.ID, existing.PractitionerID, targetService, weekday, startSecs, endSecs, vf, vu)
			if err != nil {
				return coreerrors.Internal(err.Error())
			}
			if len(overlap) > 0 {
				return coreerrors.Conflict("Chevauchement de plages horaires pour ce praticien/service/jour")
			}
		}
		existing.ServiceID = targetService
		existing.Weekday = weekday
		existing.StartTime = startN
		existing.EndTime = endN
		existing.ValidFrom = vf
		existing.ValidUntil = vu
		existing.Active = active
		existing.UpdatedAt = time.Now().UTC()
		if err := tx.Save(&existing).Error; err != nil {
			return coreerrors.Internal(err.Error())
		}
		ev := SchedAuditUpdated
		if !active {
			ev = SchedAuditDisabled
		}
		if err := s.writeScheduleAudit(tx, a.UserID, ev, EntitySchedule, existing.ID, existing.PractitionerID, existing.ServiceID, "", schedulePayload(existing)); err != nil {
			return coreerrors.Internal(err.Error())
		}
		updated = &existing
		return nil
	})
	if txErr != nil {
		return nil, txErr
	}
	return updated, nil
}

// DisableWorkingSchedule soft-disables a schedule (DELETE semantics).
func (s *Service) DisableWorkingSchedule(id uint, a Access) (*StaffWorkingSchedule, error) {
	active := false
	return s.UpdateWorkingSchedule(id, UpdateWorkingScheduleRequest{Active: &active}, a)
}

func (s *Service) GetWorkingSchedule(id uint, a Access) (*StaffWorkingSchedule, error) {
	var row StaffWorkingSchedule
	if err := s.db.First(&row, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, coreerrors.NotFound("Planning")
		}
		return nil, coreerrors.Internal(err.Error())
	}
	if err := s.assertCanReadSchedule(row.PractitionerID, row.ServiceID, a); err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *Service) ListWorkingSchedules(f ScheduleFilter, a Access) ([]StaffWorkingSchedule, int64, error) {
	q := s.db.Model(&StaffWorkingSchedule{})
	if s.canReadAllSchedules(a) {
		// unrestricted
	} else if a.Has("schedule.read.own") && !a.Has("schedule.read.service") && !a.Has("schedule.manage.service") {
		q = q.Where("practitioner_id = ?", a.UserID)
	} else {
		ids, err := s.scheduleScopeServiceIDs(a)
		if err != nil {
			return nil, 0, err
		}
		if ids != nil {
			if a.Has("schedule.read.own") {
				q = q.Where("service_id IN ? OR practitioner_id = ?", ids, a.UserID)
			} else {
				q = q.Where("service_id IN ?", ids)
			}
		}
	}
	if f.PractitionerID != nil {
		q = q.Where("practitioner_id = ?", *f.PractitionerID)
	}
	if f.ServiceID != nil {
		q = q.Where("service_id = ?", *f.ServiceID)
	}
	if f.Weekday != nil {
		q = q.Where("weekday = ?", *f.Weekday)
	}
	if f.Active != nil {
		q = q.Where("active = ?", *f.Active)
	}
	if f.ValidOn != nil {
		d := dateOnlyUTC(*f.ValidOn)
		q = q.Where("valid_from <= ? AND (valid_until IS NULL OR valid_until >= ?)", d, d)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, coreerrors.Internal(err.Error())
	}
	page, limit := f.Page, f.Limit
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 50
	}
	var rows []StaffWorkingSchedule
	if err := q.Order("practitioner_id, service_id, weekday, start_time").
		Offset((page - 1) * limit).Limit(limit).Find(&rows).Error; err != nil {
		return nil, 0, coreerrors.Internal(err.Error())
	}
	// Filter IDOR: drop rows outside read scope (defense in depth for own+service mix)
	out := make([]StaffWorkingSchedule, 0, len(rows))
	for _, r := range rows {
		if err := s.assertCanReadSchedule(r.PractitionerID, r.ServiceID, a); err == nil {
			out = append(out, r)
		}
	}
	return out, total, nil
}

// ListMyWorkingSchedules returns schedules for the authenticated user (JWT identity).
func (s *Service) ListMyWorkingSchedules(a Access, activeOnly bool) ([]StaffWorkingSchedule, error) {
	if !a.Has("schedule.read.own") && !s.canReadAllSchedules(a) && !a.Has("schedule.manage.own") {
		return nil, coreerrors.Forbidden("Lecture de mon planning non autorisée")
	}
	q := s.db.Where("practitioner_id = ?", a.UserID)
	if activeOnly {
		q = q.Where("active = true")
	}
	var rows []StaffWorkingSchedule
	if err := q.Order("service_id, weekday, start_time").Find(&rows).Error; err != nil {
		return nil, coreerrors.Internal(err.Error())
	}
	return rows, nil
}

// --- Exceptions ---

func (s *Service) findOverlappingExceptions(tx *gorm.DB, excludeID, practitionerID, serviceID uint, start, end time.Time, samePolarityOnly bool, positive bool) ([]ScheduleException, error) {
	var rows []ScheduleException
	q := tx.Where("practitioner_id = ? AND service_id = ? AND active = true AND cancelled_at IS NULL AND start_at < ? AND end_at > ?",
		practitionerID, serviceID, end, start)
	if excludeID > 0 {
		q = q.Where("id <> ?", excludeID)
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	if !samePolarityOnly {
		return rows, nil
	}
	var out []ScheduleException
	for _, r := range rows {
		if IsPositiveException(r.Type) == positive {
			out = append(out, r)
		}
	}
	return out, nil
}

func (s *Service) CreateScheduleException(req CreateScheduleExceptionRequest, a Access) (*ScheduleException, error) {
	if err := s.assertCanManageSchedule(req.PractitionerID, req.ServiceID, a); err != nil {
		return nil, err
	}
	if err := s.assertPractitionerAssignedToService(req.PractitionerID, req.ServiceID); err != nil {
		return nil, err
	}
	typ := strings.ToUpper(strings.TrimSpace(req.Type))
	if !validExceptionType(typ) {
		return nil, coreerrors.BadRequest("Type d'exception invalide")
	}
	start := req.StartAt.UTC()
	end := req.EndAt.UTC()
	if !end.After(start) {
		return nil, coreerrors.BadRequest("L'exception doit satisfaire end > start")
	}

	var created *ScheduleException
	txErr := s.db.Transaction(func(tx *gorm.DB) error {
		if err := s.advisoryLockSchedule(tx, req.PractitionerID, req.ServiceID, 255); err != nil {
			return err
		}
		// Reject same-polarity overlaps; cross-polarity allowed (negative wins in 23C).
		overlap, err := s.findOverlappingExceptions(tx, 0, req.PractitionerID, req.ServiceID, start, end, true, IsPositiveException(typ))
		if err != nil {
			return coreerrors.Internal(err.Error())
		}
		if len(overlap) > 0 {
			return coreerrors.Conflict("Chevauchement d'exceptions de même polarité")
		}
		now := time.Now().UTC()
		row := ScheduleException{
			PractitionerID: req.PractitionerID,
			ServiceID:      req.ServiceID,
			Type:           typ,
			StartAt:        start,
			EndAt:          end,
			Reason:         req.Reason,
			Active:         true,
			CreatedBy:      a.UserID,
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		if err := tx.Create(&row).Error; err != nil {
			return coreerrors.Internal(err.Error())
		}
		if err := s.writeScheduleAudit(tx, a.UserID, ExAuditCreated, EntityException, row.ID, row.PractitionerID, row.ServiceID, req.Reason, schedulePayload(row)); err != nil {
			return coreerrors.Internal(err.Error())
		}
		created = &row
		return nil
	})
	if txErr != nil {
		return nil, txErr
	}
	return created, nil
}

func (s *Service) UpdateScheduleException(id uint, req UpdateScheduleExceptionRequest, a Access) (*ScheduleException, error) {
	var existing ScheduleException
	if err := s.db.First(&existing, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, coreerrors.NotFound("Exception")
		}
		return nil, coreerrors.Internal(err.Error())
	}
	if err := s.assertCanManageSchedule(existing.PractitionerID, existing.ServiceID, a); err != nil {
		return nil, err
	}
	targetService := existing.ServiceID
	if req.ServiceID != nil && *req.ServiceID != existing.ServiceID {
		targetService = *req.ServiceID
		if err := s.assertCanManageSchedule(existing.PractitionerID, targetService, a); err != nil {
			return nil, err
		}
		if err := s.assertPractitionerAssignedToService(existing.PractitionerID, targetService); err != nil {
			return nil, err
		}
	}
	typ := existing.Type
	if req.Type != nil {
		typ = strings.ToUpper(strings.TrimSpace(*req.Type))
		if !validExceptionType(typ) {
			return nil, coreerrors.BadRequest("Type d'exception invalide")
		}
	}
	start := existing.StartAt
	if req.StartAt != nil {
		start = req.StartAt.UTC()
	}
	end := existing.EndAt
	if req.EndAt != nil {
		end = req.EndAt.UTC()
	}
	if !end.After(start) {
		return nil, coreerrors.BadRequest("L'exception doit satisfaire end > start")
	}
	reason := existing.Reason
	if req.Reason != nil {
		reason = *req.Reason
	}
	active := existing.Active
	cancel := existing.CancelledAt
	if req.Active != nil {
		active = *req.Active
		if !active && cancel == nil {
			now := time.Now().UTC()
			cancel = &now
		}
		if active {
			cancel = nil
		}
	}

	var updated *ScheduleException
	txErr := s.db.Transaction(func(tx *gorm.DB) error {
		if err := s.advisoryLockSchedule(tx, existing.PractitionerID, targetService, 255); err != nil {
			return err
		}
		if active {
			overlap, err := s.findOverlappingExceptions(tx, existing.ID, existing.PractitionerID, targetService, start, end, true, IsPositiveException(typ))
			if err != nil {
				return coreerrors.Internal(err.Error())
			}
			if len(overlap) > 0 {
				return coreerrors.Conflict("Chevauchement d'exceptions de même polarité")
			}
		}
		existing.ServiceID = targetService
		existing.Type = typ
		existing.StartAt = start
		existing.EndAt = end
		existing.Reason = reason
		existing.Active = active
		existing.CancelledAt = cancel
		existing.UpdatedAt = time.Now().UTC()
		if err := tx.Save(&existing).Error; err != nil {
			return coreerrors.Internal(err.Error())
		}
		ev := ExAuditUpdated
		if !active {
			ev = ExAuditCancelled
		}
		if err := s.writeScheduleAudit(tx, a.UserID, ev, EntityException, existing.ID, existing.PractitionerID, existing.ServiceID, reason, schedulePayload(existing)); err != nil {
			return coreerrors.Internal(err.Error())
		}
		updated = &existing
		return nil
	})
	if txErr != nil {
		return nil, txErr
	}
	return updated, nil
}

func (s *Service) CancelScheduleException(id uint, a Access) (*ScheduleException, error) {
	active := false
	return s.UpdateScheduleException(id, UpdateScheduleExceptionRequest{Active: &active}, a)
}

func (s *Service) GetScheduleException(id uint, a Access) (*ScheduleException, error) {
	var row ScheduleException
	if err := s.db.First(&row, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, coreerrors.NotFound("Exception")
		}
		return nil, coreerrors.Internal(err.Error())
	}
	if err := s.assertCanReadSchedule(row.PractitionerID, row.ServiceID, a); err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *Service) ListScheduleExceptions(f ExceptionFilter, a Access) ([]ScheduleException, int64, error) {
	q := s.db.Model(&ScheduleException{})
	if s.canReadAllSchedules(a) {
		// ok
	} else if a.Has("schedule.read.own") && !a.Has("schedule.read.service") && !a.Has("schedule.manage.service") {
		q = q.Where("practitioner_id = ?", a.UserID)
	} else {
		ids, err := s.scheduleScopeServiceIDs(a)
		if err != nil {
			return nil, 0, err
		}
		if ids != nil {
			if a.Has("schedule.read.own") {
				q = q.Where("service_id IN ? OR practitioner_id = ?", ids, a.UserID)
			} else {
				q = q.Where("service_id IN ?", ids)
			}
		}
	}
	if f.PractitionerID != nil {
		q = q.Where("practitioner_id = ?", *f.PractitionerID)
	}
	if f.ServiceID != nil {
		q = q.Where("service_id = ?", *f.ServiceID)
	}
	if f.Type != nil {
		q = q.Where("type = ?", *f.Type)
	}
	if f.Active != nil {
		q = q.Where("active = ?", *f.Active)
	}
	if f.From != nil {
		q = q.Where("end_at > ?", f.From.UTC())
	}
	if f.To != nil {
		q = q.Where("start_at < ?", f.To.UTC())
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, coreerrors.Internal(err.Error())
	}
	page, limit := f.Page, f.Limit
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 50
	}
	var rows []ScheduleException
	if err := q.Order("start_at").Offset((page - 1) * limit).Limit(limit).Find(&rows).Error; err != nil {
		return nil, 0, coreerrors.Internal(err.Error())
	}
	out := make([]ScheduleException, 0, len(rows))
	for _, r := range rows {
		if err := s.assertCanReadSchedule(r.PractitionerID, r.ServiceID, a); err == nil {
			out = append(out, r)
		}
	}
	return out, total, nil
}

// --- LOT 23C domain contract (no slot calculation) ---

// ListApplicableWorkingWindows returns active recurring windows for practitioner+service
// whose validity intersects [fromDate, toDate] (inclusive calendar dates in UTC date storage).
func (s *Service) ListApplicableWorkingWindows(practitionerID, serviceID uint, fromDate, toDate time.Time) ([]WorkingWindow, error) {
	from := dateOnlyUTC(fromDate)
	to := dateOnlyUTC(toDate)
	if to.Before(from) {
		return nil, fmt.Errorf("toDate before fromDate")
	}
	var rows []StaffWorkingSchedule
	err := s.db.Where("practitioner_id = ? AND service_id = ? AND active = true", practitionerID, serviceID).
		Where("valid_from <= ? AND (valid_until IS NULL OR valid_until >= ?)", to, from).
		Order("weekday, start_time").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]WorkingWindow, 0, len(rows))
	for _, r := range rows {
		out = append(out, WorkingWindow{
			ID: r.ID, PractitionerID: r.PractitionerID, ServiceID: r.ServiceID,
			Weekday: r.Weekday, StartTime: r.StartTime, EndTime: r.EndTime,
			ValidFrom: r.ValidFrom, ValidUntil: r.ValidUntil,
		})
	}
	return out, nil
}

// ListApplicableExceptions returns active exceptions overlapping [from, to) for practitioner+service.
func (s *Service) ListApplicableExceptions(practitionerID, serviceID uint, from, to time.Time) ([]ScheduleException, error) {
	var rows []ScheduleException
	err := s.db.Where("practitioner_id = ? AND service_id = ? AND active = true AND cancelled_at IS NULL", practitionerID, serviceID).
		Where("start_at < ? AND end_at > ?", to.UTC(), from.UTC()).
		Order("start_at").
		Find(&rows).Error
	return rows, err
}

// ExceptionPrecedenceNegativeWins documents 23C contract: when positive and negative overlap, negative wins.
func ExceptionPrecedenceNegativeWins() bool { return true }
