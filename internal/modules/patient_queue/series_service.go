package patient_queue

import (
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Advisory lock namespaces for appointment series (LOT 23O-A / 23O-B).
//
//	230405 — series create idempotency (caller + key)
//	230406 — series lifecycle (cancel entire / cancel-future) idempotency + series-id serialize
//
// Must NOT reuse 230404 (occurrence lifecycle idempotency in lifecycle_service.go).
// Lock order for series create: create-idempotency → patient → practitioner.
// Lock order for series cancel: lifecycle-idempotency → series row FOR UPDATE → patient → practitioners ASC → appointments FOR UPDATE.
const (
	bookingLockNSSeriesCreateIdempotency = 230405
	bookingLockNSSeriesLifecycle         = 230406
)

// CreateAppointmentSeriesRequest — POST /api/appointment-series (PHI-safe; no reason/diagnosis/phone).
type CreateAppointmentSeriesRequest struct {
	PatientID         uint       `json:"patientId" binding:"required"`
	ServiceID         uint       `json:"serviceId" binding:"required"`
	PractitionerID    uint       `json:"practitionerId" binding:"required"`
	AppointmentTypeID *uint      `json:"appointmentTypeId"`
	Freq              string     `json:"freq" binding:"required"`
	IntervalWeeks     int        `json:"intervalWeeks" binding:"required"`
	ByWeekdays        []int      `json:"byWeekdays" binding:"required"`
	Count             *int       `json:"count"`
	Until             *time.Time `json:"until"`
	Timezone          string     `json:"timezone" binding:"required"`
	AnchorStartAt     time.Time  `json:"anchorStartAt" binding:"required"`
	IdempotencyKey    string     `json:"idempotencyKey"`
}

// Series occurrence classification for read APIs (LOT 23O-D) — derived, not persisted.
const (
	SeriesOccKindRule                 = "RULE"                  // matches current series rule
	SeriesOccKindExceptionRescheduled = "EXCEPTION_RESCHEDULED" // 23E single-occurrence reschedule
	SeriesOccKindExceptionCancelled   = "EXCEPTION_CANCELLED"   // cancelled occurrence (23E or series cancel)
	SeriesOccKindOperational          = "OPERATIONAL"           // ARRIVED / IN_PROGRESS / COMPLETED / NO_SHOW / …
)

// SeriesOccurrenceDTO — safe occurrence summary (no patient/PHI).
type SeriesOccurrenceDTO struct {
	ID             uint      `json:"id"`
	Index          int       `json:"index"`
	ScheduledAt    time.Time `json:"scheduledAt"`
	ScheduledEndAt time.Time `json:"scheduledEndAt"`
	Status         string    `json:"status"`
	PractitionerID uint      `json:"practitionerId"`
	// Kind distinguishes rule-aligned vs 23E exceptions vs operational/history (LOT 23O-D).
	Kind string `json:"kind"`
}

// SeriesOccurrencesResponse — GET /api/appointment-series/:id/occurrences (LOT 23O-D).
type SeriesOccurrencesResponse struct {
	SeriesID uint                  `json:"seriesId"`
	Status   string                `json:"status"`
	Version  int                   `json:"version"`
	Items    []SeriesOccurrenceDTO `json:"items"`
}

// AppointmentSeriesDTO — PHI-safe series response.
type AppointmentSeriesDTO struct {
	ID                uint                  `json:"id"`
	PatientID         uint                  `json:"patientId"`
	ServiceID         uint                  `json:"serviceId"`
	PractitionerID    uint                  `json:"practitionerId"`
	AppointmentTypeID *uint                 `json:"appointmentTypeId,omitempty"`
	Freq              string                `json:"freq"`
	IntervalWeeks     int                   `json:"intervalWeeks"`
	ByWeekdays        []int                 `json:"byWeekdays"`
	Count             *int                  `json:"count,omitempty"`
	Until             *time.Time            `json:"until,omitempty"`
	Timezone          string                `json:"timezone"`
	AnchorStartAt     time.Time             `json:"anchorStartAt"`
	DurationMinutes   int                   `json:"durationMinutes"`
	Status            string                `json:"status"`
	Version           int                   `json:"version"`
	CreatedAt         time.Time             `json:"createdAt"`
	UpdatedAt         time.Time             `json:"updatedAt"`
	Occurrences       []SeriesOccurrenceDTO `json:"occurrences"`
}

type seriesSemanticPayload struct {
	PatientID         uint       `json:"patientId"`
	ServiceID         uint       `json:"serviceId"`
	PractitionerID    uint       `json:"practitionerId"`
	AppointmentTypeID *uint      `json:"appointmentTypeId"`
	Freq              string     `json:"freq"`
	IntervalWeeks     int        `json:"intervalWeeks"`
	ByWeekdays        []int      `json:"byWeekdays"`
	Count             *int       `json:"count"`
	Until             *time.Time `json:"until"`
	Timezone          string     `json:"timezone"`
	AnchorStartAt     string     `json:"anchorStartAt"`
	DurationMinutes   int        `json:"durationMinutes"`
}

func seriesRequestFingerprint(r CreateAppointmentSeriesRequest, durationMinutes int, weekdays []int, typeID *uint) string {
	var until *time.Time
	if r.Until != nil {
		u := r.Until.UTC()
		until = &u
	}
	payload := seriesSemanticPayload{
		PatientID:         r.PatientID,
		ServiceID:         r.ServiceID,
		PractitionerID:    r.PractitionerID,
		AppointmentTypeID: typeID,
		Freq:              strings.TrimSpace(r.Freq),
		IntervalWeeks:     r.IntervalWeeks,
		ByWeekdays:        weekdays,
		Count:             r.Count,
		Until:             until,
		Timezone:          strings.TrimSpace(r.Timezone),
		AnchorStartAt:     r.AnchorStartAt.UTC().Format(time.RFC3339),
		DurationMinutes:   durationMinutes,
	}
	b, _ := json.Marshal(payload)
	return string(b)
}

func sameSeriesSemantics(prior AppointmentSeries, r CreateAppointmentSeriesRequest, durationMinutes int, weekdays []int, typeID *uint) bool {
	want := seriesRequestFingerprint(r, durationMinutes, weekdays, typeID)
	priorWeekdays, err := unmarshalByWeekdays(prior.ByWeekdays)
	if err != nil {
		return false
	}
	var until *time.Time
	if prior.Until != nil {
		u := prior.Until.UTC()
		until = &u
	}
	have := seriesRequestFingerprint(CreateAppointmentSeriesRequest{
		PatientID:         prior.PatientID,
		ServiceID:         prior.ServiceID,
		PractitionerID:    prior.PractitionerID,
		AppointmentTypeID: prior.AppointmentTypeID,
		Freq:              prior.Freq,
		IntervalWeeks:     prior.IntervalWeeks,
		ByWeekdays:        priorWeekdays,
		Count:             prior.Count,
		Until:             until,
		Timezone:          prior.Timezone,
		AnchorStartAt:     prior.AnchorStartAt,
	}, prior.DurationMinutes, priorWeekdays, prior.AppointmentTypeID)
	return want == have
}

func (s *Service) findIdempotentSeriesTx(tx *gorm.DB, key string, actor uint) (*AppointmentSeries, error) {
	var row AppointmentSeries
	err := tx.Where("idempotency_key = ? AND created_by = ?", key, actor).First(&row).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, coreerrors.Internal(err.Error())
	}
	return &row, nil
}

