package patient_queue

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Lifecycle advisory-lock namespace (LOT 23E). Distinct from booking idempotency (230403).
const bookingLockNSLifecycle = 230404

// RescheduleAppointmentRequest — omitted practitionerId keeps current practitioner.
// expectedScheduledAt / expectedScheduledEndAt are required concurrency preconditions (stale → 409).
type RescheduleAppointmentRequest struct {
	StartAt                time.Time `json:"startAt" binding:"required"`
	ExpectedScheduledAt    time.Time `json:"expectedScheduledAt" binding:"required"`
	ExpectedScheduledEndAt time.Time `json:"expectedScheduledEndAt" binding:"required"`
	PractitionerID         *uint     `json:"practitionerId"`
	AppointmentTypeID      *uint     `json:"appointmentTypeId"`
	DurationMinutes        *int      `json:"durationMinutes"`
	Reason                 string    `json:"reason"`
	IdempotencyKey         string    `json:"idempotencyKey"`
}

type CancelAppointmentRequest struct {
	Reason         string `json:"reason"`
	IdempotencyKey string `json:"idempotencyKey"`
}

type NoShowAppointmentRequest struct {
	Reason         string `json:"reason"`
	IdempotencyKey string `json:"idempotencyKey"`
}

type rescheduleSnapshot struct {
	PractitionerID    *uint  `json:"practitionerId,omitempty"`
	StartAt           string `json:"startAt"`
	EndAt             string `json:"endAt,omitempty"`
	AppointmentTypeID *uint  `json:"appointmentTypeId,omitempty"`
}

type lifecycleHistoryPayload struct {
	IdempotencyKey string              `json:"idempotencyKey,omitempty"`
	Old            *rescheduleSnapshot `json:"old,omitempty"`
	New            *rescheduleSnapshot `json:"new,omitempty"`
}

func (s *Service) canRescheduleAppointments(a Access) bool {
	return a.Has("*") ||
		a.Has("appointment.reschedule.all") || a.Has("appointment.reschedule.service") ||
		a.Has("schedule.manage.all") || a.Has("schedule.manage.service")
}

func (s *Service) canCancelAppointments(a Access) bool {
	return a.Has("*") || a.Has("appointment.cancel.all") || a.Has("appointment.cancel.service")
}

func (s *Service) canMarkNoShow(a Access) bool {
	return a.Has("*") || a.Has("appointment.no_show.all") || a.Has("appointment.no_show.service")
}

// assertLifecycleServiceAccess: listed .all perms (or *) bypass perimeter.
// SERVICE actors are limited to staff assignments — queue.read.all / schedule.read.all
// must NOT globalize appointment.*.service (LOT 23I RBAC-02).
func (s *Service) assertLifecycleServiceAccess(serviceID uint, a Access, allPerms ...string) error {
	if a.Has("*") {
		return nil
	}
	for _, p := range allPerms {
		if a.Has(p) {
			return nil
		}
	}
	ids, err := s.assignedStaffServiceIDs(a)
	if err != nil {
		return coreerrors.NotFound("Rendez-vous")
	}
	for _, id := range ids {
		if id == serviceID {
			return nil
		}
	}
	return coreerrors.NotFound("Rendez-vous")
}

func (s *Service) advisoryLockLifecycle(tx *gorm.DB, op string, appointmentID, caller uint, key string) error {
	h := fnv.New32a()
	_, _ = fmt.Fprintf(h, "%s:%d:%d:%s", op, appointmentID, caller, key)
	key2 := int32(h.Sum32())
	if err := tx.Exec(`SELECT pg_advisory_xact_lock(?, ?)`, bookingLockNSLifecycle, key2).Error; err != nil {
		return coreerrors.Internal("verrou cycle de vie: " + err.Error())
	}
	return nil
}

