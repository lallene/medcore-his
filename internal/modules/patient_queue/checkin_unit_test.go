package patient_queue

import (
	"testing"
	"time"
)

func TestAssertCheckInTimingEarlyWindow(t *testing.T) {
	t.Setenv(EnvEarlyCheckinMinutes, "60")
	now := time.Date(2026, 8, 29, 13, 0, 0, 0, time.UTC)

	// Boundary: scheduled 14:00 → earliest 13:00 → allowed
	if err := assertCheckInTiming(time.Date(2026, 8, 29, 14, 0, 0, 0, time.UTC), now); err != nil {
		t.Fatalf("boundary: %v", err)
	}
	// Within window: scheduled 13:30 → earliest 12:30 → allowed
	if err := assertCheckInTiming(time.Date(2026, 8, 29, 13, 30, 0, 0, time.UTC), now); err != nil {
		t.Fatalf("within: %v", err)
	}
	// Before window: scheduled 15:00 → earliest 14:00 → reject
	if err := assertCheckInTiming(time.Date(2026, 8, 29, 15, 0, 0, 0, time.UTC), now); err == nil {
		t.Fatal("too early should reject")
	}
	// Late: scheduled 12:00, now 13:00 → allowed
	if err := assertCheckInTiming(time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC), now); err != nil {
		t.Fatalf("late: %v", err)
	}
}

func TestAssertCheckInLifecycle(t *testing.T) {
	if err := assertCheckInLifecycle(ApptScheduled); err != nil {
		t.Fatal(err)
	}
	for _, st := range []string{ApptCancelled, ApptNoShow, ApptCompleted, ApptInProgress} {
		if err := assertCheckInLifecycle(st); err == nil {
			t.Fatalf("%s should reject", st)
		}
	}
}

func TestEarlyCheckinMinutesDefault(t *testing.T) {
	t.Setenv(EnvEarlyCheckinMinutes, "")
	if earlyCheckinMinutes() != DefaultEarlyCheckinMinutes {
		t.Fatalf("default want %d", DefaultEarlyCheckinMinutes)
	}
	t.Setenv(EnvEarlyCheckinMinutes, "90")
	if earlyCheckinMinutes() != 90 {
		t.Fatal("override")
	}
	t.Setenv(EnvEarlyCheckinMinutes, "bad")
	if earlyCheckinMinutes() != DefaultEarlyCheckinMinutes {
		t.Fatal("invalid fallback")
	}
}