func (s *Service) advisoryLockSeriesIdempotency(tx *gorm.DB, caller uint, key string) error {
	h := fnv.New32a()
	_, _ = fmt.Fprintf(h, "series:%d:%s", caller, key)
	key2 := int32(h.Sum32())
	if err := tx.Exec(`SELECT pg_advisory_xact_lock(?, ?)`, bookingLockNSSeriesCreateIdempotency, key2).Error; err != nil {
		return coreerrors.Internal("verrou idempotence série: " + err.Error())
	}
	return nil
}

func (s *Service) advisoryLockSeriesLifecycle(tx *gorm.DB, seriesID, caller uint, op, key string) error {
	h := fnv.New32a()
	_, _ = fmt.Fprintf(h, "%s:%d:%d:%s", op, seriesID, caller, key)
	key2 := int32(h.Sum32())
	if err := tx.Exec(`SELECT pg_advisory_xact_lock(?, ?)`, bookingLockNSSeriesLifecycle, key2).Error; err != nil {
		return coreerrors.Internal("verrou cycle de vie série: " + err.Error())
	}
	return nil
}

// CreateAppointmentSeries atomically creates the series parent + all materialized appointments (LOT 23O-A).
// Returns (dto, reusedIdempotent, error).
func (s *Service) CreateAppointmentSeries(r CreateAppointmentSeriesRequest, a Access) (*AppointmentSeriesDTO, bool, error) {
	if !s.canBookAppointments(a) {
		return nil, false, coreerrors.Forbidden("Création de série non autorisée")
	}
	if a.UserID == 0 {
		return nil, false, coreerrors.Unauthorized("Utilisateur non authentifié")
	}
	if r.PatientID == 0 || r.ServiceID == 0 || r.PractitionerID == 0 {
		return nil, false, coreerrors.BadRequest("patient, service et practitionerId requis")
	}
	if r.AnchorStartAt.IsZero() {
		return nil, false, coreerrors.BadRequest("anchorStartAt requis")
	}
	if err := s.assertServiceExists(r.ServiceID); err != nil {
		return nil, false, err
	}
	if err := s.assertBookingServiceAccess(r.ServiceID, a); err != nil {
		return nil, false, err
	}
	if err := s.assertPatientExists(r.PatientID); err != nil {
		return nil, false, err
	}
	if err := s.assertPractitionerAssignedToService(r.PractitionerID, r.ServiceID); err != nil {
		return nil, false, err
	}

	freq := strings.TrimSpace(r.Freq)
	tz := strings.TrimSpace(r.Timezone)
	weekdays, err := normalizeByWeekdays(r.ByWeekdays)
	if err != nil {
		return nil, false, err
	}
	rule := RecurrenceRule{
		Freq:          freq,
		IntervalWeeks: r.IntervalWeeks,
		ByWeekdays:    weekdays,
		Count:         r.Count,
		Until:         r.Until,
		Timezone:      tz,
		AnchorStartAt: r.AnchorStartAt,
	}
	occurrences, err := ExpandWeeklyOccurrences(rule)
	if err != nil {
		return nil, false, err
	}

	// Duration from first occurrence start via existing booking authority (type required for series P0 consistency).
	if r.AppointmentTypeID == nil {
		return nil, false, coreerrors.BadRequest("appointmentTypeId requis pour une série")
	}
	resolved, err := s.resolveBookingDuration(r.ServiceID, r.AppointmentTypeID, nil, occurrences[0].StartAt)
	if err != nil {
		return nil, false, err
	}

	byJSON, err := marshalByWeekdays(weekdays)
	if err != nil {
		return nil, false, coreerrors.Internal(err.Error())
	}

	idemKey := strings.TrimSpace(r.IdempotencyKey)

	var out *AppointmentSeriesDTO
	var reused bool

	txErr := s.db.Transaction(func(tx *gorm.DB) error {
		if idemKey != "" {
			if e := s.advisoryLockSeriesIdempotency(tx, a.UserID, idemKey); e != nil {
				return e
			}
			prior, e := s.findIdempotentSeriesTx(tx, idemKey, a.UserID)
			if e != nil {
				return e
			}
			if prior != nil {
				if !sameSeriesSemantics(*prior, r, resolved.DurationMinutes, weekdays, resolved.AppointmentTypeID) {
					return coreerrors.Conflict("Clé d'idempotence déjà utilisée avec une autre requête")
				}
				dto, e2 := s.loadSeriesDTOTx(tx, prior.ID)
				if e2 != nil {
					return e2
				}
				out = dto
				reused = true
				return nil
			}
		}

		if e := s.advisoryLockPatient(tx, r.PatientID); e != nil {
			return e
		}
		if e := s.advisoryLockPractitioner(tx, r.PractitionerID); e != nil {
			return e
		}

		now := time.Now().UTC()
		var untilPtr *time.Time
		if r.Until != nil {
			u := r.Until.UTC()
			untilPtr = &u
		}
		var countPtr *int
		if r.Count != nil {
			c := *r.Count
			countPtr = &c
		}
		series := AppointmentSeries{
			PatientID:         r.PatientID,
			ServiceID:         r.ServiceID,
			PractitionerID:    r.PractitionerID,
			AppointmentTypeID: resolved.AppointmentTypeID,
			Freq:              SeriesFreqWeekly,
			IntervalWeeks:     r.IntervalWeeks,
			ByWeekdays:        byJSON,
			Count:             countPtr,
			Until:             untilPtr,
			Timezone:          tz,
			AnchorStartAt:     r.AnchorStartAt.UTC(),
			DurationMinutes:   resolved.DurationMinutes,
			Status:            SeriesStatusActive,
			CreatedBy:         a.UserID,
			Version:           1,
			CreatedAt:         now,
			UpdatedAt:         now,
		}
		if idemKey != "" {
			k := idemKey
			series.IdempotencyKey = &k
		}
		if e := tx.Create(&series).Error; e != nil {
			msg := strings.ToLower(e.Error())
			if idemKey != "" && (strings.Contains(msg, "ux_pq_series_idempotency") || strings.Contains(msg, "idempotency")) {
				prior, e2 := s.findIdempotentSeriesTx(tx, idemKey, a.UserID)
				if e2 != nil {
					return e2
				}
				if prior != nil && sameSeriesSemantics(*prior, r, resolved.DurationMinutes, weekdays, resolved.AppointmentTypeID) {
					dto, e3 := s.loadSeriesDTOTx(tx, prior.ID)
					if e3 != nil {
						return e3
					}
					out = dto
					reused = true
					return nil
				}
				return coreerrors.Conflict("Clé d'idempotence déjà utilisée avec une autre requête")
			}
			return coreerrors.Internal(e.Error())
		}

		prac := r.PractitionerID
		for _, occ := range occurrences {
			occResolved := bookingResolved{
				Start:             occ.StartAt,
				End:               occ.StartAt.Add(time.Duration(resolved.DurationMinutes) * time.Minute),
				DurationMinutes:   resolved.DurationMinutes,
				AppointmentTypeID: resolved.AppointmentTypeID,
			}
			idx := occ.Index
			sid := series.ID
			occKey := seriesOccurrenceIdempotencyKey(series.ID, idx)
			bookReq := BookAppointmentRequest{
				PatientID:         r.PatientID,
				ServiceID:         r.ServiceID,
				PractitionerID:    &prac,
				AppointmentTypeID: resolved.AppointmentTypeID,
				StartAt:           occ.StartAt,
				IdempotencyKey:    occKey,
			}
			_, _, e := s.bookAppointmentTx(tx, bookReq, a, occResolved, bookAppointmentTxOpts{
				LocksHeld:             true,
				SeriesID:              &sid,
				SeriesOccurrenceIndex: &idx,
			})
			if e != nil {
				return e
			}
		}

		dto, e := s.loadSeriesDTOTx(tx, series.ID)
		if e != nil {
			return e
		}
		out = dto
		return nil
	})
	if txErr != nil {
		return nil, false, txErr
	}
	return out, reused, nil
}

