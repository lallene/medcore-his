package complete

import (
	"testing"
	"time"
)

func TestClinicStateReserveAndReleaseSlot(t *testing.T) {
	start := time.Date(2026, 10, 23, 8, 15, 0, 0, time.UTC)

	tests := []struct {
		name     string
		duration int
		want     int
	}{
		{name: "45 minutes", duration: 45, want: 3},
		{name: "default duration", duration: 0, want: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &clinicState{
				slots: map[uint]map[int64]bool{},
			}

			const practitionerID uint = 42

			c.reserveSlot(practitionerID, start, tt.duration)

			if got := len(c.slots[practitionerID]); got != tt.want {
				t.Fatalf("reserved slots = %d, want %d", got, tt.want)
			}

			c.releaseSlot(practitionerID, start, tt.duration)

			if got := len(c.slots[practitionerID]); got != 0 {
				t.Fatalf("reserved slots after release = %d, want 0", got)
			}
		})
	}
}

func TestClinicStateSlotAvailableDetectsOverlap(t *testing.T) {
	const practitionerID uint = 42

	c := &clinicState{
		slots: map[uint]map[int64]bool{},
	}

	start := time.Date(2026, 10, 23, 8, 0, 0, 0, time.UTC)

	if !c.slotAvailable(practitionerID, start, 45) {
		t.Fatal("fresh 45-minute interval should be available")
	}

	c.reserveSlot(practitionerID, start, 45)

	tests := []struct {
		name     string
		start    time.Time
		duration int
		want     bool
	}{
		{
			name:     "same interval",
			start:    start,
			duration: 45,
			want:     false,
		},
		{
			name:     "overlaps last quarter",
			start:    start.Add(30 * time.Minute),
			duration: 30,
			want:     false,
		},
		{
			name:     "starts immediately after",
			start:    start.Add(45 * time.Minute),
			duration: 30,
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := c.slotAvailable(practitionerID, tt.start, tt.duration); got != tt.want {
				t.Fatalf("slotAvailable() = %v, want %v", got, tt.want)
			}
		})
	}

	c.releaseSlot(practitionerID, start, 45)

	if !c.slotAvailable(practitionerID, start, 45) {
		t.Fatal("interval should be available after releasing its reservation")
	}
}
