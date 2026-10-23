// Package complete implements `seed --demo-complete`: a deterministic, volumetric and
// coherent MedCore reference dataset covering one full year of activity.
//
// Principles (see README in the package doc of run.go):
//   - business state is reached through the real module services whenever they exist;
//   - every service call runs inside a "window" bound to a simulated instant, and the
//     timestamps it produces are re-anchored on that instant afterwards (timewarp.go),
//     because services stamp time.Now() and expose no injectable clock;
//   - the generation is fully driven by a seeded pseudo-random source and an anchor date.
package complete

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Environment knobs read by cmd/seed (never by the pure validators below).
const (
	EnvAnchorDate = "MEDCORE_DEMO_COMPLETE_ANCHOR" // YYYY-MM-DD, default: today in the business timezone
	EnvSeed       = "MEDCORE_DEMO_COMPLETE_SEED"   // uint64, default DefaultSeed
	EnvPatients   = "MEDCORE_DEMO_COMPLETE_PATIENTS"
	EnvDays       = "MEDCORE_DEMO_COMPLETE_DAYS"

	DefaultSeed     uint64 = 20260101
	DefaultPatients        = 1500
	DefaultDays            = 365

	minPatients = 40
	maxPatients = 20000
	minDays     = 14
	maxDays     = 730

	// PatientCodePrefix marks every patient created by --demo-complete.
	PatientCodePrefix = "DC-"
	// StaffEmailDomain / prefix of the demo-complete staff accounts.
	StaffEmailPrefix = "dc."
	StaffEmailDomain = "@medcore.local"
)

// Config is the complete, validated description of a dataset. Identical Config on an
// empty database => identical logical dataset.
type Config struct {
	Anchor   time.Time // start of the anchor business day, in Location
	Days     int       // number of simulated days, anchor included
	Patients int
	Seed     uint64
	Location *time.Location
}

// RawConfig carries the unparsed knobs (environment values).
type RawConfig struct {
	Anchor, Seed, Patients, Days string
}

// SnapshotHour is the simulated "present" on the anchor day: the dataset is a snapshot of
// the establishment at anchor 12:00 (business timezone), whatever the wall clock of the run.
const SnapshotHour = 12

// ParseConfig validates the raw knobs. The wall clock (now) only intervenes for the default
// anchor (latest day whose 12:00 snapshot is already past) and to refuse an explicit anchor
// whose snapshot would lie in the future (MedCore services compare with the real clock).
// It never changes the generated data of an explicit anchor.
func ParseConfig(raw RawConfig, loc *time.Location, now time.Time) (Config, error) {
	if loc == nil {
		return Config{}, errors.New("demo-complete: fuseau métier requis")
	}
	cfg := Config{Days: DefaultDays, Patients: DefaultPatients, Seed: DefaultSeed, Location: loc}
	local := now.In(loc)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	cfg.Anchor = today
	if v := strings.TrimSpace(raw.Anchor); v != "" {
		d, err := time.ParseInLocation("2006-01-02", v, loc)
		if err != nil {
			return Config{}, fmt.Errorf("demo-complete: %s invalide (format AAAA-MM-JJ)", EnvAnchorDate)
		}
		cfg.Anchor = d
		if cfg.SimulationNow().After(now) {
			return Config{}, fmt.Errorf("demo-complete: l'instant simulé (%s à %02d:00) est dans le futur — choisir une ancre antérieure ou relancer plus tard", v, SnapshotHour)
		}
	} else if cfg.SimulationNow().After(now) {
		cfg.Anchor = today.AddDate(0, 0, -1)
	}
	if v := strings.TrimSpace(raw.Seed); v != "" {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("demo-complete: %s invalide", EnvSeed)
		}
		cfg.Seed = n
	}
	var err error
	if cfg.Patients, err = intKnob(raw.Patients, EnvPatients, DefaultPatients, minPatients, maxPatients); err != nil {
		return Config{}, err
	}
	if cfg.Days, err = intKnob(raw.Days, EnvDays, DefaultDays, minDays, maxDays); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func intKnob(raw, name string, def, lo, hi int) (int, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < lo || n > hi {
		return 0, fmt.Errorf("demo-complete: %s doit être un entier entre %d et %d", name, lo, hi)
	}
	return n, nil
}

// FirstDay is the first simulated business day (anchor - Days + 1).
func (c Config) FirstDay() time.Time { return c.Anchor.AddDate(0, 0, -(c.Days - 1)) }

// SimulationNow is the unique simulated present: anchor day at SnapshotHour. It derives from
// the configuration only; the generator (horizon) and the validator both use it.
func (c Config) SimulationNow() time.Time {
	return time.Date(c.Anchor.Year(), c.Anchor.Month(), c.Anchor.Day(), SnapshotHour, 0, 0, 0, c.Anchor.Location())
}

// Scale is the volume multiplier relative to the reference configuration (1500 patients,
// 365 days); every daily volume derives from it, keeping ratios credible at any size.
func (c Config) Scale() float64 {
	return float64(c.Patients) / float64(DefaultPatients)
}
