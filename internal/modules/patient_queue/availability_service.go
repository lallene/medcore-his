package patient_queue

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/core/scheduling"
)

// AvailabilityQuery is the domain-level availability request (independent of HTTP).
type AvailabilityQuery struct {
	ServiceID         uint
	PractitionerID    *uint
	AppointmentTypeID *uint
	DurationMinutes   *int
	From              time.Time
	To                time.Time
	SlotStepMinutes   *int
}

// AvailabilitySlot is an ephemeral candidate — never persisted, never a reservation.
type AvailabilitySlot struct {
	PractitionerID    uint       `json:"practitionerId"`
	ServiceID         uint       `json:"serviceId"`
	StartAt           time.Time  `json:"startAt"`
	EndAt             time.Time  `json:"endAt"`
	DurationMinutes   int        `json:"durationMinutes"`
	AppointmentTypeID *uint      `json:"appointmentTypeId,omitempty"`
}

// AvailabilityResult is a read-only snapshot (not a booking guarantee).
type AvailabilityResult struct {
	Query             AvailabilityQueryMeta `json:"query"`
	Timezone          string                `json:"timezone"`
	Slots             []AvailabilitySlot    `json:"slots"`
	Count             int                   `json:"count"`
	SnapshotNote      string                `json:"snapshotNote"`
}

type AvailabilityQueryMeta struct {
	ServiceID         uint       `json:"serviceId"`
	PractitionerID    *uint      `json:"practitionerId,omitempty"`
	AppointmentTypeID *uint      `json:"appointmentTypeId,omitempty"`
	DurationMinutes   int        `json:"durationMinutes"`
	SlotStepMinutes   int        `json:"slotStepMinutes"`
	From              time.Time  `json:"from"`
	To                time.Time  `json:"to"`
}

const availabilitySnapshotNote = "Availability is a read-only snapshot; slots are not reserved. LOT 23D performs authoritative booking."

// AppointmentBlocksAvailability returns whether an appointment status consumes capacity.
//
//	SCHEDULED, ARRIVED, CHECKED_IN, IN_PROGRESS, COMPLETED → block
//	CANCELLED, NO_SHOW → do not block (capacity released / never used)
func AppointmentBlocksAvailability(status string) bool {
	switch status {
	case ApptScheduled, ApptArrived, ApptCheckedIn, ApptInProgress, ApptCompleted:
		return true
	case ApptCancelled, ApptNoShow:
		return false
	default:
		// Unknown statuses: fail closed (block) to avoid double-offer.
		return true
	}
}

func legacyFallbackMinutes() int {
	if v := os.Getenv(scheduling.EnvLegacyFallbackMinutes); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return scheduling.LegacyAppointmentFallbackMinutes
}

// ResolveAppointmentEnd returns a deterministic end for availability purposes without mutating DB.
func ResolveAppointmentEnd(appt Appointment, typeDur map[uint]int) time.Time {
	if appt.ScheduledEndAt != nil && appt.ScheduledEndAt.After(appt.ScheduledAt) {
		return appt.ScheduledEndAt.UTC()
	}
	mins := legacyFallbackMinutes()
	if appt.AppointmentTypeID != nil {
		if d, ok := typeDur[*appt.AppointmentTypeID]; ok && d > 0 {
			mins = d
		}
	}
	return appt.ScheduledAt.UTC().Add(time.Duration(mins) * time.Minute)
}

func (s *Service) resolveDuration(q AvailabilityQuery) (duration int, typeID *uint, err error) {
	var typeDur int
	var at *AppointmentType
	if q.AppointmentTypeID != nil {
		var row AppointmentType
		if e := s.db.First(&row, *q.AppointmentTypeID).Error; e != nil {
			return 0, nil, coreerrors.BadRequest("Type de rendez-vous introuvable")
		}
		if !row.Active {
			return 0, nil, coreerrors.BadRequest("Type de rendez-vous inactif")
		}
		if row.ServiceID != nil && *row.ServiceID != q.ServiceID {
			return 0, nil, coreerrors.BadRequest("Type de rendez-vous non applicable à ce service")
		}
		if row.DefaultDurationMinutes <= 0 {
			return 0, nil, coreerrors.BadRequest("Durée du type de rendez-vous invalide")
		}
		at = &row
		typeDur = row.DefaultDurationMinutes
		typeID = q.AppointmentTypeID
	}
	if q.DurationMinutes != nil && q.AppointmentTypeID != nil {
		if *q.DurationMinutes != typeDur {
			return 0, nil, coreerrors.BadRequest("durationMinutes contradictoire avec appointmentTypeId")
		}
		duration = typeDur
	} else if q.DurationMinutes != nil {
		duration = *q.DurationMinutes
	} else if q.AppointmentTypeID != nil {
		duration = typeDur
	} else {
		return 0, nil, coreerrors.BadRequest("appointmentTypeId ou durationMinutes requis")
	}
	_ = at
	if duration < scheduling.MinDurationMinutes || duration > scheduling.MaxDurationMinutes {
		return 0, nil, coreerrors.BadRequest(fmt.Sprintf("durée hors limites (%d–%d min)", scheduling.MinDurationMinutes, scheduling.MaxDurationMinutes))
	}
	return duration, typeID, nil
}

