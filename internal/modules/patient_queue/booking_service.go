package patient_queue

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/core/scheduling"
	"gorm.io/gorm"
)

// Advisory-lock namespaces for transactional booking (LOT 23D).
// Lock order when idempotency key present:
//  1. idempotency (caller+key)
//  2. patient
//  3. practitioners ascending
// Without idempotency key: patient, then practitioners ascending.
const (
	bookingLockNSPatient      = 230401
	bookingLockNSPractitioner = 230402
	bookingLockNSIdempotency  = 230403
)

// BookAppointmentRequest is the authoritative booking input (LOT 23D).
type BookAppointmentRequest struct {
	PatientID         uint       `json:"patientId" binding:"required"`
	ServiceID         uint       `json:"serviceId" binding:"required"`
	PractitionerID    *uint      `json:"practitionerId"`
	AppointmentTypeID *uint      `json:"appointmentTypeId"`
	StartAt           time.Time  `json:"startAt" binding:"required"`
	DurationMinutes   *int       `json:"durationMinutes"`
	Reason            string     `json:"reason"`
	IdempotencyKey    string     `json:"idempotencyKey"`
}

type bookingResolved struct {
	Start             time.Time
	End               time.Time
	DurationMinutes   int
	AppointmentTypeID *uint
}

func (s *Service) canBookAppointments(a Access) bool {
	return a.Has("*") || a.Has("queue.checkin") || a.Has("schedule.manage.service") || a.Has("schedule.manage.all")
}

func (s *Service) assertPatientExists(patientID uint) error {
	var n int64
	if err := s.db.Raw(`SELECT COUNT(1) FROM patients WHERE id=?`, patientID).Scan(&n).Error; err != nil {
		return coreerrors.Internal(err.Error())
	}
	if n == 0 {
		return coreerrors.NotFound("Patient")
	}
	return nil
}

// resolveBookingDuration mirrors LOT 23C duration policy; always produces a concrete end.
func (s *Service) resolveBookingDuration(serviceID uint, typeID *uint, durationMinutes *int, start time.Time) (bookingResolved, error) {
	var out bookingResolved
	out.Start = start.UTC()
	var typeDur int
	if typeID != nil {
		var at AppointmentType
		if err := s.db.First(&at, *typeID).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return out, coreerrors.NotFound("Type de rendez-vous")
			}
			return out, coreerrors.Internal(err.Error())
		}
		if !at.Active {
			return out, coreerrors.BadRequest("Type de rendez-vous inactif")
		}
		if at.ServiceID != nil && *at.ServiceID != serviceID {
			return out, coreerrors.BadRequest("Type de rendez-vous hors service")
		}
		if at.DefaultDurationMinutes <= 0 {
			return out, coreerrors.BadRequest("durée du type invalide")
		}
		typeDur = at.DefaultDurationMinutes
		id := at.ID
		out.AppointmentTypeID = &id
	}
	switch {
	case durationMinutes != nil && typeID != nil:
		if *durationMinutes != typeDur {
			return out, coreerrors.BadRequest("durationMinutes contradictoire avec appointmentTypeId")
		}
		out.DurationMinutes = typeDur
	case durationMinutes != nil:
		out.DurationMinutes = *durationMinutes
	case typeID != nil:
		out.DurationMinutes = typeDur
	default:
		return out, coreerrors.BadRequest("appointmentTypeId ou durationMinutes requis")
	}
	if out.DurationMinutes < scheduling.MinDurationMinutes || out.DurationMinutes > scheduling.MaxDurationMinutes {
		return out, coreerrors.BadRequest(fmt.Sprintf("durée hors limites (%d–%d min)", scheduling.MinDurationMinutes, scheduling.MaxDurationMinutes))
	}
	out.End = out.Start.Add(time.Duration(out.DurationMinutes) * time.Minute)
	if !out.End.After(out.Start) {
		return out, coreerrors.BadRequest("intervalle invalide")
	}
	return out, nil
}

func (s *Service) advisoryLockPatient(tx *gorm.DB, patientID uint) error {
	if err := tx.Exec(`SELECT pg_advisory_xact_lock(?, ?)`, bookingLockNSPatient, int64(patientID)).Error; err != nil {
		return coreerrors.Internal("verrou patient: " + err.Error())
	}
	return nil
}

