package patient_queue

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"gorm.io/gorm"
)

// Advisory lock namespace for series-level idempotency (LOT 23O-A).
// Lock order: series idempotency → patient → practitioner (fixed).
const bookingLockNSSeriesIdempotency = 230404

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

// SeriesOccurrenceDTO — safe occurrence summary (no patient/PHI).
type SeriesOccurrenceDTO struct {
	ID             uint      `json:"id"`
	Index          int       `json:"index"`
	ScheduledAt    time.Time `json:"scheduledAt"`
	ScheduledEndAt time.Time `json:"scheduledEndAt"`
	Status         string    `json:"status"`
	PractitionerID uint      `json:"practitionerId"`
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
	if err := tx.Exec(`SELECT pg_advisory_xact_lock(?, ?)`, bookingLockNSSeriesIdempotency, key2).Error; err != nil {
		return coreerrors.Internal("verrou idempotence série: " + err.Error())
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
		})
	}
	return dto, nil
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
