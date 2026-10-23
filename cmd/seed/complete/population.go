package complete

import (
	"fmt"
	"sort"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/insurance/coverage"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
)

// Medical record richness tiers.
const (
	TierMinimal  = "MINIMAL"
	TierStandard = "STANDARD"
	TierRich     = "RICH"
	TierComplex  = "COMPLEX"
)

// Profile is the deterministic description of one patient (before and after registration).
type Profile struct {
	Index       int
	Ident       identity
	Birth       time.Time
	Phone       string
	Email       string
	District    string
	Contact     string
	RegisterDay int
	Tier        string
	Conditions  []string // HTA, DIABETE, ASTHME, DREPANO, GROSSESSE, CARDIO, ARTHROSE, MIGRAINE
	Weight      float64  // relative visit propensity
	Insurance   *insurancePlan

	// Filled at registration.
	PatientID  uint
	RecordID   uint
	CoverageID uint // principal coverage (0 when none)
	Registered bool
	Visits     int
}

type insurancePlan struct {
	CompanyIdx, GuarantorIdx int
	Rate                     float64
	From, To                 time.Time
	Secondary                bool // an additional non-principal coverage
	Expired                  bool // coverage ended before the anchor
	Member                   string
}

func (p *Profile) AgeAt(t time.Time) int {
	y := t.Year() - p.Birth.Year()
	if t.YearDay() < p.Birth.YearDay() {
		y--
	}
	return y
}

func (p *Profile) Has(cond string) bool { return contains(p.Conditions, cond) }

// Insured tells whether a valid principal coverage exists at t.
// The insurance services check coverage validity against the real present, so an expired
// coverage (ended before the anchor) is shown as such but never used for new PEC requests.
func (p *Profile) Insured(t time.Time) bool {
	return p.Insurance != nil && p.CoverageID != 0 && !p.Insurance.Expired && !t.Before(p.Insurance.From) && !t.After(p.Insurance.To)
}

type patientState struct {
	Profiles []*Profile
	// registered profiles, in registration order (visit pool).
	Pool []*Profile
	// per-day new registrations (first visit the same day).
	NewOn map[int][]*Profile
}

type populationPlanner struct{}

func (populationPlanner) Name() string { return "Patients / dossiers" }

