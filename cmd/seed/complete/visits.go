package complete

import (
	"fmt"
	"sort"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/patient_queue"
)

// Outpatient and emergency journeys through the real agenda / queue services:
//
//	booking (reception) → [cancel | reschedule (+ practitioner change) | no-show]
//	→ check-in (EARLY/ON_TIME/LATE) → triage (vital signs) → doctor take → consultation
//	→ completion (disposition) → follow-ups (orders, pharmacy, billing, insurance)
//
// Walk-ins start at check-in. Steps after the horizon are never executed, which leaves the
// anchor day with patients waiting, in triage and in consultation.

// visitServices are the services that receive outpatients, with their main appointment type.
var visitServiceType = map[string]string{
	"URG": "URG-CONSULT", "GEN": "CONSULT", "PED": "PED-SUIVI", "CARD": "CARD-SUIVI", "DIAB": "DIAB-SUIVI",
	"NEURO": "CONSULT", "RHUM": "CONSULT", "ORL": "CONSULT", "GYNE": "CONSULT", "MAT": "CPN", "CHIR": "CHIR-PREOP",
}

// Visit is the state of one patient journey.
type Visit struct {
	ID          int
	P           *Profile
	Service     string
	Doctor      *Member
	Walkin      bool
	Start       time.Time // appointment start (or arrival for walk-ins)
	ApptID      uint
	TicketID    uint
	ConsultID   uint
	Priority    string
	Disposition string
	Outcome     string // HONORED | CANCELLED | NO_SHOW | LEFT
	Reschedule  bool
	Hospitalize bool
	g           *RNG
}

type clinicState struct {
	visits    int
	busy      map[uint][]span // reserved visit intervals per patient (one active visit at a time)
	workdays  map[uint]map[time.Weekday]bool
	slots     map[uint]map[int64]bool
	byService map[string][]*Member
}

type visitPlanner struct{}

func (visitPlanner) Name() string { return "Agenda / file / consultations" }

func (visitPlanner) Setup(w *World) error {
	c := w.Clinic
	c.busy = map[uint][]span{}
	c.slots = map[uint]map[int64]bool{}
	c.byService = map[string][]*Member{}
	for _, m := range w.Staff.With(func(m *Member) bool { return len(m.Specialties) > 0 }) {
		if s := m.PrimaryService(); s != "" && s != "ADMIN" {
			c.byService[s] = append(c.byService[s], m)
		}
	}
	return nil
}

// Reference daily volumes at scale 1 (1500 patients) on an ordinary weekday.
const (
	refBookingsPerDay = 13.5
	refWalkinsPerDay  = 1.9
)