func (s *Service) advisoryLockPractitioner(tx *gorm.DB, practitionerID uint) error {
	if err := tx.Exec(`SELECT pg_advisory_xact_lock(?, ?)`, bookingLockNSPractitioner, int64(practitionerID)).Error; err != nil {
		return coreerrors.Internal("verrou praticien: " + err.Error())
	}
	return nil
}

// advisoryLockIdempotency serializes same (caller, key) retries.
// key2 = int32(FNV-1a32("userID:key")) — pg_advisory_xact_lock(int4,int4); hash collisions only serialize briefly.
func (s *Service) advisoryLockIdempotency(tx *gorm.DB, caller uint, key string) error {
	h := fnv.New32a()
	_, _ = fmt.Fprintf(h, "%d:%s", caller, key)
	key2 := int32(h.Sum32())
	if err := tx.Exec(`SELECT pg_advisory_xact_lock(?, ?)`, bookingLockNSIdempotency, key2).Error; err != nil {
		return coreerrors.Internal("verrou idempotence: " + err.Error())
	}
	return nil
}

func (s *Service) loadTypeDurMap(db *gorm.DB, appts []apptBlockRow) map[uint]int {
	typeDur := map[uint]int{}
	ids := map[uint]struct{}{}
	for _, ap := range appts {
		if ap.AppointmentTypeID != nil {
			ids[*ap.AppointmentTypeID] = struct{}{}
		}
	}
	if len(ids) == 0 {
		return typeDur
	}
	list := make([]uint, 0, len(ids))
	for id := range ids {
		list = append(list, id)
	}
	var types []AppointmentType
	_ = db.Where("id IN ?", list).Find(&types)
	for _, t := range types {
		typeDur[t.ID] = t.DefaultDurationMinutes
	}
	return typeDur
}

func (s *Service) loadPractitionerAppointmentsTx(tx *gorm.DB, practitionerID, serviceID uint, from, to time.Time) ([]apptBlockRow, error) {
	var appts []apptBlockRow
	err := tx.Model(&Appointment{}).
		Select("id, expected_doctor_id, scheduled_at, scheduled_end_at, appointment_type_id, status").
		Where("service_id = ? AND expected_doctor_id = ?", serviceID, practitionerID).
		Where("scheduled_at < ?", to).
		Where("(scheduled_end_at IS NULL OR scheduled_end_at > ?)", from).
		Find(&appts).Error
	return appts, err
}

func (s *Service) loadPatientAppointmentsTx(tx *gorm.DB, patientID uint, from, to time.Time) ([]apptBlockRow, error) {
	var appts []apptBlockRow
	err := tx.Model(&Appointment{}).
		Select("id, expected_doctor_id, scheduled_at, scheduled_end_at, appointment_type_id, status").
		Where("patient_id = ?", patientID).
		Where("scheduled_at < ?", to).
		Where("(scheduled_end_at IS NULL OR scheduled_end_at > ?)", from).
		Find(&appts).Error
	return appts, err
}

func appointmentOverlapsRequested(ap apptBlockRow, start, end time.Time, typeDur map[uint]int) bool {
	if !AppointmentBlocksAvailability(ap.Status) {
		return false
	}
	apStart := ap.ScheduledAt.UTC()
	apEnd := ResolveAppointmentEnd(Appointment{
		ScheduledAt: apStart, ScheduledEndAt: ap.ScheduledEndAt, AppointmentTypeID: ap.AppointmentTypeID,
	}, typeDur)
	return apStart.Before(end) && apEnd.After(start)
}

