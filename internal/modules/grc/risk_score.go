package grc

import "fmt"

const (
	RiskScaleMin = 1
	RiskScaleMax = 5
)

func RiskScore(likelihood, impact int) (int, error) {
	if likelihood < RiskScaleMin || likelihood > RiskScaleMax {
		return 0, fmt.Errorf("likelihood must be between %d and %d", RiskScaleMin, RiskScaleMax)
	}
	if impact < RiskScaleMin || impact > RiskScaleMax {
		return 0, fmt.Errorf("impact must be between %d and %d", RiskScaleMin, RiskScaleMax)
	}
	return likelihood * impact, nil
}

func (r *Risk) RecalculateScores() error {
	inherent, err := RiskScore(r.Likelihood, r.Impact)
	if err != nil {
		return fmt.Errorf("inherent risk: %w", err)
	}

	residual, err := RiskScore(r.ResidualLikelihood, r.ResidualImpact)
	if err != nil {
		return fmt.Errorf("residual risk: %w", err)
	}

	r.InherentScore = inherent
	r.ResidualScore = residual
	return nil
}
