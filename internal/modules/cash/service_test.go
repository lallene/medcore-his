package cash

import "testing"

func TestCashTableNames(t *testing.T) {
	cases := map[string]string{(Register{}).TableName(): "cash_registers", (Session{}).TableName(): "cash_sessions", (Receipt{}).TableName(): "cash_receipts"}
	for got, want := range cases {
		if got != want {
			t.Fatalf("got %s want %s", got, want)
		}
	}
}
func TestExpectedCashFormula(t *testing.T) {
	opening, cash, card, mobile := int64(50000), int64(5000), int64(3000), int64(10000)
	expected := opening + cash
	if expected != 55000 {
		t.Fatal(expected)
	}
	if expected == opening+cash+card+mobile {
		t.Fatal("non-cash included")
	}
	if diff := int64(53000) - expected; diff != -2000 {
		t.Fatal(diff)
	}
}