func (populationPlanner) Setup(w *World) error {
	g := w.RNG.Stream("population")
	n := w.Cfg.Patients
	anchor := w.Cfg.Anchor
	st := w.People
	st.NewOn = map[int][]*Profile{}
	companies := len(w.Ref.Companies)
	var previous []identity
	for i := 0; i < n; i++ {
		pr := &Profile{Index: i}
		// Age pyramid (credible for a general hospital).
		var age int
		switch g.Weighted([]float64{8, 14, 13, 30, 22, 13}) {
		case 0:
			age = g.Between(0, 2)
		case 1:
			age = g.Between(3, 14)
		case 2:
			age = g.Between(15, 24)
		case 3:
			age = g.Between(25, 44)
		case 4:
			age = g.Between(45, 64)
		default:
			age = g.Between(65, 92)
		}
		pr.Ident = newIdentity(g, "")
		if len(previous) > 40 && g.Chance(0.02) { // homonyms: same name and first names
			h := previous[g.Intn(len(previous))]
			pr.Ident.Last, pr.Ident.First, pr.Ident.Sex = h.Last, h.First, h.Sex
		}
		previous = append(previous, pr.Ident)
		pr.Birth = anchor.AddDate(-age, -g.Between(0, 11), -g.Between(0, 27))
		if !g.Chance(0.05) {
			prefix := Pick(g, []string{"07", "05", "01"})
			pr.Phone = phone(prefix, 10000000+i*7919%89999999)
		}
		if age >= 16 && g.Chance(0.35) {
			pr.Email = fmt.Sprintf("%s.%s.%d@patients.medcore.example", slug(firstWord(pr.Ident.First)), slug(pr.Ident.Last), i)
		}
		pr.District = Pick(g, districts)
		if age < 18 || age > 75 || g.Chance(0.6) {
			c := newIdentity(g, "")
			pr.Contact = fmt.Sprintf("%s (%s) %s", c.Full(), Pick(g, relationships), phone("07", 20000000+i*104729%79999999))
		}
		// Registration: 55% existing population (first weeks), 45% new over the year.
		if g.Chance(0.55) {
			pr.RegisterDay = g.Between(0, min(20, w.Cfg.Days-1))
		} else {
			pr.RegisterDay = g.Between(min(21, w.Cfg.Days-1), w.Cfg.Days-1)
		}
		// Conditions drive specialties and visit frequency.
		pr.Weight = 1
		addCond := func(c string, p float64, w float64) {
			if g.Chance(p) {
				pr.Conditions = append(pr.Conditions, c)
				pr.Weight += w
			}
		}
		if age >= 35 {
			addCond("HTA", 0.25, 1.5)
			addCond("DIABETE", 0.13, 1.8)
			addCond("CARDIO", 0.06, 1.5)
		}
		if age >= 50 {
			addCond("ARTHROSE", 0.15, 0.8)
		}
		addCond("ASTHME", 0.06, 0.8)
		addCond("DREPANO", 0.02, 2.0)
		addCond("MIGRAINE", 0.05, 0.5)
		if pr.Ident.Sex == "F" && age >= 18 && age <= 42 {
			addCond("GROSSESSE", 0.12, 2.5)
		}
		if age <= 5 {
			pr.Weight += 1.2 // pediatric follow-up and vaccinations
		}
		if age >= 65 {
			pr.Weight += 0.8
		}
		// Medical record tier.
		switch {
		case len(pr.Conditions) >= 2 || g.Chance(0.04):
			pr.Tier = TierComplex
		case len(pr.Conditions) == 1 || g.Chance(0.12):
			pr.Tier = TierRich
		case g.Chance(0.55):
			pr.Tier = TierStandard
		default:
			pr.Tier = TierMinimal
		}
		// Insurance situations.
		if companies > 0 && g.Chance(0.55) {
			ci := g.Intn(companies)
			gs := w.Ref.Guarantors[w.Ref.Companies[ci].ID]
			plan := &insurancePlan{CompanyIdx: ci, GuarantorIdx: g.Intn(max(1, len(gs))), Member: fmt.Sprintf("%s-%07d", w.Ref.Companies[ci].Code, 1000000+i*31%8999999)}
			if len(gs) > 0 {
				plan.Rate = gs[plan.GuarantorIdx].DefaultCoverageRate
			}
			plan.From = w.Cfg.FirstDay().AddDate(-g.Between(0, 3), -g.Between(0, 11), 0)
			plan.To = anchor.AddDate(1, g.Between(0, 11), 0)
			switch {
			case g.Chance(0.08):
				plan.Expired = true
				plan.To = anchor.AddDate(0, -g.Between(1, 5), -g.Between(0, 20))
			case g.Chance(0.06): // coverage starting during the year
				plan.From = w.Cfg.FirstDay().AddDate(0, g.Between(2, 9), 0)
			}
			plan.Secondary = g.Chance(0.12)
			pr.Insurance = plan
		}
		st.Profiles = append(st.Profiles, pr)
	}
	for _, p := range st.Profiles {
		st.NewOn[p.RegisterDay] = append(st.NewOn[p.RegisterDay], p)
	}
	return nil
}

func firstWord(s string) string {
	for i, r := range s {
		if r == ' ' {
			return s[:i]
		}
	}
	return s
}

