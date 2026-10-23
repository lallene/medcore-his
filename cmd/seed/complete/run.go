package complete

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/lallene/medcore-his/backend/internal/modules/auth"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
)

// AccountFactory is cmd/seed's create-only account primitive (createDemoAccount): an
// existing account is never modified, a new one is created atomically with its profile.
type AccountFactory func(db *gorm.DB, name, email, role string, active bool, profile func(tx *gorm.DB, u auth.User) error) (auth.User, bool, error)

// Options are the dependencies injected by cmd/seed (guard already passed).
type Options struct {
	Config        Config
	CreateAccount AccountFactory
	// ReferenceCatalogs creates the legacy reference catalogs (consultation reasons,
	// medical exams, pharmacy catalog) reused as-is from clinical_demo.go.
	ReferenceCatalogs func(db *gorm.DB)
	// ComplexArchetypes enriches 8 patients with the legacy rich clinical archetypes
	// (seedClinicalDemo), used for the "complex record" tier.
	ComplexArchetypes func(db *gorm.DB, items []patients.Patient)
	Logf              func(format string, args ...any)
	// Now is the real clock (injectable for tests).
	Now func() time.Time
}

// Planner is one functional domain of the dataset.
type Planner interface {
	Name() string
	// Setup creates the domain configuration (inside the first-day window).
	Setup(w *World) error
	// PlanDay schedules the domain steps of a simulated day.
	PlanDay(w *World, d Day)
}

// StepContext is handed to every step.
type StepContext struct {
	W   *World
	Sim time.Time
}

// ErrDatasetExists means the target already holds a complete dataset: generation is
// skipped and only the validation runs (re-run strategy).
var ErrDatasetExists = errors.New("demo-complete: jeu de données déjà présent")

// Run generates the dataset, then validates it. It returns the validation report.
func Run(db *gorm.DB, opts Options) (*Report, error) {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Logf == nil {
		opts.Logf = func(string, ...any) {}
	}
	if opts.CreateAccount == nil {
		return nil, errors.New("demo-complete: fabrique de comptes requise")
	}
	if opts.Config.SimulationNow().After(opts.Now()) {
		return nil, errors.New("demo-complete: l'instant simulé est dans le futur (ancre trop récente)")
	}
	// Silent SQL logger: tens of thousands of statements, and expected "record not found".
	db = db.Session(&gorm.Session{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	state, err := inspectTarget(db)
	if err != nil {
		return nil, err
	}
	switch state {
	case targetForeign:
		return nil, errors.New("demo-complete refusé : la base contient des patients qui ne proviennent pas de --demo-complete (base dédiée requise)")
	case targetComplete:
		opts.Logf("[DEMO-COMPLETE] jeu de données existant détecté : génération ignorée, validation seule")
		rep := Validate(db, opts.Config)
		return rep, ErrDatasetExists
	}

	w, err := newWorld(db, opts)
	if err != nil {
		return nil, err
	}
	audits := registerAudit(db)
	planners := allPlanners()
	start := opts.Now()
	cal := BuildCalendar(opts.Config, w.RNG.Stream("calendar"))
	w.Calendar = cal

	// Setup (reference data, organisation, staff, RBAC, catalogs, base population).
	first := cal[0]
	for _, p := range planners {
		if err := w.Warp.Run(first.At(6, 0), func() error { return p.Setup(w) }); err != nil {
			return nil, fmt.Errorf("%s: %w", p.Name(), err)
		}
		w.progress(p.Name() + " (configuration)")
	}
	audits.settle()
	if err := w.Warp.Flush(); err != nil {
		return nil, err
	}

	// Day loop.
	for _, d := range cal {
		for _, p := range planners {
			p.PlanDay(w, d)
		}
		dayEnd := d.Date.AddDate(0, 0, 1)
		for {
			st, ok := w.Sched.popBefore(dayEnd)
			if !ok {
				break
			}
			ctx := &StepContext{W: w, Sim: st.At}
			if err := w.Warp.Run(st.At, func() error { return st.Run(ctx) }); err != nil {
				w.Stats.fail(st.Name, err)
			} else {
				w.Stats.ok(st.Name)
			}
		}
		audits.settle()
		if err := w.Warp.Flush(); err != nil {
			return nil, fmt.Errorf("timewarp jour %s: %w", d.Date.Format("2006-01-02"), err)
		}
		if d.Index%30 == 0 || d.IsToday {
			opts.Logf("[DEMO-COMPLETE] jour %3d/%d %s (%s) — étapes OK %d, en échec %d",
				d.Index+1, len(cal), d.Date.Format("2006-01-02"), d.Kind, w.Stats.totalOK(), w.Stats.totalFail())
		}
	}
	// Notification worker pass at the present instant (safe adapters, see notifications.go).
	if err := w.Warp.Run(w.Horizon, func() error { return processNotifications(w) }); err != nil {
		w.Stats.fail("notifications.worker", err)
	} else {
		w.Stats.ok("notifications.worker")
	}
	audits.settle()
	if err := w.Warp.Flush(); err != nil {
		return nil, err
	}
	opts.Logf("[DEMO-COMPLETE] génération terminée en %s (%d lignes ré-ancrées dans le temps)", opts.Now().Sub(start).Round(time.Second), w.Warp.Rows)
	w.Stats.log(opts.Logf, w.Sched.dropped)
	rep := Validate(db, opts.Config)
	return rep, nil
}

// Stats counts executed steps per domain; expected business refusals are recorded too.
type Stats struct {
	okN     map[string]int
	failN   map[string]int
	samples map[string]string
}

func newStats() *Stats {
	return &Stats{okN: map[string]int{}, failN: map[string]int{}, samples: map[string]string{}}
}
func (s *Stats) ok(n string) { s.okN[n]++ }
func (s *Stats) fail(n string, err error) {
	s.failN[n]++
	if _, ok := s.samples[n]; !ok {
		s.samples[n] = err.Error()
	}
}
func (s *Stats) totalOK() (t int) {
	for _, v := range s.okN {
		t += v
	}
	return
}
func (s *Stats) totalFail() (t int) {
	for _, v := range s.failN {
		t += v
	}
	return
}
func (s *Stats) log(logf func(string, ...any), dropped map[string]int) {
	names := map[string]bool{}
	for k := range s.okN {
		names[k] = true
	}
	for k := range s.failN {
		names[k] = true
	}
	keys := make([]string, 0, len(names))
	for k := range names {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	logf("[DEMO-COMPLETE] étapes par domaine (OK / échec / non exécutées car futures) :")
	for _, k := range keys {
		line := fmt.Sprintf("  %-34s %6d / %4d / %4d", k, s.okN[k], s.failN[k], dropped[k])
		if s.failN[k] > 0 {
			line += "  ex: " + truncate(s.samples[k], 140)
		}
		logf("%s", line)
	}
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
