package businessdate

import (
	"testing"
	"time"
)

func TestFromTimeUsesValueDateComponents(t *testing.T) {
	// DATE-like UTC midnight must keep civil day 18, not shift via Local.
	due := time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)
	got := FromTime(due)
	if got.Year != 2026 || got.Month != time.September || got.Day != 18 {
		t.Fatalf("got %+v", got)
	}
}

func TestTodayAtIgnoresProcessLocal(t *testing.T) {
	// Fixed instant: 2026-09-19 01:30 in UTC+2 == still 2026-09-18 in America/New_York.
	now := time.Date(2026, 9, 18, 23, 30, 0, 0, time.UTC)
	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatal(err)
	}
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	if got := TodayAt(now, paris); !got.Equal(Date{2026, time.September, 19}) {
		t.Fatalf("paris today=%+v", got)
	}
	if got := TodayAt(now, ny); !got.Equal(Date{2026, time.September, 18}) {
		t.Fatalf("ny today=%+v", got)
	}
}

func TestIsOverdueCivilSemantics(t *testing.T) {
	today := Date{2026, time.September, 19}
	yesterday := time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)
	same := time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)
	if !IsOverdue(&yesterday, today) {
		t.Fatal("yesterday must be overdue")
	}
	if IsOverdue(&same, today) {
		t.Fatal("today must not be overdue")
	}
	if IsOverdue(nil, today) {
		t.Fatal("nil due must not be overdue")
	}
}
