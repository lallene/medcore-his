package medical_records

import (
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// MedicalDocument is ACTIVE when ArchivedAt is nil.
func medicalDocumentIsActive(doc *MedicalDocument) bool {
	return doc != nil && doc.ArchivedAt == nil
}

// archiveMedicalDocuments applies C2-B REMOVAL-C: delete_ids archive persisted rows
// (no hard delete). Caller must ensure Present==true and handle upsert separately.
func archiveMedicalDocuments(
	tx *gorm.DB,
	recordID uint,
	authorID uint,
	deleteIDs []uint,
	upsertIDs map[uint]struct{},
) (bool, error) {
	seen := map[uint]struct{}{}
	changed := false
	now := time.Now()

	for _, id := range deleteIDs {
		if id == 0 {
			return false, invalid("delete_ids", "un identifiant ne peut pas être nul")
		}
		if _, exists := seen[id]; exists {
			return false, invalid("delete_ids", "identifiant dupliqué")
		}
		seen[id] = struct{}{}
		if _, conflict := upsertIDs[id]; conflict {
			return false, invalid("document", "archivage et mise à jour conflictuels pour le même document")
		}

		var doc MedicalDocument
		err := tx.Where("id = ? AND medical_record_id = ?", id, recordID).First(&doc).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, fmt.Errorf("%w: id=%d", ErrCommonMedicalRecordChild, id)
		}
		if err != nil {
			return false, err
		}
		if !medicalDocumentIsActive(&doc) {
			// Deterministic: re-archive is invalid (no unarchive / no silent idempotent no-op).
			return false, invalid("document", "document déjà archivé")
		}

		archBy := authorID
		result := tx.Model(&MedicalDocument{}).
			Where("id = ? AND medical_record_id = ? AND archived_at IS NULL", id, recordID).
			Updates(map[string]any{
				"archived_at": now,
				"archived_by": archBy,
			})
		if result.Error != nil {
			return false, result.Error
		}
		if result.RowsAffected != 1 {
			return false, invalid("document", "document déjà archivé")
		}
		changed = true
	}
	return changed, nil
}

func rejectArchivedMedicalDocumentMutation(tx *gorm.DB, recordID, documentID uint) error {
	var doc MedicalDocument
	err := tx.Where("id = ? AND medical_record_id = ?", documentID, recordID).First(&doc).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("%w: id=%d", ErrCommonMedicalRecordChild, documentID)
	}
	if err != nil {
		return err
	}
	if !medicalDocumentIsActive(&doc) {
		return invalid("document", "document archivé non modifiable")
	}
	return nil
}
