package patient_queue

import (
	"testing"
	"time"
)

func TestExpandWeeklyHappyPath(t *testing.T) {
	anchor := time.Date(2026, 9, 14, 9, 0, 0, 0, time.UTC) // Monday
	count := 4
	out, err := ExpandWeeklyOccurrences(RecurrenceRule{
		Freq: SeriesFreqWeekly, IntervalWeeks: 1,
		ByWeekdays: []int{int(time.Monday)}, Count: &count,
		Timezone: "UTC", AnchorStartAt: anchor,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 4 {
		t.Fatalf("want 4 got %d", len(out))
	}
	for i, o := range out {
		if o.Index != i+1 {
			t.Fatalf("index %d want %d", o.Index, i+1)
		}
		want := anchor.AddDate(0, 0, 7*i)
		if !o.StartAt.Equal(want) {
			t.Fatalf("occ %d want %v got %v", i+1, want, o.StartAt)
		}
	}
}

func TestExpandWeeklyMultipleWeekdaysSorted(t *testing.T) {
	anchor := time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC) // Monday
	count := 4
	out, err := ExpandWeeklyOccurrences(RecurrenceRule{
		Freq: SeriesFreqWeekly, IntervalWeeks: 1,
		ByWeekdays: []int{int(time.Wednesday), int(time.Monday)}, // unsorted input
		Count:      &count, Timezone: "UTC", AnchorStartAt: anchor,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 4 {
		t.Fatalf("want 4 got %d", len(out))
	}
	// Mon 14, Wed 16, Mon 21, Wed 23
	wants := []time.Time{
		time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC),
		time.Date(2026, 9, 16, 10, 0, 0, 0, time.UTC),
		time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC),
		time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC),
	}
	for i, w := range wants {
		if !out[i].StartAt.Equal(w) {
			t.Fatalf("occ %d want %v got %v", i+1, w, out[i].StartAt)
		}
	}
}

func TestExpandWeeklyInterval2(t *testing.T) {
	anchor := time.Date(2026, 9, 14, 9, 0, 0, 0, time.UTC)
	count := 3
	out, err := ExpandWeeklyOccurrences(RecurrenceRule{
		Freq: SeriesFreqWeekly, IntervalWeeks: 2,
		ByWeekdays: []int{int(time.Monday)}, Count: &count,
		Timezone: "UTC", AnchorStartAt: anchor,
	})
	if err != nil {
		t.Fatal(err)
	}
	wants := []time.Time{
		anchor,
		anchor.AddDate(0, 0, 14),
		anchor.AddDate(0, 0, 28),
	}
	for i, w := range wants {
		if !out[i].StartAt.Equal(w) {
			t.Fatalf("occ %d want %v got %v", i+1, w, out[i].StartAt)
		}
	}
}

