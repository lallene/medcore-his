package scheduling

import (
	"sort"
	"time"
)

// Interval is a half-open time range [Start, End).
type Interval struct {
	Start time.Time
	End   time.Time
}

func (iv Interval) Valid() bool { return iv.End.After(iv.Start) }

func (iv Interval) Duration() time.Duration {
	if !iv.Valid() {
		return 0
	}
	return iv.End.Sub(iv.Start)
}

// Overlaps reports whether a and b overlap under [start,end) semantics.
func Overlaps(a, b Interval) bool {
	return a.Start.Before(b.End) && b.Start.Before(a.End)
}

// SortIntervals sorts by Start ASC, then End ASC (stable for equal starts).
func SortIntervals(xs []Interval) {
	sort.SliceStable(xs, func(i, j int) bool {
		if xs[i].Start.Equal(xs[j].Start) {
			return xs[i].End.Before(xs[j].End)
		}
		return xs[i].Start.Before(xs[j].Start)
	})
}

// Normalize drops invalid intervals and sorts.
func Normalize(xs []Interval) []Interval {
	out := make([]Interval, 0, len(xs))
	for _, iv := range xs {
		if iv.Valid() {
			out = append(out, Interval{Start: iv.Start.UTC(), End: iv.End.UTC()})
		}
	}
	SortIntervals(out)
	return out
}

// Merge unions overlapping or adjacent half-open intervals.
// Adjacent [08,10)+[10,12) → [08,12).
func Merge(xs []Interval) []Interval {
	xs = Normalize(xs)
	if len(xs) == 0 {
		return nil
	}
	out := []Interval{xs[0]}
	for i := 1; i < len(xs); i++ {
		cur := xs[i]
		last := &out[len(out)-1]
		// Overlap or adjacent (cur.Start <= last.End)
		if !cur.Start.After(last.End) {
			if cur.End.After(last.End) {
				last.End = cur.End
			}
			continue
		}
		out = append(out, cur)
	}
	return out
}

// Intersect returns the intersection of two intervals, or empty if none.
func Intersect(a, b Interval) (Interval, bool) {
	start := a.Start
	if b.Start.After(start) {
		start = b.Start
	}
	end := a.End
	if b.End.Before(end) {
		end = b.End
	}
	iv := Interval{Start: start, End: end}
	if !iv.Valid() {
		return Interval{}, false
	}
	return iv, true
}

// Clip restricts iv to [bound.Start, bound.End).
func Clip(iv, bound Interval) (Interval, bool) {
	return Intersect(iv, bound)
}

// Subtract removes each blocker from base intervals (half-open).
// Supports full removal, left/right trim, middle split, no-op, multiple blockers.
func Subtract(bases []Interval, blockers []Interval) []Interval {
	bases = Merge(bases)
	blockers = Merge(blockers)
	if len(bases) == 0 {
		return nil
	}
	if len(blockers) == 0 {
		return bases
	}
	out := make([]Interval, 0, len(bases))
	for _, base := range bases {
		parts := []Interval{base}
		for _, blk := range blockers {
			next := make([]Interval, 0, len(parts)*2)
			for _, p := range parts {
				next = append(next, subtractOne(p, blk)...)
			}
			parts = next
		}
		out = append(out, parts...)
	}
	return Merge(out)
}

func subtractOne(base, blk Interval) []Interval {
	if !Overlaps(base, blk) {
		return []Interval{base}
	}
	// Full cover
	if !blk.Start.After(base.Start) && !blk.End.Before(base.End) {
		return nil
	}
	var out []Interval
	// Left remnant: [base.Start, blk.Start)
	if blk.Start.After(base.Start) {
		left := Interval{Start: base.Start, End: blk.Start}
		if left.Valid() {
			out = append(out, left)
		}
	}
	// Right remnant: [blk.End, base.End)
	if blk.End.Before(base.End) {
		right := Interval{Start: blk.End, End: base.End}
		if right.Valid() {
			out = append(out, right)
		}
	}
	return out
}

// Slot is an ephemeral generated candidate (never persisted).
type Slot struct {
	Start time.Time
	End   time.Time
}

// GenerateSlots fills each free interval with [start, start+duration) stepping by step.
// Alignment is from the beginning of each free interval (not Unix epoch).
// No partial slots: slot.End must be <= free.End.
func GenerateSlots(free []Interval, duration, step time.Duration) []Slot {
	if duration <= 0 || step <= 0 {
		return nil
	}
	free = Merge(free)
	var out []Slot
	for _, iv := range free {
		for t := iv.Start; ; t = t.Add(step) {
			end := t.Add(duration)
			if end.After(iv.End) {
				break
			}
			out = append(out, Slot{Start: t, End: end})
			// Prevent infinite loop if step somehow zero (already guarded)
			if !t.Before(iv.End) {
				break
			}
		}
	}
	return out
}