func (s *Service) loadSeriesDTOTx(tx *gorm.DB, seriesID uint) (*AppointmentSeriesDTO, error) {
	var series AppointmentSeries
	if err := tx.First(&series, seriesID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, coreerrors.NotFound("Série")
		}
		return nil, coreerrors.Internal(err.Error())
	}
	var appts []Appointment
	if err := tx.Where("series_id = ?", seriesID).
		Order("series_occurrence_index ASC").
		Find(&appts).Error; err != nil {
		return nil, coreerrors.Internal(err.Error())
	}
	return toSeriesDTO(series, appts)
}

func toSeriesDTO(series AppointmentSeries, appts []Appointment) (*AppointmentSeriesDTO, error) {
	weekdays, err := unmarshalByWeekdays(series.ByWeekdays)
	if err != nil {
		return nil, coreerrors.Internal("byWeekdays série invalide")
	}
	expected, err := buildSeriesExpectedStartsByIndex(&series, appts)
	if err != nil {
		// CANCELLED / truncated rules may fail expansion; still return occurrences without rule alignment.
		expected = map[int]time.Time{}
	}
	dto := &AppointmentSeriesDTO{
		ID:                series.ID,
		PatientID:         series.PatientID,
		ServiceID:         series.ServiceID,
		PractitionerID:    series.PractitionerID,
		AppointmentTypeID: series.AppointmentTypeID,
		Freq:              series.Freq,
		IntervalWeeks:     series.IntervalWeeks,
		ByWeekdays:        weekdays,
		Count:             series.Count,
		Until:             series.Until,
		Timezone:          series.Timezone,
		AnchorStartAt:     series.AnchorStartAt,
		DurationMinutes:   series.DurationMinutes,
		Status:            series.Status,
		Version:           series.Version,
		CreatedAt:         series.CreatedAt,
		UpdatedAt:         series.UpdatedAt,
		Occurrences:       make([]SeriesOccurrenceDTO, 0, len(appts)),
	}
	for _, ap := range appts {
		if ap.SeriesOccurrenceIndex == nil || ap.ScheduledEndAt == nil || ap.ExpectedDoctorID == nil {
			return nil, coreerrors.Internal("occurrence série incomplète")
		}
		dto.Occurrences = append(dto.Occurrences, SeriesOccurrenceDTO{
			ID:             ap.ID,
			Index:          *ap.SeriesOccurrenceIndex,
			ScheduledAt:    ap.ScheduledAt,
			ScheduledEndAt: *ap.ScheduledEndAt,
			Status:         ap.Status,
			PractitionerID: *ap.ExpectedDoctorID,
			Kind:           classifySeriesOccurrence(ap, &series, expected),
		})
	}
	return dto, nil
}

