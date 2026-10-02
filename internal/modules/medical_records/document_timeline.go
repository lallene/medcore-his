package medical_records

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
)

// LOT28E-B1 canonical MedicalDocument timeline vocabulary (matches seed document_added).
const (
	TimelineEventDocumentAdded       = "document_added"
	TimelineEventDocumentArchived    = "document_archived"
	TimelineCategoryDocument         = "document"
	TimelineReferenceMedicalDocument = "MedicalDocument"
)

// CommonMedicalRecordSaveResult classifies mutations for timeline emission policy.
type CommonMedicalRecordSaveResult struct {
	NonDocumentChanged       bool
	DocumentLifecycleChanged bool
	DocumentMetadataChanged  bool
}

// ShouldEmitGenericCMREvent is true when non-document CMR data changed.
// Document-only create/archive/metadata must not emit common_medical_record_updated.
func (r CommonMedicalRecordSaveResult) ShouldEmitGenericCMREvent() bool {
	return r.NonDocumentChanged
}

func documentAddedTitle() string    { return "Document médical ajouté" }
func documentArchivedTitle() string { return "Document médical archivé" }

// ensureDocumentTimelineEvent inserts a document lifecycle event inside the caller TX.
// Semantic key: (patient_id, event_type, reference_type, reference_id).
// Parent MedicalRecord FOR UPDATE serializes concurrent CMR writers (B126).
func ensureDocumentTimelineEvent(
	tx *gorm.DB,
	record *MedicalRecord,
	eventType string,
	documentID uint,
	label string,
	actorID uint,
	eventDate time.Time,
) error {
	var existing MedicalTimelineEvent
	err := tx.Where(
		"patient_id = ? AND event_type = ? AND reference_type = ? AND reference_id = ?",
		record.PatientID,
		eventType,
		TimelineReferenceMedicalDocument,
		documentID,
	).First(&existing).Error
	if err == nil {
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	title := documentAddedTitle()
	if eventType == TimelineEventDocumentArchived {
		title = documentArchivedTitle()
	}
	ref := documentID
	event := &MedicalTimelineEvent{
		MedicalRecordID: record.ID,
		PatientID:       record.PatientID,
		EventType:       eventType,
		Category:        TimelineCategoryDocument,
		Title:           title,
		Description:     sanitizeDocumentTimelineDescription(label),
		ReferenceType:   TimelineReferenceMedicalDocument,
		ReferenceID:     &ref,
		Severity:        "info",
		EventDate:       eventDate,
		CreatedBy:       actorID,
	}
	return tx.Create(event).Error
}

func sanitizeDocumentTimelineDescription(label string) string {
	trimmed := strings.TrimSpace(label)
	if utf8.RuneCountInString(trimmed) > 255 {
		runes := []rune(trimmed)
		return string(runes[:255])
	}
	return trimmed
}
