package grc

import "testing"

func TestRiskLevelBoundaries(t *testing.T) {
	tests := []struct {
		score int
		want  string
	}{
		{1, RiskLevelLow},
		{4, RiskLevelLow},
		{5, RiskLevelMedium},
		{9, RiskLevelMedium},
		{10, RiskLevelHigh},
		{14, RiskLevelHigh},
		{15, RiskLevelCritical},
		{25, RiskLevelCritical},
	}
	for _, tt := range tests {
		got, err := RiskLevel(tt.score)
		if err != nil || got != tt.want {
			t.Fatalf("RiskLevel(%d) = %q, %v; want %q", tt.score, got, err, tt.want)
		}
	}
}

func TestRiskLevelRejectsOutOfDomainScores(t *testing.T) {
	for _, score := range []int{-1, 0, 26, 100} {
		if got, err := RiskLevel(score); err == nil || got != "" {
			t.Fatalf("RiskLevel(%d) = %q, %v; want an error and no level", score, got, err)
		}
	}
}