func TestExpandWeeklyWeekdayConventionSundayZero(t *testing.T) {
	// 2026-09-13 is Sunday
	anchor := time.Date(2026, 9, 13, 11, 0, 0, 0, time.UTC)
	count := 2
	out, err := ExpandWeeklyOccurrences(RecurrenceRule{
		Freq: SeriesFreqWeekly, IntervalWeeks: 1,
		ByWeekdays: []int{0}, Count: &count, // Sunday = 0
		Timezone: "UTC", AnchorStartAt: anchor,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !out[0].StartAt.Equal(anchor) || !out[1].StartAt.Equal(anchor.AddDate(0, 0, 7)) {
		t.Fatalf("sunday convention failed: %+v", out)
	}
}

func TestExpandWeeklyCountXORUntil(t *testing.T) {
	anchor := time.Date(2026, 9, 14, 9, 0, 0, 0, time.UTC)
	count := 2
	until := anchor.AddDate(0, 0, 14)
	_, err := ExpandWeeklyOccurrences(RecurrenceRule{
		Freq: SeriesFreqWeekly, IntervalWeeks: 1, ByWeekdays: []int{1},
		Count: &count, Until: &until, Timezone: "UTC", AnchorStartAt: anchor,
	})
	if statusOf(err) != 400 {
		t.Fatalf("both count and until want 400 got %d %v", statusOf(err), err)
	}
	_, err = ExpandWeeklyOccurrences(RecurrenceRule{
		Freq: SeriesFreqWeekly, IntervalWeeks: 1, ByWeekdays: []int{1},
		Timezone: "UTC", AnchorStartAt: anchor,
	})
	if statusOf(err) != 400 {
		t.Fatalf("neither want 400 got %d %v", statusOf(err), err)
	}
}

func TestExpandWeeklyCountBounds(t *testing.T) {
	anchor := time.Date(2026, 9, 14, 9, 0, 0, 0, time.UTC)
	one := 1
	out, err := ExpandWeeklyOccurrences(RecurrenceRule{
		Freq: SeriesFreqWeekly, IntervalWeeks: 1, ByWeekdays: []int{1},
		Count: &one, Timezone: "UTC", AnchorStartAt: anchor,
	})
	if err != nil || len(out) != 1 {
		t.Fatalf("count 1: %v len=%d", err, len(out))
	}
	fiftyTwo := 52
	out, err = ExpandWeeklyOccurrences(RecurrenceRule{
		Freq: SeriesFreqWeekly, IntervalWeeks: 1, ByWeekdays: []int{1},
		Count: &fiftyTwo, Timezone: "UTC", AnchorStartAt: anchor,
	})
	if err != nil || len(out) != 52 {
		t.Fatalf("count 52: %v len=%d", err, len(out))
	}
	fiftyThree := 53
	_, err = ExpandWeeklyOccurrences(RecurrenceRule{
		Freq: SeriesFreqWeekly, IntervalWeeks: 1, ByWeekdays: []int{1},
		Count: &fiftyThree, Timezone: "UTC", AnchorStartAt: anchor,
	})
	if statusOf(err) != 400 {
		t.Fatalf("count 53 want 400 got %d", statusOf(err))
	}
}

func TestExpandWeeklyHorizonRejected(t *testing.T) {
	anchor := time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC) // Monday
	// until far past 12 months — expansion would require occurrences beyond the civil horizon.
	until := anchor.AddDate(0, 13, 0)
	_, err := ExpandWeeklyOccurrences(RecurrenceRule{
		Freq: SeriesFreqWeekly, IntervalWeeks: 1, ByWeekdays: []int{1},
		Until: &until, Timezone: "UTC", AnchorStartAt: anchor,
	})
	if statusOf(err) != 400 {
		t.Fatalf("horizon until want 400 got %d %v", statusOf(err), err)
	}
}

func TestExpandWeeklyHorizonBoundaryInclusiveCountAndUntil(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatal(err)
	}
	// 2026-03-28 10:00 CET (+1); +12 months → 2027-03-28 10:00 CEST (+2) after spring-forward day.
	// UTC offset therefore differs between anchor and civil horizon.
	anchor := time.Date(2026, 3, 28, 10, 0, 0, 0, loc) // Saturday
	horizonLocal := anchor.AddDate(0, 12, 0)           // Sunday 2027-03-28 10:00 Paris
	horizonUTC := recurrenceHorizonUTC(anchor)
	if !horizonUTC.Equal(horizonLocal.UTC()) {
		t.Fatalf("horizon helper want %v got %v", horizonLocal.UTC(), horizonUTC)
	}
	if _, anchorOff := anchor.Zone(); true {
		_, horizonOff := horizonLocal.Zone()
		if anchorOff == horizonOff {
			t.Fatalf("expected DST offset change between anchor and horizon (got %d)", anchorOff)
		}
	}

	// interval 53 weeks + Sat/Sun: 2nd kept occurrence is Sun 2027-03-28 10:00 = civil horizon.
	count := 2
	out, err := ExpandWeeklyOccurrences(RecurrenceRule{
		Freq: SeriesFreqWeekly, IntervalWeeks: 53,
		ByWeekdays: []int{int(time.Saturday), int(time.Sunday)},
		Count:      &count, Timezone: "Europe/Paris", AnchorStartAt: anchor,
	})
	if err != nil {
		t.Fatalf("count path at boundary: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("want 2 got %d (%v)", len(out), out)
	}
	last := out[len(out)-1].StartAt
	if !last.Equal(horizonUTC) {
		t.Fatalf("last occ want exact horizon %v got %v", horizonUTC, last)
	}

	until := horizonUTC
	outUntil, err := ExpandWeeklyOccurrences(RecurrenceRule{
		Freq: SeriesFreqWeekly, IntervalWeeks: 53,
		ByWeekdays: []int{int(time.Saturday), int(time.Sunday)},
		Until:      &until, Timezone: "Europe/Paris", AnchorStartAt: anchor,
	})
	if err != nil {
		t.Fatalf("until path at boundary: %v", err)
	}
	if len(outUntil) != 2 {
		t.Fatalf("until want 2 got %d", len(outUntil))
	}
	if !outUntil[len(outUntil)-1].StartAt.Equal(horizonUTC) {
		t.Fatalf("until last want horizon %v got %v", horizonUTC, outUntil[len(outUntil)-1].StartAt)
	}
}

func TestExpandWeeklyHorizonBeyondRejectedCountAndUntil(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatal(err)
	}
	anchor := time.Date(2026, 3, 28, 10, 0, 0, 0, loc) // Saturday
	// count=3 would continue to weekOffset 106 → past +12 months.
	count := 3
	_, err = ExpandWeeklyOccurrences(RecurrenceRule{
		Freq: SeriesFreqWeekly, IntervalWeeks: 53,
		ByWeekdays: []int{int(time.Saturday), int(time.Sunday)},
		Count:      &count, Timezone: "Europe/Paris", AnchorStartAt: anchor,
	})
	if statusOf(err) != 400 {
		t.Fatalf("count beyond horizon want 400 got %d %v", statusOf(err), err)
	}

	// until past the civil horizon but still covering the next 53-week step → must reject.
	until := recurrenceHorizonUTC(anchor).Add(53 * 7 * 24 * time.Hour)
	_, err = ExpandWeeklyOccurrences(RecurrenceRule{
		Freq: SeriesFreqWeekly, IntervalWeeks: 53,
		ByWeekdays: []int{int(time.Saturday), int(time.Sunday)},
		Until:      &until, Timezone: "Europe/Paris", AnchorStartAt: anchor,
	})
	if statusOf(err) != 400 {
		t.Fatalf("until beyond horizon want 400 got %d %v", statusOf(err), err)
	}
}

