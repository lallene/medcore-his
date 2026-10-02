package billing

import (
	"errors"
	"fmt"
	"strings"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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

func legacyBillableKey(sourceType string, sourceID uint) string {
	return fmt.Sprintf("%s:%d", strings.ToUpper(strings.TrimSpace(sourceType)), sourceID)
}

// clinicalSourceTable maps producer source_type → clinical table for FOR UPDATE.
func clinicalSourceTable(sourceType string) (string, bool) {
	switch strings.ToUpper(strings.TrimSpace(sourceType)) {
	case "CONSULTATION":
		return "consultations", true
	case "LABORATORY":
		return "laboratory_orders", true
	case "IMAGING":
		return "imaging_orders", true
	default:
		return "", false
	}
}

// lockClinicalSource serializes legacy CreateInvoice and producer/PA billing on the
// same clinical row (LOT28C). Missing rows/tables are ignored for unit fixtures.
//
// Lock order (deterministic):
//  1. clinical source row (CONSULTATION/LABORATORY/IMAGING)
//  2. then PA / invoice rows as applicable by caller
func (s *Service) lockClinicalSource(tx *gorm.DB, sourceType string, sourceID uint) error {
	table, ok := clinicalSourceTable(sourceType)
	if !ok || sourceID == 0 || tx == nil {
		return nil
	}
	var id uint
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Table(table).Select("id").Where("id = ?", sourceID).Take(&id).Error
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "no such table") || strings.Contains(msg, "does not exist") {
		return nil
	}
	return err
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

// hasFinanciallyAuthoritativeLegacy reports whether an active (is_active) legacy
// invoice line exists for the clinical occurrence. CANCELLED invoices deactivate
// lines — those no longer reserve financial identity (LOT28C).
func (s *Service) hasFinanciallyAuthoritativeLegacy(tx *gorm.DB, sourceType string, sourceID uint) (bool, error) {
	if sourceID == 0 {
		return false, nil
	}
	if _, ok := producerSourceTypeForLegacy(sourceType); !ok {
		return false, nil
	}
	var n int64
	err := tx.Table("billing_invoice_lines").
		Where("billable_key = ? AND is_active = ?", legacyBillableKey(sourceType, sourceID), true).
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

// rejectPAWhenAuthoritativeLegacy blocks NEW PA billing when a financially
// relevant legacy invoice line still reserves the clinical occurrence (LOT28C).
func (s *Service) rejectPAWhenAuthoritativeLegacy(tx *gorm.DB, sourceType string, sourceID uint) error {
	ok, err := s.hasFinanciallyAuthoritativeLegacy(tx, sourceType, sourceID)
	if err != nil {
		return err
	}
	if ok {
		return coreerrors.Conflict("Cet acte réalisé est déjà couvert par une facturation clinique legacy pour la même occurrence")
	}
	return nil
}
