package patient_queue

import (
	"testing"
	"time"
)

func TestSeriesOccurrenceDivergedUnit23OC(t *testing.T) {
	prac := uint(10)
	typeID := uint(5)
	series := &AppointmentSeries{
		PractitionerID: prac, AppointmentTypeID: &typeID, DurationMinutes: 30,
		AnchorStartAt: time.Date(2026, 12, 7, 9, 0, 0, 0, time.UTC),
	}
	idx := 2
	start := time.Date(2026, 12, 14, 9, 0, 0, 0, time.UTC)
	end := start.Add(30 * time.Minute)
	oldByIndex := map[int]time.Time{1: series.AnchorStartAt, 2: start}

	aligned := Appointment{
		SeriesOccurrenceIndex: &idx, Status: ApptScheduled,
		ScheduledAt: start, ScheduledEndAt: &end,
		ExpectedDoctorID: &prac, AppointmentTypeID: &typeID,
	}
	if seriesOccurrenceDiverged(aligned, series, oldByIndex) {
		t.Fatal("aligned occurrence must not diverge")
	}

	moved := aligned
	moved.ScheduledAt = start.Add(time.Hour)
	if !seriesOccurrenceDiverged(moved, series, oldByIndex) {
		t.Fatal("rescheduled exception must diverge")
	}

	otherPrac := uint(11)
	swapped := aligned
	swapped.ExpectedDoctorID = &otherPrac
	if !seriesOccurrenceDiverged(swapped, series, oldByIndex) {
		t.Fatal("practitioner change must diverge")
	}
}

func TestBuildSeriesExpectedStartsByIndexUnit23OC(t *testing.T) {
	count := 3
	weekdays, _ := marshalByWeekdays([]int{int(time.Monday)})
	series := &AppointmentSeries{
		Freq: SeriesFreqWeekly, IntervalWeeks: 1, ByWeekdays: weekdays,
		Count: &count, Timezone: "UTC",
		AnchorStartAt:  time.Date(2026, 12, 7, 9, 0, 0, 0, time.UTC),
		PractitionerID: 1, DurationMinutes: 30,
	}
	idx1, idx2, idx3 := 1, 2, 3
	appts := []Appointment{
		{SeriesOccurrenceIndex: &idx1, ScheduledAt: series.AnchorStartAt},
		{SeriesOccurrenceIndex: &idx2, ScheduledAt: series.AnchorStartAt.AddDate(0, 0, 7)},
		{SeriesOccurrenceIndex: &idx3, ScheduledAt: series.AnchorStartAt.AddDate(0, 0, 14)},
	}
	m, err := buildSeriesExpectedStartsByIndex(series, appts)
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 3 || !m[1].Equal(series.AnchorStartAt) {
		t.Fatalf("unexpected map: %v", m)
	}

	// Mid-series regen: anchor aligns to index 3
	series.AnchorStartAt = time.Date(2026, 12, 21, 10, 0, 0, 0, time.UTC)
	countRem := 2
	series.Count = &countRem
	appts2 := []Appointment{
		{SeriesOccurrenceIndex: &idx1, ScheduledAt: time.Date(2026, 12, 7, 9, 0, 0, 0, time.UTC)},
		{SeriesOccurrenceIndex: &idx2, ScheduledAt: time.Date(2026, 12, 14, 9, 0, 0, 0, time.UTC)},
		{SeriesOccurrenceIndex: &idx3, ScheduledAt: series.AnchorStartAt},
	}
	m2, err := buildSeriesExpectedStartsByIndex(series, appts2)
	if err != nil {
		t.Fatal(err)
	}
	if !m2[3].Equal(series.AnchorStartAt) {
		t.Fatalf("base index 3 want anchor, got %v", m2)
	}
	if _, ok := m2[4]; !ok {
		t.Fatalf("want index 4 mapped, got %v", m2)
	}
}