func (s *Service) validateAvailabilityRange(from, to time.Time) error {
	from = from.UTC()
	to = to.UTC()
	if !to.After(from) {
		return coreerrors.BadRequest("to doit être > from")
	}
	if to.Sub(from) > time.Duration(scheduling.MaxQueryRangeDays)*24*time.Hour {
		return coreerrors.BadRequest(fmt.Sprintf("plage maximale %d jours", scheduling.MaxQueryRangeDays))
	}
	return nil
}

func (s *Service) assertCanReadAvailability(serviceID uint, practitionerID *uint, a Access) error {
	if s.canReadAllSchedules(a) {
		return nil
	}
	if practitionerID != nil && a.Has("schedule.read.own") && *practitionerID == a.UserID {
		// own availability still requires service assignment for that service
		return s.assertPractitionerAssignedToService(a.UserID, serviceID)
	}
	if a.Has("schedule.read.service") || a.Has("schedule.manage.service") || a.Has("schedule.manage.all") {
		return s.assertScheduleServiceInScope(serviceID, a)
	}
	if a.Has("schedule.read.own") && practitionerID == nil {
		// service-wide with only own: not allowed — would enumerate other practitioners
		return coreerrors.Forbidden("Lecture disponibilité service non autorisée")
	}
	return coreerrors.Forbidden("Lecture disponibilité non autorisée")
}

// listEligiblePractitionerIDs: active staff assigned to service (no parallel doctor table).
// Limitation: MedCore has no reliable exclusive "clinician" flag beyond assignment;
// bookable capacity is further filtered by schedules/positive exceptions below.
func (s *Service) listEligiblePractitionerIDs(serviceID uint) ([]uint, error) {
	var ids []uint
	err := s.db.Raw(`
		SELECT DISTINCT sp.user_id
		FROM staff_profiles sp
		JOIN staff_service_assignments ssa ON ssa.profile_id = sp.id AND ssa.active
		WHERE sp.active AND ssa.service_id = ?
		ORDER BY sp.user_id`, serviceID).Scan(&ids).Error
	if err != nil {
		return nil, coreerrors.Internal(err.Error())
	}
	return ids, nil
}

type apptBlockRow struct {
	ID                uint
	ExpectedDoctorID  *uint
	ScheduledAt       time.Time
	ScheduledEndAt    *time.Time
	AppointmentTypeID *uint
	Status            string
}

