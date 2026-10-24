package patient_queue

import (
	"github.com/lallene/medcore-his/backend/internal/core/period"
	"gorm.io/gorm"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
)

// maxDailyStatsDays borne les séries journalières (une année civile + marge).
const maxDailyStatsDays = 366

// DailyQueueFlow : tickets arrivés et terminés d'un jour civil (mêmes règles
// que les KPI de période : checked_in_at, et updated_at des tickets terminés).
type DailyQueueFlow struct {
	Date      string `json:"date"`
	Arrived   int64  `json:"arrived"`
	Completed int64  `json:"completed"`
}

// DailyAppointments : rendez-vous prévus un jour civil, par statut actuel.
type DailyAppointments struct {
	Date       string `json:"date"`
	Scheduled  int64  `json:"scheduled"`
	Arrived    int64  `json:"arrived"`
	InProgress int64  `json:"inProgress"`
	Completed  int64  `json:"completed"`
	NoShow     int64  `json:"noShow"`
	Cancelled  int64  `json:"cancelled"`
}

// civilDays liste les jours civils de [from,to) dans le fuseau de la session.
func (s *Service) civilDays(f period.Filter) ([]string, error) {
	if f.From == "" || f.To == "" {
		return nil, coreerrors.BadRequest("from et to sont obligatoires")
	}
	var days []string
	if err := s.db.Raw(`SELECT d::date::text FROM generate_series(?::date, ?::date - 1, interval '1 day') AS d ORDER BY d`, f.From, f.To).
		Scan(&days).Error; err != nil {
		return nil, coreerrors.Internal(err.Error())
	}
	if len(days) > maxDailyStatsDays {
		return nil, coreerrors.BadRequest("période trop longue (366 jours maximum)")
	}
	return days, nil
}

// QueueDailyFlow : arrivées et sorties de la file par jour civil de [from,to).
func (s *Service) QueueDailyFlow(a Access, f period.Filter) ([]DailyQueueFlow, error) {
	if !s.has(a, "queue.reception.read") && !s.has(a, "queue.read.service") && !s.has(a, "queue.read.all") && !s.has(a, "*") {
		return nil, coreerrors.Forbidden("Lecture KPI refusée")
	}
	days, err := s.civilDays(f)
	if err != nil {
		return nil, err
	}
	type dayCount struct {
		Day string
		N   int64
	}
	countBy := func(dateColumn string, where func(*gorm.DB) *gorm.DB) (map[string]int64, error) {
		q := period.Filter{ServiceID: f.ServiceID}.Apply(s.db.Model(&Ticket{}), "", "service_id")
		q, err := s.applyServiceScope(q, a, "service_id")
		if err != nil {
			return nil, err
		}
		q = period.Filter{From: f.From, To: f.To}.Apply(where(q), dateColumn, "")
		var rows []dayCount
		if err := q.Select(dateColumn + "::date::text AS day, COUNT(*) AS n").Group("day").Scan(&rows).Error; err != nil {
			return nil, coreerrors.Internal(err.Error())
		}
		out := make(map[string]int64, len(rows))
		for _, r := range rows {
			out[r.Day] = r.N
		}
		return out, nil
	}
	arrived, err := countBy("checked_in_at", func(q *gorm.DB) *gorm.DB { return q.Where("checked_in_at IS NOT NULL") })
	if err != nil {
		return nil, err
	}
	completed, err := countBy("updated_at", func(q *gorm.DB) *gorm.DB { return q.Where("status = ?", StatusCompleted) })
	if err != nil {
		return nil, err
	}
	out := make([]DailyQueueFlow, len(days))
	for i, day := range days {
		out[i] = DailyQueueFlow{Date: day, Arrived: arrived[day], Completed: completed[day]}
	}
	return out, nil
}

// AppointmentDaily : rendez-vous prévus par jour civil de [from,to), ventilés
// par statut (mêmes regroupements que AppointmentStats).
func (s *Service) AppointmentDaily(a Access, f period.Filter) ([]DailyAppointments, error) {
	days, err := s.civilDays(f)
	if err != nil {
		return nil, err
	}
	q := f.Apply(s.db.Model(&Appointment{}), "scheduled_at", "service_id")
	q, err = s.applyServiceScope(q, a, "service_id")
	if err != nil {
		return nil, err
	}
	var rows []DailyAppointments
	if err := q.Select(`
		scheduled_at::date::text AS date,
		COUNT(*) FILTER (WHERE status = 'SCHEDULED') AS scheduled,
		COUNT(*) FILTER (WHERE status IN ('ARRIVED', 'CHECKED_IN')) AS arrived,
		COUNT(*) FILTER (WHERE status = 'IN_PROGRESS') AS in_progress,
		COUNT(*) FILTER (WHERE status = 'COMPLETED') AS completed,
		COUNT(*) FILTER (WHERE status = 'NO_SHOW') AS no_show,
		COUNT(*) FILTER (WHERE status = 'CANCELLED') AS cancelled`).
		Group("date").Scan(&rows).Error; err != nil {
		return nil, coreerrors.Internal(err.Error())
	}
	byDay := make(map[string]DailyAppointments, len(rows))
	for _, r := range rows {
		byDay[r.Date] = r
	}
	out := make([]DailyAppointments, len(days))
	for i, day := range days {
		row := byDay[day]
		row.Date = day
		out[i] = row
	}
	return out, nil
}
