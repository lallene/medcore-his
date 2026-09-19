package receivables

import (
	"testing"
	"time"
)

func dateOnlyUTC(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func TestDebtStatus(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 9, 19, 15, 0, 0, 0, time.UTC)
	y, m, d := now.Date()
	today := dateOnlyUTC(y, m, d)
	yesterday := today.AddDate(0, 0, -1)
	tomorrow := today.AddDate(0, 0, 1)

	cases := []struct {
		name          string
		balance, paid int64
		due           *time.Time
		want          string
	}{
		{"A paid", 0, 100, nil, "PAID"},
		{"B unpaid nil due", 100, 0, nil, "DUE"},
		{"C unpaid today", 100, 0, &today, "DUE"},
		{"D unpaid future", 100, 0, &tomorrow, "DUE"},
		{"E unpaid yesterday DATE", 100, 0, &yesterday, "OVERDUE"},
		{"F partial future", 100, 20, &tomorrow, "PARTIALLY_PAID"},
		{"G partial nil due", 100, 20, nil, "PARTIALLY_PAID"},
		{"H partial yesterday DATE", 100, 20, &yesterday, "OVERDUE"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := debtStatusAt(c.balance, c.paid, c.due, now, loc); got != c.want {
				t.Fatalf("got %s want %s", got, c.want)
			}
		})
	}
}

func TestDebtStatusIgnoresProcessLocal(t *testing.T) {
	// Instant is still 18 Sep in America/New_York, already 19 Sep in Europe/Paris.
	now := time.Date(2026, 9, 18, 23, 30, 0, 0, time.UTC)
	due := dateOnlyUTC(2026, 9, 18)
	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatal(err)
	}
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	if got := debtStatusAt(100, 20, &due, now, paris); got != "OVERDUE" {
		t.Fatalf("paris business day: got %s want OVERDUE", got)
	}
	if got := debtStatusAt(100, 20, &due, now, ny); got != "PARTIALLY_PAID" {
		t.Fatalf("ny business day: got %s want PARTIALLY_PAID", got)
	}
}

func TestFollowUpDoesNotRepresentPayment(t *testing.T) {
	if followTypes["PAYMENT_PROMISE"] != true {
		t.Fatal("payment promise missing")
	}
	for _, typ := range []string{"PAYMENT", "CASH", "RECEIPT"} {
		if followTypes[typ] {
			t.Fatalf("%s must not be a follow-up", typ)
		}
	}
}
