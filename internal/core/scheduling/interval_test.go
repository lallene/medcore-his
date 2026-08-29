package scheduling

import (
	"testing"
	"time"
)

func tUTC(h, m int) time.Time {
	return time.Date(2026, 9, 14, h, m, 0, 0, time.UTC)
}

func iv(sh, sm, eh, em int) Interval {
	return Interval{Start: tUTC(sh, sm), End: tUTC(eh, em)}
}

func TestMergeOverlapping(t *testing.T) {
	got := Merge([]Interval{iv(8, 0, 12, 0), iv(11, 0, 14, 0)})
	if len(got) != 1 || !got[0].Start.Equal(tUTC(8, 0)) || !got[0].End.Equal(tUTC(14, 0)) {
		t.Fatalf("%v", got)
	}
}

func TestMergeAdjacent(t *testing.T) {
	got := Merge([]Interval{iv(8, 0, 10, 0), iv(10, 0, 12, 0)})
	if len(got) != 1 || !got[0].End.Equal(tUTC(12, 0)) {
		t.Fatalf("adjacent merge: %v", got)
	}
}

func TestPreserveSeparated(t *testing.T) {
	got := Merge([]Interval{iv(8, 0, 10, 0), iv(11, 0, 12, 0)})
	if len(got) != 2 {
		t.Fatalf("%v", got)
	}
}

func TestSubtractVariants(t *testing.T) {
	base := []Interval{iv(8, 0, 12, 0)}
	if len(Subtract(base, []Interval{iv(14, 0, 15, 0)})) != 1 {
		t.Fatal("no overlap")
	}
	if len(Subtract(base, []Interval{iv(8, 0, 12, 0)})) != 0 {
		t.Fatal("full overlap")
	}
	left := Subtract(base, []Interval{iv(8, 0, 9, 0)})
	if len(left) != 1 || !left[0].Start.Equal(tUTC(9, 0)) {
		t.Fatalf("left trim %v", left)
	}
	right := Subtract(base, []Interval{iv(11, 0, 12, 0)})
	if len(right) != 1 || !right[0].End.Equal(tUTC(11, 0)) {
		t.Fatalf("right trim %v", right)
	}
	mid := Subtract(base, []Interval{iv(9, 0, 10, 0)})
	if len(mid) != 2 || !mid[0].End.Equal(tUTC(9, 0)) || !mid[1].Start.Equal(tUTC(10, 0)) {
		t.Fatalf("middle split %v", mid)
	}
	multi := Subtract(base, []Interval{iv(8, 30, 9, 0), iv(10, 0, 10, 30)})
	if len(multi) != 3 {
		t.Fatalf("multi %v", multi)
	}
}

func TestClipAndEmpty(t *testing.T) {
	c, ok := Clip(iv(8, 0, 12, 0), iv(9, 0, 17, 0))
	if !ok || !c.Start.Equal(tUTC(9, 0)) || !c.End.Equal(tUTC(12, 0)) {
		t.Fatalf("%v %v", c, ok)
	}
	if _, ok := Clip(iv(8, 0, 9, 0), iv(10, 0, 11, 0)); ok {
		t.Fatal("empty")
	}
}

func TestHalfOpenBoundary(t *testing.T) {
	if Overlaps(iv(8, 0, 10, 0), iv(10, 0, 12, 0)) {
		t.Fatal("adjacent half-open must not overlap")
	}
}

func TestGenerateSlots(t *testing.T) {
	free := []Interval{iv(8, 0, 9, 0)}
	d30 := 30 * time.Minute
	slots := GenerateSlots(free, d30, d30)
	if len(slots) != 2 {
		t.Fatalf("exact fit want 2 got %d", len(slots))
	}
	if len(GenerateSlots([]Interval{iv(8, 0, 8, 20)}, d30, d30)) != 0 {
		t.Fatal("shorter than duration")
	}
	step15 := GenerateSlots(free, d30, 15*time.Minute)
	if len(step15) != 3 { // 08:00, 08:15, 08:30
		t.Fatalf("step<duration want 3 got %d", len(step15))
	}
	// alignment from free start 08:10
	freeLate := []Interval{iv(8, 10, 9, 0)}
	aligned := GenerateSlots(freeLate, d30, d30)
	if len(aligned) != 1 || !aligned[0].Start.Equal(tUTC(8, 10)) || !aligned[0].End.Equal(tUTC(8, 40)) {
		t.Fatalf("alignment %v", aligned)
	}
	// no partial at end: 08:00-08:50 with 30/30 → only 08:00-08:30
	partial := GenerateSlots([]Interval{iv(8, 0, 8, 50)}, d30, d30)
	if len(partial) != 1 {
		t.Fatalf("no partial %v", partial)
	}
	multi := GenerateSlots([]Interval{iv(8, 0, 8, 30), iv(14, 0, 14, 30)}, d30, d30)
	if len(multi) != 2 {
		t.Fatalf("multi free %v", multi)
	}
}

func TestDSTProjection(t *testing.T) {
	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatal(err)
	}
	// Winter: 2026-01-12 Monday 08:00 CET = UTC+1
	winter := time.Date(2026, 1, 12, 0, 0, 0, 0, paris)
	w, err := ProjectWallClock(winter, "08:00:00", paris)
	if err != nil {
		t.Fatal(err)
	}
	if _, off := w.Zone(); off != 3600 {
		t.Fatalf("winter offset want 3600 got %d at %v", off, w)
	}
	// Summer: 2026-07-13 Monday 08:00 CEST = UTC+2
	summer := time.Date(2026, 7, 13, 0, 0, 0, 0, paris)
	s, err := ProjectWallClock(summer, "08:00:00", paris)
	if err != nil {
		t.Fatal(err)
	}
	if _, off := s.Zone(); off != 7200 {
		t.Fatalf("summer offset want 7200 got %d", off)
	}
	// Spring forward 2026-03-29: 02:00–03:00 nonexistent
	spring := time.Date(2026, 3, 29, 0, 0, 0, 0, paris)
	gap, err := ProjectWallClock(spring, "02:30:00", paris)
	if err != nil {
		t.Fatal(err)
	}
	// Go normalizes forward — must not be a fixed +1h from UTC midnight interpretation
	if gap.Location().String() != "Europe/Paris" {
		t.Fatal(gap.Location())
	}
	// Autumn back 2026-10-25: 02:00–03:00 ambiguous
	autumn := time.Date(2026, 10, 25, 0, 0, 0, 0, paris)
	fold, err := ProjectWallClock(autumn, "02:30:00", paris)
	if err != nil {
		t.Fatal(err)
	}
	if fold.Location().String() != "Europe/Paris" {
		t.Fatal(fold)
	}
	// Prove not fixed offset: winter and summer same wall clock → different UTC
	if w.UTC().Hour() == s.UTC().Hour() {
		t.Fatal("winter/summer UTC hours should differ under DST")
	}
}
