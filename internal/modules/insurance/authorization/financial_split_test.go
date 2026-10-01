package authorization

import (
	"math"
	"testing"
)

// LOT27F — Calculate contract matrix (unit). Complements existing rate/fixed/ceiling smoke cases.

func TestCalculateZeroRequested(t *testing.T) {
	ins, pat, err := Calculate(0, StatusApproved, f(80), nil, nil)
	if err != nil || ins != 0 || pat != 0 {
		t.Fatalf("zero requested: ins=%v pat=%v err=%v", ins, pat, err)
	}
	ins, pat, err = Calculate(0, StatusRejected, nil, nil, nil)
	if err != nil || ins != 0 || pat != 0 {
		t.Fatalf("zero rejected: ins=%v pat=%v err=%v", ins, pat, err)
	}
}

func TestCalculateRateBoundaries(t *testing.T) {
	if _, _, err := Calculate(1000, StatusApproved, f(-1), nil, nil); err == nil {
		t.Fatal("negative rate accepted")
	}
	ins, pat, err := Calculate(1000, StatusApproved, f(0), nil, nil)
	if err != nil || ins != 0 || pat != 1000 {
		t.Fatalf("rate 0: %v %v %v", ins, pat, err)
	}
	ins, pat, err = Calculate(1000, StatusApproved, f(100), nil, nil)
	if err != nil || ins != 1000 || pat != 0 {
		t.Fatalf("rate 100: %v %v %v", ins, pat, err)
	}
}

func TestCalculateFixedBoundaries(t *testing.T) {
	if _, _, err := Calculate(1000, StatusApproved, nil, f(-5), nil); err == nil {
		t.Fatal("negative fixed accepted")
	}
	ins, pat, err := Calculate(1000, StatusApproved, nil, f(0), nil)
	if err != nil || ins != 0 || pat != 1000 {
		t.Fatalf("fixed 0: %v %v %v", ins, pat, err)
	}
	// Fixed above requested is capped to requested by min(requested, insurance).
	ins, pat, err = Calculate(1000, StatusApproved, nil, f(5000), nil)
	if err != nil || ins != 1000 || pat != 0 {
		t.Fatalf("fixed > requested: %v %v %v", ins, pat, err)
	}
}

func TestCalculateCeilingBoundaries(t *testing.T) {
	if _, _, err := Calculate(1000, StatusApproved, f(80), nil, f(-1)); err == nil {
		t.Fatal("negative ceiling accepted")
	}
	ins, pat, err := Calculate(1000, StatusApproved, f(80), nil, f(0))
	if err != nil || ins != 0 || pat != 1000 {
		t.Fatalf("ceiling 0: %v %v %v", ins, pat, err)
	}
	ins, pat, err = Calculate(1000, StatusApproved, f(80), nil, f(900))
	if err != nil || ins != 800 || pat != 200 {
		t.Fatalf("ceiling above calculated: %v %v %v", ins, pat, err)
	}
	ins, pat, err = Calculate(1000, StatusApproved, f(80), nil, f(500))
	if err != nil || ins != 500 || pat != 500 {
		t.Fatalf("ceiling below calculated: %v %v %v", ins, pat, err)
	}
}

func TestCalculateApprovedAndPartialSameMath(t *testing.T) {
	rate, fixed, ceiling := f(75), f(400), f(350)
	aIns, aPat, aErr := Calculate(1000, StatusApproved, rate, fixed, ceiling)
	pIns, pPat, pErr := Calculate(1000, StatusPartiallyApproved, rate, fixed, ceiling)
	if aErr != nil || pErr != nil || aIns != pIns || aPat != pPat {
		t.Fatalf("status must not alter formula: approved=(%v,%v,%v) partial=(%v,%v,%v)", aIns, aPat, aErr, pIns, pPat, pErr)
	}
	if aIns != 350 || aPat != 650 {
		t.Fatalf("expected ceiling-capped split 350/650 got %v/%v", aIns, aPat)
	}
}

func TestCalculateRoundingInvariant(t *testing.T) {
	cases := []struct {
		name      string
		requested float64
		rate      float64
	}{
		{"pct_33_33", 100.0, 33.33},
		{"half_odd_cents", 12345.67, 50},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ins, pat, err := Calculate(tc.requested, StatusApproved, f(tc.rate), nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			reqRounded := math.Round(tc.requested*100) / 100
			if math.Abs(ins+pat-reqRounded) > 1e-9 {
				t.Fatalf("invariant broken: ins=%v pat=%v sum=%v req=%v", ins, pat, ins+pat, reqRounded)
			}
		})
	}
}

func TestCalculateRejectIgnoresRateInputs(t *testing.T) {
	ins, pat, err := Calculate(25000, StatusRejected, f(80), f(10000), f(5000))
	if err != nil || ins != 0 || pat != 25000 {
		t.Fatalf("reject: %v %v %v", ins, pat, err)
	}
}
