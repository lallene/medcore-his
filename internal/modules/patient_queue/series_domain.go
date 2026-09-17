package patient_queue

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
)

// P0 recurrence hard limits (LOT 23O-A).
const (
	SeriesMaxOccurrences   = 52
	SeriesMaxHorizonMonths = 12
)

// SeriesOccurrence is one expanded start instant (UTC) with 1-based index.
type SeriesOccurrence struct {
	Index   int
	StartAt time.Time // UTC
}

// RecurrenceRule is the pure expansion input (no DB).
type RecurrenceRule struct {
	Freq          string
	IntervalWeeks int
	ByWeekdays    []int // Go time.Weekday 0–6
	Count         *int
	Until         *time.Time // inclusive UTC
	Timezone      string     // IANA
	AnchorStartAt time.Time
}

// ExpandWeeklyOccurrences expands a WEEKLY P0 rule into deterministic ascending UTC starts.
// Rejects (does not silently truncate) rules that would exceed 52 occurrences or the 12-month horizon.
// DST: non-existent local civil times are rejected; fall-back ambiguity uses Go time.Date (earlier instant).
func ExpandWeeklyOccurrences(rule RecurrenceRule) ([]SeriesOccurrence, error) {
	if rule.Freq != SeriesFreqWeekly {
		return nil, coreerrors.BadRequest("freq: seule WEEKLY est supportée en P0")
	}
	if rule.IntervalWeeks < 1 {
		return nil, coreerrors.BadRequest("intervalWeeks doit être >= 1")
	}
	if rule.AnchorStartAt.IsZero() {
		return nil, coreerrors.BadRequest("anchorStartAt requis")
	}
	hasCount := rule.Count != nil
	hasUntil := rule.Until != nil
	if hasCount == hasUntil {
		return nil, coreerrors.BadRequest("count XOR until requis (exactement l'un des deux)")
	}
	if hasCount {
		if *rule.Count < 1 || *rule.Count > SeriesMaxOccurrences {
			return nil, coreerrors.BadRequest(fmt.Sprintf("count doit être entre 1 et %d", SeriesMaxOccurrences))
		}
	}

	weekdays, err := normalizeByWeekdays(rule.ByWeekdays)
	if err != nil {
		return nil, err
	}

	loc, err := time.LoadLocation(rule.Timezone)
	if err != nil || rule.Timezone == "" {
		return nil, coreerrors.BadRequest("timezone IANA invalide")
	}

	anchorLocal := rule.AnchorStartAt.In(loc)
	ah, ami, asec := anchorLocal.Clock()
	anchorWD := int(anchorLocal.Weekday())
	if !containsWeekday(weekdays, anchorWD) {
		return nil, coreerrors.BadRequest("anchorStartAt: le jour de la semaine doit figurer dans byWeekdays")
	}

	anchorUTC := rule.AnchorStartAt.UTC()
	// 12-month horizon is a civil/calendar boundary in the series IANA zone (not UTC AddDate).
	// The boundary instant itself is allowed; only strictly later occurrences are rejected.
	horizonUTC := recurrenceHorizonUTC(anchorLocal)
	var untilUTC time.Time
	if hasUntil {
		untilUTC = rule.Until.UTC()
		if untilUTC.Before(anchorUTC) {
			return nil, coreerrors.BadRequest("until doit être >= anchorStartAt")
		}
	}

	// Sunday-based week containing the anchor local civil date (same convention as StaffWorkingSchedule weekdays).
	ay, am, ad := anchorLocal.Date()
	anchorDate := time.Date(ay, am, ad, 0, 0, 0, 0, loc)
	weekStart := anchorDate.AddDate(0, 0, -int(anchorDate.Weekday()))

	seen := map[int64]struct{}{}
	var out []SeriesOccurrence
	maxScanWeeks := SeriesMaxOccurrences*7 + rule.IntervalWeeks*2 // safety bound before hard rejects

	for weekOffset := 0; weekOffset <= maxScanWeeks; weekOffset += rule.IntervalWeeks {
		for _, wd := range weekdays {
			day := weekStart.AddDate(0, 0, weekOffset*7+wd)
			dy, dm, dd := day.Date()
			local, err := civilLocalInLocation(loc, dy, dm, dd, ah, ami, asec)
			if err != nil {
				return nil, err
			}
			startUTC := local.UTC()
			if startUTC.Before(anchorUTC) {
				continue
			}
			if hasUntil && startUTC.After(untilUTC) {
				goto done
			}
			// Hard 12-month horizon: never silently truncate past-horizon occurrences.
			if startUTC.After(horizonUTC) {
				return nil, coreerrors.BadRequest("règle de récurrence dépasse l'horizon de 12 mois")
			}
			key := startUTC.UnixNano()
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, SeriesOccurrence{Index: len(out) + 1, StartAt: startUTC})
			if len(out) > SeriesMaxOccurrences {
				return nil, coreerrors.BadRequest(fmt.Sprintf("règle de récurrence produit plus de %d occurrences", SeriesMaxOccurrences))
			}
			if hasCount && len(out) == *rule.Count {
				goto done
			}
		}
	}

