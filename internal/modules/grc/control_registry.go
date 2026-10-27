package grc

import (
	"unicode/utf8"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"gorm.io/gorm"
)

// Lengths mirror the grc_controls columns.
const (
	controlCodeMaxLength     = 60
	controlNameMaxLength     = 220
	controlCategoryMaxLength = 100
)

func normalizeControlRequest(r *ControlRequest) {
	r.Code = normalizeCode(r.Code)
	r.Name = normalizeText(r.Name)
	r.Description = normalizeText(r.Description)
	r.Category = normalizeText(r.Category)
	r.Status = normalizeEnum(r.Status)
}

// validateControlRequest checks a normalized request without any database
// access; relation existence and activity are checked by the service.
func validateControlRequest(r ControlRequest) error {
	if r.Code == "" || r.Name == "" {
		return coreerrors.BadRequest("Code et nom du contrôle obligatoires")
	}
	if utf8.RuneCountInString(r.Code) > controlCodeMaxLength {
		return coreerrors.BadRequest("Code du contrôle trop long")
	}
	if utf8.RuneCountInString(r.Name) > controlNameMaxLength {
		return coreerrors.BadRequest("Nom du contrôle trop long")
	}
	if utf8.RuneCountInString(r.Category) > controlCategoryMaxLength {
		return coreerrors.BadRequest("Catégorie du contrôle trop longue")
	}
	if !isValidControlStatus(r.Status) {
		return coreerrors.BadRequest("Statut du contrôle invalide")
	}
	if !isPositiveOptionalID(r.OwnerProfileID) || !isPositiveOptionalID(r.OwningServiceID) {
		return coreerrors.BadRequest("Identifiant de relation du contrôle invalide")
	}
	return nil
}

func normalizeControlImplementationRequest(r *ControlImplementationRequest) {
	r.Status = normalizeEnum(r.Status)
	r.ImplementationDetails = normalizeText(r.ImplementationDetails)
}

func validateControlImplementationRequest(r ControlImplementationRequest) error {
	if r.ControlID == 0 || r.AssetID == 0 {
		return coreerrors.BadRequest("Contrôle et actif de l'implémentation obligatoires")
	}
	if !isValidControlImplementationStatus(r.Status) {
		return coreerrors.BadRequest("Statut de l'implémentation invalide")
	}
	if !isPositiveOptionalID(r.OwnerProfileID) || !isPositiveOptionalID(r.OwningServiceID) {
		return coreerrors.BadRequest("Identifiant de relation de l'implémentation invalide")
	}
	return nil
}

// countByControl counts the rows of one relation table per control, for the
// controls of a page, in one grouped query.
func countByControl(db *gorm.DB, model any, controlIDs []uint) (map[uint]int64, error) {
	out := map[uint]int64{}
	if len(controlIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		ControlID uint
		Total     int64
	}
	if err := db.Model(model).
		Select("control_id, COUNT(*) AS total").
		Where("control_id IN ?", controlIDs).
		Group("control_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.ControlID] = row.Total
	}
	return out, nil
}

// fillControlCounts sets the relation counts (requirements, risks,
// implementations) of the given controls: three grouped queries per call.
func (s *GRCService) fillControlCounts(controls []Control) error {
	ids := make([]uint, len(controls))
	for i := range controls {
		ids[i] = controls[i].ID
	}
	requirements, err := countByControl(s.db, &RequirementControl{}, ids)
	if err != nil {
		return err
	}
	risks, err := countByControl(s.db, &ControlRisk{}, ids)
	if err != nil {
		return err
	}
	implementations, err := countByControl(s.db, &ControlImplementation{}, ids)
	if err != nil {
		return err
	}
	for i := range controls {
		id := controls[i].ID
		controls[i].RequirementCount = requirements[id]
		controls[i].RiskCount = risks[id]
		controls[i].ImplementationCount = implementations[id]
	}
	return nil
}
