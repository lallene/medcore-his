package patient_queue

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var wallClockRE = regexp.MustCompile(`^([01]\d|2[0-3]):([0-5]\d)(?::([0-5]\d))?$`)

// ParseWallClock parses local wall-clock "HH:MM" or "HH:MM:SS" into minutes from midnight [0, 1440).
func ParseWallClock(s string) (minutes int, normalized string, err error) {
	s = strings.TrimSpace(s)
	m := wallClockRE.FindStringSubmatch(s)
	if m == nil {
		return 0, "", fmt.Errorf("heure invalide %q (attendu HH:MM ou HH:MM:SS)", s)
	}
	h, _ := strconv.Atoi(m[1])
	min, _ := strconv.Atoi(m[2])
	sec := 0
	if m[3] != "" {
		sec, _ = strconv.Atoi(m[3])
	}
	if h == 24 {
		return 0, "", fmt.Errorf("heure invalide")
	}
	minutes = h*60 + min
	if sec > 0 {
		// Sub-minute precision kept in normalized string; overlap uses minute floor for windows.
		// End exclusive at second granularity: store full HH:MM:SS.
	}
	normalized = fmt.Sprintf("%02d:%02d:%02d", h, min, sec)
	// For overlap we use minutes + fractional? Prefer second-based for accuracy:
	return minutes*60 + sec, normalized, nil // return seconds from midnight
}

// ParseWallClockSeconds returns seconds from midnight and normalized HH:MM:SS.
func ParseWallClockSeconds(s string) (secs int, normalized string, err error) {
	return ParseWallClock(s)
}

func validateWeekday(d int) error {
	if d < int(time.Sunday) || d > int(time.Saturday) {
		return fmt.Errorf("weekday invalide %d (attendu 0=Sunday … 6=Saturday, time.Weekday)", d)
	}
	return nil
}

func dateOnlyUTC(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// validityRangesOverlap: both ranges are inclusive date intervals; nil until = open-ended.
func validityRangesOverlap(aFrom time.Time, aUntil *time.Time, bFrom time.Time, bUntil *time.Time) bool {
	aFrom = dateOnlyUTC(aFrom)
	bFrom = dateOnlyUTC(bFrom)
	aEnd := time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC)
	if aUntil != nil {
		aEnd = dateOnlyUTC(*aUntil)
	}
	bEnd := time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC)
	if bUntil != nil {
		bEnd = dateOnlyUTC(*bUntil)
	}
	// Inclusive dates: overlap if aFrom <= bEnd && bFrom <= aEnd
	return !aFrom.After(bEnd) && !bFrom.After(aEnd)
}

// timeWindowsOverlapHalfOpen: [start, end) in seconds from midnight.
func timeWindowsOverlapHalfOpen(aStart, aEnd, bStart, bEnd int) bool {
	return aStart < bEnd && bStart < aEnd
}

func validExceptionType(t string) bool {
	switch t {
	case ExAbsence, ExLeave, ExMeeting, ExBlocked, ExTraining, ExOther, ExExtraAvailability:
		return true
	default:
		return false
	}
}
