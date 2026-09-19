package businessdate

import "time"

// Date is a civil calendar day (Y/M/D) with no time-of-day or location.
type Date struct {
	Year  int
	Month time.Month
	Day   int
}

// FromTime extracts civil Y/M/D from t using t.Date() in t's own location.
// PostgreSQL DATE values must use this path — do not reinterpret into another zone first.
func FromTime(t time.Time) Date {
	y, m, d := t.Date()
	return Date{Year: y, Month: m, Day: d}
}

// TodayIn returns the civil business day for now in loc.
func TodayIn(loc *time.Location) Date {
	if loc == nil {
		loc = time.UTC
	}
	return TodayAt(time.Now(), loc)
}

// TodayAt returns the civil business day for an explicit instant in loc (tests).
func TodayAt(now time.Time, loc *time.Location) Date {
	if loc == nil {
		loc = time.UTC
	}
	return FromTime(now.In(loc))
}

// Before reports whether a is strictly before b.
func (a Date) Before(b Date) bool {
	if a.Year != b.Year {
		return a.Year < b.Year
	}
	if a.Month != b.Month {
		return a.Month < b.Month
	}
	return a.Day < b.Day
}

// Equal reports whether a and b are the same civil day.
func (a Date) Equal(b Date) bool {
	return a.Year == b.Year && a.Month == b.Month && a.Day == b.Day
}

// IsOverdue reports whether due's civil day is strictly before the given today Date.
// A nil due is never overdue.
func IsOverdue(due *time.Time, today Date) bool {
	if due == nil {
		return false
	}
	return FromTime(*due).Before(today)
}