func TestExpandWeeklyCount52Allowed53Rejected(t *testing.T) {
	anchor := time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC) // Monday
	fiftyTwo := 52
	out, err := ExpandWeeklyOccurrences(RecurrenceRule{
		Freq: SeriesFreqWeekly, IntervalWeeks: 1, ByWeekdays: []int{1},
		Count: &fiftyTwo, Timezone: "UTC", AnchorStartAt: anchor,
	})
	if err != nil || len(out) != 52 {
		t.Fatalf("count 52: %v len=%d", err, len(out))
	}
	fiftyThree := 53
	_, err = ExpandWeeklyOccurrences(RecurrenceRule{
		Freq: SeriesFreqWeekly, IntervalWeeks: 1, ByWeekdays: []int{1},
		Count: &fiftyThree, Timezone: "UTC", AnchorStartAt: anchor,
	})
	if statusOf(err) != 400 {
		t.Fatalf("count 53 want 400 got %d", statusOf(err))
	}
}

func TestExpandWeeklyInvalidTimezone(t *testing.T) {
	anchor := time.Date(2026, 9, 14, 9, 0, 0, 0, time.UTC)
	count := 1
	_, err := ExpandWeeklyOccurrences(RecurrenceRule{
		Freq: SeriesFreqWeekly, IntervalWeeks: 1, ByWeekdays: []int{1},
		Count: &count, Timezone: "Not/AZone", AnchorStartAt: anchor,
	})
	if statusOf(err) != 400 {
		t.Fatalf("bad tz want 400 got %d", statusOf(err))
	}
}

