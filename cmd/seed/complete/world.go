package complete

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

// World is the shared state of one generation run.
type World struct {
	DB       *gorm.DB
	Opts     Options
	Cfg      Config
	RNG      *RNG
	Warp     *Timewarp
	Sched    *Scheduler
	Stats    *Stats
	Calendar []Day
	Horizon  time.Time

	Svc     *Services
	Ref     *refState
	Org     *orgState
	Staff   *staffState
	People  *patientState
	Clinic  *clinicState
	Finance *financeState
	Ins     *insuranceState
}

func newWorld(db *gorm.DB, opts Options) (*World, error) {
	tw, err := NewTimewarp(db, opts.Config.Location)
	if err != nil {
		return nil, err
	}
	// The simulation horizon belongs to the configuration, never to the launch clock.
	horizon := opts.Config.SimulationNow()
	w := &World{
		DB: db, Opts: opts, Cfg: opts.Config,
		RNG:     newRNG(opts.Config.Seed, "demo-complete"),
		Warp:    tw,
		Sched:   NewScheduler(horizon),
		Stats:   newStats(),
		Horizon: horizon,
		Org:     &orgState{},
		Staff:   &staffState{},
		People:  &patientState{},
		Clinic:  &clinicState{},
		Finance: &financeState{},
		Ins:     &insuranceState{},
	}
	w.Svc = newServices(w)
	return w, nil
}

// Today is the anchor day.
func (w *World) Today() Day { return w.Calendar[len(w.Calendar)-1] }

// Slow stretches durations on the anchor day (crowded day: patients pile up in the queues
// and work lists at the present instant).
func (w *World) Slow(at time.Time, d time.Duration) time.Duration {
	if !at.Before(w.Cfg.Anchor) {
		return d * 3
	}
	return d
}

// IsPast tells whether a simulated instant is executable.
func (w *World) IsPast(t time.Time) bool { return !t.After(w.Horizon) }

func (w *World) progress(label string) {
	w.Opts.Logf("[DEMO-COMPLETE] %s", label)
}

func (w *World) count(label string, n int64) {
	dots := 26 - len([]rune(label))
	if dots < 2 {
		dots = 2
	}
	w.Opts.Logf("[DEMO-COMPLETE] %s %s %d", label, strings.Repeat(".", dots), n)
}

// rowCount counts rows of a table (statistics helper).
func (w *World) rowCount(table string, where string, args ...any) int64 {
	var n int64
	q := w.DB.Table(table)
	if where != "" {
		q = q.Where(where, args...)
	}
	_ = q.Count(&n).Error
	return n
}

func wrapf(err error, format string, args ...any) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", fmt.Sprintf(format, args...), err)
}