// ComputeAvailability is the domain entry-point for LOT 23C (and reusable by 23D under its own locks).
// Read-only: does not mutate appointments, schedules, exceptions, or queue tickets.
func (s *Service) ComputeAvailability(q AvailabilityQuery, a Access) (*AvailabilityResult, error) {
	if q.ServiceID == 0 {
		return nil, coreerrors.BadRequest("serviceId requis")
	}
	if err := s.validateAvailabilityRange(q.From, q.To); err != nil {
		return nil, err
	}
	if err := s.assertServiceExists(q.ServiceID); err != nil {
		return nil, err
	}
	if err := s.assertCanReadAvailability(q.ServiceID, q.PractitionerID, a); err != nil {
		return nil, err
	}

	duration, typeID, err := s.resolveDuration(q)
	if err != nil {
		return nil, err
	}
	step := duration
	if q.SlotStepMinutes != nil {
		step = *q.SlotStepMinutes
		if step < scheduling.MinSlotStepMinutes || step > scheduling.MaxSlotStepMinutes {
			return nil, coreerrors.BadRequest("slotStepMinutes hors limites")
		}
	}

	from := q.From.UTC()
	to := q.To.UTC()
	loc := scheduling.Location()

	var practitionerIDs []uint
	if q.PractitionerID != nil {
		pid := *q.PractitionerID
		if err := s.assertPractitionerAssignedToService(pid, q.ServiceID); err != nil {
			// domain error (not silent empty)
			return nil, err
		}
		practitionerIDs = []uint{pid}
	} else {
		practitionerIDs, err = s.listEligiblePractitionerIDs(q.ServiceID)
		if err != nil {
			return nil, err
		}
	}
	if len(practitionerIDs) == 0 {
		return &AvailabilityResult{
			Query: AvailabilityQueryMeta{
				ServiceID: q.ServiceID, PractitionerID: q.PractitionerID, AppointmentTypeID: typeID,
				DurationMinutes: duration, SlotStepMinutes: step, From: from, To: to,
			},
			Timezone: scheduling.LocationName(), Slots: []AvailabilitySlot{}, Count: 0,
			SnapshotNote: availabilitySnapshotNote,
		}, nil
	}

	// --- Batched loads (avoid N+1) ---
	fromDate := dateOnlyUTC(from.In(loc))
	toDate := dateOnlyUTC(to.In(loc).Add(-time.Nanosecond)) // last instant in range

	var schedules []StaffWorkingSchedule
	if err := s.db.Where("service_id = ? AND active = true AND practitioner_id IN ?", q.ServiceID, practitionerIDs).
		Where("valid_from <= ? AND (valid_until IS NULL OR valid_until >= ?)", toDate, fromDate).
		Find(&schedules).Error; err != nil {
		return nil, coreerrors.Internal(err.Error())
	}

	var exceptions []ScheduleException
	if err := s.db.Where("service_id = ? AND active = true AND cancelled_at IS NULL AND practitioner_id IN ?", q.ServiceID, practitionerIDs).
		Where("start_at < ? AND end_at > ?", to, from).
		Find(&exceptions).Error; err != nil {
		return nil, coreerrors.Internal(err.Error())
	}

	var appts []apptBlockRow
	if err := s.db.Model(&Appointment{}).
		Select("id, expected_doctor_id, scheduled_at, scheduled_end_at, appointment_type_id, status").
		Where("service_id = ? AND expected_doctor_id IN ?", q.ServiceID, practitionerIDs).
		Where("scheduled_at < ?", to).
		Where("(scheduled_end_at IS NULL OR scheduled_end_at > ?)", from).
		Find(&appts).Error; err != nil {
		return nil, coreerrors.Internal(err.Error())
	}

	// Load type durations for legacy end resolution (one query)
	typeIDs := map[uint]struct{}{}
	for _, ap := range appts {
		if ap.AppointmentTypeID != nil {
			typeIDs[*ap.AppointmentTypeID] = struct{}{}
		}
	}
	typeDur := map[uint]int{}
	if len(typeIDs) > 0 {
		ids := make([]uint, 0, len(typeIDs))
		for id := range typeIDs {
			ids = append(ids, id)
		}
		var types []AppointmentType
		_ = s.db.Where("id IN ?", ids).Find(&types)
		for _, t := range types {
			typeDur[t.ID] = t.DefaultDurationMinutes
		}
	}

	schedByPrac := map[uint][]StaffWorkingSchedule{}
	for _, row := range schedules {
		schedByPrac[row.PractitionerID] = append(schedByPrac[row.PractitionerID], row)
	}
	exByPrac := map[uint][]ScheduleException{}
	for _, row := range exceptions {
		exByPrac[row.PractitionerID] = append(exByPrac[row.PractitionerID], row)
	}
	apptByPrac := map[uint][]apptBlockRow{}
	for _, row := range appts {
		if row.ExpectedDoctorID == nil {
			continue
		}
		apptByPrac[*row.ExpectedDoctorID] = append(apptByPrac[*row.ExpectedDoctorID], row)
	}

	queryBound := scheduling.Interval{Start: from, End: to}
	dur := time.Duration(duration) * time.Minute
	stepDur := time.Duration(step) * time.Minute

	var slots []AvailabilitySlot
	for _, pid := range practitionerIDs {
		free := s.computePractitionerFreeIntervals(
			pid, q.ServiceID, from, to, loc, queryBound,
			schedByPrac[pid], exByPrac[pid], apptByPrac[pid], typeDur,
		)
		generated := scheduling.GenerateSlots(free, dur, stepDur)
		for _, g := range generated {
			slots = append(slots, AvailabilitySlot{
				PractitionerID: pid, ServiceID: q.ServiceID,
				StartAt: g.Start.UTC(), EndAt: g.End.UTC(),
				DurationMinutes: duration, AppointmentTypeID: typeID,
			})
		}
	}

	sort.SliceStable(slots, func(i, j int) bool {
		if !slots[i].StartAt.Equal(slots[j].StartAt) {
			return slots[i].StartAt.Before(slots[j].StartAt)
		}
		if slots[i].PractitionerID != slots[j].PractitionerID {
			return slots[i].PractitionerID < slots[j].PractitionerID
		}
		return slots[i].EndAt.Before(slots[j].EndAt)
	})

	if len(slots) > scheduling.MaxGeneratedSlots {
		return nil, coreerrors.BadRequest(fmt.Sprintf("trop de créneaux générés (>%d); réduisez la plage ou augmentez la durée", scheduling.MaxGeneratedSlots))
	}

	return &AvailabilityResult{
		Query: AvailabilityQueryMeta{
			ServiceID: q.ServiceID, PractitionerID: q.PractitionerID, AppointmentTypeID: typeID,
			DurationMinutes: duration, SlotStepMinutes: step, From: from, To: to,
		},
		Timezone:     scheduling.LocationName(),
		Slots:        slots,
		Count:        len(slots),
		SnapshotNote: availabilitySnapshotNote,
	}, nil
}

