package insurance_receivables

import (
	"testing"
	"time"
)

// dateOnlyUTC builds a PostgreSQL DATE-like value (civil day at UTC midnight).
func dateOnlyUTC(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func TestInsuranceStatus(t *testing.T) {
	now := time.Now().In(time.Local)
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
			if got := insuranceStatus(c.balance, c.paid, c.due); got != c.want {
				t.Fatalf("got=%s want=%s", got, c.want)
			}
		})
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
