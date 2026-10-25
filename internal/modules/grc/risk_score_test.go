package grc

import "testing"

func TestRiskScore(t *testing.T) {
	tests := []struct {
		name       string
		likelihood int
		impact     int
		want       int
		wantErr    bool
	}{
		{name: "minimum", likelihood: 1, impact: 1, want: 1},
		{name: "middle", likelihood: 3, impact: 4, want: 12},
		{name: "maximum", likelihood: 5, impact: 5, want: 25},
		{name: "likelihood too low", likelihood: 0, impact: 3, wantErr: true},
		{name: "likelihood too high", likelihood: 6, impact: 3, wantErr: true},
		{name: "impact too low", likelihood: 3, impact: 0, wantErr: true},
		{name: "impact too high", likelihood: 3, impact: 6, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RiskScore(tt.likelihood, tt.impact)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("RiskScore(%d, %d) expected error", tt.likelihood, tt.impact)
				}
				return
			}

			if err != nil {
				t.Fatalf("RiskScore(%d, %d) unexpected error: %v", tt.likelihood, tt.impact, err)
			}

			if got != tt.want {
				t.Fatalf("RiskScore(%d, %d) = %d, want %d", tt.likelihood, tt.impact, got, tt.want)
			}
		})
	}
}

func TestRiskRecalculateScores(t *testing.T) {
	risk := Risk{
		Likelihood:         4,
		Impact:             5,
		ResidualLikelihood: 2,
		ResidualImpact:     3,
		InherentScore:      1,
		ResidualScore:      1,
	}

	if err := risk.RecalculateScores(); err != nil {
		t.Fatalf("RecalculateScores() unexpected error: %v", err)
	}

	if risk.InherentScore != 20 {
		t.Fatalf("InherentScore = %d, want 20", risk.InherentScore)
	}

	if risk.ResidualScore != 6 {
		t.Fatalf("ResidualScore = %d, want 6", risk.ResidualScore)
	}
}

func TestRiskRecalculateScoresRejectsInvalidScale(t *testing.T) {
	risk := Risk{
		Likelihood:         6,
		Impact:             5,
		ResidualLikelihood: 2,
		ResidualImpact:     3,
	}

	if err := risk.RecalculateScores(); err == nil {
		t.Fatal("RecalculateScores() expected error for invalid likelihood")
	}
}