func classifySeriesOccurrence(ap Appointment, series *AppointmentSeries, expected map[int]time.Time) string {
	switch ap.Status {
	case ApptCancelled:
		return SeriesOccKindExceptionCancelled
	case ApptScheduled:
		if seriesOccurrenceDiverged(ap, series, expected) {
			return SeriesOccKindExceptionRescheduled
		}
		return SeriesOccKindRule
	default:
		return SeriesOccKindOperational
	}
}

// ListAppointmentSeriesOccurrences — GET /api/appointment-series/:id/occurrences (LOT 23O-D).
func (s *Service) ListAppointmentSeriesOccurrences(id uint, a Access) (*SeriesOccurrencesResponse, error) {
	dto, err := s.GetAppointmentSeries(id, a)
	if err != nil {
		return nil, err
	}
	return &SeriesOccurrencesResponse{
		SeriesID: dto.ID,
		Status:   dto.Status,
		Version:  dto.Version,
		Items:    dto.Occurrences,
	}, nil
}

// GetAppointmentSeries — GET /api/appointment-series/:id with schedule.read scope (anti-enumeration).
func (s *Service) GetAppointmentSeries(id uint, a Access) (*AppointmentSeriesDTO, error) {
	if id == 0 {
		return nil, coreerrors.BadRequest("id requis")
	}
	if !s.canReadAppointments(a) {
		return nil, coreerrors.NotFound("Série")
	}
	var series AppointmentSeries
	if err := s.db.First(&series, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, coreerrors.NotFound("Série")
		}
		return nil, coreerrors.Internal(err.Error())
	}
	if err := s.assertCanReadSeries(series, a); err != nil {
		return nil, err
	}
	var appts []Appointment
	if err := s.db.Where("series_id = ?", id).Order("series_occurrence_index ASC").Find(&appts).Error; err != nil {
		return nil, coreerrors.Internal(err.Error())
	}
	return toSeriesDTO(series, appts)
}

