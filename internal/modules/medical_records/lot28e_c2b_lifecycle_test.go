package medical_records

import (
	"errors"
	"testing"
)

func TestLot28EC2B_P01_P20_DocumentLifecycle(t *testing.T) {
	db := c1TestDB(t)
	repo := NewRepository(db)

	record := MedicalRecord{PatientID: 90, RecordNumber: "MR-90", Status: "active"}
	if err := db.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&stubConsultationRow{ID: 901, PatientID: 90}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&stubConsultationRow{ID: 902, PatientID: 91}).Error; err != nil {
		t.Fatal(err)
	}

	label := "Compte rendu"
	typ := "report"
	url := "https://docs.example.com/cr.pdf"
	sameID := uint(901)
	otherID := uint(902)
	creator := uint(21)

	// P01 create → UploadedBy creator, archive fields NULL
	err := repo.SaveCommonMedicalRecord(&record, UpdateCommonMedicalRecordRequest{
		Documents: PatchCollection[MedicalDocumentRequest]{
			Present: true,
			Upsert: []MedicalDocumentRequest{{
				Label:          &label,
				Type:           &typ,
				FileURL:        &url,
				ConsultationID: NullableUintPatch{Set: true, Value: &sameID},
			}},
		},
	}, creator)
	if err != nil {
		t.Fatalf("P01: %v", err)
	}
	var created MedicalDocument
	if err := db.Where("medical_record_id = ?", record.ID).First(&created).Error; err != nil {
		t.Fatal(err)
	}
	if created.UploadedBy != creator {
		t.Fatalf("P01: UploadedBy=%d", created.UploadedBy)
	}
	if created.ArchivedAt != nil || created.ArchivedBy != nil {
		t.Fatalf("P01: archive fields must be NULL, got at=%v by=%v", created.ArchivedAt, created.ArchivedBy)
	}
	createdAt := created.CreatedAt
	parentStamp := record.UpdatedAt

	// P02/P03/P04 active metadata updates
	newLabel := "Compte rendu corrigé"
	newType := "certificate"
	err = repo.SaveCommonMedicalRecord(&record, UpdateCommonMedicalRecordRequest{
		ExpectedUpdatedAt: &parentStamp,
		Documents: PatchCollection[MedicalDocumentRequest]{
			Present: true,
			Upsert: []MedicalDocumentRequest{{
				ID:    created.ID,
				Label: &newLabel,
				Type:  &newType,
			}},
		},
	}, 99)
	if err != nil {
		t.Fatalf("P02-P04: %v", err)
	}
	if err := db.First(&created, created.ID).Error; err != nil {
		t.Fatal(err)
	}
	if created.Label != newLabel || created.Type != newType {
		t.Fatalf("P03/P04: label/type not updated")
	}
	if created.UploadedBy != creator {
		t.Fatalf("P02: creator changed")
	}
	if !created.CreatedAt.Equal(createdAt) {
		t.Fatalf("CreatedAt mutated")
	}
	parentStamp = record.UpdatedAt

	// P05 FileURL valid HTTPS update
	nextURL := "https://docs.example.com/cr-v2.pdf?sig=1"
	err = repo.SaveCommonMedicalRecord(&record, UpdateCommonMedicalRecordRequest{
		ExpectedUpdatedAt: &parentStamp,
		Documents: PatchCollection[MedicalDocumentRequest]{
			Present: true,
			Upsert:  []MedicalDocumentRequest{{ID: created.ID, FileURL: &nextURL}},
		},
	}, 99)
	if err != nil {
		t.Fatalf("P05: %v", err)
	}
	parentStamp = record.UpdatedAt

	// P06 ConsultationID same patient → NULL
	err = repo.SaveCommonMedicalRecord(&record, UpdateCommonMedicalRecordRequest{
		ExpectedUpdatedAt: &parentStamp,
		Documents: PatchCollection[MedicalDocumentRequest]{
			Present: true,
			Upsert: []MedicalDocumentRequest{{
				ID:             created.ID,
				ConsultationID: NullableUintPatch{Set: true, Value: nil},
			}},
		},
	}, 99)
	if err != nil {
		t.Fatalf("P06: %v", err)
	}
	parentStamp = record.UpdatedAt

	// P12 C1 cross-patient rejected
	err = repo.SaveCommonMedicalRecord(&record, UpdateCommonMedicalRecordRequest{
		ExpectedUpdatedAt: &parentStamp,
		Documents: PatchCollection[MedicalDocumentRequest]{
			Present: true,
			Upsert: []MedicalDocumentRequest{{
				ID:             created.ID,
				ConsultationID: NullableUintPatch{Set: true, Value: &otherID},
			}},
		},
	}, 99)
	if !errors.Is(err, ErrCommonMedicalRecordInvalid) {
		t.Fatalf("P12: want invalid, got %v", err)
	}

	// P13 C2-A unsafe FileURL rejected
	badURL := "http://docs.example.com/x.pdf"
	err = repo.SaveCommonMedicalRecord(&record, UpdateCommonMedicalRecordRequest{
		ExpectedUpdatedAt: &parentStamp,
		Documents: PatchCollection[MedicalDocumentRequest]{
			Present: true,
			Upsert:  []MedicalDocumentRequest{{ID: created.ID, FileURL: &badURL}},
		},
	}, 99)
	if !errors.Is(err, ErrCommonMedicalRecordInvalid) {
		t.Fatalf("P13: want invalid, got %v", err)
	}

	// P08 omit documents → no archive
	if err := db.First(&created, created.ID).Error; err != nil {
		t.Fatal(err)
	}
	keptURL := created.FileURL
	err = repo.SaveCommonMedicalRecord(&record, UpdateCommonMedicalRecordRequest{
		ExpectedUpdatedAt: &parentStamp,
		Profile:           &PatientMedicalProfileRequest{Profession: str("médecin")},
	}, 99)
	if err != nil {
		t.Fatalf("P08 profile: %v", err)
	}
	parentStamp = record.UpdatedAt
	if err := db.First(&created, created.ID).Error; err != nil {
		t.Fatal(err)
	}
	if created.ArchivedAt != nil || created.FileURL != keptURL {
		t.Fatalf("P08: document must remain active unchanged")
	}

	// P18 same ID in upsert + delete_ids → reject
	desc := "should not apply"
	err = repo.SaveCommonMedicalRecord(&record, UpdateCommonMedicalRecordRequest{
		ExpectedUpdatedAt: &parentStamp,
		Documents: PatchCollection[MedicalDocumentRequest]{
			Present:   true,
			DeleteIDs: []uint{created.ID},
			Upsert:    []MedicalDocumentRequest{{ID: created.ID, Description: &desc}},
		},
	}, 55)
	if !errors.Is(err, ErrCommonMedicalRecordInvalid) {
		t.Fatalf("P18: want invalid, got %v", err)
	}
	if err := db.First(&created, created.ID).Error; err != nil {
		t.Fatal(err)
	}
	if created.ArchivedAt != nil || created.Description == desc {
		t.Fatalf("P18: conflicting request must not mutate")
	}

	// P19 duplicate delete_ids
	err = repo.SaveCommonMedicalRecord(&record, UpdateCommonMedicalRecordRequest{
		ExpectedUpdatedAt: &parentStamp,
		Documents: PatchCollection[MedicalDocumentRequest]{
			Present:   true,
			DeleteIDs: []uint{created.ID, created.ID},
		},
	}, 55)
	if !errors.Is(err, ErrCommonMedicalRecordInvalid) {
		t.Fatalf("P19: want invalid, got %v", err)
	}

	// P07 archive via delete_ids — row remains
	archiver := uint(55)
	beforeArchiveStamp := parentStamp
	err = repo.SaveCommonMedicalRecord(&record, UpdateCommonMedicalRecordRequest{
		ExpectedUpdatedAt: &parentStamp,
		Documents: PatchCollection[MedicalDocumentRequest]{
			Present:   true,
			DeleteIDs: []uint{created.ID},
		},
	}, archiver)
	if err != nil {
		t.Fatalf("P07: %v", err)
	}
	if record.UpdatedAt.Equal(beforeArchiveStamp) {
		t.Fatal("P20: parent UpdatedAt must bump after archive")
	}
	parentStamp = record.UpdatedAt

	var archived MedicalDocument
	if err := db.First(&archived, created.ID).Error; err != nil {
		t.Fatalf("P07 hard-delete proof: row missing: %v", err)
	}
	if archived.ArchivedAt == nil {
		t.Fatal("P07: ArchivedAt nil")
	}
	if archived.ArchivedBy == nil || *archived.ArchivedBy != archiver {
		t.Fatalf("P07: ArchivedBy=%v want %d", archived.ArchivedBy, archiver)
	}
	if archived.UploadedBy != creator {
		t.Fatalf("P07: UploadedBy changed")
	}
	if !archived.CreatedAt.Equal(createdAt) {
		t.Fatal("P07: CreatedAt changed")
	}
	if archived.FileURL != keptURL || archived.Label != newLabel {
		t.Fatal("P07: metadata must be preserved")
	}

	// P09 ordinary read excludes archived
	resp, err := repo.GetCommonMedicalRecord(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range resp.Documents {
		if d.ID == created.ID {
			t.Fatal("P09: archived document leaked in ordinary read")
		}
	}

	// P10 update archived → rejected (no resurrection / no new row)
	var countBefore int64
	db.Model(&MedicalDocument{}).Where("medical_record_id = ?", record.ID).Count(&countBefore)
	reviveLabel := "resurrect"
	err = repo.SaveCommonMedicalRecord(&record, UpdateCommonMedicalRecordRequest{
		ExpectedUpdatedAt: &parentStamp,
		Documents: PatchCollection[MedicalDocumentRequest]{
			Present: true,
			Upsert:  []MedicalDocumentRequest{{ID: created.ID, Label: &reviveLabel}},
		},
	}, 99)
	if !errors.Is(err, ErrCommonMedicalRecordInvalid) {
		t.Fatalf("P10: want invalid, got %v", err)
	}
	var countAfter int64
	db.Model(&MedicalDocument{}).Where("medical_record_id = ?", record.ID).Count(&countAfter)
	if countAfter != countBefore {
		t.Fatal("P10: must not create replacement row")
	}
	if err := db.First(&archived, created.ID).Error; err != nil {
		t.Fatal(err)
	}
	if archived.Label == reviveLabel || archived.ArchivedAt == nil {
		t.Fatal("P10: archived row mutated or unarchived")
	}

	// P11 re-archive already archived → invalid (no unarchive path)
	err = repo.SaveCommonMedicalRecord(&record, UpdateCommonMedicalRecordRequest{
		ExpectedUpdatedAt: &parentStamp,
		Documents: PatchCollection[MedicalDocumentRequest]{
			Present:   true,
			DeleteIDs: []uint{created.ID},
		},
	}, 99)
	if !errors.Is(err, ErrCommonMedicalRecordInvalid) {
		t.Fatalf("P11: want invalid re-archive, got %v", err)
	}

	// Create second active doc for remaining tests
	label2 := "Doc 2"
	url2 := "https://docs.example.com/d2.pdf"
	err = repo.SaveCommonMedicalRecord(&record, UpdateCommonMedicalRecordRequest{
		ExpectedUpdatedAt: &parentStamp,
		Documents: PatchCollection[MedicalDocumentRequest]{
			Present: true,
			Upsert: []MedicalDocumentRequest{{
				Label:   &label2,
				Type:    &typ,
				FileURL: &url2,
			}},
		},
	}, creator)
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	parentStamp = record.UpdatedAt
	var second MedicalDocument
	if err := db.Where("medical_record_id = ? AND archived_at IS NULL", record.ID).First(&second).Error; err != nil {
		t.Fatal(err)
	}

	// P17 delete_id outside aggregate
	otherRecord := MedicalRecord{PatientID: 91, RecordNumber: "MR-91", Status: "active"}
	if err := db.Create(&otherRecord).Error; err != nil {
		t.Fatal(err)
	}
	foreign := MedicalDocument{
		MedicalRecordID: otherRecord.ID,
		PatientID:       91,
		Type:            "report",
		Label:           "foreign",
		FileURL:         "https://docs.example.com/f.pdf",
		UploadedBy:      1,
	}
	if err := db.Create(&foreign).Error; err != nil {
		t.Fatal(err)
	}
	err = repo.SaveCommonMedicalRecord(&record, UpdateCommonMedicalRecordRequest{
		ExpectedUpdatedAt: &parentStamp,
		Documents: PatchCollection[MedicalDocumentRequest]{
			Present:   true,
			DeleteIDs: []uint{foreign.ID},
		},
	}, 99)
	if !errors.Is(err, ErrCommonMedicalRecordChild) {
		t.Fatalf("P17: want child error, got %v", err)
	}

	// P14 archive failure rolls back other mutations
	beforeProf, err := repo.GetCommonMedicalRecord(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	err = repo.SaveCommonMedicalRecord(&record, UpdateCommonMedicalRecordRequest{
		ExpectedUpdatedAt: &parentStamp,
		Profile:           &PatientMedicalProfileRequest{Profession: str("ne doit pas persister")},
		Documents: PatchCollection[MedicalDocumentRequest]{
			Present:   true,
			DeleteIDs: []uint{foreign.ID},
		},
	}, 99)
	if !errors.Is(err, ErrCommonMedicalRecordChild) {
		t.Fatalf("P14: want child error, got %v", err)
	}
	afterProf, err := repo.GetCommonMedicalRecord(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if beforeProf.Profile != nil && afterProf.Profile != nil &&
		beforeProf.Profile.Profession != afterProf.Profile.Profession {
		t.Fatal("P14: profile mutation must roll back")
	}
	if err := db.First(&second, second.ID).Error; err != nil {
		t.Fatal(err)
	}
	if second.ArchivedAt != nil {
		t.Fatal("P14: local doc must not be archived")
	}

	// P15 stale OCC archive → 409
	stale := parentStamp
	err = repo.SaveCommonMedicalRecord(&record, UpdateCommonMedicalRecordRequest{
		ExpectedUpdatedAt: &parentStamp,
		Documents: PatchCollection[MedicalDocumentRequest]{
			Present: true,
			Upsert:  []MedicalDocumentRequest{{ID: second.ID, Description: str("A edit")}},
		},
	}, 11)
	if err != nil {
		t.Fatalf("P15 setup A: %v", err)
	}
	freshStamp := record.UpdatedAt
	err = repo.SaveCommonMedicalRecord(&record, UpdateCommonMedicalRecordRequest{
		ExpectedUpdatedAt: &stale,
		Documents: PatchCollection[MedicalDocumentRequest]{
			Present:   true,
			DeleteIDs: []uint{second.ID},
		},
	}, 22)
	if !errors.Is(err, ErrCommonMedicalRecordConflict) {
		t.Fatalf("P15: want conflict, got %v", err)
	}
	if err := db.First(&second, second.ID).Error; err != nil {
		t.Fatal(err)
	}
	if second.ArchivedAt != nil {
		t.Fatal("P15: stale archive must not apply")
	}
	if second.Description != "A edit" {
		t.Fatal("P15: A state must remain")
	}

	// P16 stale OCC edit after archive → 409, no resurrection
	err = repo.SaveCommonMedicalRecord(&record, UpdateCommonMedicalRecordRequest{
		ExpectedUpdatedAt: &freshStamp,
		Documents: PatchCollection[MedicalDocumentRequest]{
			Present:   true,
			DeleteIDs: []uint{second.ID},
		},
	}, archiver)
	if err != nil {
		t.Fatalf("P16 archive A: %v", err)
	}
	err = repo.SaveCommonMedicalRecord(&record, UpdateCommonMedicalRecordRequest{
		ExpectedUpdatedAt: &freshStamp,
		Documents: PatchCollection[MedicalDocumentRequest]{
			Present: true,
			Upsert:  []MedicalDocumentRequest{{ID: second.ID, Label: str("stale edit")}},
		},
	}, 33)
	if !errors.Is(err, ErrCommonMedicalRecordConflict) {
		t.Fatalf("P16: want conflict, got %v", err)
	}
	if err := db.First(&second, second.ID).Error; err != nil {
		t.Fatal(err)
	}
	if second.ArchivedAt == nil || second.Label == "stale edit" {
		t.Fatal("P16: must remain archived without resurrection")
	}
}

func TestLot28EC2B_MigrationExistingRowsRemainActive(t *testing.T) {
	db := c1TestDB(t)
	rec := MedicalRecord{PatientID: 1, RecordNumber: "MR-1", Status: "active"}
	if err := db.Create(&rec).Error; err != nil {
		t.Fatal(err)
	}
	doc := MedicalDocument{
		MedicalRecordID: rec.ID,
		PatientID:       1,
		Type:            "report",
		Label:           "legacy",
		FileURL:         "https://docs.example.com/legacy.pdf",
		UploadedBy:      7,
	}
	if err := db.Create(&doc).Error; err != nil {
		t.Fatal(err)
	}
	var loaded MedicalDocument
	if err := db.First(&loaded, doc.ID).Error; err != nil {
		t.Fatal(err)
	}
	if loaded.ArchivedAt != nil || loaded.ArchivedBy != nil {
		t.Fatalf("existing rows must remain active NULL archive fields")
	}
	if !medicalDocumentIsActive(&loaded) {
		t.Fatal("expected active")
	}
}
