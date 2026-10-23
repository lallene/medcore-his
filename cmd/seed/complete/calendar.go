package complete

import "time"

// Day is one simulated business day with its activity profile.
type Day struct {
	Index   int       // 0 = first day, Days-1 = anchor
	Date    time.Time // 00:00 in the business timezone
	Load    float64   // activity multiplier (1.0 = ordinary weekday)
	Kind    string    // CALM | NORMAL | BUSY | PEAK | WEEKEND | TODAY
	IsToday bool
}

// At returns the instant hh:mm of the day.
func (d Day) At(hour, minute int) time.Time {
	return time.Date(d.Date.Year(), d.Date.Month(), d.Date.Day(), hour, minute, 0, 0, d.Date.Location())
}

// BuildCalendar returns the deterministic list of simulated days.
// Weekdays carry the bulk of the activity, Saturday is reduced, Sunday is emergencies only;
// month starts/ends are busier; a few peaks (epidemic-like) and calm days are spread over
// the year; the anchor day is busy so that "today" KPIs are meaningful.
func BuildCalendar(cfg Config, g *RNG) []Day {
	days := make([]Day, 0, cfg.Days)
	first := cfg.FirstDay()
	peaks := map[int]bool{}
	calm := map[int]bool{}
	pr := g.Stream("calendar.peaks")
	for i := 0; i < cfg.Days/45+1; i++ { // ~8 peak days a year, in short clusters
		start := pr.Intn(cfg.Days - 3)
		for k := 0; k < pr.Between(1, 3); k++ {
			peaks[start+k] = true
		}
	}
	for i := 0; i < cfg.Days/30; i++ {
		calm[pr.Intn(cfg.Days)] = true
	}
	jitter := g.Stream("calendar.jitter")
	for i := 0; i < cfg.Days; i++ {
		date := first.AddDate(0, 0, i)
		d := Day{Index: i, Date: date, Load: 1, Kind: "NORMAL"}
		switch date.Weekday() {
		case time.Saturday:
			d.Load, d.Kind = 0.45, "WEEKEND"
		case time.Sunday:
			d.Load, d.Kind = 0.18, "WEEKEND"
		}
		if d.Kind == "NORMAL" {
			if date.Day() <= 3 {
				d.Load *= 1.2
			}
			if date.AddDate(0, 0, 3).Month() != date.Month() {
				d.Load *= 1.12
			}
			if date.Weekday() == time.Monday {
				d.Load *= 1.15
			}
			switch {
			case peaks[i]:
				d.Load, d.Kind = d.Load*1.9, "PEAK"
			case calm[i]:
				d.Load, d.Kind = d.Load*0.5, "CALM"
			case d.Load > 1.15:
				d.Kind = "BUSY"
			}
			d.Load *= 0.9 + jitter.Float()*0.2
		}
		if i == cfg.Days-1 {
			d.IsToday, d.Kind = true, "TODAY"
			if d.Load < 3.2 { // busy "today" so that the daily KPIs and work queues are populated
				d.Load = 3.2
			}
		}
		days = append(days, d)
	}
	return days
}

// volume turns a reference daily count into the count for this day (scale + load),
// with stochastic rounding so totals stay close to expectations.
func volume(g *RNG, ref float64, cfg Config, d Day) int {
	x := ref * cfg.Scale() * d.Load
	n := int(x)
	if g.Float() < x-float64(n) {
		n++
	}
	return n
}
