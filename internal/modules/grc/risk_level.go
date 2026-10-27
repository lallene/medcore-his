package grc

import "fmt"

// Risk levels are derived from a 1–25 risk score for aggregation and display
// only: the numeric score stays the reference and no level is persisted.
const (
	RiskLevelLow      = "LOW"
	RiskLevelMedium   = "MEDIUM"
	RiskLevelHigh     = "HIGH"
	RiskLevelCritical = "CRITICAL"
)

// riskLevels lists the levels in ascending order.
var riskLevels = []string{RiskLevelLow, RiskLevelMedium, RiskLevelHigh, RiskLevelCritical}

// RiskLevel classifies a risk score: LOW 1–4, MEDIUM 5–9, HIGH 10–14,
// CRITICAL 15–25. A score outside 1–25 is an error, never a level.
func RiskLevel(score int) (string, error) {
	minScore, maxScore := RiskScaleMin*RiskScaleMin, RiskScaleMax*RiskScaleMax
	switch {
	case score < minScore || score > maxScore:
		return "", fmt.Errorf("risk score must be between %d and %d", minScore, maxScore)
	case score >= 15:
		return RiskLevelCritical, nil
	case score >= 10:
		return RiskLevelHigh, nil
	case score >= 5:
		return RiskLevelMedium, nil
	default:
		return RiskLevelLow, nil
	}
}
