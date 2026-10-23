package complete

import (
	"fmt"
	"sort"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/consultations"
	"github.com/lallene/medcore-his/backend/internal/modules/patient_queue"
)

// Agenda configuration over the year (schedule exceptions of every type, some cancelled),
// appointment series (weekly/biweekly follow-ups; partial or full cancellation; practitioner
// change from an occurrence), and consultations opened outside the queue (left as draft,
// or cancelled).

type exceptionPlan struct {
	doc        *Member
	typ        string
	start, end time.Time
	cancel     bool
}

type agendaPlanner struct{}

func (agendaPlanner) Name() string { return "Agenda (exceptions, séries)" }

func (agendaPlanner) Setup(w *World) error {
	g := w.RNG.Stream("agenda.exceptions")
	types := []struct {
		typ         string
		days, hours int
	}{{patient_queue.ExLeave, 10, 0}, {patient_queue.ExAbsence, 1, 0}, {patient_queue.ExMeeting, 0, 2}, {patient_queue.ExTraining, 2, 0}, {patient_queue.ExBlocked, 0, 4}, {patient_queue.ExOther, 0, 3}}
	var plans []exceptionPlan
	services := make([]string, 0, len(w.Clinic.byService))
	for s := range w.Clinic.byService {
		services = append(services, s)
	}
	sort.Strings(services) // deterministic draw order (Go map iteration is randomised)
	for _, svc := range services {
		for _, doc := range w.Clinic.byService[svc] {
			for k := 0; k < g.Between(2, 4); k++ {
				t := types[g.Intn(len(types))]
				day := w.Cfg.FirstDay().AddDate(0, 0, g.Between(10, w.Cfg.Days+30))
				start := time.Date(day.Year(), day.Month(), day.Day(), 8, 0, 0, 0, w.Cfg.Location)
				end := start.AddDate(0, 0, t.days).Add(time.Duration(t.hours) * time.Hour)
				if t.hours > 0 {
					start = start.Add(time.Duration(g.Between(1, 6)) * time.Hour)
					end = start.Add(time.Duration(t.hours) * time.Hour)
				}
				overlap := false
				for _, q := range plans {
					if q.doc == doc && start.Before(q.end.Add(24*time.Hour)) && q.start.Add(-24*time.Hour).Before(end) {
						overlap = true
					}
				}
				if !overlap {
					plans = append(plans, exceptionPlan{doc: doc, typ: t.typ, start: start, end: end, cancel: g.Chance(0.1)})
				}
			}
			if g.Chance(0.3) { // extra Saturday availability
				day := w.Cfg.FirstDay().AddDate(0, 0, g.Between(20, w.Cfg.Days))
				for day.Weekday() != time.Saturday {
					day = day.AddDate(0, 0, 1)
				}
				start := time.Date(day.Year(), day.Month(), day.Day(), 9, 0, 0, 0, w.Cfg.Location)
				plans = append(plans, exceptionPlan{doc: doc, typ: patient_queue.ExExtraAvailability, start: start, end: start.Add(4 * time.Hour)})
			}
		}
	}
	// Block the unavailability windows in the booking model so appointments avoid them.
	for _, p := range plans {
		if p.typ == patient_queue.ExExtraAvailability || p.cancel {
			continue
		}
		occ := w.Clinic.slots[p.doc.UserID]
		if occ == nil {
			occ = map[int64]bool{}
			w.Clinic.slots[p.doc.UserID] = occ
		}
		for t := p.start.Truncate(15 * time.Minute); t.Before(p.end); t = t.Add(15 * time.Minute) {
			occ[t.Unix()] = true
		}
	}
	for i := range plans {
		p := plans[i]
		lead := p.start.AddDate(0, 0, -g.Between(5, 30))
		if lead.Before(w.Cfg.FirstDay()) {
			lead = w.Cfg.FirstDay().Add(7 * time.Hour)
		}
		w.Sched.At(lead, "agenda.exception", func(ctx *StepContext) error {
			a := ctx.W.Staff.Admin.QueueAccess(ctx.W)
			ex, err := ctx.W.Svc.Queue.CreateScheduleException(patient_queue.CreateScheduleExceptionRequest{PractitionerID: p.doc.UserID, ServiceID: ctx.W.Org.ServiceID(p.doc.PrimaryService()), Type: p.typ, StartAt: p.start, EndAt: p.end, Reason: exceptionReason(p.typ)}, a)
			if err != nil {
				return err
			}
			if p.cancel {
				ctx.W.Sched.At(ctx.Sim.AddDate(0, 0, 2), "agenda.exception-annulee", func(ctx *StepContext) error {
					_, err := ctx.W.Svc.Queue.CancelScheduleException(ex.ID, a)
					return err
				})
			}
			return nil
		})
	}
	return nil
}

