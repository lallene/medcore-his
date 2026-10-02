package medical_records

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestLot28EC2A_U01_U15_ValidateMedicalDocumentFileURL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{name: "U01 valid https", raw: "https://docs.example.com/report.pdf", wantErr: false},
		{name: "U02 https path query", raw: "https://docs.example.com/a/b?token=abc&x=1", wantErr: false},
		{name: "U03 relative", raw: "/documents/a.pdf", wantErr: true},
		{name: "U04 http", raw: "http://docs.example.com/a.pdf", wantErr: true},
		{name: "U05 javascript", raw: "javascript:alert(1)", wantErr: true},
		{name: "U06 data", raw: "data:text/plain;base64,AAAA", wantErr: true},
		{name: "U07 file", raw: "file:///tmp/x.pdf", wantErr: true},
		{name: "U08 ftp", raw: "ftp://files.example.com/a.pdf", wantErr: true},
		{name: "U09 missing host", raw: "https:///path-only", wantErr: true},
		{name: "U10 user password", raw: "https://user:password@docs.example.com/a.pdf", wantErr: true},
		{name: "U11 user only", raw: "https://user@docs.example.com/a.pdf", wantErr: true},
		{name: "U12 empty", raw: "   ", wantErr: true},
		{name: "U13 over length", raw: "https://example.com/" + strings.Repeat("a", MedicalDocumentFileURLMaxLen), wantErr: true},
		{name: "U14 fragment accepted", raw: "https://docs.example.com/report.pdf#section-2", wantErr: false},
		{name: "U15 mixed-case scheme", raw: "HTTPS://Docs.Example.com/Report.PDF", wantErr: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateMedicalDocumentFileURL(tc.raw)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error for %q", tc.raw)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.raw, err)
			}
			if err != nil && !errors.Is(err, ErrCommonMedicalRecordInvalid) {
				t.Fatalf("want ErrCommonMedicalRecordInvalid, got %v", err)
			}
			// Errors must not echo the raw URL (query-bearing secrets).
			if err != nil && strings.Contains(err.Error(), "token=") {
				t.Fatalf("error must not echo URL secrets: %v", err)
			}
		})
	}
}