func (s *Service) assertCanReadSeries(series AppointmentSeries, a Access) error {
	if !s.canReadAppointments(a) {
		return coreerrors.NotFound("Série")
	}
	if s.canReadAllSchedules(a) {
		return nil
	}
	ownOnly := a.Has("schedule.read.own") && !a.Has("schedule.read.service")
	if ownOnly {
		if series.PractitionerID == a.UserID {
			return nil
		}
		return coreerrors.NotFound("Série")
	}
	if a.Has("schedule.read.service") {
		if err := s.assertScheduleServiceInScope(series.ServiceID, a); err != nil {
			if a.Has("schedule.read.own") && series.PractitionerID == a.UserID {
				return nil
			}
			return coreerrors.NotFound("Série")
		}
		return nil
	}
	if a.Has("schedule.read.own") && series.PractitionerID == a.UserID {
		return nil
	}
	return coreerrors.NotFound("Série")
}

// CancelAppointmentSeriesRequest — POST /api/appointment-series/:id/cancel (LOT 23O-B).
type CancelAppointmentSeriesRequest struct {
	ExpectedVersion int    `json:"expectedVersion" binding:"required"`
	Reason          string `json:"reason"`
	IdempotencyKey  string `json:"idempotencyKey"`
}

// CancelAppointmentSeriesFutureRequest — POST /api/appointment-series/:id/cancel-future.
// fromOccurrenceIndex is 1-based and inclusive. Optionally resolve fromAppointmentId instead.
type CancelAppointmentSeriesFutureRequest struct {
	ExpectedVersion     int    `json:"expectedVersion" binding:"required"`
	FromOccurrenceIndex *int   `json:"fromOccurrenceIndex"`
	FromAppointmentID   *uint  `json:"fromAppointmentId"`
	Reason              string `json:"reason"`
	IdempotencyKey      string `json:"idempotencyKey"`
}

