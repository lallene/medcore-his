package receivables

import (
	"testing"
	"time"
)

func TestDebtStatus(t *testing.T) {
	past := time.Now().AddDate(0, 0, -1)
	future := time.Now().AddDate(0, 0, 1)
	cases := []struct {
		balance, paid int64
		due           *time.Time
		want          string
	}{{0, 100, nil, "PAID"}, {100, 0, nil, "DUE"}, {100, 20, nil, "PARTIALLY_PAID"}, {100, 0, &past, "OVERDUE"}, {100, 0, &future, "DUE"}}
	for _, c := range cases {
		if got := debtStatus(c.balance, c.paid, c.due); got != c.want {
			t.Fatalf("got %s want %s", got, c.want)
		}
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