func (s *Service) patientHasOverlapTx(tx *gorm.DB, patientID uint, start, end time.Time) (bool, error) {
	appts, err := s.loadPatientAppointmentsTx(tx, patientID, start, end)
	if err != nil {
		return false, err
	}
	typeDur := s.loadTypeDurMap(tx, appts)
	for _, ap := range appts {
		if appointmentOverlapsRequested(ap, start, end, typeDur) {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) isIntervalFullyAvailableTx(tx *gorm.DB, practitionerID, serviceID uint, start, end time.Time) (bool, error) {
	from, to := start.UTC(), end.UTC()
	loc := scheduling.Location()
	queryBound := scheduling.Interval{Start: from, End: to}
	fromDate := dateOnlyUTC(from.In(loc))
	toDate := dateOnlyUTC(to.In(loc).Add(-time.Nanosecond))

	var schedules []StaffWorkingSchedule
	if err := tx.Where("practitioner_id = ? AND service_id = ? AND active = true", practitionerID, serviceID).
		Where("valid_from <= ? AND (valid_until IS NULL OR valid_until >= ?)", toDate, fromDate).
		Find(&schedules).Error; err != nil {
		return false, err
	}
	var exceptions []ScheduleException
	if err := tx.Where("practitioner_id = ? AND service_id = ? AND active = true AND cancelled_at IS NULL", practitionerID, serviceID).
		Where("start_at < ? AND end_at > ?", to, from).
		Find(&exceptions).Error; err != nil {
		return false, err
	}
	appts, err := s.loadPractitionerAppointmentsTx(tx, practitionerID, serviceID, from, to)
	if err != nil {
		return false, err
	}
	typeDur := s.loadTypeDurMap(tx, appts)
	free := s.computePractitionerFreeIntervals(practitionerID, serviceID, from, to, loc, queryBound, schedules, exceptions, appts, typeDur)
	for _, iv := range free {
		if !iv.Start.After(from) && !iv.End.Before(to) {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) resolveBookingCandidates(serviceID uint, practitionerID *uint, start, end time.Time) ([]uint, error) {
	if practitionerID != nil {
		if err := s.assertPractitionerAssignedToService(*practitionerID, serviceID); err != nil {
			return nil, err
		}
		ok, err := s.isIntervalFullyAvailableTx(s.db, *practitionerID, serviceID, start, end)
		if err != nil {
			return nil, coreerrors.Internal(err.Error())
		}
		if !ok {
			return nil, coreerrors.Conflict("Créneau indisponible pour ce praticien")
		}
		return []uint{*practitionerID}, nil
	}
	ids, err := s.listEligiblePractitionerIDs(serviceID)
	if err != nil {
		return nil, err
	}
	var candidates []uint
	for _, id := range ids {
		ok, e := s.isIntervalFullyAvailableTx(s.db, id, serviceID, start, end)
		if e != nil {
			return nil, coreerrors.Internal(e.Error())
		}
		if ok {
			candidates = append(candidates, id)
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i] < candidates[j] })
	if len(candidates) == 0 {
		return nil, coreerrors.Conflict("Aucun praticien disponible pour ce créneau")
	}
	return candidates, nil
}

func bookingRequestFingerprint(r BookAppointmentRequest, resolved bookingResolved, practitionerID *uint) string {
	auto := r.PractitionerID == nil
	payload := map[string]any{
		"patientId": r.PatientID, "serviceId": r.ServiceID,
		"practitionerId": practitionerID, "appointmentTypeId": resolved.AppointmentTypeID,
		"startAt": resolved.Start.UTC().Format(time.RFC3339),
		"endAt":   resolved.End.UTC().Format(time.RFC3339),
		"durationMinutes": resolved.DurationMinutes,
		"autoAssigned":    auto,
		"reason":          strings.TrimSpace(r.Reason),
	}
	b, _ := json.Marshal(payload)
	return string(b)
}

func ptrEqualUint(a, b *uint) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

// sameBookingSemantics — exact semantic identity for idempotent retry.
// Reason IS part of semantics (trimmed). Appointment type compared by ID, not only duration.
// Specific practitioner request must match practitioner; auto-assignment intent stays auto
// (stored practitioner may be any assigned winner).
func sameBookingSemantics(prior Appointment, r BookAppointmentRequest, resolved bookingResolved) bool {
	if prior.PatientID != r.PatientID || prior.ServiceID != r.ServiceID {
		return false
	}
	if !prior.ScheduledAt.Equal(resolved.Start) {
		return false
	}
	if prior.ScheduledEndAt == nil || !prior.ScheduledEndAt.Equal(resolved.End) {
		return false
	}
	if !ptrEqualUint(prior.AppointmentTypeID, resolved.AppointmentTypeID) {
		return false
	}
	if strings.TrimSpace(prior.Reason) != strings.TrimSpace(r.Reason) {
		return false
	}
	if r.PractitionerID != nil {
		if prior.ExpectedDoctorID == nil || *prior.ExpectedDoctorID != *r.PractitionerID {
			return false
		}
	}
	// auto-assignment: request has nil practitioner — prior is accepted if other fields match
	return true
}

func (s *Service) findIdempotentAppointmentTx(tx *gorm.DB, key string, actor uint) (*Appointment, error) {
	var row Appointment
	err := tx.Where("idempotency_key = ? AND created_by = ?", key, actor).First(&row).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, coreerrors.Internal(err.Error())
	}
	return &row, nil
}

// BookAppointment creates a SCHEDULED appointment with transactional double-booking protection.
// Returns (appointment, reusedIdempotent, error). reusedIdempotent true → same prior row (HTTP 200/201 OK).
func (s *Service) BookAppointment(r BookAppointmentRequest, a Access) (*Appointment, bool, error) {
	if !s.canBookAppointments(a) {
		return nil, false, coreerrors.Forbidden("Réservation non autorisée")
	}
	if r.PatientID == 0 || r.ServiceID == 0 {
		return nil, false, coreerrors.BadRequest("patient et service requis")
	}
	if r.StartAt.IsZero() {
		return nil, false, coreerrors.BadRequest("startAt requis")
	}
	if a.UserID == 0 {
		return nil, false, coreerrors.Unauthorized("Utilisateur non authentifié")
	}
	if err := s.assertServiceExists(r.ServiceID); err != nil {
		return nil, false, err
	}
	if err := s.assertServiceInScope(r.ServiceID, a); err != nil {
		return nil, false, err
	}
	if err := s.assertPatientExists(r.PatientID); err != nil {
		return nil, false, err
	}

	resolved, err := s.resolveBookingDuration(r.ServiceID, r.AppointmentTypeID, r.DurationMinutes, r.StartAt)
	if err != nil {
		return nil, false, err
	}

	idemKey := strings.TrimSpace(r.IdempotencyKey)

	// Pre-resolve candidates outside TX only when not racing on idempotency;
	// when idempotency key present, resolve after idempotency lock inside TX path still needs candidates first for lock list.
	// Candidates snapshot before TX is OK; re-check under locks.
	candidates, err := s.resolveBookingCandidates(r.ServiceID, r.PractitionerID, resolved.Start, resolved.End)
	if err != nil {
		// If idempotent prior exists, concurrent loser of slot may still need to return prior — handle inside TX after idem lock.
		if idemKey == "" {
			return nil, false, err
		}
		candidates = nil
	}

	var created *Appointment
	var reused bool
	candidateResolveErr := err

	txErr := s.db.Transaction(func(tx *gorm.DB) error {
		if idemKey != "" {
			if e := s.advisoryLockIdempotency(tx, a.UserID, idemKey); e != nil {
				return e
			}
			prior, e := s.findIdempotentAppointmentTx(tx, idemKey, a.UserID)
			if e != nil {
				return e
			}
			if prior != nil {
				if !sameBookingSemantics(*prior, r, resolved) {
					return coreerrors.Conflict("Clé d'idempotence déjà utilisée avec une autre requête")
				}
				created = prior
				reused = true
				return nil
			}
		}

		// Fresh create path — need candidates under booking locks
		cands := candidates
		if len(cands) == 0 {
			if candidateResolveErr != nil {
				return candidateResolveErr
			}
			var e error
			cands, e = s.resolveBookingCandidates(r.ServiceID, r.PractitionerID, resolved.Start, resolved.End)
			if e != nil {
				return e
			}
		}
		lockList := append([]uint(nil), cands...)
		sort.Slice(lockList, func(i, j int) bool { return lockList[i] < lockList[j] })

		if e := s.advisoryLockPatient(tx, r.PatientID); e != nil {
			return e
		}
		for _, pid := range lockList {
			if e := s.advisoryLockPractitioner(tx, pid); e != nil {
				return e
			}
		}

		overlap, e := s.patientHasOverlapTx(tx, r.PatientID, resolved.Start, resolved.End)
		if e != nil {
			return coreerrors.Internal(e.Error())
		}
		if overlap {
			return coreerrors.Conflict("Le patient a déjà un rendez-vous sur ce créneau")
		}

		var chosen uint
		found := false
		for _, pid := range cands {
			ok, e := s.isIntervalFullyAvailableTx(tx, pid, r.ServiceID, resolved.Start, resolved.End)
			if e != nil {
				return coreerrors.Internal(e.Error())
			}
			if ok {
				chosen = pid
				found = true
				break
			}
		}
		if !found {
			return coreerrors.Conflict("Créneau indisponible (conflit concurrent)")
		}

		now := time.Now().UTC()
		end := resolved.End
		prac := chosen
		appt := Appointment{
			PatientID:         r.PatientID,
			ServiceID:         r.ServiceID,
			ExpectedDoctorID:  &prac,
			AppointmentTypeID: resolved.AppointmentTypeID,
			ScheduledAt:       resolved.Start,
			ScheduledEndAt:    &end,
			Reason:            strings.TrimSpace(r.Reason),
			Status:            ApptScheduled,
			CreatedBy:         a.UserID,
			CreatedAt:         now,
			UpdatedAt:         now,
		}
		if idemKey != "" {
			k := idemKey
			appt.IdempotencyKey = &k
		}
		if e := tx.Create(&appt).Error; e != nil {
			msg := strings.ToLower(e.Error())
			if strings.Contains(msg, "ux_pq_appt_idempotency") || strings.Contains(msg, "idempotency") {
				// Race lost unique: re-read under same idempotency lock and return if same semantics
				prior, e2 := s.findIdempotentAppointmentTx(tx, idemKey, a.UserID)
				if e2 != nil {
					return e2
				}
				if prior != nil && sameBookingSemantics(*prior, r, resolved) {
					created = prior
					reused = true
					return nil
				}
				return coreerrors.Conflict("Clé d'idempotence déjà utilisée avec une autre requête")
			}
			return coreerrors.Internal(e.Error())
		}
		payload := bookingRequestFingerprint(r, resolved, &prac)
		if e := s.writeAppointmentHistory(tx, appt.ID, a.UserID, ApptHistCreated, "", ApptScheduled, r.Reason, payload); e != nil {
			return coreerrors.Internal(e.Error())
		}
		created = &appt
		return nil
	})
	if txErr != nil {
		return nil, false, txErr
	}
	return created, reused, nil
}

// mapLegacyCreateToBook maps CreateAppointmentRequest onto BookAppointmentRequest.
// Requires appointmentTypeId and/or scheduledEndAt (never invents duration).
func mapLegacyCreateToBook(r CreateAppointmentRequest) (BookAppointmentRequest, error) {
	book := BookAppointmentRequest{
		PatientID:         r.PatientID,
		ServiceID:         r.ServiceID,
		PractitionerID:    r.ExpectedDoctorID,
		AppointmentTypeID: r.AppointmentTypeID,
		StartAt:           r.ScheduledAt,
		Reason:            r.Reason,
	}
	if r.AppointmentTypeID == nil && r.ScheduledEndAt == nil {
		return book, coreerrors.BadRequest("appointmentTypeId ou scheduledEndAt requis pour la réservation")
	}
	if r.ScheduledEndAt != nil {
		start := r.ScheduledAt.UTC()
		end := r.ScheduledEndAt.UTC()
		if !end.After(start) {
			return book, coreerrors.BadRequest("scheduledEndAt doit être strictement après scheduledAt")
		}
		mins := int(end.Sub(start) / time.Minute)
		if mins <= 0 || !start.Add(time.Duration(mins)*time.Minute).Equal(end) {
			return book, coreerrors.BadRequest("scheduledEndAt doit correspondre à une durée entière en minutes")
		}
		book.DurationMinutes = &mins
	}
	return book, nil
}