func (s *Service) findLifecycleIdempotentHistory(tx *gorm.DB, appointmentID, actor uint, event, key string) (*AppointmentHistory, error) {
	if key == "" {
		return nil, nil
	}
	var rows []AppointmentHistory
	err := tx.Where("appointment_id = ? AND actor_user_id = ? AND event_type = ?", appointmentID, actor, event).
		Order("id DESC").Limit(50).Find(&rows).Error
	if err != nil {
		return nil, coreerrors.Internal(err.Error())
	}
	for i := range rows {
		var p lifecycleHistoryPayload
		if rows[i].Payload == "" {
			continue
		}
		if err := json.Unmarshal([]byte(rows[i].Payload), &p); err != nil {
			continue // malformed historic payload: skip safely
		}
		if p.IdempotencyKey == key {
			return &rows[i], nil
		}
	}
	return nil, nil
}

func snapshotAppointment(appt Appointment) rescheduleSnapshot {
	snap := rescheduleSnapshot{
		PractitionerID:    appt.ExpectedDoctorID,
		StartAt:           appt.ScheduledAt.UTC().Format(time.RFC3339),
		AppointmentTypeID: appt.AppointmentTypeID,
	}
	if appt.ScheduledEndAt != nil {
		snap.EndAt = appt.ScheduledEndAt.UTC().Format(time.RFC3339)
	}
	return snap
}

func marshalLifecyclePayload(p lifecycleHistoryPayload) string {
	b, _ := json.Marshal(p)
	return string(b)
}

func currentDurationMinutes(appt Appointment) (int, error) {
	if appt.ScheduledEndAt != nil {
		mins := int(appt.ScheduledEndAt.Sub(appt.ScheduledAt) / time.Minute)
		if mins > 0 && appt.ScheduledAt.Add(time.Duration(mins)*time.Minute).Equal(*appt.ScheduledEndAt) {
			return mins, nil
		}
		if mins > 0 {
			return mins, nil
		}
	}
	return 0, coreerrors.BadRequest("durée actuelle du rendez-vous indéterminée")
}

func (s *Service) resolveRescheduleInterval(appt Appointment, r RescheduleAppointmentRequest) (bookingResolved, error) {
	if r.StartAt.IsZero() {
		return bookingResolved{}, coreerrors.BadRequest("startAt requis")
	}
	typeID := appt.AppointmentTypeID
	if r.AppointmentTypeID != nil {
		typeID = r.AppointmentTypeID
	}
	var dur *int
	switch {
	case r.DurationMinutes != nil:
		dur = r.DurationMinutes
	case r.AppointmentTypeID != nil:
		dur = nil // derive from new type
	default:
		mins, err := currentDurationMinutes(appt)
		if err != nil {
			if typeID != nil {
				return s.resolveBookingDuration(appt.ServiceID, typeID, nil, r.StartAt)
			}
			return bookingResolved{}, err
		}
		dur = &mins
	}
	return s.resolveBookingDuration(appt.ServiceID, typeID, dur, r.StartAt)
}

func sameRescheduleSemantics(prior AppointmentHistory, r RescheduleAppointmentRequest, resolved bookingResolved, prac uint) bool {
	if strings.TrimSpace(prior.Reason) != strings.TrimSpace(r.Reason) {
		return false
	}
	var p lifecycleHistoryPayload
	if err := json.Unmarshal([]byte(prior.Payload), &p); err != nil || p.New == nil {
		return false
	}
	n := p.New
	if n.StartAt != resolved.Start.UTC().Format(time.RFC3339) {
		return false
	}
	if n.EndAt != resolved.End.UTC().Format(time.RFC3339) {
		return false
	}
	if !ptrEqualUint(n.AppointmentTypeID, resolved.AppointmentTypeID) {
		return false
	}
	if n.PractitionerID == nil || *n.PractitionerID != prac {
		return false
	}
	return true
}

func assertReschedulePrecondition(appt Appointment, r RescheduleAppointmentRequest) error {
	expStart := r.ExpectedScheduledAt.UTC()
	expEnd := r.ExpectedScheduledEndAt.UTC()
	if expStart.IsZero() || expEnd.IsZero() {
		return coreerrors.BadRequest("expectedScheduledAt et expectedScheduledEndAt requis")
	}
	if !expEnd.After(expStart) {
		return coreerrors.BadRequest("expectedScheduledEndAt doit être après expectedScheduledAt")
	}
	if !appt.ScheduledAt.Equal(expStart) {
		return coreerrors.Conflict("État du rendez-vous obsolète (start)")
	}
	if appt.ScheduledEndAt == nil || !appt.ScheduledEndAt.Equal(expEnd) {
		return coreerrors.Conflict("État du rendez-vous obsolète (end)")
	}
	return nil
}

