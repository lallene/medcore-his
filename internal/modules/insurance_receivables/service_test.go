package insurance_receivables

import (
	"testing"
	"time"
)

func dateOnlyUTC(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func TestInsuranceStatus(t *testing.T) {
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
		{"A paid", 0, 35000, nil, "PAID"},
		{"B unpaid nil due", 35000, 0, nil, "UNPAID"},
		{"C unpaid today DATE", 35000, 0, &today, "UNPAID"},
		{"D unpaid future DATE", 35000, 0, &tomorrow, "UNPAID"},
		{"E unpaid yesterday DATE", 35000, 0, &yesterday, "OVERDUE"},
		{"F partial nil due", 15000, 20000, nil, "PARTIALLY_PAID"},
		{"G partial future DATE", 15000, 20000, &tomorrow, "PARTIALLY_PAID"},
		{"H partial yesterday DATE", 15000, 20000, &yesterday, "OVERDUE"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := insuranceStatusAt(c.balance, c.paid, c.due, now, loc); got != c.want {
				t.Fatalf("got=%s want=%s", got, c.want)
			}
		})
	}
}

func TestInsuranceStatusIgnoresProcessLocal(t *testing.T) {
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
	if got := insuranceStatusAt(15000, 20000, &due, now, paris); got != "OVERDUE" {
		t.Fatalf("paris: got %s want OVERDUE", got)
	}
	if got := insuranceStatusAt(15000, 20000, &due, now, ny); got != "PARTIALLY_PAID" {
		t.Fatalf("ny: got %s want PARTIALLY_PAID", got)
	}
}

func TestSettlementMethodsExcludePatientCash(t *testing.T) {
	if settlementMethods["CASH"] {
		t.Fatal("insurance settlements must not use patient CASH")
	}
	for _, x := range []string{"BANK_TRANSFER", "CHECK", "OTHER"} {
		if !settlementMethods[x] {
			t.Fatalf("missing %s", x)
		}
	}
}
