package grc

import (
	"sort"
	"time"
	"unicode/utf8"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
)

const (
	governancePolicyReferenceMaxLength = 60
	governancePolicyTitleMaxLength     = 220
	governancePolicyVersionMaxLength   = 40
)

var (
	validGovernancePolicyStatuses = map[string]bool{
		GovernancePolicyStatusDraft:     true,
		GovernancePolicyStatusInReview:  true,
		GovernancePolicyStatusApproved:  true,
		GovernancePolicyStatusPublished: true,
		GovernancePolicyStatusRetired:   true,
	}
	// governancePolicyTransitions is the official lifecycle, including the
	// sensitive edges. Generic update refuses APPROVED, PUBLISHED and RETIRED
	// as a target even when this table allows the edge.
	governancePolicyTransitions = map[string]map[string]bool{
		GovernancePolicyStatusDraft: {
			GovernancePolicyStatusDraft:    true,
			GovernancePolicyStatusInReview: true,
		},
		GovernancePolicyStatusInReview: {
			GovernancePolicyStatusInReview: true,
			GovernancePolicyStatusDraft:    true,
			GovernancePolicyStatusApproved: true,
		},
		GovernancePolicyStatusApproved: {
			GovernancePolicyStatusApproved:  true,
			GovernancePolicyStatusInReview:  true,
			GovernancePolicyStatusPublished: true,
		},
		GovernancePolicyStatusPublished: {
			GovernancePolicyStatusPublished: true,
			GovernancePolicyStatusRetired:   true,
		},
		GovernancePolicyStatusRetired: {
			GovernancePolicyStatusRetired: true,
		},
	}
	governancePolicyReservedRoutes = map[string]string{
		GovernancePolicyStatusApproved:  "/approve",
		GovernancePolicyStatusPublished: "/publish",
		GovernancePolicyStatusRetired:   "/retire",
	}
)

func normalizeGovernancePolicyRequest(r *GovernancePolicyRequest) {
	r.Reference = normalizeCode(r.Reference)
	r.Title = normalizeText(r.Title)
	r.Description = normalizeText(r.Description)
	r.Version = normalizeText(r.Version)
	r.Status = normalizeEnum(r.Status)
	r.EffectiveFrom = utcInstant(r.EffectiveFrom)
	r.EffectiveTo = utcInstant(r.EffectiveTo)
}

func utcInstant(value *time.Time) *time.Time {
	if value == nil || value.IsZero() {
		return nil
	}
	utc := value.UTC()
	return &utc
}

// validateGovernancePolicyRequest checks a normalized request. It never reads
// the clock: effective dates are compared to each other only. creating ignores
// status because the service forces DRAFT. An empty status on update means
// "keep the current status".
func validateGovernancePolicyRequest(r GovernancePolicyRequest, creating bool) error {
	if r.Reference == "" || r.Title == "" || r.Version == "" {
		return coreerrors.BadRequest("Référence, titre et version de la politique obligatoires")
	}
	if utf8.RuneCountInString(r.Reference) > governancePolicyReferenceMaxLength {
		return coreerrors.BadRequest("Référence de la politique trop longue")
	}
	if utf8.RuneCountInString(r.Title) > governancePolicyTitleMaxLength {
		return coreerrors.BadRequest("Titre de la politique trop long")
	}
	if utf8.RuneCountInString(r.Version) > governancePolicyVersionMaxLength {
		return coreerrors.BadRequest("Version de la politique trop longue")
	}
	if !creating && r.Status != "" && !validGovernancePolicyStatuses[r.Status] {
		return coreerrors.BadRequest("Statut de politique invalide")
	}
	if r.EffectiveFrom == nil && r.EffectiveTo != nil {
		return coreerrors.BadRequest("La fin d'effet exige un début d'effet")
	}
	if r.EffectiveFrom != nil && r.EffectiveTo != nil && r.EffectiveTo.Before(*r.EffectiveFrom) {
		return coreerrors.BadRequest("La fin d'effet ne peut pas précéder le début d'effet")
	}
	if !isPositiveOptionalID(r.OwningServiceID) || !isPositiveOptionalID(r.OwnerProfileID) || !isPositiveOptionalID(r.ApproverProfileID) {
		return coreerrors.BadRequest("Identifiant de relation invalide")
	}
	if _, _, err := normalizePolicyIDList(r.FrameworkIDs); err != nil {
		return err
	}
	if _, _, err := normalizePolicyIDList(r.ControlIDs); err != nil {
		return err
	}
	return nil
}

func validateGovernancePolicyTransition(from, to string) error {
	if governancePolicyTransitions[from][to] {
		return nil
	}
	return coreerrors.Conflict("Transition de politique invalide")
}

// governancePolicyReservedStatus refuses a generic update whose target is a
// sensitive status. An empty status is not a target.
func governancePolicyReservedStatus(status string) error {
	if route, ok := governancePolicyReservedRoutes[status]; ok {
		return coreerrors.Forbidden("Ce changement de statut passe par la route dédiée " + route)
	}
	return nil
}

// normalizePolicyIDList reports whether the field was present.
// A nil pointer is absent. Duplicates are dropped and the result is sorted,
// so [2,1,2] and [1,2] are the same assignment. Zero is rejected.
func normalizePolicyIDList(ids *[]uint) ([]uint, bool, error) {
	if ids == nil {
		return nil, false, nil
	}
	seen := map[uint]struct{}{}
	out := make([]uint, 0, len(*ids))
	for _, id := range *ids {
		if id == 0 {
			return nil, true, coreerrors.BadRequest("Identifiant de relation invalide")
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, true, nil
}

func samePolicyIDs(left, right []uint) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func normalizeGovernancePolicyFilter(f *GovernancePolicyFilter) {
	f.Search = normalizeText(f.Search)
	f.Status = normalizeEnum(f.Status)
}

func validateGovernancePolicyFilter(f GovernancePolicyFilter) error {
	if f.Status != "" && !validGovernancePolicyStatuses[f.Status] {
		return coreerrors.BadRequest("Statut de politique invalide")
	}
	for _, id := range []*uint{f.OwningServiceID, f.OwnerProfileID, f.ApproverProfileID, f.FrameworkID, f.ControlID} {
		if id != nil && *id == 0 {
			return coreerrors.BadRequest("Identifiant de filtre invalide")
		}
	}
	return nil
}