func (visitPlanner) PlanDay(w *World, d Day) {
	g := w.RNG.Stream("visits.day", uint64(d.Index))
	// 1. Bookings made today (appointments 0-21 days ahead; extra bootstrap on day 0).
	n := volume(g, refBookingsPerDay, w.Cfg, d)
	if d.Index == 0 {
		n += int(refBookingsPerDay * 6 * w.Cfg.Scale())
	}
	for i := 0; i < n; i++ {
		lead := g.Weighted([]float64{6, 10, 10, 9, 8, 7, 6, 6, 5, 5, 4, 4, 3, 3, 3, 2, 2, 2, 2, 1, 1, 1})
		if d.Index == 0 {
			lead = g.Between(0, 20)
		}
		target := w.Cfg.FirstDay().AddDate(0, 0, d.Index+lead)
		if target.Sub(w.Cfg.Anchor) > 45*24*time.Hour {
			continue
		}
		bookAt := d.At(8, 0).Add(g.Minutes(0, 9*60))
		planBooking(w, g.Stream("booking", uint64(i)), bookAt, target)
	}
	// Extra bookings for the anchor day during the previous ten days (busy "today" agenda).
	if !d.IsToday && d.Index >= w.Cfg.Days-10 {
		for i := 0; i < volume(g, 3, w.Cfg, Day{Load: 1}); i++ {
			planBooking(w, g.Stream("booking-today", uint64(i)), d.At(9, 0).Add(g.Minutes(0, 8*60)), w.Cfg.Anchor)
		}
	}
	// "Now" pulse: on the anchor day, emergency arrivals during the last two and a half hours
	// before the present instant (24/7 service), plus same-day emergency appointments within
	// opening hours, so that waiting rooms and consultations are in progress whatever the
	// time of the run.
	if d.IsToday {
		for i := 0; i < 8; i++ {
			at := w.Horizon.Add(-time.Duration(15+i*18+g.Between(0, 10)) * time.Minute)
			if at.Before(d.Date) {
				continue
			}
			if p := pickPatient(w, g, at); p != nil {
				planWalkin(w, g.Stream("now-walkin", uint64(i)), p, "URG", at)
			}
		}
		for i := 0; i < 4; i++ {
			start := w.Horizon.Add(-time.Duration(40+i*25) * time.Minute).Truncate(15 * time.Minute)
			if start.Hour() < 7 || start.Hour() >= 18 || start.Before(d.Date) {
				continue
			}
			planSameDayAppointment(w, g.Stream("now-appt", uint64(i)), d, start)
		}
	}
	// 2. Walk-ins (emergencies mostly, 24/7 with a day peak), incl. first visit of new patients.
	walkins := volume(g, refWalkinsPerDay, w.Cfg, Day{Load: max(d.Load, 0.6), Index: d.Index})
	for i := 0; i < walkins; i++ {
		hour := Pick(g, []int{0, 3, 7, 8, 9, 9, 10, 10, 11, 11, 12, 14, 15, 16, 17, 18, 19, 20, 21, 22})
		at := d.At(hour, 0).Add(g.Minutes(0, 59))
		p := pickPatient(w, g, at)
		if p == nil {
			continue
		}
		svc := "URG"
		if hour >= 8 && hour <= 16 && g.Chance(0.25) {
			svc = "GEN"
		}
		planWalkin(w, g.Stream("walkin", uint64(i)), p, svc, at)
	}
	for _, p := range w.People.NewOn[d.Index] {
		if g.Chance(0.45) {
			at := d.At(9, 0).Add(g.Minutes(0, 8*60))
			p := p
			// The registration step runs first on the same day (07:00-16:00): retry later.
			w.Sched.At(at.Add(30*time.Minute), "visites.premier-passage", func(ctx *StepContext) error {
				if !p.Registered {
					return nil
				}
				planWalkin(ctx.W, ctx.W.RNG.Stream("first-visit", uint64(p.Index)), p, serviceFor(ctx.W, g, p, ctx.Sim), ctx.Sim.Add(5*time.Minute))
				return nil
			})
		}
	}
}

func serviceFor(w *World, g *RNG, p *Profile, at time.Time) string {
	age := p.AgeAt(at)
	switch {
	case age < 15 && g.Chance(0.8):
		return "PED"
	case p.Has("GROSSESSE") && g.Chance(0.75):
		return "MAT"
	case p.Has("CARDIO") && g.Chance(0.6), p.Has("HTA") && g.Chance(0.4):
		return "CARD"
	case p.Has("DIABETE") && g.Chance(0.6):
		return "DIAB"
	case p.Has("ARTHROSE") && g.Chance(0.5):
		return "RHUM"
	case p.Has("MIGRAINE") && g.Chance(0.5):
		return "NEURO"
	}
	return Pick(g, []string{"GEN", "GEN", "GEN", "GEN", "GEN", "ORL", "GYNE", "CHIR", "NEURO", "RHUM", "CARD"})
}

// pickPatient draws a registered patient without an ongoing visit (weighted by propensity).
func pickPatient(w *World, g *RNG, at time.Time) *Profile {
	pool := w.People.Pool
	if len(pool) == 0 {
		return nil
	}
	for try := 0; try < 12; try++ {
		p := pool[g.Intn(len(pool))]
		if g.Float()*4 > p.Weight+0.6 {
			continue
		}
		if w.Clinic.isBusy(p, at, at.Add(4*time.Hour)) {
			continue
		}
		return p
	}
	return nil
}

type span struct {
	from, to time.Time
	visit    int
}

func (c *clinicState) isBusy(p *Profile, from, to time.Time) bool {
	for _, s := range c.busy[p.PatientID] {
		if from.Before(s.to) && s.from.Before(to) {
			return true
		}
	}
	return false
}

