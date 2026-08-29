package scheduling

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// EnvTimezone is the IANA timezone used to interpret recurring wall-clock schedules.
const EnvTimezone = "MEDCORE_TIMEZONE"

// DefaultTimezone is used when MEDCORE_TIMEZONE is unset.
// UTC matches existing appointment storage conventions until a deployment sets an explicit zone.
const DefaultTimezone = "UTC"

var (
	locMu sync.RWMutex
	loc   *time.Location = time.UTC
)

// LoadLocationFromEnv loads MEDCORE_TIMEZONE (or DefaultTimezone) into the process location.
func LoadLocationFromEnv() error {
	name := os.Getenv(EnvTimezone)
	if name == "" {
		name = DefaultTimezone
	}
	return SetLocation(name)
}

// SetLocation sets the scheduling IANA location (must be a valid IANA name).
func SetLocation(name string) error {
	l, err := time.LoadLocation(name)
	if err != nil {
		return fmt.Errorf("MEDCORE_TIMEZONE invalide %q: %w", name, err)
	}
	locMu.Lock()
	loc = l
	locMu.Unlock()
	return nil
}

// Location returns the configured scheduling timezone (IANA semantics, DST-aware).
func Location() *time.Location {
	locMu.RLock()
	defer locMu.RUnlock()
	return loc
}

// LocationName returns the IANA name of the current scheduling timezone.
func LocationName() string {
	return Location().String()
}