func TestLot28EC2A_W01_W09_DocumentWritePath(t *testing.T) {
	db := c1TestDB(t)
	repo := NewRepository(db)

	record := MedicalRecord{PatientID: 70, RecordNumber: "MR-70", Status: "active"}
	if err := db.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&stubConsultationRow{ID: 801, PatientID: 70}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&stubConsultationRow{ID: 802, PatientID: 71}).Error; err != nil {
		t.Fatal(err)
	}

	label := "Compte rendu externe"
	typ := "report"
	validURL := "https://docs.example.com/cr.pdf"
	sameID := uint(801)
	otherID := uint(802)

	// W01 valid create
	err := repo.SaveCommonMedicalRecord(&record, UpdateCommonMedicalRecordRequest{
		Documents: PatchCollection[MedicalDocumentRequest]{
			Present: true,
			Upsert: []MedicalDocumentRequest{{
				Label:          &label,
				Type:           &typ,
				FileURL:        &validURL,
				ConsultationID: NullableUintPatch{Set: true, Value: &sameID},
			}},
		},
	}, 11)
	if err != nil {
		t.Fatalf("W01: %v", err)
	}
	var created MedicalDocument
	if err := db.Where("medical_record_id = ?", record.ID).First(&created).Error; err != nil {
		t.Fatal(err)
	}
	if created.UploadedBy != 11 {
		t.Fatalf("W08: UploadedBy want 11 got %d", created.UploadedBy)
	}
	if created.FileURL != validURL {
		t.Fatalf("W01: file_url rewritten unexpectedly: %q", created.FileURL)
	}

	// W02 invalid URL create
	bad := "javascript:alert(1)"
	err = repo.SaveCommonMedicalRecord(&record, UpdateCommonMedicalRecordRequest{
		Documents: PatchCollection[MedicalDocumentRequest]{
			Present: true,
			Upsert: []MedicalDocumentRequest{{
				Label:   &label,
				Type:    &typ,
				FileURL: &bad,
			}},
		},
	}, 11)
	if !errors.Is(err, ErrCommonMedicalRecordInvalid) {
		t.Fatalf("W02: want invalid, got %v", err)
	}

	// W03 valid URL update
	next := "https://docs.example.com/cr-v2.pdf?sig=1"
	err = repo.SaveCommonMedicalRecord(&record, UpdateCommonMedicalRecordRequest{
		Documents: PatchCollection[MedicalDocumentRequest]{
			Present: true,
			Upsert: []MedicalDocumentRequest{{
				ID:      created.ID,
				FileURL: &next,
			}},
		},
	}, 99)
	if err != nil {
		t.Fatalf("W03: %v", err)
	}
	var after MedicalDocument
	if err := db.First(&after, created.ID).Error; err != nil {
		t.Fatal(err)
	}
	if after.FileURL != next {
		t.Fatalf("W03: got %q", after.FileURL)
	}
	if after.UploadedBy != 11 {
		t.Fatalf("W08: UploadedBy must remain creator, got %d", after.UploadedBy)
	}

	// W04 invalid URL update
	httpURL := "http://docs.example.com/x.pdf"
	err = repo.SaveCommonMedicalRecord(&record, UpdateCommonMedicalRecordRequest{
		Documents: PatchCollection[MedicalDocumentRequest]{
			Present: true,
			Upsert: []MedicalDocumentRequest{{
				ID:      created.ID,
				FileURL: &httpURL,
			}},
		},
	}, 99)
	if !errors.Is(err, ErrCommonMedicalRecordInvalid) {
		t.Fatalf("W04: want invalid, got %v", err)
	}

	// W05 same-patient association still accepted
	err = repo.SaveCommonMedicalRecord(&record, UpdateCommonMedicalRecordRequest{
		Documents: PatchCollection[MedicalDocumentRequest]{
			Present: true,
			Upsert: []MedicalDocumentRequest{{
				ID:             created.ID,
				ConsultationID: NullableUintPatch{Set: true, Value: &sameID},
			}},
		},
	}, 99)
	if err != nil {
		t.Fatalf("W05: %v", err)
	}

	// W06 cross-patient rejected
	err = repo.SaveCommonMedicalRecord(&record, UpdateCommonMedicalRecordRequest{
		Documents: PatchCollection[MedicalDocumentRequest]{
			Present: true,
			Upsert: []MedicalDocumentRequest{{
				ID:             created.ID,
				ConsultationID: NullableUintPatch{Set: true, Value: &otherID},
			}},
		},
	}, 99)
	if !errors.Is(err, ErrCommonMedicalRecordInvalid) {
		t.Fatalf("W06: want invalid, got %v", err)
	}

	// W07 null consultation preserved
	err = repo.SaveCommonMedicalRecord(&record, UpdateCommonMedicalRecordRequest{
		Documents: PatchCollection[MedicalDocumentRequest]{
			Present: true,
			Upsert: []MedicalDocumentRequest{{
				ID:             created.ID,
				ConsultationID: NullableUintPatch{Set: true, Value: nil},
			}},
		},
	}, 99)
	if err != nil {
		t.Fatalf("W07: %v", err)
	}

	// W09 unrelated description update does not rewrite file_url
	beforeURL := after.FileURL
	_ = db.First(&after, created.ID)
	beforeURL = after.FileURL
	desc := "note clinique"
	err = repo.SaveCommonMedicalRecord(&record, UpdateCommonMedicalRecordRequest{
		Documents: PatchCollection[MedicalDocumentRequest]{
			Present: true,
			Upsert: []MedicalDocumentRequest{{
				ID:          created.ID,
				Description: &desc,
			}},
		},
	}, 99)
	if err != nil {
		t.Fatalf("W09: %v", err)
	}
	if err := db.First(&after, created.ID).Error; err != nil {
		t.Fatal(err)
	}
	if after.FileURL != beforeURL {
		t.Fatalf("W09: file_url changed on unrelated update: %q → %q", beforeURL, after.FileURL)
	}
	if after.Description != desc {
		t.Fatalf("W09: description not updated")
	}
	_ = time.Now()
}