// PlanDay registers the day's patients (reception staff), through the patients service.
func (populationPlanner) PlanDay(w *World, d Day) {
	list := w.People.NewOn[d.Index]
	sort.Slice(list, func(i, j int) bool { return list[i].Index < list[j].Index })
	g := w.RNG.Stream("population.day", uint64(d.Index))
	for _, p := range list {
		p := p
		at := d.At(7, 0).Add(g.Minutes(0, 9*60))
		if d.IsToday && at.After(w.Horizon.Add(-10*time.Minute)) { // today: registered before now
			at = d.At(0, 0).Add(time.Duration(float64(w.Horizon.Sub(d.At(0, 0))) * g.Float() * 0.9))
		}
		if d.Index == 0 {
			at = d.At(6, 30).Add(time.Duration(p.Index%400) * 20 * time.Second)
		}
		w.Sched.At(at, "patients.registration", func(ctx *StepContext) error { return registerPatient(ctx, p) })
	}
	if d.Index == 1 && w.Opts.ComplexArchetypes != nil {
		w.Sched.At(d.At(6, 0), "dossiers.archetypes-legacy", func(ctx *StepContext) error { return legacyArchetypes(ctx) })
	}
}

func registerPatient(ctx *StepContext, p *Profile) error {
	w := ctx.W
	req := patients.CreatePatientRequest{Nom: p.Ident.Last, Prenoms: p.Ident.First, Sexe: p.Ident.Sex, DateNaissance: p.Birth.Format("2006-01-02"),
		Telephone: p.Phone, Email: p.Email, Quartier: p.District, PersonneContact: p.Contact}
	if p.Insurance != nil {
		req.IsAssure, req.TauxCouverture, req.MatriculeAssure = true, p.Insurance.Rate, p.Insurance.Member
	}
	created, err := w.Svc.Patients.Create(req)
	if err != nil {
		return err
	}
	p.PatientID = created.ID
	rec, err := w.Svc.MedRec.GetOrCreateMedicalRecord(created.ID)
	if err != nil {
		return err
	}
	p.RecordID = rec.ID
	p.Registered = true
	w.People.Pool = append(w.People.Pool, p)
	if p.Insurance != nil {
		if err := createCoverages(w, p); err != nil {
			return err
		}
	}
	return enrichRecord(ctx, p)
}

func createCoverages(w *World, p *Profile) error {
	plan := p.Insurance
	comp := w.Ref.Companies[plan.CompanyIdx]
	gs := w.Ref.Guarantors[comp.ID]
	if len(gs) == 0 {
		return nil
	}
	gu := gs[plan.GuarantorIdx%len(gs)]
	cov, err := w.Svc.Coverage.Create(coverage.CreateCoverageRequest{PatientID: p.PatientID, CompanyID: comp.ID, GuarantorID: gu.ID, MemberNumber: plan.Member,
		Subscriber: p.Ident.Full(), Beneficiary: p.Ident.Full(), CoverageRate: plan.Rate, ValidFrom: plan.From.Format("2006-01-02"), ValidTo: plan.To.Format("2006-01-02"), IsPrincipal: true})
	if err != nil {
		return wrapf(err, "couverture")
	}
	p.CoverageID = cov.ID
	if plan.Secondary {
		other := w.Ref.Companies[(plan.CompanyIdx+1)%len(w.Ref.Companies)]
		og := w.Ref.Guarantors[other.ID]
		if len(og) > 0 {
			if _, err := w.Svc.Coverage.Create(coverage.CreateCoverageRequest{PatientID: p.PatientID, CompanyID: other.ID, GuarantorID: og[0].ID, MemberNumber: plan.Member + "-C",
				Subscriber: p.Ident.Full(), Beneficiary: p.Ident.Full(), CoverageRate: og[0].DefaultCoverageRate / 2, ValidFrom: plan.From.Format("2006-01-02"), ValidTo: plan.To.Format("2006-01-02"), IsPrincipal: false}); err != nil {
				return wrapf(err, "couverture secondaire")
			}
		}
	}
	return nil
}

func legacyArchetypes(ctx *StepContext) error {
	w := ctx.W
	var items []patients.Patient
	for _, p := range w.People.Pool {
		if len(items) == 8 {
			break
		}
		if p.Tier == TierComplex && p.Insurance == nil {
			var pt patients.Patient
			if err := w.DB.First(&pt, p.PatientID).Error; err == nil {
				items = append(items, pt)
			}
		}
	}
	if len(items) == 8 {
		w.Opts.ComplexArchetypes(w.DB, items)
	}
	return nil
}
