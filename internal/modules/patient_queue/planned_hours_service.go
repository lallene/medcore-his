package patient_queue

import (
	"math"
	"sort"
	"time"

	"github.com/lallene/medcore-his/backend/internal/core/period"
	"github.com/lallene/medcore-his/backend/internal/core/scheduling"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
)

// maxPlannedHoursDays borne la période (une année civile + marge).
const maxPlannedHoursDays = 366

// StaffPlannedHours : heures planifiées d'un membre du personnel sur la période.
type StaffPlannedHours struct {
	PractitionerID uint    `json:"practitionerId"`
	Name           string  `json:"name"`
	Hours          float64 `json:"hours"`
}

type plannedDay struct {
	Day   string
	Start time.Time
}

// PlannedHoursByStaff : heures planifiées de chaque membre du personnel sur
// [from,to), triées par heures décroissantes puis par nom.
func (s *Service) PlannedHoursByStaff(a Access, f period.Filter) ([]StaffPlannedHours, error) {
	_, byStaff, err := s.plannedMinutes(a, f)
	if err != nil {
		return nil, err
	}
	out := make([]StaffPlannedHours, 0, len(byStaff))
	ids := make([]uint, 0, len(byStaff))
	for id, perDay := range byStaff {
		total := 0.0
		for _, m := range perDay {
			total += m
		}
		if total <= 0 {
			continue
		}
		ids = append(ids, id)
		out = append(out, StaffPlannedHours{PractitionerID: id, Hours: roundHours(total)})
	}
	if len(ids) > 0 {
		var users []struct {
			ID   uint
			Name string
		}
		if err := s.db.Raw(`SELECT id, COALESCE(name,'') AS name FROM users WHERE id IN ?`, ids).
			Scan(&users).Error; err != nil {
			return nil, coreerrors.Internal(err.Error())
		}
		names := make(map[uint]string, len(users))
		for _, u := range users {
			names[u.ID] = u.Name
		}
		for i := range out {
			out[i].Name = names[out[i].PractitionerID]
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Hours != out[j].Hours {
			return out[i].Hours > out[j].Hours
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func roundHours(minutes float64) float64 { return math.Round(minutes/60*10) / 10 }

// plannedMinutes calcule, par membre du personnel, les minutes de présence
// planifiées de chaque jour civil de [from,to) : créneaux récurrents +
// disponibilités exceptionnelles − exceptions négatives (même règle que les
// disponibilités, sans les rendez-vous). Agrégat de service :
// schedule.read.service/manage.service (services affectés) ou read.all/manage.all ;
// schedule.read.own seul ne suffit pas.
func (s *Service) plannedMinutes(a Access, f period.Filter) ([]plannedDay, map[uint][]float64, error) {
	if !s.canReadAllSchedules(a) && !s.canManageAllSchedules(a) &&
		!a.Has("schedule.read.service") && !a.Has("schedule.manage.service") {
		return nil, nil, coreerrors.Forbidden("Lecture des heures planifiées non autorisée")
	}
	if f.From == "" || f.To == "" {
		return nil, nil, coreerrors.BadRequest("from et to sont obligatoires")
	}
	scope, err := s.scheduleScopeServiceIDs(a)
	if err != nil {
		return nil, nil, err
	}

	// Bornes civiles calculées par PostgreSQL dans le fuseau métier de la session.
	var days []plannedDay
	if err := s.db.Raw(`
		SELECT d::date::text AS day, d::date::timestamptz AS start
		FROM generate_series(?::date, ?::date, interval '1 day') AS d`, f.From, f.To).
		Scan(&days).Error; err != nil {
		return nil, nil, coreerrors.Internal(err.Error())
	}
	if len(days) < 2 {
		return days, map[uint][]float64{}, nil
	}
	if len(days)-1 > maxPlannedHoursDays {
		return nil, nil, coreerrors.BadRequest("période trop longue (366 jours maximum)")
	}
	rangeStart, rangeEnd := days[0].Start, days[len(days)-1].Start
	loc := scheduling.Location()

	schedQ := s.db.Where("active = true").
		Where("valid_from <= ? AND (valid_until IS NULL OR valid_until >= ?)",
			dateOnlyUTC(rangeEnd.In(loc)), dateOnlyUTC(rangeStart.In(loc)))
	exQ := s.db.Where("active = true AND cancelled_at IS NULL").
		Where("start_at < ? AND end_at > ?", rangeEnd, rangeStart)
	if f.ServiceID != nil {
		schedQ = schedQ.Where("service_id = ?", *f.ServiceID)
		exQ = exQ.Where("service_id = ?", *f.ServiceID)
	}
	if scope != nil {
		schedQ = schedQ.Where("service_id IN ?", scope)
		exQ = exQ.Where("service_id IN ?", scope)
	}
	var schedules []StaffWorkingSchedule
	if err := schedQ.Find(&schedules).Error; err != nil {
		return nil, nil, coreerrors.Internal(err.Error())
	}
	var exceptions []ScheduleException
	if err := exQ.Find(&exceptions).Error; err != nil {
		return nil, nil, coreerrors.Internal(err.Error())
	}

	type key struct{ practitioner, service uint }
	schedBy := map[key][]StaffWorkingSchedule{}
	for _, w := range schedules {
		k := key{w.PractitionerID, w.ServiceID}
		schedBy[k] = append(schedBy[k], w)
	}
	exBy := map[key][]ScheduleException{}
	for _, ex := range exceptions {
		k := key{ex.PractitionerID, ex.ServiceID}
		exBy[k] = append(exBy[k], ex)
	}
	keys := map[key]bool{}
	for k := range schedBy {
		keys[k] = true
	}
	for k := range exBy {
		keys[k] = true
	}

	// Projection élargie d'un jour de chaque côté (fuseaux), puis découpage exact.
	projFrom, projTo := rangeStart.AddDate(0, 0, -1), rangeEnd.AddDate(0, 0, 1)
	byStaff := map[uint][]float64{}
	for k := range keys {
		minutes, ok := byStaff[k.practitioner]
		if !ok {
			minutes = make([]float64, len(days)-1)
			byStaff[k.practitioner] = minutes
		}
		for _, iv := range workingIntervals(projFrom, projTo, loc, schedBy[k], exBy[k]) {
			for i := 0; i < len(minutes); i++ {
				start, end := days[i].Start, days[i+1].Start
				if iv.End.After(start) && iv.Start.Before(end) {
					lo, hi := maxTime(iv.Start, start), minTime(iv.End, end)
					minutes[i] += hi.Sub(lo).Minutes()
				}
			}
		}
	}
	return days, byStaff, nil
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