func (c *clinicState) reserve(p *Profile, visit int, from, to time.Time) {
	c.busy[p.PatientID] = append(c.busy[p.PatientID], span{from, to, visit})
}

func (c *clinicState) release(p *Profile, visit int) {
	kept := c.busy[p.PatientID][:0]
	for _, s := range c.busy[p.PatientID] {
		if s.visit != visit {
			kept = append(kept, s)
		}
	}
	c.busy[p.PatientID] = kept
}

func (c *clinicState) slotAvailable(practitionerID uint, start time.Time, durationMinutes int) bool {
	if durationMinutes <= 0 {
		durationMinutes = 30
	}
	occupied := c.slots[practitionerID]
	for m := 0; m < durationMinutes; m += 15 {
		if occupied[start.Add(time.Duration(m)*time.Minute).Unix()] {
			return false
		}
	}
	return true
}

func (c *clinicState) reserveSlot(practitionerID uint, start time.Time, durationMinutes int) {
	if durationMinutes <= 0 {
		durationMinutes = 30
	}
	occupied := c.slots[practitionerID]
	if occupied == nil {
		occupied = map[int64]bool{}
		c.slots[practitionerID] = occupied
	}
	for m := 0; m < durationMinutes; m += 15 {
		occupied[start.Add(time.Duration(m)*time.Minute).Unix()] = true
	}
}

func (c *clinicState) releaseSlot(practitionerID uint, start time.Time, durationMinutes int) {
	if durationMinutes <= 0 {
		durationMinutes = 30
	}
	occupied := c.slots[practitionerID]
	if occupied == nil {
		return
	}
	for m := 0; m < durationMinutes; m += 15 {
		delete(occupied, start.Add(time.Duration(m)*time.Minute).Unix())
	}
}

// ---------------------------------------------------------------------------------------
// Booking.

func planBooking(w *World, g *RNG, bookAt, target time.Time) {
	p := pickPatient(w, g, target)
	if p == nil {
		return
	}
	svc := serviceFor(w, g, p, target)
	if target.Weekday() == time.Sunday && svc != "URG" {
		target = target.AddDate(0, 0, 1)
	}
	doctors := w.Clinic.byService[svc]
	if len(doctors) == 0 {
		svc, doctors = "GEN", w.Clinic.byService["GEN"]
	}
	doc := doctors[g.Intn(len(doctors))]
	start, ok := freeSlot(w, g, doc, target, visitServiceType[svc])
	if !ok {
		return
	}
	if !bookAt.Before(start) {
		bookAt = start.Add(-time.Duration(g.Between(30, 180)) * time.Minute)
	}
	if w.Clinic.isBusy(p, start.Add(-2*time.Hour), start.Add(6*time.Hour)) {
		return
	}
	w.Clinic.visits++
	v := &Visit{ID: w.Clinic.visits, P: p, Service: svc, Doctor: doc, Start: start, Priority: patient_queue.PriorityNormal, g: g}
	switch r := g.Float(); {
	case r < 0.08:
		v.Outcome = "CANCELLED"
	case r < 0.14:
		v.Outcome = "NO_SHOW"
	case r < 0.17:
		v.Outcome = "LEFT"
	default:
		v.Outcome = "HONORED"
	}
	v.Reschedule = v.Outcome == "HONORED" && g.Chance(0.08)
	w.Clinic.reserve(p, v.ID, start.Add(-2*time.Hour), start.Add(6*time.Hour))
	w.Sched.At(bookAt, "agenda.reservation", func(ctx *StepContext) error { return v.book(ctx) })
}