func TestExpandWeeklyDSTSpringNonExistentRejected(t *testing.T) {
	// Europe/Paris: 2026-03-29 clocks spring forward 02:00 → 03:00; 02:30 does not exist.
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatal(err)
	}
	anchorLocal := time.Date(2026, 3, 22, 2, 30, 0, 0, loc) // Sunday before transition
	count := 2
	_, err = ExpandWeeklyOccurrences(RecurrenceRule{
		Freq: SeriesFreqWeekly, IntervalWeeks: 1, ByWeekdays: []int{int(time.Sunday)},
		Count: &count, Timezone: "Europe/Paris", AnchorStartAt: anchorLocal,
	})
	if statusOf(err) != 400 {
		t.Fatalf("DST spring gap want 400 got %d %v", statusOf(err), err)
	}
}

func TestExpandWeeklyDSTFallDeterministic(t *testing.T) {
	// Europe/Paris: 2026-10-25 fall back; 02:30 occurs twice. Go time.Date picks earlier.
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatal(err)
	}
	anchorLocal := time.Date(2026, 10, 18, 2, 30, 0, 0, loc) // Sunday
	count := 2
	out, err := ExpandWeeklyOccurrences(RecurrenceRule{
		Freq: SeriesFreqWeekly, IntervalWeeks: 1, ByWeekdays: []int{int(time.Sunday)},
		Count: &count, Timezone: "Europe/Paris", AnchorStartAt: anchorLocal,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("want 2 got %d", len(out))
	}
	// Second occurrence: 2026-10-25 02:30 Paris — earlier offset (CEST +2) before fall-back.
	secondLocal := time.Date(2026, 10, 25, 2, 30, 0, 0, loc)
	if !out[1].StartAt.Equal(secondLocal.UTC()) {
		t.Fatalf("fall-back want %v got %v (offset first=%s second=%s)",
			secondLocal.UTC(), out[1].StartAt, out[0].StartAt.In(loc), out[1].StartAt.In(loc))
	}
	// Deterministic replay
	out2, err := ExpandWeeklyOccurrences(RecurrenceRule{
		Freq: SeriesFreqWeekly, IntervalWeeks: 1, ByWeekdays: []int{int(time.Sunday)},
		Count: &count, Timezone: "Europe/Paris", AnchorStartAt: anchorLocal,
	})
	if err != nil || !out2[1].StartAt.Equal(out[1].StartAt) {
		t.Fatalf("fall-back not deterministic: %v %v vs %v", err, out2, out)
	}
}

func TestExpandWeeklyDuplicateWeekdaysRejected(t *testing.T) {
	anchor := time.Date(2026, 9, 14, 9, 0, 0, 0, time.UTC)
	count := 1
	_, err := ExpandWeeklyOccurrences(RecurrenceRule{
		Freq: SeriesFreqWeekly, IntervalWeeks: 1, ByWeekdays: []int{1, 1},
		Count: &count, Timezone: "UTC", AnchorStartAt: anchor,
	})
	if statusOf(err) != 400 {
		t.Fatalf("dup weekday want 400 got %d", statusOf(err))
	}
}

func TestExpandWeeklyUntilInclusive(t *testing.T) {
	anchor := time.Date(2026, 9, 14, 9, 0, 0, 0, time.UTC)
	until := time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)
	out, err := ExpandWeeklyOccurrences(RecurrenceRule{
		Freq: SeriesFreqWeekly, IntervalWeeks: 1, ByWeekdays: []int{1},
		Until: &until, Timezone: "UTC", AnchorStartAt: anchor,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 3 { // 14, 21, 28
		t.Fatalf("want 3 got %d %+v", len(out), out)
	}
}

func TestCivilLocalInLocationSpringGap(t *testing.T) {
	loc, _ := time.LoadLocation("America/New_York")
	// 2026-03-08 02:30 does not exist (spring forward)
	_, err := civilLocalInLocation(loc, 2026, time.March, 8, 2, 30, 0)
	if statusOf(err) != 400 {
		t.Fatalf("want 400 got %d %v", statusOf(err), err)
	}
}