// RescheduleAppointment updates the canonical appointment interval/practitioner (same ID).
func (s *Service) RescheduleAppointment(appointmentID uint, r RescheduleAppointmentRequest, a Access) (*Appointment, error) {
	if !s.canRescheduleAppointments(a) {
		return nil, coreerrors.Forbidden("Reschedule non autorisé")
	}
	if a.UserID == 0 {
		return nil, coreerrors.Unauthorized("Utilisateur non authentifié")
	}
	idemKey := strings.TrimSpace(r.IdempotencyKey)

	var out *Appointment
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if idemKey != "" {
			if e := s.advisoryLockLifecycle(tx, LifecycleOpReschedule, appointmentID, a.UserID, idemKey); e != nil {
				return e
			}
		}
		var appt Appointment
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&appt, appointmentID).Error; e != nil {
			if e == gorm.ErrRecordNotFound {
				return coreerrors.NotFound("Rendez-vous")
			}
			return coreerrors.Internal(e.Error())
		}
		if e := s.assertLifecycleServiceAccess(appt.ServiceID, a, "appointment.reschedule.all", "schedule.manage.all"); e != nil {
			return e
		}
		if e := assertNoActiveQueueLink(appt); e != nil {
			return e
		}
		if e := canTransitionAppointment(appt.Status, LifecycleOpReschedule); e != nil {
			return e
		}
		if e := assertReschedulePrecondition(appt, r); e != nil {
			return e
		}

		if idemKey != "" {
			prior, e := s.findLifecycleIdempotentHistory(tx, appointmentID, a.UserID, ApptHistRescheduled, idemKey)
			if e != nil {
				return e
			}
			if prior != nil {
				resolved, e2 := s.resolveRescheduleInterval(appt, r)
				if e2 != nil {
					return e2
				}
				targetPrac := appt.ExpectedDoctorID
				if r.PractitionerID != nil {
					targetPrac = r.PractitionerID
				}
				if targetPrac == nil {
					return coreerrors.BadRequest("praticien requis pour le reschedule")
				}
				if !sameRescheduleSemantics(*prior, r, resolved, *targetPrac) {
					return coreerrors.Conflict("Clé d'idempotence déjà utilisée avec une autre requête")
				}
				out = &appt
				return nil
			}
		}

		resolved, e := s.resolveRescheduleInterval(appt, r)
		if e != nil {
			return e
		}

		// Omitted practitionerId = keep current (not auto-assign).
		targetPrac := appt.ExpectedDoctorID
		if r.PractitionerID != nil {
			targetPrac = r.PractitionerID
		}
		if targetPrac == nil {
			return coreerrors.BadRequest("praticien requis pour le reschedule")
		}
		if e := s.assertPractitionerAssignedToService(*targetPrac, appt.ServiceID); e != nil {
			return e
		}

		pracIDs := []uint{}
		if appt.ExpectedDoctorID != nil {
			pracIDs = append(pracIDs, *appt.ExpectedDoctorID)
		}
		pracIDs = append(pracIDs, *targetPrac)
		sort.Slice(pracIDs, func(i, j int) bool { return pracIDs[i] < pracIDs[j] })
		dedup := pracIDs[:0]
		var last uint
		for i, id := range pracIDs {
			if i == 0 || id != last {
				dedup = append(dedup, id)
				last = id
			}
		}

		if e := s.advisoryLockPatient(tx, appt.PatientID); e != nil {
			return e
		}
		for _, pid := range dedup {
			if e := s.advisoryLockPractitioner(tx, pid); e != nil {
				return e
			}
		}

		// Re-read under locks; re-check stale precondition against committed state.
		if e := tx.First(&appt, appointmentID).Error; e != nil {
			return coreerrors.Internal(e.Error())
		}
		if e := canTransitionAppointment(appt.Status, LifecycleOpReschedule); e != nil {
			return e
		}
		if e := assertNoActiveQueueLink(appt); e != nil {
			return e
		}
		if e := assertReschedulePrecondition(appt, r); e != nil {
			return e
		}

		overlap, e := s.patientHasOverlapTx(tx, appt.PatientID, resolved.Start, resolved.End, appt.ID)
		if e != nil {
			return coreerrors.Internal(e.Error())
		}
		if overlap {
			return coreerrors.Conflict("Le patient a déjà un rendez-vous sur ce créneau")
		}
		ok, e := s.isIntervalFullyAvailableTx(tx, *targetPrac, appt.ServiceID, resolved.Start, resolved.End, appt.ID)
		if e != nil {
			return coreerrors.Internal(e.Error())
		}
		if !ok {
			return coreerrors.Conflict("Créneau indisponible pour ce praticien")
		}

		oldSnap := snapshotAppointment(appt)
		now := time.Now().UTC()
		end := resolved.End
		prac := *targetPrac
		appt.ExpectedDoctorID = &prac
		appt.AppointmentTypeID = resolved.AppointmentTypeID
		appt.ScheduledAt = resolved.Start
		appt.ScheduledEndAt = &end
		appt.UpdatedAt = now
		if e := tx.Save(&appt).Error; e != nil {
			return coreerrors.Internal(e.Error())
		}
		newSnap := snapshotAppointment(appt)
		payload := marshalLifecyclePayload(lifecycleHistoryPayload{
			IdempotencyKey: idemKey,
			Old:            &oldSnap,
			New:            &newSnap,
		})
		reason := strings.TrimSpace(r.Reason)
		if e := s.writeAppointmentHistory(tx, appt.ID, a.UserID, ApptHistRescheduled, ApptScheduled, ApptScheduled, reason, payload); e != nil {
			return coreerrors.Internal(e.Error())
		}
		out = &appt
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// CancelAppointment sets CANCELLED without deleting the row. Does not mutate queue tickets.
func (s *Service) CancelAppointment(appointmentID uint, r CancelAppointmentRequest, a Access) (*Appointment, error) {
	if !s.canCancelAppointments(a) {
		return nil, coreerrors.Forbidden("Annulation non autorisée")
	}
	if a.UserID == 0 {
		return nil, coreerrors.Unauthorized("Utilisateur non authentifié")
	}
	idemKey := strings.TrimSpace(r.IdempotencyKey)
	reason := strings.TrimSpace(r.Reason)

	var out *Appointment
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if idemKey != "" {
			if e := s.advisoryLockLifecycle(tx, LifecycleOpCancel, appointmentID, a.UserID, idemKey); e != nil {
				return e
			}
		}
		var appt Appointment
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&appt, appointmentID).Error; e != nil {
			if e == gorm.ErrRecordNotFound {
				return coreerrors.NotFound("Rendez-vous")
			}
			return coreerrors.Internal(e.Error())
		}
		if e := s.assertLifecycleServiceAccess(appt.ServiceID, a, "appointment.cancel.all"); e != nil {
			return e
		}

		if appt.Status == ApptCancelled {
			if idemKey != "" {
				prior, e := s.findLifecycleIdempotentHistory(tx, appointmentID, a.UserID, ApptHistCancelled, idemKey)
				if e != nil {
					return e
				}
				if prior != nil {
					if strings.TrimSpace(prior.Reason) != reason {
						return coreerrors.Conflict("Clé d'idempotence déjà utilisée avec une autre requête")
					}
					out = &appt
					return nil
				}
				// Already cancelled under a different/absent key: terminal no-op (no new history).
			}
			// Terminal-state idempotence without key: 200/no-op, no duplicate history.
			out = &appt
			return nil
		}

		if e := assertNoActiveQueueLink(appt); e != nil {
			return e
		}
		if e := canTransitionAppointment(appt.Status, LifecycleOpCancel); e != nil {
			return e
		}

		if idemKey != "" {
			prior, e := s.findLifecycleIdempotentHistory(tx, appointmentID, a.UserID, ApptHistCancelled, idemKey)
			if e != nil {
				return e
			}
			if prior != nil {
				if strings.TrimSpace(prior.Reason) != reason {
					return coreerrors.Conflict("Clé d'idempotence déjà utilisée avec une autre requête")
				}
				out = &appt
				return nil
			}
		}

		if e := s.advisoryLockPatient(tx, appt.PatientID); e != nil {
			return e
		}
		if appt.ExpectedDoctorID != nil {
			if e := s.advisoryLockPractitioner(tx, *appt.ExpectedDoctorID); e != nil {
				return e
			}
		}

		from := appt.Status
		now := time.Now().UTC()
		appt.Status = ApptCancelled
		appt.UpdatedAt = now
		if e := tx.Save(&appt).Error; e != nil {
			return coreerrors.Internal(e.Error())
		}
		payload := marshalLifecyclePayload(lifecycleHistoryPayload{IdempotencyKey: idemKey})
		if e := s.writeAppointmentHistory(tx, appt.ID, a.UserID, ApptHistCancelled, from, ApptCancelled, reason, payload); e != nil {
			return coreerrors.Internal(e.Error())
		}
		out = &appt
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// MarkNoShow marks NO_SHOW. Requires scheduled_at <= now (not future). Does not mutate queue.
func (s *Service) MarkNoShow(appointmentID uint, r NoShowAppointmentRequest, a Access) (*Appointment, error) {
	if !s.canMarkNoShow(a) {
		return nil, coreerrors.Forbidden("Permission refusée")
	}
	if a.UserID == 0 {
		return nil, coreerrors.Unauthorized("Utilisateur non authentifié")
	}
	idemKey := strings.TrimSpace(r.IdempotencyKey)
	reason := strings.TrimSpace(r.Reason)

	var out *Appointment
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if idemKey != "" {
			if e := s.advisoryLockLifecycle(tx, LifecycleOpNoShow, appointmentID, a.UserID, idemKey); e != nil {
				return e
			}
		}
		var appt Appointment
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&appt, appointmentID).Error; e != nil {
			if e == gorm.ErrRecordNotFound {
				return coreerrors.NotFound("Rendez-vous")
			}
			return coreerrors.Internal(e.Error())
		}
		if e := s.assertLifecycleServiceAccess(appt.ServiceID, a, "appointment.no_show.all"); e != nil {
			return e
		}

		if appt.Status == ApptNoShow {
			if idemKey != "" {
				prior, e := s.findLifecycleIdempotentHistory(tx, appointmentID, a.UserID, ApptHistNoShow, idemKey)
				if e != nil {
					return e
				}
				if prior != nil {
					if strings.TrimSpace(prior.Reason) != reason {
						return coreerrors.Conflict("Clé d'idempotence déjà utilisée avec une autre requête")
					}
				}
			}
			// Terminal-state idempotence: 200/no-op, no duplicate history.
			out = &appt
			return nil
		}

		if e := assertNoActiveQueueLink(appt); e != nil {
			return e
		}
		if e := canTransitionAppointment(appt.Status, LifecycleOpNoShow); e != nil {
			return e
		}
		now := time.Now().UTC()
		if !noShowEligibleTime(appt.ScheduledAt, now) {
			return coreerrors.BadRequest("No-show refusé: le rendez-vous est encore dans le futur")
		}

		if idemKey != "" {
			prior, e := s.findLifecycleIdempotentHistory(tx, appointmentID, a.UserID, ApptHistNoShow, idemKey)
			if e != nil {
				return e
			}
			if prior != nil {
				if strings.TrimSpace(prior.Reason) != reason {
					return coreerrors.Conflict("Clé d'idempotence déjà utilisée avec une autre requête")
				}
				out = &appt
				return nil
			}
		}

		if e := s.advisoryLockPatient(tx, appt.PatientID); e != nil {
			return e
		}
		if appt.ExpectedDoctorID != nil {
			if e := s.advisoryLockPractitioner(tx, *appt.ExpectedDoctorID); e != nil {
				return e
			}
		}

		from := appt.Status
		appt.Status = ApptNoShow
		appt.UpdatedAt = now
		if e := tx.Save(&appt).Error; e != nil {
			return coreerrors.Internal(e.Error())
		}
		payload := marshalLifecyclePayload(lifecycleHistoryPayload{IdempotencyKey: idemKey})
		if e := s.writeAppointmentHistory(tx, appt.ID, a.UserID, ApptHistNoShow, from, ApptNoShow, reason, payload); e != nil {
			return coreerrors.Internal(e.Error())
		}
		out = &appt
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
