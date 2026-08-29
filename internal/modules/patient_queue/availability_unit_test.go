package patient_queue

import (
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/core/scheduling"
)

func TestAppointmentBlocksAvailabilityMatrix(t *testing.T) {
	cases := map[string]bool{
		ApptScheduled: true, ApptArrived: true, ApptCheckedIn: true,
		ApptInProgress: true, ApptCompleted: true,
		ApptCancelled: false, ApptNoShow: false,
	}
	for st, want := range cases {
		if AppointmentBlocksAvailability(st) != want {
			t.Fatalf("%s want %v", st, want)
		}
	}
}

func TestLegacyEndFallback(t *testing.T) {
	start := time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)
	appt := Appointment{ScheduledAt: start, AppointmentTypeID: nil}
	end := ResolveAppointmentEnd(appt, nil)
	if !end.Equal(start.Add(30 * time.Minute)) {
		t.Fatalf("fallback %v", end)
	}
	tid := uint(3)
	appt.AppointmentTypeID = &tid
	end = ResolveAppointmentEnd(appt, map[uint]int{3: 45})
	if !end.Equal(start.Add(45 * time.Minute)) {
		t.Fatalf("type dur %v", end)
	}
	explicit := start.Add(20 * time.Minute)
	appt.ScheduledEndAt = &explicit
	end = ResolveAppointmentEnd(appt, map[uint]int{3: 45})
	if !end.Equal(explicit) {
		t.Fatalf("explicit end %v", end)
	}
}

func TestNegativePrecedenceAlgebra(t *testing.T) {
	// Working 08–12 + EXTRA 11–14 − MEETING 10–11:30 → 08–10 + 11:30–14
	base := scheduling.Merge([]scheduling.Interval{
		{Start: time.Date(2026, 9, 14, 8, 0, 0, 0, time.UTC), End: time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)},
		{Start: time.Date(2026, 9, 14, 11, 0, 0, 0, time.UTC), End: time.Date(2026, 9, 14, 14, 0, 0, 0, time.UTC)},
	})
	neg := []scheduling.Interval{
		{Start: time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC), End: time.Date(2026, 9, 14, 11, 30, 0, 0, time.UTC)},
	}
	got := scheduling.Subtract(base, neg)
	if len(got) != 2 {
		t.Fatalf("%v", got)
	}
	if !got[0].End.Equal(time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)) {
		t.Fatal(got[0])
	}
	if !got[1].Start.Equal(time.Date(2026, 9, 14, 11, 30, 0, 0, time.UTC)) {
		t.Fatal(got[1])
	}
	if !ExceptionPrecedenceNegativeWins() {
		t.Fatal("precedence")
	}
}

func TestAdjacentAppointmentNoFalseOverlap(t *testing.T) {
	a := scheduling.Interval{
		Start: time.Date(2026, 9, 14, 9, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 9, 14, 9, 30, 0, 0, time.UTC),
	}
	b := scheduling.Interval{
		Start: time.Date(2026, 9, 14, 9, 30, 0, 0, time.UTC),
		End:   time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC),
	}
	if scheduling.Overlaps(a, b) {
		t.Fatal("adjacent appointments must not overlap")
	}
}