const (
	seriesLifecycleOpCancelEntire = "SERIES_CANCEL"
	seriesLifecycleOpCancelFuture = "SERIES_CANCEL_FUTURE"
)

// CancelAppointmentSeries cancels every SCHEDULED occurrence and marks the series CANCELLED.
func (s *Service) CancelAppointmentSeries(id uint, r CancelAppointmentSeriesRequest, a Access) (*AppointmentSeriesDTO, error) {
	if !s.canCancelAppointments(a) {
		return nil, coreerrors.Forbidden("Annulation de série non autorisée")
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
			if e := s.advisoryLockSeriesLifecycle(tx, id, a.UserID, seriesLifecycleOpCancelEntire, idemKey); e != nil {
				return e
			}
		}
		series, e := s.lockSeriesForMutationTx(tx, id, a, r.ExpectedVersion, "appointment.cancel.all")
		if e != nil {
			return e
		}
		if series.Status == SeriesStatusCancelled {
			dto, e2 := s.loadSeriesDTOTx(tx, series.ID)
			if e2 != nil {
				return e2
			}
			out = dto
			return nil
		}
		if series.Status != SeriesStatusActive {
			return coreerrors.Conflict("Statut de série non annulable")
		}

		targets, e := s.loadSeriesScheduledAppointmentsTx(tx, series.ID, nil)
		if e != nil {
			return e
		}
		if e := s.cancelSeriesOccurrencesTx(tx, series, targets, reason, a); e != nil {
			return e
		}
		if e := s.markSeriesCancelledTx(tx, series); e != nil {
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

// CancelAppointmentSeriesFuture cancels SCHEDULED occurrences with index >= cutoff.
// Series becomes CANCELLED only when no SCHEDULED occurrences remain.
func (s *Service) CancelAppointmentSeriesFuture(id uint, r CancelAppointmentSeriesFutureRequest, a Access) (*AppointmentSeriesDTO, error) {
	if !s.canCancelAppointments(a) {
		return nil, coreerrors.Forbidden("Annulation de série non autorisée")
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
			if e := s.advisoryLockSeriesLifecycle(tx, id, a.UserID, seriesLifecycleOpCancelFuture, idemKey); e != nil {
				return e
			}
		}
		series, e := s.lockSeriesForMutationTx(tx, id, a, r.ExpectedVersion, "appointment.cancel.all")
		if e != nil {
			return e
		}
		if series.Status == SeriesStatusCancelled {
			return coreerrors.Conflict("Série déjà annulée")
		}
		if series.Status != SeriesStatusActive {
			return coreerrors.Conflict("Statut de série non annulable")
		}

		fromIdx, e := s.resolveCancelFutureCutoffTx(tx, series.ID, r)
		if e != nil {
			return e
		}
		targets, e := s.loadSeriesScheduledAppointmentsTx(tx, series.ID, &fromIdx)
		if e != nil {
			return e
		}
		if e := s.cancelSeriesOccurrencesTx(tx, series, targets, reason, a); e != nil {
			return e
		}

		var remaining int64
		if e := tx.Model(&Appointment{}).
			Where("series_id = ? AND status = ?", series.ID, ApptScheduled).
			Count(&remaining).Error; e != nil {
			return coreerrors.Internal(e.Error())
		}
		now := time.Now().UTC()
		updates := map[string]any{
			"version":    gorm.Expr("version + 1"),
			"updated_at": now,
		}
		if remaining == 0 {
			updates["status"] = SeriesStatusCancelled
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

func (s *Service) lockSeriesForMutationTx(tx *gorm.DB, id uint, a Access, expectedVersion int, allPerms ...string) (*AppointmentSeries, error) {
	var series AppointmentSeries
	if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&series, id).Error; e != nil {
		if e == gorm.ErrRecordNotFound {
			return nil, coreerrors.NotFound("Série")
		}
		return nil, coreerrors.Internal(e.Error())
	}
	if e := s.assertLifecycleServiceAccess(series.ServiceID, a, allPerms...); e != nil {
		var ae *coreerrors.AppError
		if errors.As(e, &ae) && ae.Status == 404 {
			return nil, coreerrors.NotFound("Série")
		}
		return nil, e
	}
	if series.Version != expectedVersion {
		return nil, coreerrors.Conflict("État de la série obsolète")
	}
	return &series, nil
}

func (s *Service) resolveCancelFutureCutoffTx(tx *gorm.DB, seriesID uint, r CancelAppointmentSeriesFutureRequest) (int, error) {
	hasIdx := r.FromOccurrenceIndex != nil
	hasAppt := r.FromAppointmentID != nil && *r.FromAppointmentID != 0
	if hasIdx == hasAppt {
		return 0, coreerrors.BadRequest("fromOccurrenceIndex XOR fromAppointmentId requis")
	}
	if hasIdx {
		if *r.FromOccurrenceIndex < 1 {
			return 0, coreerrors.BadRequest("fromOccurrenceIndex doit être >= 1")
		}
		return *r.FromOccurrenceIndex, nil
	}
	var appt Appointment
	if e := tx.Where("id = ? AND series_id = ?", *r.FromAppointmentID, seriesID).First(&appt).Error; e != nil {
		if e == gorm.ErrRecordNotFound {
			return 0, coreerrors.BadRequest("fromAppointmentId n'appartient pas à cette série")
		}
		return 0, coreerrors.Internal(e.Error())
	}
	if appt.SeriesOccurrenceIndex == nil || *appt.SeriesOccurrenceIndex < 1 {
		return 0, coreerrors.Internal("occurrence série sans index")
	}
	return *appt.SeriesOccurrenceIndex, nil
}

func (s *Service) loadSeriesScheduledAppointmentsTx(tx *gorm.DB, seriesID uint, fromIndex *int) ([]Appointment, error) {
	q := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("series_id = ? AND status = ?", seriesID, ApptScheduled)
	if fromIndex != nil {
		q = q.Where("series_occurrence_index >= ?", *fromIndex)
	}
	var appts []Appointment
	if e := q.Order("id ASC").Find(&appts).Error; e != nil {
		return nil, coreerrors.Internal(e.Error())
	}
	return appts, nil
}

func (s *Service) cancelSeriesOccurrencesTx(tx *gorm.DB, series *AppointmentSeries, targets []Appointment, reason string, a Access) error {
	if e := s.advisoryLockPatient(tx, series.PatientID); e != nil {
		return e
	}
	pracSet := map[uint]struct{}{series.PractitionerID: {}}
	for _, ap := range targets {
		if ap.ExpectedDoctorID != nil {
			pracSet[*ap.ExpectedDoctorID] = struct{}{}
		}
	}
	pracIDs := make([]uint, 0, len(pracSet))
	for id := range pracSet {
		pracIDs = append(pracIDs, id)
	}
	sort.Slice(pracIDs, func(i, j int) bool { return pracIDs[i] < pracIDs[j] })
	for _, pid := range pracIDs {
		if e := s.advisoryLockPractitioner(tx, pid); e != nil {
			return e
		}
	}

	cancelReq := CancelAppointmentRequest{Reason: reason}
	for i := range targets {
		ap := targets[i]
		_, e := s.cancelAppointmentTx(tx, ap.ID, cancelReq, a, cancelAppointmentTxOpts{
			LocksHeld:       true,
			SkipAccessCheck: true,
			Appointment:     &ap,
		})
		if e != nil {
			return e
		}
	}
	return nil
}

func (s *Service) markSeriesCancelledTx(tx *gorm.DB, series *AppointmentSeries) error {
	now := time.Now().UTC()
	res := tx.Model(&AppointmentSeries{}).
		Where("id = ? AND version = ? AND status = ?", series.ID, series.Version, SeriesStatusActive).
		Updates(map[string]any{
			"status":     SeriesStatusCancelled,
			"version":    gorm.Expr("version + 1"),
			"updated_at": now,
		})
	if res.Error != nil {
		return coreerrors.Internal(res.Error.Error())
	}
	if res.RowsAffected == 0 {
		return coreerrors.Conflict("État de la série obsolète")
	}
	return nil
}

// assertSeriesAllowsOccurrenceReschedule rejects reschedule when parent series is CANCELLED.
func (s *Service) assertSeriesAllowsOccurrenceReschedule(tx *gorm.DB, appt Appointment) error {
	if appt.SeriesID == nil {
		return nil
	}
	var st string
	if e := tx.Model(&AppointmentSeries{}).Select("status").Where("id = ?", *appt.SeriesID).Scan(&st).Error; e != nil {
		return coreerrors.Internal(e.Error())
	}
	if st == SeriesStatusCancelled {
		return coreerrors.Conflict("Reschedule interdit: série annulée")
	}
	return nil
}