// planSameDayAppointment books an emergency-department appointment at a given start (when
// the practitioner is free) and makes the patient arrive on time.
func planSameDayAppointment(w *World, g *RNG, d Day, start time.Time) {
	p := pickPatient(w, g, start)
	docs := w.Clinic.byService["URG"]
	if p == nil || len(docs) == 0 || w.Clinic.isBusy(p, start.Add(-2*time.Hour), start.Add(6*time.Hour)) {
		return
	}
	doc := docs[g.Intn(len(docs))]
	dur := w.Ref.ApptTypes[visitServiceType["URG"]].DefaultDurationMinutes
	if dur <= 0 {
		dur = 30
	}
	occ := w.Clinic.slots[doc.UserID]
	if occ == nil {
		occ = map[int64]bool{}
		w.Clinic.slots[doc.UserID] = occ
	}
	for m := 0; m < dur; m += 15 {
		if occ[start.Add(time.Duration(m)*time.Minute).Unix()] {
			return
		}
	}
	w.Clinic.reserveSlot(doc.UserID, start, dur)
	w.Clinic.visits++
	v := &Visit{ID: w.Clinic.visits, P: p, Service: "URG", Doctor: doc, Start: start, Priority: patient_queue.PriorityNormal, Outcome: "HONORED", g: g}
	w.Clinic.reserve(p, v.ID, start.Add(-2*time.Hour), start.Add(6*time.Hour))
	w.Sched.At(d.At(6, 45), "agenda.reservation", func(ctx *StepContext) error { return v.book(ctx) })
}

// freeSlot returns a free slot inside the practitioner's working hours.
func freeSlot(w *World, g *RNG, doc *Member, day time.Time, typ string) (time.Time, bool) {
	dur := w.Ref.ApptTypes[typ].DefaultDurationMinutes
	if dur <= 0 {
		dur = 30
	}
	occupied := w.Clinic.slots[doc.UserID]
	if occupied == nil {
		occupied = map[int64]bool{}
		w.Clinic.slots[doc.UserID] = occupied
	}
	loc := w.Cfg.Location
	base := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, loc)
	var windows [][2]int // minutes from midnight
	if doc.PrimaryService() == "URG" {
		windows = [][2]int{{7 * 60, 19 * 60}}
	} else {
		if !w.Clinic.workdays[doc.UserID][base.Weekday()] {
			return time.Time{}, false
		}
		if base.Weekday() == time.Saturday {
			windows = [][2]int{{8 * 60, 12*60 + 30}}
		} else {
			windows = [][2]int{{8 * 60, 12*60 + 30}, {14 * 60, 17*60 + 30}}
		}
	}
	for try := 0; try < 20; try++ {
		win := windows[g.Intn(len(windows))]
		slots := (win[1] - win[0] - dur) / 15
		if slots <= 0 {
			continue
		}
		m := win[0] + 15*g.Intn(slots+1)
		start := base.Add(time.Duration(m) * time.Minute)
		key := start.Unix()
		clash := false
		for k := int64(0); k < int64(dur); k += 15 {
			if occupied[key+k*60] {
				clash = true
				break
			}
		}
		if clash {
			continue
		}
		w.Clinic.reserveSlot(doc.UserID, start, dur)
		return start, true
	}
	return time.Time{}, false
}

func receptionFor(w *World, svc string) *Member {
	for _, m := range w.Staff.ByFunction("ACCUEIL") {
		if contains(m.Services, svc) {
			return m
		}
	}
	return w.Staff.Admin
}