func exceptionReason(t string) string {
	return map[string]string{patient_queue.ExLeave: "Congés annuels", patient_queue.ExAbsence: "Absence (raison personnelle)", patient_queue.ExMeeting: "Staff médical",
		patient_queue.ExTraining: "Formation continue", patient_queue.ExBlocked: "Créneau bloqué (garde)", patient_queue.ExOther: "Autre indisponibilité", patient_queue.ExExtraAvailability: "Consultations supplémentaires du samedi"}[t]
}

func (agendaPlanner) PlanDay(w *World, d Day) {
	g := w.RNG.Stream("agenda.day", uint64(d.Index))
	// Appointment series for chronic follow-up (about one every five working days).
	if d.Kind != "WEEKEND" && g.Chance(0.2*w.Cfg.Scale()+0.02) {
		w.Sched.At(d.At(9, 30).Add(g.Minutes(0, 300)), "agenda.serie", func(ctx *StepContext) error { return createSeries(ctx, g.Stream("series", uint64(d.Index))) })
	}
	// Consultations opened outside the queue (teleconsultation note, administrative), left
	// as draft or cancelled.
	if g.Chance(0.35 * w.Cfg.Scale()) {
		w.Sched.At(d.At(11, 0).Add(g.Minutes(0, 360)), "consultations.hors-file", func(ctx *StepContext) error { return directConsultation(ctx, g.Stream("direct", uint64(d.Index))) })
	}
}

