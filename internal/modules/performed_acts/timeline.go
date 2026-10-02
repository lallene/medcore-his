package performed_acts

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/modules/medical_records"
	"gorm.io/gorm"
)

// LOT28E-B3 — PerformedAct clinical longitudinal timeline (same-TX / fail-closed).
const (
	TimelineEventPerformedActPerformed = "performed_act_performed"
	TimelineEventPerformedActVoided    = "performed_act_voided"
	TimelineCategoryPerformedAct       = "performed_act"
	TimelineReferencePerformedAct      = "PerformedAct"

	timelineTitlePerformed = "Acte réalisé"
	timelineTitleVoided    = "Acte annulé"
)

// resolveMedicalRecordForPatientTx loads the patient's MedicalRecord inside caller TX.
// Missing record fails closed — no PA create/void without timeline home.
func resolveMedicalRecordForPatientTx(tx *gorm.DB, patientID uint) (*medical_records.MedicalRecord, error) {
	if tx == nil {
		return nil, coreerrors.Internal("performed_acts timeline: nil transaction")
	}
	var record medical_records.MedicalRecord
	if err := tx.Where("patient_id = ?", patientID).First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, coreerrors.Conflict("Dossier médical introuvable pour ce patient")
		}
		return nil, err
	}
	return &record, nil
}

func sanitizePerformedActTimelineDescription(label string) string {
	trimmed := strings.TrimSpace(label)
	if utf8.RuneCountInString(trimmed) > 255 {
		runes := []rune(trimmed)
		return string(runes[:255])
	}
	return trimmed
}

// recordPerformedActPerformedTimeline inserts performed_act_performed using caller TX.
// EventDate is the authoritative PerformedAt (not persistence time).
func recordPerformedActPerformedTimeline(
	tx *gorm.DB,
	record *medical_records.MedicalRecord,
	act *Act,
	actorID uint,
) error {
	if tx == nil || record == nil || act == nil || act.ID == 0 {
		return coreerrors.Internal("performed_acts timeline: invalid performed payload")
	}
	ref := act.ID
	event := &medical_records.MedicalTimelineEvent{
		MedicalRecordID: record.ID,
		PatientID:       act.PatientID,
		EventType:       TimelineEventPerformedActPerformed,
		Category:        TimelineCategoryPerformedAct,
		Title:           timelineTitlePerformed,
		Description:     sanitizePerformedActTimelineDescription(act.ActLabel),
		ReferenceType:   TimelineReferencePerformedAct,
		ReferenceID:     &ref,
		Severity:        "info",
		EventDate:       act.PerformedAt,
		CreatedBy:       actorID,
	}
	return tx.Create(event).Error
}

// recordPerformedActVoidedTimeline inserts performed_act_voided using caller TX.
// EventDate must be the transition VoidedAt (same authoritative instant).
func recordPerformedActVoidedTimeline(
	tx *gorm.DB,
	record *medical_records.MedicalRecord,
	act *Act,
	actorID uint,
	voidedAt time.Time,
) error {
	if tx == nil || record == nil || act == nil || act.ID == 0 {
		return coreerrors.Internal("performed_acts timeline: invalid void payload")
	}
	ref := act.ID
	event := &medical_records.MedicalTimelineEvent{
		MedicalRecordID: record.ID,
		PatientID:       act.PatientID,
		EventType:       TimelineEventPerformedActVoided,
		Category:        TimelineCategoryPerformedAct,
		Title:           timelineTitleVoided,
		Description:     "", // no VoidReason / label required on void occurrence
		ReferenceType:   TimelineReferencePerformedAct,
		ReferenceID:     &ref,
		Severity:        "info",
		EventDate:       voidedAt,
		CreatedBy:       actorID,
	}
	return tx.Create(event).Error
}
