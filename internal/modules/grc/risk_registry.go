package grc

import (
	"unicode/utf8"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
)

// Lengths mirror the grc_risks columns.
const (
	riskReferenceMaxLength = 60
	riskTitleMaxLength     = 220
)

func normalizeRiskRequest(r *RiskRequest) {
	r.Reference = normalizeCode(r.Reference)
	r.Title = normalizeText(r.Title)
	r.Description = normalizeText(r.Description)
	r.Threat = normalizeText(r.Threat)
	r.Vulnerability = normalizeText(r.Vulnerability)
	r.Treatment = normalizeEnum(r.Treatment)
	r.Status = normalizeEnum(r.Status)
}

// validateRiskRequest checks a normalized request without any database
// access. Score components go through RiskScore, the only scoring rule;
// relation existence and activity are checked by the service.
func validateRiskRequest(r RiskRequest) error {
	if r.Reference == "" || r.Title == "" {
		return coreerrors.BadRequest("Référence et titre du risque obligatoires")
	}
	if utf8.RuneCountInString(r.Reference) > riskReferenceMaxLength {
		return coreerrors.BadRequest("Référence du risque trop longue")
	}
	if utf8.RuneCountInString(r.Title) > riskTitleMaxLength {
		return coreerrors.BadRequest("Titre du risque trop long")
	}
	if !isValidRiskTreatment(r.Treatment) {
		return coreerrors.BadRequest("Traitement du risque invalide")
	}
	if !isValidRiskStatus(r.Status) {
		return coreerrors.BadRequest("Statut du risque invalide")
	}
	if _, err := RiskScore(r.Likelihood, r.Impact); err != nil {
		return coreerrors.BadRequest("inherent risk: " + err.Error())
	}
	if _, err := RiskScore(r.ResidualLikelihood, r.ResidualImpact); err != nil {
		return coreerrors.BadRequest("residual risk: " + err.Error())
	}
	for _, id := range []*uint{r.AssetID, r.OwningServiceID, r.OwnerProfileID, r.ApproverProfileID} {
		if !isPositiveOptionalID(id) {
			return coreerrors.BadRequest("Identifiant de relation du risque invalide")
		}
	}
	return nil
}

// riskLevelScores lists the scores of a level by asking RiskLevel for every
// score of the scale, so a level filter never restates the thresholds.
func riskLevelScores(level string) ([]int, error) {
	minScore, maxScore := RiskScaleMin*RiskScaleMin, RiskScaleMax*RiskScaleMax
	var scores []int
	for score := minScore; score <= maxScore; score++ {
		current, err := RiskLevel(score)
		if err != nil {
			return nil, err
		}
		if current == level {
			scores = append(scores, score)
		}
	}
	if len(scores) == 0 {
		return nil, coreerrors.BadRequest("Niveau de risque invalide")
	}
	return scores, nil
}

// fillRiskLevels derives the levels from the stored scores; they are never
// persisted. A score outside the scale is an error, never a silent level.
func fillRiskLevels(risk *Risk) error {
	var err error
	if risk.InherentLevel, err = RiskLevel(risk.InherentScore); err != nil {
		return err
	}
	risk.ResidualLevel, err = RiskLevel(risk.ResidualScore)
	return err
}
