package billing

import (
	"strings"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"gorm.io/gorm"
)

// producerSourceTypeForLegacy maps supported legacy billable act types to the
// PerformedAct producer source_type. Unsupported types (MEDICATION, HOSPITALIZATION, …)
// return false — no invented mappings.
func producerSourceTypeForLegacy(actType string) (string, bool) {
	switch strings.ToUpper(strings.TrimSpace(actType)) {
	case "CONSULTATION":
		return "CONSULTATION", true
	case "LABORATORY":
		return "LABORATORY", true
	case "IMAGING":
		return "IMAGING", true
	default:
		return "", false
	}
}

// hasCanonicalPerformedAct reports whether a PERFORMED producer-created PerformedAct
// exists for the legacy clinical reference (source_type + source_id).
// VOIDED acts do not suppress legacy billing.
func (s *Service) hasCanonicalPerformedAct(tx *gorm.DB, legacyActType string, referenceID uint) (bool, error) {
	sourceType, ok := producerSourceTypeForLegacy(legacyActType)
	if !ok || referenceID == 0 {
		return false, nil
	}
	var n int64
	err := tx.Table("performed_acts").
		Where("source_type = ? AND source_id = ? AND status = ?", sourceType, referenceID, "PERFORMED").
		Count(&n).Error
	return n > 0, err
}

// rejectLegacyWhenCanonicalPA enforces Policy 2 on CreateInvoice: when a linked
// PERFORMED PerformedAct exists, the legacy identity must not be billed.
func (s *Service) rejectLegacyWhenCanonicalPA(tx *gorm.DB, legacyActType string, referenceID uint) error {
	ok, err := s.hasCanonicalPerformedAct(tx, legacyActType, referenceID)
	if err != nil {
		return err
	}
	if ok {
		return coreerrors.Conflict("Cet acte clinique est facturable via l'acte réalisé (PERFORMED_ACT) lié au producteur")
	}
	return nil
}
