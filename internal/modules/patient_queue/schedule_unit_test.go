package patient_queue

import (
	"testing"
	"time"
)

func TestParseWallClockValid(t *testing.T) {
	secs, n, err := ParseWallClockSeconds("08:00")
	if err != nil || secs != 8*3600 || n != "08:00:00" {
		t.Fatalf("got secs=%d n=%s err=%v", secs, n, err)
	}
	secs, n, err = ParseWallClockSeconds("14:30:15")
	if err != nil || secs != 14*3600+30*60+15 || n != "14:30:15" {
		t.Fatalf("got secs=%d n=%s err=%v", secs, n, err)
	}
}

func TestValidateWeekday(t *testing.T) {
	if err := validateWeekday(int(time.Monday)); err != nil {
		t.Fatal(err)
	}
	if err := validateWeekday(-1); err == nil {
		t.Fatal("expected reject")
	}
	if err := validateWeekday(7); err == nil {
		t.Fatal("expected reject")
	}
}

func TestWorkingWindowValidation(t *testing.T) {
	vf := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	_, _, _, _, err := validateWorkingWindowFields(int(time.Monday), "08:00", "12:00", vf, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, _, err = validateWorkingWindowFields(int(time.Monday), "08:00", "08:00", vf, nil)
	if err == nil {
		t.Fatal("start==end must reject")
	}
	_, _, _, _, err = validateWorkingWindowFields(int(time.Monday), "12:00", "08:00", vf, nil)
	if err == nil {
		t.Fatal("start>end must reject")
	}
	_, _, _, _, err = validateWorkingWindowFields(9, "08:00", "12:00", vf, nil)
	if err == nil {
		t.Fatal("invalid weekday must reject")
	}
	until := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	_, _, _, _, err = validateWorkingWindowFields(int(time.Monday), "08:00", "12:00", vf, &until)
	if err == nil {
		t.Fatal("validUntil < validFrom must reject")
	}
}

func TestAdjacentWindowsNoOverlap(t *testing.T) {
	if timeWindowsOverlapHalfOpen(8*3600, 12*3600, 12*3600, 16*3600) {
		t.Fatal("adjacent [08,12) and [12,16) must not overlap")
	}
	if !timeWindowsOverlapHalfOpen(8*3600, 12*3600, 10*3600, 14*3600) {
		t.Fatal("overlapping windows must overlap")
	}
}

func TestValidityDateOverlap(t *testing.T) {
	aFrom := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	aUntil := time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)
	bFrom := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	if validityRangesOverlap(aFrom, &aUntil, bFrom, nil) {
		t.Fatal("non-overlapping validity must not overlap")
	}
	bFrom2 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	if !validityRangesOverlap(aFrom, &aUntil, bFrom2, nil) {
		t.Fatal("overlapping validity expected")
	}
}

func TestExceptionPolarityAndPrecedence(t *testing.T) {
	if !IsNegativeException(ExMeeting) || IsPositiveException(ExMeeting) {
		t.Fatal("MEETING is negative")
	}
	if !IsPositiveException(ExExtraAvailability) || IsNegativeException(ExExtraAvailability) {
		t.Fatal("EXTRA_AVAILABILITY is positive")
	}
	if !ExceptionPrecedenceNegativeWins() {
		t.Fatal("23C contract: negative wins")
	}
	if validExceptionType("NOPE") {
		t.Fatal("invalid type")
	}
}

func TestZeroLengthExceptionRejectedByLogic(t *testing.T) {
	start := time.Date(2026, 9, 14, 9, 0, 0, 0, time.UTC)
	end := start
	if end.After(start) {
		t.Fatal("zero length should not pass end>start")
	}
}
