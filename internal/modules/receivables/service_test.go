package receivables

import (
	"testing"
	"time"
)

// dateOnlyUTC builds a PostgreSQL DATE-like value (civil day at UTC midnight).
func dateOnlyUTC(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func TestDebtStatus(t *testing.T) {
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
			if got := debtStatus(c.balance, c.paid, c.due); got != c.want {
				t.Fatalf("got %s want %s", got, c.want)
			}
		})
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