func (v *Visit) book(ctx *StepContext) error {
	w := ctx.W
	agent := receptionFor(w, v.Service)
	tid := w.Ref.ApptTypes[visitServiceType[v.Service]].ID
	pid := v.Doctor.UserID
	appt, _, err := w.Svc.Queue.BookAppointment(patient_queue.BookAppointmentRequest{
		PatientID: v.P.PatientID, ServiceID: w.Org.ServiceID(v.Service), PractitionerID: &pid, AppointmentTypeID: &tid,
		StartAt: v.Start, Reason: Pick(v.g, bookingReasons), IdempotencyKey: fmt.Sprintf("dc-booking-%d", v.ID),
	}, agent.QueueAccess(w))
	if err != nil {
		w.Clinic.release(v.P, v.ID)
		dur := w.Ref.ApptTypes[visitServiceType[v.Service]].DefaultDurationMinutes
		w.Clinic.releaseSlot(v.Doctor.UserID, v.Start, dur)
		return err
	}
	v.ApptID = appt.ID
	g := v.g
	switch {
	case v.Outcome == "CANCELLED":
		at := ctx.Sim.Add(time.Duration(float64(v.Start.Sub(ctx.Sim)) * g.Float()))
		if g.Chance(0.3) { // the patient changes their mind right after booking
			at = ctx.Sim.Add(time.Duration(g.Between(10, 60)) * time.Minute)
		}
		w.Sched.At(at, "agenda.annulation", func(ctx *StepContext) error {
			_, err := w.Svc.Queue.CancelAppointment(v.ApptID, patient_queue.CancelAppointmentRequest{Reason: Pick(g, cancelReasons), IdempotencyKey: fmt.Sprintf("dc-cancel-%d", v.ID)}, agent.QueueAccess(w))
			return err
		})
	case v.Outcome == "NO_SHOW":
		w.Sched.At(v.Start.Add(time.Duration(g.Between(90, 240))*time.Minute), "agenda.absence", func(ctx *StepContext) error {
			_, err := w.Svc.Queue.MarkNoShow(v.ApptID, patient_queue.NoShowAppointmentRequest{Reason: "Patient non présenté", IdempotencyKey: fmt.Sprintf("dc-noshow-%d", v.ID)}, agent.QueueAccess(w))
			return err
		})
	default:
		if v.Reschedule {
			at := ctx.Sim.Add(time.Duration(float64(v.Start.Sub(ctx.Sim)) * 0.5))
			// Reception agents have no appointment.reschedule.* permission (real RBAC rule):
			// reschedules are done by the agenda manager.
			w.Sched.At(at, "agenda.report", func(ctx *StepContext) error { return v.reschedule(ctx, ctx.W.Staff.Admin) })
			return nil
		}
		v.scheduleArrival(w)
	}
	return nil
}

func (v *Visit) reschedule(ctx *StepContext, agent *Member) error {
	w := ctx.W
	g := v.g
	newDay := v.Start.AddDate(0, 0, g.Between(1, 7))
	doc := v.Doctor
	if g.Chance(0.35) { // practitioner change
		if docs := w.Clinic.byService[v.Service]; len(docs) > 1 {
			doc = docs[g.Intn(len(docs))]
		}
	}
	start, ok := freeSlot(w, g, doc, newDay, visitServiceType[v.Service])
	if !ok {
		v.scheduleArrival(w)
		return nil
	}
	var appt patient_queue.Appointment
	if err := w.DB.First(&appt, v.ApptID).Error; err != nil {
		return err
	}
	pid := doc.UserID
	end := appt.ScheduledAt
	if appt.ScheduledEndAt != nil {
		end = *appt.ScheduledEndAt
	}
	req := patient_queue.RescheduleAppointmentRequest{StartAt: start, ExpectedScheduledAt: appt.ScheduledAt, ExpectedScheduledEndAt: end,
		Reason: Pick(g, rescheduleReasons), IdempotencyKey: fmt.Sprintf("dc-resched-%d", v.ID)}
	if doc.UserID != v.Doctor.UserID {
		req.PractitionerID = &pid
	}
	dur := w.Ref.ApptTypes[visitServiceType[v.Service]].DefaultDurationMinutes
	if _, err := w.Svc.Queue.RescheduleAppointment(v.ApptID, req, agent.QueueAccess(w)); err != nil {
		w.Clinic.releaseSlot(doc.UserID, start, dur)
		v.scheduleArrival(w)
		return err
	}
	w.Clinic.releaseSlot(v.Doctor.UserID, v.Start, dur)
	v.Start, v.Doctor = start, doc
	w.Clinic.release(v.P, v.ID)
	w.Clinic.reserve(v.P, v.ID, start.Add(-2*time.Hour), start.Add(6*time.Hour))
	v.scheduleArrival(w)
	return nil
}

func (v *Visit) scheduleArrival(w *World) {
	g := v.g
	var offset time.Duration
	switch r := g.Float(); {
	case r < 0.25:
		offset = -time.Duration(g.Between(16, 45)) * time.Minute // EARLY
	case r < 0.8:
		offset = time.Duration(g.Between(-10, 14)) * time.Minute // ON_TIME
	default:
		offset = time.Duration(g.Between(16, 75)) * time.Minute // LATE
	}
	w.Sched.At(v.Start.Add(offset), "file.check-in-rdv", func(ctx *StepContext) error { return v.checkIn(ctx) })
}

