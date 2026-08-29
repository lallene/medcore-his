package scheduling

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var wallRE = regexp.MustCompile(`^([01]\d|2[0-3]):([0-5]\d)(?::([0-5]\d))?$`)

// ParseWallClockSeconds parses HH:MM or HH:MM:SS into seconds from midnight.
func ParseWallClockSeconds(s string) (secs int, normalized string, err error) {
	s = strings.TrimSpace(s)
	m := wallRE.FindStringSubmatch(s)
	if m == nil {
		return 0, "", fmt.Errorf("heure murale invalide %q", s)
	}
	h, _ := strconv.Atoi(m[1])
	min, _ := strconv.Atoi(m[2])
	sec := 0
	if m[3] != "" {
		sec, _ = strconv.Atoi(m[3])
	}
	return h*3600 + min*60 + sec, fmt.Sprintf("%02d:%02d:%02d", h, min, sec), nil
}

// ProjectWallClock projects a local wall-clock TIME onto a calendar date in loc.
// Uses IANA/DST semantics via time.Date — never a fixed UTC offset.
//
// DST policy (Go time.Date):
//   - Spring gap (nonexistent local time): Go normalizes forward into the valid clock.
//   - Autumn fold (ambiguous local time): Go chooses the earlier occurrence.
func ProjectWallClock(date time.Time, wallHHMMSS string, loc *time.Location) (time.Time, error) {
	if loc == nil {
		loc = time.UTC
	}
	secs, _, err := ParseWallClockSeconds(wallHHMMSS)
	if err != nil {
		return time.Time{}, err
	}
	y, m, d := date.In(loc).Date()
	h := secs / 3600
	min := (secs % 3600) / 60
	sec := secs % 60
	return time.Date(y, m, d, h, min, sec, 0, loc), nil
}