done:
	if len(out) == 0 {
		return nil, coreerrors.BadRequest("aucune occurrence générée")
	}
	if hasCount && len(out) != *rule.Count {
		// Incomplete expansion without hitting until — horizon or scan limit.
		return nil, coreerrors.BadRequest("règle de récurrence dépasse l'horizon de 12 mois")
	}
	if hasUntil && len(out) > SeriesMaxOccurrences {
		return nil, coreerrors.BadRequest(fmt.Sprintf("règle de récurrence produit plus de %d occurrences", SeriesMaxOccurrences))
	}
	// until-path: if more than 52 would be needed we already rejected during loop;
	// if expansion stopped at until with <=52 OK.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].StartAt.Equal(out[j].StartAt) {
			return out[i].Index < out[j].Index
		}
		return out[i].StartAt.Before(out[j].StartAt)
	})
	for i := range out {
		out[i].Index = i + 1
	}
	return out, nil
}

func normalizeByWeekdays(in []int) ([]int, error) {
	if len(in) == 0 {
		return nil, coreerrors.BadRequest("byWeekdays non vide requis")
	}
	seen := map[int]struct{}{}
	var out []int
	for _, w := range in {
		if w < 0 || w > 6 {
			return nil, coreerrors.BadRequest("byWeekdays: valeurs 0–6 (time.Weekday) uniquement")
		}
		if _, ok := seen[w]; ok {
			return nil, coreerrors.BadRequest("byWeekdays: doublons interdits")
		}
		seen[w] = struct{}{}
		out = append(out, w)
	}
	sort.Ints(out)
	return out, nil
}

func containsWeekday(sorted []int, wd int) bool {
	for _, w := range sorted {
		if w == wd {
			return true
		}
	}
	return false
}

// recurrenceHorizonUTC returns the inclusive 12-calendar-month boundary as UTC.
// anchorLocal must already be in the series IANA location; wall-clock is preserved via AddDate.
func recurrenceHorizonUTC(anchorLocal time.Time) time.Time {
	return anchorLocal.AddDate(0, SeriesMaxHorizonMonths, 0).UTC()
}

// civilLocalInLocation builds a local civil datetime and rejects DST spring gaps
// (Go would otherwise normalize silently). Fall-back ambiguity follows time.Date
// (earlier of the two possible instants — deterministic).
func civilLocalInLocation(loc *time.Location, year int, month time.Month, day, hour, min, sec int) (time.Time, error) {
	t := time.Date(year, month, day, hour, min, sec, 0, loc)
	y, m, d := t.Date()
	h, mi, s := t.Clock()
	if y != year || m != month || d != day || h != hour || mi != min || s != sec {
		return time.Time{}, coreerrors.BadRequest(
			fmt.Sprintf("heure locale inexistante (DST): %04d-%02d-%02d %02d:%02d:%02d (%s)",
				year, month, day, hour, min, sec, loc.String()),
		)
	}
	return t, nil
}

func marshalByWeekdays(weekdays []int) (string, error) {
	b, err := json.Marshal(weekdays)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func unmarshalByWeekdays(raw string) ([]int, error) {
	var w []int
	if err := json.Unmarshal([]byte(raw), &w); err != nil {
		return nil, err
	}
	return normalizeByWeekdays(w)
}

func seriesOccurrenceIdempotencyKey(seriesID uint, index int) string {
	return fmt.Sprintf("series:%d:occ:%d", seriesID, index)
}
