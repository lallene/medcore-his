package insurance_receivables

import (
	"testing"
	"time"
)

func TestInsuranceStatus(t *testing.T) {
	past := time.Now().AddDate(0, 0, -1)
	future := time.Now().AddDate(0, 0, 1)
	cases := []struct {
		balance, paid int64
		due           *time.Time
		want          string
	}{{0, 35000, nil, "PAID"}, {35000, 0, nil, "UNPAID"}, {15000, 20000, nil, "PARTIALLY_PAID"}, {35000, 0, &past, "OVERDUE"}, {35000, 0, &future, "UNPAID"}}
	for _, c := range cases {
		if got := insuranceStatus(c.balance, c.paid, c.due); got != c.want {
			t.Fatalf("got=%s want=%s", got, c.want)
		}
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
