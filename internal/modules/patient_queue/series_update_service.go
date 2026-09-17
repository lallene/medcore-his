package patient_queue

import (
	"fmt"
	"sort"
	"strings"
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const seriesLifecycleOpUpdate = "SERIES_UPDATE"

// UpdateAppointmentSeriesRequest — PATCH /api/appointment-series/:id (LOT 23O-C).
// Updates future SCHEDULED occurrences from a cutoff; preserves historical/operational/exception rows.
// Immutable: patientId, serviceId, status, freq, createdBy, create idempotencyKey.
type UpdateAppointmentSeriesRequest struct {
	ExpectedVersion     int        `json:"expectedVersion" binding:"required"`
	FromOccurrenceIndex *int       `json:"fromOccurrenceIndex"`
	FromAppointmentID   *uint      `json:"fromAppointmentId"`
	PractitionerID      *uint      `json:"practitionerId"`
	AppointmentTypeID   *uint      `json:"appointmentTypeId"`
	IntervalWeeks       *int       `json:"intervalWeeks"`
	ByWeekdays          []int      `json:"byWeekdays"`
	Count               *int       `json:"count"`
	Until               *time.Time `json:"until"`
	Timezone            *string    `json:"timezone"`
	// AnchorStartAt is required when regenerating timing/recurrence: first occurrence of the updated segment.
	AnchorStartAt  *time.Time `json:"anchorStartAt"`
	Reason         string     `json:"reason"`
	IdempotencyKey string     `json:"idempotencyKey"`
}

type seriesUpdatePlan struct {
	PractitionerID    uint
	AppointmentTypeID *uint
	IntervalWeeks     int
	ByWeekdays        []int
	ByWeekdaysJSON    string
	Count             *int
	Until             *time.Time
	Timezone          string
	DurationMinutes   int
	AnchorStartAt     time.Time
	NewStarts         []time.Time
	Regenerate        bool
}

// UpdateAppointmentSeries modifies an ACTIVE series from a cutoff forward (LOT 23O-C).
func (s *Service) UpdateAppointmentSeries(id uint, r UpdateAppointmentSeriesRequest, a Access) (*AppointmentSeriesDTO, error) {
	if !s.canRescheduleAppointments(a) {
		return nil, coreerrors.Forbidden("Modification de série non autorisée")
	}
	if a.UserID == 0 {
		return nil, coreerrors.Unauthorized("Utilisateur non authentifié")
	}
	if id == 0 {
		return nil, coreerrors.BadRequest("id requis")
	}
	if r.ExpectedVersion < 1 {
		return nil, coreerrors.BadRequest("expectedVersion requis")
	}

	idemKey := strings.TrimSpace(r.IdempotencyKey)
	reason := strings.TrimSpace(r.Reason)
	var out *AppointmentSeriesDTO

	err := s.db.Transaction(func(tx *gorm.DB) error {
		if idemKey != "" {
			if e := s.advisoryLockSeriesLifecycle(tx, id, a.UserID, seriesLifecycleOpUpdate, idemKey); e != nil {
				return e
			}
		}
		series, e := s.lockSeriesForMutationTx(tx, id, a, r.ExpectedVersion,
			"appointment.reschedule.all", "schedule.manage.all")
		if e != nil {
			return e
		}
		if series.Status == SeriesStatusCancelled {
			return coreerrors.Conflict("Série déjà annulée")
		}
		if series.Status != SeriesStatusActive {
			return coreerrors.Conflict("Statut de série non modifiable")
		}

		fromIdx, e := s.resolveSeriesCutoffTx(tx, series.ID, r.FromOccurrenceIndex, r.FromAppointmentID)
		if e != nil {
			return e
		}
		plan, e := s.buildSeriesUpdatePlan(series, r, fromIdx)
		if e != nil {
			return e
		}

		allAppts, e := s.loadAllSeriesAppointmentsForUpdateTx(tx, series.ID)
		if e != nil {
			return e
		}
		oldByIndex, e := buildSeriesExpectedStartsByIndex(series, allAppts)
		if e != nil {
			return e
		}

		updatable := selectUpdatableSeriesOccurrences(allAppts, fromIdx, series, oldByIndex)

		if e := s.advisoryLockPatient(tx, series.PatientID); e != nil {
			return e
		}
		pracSet := map[uint]struct{}{series.PractitionerID: {}, plan.PractitionerID: {}}
		for _, ap := range updatable {
			if ap.ExpectedDoctorID != nil {
				pracSet[*ap.ExpectedDoctorID] = struct{}{}
			}
		}
		pracIDs := make([]uint, 0, len(pracSet))
		for pid := range pracSet {
			pracIDs = append(pracIDs, pid)
		}
		sort.Slice(pracIDs, func(i, j int) bool { return pracIDs[i] < pracIDs[j] })
		for _, pid := range pracIDs {
			if e := s.advisoryLockPractitioner(tx, pid); e != nil {
				return e
			}
		}

		if e := s.applySeriesUpdatePlanTx(tx, series, plan, fromIdx, updatable, allAppts, reason, a); e != nil {
			return e
		}
		if e := s.persistSeriesUpdateMetadataTx(tx, series, plan); e != nil {
			return e
		}
		dto, e := s.loadSeriesDTOTx(tx, series.ID)
		if e != nil {
			return e
		}
		out = dto
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) resolveSeriesCutoffTx(tx *gorm.DB, seriesID uint, fromIdx *int, fromAppt *uint) (int, error) {
	return s.resolveCancelFutureCutoffTx(tx, seriesID, CancelAppointmentSeriesFutureRequest{
		FromOccurrenceIndex: fromIdx,
		FromAppointmentID:   fromAppt,
	})
}

func expandSeriesOccurrences(series *AppointmentSeries) ([]SeriesOccurrence, error) {
	weekdays, err := unmarshalByWeekdays(series.ByWeekdays)
	if err != nil {
		return nil, coreerrors.Internal("byWeekdays série invalide")
	}
	return ExpandWeeklyOccurrences(RecurrenceRule{
		Freq: series.Freq, IntervalWeeks: series.IntervalWeeks, ByWeekdays: weekdays,
		Count: series.Count, Until: series.Until, Timezone: series.Timezone,
		AnchorStartAt: series.AnchorStartAt,
	})
}

// buildSeriesExpectedStartsByIndex maps series_occurrence_index → expected start under the current rule.
// AnchorStartAt aligns to the occurrence whose ScheduledAt equals the series anchor (else index 1).
func buildSeriesExpectedStartsByIndex(series *AppointmentSeries, all []Appointment) (map[int]time.Time, error) {
	occs, err := expandSeriesOccurrences(series)
	if err != nil {
		return nil, err
	}
	baseIdx := 1
	found := false
	for _, ap := range all {
		if ap.SeriesOccurrenceIndex == nil {
			continue
		}
		if ap.ScheduledAt.Equal(series.AnchorStartAt) {
			idx := *ap.SeriesOccurrenceIndex
			if !found || idx < baseIdx {
				baseIdx = idx
				found = true
			}
		}
	}
	out := make(map[int]time.Time, len(occs))
	for i, o := range occs {
		out[baseIdx+i] = o.StartAt
	}
	return out, nil
}

func (s *Service) buildSeriesUpdatePlan(series *AppointmentSeries, r UpdateAppointmentSeriesRequest, fromIdx int) (*seriesUpdatePlan, error) {
	plan := &seriesUpdatePlan{
		PractitionerID:    series.PractitionerID,
		AppointmentTypeID: series.AppointmentTypeID,
		IntervalWeeks:     series.IntervalWeeks,
		Timezone:          series.Timezone,
		DurationMinutes:   series.DurationMinutes,
		Count:             series.Count,
		Until:             series.Until,
		AnchorStartAt:     series.AnchorStartAt,
	}
	weekdays, err := unmarshalByWeekdays(series.ByWeekdays)
	if err != nil {
		return nil, coreerrors.Internal("byWeekdays série invalide")
	}
	plan.ByWeekdays = weekdays
	plan.ByWeekdaysJSON = series.ByWeekdays

	changedMeta := false
	if r.PractitionerID != nil {
		if *r.PractitionerID == 0 {
			return nil, coreerrors.BadRequest("practitionerId invalide")
		}
		if e := s.assertPractitionerAssignedToService(*r.PractitionerID, series.ServiceID); e != nil {
			return nil, e
		}
		plan.PractitionerID = *r.PractitionerID
		changedMeta = true
	}
	if r.AppointmentTypeID != nil {
		resolved, e := s.resolveBookingDuration(series.ServiceID, r.AppointmentTypeID, nil, series.AnchorStartAt)
		if e != nil {
			return nil, e
		}
		plan.AppointmentTypeID = resolved.AppointmentTypeID
		plan.DurationMinutes = resolved.DurationMinutes
		changedMeta = true
	}
	regen := false
	if r.IntervalWeeks != nil {
		if *r.IntervalWeeks < 1 {
			return nil, coreerrors.BadRequest("intervalWeeks doit être >= 1")
		}
		plan.IntervalWeeks = *r.IntervalWeeks
		regen = true
	}
	if r.ByWeekdays != nil {
		w, e := normalizeByWeekdays(r.ByWeekdays)
		if e != nil {
			return nil, e
		}
		js, e := marshalByWeekdays(w)
		if e != nil {
			return nil, coreerrors.Internal(e.Error())
		}
		plan.ByWeekdays = w
		plan.ByWeekdaysJSON = js
		regen = true
	}
	if r.Timezone != nil {
		tz := strings.TrimSpace(*r.Timezone)
		if tz == "" {
			return nil, coreerrors.BadRequest("timezone IANA invalide")
		}
		if _, e := time.LoadLocation(tz); e != nil {
			return nil, coreerrors.BadRequest("timezone IANA invalide")
		}
		plan.Timezone = tz
		regen = true
	}
	hasCount := r.Count != nil
	hasUntil := r.Until != nil
	if hasCount && hasUntil {
		return nil, coreerrors.BadRequest("count XOR until requis (exactement l'un des deux)")
	}
	if hasCount || hasUntil {
		regen = true
		if hasCount {
			c := *r.Count
			plan.Count = &c
			plan.Until = nil
		} else {
			u := r.Until.UTC()
			plan.Until = &u
			plan.Count = nil
		}
	}
	if r.AnchorStartAt != nil {
		if r.AnchorStartAt.IsZero() {
			return nil, coreerrors.BadRequest("anchorStartAt invalide")
		}
		regen = true
	}

	if !changedMeta && !regen {
		return nil, coreerrors.BadRequest("aucune modification de série fournie")
	}
	plan.Regenerate = regen

	if !regen {
		return plan, nil
	}

	if r.AnchorStartAt == nil || r.AnchorStartAt.IsZero() {
		return nil, coreerrors.BadRequest("anchorStartAt requis pour une régénération de récurrence/horaire")
	}
	plan.AnchorStartAt = r.AnchorStartAt.UTC()

	if !hasCount && !hasUntil {
		if series.Count != nil && *series.Count >= fromIdx {
			rem := *series.Count - fromIdx + 1
			plan.Count = &rem
			plan.Until = nil
		} else if series.Until != nil {
			u := series.Until.UTC()
			plan.Until = &u
			plan.Count = nil
		} else {
			return nil, coreerrors.BadRequest("count ou until requis pour la régénération")
		}
	}

	occs, e := ExpandWeeklyOccurrences(RecurrenceRule{
		Freq: SeriesFreqWeekly, IntervalWeeks: plan.IntervalWeeks, ByWeekdays: plan.ByWeekdays,
		Count: plan.Count, Until: plan.Until, Timezone: plan.Timezone,
		AnchorStartAt: plan.AnchorStartAt,
	})
	if e != nil {
		return nil, e
	}
	if len(occs) == 0 {
		return nil, coreerrors.BadRequest("aucune occurrence générée")
	}
	plan.NewStarts = make([]time.Time, len(occs))
	for i, o := range occs {
		plan.NewStarts[i] = o.StartAt
	}
	resolved, e := s.resolveBookingDuration(series.ServiceID, plan.AppointmentTypeID, nil, plan.NewStarts[0])
	if e != nil {
		return nil, e
	}
	plan.AppointmentTypeID = resolved.AppointmentTypeID
	plan.DurationMinutes = resolved.DurationMinutes
	return plan, nil
}

func (s *Service) loadAllSeriesAppointmentsForUpdateTx(tx *gorm.DB, seriesID uint) ([]Appointment, error) {
	var appts []Appointment
	if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("series_id = ?", seriesID).
		Order("series_occurrence_index ASC, id ASC").
		Find(&appts).Error; e != nil {
		return nil, coreerrors.Internal(e.Error())
	}
	return appts, nil
}

func selectUpdatableSeriesOccurrences(
	all []Appointment,
	fromIdx int,
	series *AppointmentSeries,
	oldByIndex map[int]time.Time,
) []Appointment {
	var out []Appointment
	for _, ap := range all {
		if ap.SeriesOccurrenceIndex == nil {
			continue
		}
		idx := *ap.SeriesOccurrenceIndex
		if idx < fromIdx {
			continue
		}
		if ap.Status != ApptScheduled {
			continue
		}
		if seriesOccurrenceDiverged(ap, series, oldByIndex) {
			continue
		}
		out = append(out, ap)
	}
	sort.Slice(out, func(i, j int) bool {
		return *out[i].SeriesOccurrenceIndex < *out[j].SeriesOccurrenceIndex
	})
	return out
}

func seriesOccurrenceDiverged(ap Appointment, series *AppointmentSeries, oldByIndex map[int]time.Time) bool {
	if ap.SeriesOccurrenceIndex == nil {
		return true
	}
	idx := *ap.SeriesOccurrenceIndex
	oldStart, ok := oldByIndex[idx]
	if !ok {
		return true
	}
	if !ap.ScheduledAt.Equal(oldStart) {
		return true
	}
	if ap.ExpectedDoctorID == nil || *ap.ExpectedDoctorID != series.PractitionerID {
		return true
	}
	if !ptrEqualUint(ap.AppointmentTypeID, series.AppointmentTypeID) {
		return true
	}
	if ap.ScheduledEndAt != nil {
		wantEnd := oldStart.Add(time.Duration(series.DurationMinutes) * time.Minute)
		if !ap.ScheduledEndAt.Equal(wantEnd) {
			return true
		}
	}
	return false
}

func (s *Service) applySeriesUpdatePlanTx(
	tx *gorm.DB,
	series *AppointmentSeries,
	plan *seriesUpdatePlan,
	fromIdx int,
	updatable []Appointment,
	all []Appointment,
	reason string,
	a Access,
) error {
	prac := plan.PractitionerID
	if !plan.Regenerate {
		for i := range updatable {
			ap := updatable[i]
			if e := s.rescheduleSeriesOccurrenceInPlaceTx(tx, &ap, ap.ScheduledAt, plan, prac, reason, a); e != nil {
				return e
			}
		}
		return nil
	}

	occupied := map[int]Appointment{}
	maxIdx := 0
	for _, ap := range all {
		if ap.SeriesOccurrenceIndex == nil {
			continue
		}
		idx := *ap.SeriesOccurrenceIndex
		occupied[idx] = ap
		if idx > maxIdx {
			maxIdx = idx
		}
	}

	updatableByIdx := map[int]*Appointment{}
	unused := map[uint]*Appointment{}
	for i := range updatable {
		ap := &updatable[i]
		updatableByIdx[*ap.SeriesOccurrenceIndex] = ap
		unused[ap.ID] = ap
	}

	for k, start := range plan.NewStarts {
		desiredIdx := fromIdx + k
		var target *Appointment
		targetIdx := desiredIdx

		if ap, ok := updatableByIdx[desiredIdx]; ok && unused[ap.ID] != nil {
			target = ap
			targetIdx = desiredIdx
		} else if _, taken := occupied[desiredIdx]; taken {
			// Index held by cancelled/exception/non-updatable — allocate a fresh index.
			targetIdx = maxIdx + 1
			maxIdx = targetIdx
			for _, ap := range unused {
				target = ap
				break
			}
		} else {
			// Free index: reuse any unused updatable row, else create.
			for _, ap := range unused {
				target = ap
				break
			}
			targetIdx = desiredIdx
		}

		if target != nil {
			delete(unused, target.ID)
			if target.SeriesOccurrenceIndex != nil {
				oldIdx := *target.SeriesOccurrenceIndex
				if oldIdx != targetIdx {
					delete(occupied, oldIdx)
				}
			}
			idx := targetIdx
			target.SeriesOccurrenceIndex = &idx
			if e := s.rescheduleSeriesOccurrenceInPlaceTx(tx, target, start, plan, prac, reason, a); e != nil {
				return e
			}
			occupied[targetIdx] = *target
			if targetIdx > maxIdx {
				maxIdx = targetIdx
			}
			continue
		}

		idx := targetIdx
		if _, taken := occupied[idx]; taken {
			idx = maxIdx + 1
			maxIdx = idx
		}
		sid := series.ID
		end := start.Add(time.Duration(plan.DurationMinutes) * time.Minute)
		occKey := fmt.Sprintf("series:%d:v%d:occ:%d", series.ID, series.Version+1, idx)
		bookReq := BookAppointmentRequest{
			PatientID: series.PatientID, ServiceID: series.ServiceID,
			PractitionerID: &prac, AppointmentTypeID: plan.AppointmentTypeID,
			StartAt: start, IdempotencyKey: occKey,
		}
		resolved := bookingResolved{
			Start: start, End: end, DurationMinutes: plan.DurationMinutes,
			AppointmentTypeID: plan.AppointmentTypeID,
		}
		_, _, e := s.bookAppointmentTx(tx, bookReq, a, resolved, bookAppointmentTxOpts{
			LocksHeld: true, SeriesID: &sid, SeriesOccurrenceIndex: &idx,
		})
		if e != nil {
			return e
		}
		occupied[idx] = Appointment{}
		if idx > maxIdx {
			maxIdx = idx
		}
	}

	for _, ap := range unused {
		_, e := s.cancelAppointmentTx(tx, ap.ID, CancelAppointmentRequest{Reason: reason}, a, cancelAppointmentTxOpts{
			LocksHeld: true, SkipAccessCheck: true, Appointment: ap,
		})
		if e != nil {
			return e
		}
	}
	return nil
}

func (s *Service) rescheduleSeriesOccurrenceInPlaceTx(
	tx *gorm.DB,
	ap *Appointment,
	start time.Time,
	plan *seriesUpdatePlan,
	prac uint,
	reason string,
	a Access,
) error {
	if e := assertNoActiveQueueLink(*ap); e != nil {
		return e
	}
	if e := canTransitionAppointment(ap.Status, LifecycleOpReschedule); e != nil {
		return e
	}
	end := start.Add(time.Duration(plan.DurationMinutes) * time.Minute)
	overlap, e := s.patientHasOverlapTx(tx, ap.PatientID, start, end, ap.ID)
	if e != nil {
		return coreerrors.Internal(e.Error())
	}
	if overlap {
		return coreerrors.Conflict("Le patient a déjà un rendez-vous sur ce créneau")
	}
	ok, e := s.isIntervalFullyAvailableTx(tx, prac, ap.ServiceID, start, end, ap.ID)
	if e != nil {
		return coreerrors.Internal(e.Error())
	}
	if !ok {
		return coreerrors.Conflict("Créneau indisponible pour ce praticien")
	}

	oldSnap := snapshotAppointment(*ap)
	oldScheduledAt := ap.ScheduledAt
	now := time.Now().UTC()
	ap.ExpectedDoctorID = &prac
	ap.AppointmentTypeID = plan.AppointmentTypeID
	ap.ScheduledAt = start
	ap.ScheduledEndAt = &end
	ap.UpdatedAt = now
	if e := tx.Save(ap).Error; e != nil {
		return coreerrors.Internal(e.Error())
	}
	newSnap := snapshotAppointment(*ap)
	payload := marshalLifecyclePayload(lifecycleHistoryPayload{
		Old: &oldSnap, New: &newSnap,
	})
	if e := s.writeAppointmentHistory(tx, ap.ID, a.UserID, ApptHistRescheduled, ApptScheduled, ApptScheduled, reason, payload); e != nil {
		return coreerrors.Internal(e.Error())
	}
	return s.applyRescheduleNotificationIntentsTx(tx, oldScheduledAt, *ap, now)
}

func (s *Service) persistSeriesUpdateMetadataTx(tx *gorm.DB, series *AppointmentSeries, plan *seriesUpdatePlan) error {
	now := time.Now().UTC()
	updates := map[string]any{
		"practitioner_id":     plan.PractitionerID,
		"appointment_type_id": plan.AppointmentTypeID,
		"interval_weeks":      plan.IntervalWeeks,
		"by_weekdays":         plan.ByWeekdaysJSON,
		"timezone":            plan.Timezone,
		"duration_minutes":    plan.DurationMinutes,
		"version":             gorm.Expr("version + 1"),
		"updated_at":          now,
	}
	if plan.Regenerate {
		updates["anchor_start_at"] = plan.AnchorStartAt
		updates["count"] = plan.Count
		updates["until"] = plan.Until
	}
	res := tx.Model(&AppointmentSeries{}).
		Where("id = ? AND version = ? AND status = ?", series.ID, series.Version, SeriesStatusActive).
		Updates(updates)
	if res.Error != nil {
		return coreerrors.Internal(res.Error.Error())
	}
	if res.RowsAffected == 0 {
		return coreerrors.Conflict("État de la série obsolète")
	}
	return nil
}