func (v *Visit) checkIn(ctx *StepContext) error {
	w := ctx.W
	agent := receptionFor(w, v.Service)
	req := patient_queue.AppointmentCheckInRequest{IdentityConfirmed: true}
	fin, _ := w.Svc.Queue.EvaluateFinance(v.P.PatientID)
	if fin == patient_queue.FinancePaymentRequired {
		req.FinanceOverride, req.FinanceOverrideNote = true, "Solde antérieur — admission accordée, régularisation à la caisse"
	}
	if v.g.Chance(0.1) {
		req.Priority = patient_queue.PriorityHigh
	}
	t, _, err := w.Svc.Queue.CheckInAppointment(v.ApptID, req, agent.QueueAccess(w))
	if err != nil {
		return err
	}
	v.TicketID = t.ID
	v.continueFromCheckIn(ctx)
	return nil
}

// ---------------------------------------------------------------------------------------
// Walk-in.

func planWalkin(w *World, g *RNG, p *Profile, svc string, at time.Time) {
	if w.Clinic.isBusy(p, at.Add(-30*time.Minute), at.Add(8*time.Hour)) {
		return
	}
	doctors := w.Clinic.byService[svc]
	if len(doctors) == 0 {
		svc, doctors = "GEN", w.Clinic.byService["GEN"]
	}
	if len(doctors) == 0 {
		return
	}
	w.Clinic.visits++
	v := &Visit{ID: w.Clinic.visits, P: p, Service: svc, Doctor: doctors[g.Intn(len(doctors))], Walkin: true, Start: at, Outcome: "HONORED", g: g}
	v.Priority = Pick(g, []string{patient_queue.PriorityNormal, patient_queue.PriorityNormal, patient_queue.PriorityHigh, patient_queue.PriorityLow})
	if svc == "URG" && g.Chance(0.18) {
		v.Priority = patient_queue.PriorityUrgent
	}
	if g.Chance(0.04) {
		v.Outcome = "LEFT"
	}
	w.Clinic.reserve(p, v.ID, at.Add(-30*time.Minute), at.Add(8*time.Hour))
	w.Sched.At(at, "file.check-in-sans-rdv", func(ctx *StepContext) error {
		agent := receptionFor(ctx.W, v.Service)
		req := patient_queue.WalkInCheckInRequest{PatientID: v.P.PatientID, ServiceID: ctx.W.Org.ServiceID(v.Service), IdentityConfirmed: true, Priority: v.Priority, Reason: Pick(g, walkinReasons)}
		if fin, _ := ctx.W.Svc.Queue.EvaluateFinance(v.P.PatientID); fin == patient_queue.FinancePaymentRequired {
			req.FinanceOverride, req.FinanceOverrideNote = true, "Urgence — prise en charge avant régularisation"
		}
		t, err := ctx.W.Svc.Queue.CheckInWalkIn(req, agent.QueueAccess(ctx.W))
		if err != nil {
			return err
		}
		v.TicketID = t.ID
		v.continueFromCheckIn(ctx)
		return nil
	})
}

// ---------------------------------------------------------------------------------------
// Queue: triage, doctor, completion.

func triageStaff(w *World, svc string, g *RNG) *Member {
	var c []*Member
	for _, m := range w.Staff.With(func(m *Member) bool {
		return (contains(m.Functions, "INFIRMIER") || contains(m.Functions, "AIDE_SOIGNANT")) && contains(m.Services, svc) && !m.Tags["restricted"]
	}) {
		c = append(c, m)
	}
	if len(c) == 0 {
		return nil
	}
	return c[g.Intn(len(c))]
}

func (v *Visit) continueFromCheckIn(ctx *StepContext) {
	w := ctx.W
	g := v.g
	wait := time.Duration(g.Between(4, 35)) * time.Minute
	if v.Priority == patient_queue.PriorityUrgent {
		wait = time.Duration(g.Between(1, 6)) * time.Minute
	}
	if v.Outcome == "LEFT" {
		w.Sched.At(ctx.Sim.Add(wait+time.Duration(g.Between(20, 90))*time.Minute), "file.depart-sans-soins", func(ctx *StepContext) error {
			agent := receptionFor(ctx.W, v.Service)
			_, err := ctx.W.Svc.Queue.Cancel(v.TicketID, patient_queue.CancelRequest{Reason: "Patient parti avant la prise en charge"}, agent.QueueAccess(ctx.W))
			return err
		})
		return
	}
	w.Sched.At(ctx.Sim.Add(w.Slow(ctx.Sim, wait)), "file.triage", func(ctx *StepContext) error { return v.triage(ctx) })
}

