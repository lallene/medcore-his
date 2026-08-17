package billing

import "testing"

func ptr(v float64) *float64 { return &v }
func TestAllocateInsurance(t *testing.T) {
	tests := []struct {
		name            string
		gross           int64
		rate            *float64
		remaining, want int64
	}{
		{"none left", 50000, ptr(70), 0, 0},
		{"approved rate", 50000, ptr(70), 100000, 35000},
		{"global ceiling", 120000, ptr(80), 70000, 70000},
		{"covered act consumes remainder", 70000, ptr(80), 40000, 40000},
		{"fixed allocation cannot exceed gross", 40000, nil, 70000, 40000},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := allocateInsurance(tc.gross, tc.rate, tc.remaining); got != tc.want {
				t.Fatalf("got %d want %d", got, tc.want)
			}
		})
	}
}
func TestFinancialTableNames(t *testing.T) {
	cases := map[string]string{(Tariff{}).TableName(): "billing_tariffs", (Invoice{}).TableName(): "billing_invoices", (InvoiceLine{}).TableName(): "billing_invoice_lines", (AuthorizationAllocation{}).TableName(): "billing_authorization_allocations", (Payment{}).TableName(): "billing_payments"}
	for got, want := range cases {
		if got != want {
			t.Fatalf("got %s want %s", got, want)
		}
	}
}
