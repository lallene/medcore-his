package billing

import (
	"math"
	"sort"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/authorization"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type performedActAuthClass int

const (
	paAuthNone performedActAuthClass = iota
	paAuthOpen
	paAuthOneFinal
	paAuthAmbiguousFinal
	paAuthRejectedOnly
)

type performedActAuthHit struct {
	auth      authorization.InsuranceAuthorization
	matchType string
}

// listPerformedActAuthorizations collects non-cancelled authorizations that apply
// to a PERFORMED_ACT reference (direct or active covered-act link), without
// choosing a PatientCoverage first.
func (s *Service) listPerformedActAuthorizations(tx *gorm.DB, patient, performedActID uint) ([]performedActAuthHit, error) {
	byID := map[uint]performedActAuthHit{}

	var directs []authorization.InsuranceAuthorization
	if err := tx.Where(
		"patient_id = ? AND reference_type = ? AND reference_id = ? AND status <> ?",
		patient, authorization.ReferencePerformedAct, performedActID, authorization.StatusCancelled,
	).Find(&directs).Error; err != nil {
		return nil, err
	}
	for _, a := range directs {
		byID[a.ID] = performedActAuthHit{auth: a, matchType: "DIRECT"}
	}

	var linkIDs []uint
	if err := tx.Model(&authorization.InsuranceAuthorizationAct{}).
		Where("patient_id = ? AND reference_type = ? AND reference_id = ? AND is_active",
			patient, authorization.ReferencePerformedAct, performedActID).
		Pluck("insurance_authorization_id", &linkIDs).Error; err != nil {
		return nil, err
	}
	if len(linkIDs) > 0 {
		var linked []authorization.InsuranceAuthorization
		if err := tx.Where("id IN ? AND patient_id = ? AND status <> ?", linkIDs, patient, authorization.StatusCancelled).
			Find(&linked).Error; err != nil {
			return nil, err
		}
		for _, a := range linked {
			if _, exists := byID[a.ID]; exists {
				continue
			}
			byID[a.ID] = performedActAuthHit{auth: a, matchType: "COVERED"}
		}
	}

	out := make([]performedActAuthHit, 0, len(byID))
	for _, h := range byID {
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].auth.ID < out[j].auth.ID })
	return out, nil
}

func classifyPerformedActAuthorizations(hits []performedActAuthHit) (performedActAuthClass, []performedActAuthHit, []performedActAuthHit, []performedActAuthHit) {
	var open, finalApplicable, rejected []performedActAuthHit
	for _, h := range hits {
		switch h.auth.Status {
		case authorization.StatusDraft, authorization.StatusSubmitted, authorization.StatusPending:
			open = append(open, h)
		case authorization.StatusApproved, authorization.StatusPartiallyApproved:
			finalApplicable = append(finalApplicable, h)
		case authorization.StatusRejected:
			rejected = append(rejected, h)
		}
	}
	switch {
	case len(finalApplicable) > 1:
		return paAuthAmbiguousFinal, open, finalApplicable, rejected
	case len(finalApplicable) == 1:
		return paAuthOneFinal, open, finalApplicable, rejected
	case len(open) > 0:
		return paAuthOpen, open, finalApplicable, rejected
	case len(rejected) > 0:
		return paAuthRejectedOnly, open, finalApplicable, rejected
	default:
		return paAuthNone, open, finalApplicable, rejected
	}
}

func (s *Service) financialCoverageFromPerformedActAuths(tx *gorm.DB, patient uint, a actSnapshot, gross int64) (string, *authorization.Response, int64, bool, error) {
	hits, err := s.listPerformedActAuthorizations(tx, patient, a.coverageReferenceID)
	if err != nil {
		return "", nil, 0, false, err
	}
	class, open, finals, rejected := classifyPerformedActAuthorizations(hits)

	switch class {
	case paAuthAmbiguousFinal:
		return "", nil, 0, false, coreerrors.Conflict("Plusieurs autorisations PEC finales applicables pour cet acte réalisé")
	case paAuthOneFinal:
		hit := finals[0]
		resp, findErr := s.authorizations.FindByID(hit.auth.ID)
		if findErr != nil {
			return "", nil, 0, false, findErr
		}
		var locked authorization.InsuranceAuthorization
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, hit.auth.ID).Error; e != nil {
			return "", nil, 0, false, e
		}
		var used int64
		if e := tx.Model(&AuthorizationAllocation{}).Where("authorization_id=?", locked.ID).Select("COALESCE(SUM(amount),0)").Scan(&used).Error; e != nil {
			return "", nil, 0, false, e
		}
		cap := int64(math.MaxInt64)
		if locked.InsuranceAmount != nil {
			cap = round(*locked.InsuranceAmount)
		}
		remaining := cap - used
		if remaining < 0 {
			remaining = 0
		}
		amount := allocateInsurance(gross, locked.ApprovedRate, remaining)
		return hit.matchType, resp, amount, false, nil
	case paAuthOpen:
		hit := open[0]
		resp, findErr := s.authorizations.FindByID(hit.auth.ID)
		if findErr != nil {
			return "", nil, 0, false, findErr
		}
		return hit.matchType, resp, 0, true, nil
	case paAuthRejectedOnly:
		hit := rejected[0]
		resp, findErr := s.authorizations.FindByID(hit.auth.ID)
		if findErr != nil {
			return "", nil, 0, false, findErr
		}
		return hit.matchType, resp, 0, false, nil
	default:
		return "NONE", nil, 0, false, nil
	}
}

// financialCoverage resolves insurer allocation.
// PERFORMED_ACT: authorization-first (Policy 1) — never picks activeCoverage.
// Other act types: preserve historical activeCoverage + FindAuthorizationForAct path.
func (s *Service) financialCoverage(tx *gorm.DB, patient uint, a actSnapshot, gross int64) (string, *authorization.Response, int64, bool, error) {
	if a.coverageReferenceType == authorization.ReferencePerformedAct {
		return s.financialCoverageFromPerformedActAuths(tx, patient, a, gross)
	}
	cov, e := s.activeCoverage(tx, patient)
	if e != nil || cov == nil {
		return "NONE", nil, 0, false, e
	}
	match, e := s.authorizations.FindAuthorizationForAct(patient, cov.ID, a.coverageReferenceType, a.coverageReferenceID)
	if e != nil {
		return "", nil, 0, false, e
	}
	if match.Authorization == nil {
		return "NONE", nil, 0, false, nil
	}
	auth := match.Authorization
	if auth.Status == authorization.StatusRejected {
		return match.MatchType, auth, 0, false, nil
	}
	if auth.Status != authorization.StatusApproved && auth.Status != authorization.StatusPartiallyApproved {
		return match.MatchType, auth, 0, true, nil
	}
	var locked authorization.InsuranceAuthorization
	if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, auth.ID).Error; e != nil {
		return "", nil, 0, false, e
	}
	var used int64
	if e := tx.Model(&AuthorizationAllocation{}).Where("authorization_id=?", auth.ID).Select("COALESCE(SUM(amount),0)").Scan(&used).Error; e != nil {
		return "", nil, 0, false, e
	}
	cap := int64(math.MaxInt64)
	if locked.InsuranceAmount != nil {
		cap = round(*locked.InsuranceAmount)
	}
	remaining := cap - used
	if remaining < 0 {
		remaining = 0
	}
	amount := allocateInsurance(gross, locked.ApprovedRate, remaining)
	return match.MatchType, auth, amount, false, nil
}