func (v *Visit) triage(ctx *StepContext) error {
	w := ctx.W
	g := v.g
	nurse := triageStaff(w, v.Service, g)
	actor := w.Staff.Admin
	if nurse != nil {
		actor = nurse
	}
	if _, err := w.Svc.Queue.TakeTriage(v.TicketID, actor.QueueAccess(w)); err != nil {
		return err
	}
	w.Sched.At(ctx.Sim.Add(time.Duration(g.Between(4, 15))*time.Minute), "file.triage-termine", func(ctx *StepContext) error {
		vsID, err := addVitals(w, g, v.P, ctx.Sim, actor.UserID, nil)
		if err != nil {
			return err
		}
		if _, err := w.Svc.Queue.CompleteTriage(v.TicketID, patient_queue.CompleteTriageRequest{VitalSignsID: &vsID}, actor.QueueAccess(w)); err != nil {
			return err
		}
		wait := time.Duration(g.Between(10, 80)) * time.Minute
		if v.Priority == patient_queue.PriorityUrgent {
			wait = time.Duration(g.Between(2, 10)) * time.Minute
		}
		w.Sched.At(ctx.Sim.Add(w.Slow(ctx.Sim, wait)), "file.prise-en-charge-medecin", func(ctx *StepContext) error { return v.doctorTake(ctx) })
		return nil
	})
	return nil
}

func (v *Visit) doctorTake(ctx *StepContext) error {
	w := ctx.W
	t, err := w.Svc.Queue.TakeDoctor(v.TicketID, patient_queue.TakeDoctorRequest{CreateConsultation: true}, v.Doctor.QueueAccess(w))
	if err != nil {
		return err
	}
	if t.ConsultationID != nil {
		v.ConsultID = *t.ConsultationID
	}
	g := v.g
	w.Sched.At(ctx.Sim.Add(w.Slow(ctx.Sim, time.Duration(g.Between(8, 25))*time.Minute)), "consultations.saisie", func(ctx *StepContext) error { return v.document(ctx) })
	return nil
}

func (v *Visit) complete(ctx *StepContext) error {
	w := ctx.W
	if _, err := w.Svc.Queue.Complete(v.TicketID, patient_queue.CompleteRequest{Disposition: v.Disposition, DispositionNote: dispositionNote(v.Disposition)}, v.Doctor.QueueAccess(w)); err != nil {
		return err
	}
	v.P.Visits++
	w.Clinic.release(v.P, v.ID)
	afterConsultation(ctx, v)
	return nil
}

func dispositionNote(d string) string {
	switch d {
	case "HOSPITALIZED":
		return "Hospitalisation décidée"
	case "REFERRED":
		return "Orienté vers un spécialiste"
	case "TRANSFERRED":
		return "Transfert vers un autre établissement"
	case "OBSERVATION":
		return "Surveillance de quelques heures"
	}
	return ""
}

var bookingReasons = []string{"Consultation de suivi", "Renouvellement d'ordonnance", "Bilan annuel", "Douleurs persistantes", "Contrôle tensionnel", "Résultats d'examens", "Première consultation", "Suivi de grossesse"}
var cancelReasons = []string{"Empêchement du patient", "Patient hospitalisé ailleurs", "Rendez-vous pris en double", "Problème de transport", "Indisponibilité du praticien"}
var rescheduleReasons = []string{"Demande du patient", "Congé du praticien", "Conflit d'horaire", "Report pour examens complémentaires"}
var walkinReasons = []string{"Fièvre", "Douleur thoracique", "Traumatisme", "Douleurs abdominales", "Crise d'asthme", "Malaise", "Céphalées intenses", "Plaie à suturer", "Vomissements"}

var _ = sort.Strings
