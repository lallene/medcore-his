package billing

import (
	"errors"
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"gorm.io/gorm"
)

// CodeTariffNotEligible rejects a client tariffId that is not applicable to the billed occurrence.
const CodeTariffNotEligible = "TARIFF_NOT_ELIGIBLE"

func tariffNotEligible(message string) error {
	return coreerrors.New(409, CodeTariffNotEligible, message, nil)
}

// eligibleTariffs is the server-authoritative tariff choice set for one billable occurrence
// (LOT30-A′): same act type, active and effective at `at`.
//
//   - Occurrence with a specific referential (exam / presentation / act catalogue entry):
//     tariffs dedicated to that referential are eligible; generic tariffs (reference_id NULL)
//     are eligible only when no dedicated tariff exists — the historical BillableActs precedence.
//   - Occurrence without referential (consultation, hospitalisation): generic tariffs only.
//
// Several eligible tariffs may remain (e.g. two consultation tariffs): the client may choose
// among them, never outside them. Ordered by effective_from DESC, id DESC (first = suggestion).
func eligibleTariffs(tx *gorm.DB, actType string, referenceID uint, at time.Time) ([]Tariff, error) {
	base := func() *gorm.DB {
		return tx.Model(&Tariff{}).
			Where("act_type=? AND is_active AND effective_from<=? AND (effective_to IS NULL OR effective_to>=?)", actType, at, at).
			Order("effective_from DESC, id DESC")
	}
	var out []Tariff
	if referenceID > 0 {
		if e := base().Where("reference_id=?", referenceID).Find(&out).Error; e != nil {
			return nil, e
		}
		if len(out) > 0 {
			return out, nil
		}
	}
	if e := base().Where("reference_id IS NULL").Find(&out).Error; e != nil {
		return nil, e
	}
	return out, nil
}

// resolveRequestedTariff validates the client-selected tariffId against eligibleTariffs.
// The returned tariff's unit price is the gross authority for the invoice line.
func resolveRequestedTariff(tx *gorm.DB, tariffID uint, actType string, referenceID uint, at time.Time) (*Tariff, error) {
	var requested Tariff
	if e := tx.First(&requested, tariffID).Error; e != nil {
		if errors.Is(e, gorm.ErrRecordNotFound) {
			return nil, tariffNotEligible("Tarif inactif ou non applicable")
		}
		return nil, e
	}
	if requested.ActType != actType {
		return nil, tariffNotEligible("Le tarif ne correspond pas au type d'acte")
	}
	if requested.ReferenceID != nil && *requested.ReferenceID != referenceID {
		return nil, tariffNotEligible("Le tarif ne correspond pas au référentiel de cet acte")
	}
	eligible, e := eligibleTariffs(tx, actType, referenceID, at)
	if e != nil {
		return nil, e
	}
	for i := range eligible {
		if eligible[i].ID == requested.ID {
			return &eligible[i], nil
		}
	}
	if !requested.IsActive || requested.EffectiveFrom.After(at) || (requested.EffectiveTo != nil && requested.EffectiveTo.Before(at)) {
		return nil, tariffNotEligible("Tarif inactif ou non applicable")
	}
	return nil, tariffNotEligible("Un tarif spécifique existe pour cet acte — le tarif générique n'est pas applicable")
}
