package authorization

import "testing"

func f(v float64) *float64 { return &v }
func TestCalculate(t *testing.T) {
	cases := []struct {
		name                 string
		requested            float64
		status               string
		rate, fixed, ceiling *float64
		insurance, patient   float64
	}{{"rate", 50000, StatusApproved, f(80), nil, nil, 40000, 10000}, {"fixed", 100000, StatusApproved, nil, f(50000), nil, 50000, 50000}, {"ceiling", 120000, StatusPartiallyApproved, f(80), nil, f(70000), 70000, 50000}, {"reject", 100000, StatusRejected, nil, nil, nil, 0, 100000}, {"fixed caps rate", 100000, StatusPartiallyApproved, f(80), f(50000), nil, 50000, 50000}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, p, e := Calculate(tc.requested, tc.status, tc.rate, tc.fixed, tc.ceiling)
			if e != nil || a != tc.insurance || p != tc.patient {
				t.Fatalf("got %v %v %v", a, p, e)
			}
		})
	}
}
func TestCalculateValidation(t *testing.T) {
	if _, _, e := Calculate(100, StatusApproved, f(101), nil, nil); e == nil {
		t.Fatal("rate > 100 accepted")
	}
	if _, _, e := Calculate(100, StatusApproved, nil, nil, nil); e == nil {
		t.Fatal("empty decision accepted")
	}
}

func TestEmptyPageUsesJSONArray(t *testing.T) {
	page := Page{Items: []Response{}}
	if page.Items == nil {
		t.Fatal("items must serialize as [] instead of null")
	}
}