func createSeries(ctx *StepContext, g *RNG) error {
	w := ctx.W
	var p *Profile
	for try := 0; try < 30; try++ {
		c := w.People.Pool[g.Intn(len(w.People.Pool))]
		if len(c.Conditions) > 0 {
			p = c
			break
		}
	}
	if p == nil {
		return nil
	}
	svc := serviceFor(w, g, p, ctx.Sim)
	if svc == "URG" {
		svc = "GEN"
	}
	docs := w.Clinic.byService[svc]
	if len(docs) == 0 {
		return nil
	}
	doc := docs[g.Intn(len(docs))]
	typ := w.Ref.ApptTypes[visitServiceType[svc]]
	// Anchor on a free slot about one week ahead, on a working day of the practitioner.
	var anchor time.Time
	for k := 5; k < 15; k++ {
		day := ctx.Sim.AddDate(0, 0, k)
		if s, ok := freeSlot(w, g, doc, day, visitServiceType[svc]); ok {
			anchor = s
			break
		}
	}
	if anchor.IsZero() {
		return nil
	}
	count := g.Between(4, 10)
	interval := Pick(g, []int{1, 2, 2, 4})
	tid := typ.ID
	// Verify every future occurrence before reserving any of them.
	// The anchor has already been reserved by freeSlot.
	for k := 1; k < count; k++ {
		t := anchor.AddDate(0, 0, 7*interval*k)
		if !w.Clinic.slotAvailable(doc.UserID, t, typ.DefaultDurationMinutes) {
			w.Clinic.releaseSlot(doc.UserID, anchor, typ.DefaultDurationMinutes)
			return nil
		}
	}
	for k := 1; k < count; k++ {
		t := anchor.AddDate(0, 0, 7*interval*k)
		w.Clinic.reserveSlot(doc.UserID, t, typ.DefaultDurationMinutes)
	}
	agent := w.Staff.Admin
	dto, _, err := w.Svc.Queue.CreateAppointmentSeries(patient_queue.CreateAppointmentSeriesRequest{PatientID: p.PatientID, ServiceID: w.Org.ServiceID(svc), PractitionerID: doc.UserID, AppointmentTypeID: &tid,
		Freq: "WEEKLY", IntervalWeeks: interval, ByWeekdays: []int{int(anchor.Weekday())}, Count: &count, Timezone: w.Cfg.Location.String(), AnchorStartAt: anchor,
		IdempotencyKey: fmt.Sprintf("dc-series-%d-%d", p.PatientID, ctx.Sim.Unix())}, agent.QueueAccess(w))
	if err != nil {
		w.Clinic.releaseSlot(doc.UserID, anchor, typ.DefaultDurationMinutes)
		for k := 1; k < count; k++ {
			t := anchor.AddDate(0, 0, 7*interval*k)
			w.Clinic.releaseSlot(doc.UserID, t, typ.DefaultDurationMinutes)
		}
		return err
	}
	var occs []patient_queue.Appointment
	if err := w.DB.Where("series_id = ?", dto.ID).Order("series_occurrence_index").Find(&occs).Error; err != nil {
		return err
	}
	// Series outcome: full cancellation, cancellation of the future occurrences, or honoured.
	switch r := g.Float(); {
	case r < 0.06:
		w.Sched.At(ctx.Sim.AddDate(0, 0, g.Between(1, 4)), "agenda.serie-annulee", func(ctx *StepContext) error {
			cur, err := w.Svc.Queue.GetAppointmentSeries(dto.ID, agent.QueueAccess(w))
			if err != nil {
				return err
			}
			_, err = w.Svc.Queue.CancelAppointmentSeries(dto.ID, patient_queue.CancelAppointmentSeriesRequest{ExpectedVersion: cur.Version, Reason: "Suivi arrêté", IdempotencyKey: fmt.Sprintf("dc-series-cancel-%d", dto.ID)}, agent.QueueAccess(w))
			return err
		})
		return nil
	case r < 0.2 && len(occs) > 3:
		from := len(occs) / 2
		occs = occs[:from]
		fromIdx := from + 1
		w.Sched.At(occs[from-1].ScheduledAt.Add(3*time.Hour), "agenda.serie-fin-anticipee", func(ctx *StepContext) error {
			cur, err := w.Svc.Queue.GetAppointmentSeries(dto.ID, agent.QueueAccess(w))
			if err != nil {
				return err
			}
			_, err = w.Svc.Queue.CancelAppointmentSeriesFuture(dto.ID, patient_queue.CancelAppointmentSeriesFutureRequest{ExpectedVersion: cur.Version, FromOccurrenceIndex: &fromIdx, Reason: "Objectif atteint, fin du suivi rapproché", IdempotencyKey: fmt.Sprintf("dc-series-future-%d", dto.ID)}, agent.QueueAccess(w))
			return err
		})
	}
	for i := range occs {
		o := occs[i]
		w.Clinic.visits++
		v := &Visit{ID: w.Clinic.visits, P: p, Service: svc, Doctor: doc, Start: o.ScheduledAt, ApptID: o.ID, Priority: patient_queue.PriorityNormal, Outcome: "HONORED", g: g.Stream("occ", uint64(i))}
		if v.g.Chance(0.08) {
			w.Sched.At(o.ScheduledAt.Add(3*time.Hour), "agenda.absence", func(ctx *StepContext) error {
				_, err := w.Svc.Queue.MarkNoShow(o.ID, patient_queue.NoShowAppointmentRequest{Reason: "Absent à la séance", IdempotencyKey: fmt.Sprintf("dc-noshow-occ-%d", o.ID)}, agent.QueueAccess(w))
				return err
			})
			continue
		}
		if w.Clinic.isBusy(p, o.ScheduledAt.Add(-2*time.Hour), o.ScheduledAt.Add(6*time.Hour)) {
			continue
		}
		w.Clinic.reserve(p, v.ID, o.ScheduledAt.Add(-2*time.Hour), o.ScheduledAt.Add(6*time.Hour))
		v.scheduleArrival(w)
	}
	return nil
}

func directConsultation(ctx *StepContext, g *RNG) error {
	w := ctx.W
	p := pickPatient(w, g, ctx.Sim)
	if p == nil {
		return nil
	}
	docs := w.Clinic.byService["GEN"]
	doc := docs[g.Intn(len(docs))]
	sid := w.Org.ServiceID("GEN")
	c, err := w.Svc.Consult.CreateConsultation(consultations.CreateConsultationRequest{PatientID: p.PatientID, DoctorName: doc.Name, ServiceID: &sid,
		Diagnosis: "Avis sur dossier", Observations: "Consultation ouverte hors file d'attente (avis, téléconsultation)"}, doc.UserID)
	if err != nil {
		return err
	}
	if g.Chance(0.5) { // left as draft
		return nil
	}
	w.Sched.At(ctx.Sim.Add(time.Duration(g.Between(10, 600))*time.Minute), "consultations.annulation", func(ctx *StepContext) error {
		cur, err := w.Svc.Consult.GetConsultation(c.ID, doc.ConsultAccess())
		if err != nil {
			return err
		}
		_, err = w.Svc.Consult.UpdateStatus(c.ID, consultations.UpdateConsultationStatusRequest{Status: consultations.ConsultationStatusCancelled, ExpectedVersion: cur.Version, CancellationReason: "Ouverte par erreur"}, doc.UserID, doc.ConsultAccess())
		return err
	})
	return nil
}