func (s *Service) computePractitionerFreeIntervals(
	practitionerID, serviceID uint,
	from, to time.Time,
	loc *time.Location,
	queryBound scheduling.Interval,
	schedules []StaffWorkingSchedule,
	exceptions []ScheduleException,
	appts []apptBlockRow,
	typeDur map[uint]int,
) []scheduling.Interval {
	_ = practitionerID
	_ = serviceID
	// 1. Project recurring working windows onto concrete dates
	var base []scheduling.Interval
	for d := dateOnlyInLoc(from, loc); !d.After(dateOnlyInLoc(to.Add(-time.Nanosecond), loc)); d = d.AddDate(0, 0, 1) {
		wd := int(d.Weekday())
		for _, w := range schedules {
			if w.Weekday != wd {
				continue
			}
			if !scheduleValidOn(w, d, loc) {
				continue
			}
			start, err1 := scheduling.ProjectWallClock(d, w.StartTime, loc)
			end, err2 := scheduling.ProjectWallClock(d, w.EndTime, loc)
			if err1 != nil || err2 != nil || !end.After(start) {
				continue
			}
			base = append(base, scheduling.Interval{Start: start.UTC(), End: end.UTC()})
		}
	}

	// 2. Union positive exceptions
	var positive []scheduling.Interval
	var negative []scheduling.Interval
	for _, ex := range exceptions {
		iv := scheduling.Interval{Start: ex.StartAt.UTC(), End: ex.EndAt.UTC()}
		if !iv.Valid() {
			continue
		}
		if IsPositiveException(ex.Type) {
			positive = append(positive, iv)
		} else {
			negative = append(negative, iv)
		}
	}
	base = scheduling.Merge(append(base, positive...))

	// 3. Subtract negative (negative wins over positive via subtract after union)
	base = scheduling.Subtract(base, negative)

	// 4. Subtract blocking appointments
	var blockers []scheduling.Interval
	for _, ap := range appts {
		if !AppointmentBlocksAvailability(ap.Status) {
			continue
		}
		start := ap.ScheduledAt.UTC()
		end := ResolveAppointmentEnd(Appointment{
			ScheduledAt: start, ScheduledEndAt: ap.ScheduledEndAt, AppointmentTypeID: ap.AppointmentTypeID,
		}, typeDur)
		iv := scheduling.Interval{Start: start, End: end}
		if !iv.Valid() {
			continue
		}
		if clipped, ok := scheduling.Clip(iv, queryBound); ok {
			blockers = append(blockers, clipped)
		}
	}
	base = scheduling.Subtract(base, blockers)

	// 5. Clip to query range
	var clipped []scheduling.Interval
	for _, iv := range base {
		if c, ok := scheduling.Clip(iv, queryBound); ok {
			clipped = append(clipped, c)
		}
	}
	return scheduling.Merge(clipped)
}

func dateOnlyInLoc(t time.Time, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

func scheduleValidOn(w StaffWorkingSchedule, day time.Time, loc *time.Location) bool {
	y, m, d := day.In(loc).Date()
	dayUTC := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	vf := dateOnlyUTC(w.ValidFrom)
	if dayUTC.Before(vf) {
		return false
	}
	if w.ValidUntil != nil {
		vu := dateOnlyUTC(*w.ValidUntil)
		if dayUTC.After(vu) {
			return false
		}
	}
	return true
}

// FirstAvailable returns the earliest generated candidate slot (read-only discovery).
func (s *Service) FirstAvailable(q AvailabilityQuery, a Access) (*AvailabilitySlot, error) {
	res, err := s.ComputeAvailability(q, a)
	if err != nil {
		return nil, err
	}
	if len(res.Slots) == 0 {
		return nil, coreerrors.NotFound("Aucune disponibilité")
	}
	slot := res.Slots[0]
	return &slot, nil
}

// IsIntervalAvailable checks whether [start,end) is fully contained in free capacity.
// NOT concurrency protection — 23D must re-check under transactional locks.
func (s *Service) IsIntervalAvailable(practitionerID, serviceID uint, start, end time.Time, a Access) (bool, error) {
	if !end.After(start) {
		return false, coreerrors.BadRequest("intervalle invalide")
	}
	if err := s.assertCanReadAvailability(serviceID, &practitionerID, a); err != nil {
		return false, err
	}
	return s.isIntervalFullyAvailableTx(s.db, practitionerID, serviceID, start.UTC(), end.UTC(), 0)
}
