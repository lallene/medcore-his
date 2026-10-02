package medical_records

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func b1DocLabel() *string { s := "Compte rendu B1"; return &s }
func b1DocType() *string  { s := "report"; return &s }
func b1DocURL() *string   { s := "https://docs.example.com/b1.pdf"; return &s }

func b1SeedRecord(t *testing.T, db *gorm.DB, patientID uint) MedicalRecord {
	t.Helper()
	record := MedicalRecord{PatientID: patientID, RecordNumber: fmt.Sprintf("B1-%d", patientID), Status: "active"}
	if err := db.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	return record
}

func b1CountEvents(t *testing.T, db *gorm.DB, where string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := db.Model(&MedicalTimelineEvent{}).Where(where, args...).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func b1CreateDoc(t *testing.T, db *gorm.DB, repo Repository, record *MedicalRecord, authorID uint, label string) MedicalDocument {
	t.Helper()
	lbl := label
	typ := "report"
	url := "https://docs.example.com/" + strings.ReplaceAll(label, " ", "-") + ".pdf"
	_, err := repo.SaveCommonMedicalRecord(record, UpdateCommonMedicalRecordRequest{
		ExpectedUpdatedAt: &record.UpdatedAt,
		Documents: PatchCollection[MedicalDocumentRequest]{
			Present: true,
			Upsert: []MedicalDocumentRequest{{
				Label:   &lbl,
				Type:    &typ,
				FileURL: &url,
			}},
		},
	}, authorID)
	if err != nil {
		t.Fatal(err)
	}
	var doc MedicalDocument
	if err := db.Where("medical_record_id = ? AND label = ? AND archived_at IS NULL", record.ID, label).
		Order("id DESC").First(&doc).Error; err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestLot28EB1_B101_B126_DocumentTimeline(t *testing.T) {
	db := c1TestDB(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(8)
	repo := NewRepository(db)
	service := NewService(repo)

	record := b1SeedRecord(t, db, 2801)
	creator := uint(41)
	archiver := uint(42)

	// B101/B102 create one document → exactly one document_added with correct identity
	doc1 := b1CreateDoc(t, db, repo, &record, creator, "Doc One")
	addedCount := b1CountEvents(t, db,
		"patient_id = ? AND event_type = ? AND reference_type = ? AND reference_id = ?",
		record.PatientID, TimelineEventDocumentAdded, TimelineReferenceMedicalDocument, doc1.ID)
	if addedCount != 1 {
		t.Fatalf("B101: document_added count=%d want 1", addedCount)
	}
	var added MedicalTimelineEvent
	if err := db.Where(
		"patient_id = ? AND event_type = ? AND reference_id = ?",
		record.PatientID, TimelineEventDocumentAdded, doc1.ID,
	).First(&added).Error; err != nil {
		t.Fatal(err)
	}
	if added.PatientID != record.PatientID {
		t.Fatalf("B102: patient=%d", added.PatientID)
	}
	if added.ReferenceType != TimelineReferenceMedicalDocument {
		t.Fatalf("B102: reference_type=%q", added.ReferenceType)
	}
	if added.ReferenceID == nil || *added.ReferenceID != doc1.ID {
		t.Fatalf("B102: reference_id=%v", added.ReferenceID)
	}
	if added.CreatedBy != creator {
		t.Fatalf("B102: actor=%d want %d", added.CreatedBy, creator)
	}
	if added.EventDate.IsZero() {
		t.Fatal("B102: EventDate zero")
	}
	payload := added.Title + " " + added.Description
	if strings.Contains(strings.ToLower(payload), "http") || strings.Contains(payload, "docs.example.com") {
		t.Fatalf("B102: URL leaked in payload: %q / %q", added.Title, added.Description)
	}
	if added.Title != documentAddedTitle() {
		t.Fatalf("B102: title=%q", added.Title)
	}

	// B103 create multiple → one event each
	doc2 := b1CreateDoc(t, db, repo, &record, creator, "Doc Two")
	doc3 := b1CreateDoc(t, db, repo, &record, creator, "Doc Three")
	for _, d := range []MedicalDocument{doc2, doc3} {
		n := b1CountEvents(t, db,
			"patient_id = ? AND event_type = ? AND reference_id = ?",
			record.PatientID, TimelineEventDocumentAdded, d.ID)
		if n != 1 {
			t.Fatalf("B103: doc %d added count=%d", d.ID, n)
		}
	}

	// B109 document-only create → no generic CMR (via service)
	recOnly := b1SeedRecord(t, db, 2802)
	beforeGeneric := b1CountEvents(t, db, "patient_id = ? AND event_type = ?", recOnly.PatientID, "common_medical_record_updated")
	if _, err := service.UpdateCommonMedicalRecord(recOnly.PatientID, UpdateCommonMedicalRecordRequest{
		ExpectedUpdatedAt: &recOnly.UpdatedAt,
		Documents: PatchCollection[MedicalDocumentRequest]{
			Present: true,
			Upsert: []MedicalDocumentRequest{{
				Label:   b1DocLabel(),
				Type:    b1DocType(),
				FileURL: b1DocURL(),
			}},
		},
	}, creator); err != nil {
		t.Fatalf("B109: %v", err)
	}
	afterGeneric := b1CountEvents(t, db, "patient_id = ? AND event_type = ?", recOnly.PatientID, "common_medical_record_updated")
	if afterGeneric != beforeGeneric {
		t.Fatalf("B109: generic CMR emitted for document-only create")
	}
	addedOnly := b1CountEvents(t, db, "patient_id = ? AND event_type = ?", recOnly.PatientID, TimelineEventDocumentAdded)
	if addedOnly != 1 {
		t.Fatalf("B109: document_added=%d", addedOnly)
	}

	// B107/B108/B111 metadata + FileURL-only → no document event, no generic
	stamp := record.UpdatedAt
	newLabel := "Label edit"
	_, err = repo.SaveCommonMedicalRecord(&record, UpdateCommonMedicalRecordRequest{
		ExpectedUpdatedAt: &stamp,
		Documents: PatchCollection[MedicalDocumentRequest]{
			Present: true,
			Upsert:  []MedicalDocumentRequest{{ID: doc1.ID, Label: &newLabel}},
		},
	}, 99)
	if err != nil {
		t.Fatalf("B107: %v", err)
	}
	if b1CountEvents(t, db, "event_type = ? AND reference_id = ?", "document_updated", doc1.ID) != 0 {
		t.Fatal("B107: document_updated must not exist")
	}
	metaGeneric := b1CountEvents(t, db, "patient_id = ? AND event_type = ?", record.PatientID, "common_medical_record_updated")
	if metaGeneric != 0 {
		t.Fatalf("B111: generic after metadata edit: %d", metaGeneric)
	}
	stamp = record.UpdatedAt
	newURL := "https://docs.example.com/b1-v2.pdf"
	beforeURLEvents := b1CountEvents(t, db, "patient_id = ? AND reference_id = ?", record.PatientID, doc1.ID)
	_, err = repo.SaveCommonMedicalRecord(&record, UpdateCommonMedicalRecordRequest{
		ExpectedUpdatedAt: &stamp,
		Documents: PatchCollection[MedicalDocumentRequest]{
			Present: true,
			Upsert:  []MedicalDocumentRequest{{ID: doc1.ID, FileURL: &newURL}},
		},
	}, 99)
	if err != nil {
		t.Fatalf("B108: %v", err)
	}
	afterURLEvents := b1CountEvents(t, db, "patient_id = ? AND reference_id = ?", record.PatientID, doc1.ID)
	if afterURLEvents != beforeURLEvents {
		t.Fatal("B108: FileURL edit must not add document timeline event")
	}
	if b1CountEvents(t, db, "patient_id = ? AND event_type = ?", record.PatientID, "common_medical_record_updated") != 0 {
		t.Fatal("B111: generic after FileURL edit")
	}

	// B112 mixed non-document + document create
	recMixed := b1SeedRecord(t, db, 2803)
	if err := db.Create(&PatientMedicalProfile{MedicalRecordID: recMixed.ID, PatientID: recMixed.PatientID, Profession: "old"}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateCommonMedicalRecord(recMixed.PatientID, UpdateCommonMedicalRecordRequest{
		ExpectedUpdatedAt: &recMixed.UpdatedAt,
		Profile:           &PatientMedicalProfileRequest{Profession: str("chirurgien")},
		Documents: PatchCollection[MedicalDocumentRequest]{
			Present: true,
			Upsert: []MedicalDocumentRequest{{
				Label:   b1DocLabel(),
				Type:    b1DocType(),
				FileURL: b1DocURL(),
			}},
		},
	}, creator); err != nil {
		t.Fatalf("B112: %v", err)
	}
	if b1CountEvents(t, db, "patient_id = ? AND event_type = ?", recMixed.PatientID, TimelineEventDocumentAdded) != 1 {
		t.Fatal("B112: missing document_added")
	}
	if b1CountEvents(t, db, "patient_id = ? AND event_type = ?", recMixed.PatientID, "common_medical_record_updated") != 1 {
		t.Fatal("B112: want exactly one generic CMR")
	}

	// B104/B105/B106/B110 archive
	stamp = record.UpdatedAt
	beforeArchivedEvents := b1CountEvents(t, db,
		"patient_id = ? AND event_type = ? AND reference_id = ?",
		record.PatientID, TimelineEventDocumentArchived, doc2.ID)
	_, err = repo.SaveCommonMedicalRecord(&record, UpdateCommonMedicalRecordRequest{
		ExpectedUpdatedAt: &stamp,
		Documents: PatchCollection[MedicalDocumentRequest]{
			Present:   true,
			DeleteIDs: []uint{doc2.ID},
		},
	}, archiver)
	if err != nil {
		t.Fatalf("B104: %v", err)
	}
	archCount := b1CountEvents(t, db,
		"patient_id = ? AND event_type = ? AND reference_id = ?",
		record.PatientID, TimelineEventDocumentArchived, doc2.ID)
	if archCount != beforeArchivedEvents+1 {
		t.Fatalf("B104: archived events=%d", archCount)
	}
	var archEvt MedicalTimelineEvent
	if err := db.Where(
		"patient_id = ? AND event_type = ? AND reference_id = ?",
		record.PatientID, TimelineEventDocumentArchived, doc2.ID,
	).First(&archEvt).Error; err != nil {
		t.Fatal(err)
	}
	if archEvt.CreatedBy != archiver {
		t.Fatalf("B105: actor=%d want %d", archEvt.CreatedBy, archiver)
	}
	if archEvt.PatientID != record.PatientID || archEvt.ReferenceID == nil || *archEvt.ReferenceID != doc2.ID {
		t.Fatal("B105: identity mismatch")
	}
	archPayload := strings.ToLower(archEvt.Title + " " + archEvt.Description)
	if strings.Contains(archPayload, "supprim") || strings.Contains(archPayload, "delet") {
		t.Fatalf("B105: archive wording implies deletion: %q", archEvt.Title)
	}
	if archEvt.Title != documentArchivedTitle() {
		t.Fatalf("B105: title=%q", archEvt.Title)
	}
	var archivedRow MedicalDocument
	if err := db.First(&archivedRow, doc2.ID).Error; err != nil {
		t.Fatalf("B106: row missing: %v", err)
	}
	if archivedRow.ArchivedAt == nil || archivedRow.ArchivedBy == nil || *archivedRow.ArchivedBy != archiver {
		t.Fatal("B106: archive fields incorrect")
	}

	// B110 document-only archive via service → no generic
	recArch := b1SeedRecord(t, db, 2804)
	dArch := b1CreateDoc(t, db, repo, &recArch, creator, "To Archive")
	if _, err := service.UpdateCommonMedicalRecord(recArch.PatientID, UpdateCommonMedicalRecordRequest{
		ExpectedUpdatedAt: &recArch.UpdatedAt,
		Documents: PatchCollection[MedicalDocumentRequest]{
			Present:   true,
			DeleteIDs: []uint{dArch.ID},
		},
	}, archiver); err != nil {
		t.Fatalf("B110: %v", err)
	}
	if b1CountEvents(t, db, "patient_id = ? AND event_type = ?", recArch.PatientID, "common_medical_record_updated") != 0 {
		t.Fatal("B110: generic CMR on document-only archive")
	}
	if b1CountEvents(t, db, "patient_id = ? AND event_type = ? AND reference_id = ?",
		recArch.PatientID, TimelineEventDocumentArchived, dArch.ID) != 1 {
		t.Fatal("B110: missing document_archived")
	}

	// B113 mixed non-document + archive
	recMixArch := b1SeedRecord(t, db, 2805)
	if err := db.Create(&PatientMedicalProfile{MedicalRecordID: recMixArch.ID, PatientID: recMixArch.PatientID, Profession: "a"}).Error; err != nil {
		t.Fatal(err)
	}
	dMix := b1CreateDoc(t, db, repo, &recMixArch, creator, "Mix Arch")
	if _, err := service.UpdateCommonMedicalRecord(recMixArch.PatientID, UpdateCommonMedicalRecordRequest{
		ExpectedUpdatedAt: &recMixArch.UpdatedAt,
		Profile:           &PatientMedicalProfileRequest{Profession: str("b")},
		Documents: PatchCollection[MedicalDocumentRequest]{
			Present:   true,
			DeleteIDs: []uint{dMix.ID},
		},
	}, archiver); err != nil {
		t.Fatalf("B113: %v", err)
	}
	if b1CountEvents(t, db, "patient_id = ? AND event_type = ?", recMixArch.PatientID, TimelineEventDocumentArchived) != 1 {
		t.Fatal("B113: missing document_archived")
	}
	if b1CountEvents(t, db, "patient_id = ? AND event_type = ?", recMixArch.PatientID, "common_medical_record_updated") != 1 {
		t.Fatal("B113: want exactly one generic CMR")
	}

	// B114 create + archive distinct documents → no replacement
	stamp = record.UpdatedAt
	newLbl := "Fresh Doc"
	newTyp := "report"
	newU := "https://docs.example.com/fresh.pdf"
	_, err = repo.SaveCommonMedicalRecord(&record, UpdateCommonMedicalRecordRequest{
		ExpectedUpdatedAt: &stamp,
		Documents: PatchCollection[MedicalDocumentRequest]{
			Present:   true,
			DeleteIDs: []uint{doc3.ID},
			Upsert: []MedicalDocumentRequest{{
				Label:   &newLbl,
				Type:    &newTyp,
				FileURL: &newU,
			}},
		},
	}, archiver)
	if err != nil {
		t.Fatalf("B114: %v", err)
	}
	if b1CountEvents(t, db, "patient_id = ? AND event_type = ? AND reference_id = ?",
		record.PatientID, TimelineEventDocumentArchived, doc3.ID) != 1 {
		t.Fatal("B114: missing archived for D1")
	}
	var fresh MedicalDocument
	if err := db.Where("medical_record_id = ? AND label = ? AND archived_at IS NULL", record.ID, newLbl).First(&fresh).Error; err != nil {
		t.Fatal(err)
	}
	if b1CountEvents(t, db, "patient_id = ? AND event_type = ? AND reference_id = ?",
		record.PatientID, TimelineEventDocumentAdded, fresh.ID) != 1 {
		t.Fatal("B114: missing added for D2")
	}
	if b1CountEvents(t, db, "event_type LIKE ?", "%replaced%") != 0 {
		t.Fatal("B114: replacement event must not exist")
	}

	// B115/B116 idempotent ensure
	if err := ensureDocumentTimelineEvent(db, &record, TimelineEventDocumentAdded, doc1.ID, doc1.Label, creator, time.Now()); err != nil {
		t.Fatal(err)
	}
	if b1CountEvents(t, db, "patient_id = ? AND event_type = ? AND reference_id = ?",
		record.PatientID, TimelineEventDocumentAdded, doc1.ID) != 1 {
		t.Fatal("B115: duplicate document_added")
	}
	if err := ensureDocumentTimelineEvent(db, &record, TimelineEventDocumentArchived, doc2.ID, doc2.Label, archiver, time.Now()); err != nil {
		t.Fatal(err)
	}
	if b1CountEvents(t, db, "patient_id = ? AND event_type = ? AND reference_id = ?",
		record.PatientID, TimelineEventDocumentArchived, doc2.ID) != 1 {
		t.Fatal("B116: duplicate document_archived")
	}

	// B117 same document may have added + archived
	bothDoc := b1CreateDoc(t, db, repo, &record, creator, "Both Events")
	stamp = record.UpdatedAt
	_, err = repo.SaveCommonMedicalRecord(&record, UpdateCommonMedicalRecordRequest{
		ExpectedUpdatedAt: &stamp,
		Documents: PatchCollection[MedicalDocumentRequest]{
			Present:   true,
			DeleteIDs: []uint{bothDoc.ID},
		},
	}, archiver)
	if err != nil {
		t.Fatalf("B117 archive: %v", err)
	}
	if b1CountEvents(t, db, "patient_id = ? AND event_type = ? AND reference_id = ?",
		record.PatientID, TimelineEventDocumentAdded, bothDoc.ID) != 1 {
		t.Fatal("B117: missing added")
	}
	if b1CountEvents(t, db, "patient_id = ? AND event_type = ? AND reference_id = ?",
		record.PatientID, TimelineEventDocumentArchived, bothDoc.ID) != 1 {
		t.Fatal("B117: missing archived")
	}

	// B118 timeline insert failure on CREATE → rollback
	recFailCreate := b1SeedRecord(t, db, 2818)
	if err := db.Migrator().DropTable(&MedicalTimelineEvent{}); err != nil {
		t.Fatal(err)
	}
	_, err = repo.SaveCommonMedicalRecord(&recFailCreate, UpdateCommonMedicalRecordRequest{
		ExpectedUpdatedAt: &recFailCreate.UpdatedAt,
		Documents: PatchCollection[MedicalDocumentRequest]{
			Present: true,
			Upsert: []MedicalDocumentRequest{{
				Label:   b1DocLabel(),
				Type:    b1DocType(),
				FileURL: b1DocURL(),
			}},
		},
	}, creator)
	if err == nil {
		t.Fatal("B118: expected timeline failure")
	}
	var failCreateCount int64
	db.Model(&MedicalDocument{}).Where("medical_record_id = ?", recFailCreate.ID).Count(&failCreateCount)
	if failCreateCount != 0 {
		t.Fatalf("B118: document persisted despite timeline failure: %d", failCreateCount)
	}
	if err := db.AutoMigrate(&MedicalTimelineEvent{}); err != nil {
		t.Fatal(err)
	}

	// B119 timeline insert failure on ARCHIVE → document remains active
	recFailArch := b1SeedRecord(t, db, 2819)
	live := b1CreateDoc(t, db, repo, &recFailArch, creator, "Fail Arch Target")
	stampBefore := recFailArch.UpdatedAt
	if err := db.Migrator().DropTable(&MedicalTimelineEvent{}); err != nil {
		t.Fatal(err)
	}
	_, err = repo.SaveCommonMedicalRecord(&recFailArch, UpdateCommonMedicalRecordRequest{
		ExpectedUpdatedAt: &stampBefore,
		Documents: PatchCollection[MedicalDocumentRequest]{
			Present:   true,
			DeleteIDs: []uint{live.ID},
		},
	}, archiver)
	if err == nil {
		t.Fatal("B119: expected timeline failure")
	}
	var still MedicalDocument
	if err := db.First(&still, live.ID).Error; err != nil {
		t.Fatal(err)
	}
	if still.ArchivedAt != nil || still.ArchivedBy != nil {
		t.Fatal("B119: document must remain active after archive TX rollback")
	}
	var parent MedicalRecord
	if err := db.First(&parent, recFailArch.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !parent.UpdatedAt.Equal(stampBefore) {
		t.Fatal("B119: parent stamp must roll back")
	}
	if err := db.AutoMigrate(&MedicalTimelineEvent{}); err != nil {
		t.Fatal(err)
	}

	// B120 C1 invalid association → no document event
	recC1 := b1SeedRecord(t, db, 2820)
	if err := db.Create(&stubConsultationRow{ID: 9991, PatientID: 9999}).Error; err != nil {
		t.Fatal(err)
	}
	beforeC1 := b1CountEvents(t, db, "patient_id = ?", recC1.PatientID)
	crossID := uint(9991)
	_, err = repo.SaveCommonMedicalRecord(&recC1, UpdateCommonMedicalRecordRequest{
		ExpectedUpdatedAt: &recC1.UpdatedAt,
		Documents: PatchCollection[MedicalDocumentRequest]{
			Present: true,
			Upsert: []MedicalDocumentRequest{{
				Label:          b1DocLabel(),
				Type:           b1DocType(),
				FileURL:        b1DocURL(),
				ConsultationID: NullableUintPatch{Set: true, Value: &crossID},
			}},
		},
	}, creator)
	if !errors.Is(err, ErrCommonMedicalRecordInvalid) {
		t.Fatalf("B120: want invalid, got %v", err)
	}
	if b1CountEvents(t, db, "patient_id = ?", recC1.PatientID) != beforeC1 {
		t.Fatal("B120: event written on C1 failure")
	}

	// B121 C2-A invalid URL → no document event
	recURL := b1SeedRecord(t, db, 2821)
	beforeURL := b1CountEvents(t, db, "patient_id = ?", recURL.PatientID)
	bad := "http://insecure.example.com/x.pdf"
	_, err = repo.SaveCommonMedicalRecord(&recURL, UpdateCommonMedicalRecordRequest{
		ExpectedUpdatedAt: &recURL.UpdatedAt,
		Documents: PatchCollection[MedicalDocumentRequest]{
			Present: true,
			Upsert: []MedicalDocumentRequest{{
				Label:   b1DocLabel(),
				Type:    b1DocType(),
				FileURL: &bad,
			}},
		},
	}, creator)
	if !errors.Is(err, ErrCommonMedicalRecordInvalid) {
		t.Fatalf("B121: want invalid, got %v", err)
	}
	if b1CountEvents(t, db, "patient_id = ?", recURL.PatientID) != beforeURL {
		t.Fatal("B121: event written on unsafe URL")
	}

	// B122 stale OCC archive → 409, no archive event
	recOCC := b1SeedRecord(t, db, 2822)
	docOCC := b1CreateDoc(t, db, repo, &recOCC, creator, "OCC Doc")
	stale := recOCC.UpdatedAt
	// bump parent via profile so OCC is stale
	if err := db.Create(&PatientMedicalProfile{MedicalRecordID: recOCC.ID, PatientID: recOCC.PatientID, Profession: "x"}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateCommonMedicalRecord(recOCC.PatientID, UpdateCommonMedicalRecordRequest{
		ExpectedUpdatedAt: &recOCC.UpdatedAt,
		Profile:           &PatientMedicalProfileRequest{Profession: str("y")},
	}, 99); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		rbac.SetUser(c, archiver, "doctor", nil)
		c.Next()
	})
	router.PUT("/api/patients/:id/common-medical-record", NewHandler(service).UpdateCommonMedicalRecord)
	body, _ := json.Marshal(map[string]any{
		"expected_updated_at": stale,
		"documents":           map[string]any{"delete_ids": []uint{docOCC.ID}},
	})
	req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/patients/%d/common-medical-record", recOCC.PatientID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("B122: status=%d body=%s", w.Code, w.Body.String())
	}
	if b1CountEvents(t, db, "patient_id = ? AND event_type = ? AND reference_id = ?",
		recOCC.PatientID, TimelineEventDocumentArchived, docOCC.ID) != 0 {
		t.Fatal("B122: archive event on stale OCC")
	}
	var occDoc MedicalDocument
	db.First(&occDoc, docOCC.ID)
	if occDoc.ArchivedAt != nil {
		t.Fatal("B122: document archived on stale OCC")
	}

	// B123 cross-aggregate archive → no event
	recA := b1SeedRecord(t, db, 2823)
	recB := b1SeedRecord(t, db, 2824)
	foreign := MedicalDocument{
		MedicalRecordID: recB.ID,
		PatientID:       recB.PatientID,
		Type:            "report",
		Label:           "foreign",
		FileURL:         "https://docs.example.com/f.pdf",
		UploadedBy:      creator,
	}
	if err := db.Create(&foreign).Error; err != nil {
		t.Fatal(err)
	}
	_, err = repo.SaveCommonMedicalRecord(&recA, UpdateCommonMedicalRecordRequest{
		ExpectedUpdatedAt: &recA.UpdatedAt,
		Documents: PatchCollection[MedicalDocumentRequest]{
			Present:   true,
			DeleteIDs: []uint{foreign.ID},
		},
	}, archiver)
	if !errors.Is(err, ErrCommonMedicalRecordChild) {
		t.Fatalf("B123: want child err, got %v", err)
	}
	if b1CountEvents(t, db, "reference_id = ? AND event_type = ?", foreign.ID, TimelineEventDocumentArchived) != 0 {
		t.Fatal("B123: event on cross-aggregate archive")
	}

	// B124 archived document update → no event
	recUpd := b1SeedRecord(t, db, 2825)
	dUpd := b1CreateDoc(t, db, repo, &recUpd, creator, "Upd Arch")
	stamp = recUpd.UpdatedAt
	_, err = repo.SaveCommonMedicalRecord(&recUpd, UpdateCommonMedicalRecordRequest{
		ExpectedUpdatedAt: &stamp,
		Documents: PatchCollection[MedicalDocumentRequest]{
			Present:   true,
			DeleteIDs: []uint{dUpd.ID},
		},
	}, archiver)
	if err != nil {
		t.Fatal(err)
	}
	beforeUpd := b1CountEvents(t, db, "patient_id = ?", recUpd.PatientID)
	stamp = recUpd.UpdatedAt
	revive := "nope"
	_, err = repo.SaveCommonMedicalRecord(&recUpd, UpdateCommonMedicalRecordRequest{
		ExpectedUpdatedAt: &stamp,
		Documents: PatchCollection[MedicalDocumentRequest]{
			Present: true,
			Upsert:  []MedicalDocumentRequest{{ID: dUpd.ID, Label: &revive}},
		},
	}, 99)
	if !errors.Is(err, ErrCommonMedicalRecordInvalid) {
		t.Fatalf("B124: want invalid, got %v", err)
	}
	if b1CountEvents(t, db, "patient_id = ?", recUpd.PatientID) != beforeUpd {
		t.Fatal("B124: event on archived update rejection")
	}

	// B125 no-op CMR → no timeline event
	recNoop := b1SeedRecord(t, db, 2826)
	beforeNoop := b1CountEvents(t, db, "patient_id = ?", recNoop.PatientID)
	if _, err := service.UpdateCommonMedicalRecord(recNoop.PatientID, UpdateCommonMedicalRecordRequest{
		ExpectedUpdatedAt: &recNoop.UpdatedAt,
	}, 99); err != nil {
		t.Fatal(err)
	}
	if b1CountEvents(t, db, "patient_id = ?", recNoop.PatientID) != beforeNoop {
		t.Fatal("B125: timeline on no-op")
	}

	// B126 concurrency: parent FOR UPDATE serializes ensure → exactly one event
	recConc := b1SeedRecord(t, db, 2826+100)
	docConc := MedicalDocument{
		MedicalRecordID: recConc.ID,
		PatientID:       recConc.PatientID,
		Type:            "report",
		Label:           "Concurrent",
		FileURL:         "https://docs.example.com/c.pdf",
		UploadedBy:      creator,
	}
	if err := db.Create(&docConc).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("PRAGMA busy_timeout = 10000").Error; err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errCh := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var last error
			for attempt := 0; attempt < 20; attempt++ {
				last = db.Transaction(func(tx *gorm.DB) error {
					var current MedicalRecord
					if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
						First(&current, recConc.ID).Error; err != nil {
						return err
					}
					return ensureDocumentTimelineEvent(
						tx,
						&current,
						TimelineEventDocumentAdded,
						docConc.ID,
						docConc.Label,
						creator,
						time.Now(),
					)
				})
				if last == nil {
					errCh <- nil
					return
				}
				msg := last.Error()
				if strings.Contains(msg, "locked") || strings.Contains(msg, "BUSY") {
					time.Sleep(time.Duration(5+attempt) * time.Millisecond)
					continue
				}
				errCh <- last
				return
			}
			errCh <- last
		}()
	}
	wg.Wait()
	close(errCh)
	for e := range errCh {
		if e != nil {
			t.Fatalf("B126: worker error: %v", e)
		}
	}
	if b1CountEvents(t, db, "patient_id = ? AND event_type = ? AND reference_id = ?",
		recConc.PatientID, TimelineEventDocumentAdded, docConc.ID) != 1 {
		t.Fatal("B126: concurrency produced duplicate document_added")
	}
}
