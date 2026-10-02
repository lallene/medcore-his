package medical_records

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Minimal consultations row for association / summary projection tests (C1).
type stubConsultationRow struct {
	ID                      uint `gorm:"primaryKey"`
	PatientID               uint `gorm:"not null;index"`
	DoctorName              string
	Service                 string
	Status                  string
	Diagnosis               string
	Observations            string
	Treatment               string
	SickLeaveRequired       bool
	HospitalizationRequired bool
	CreatedAt               time.Time
}

func (stubConsultationRow) TableName() string { return "consultations" }

type stubExamRequestRow struct {
	ID             uint `gorm:"primaryKey"`
	ConsultationID uint `gorm:"index"`
}

func (stubExamRequestRow) TableName() string { return "consultation_exam_requests" }

type stubPrescriptionRow struct {
	ID             uint `gorm:"primaryKey"`
	ConsultationID uint `gorm:"index"`
}

func (stubPrescriptionRow) TableName() string { return "consultation_prescriptions" }

func c1TestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:lot28ec1_"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&MedicalRecord{},
		&PatientMedicalProfile{},
		&Allergy{},
		&MedicalHistory{},
		&SurgicalHistory{},
		&FamilyMedicalHistory{},
		&RegularTreatment{},
		&Vaccination{},
		&Disability{},
		&Lifestyle{},
		&MedicalDevice{},
		&VitalSign{},
		&MedicalDocument{},
		&MedicalTimelineEvent{},
		&stubConsultationRow{},
		&stubExamRequestRow{},
		&stubPrescriptionRow{},
	); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestLot28EC1_CanProjectConsultationDocuments(t *testing.T) {
	if canProjectConsultationDocuments(nil) {
		t.Fatal("nil perms must not project")
	}
	if canProjectConsultationDocuments([]string{"medical_records.read", "patients.360.read"}) {
		t.Fatal("medical_records.read / patients.360.read alone must not project")
	}
	if !canProjectConsultationDocuments([]string{"medical_records.read", "consultations.read"}) {
		t.Fatal("consultations.read must project")
	}
	if !canProjectConsultationDocuments([]string{"*"}) {
		t.Fatal("* must project")
	}
}

func TestLot28EC1_S01_S02_SummaryDocumentProjection(t *testing.T) {
	db := c1TestDB(t)
	repo := NewRepository(db)
	svc := NewService(repo)

	record := &MedicalRecord{PatientID: 41, RecordNumber: "MR-41", Status: "active"}
	if err := db.Create(record).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&stubConsultationRow{ID: 901, PatientID: 41, CreatedAt: time.Now(), Status: "completed"}).Error; err != nil {
		t.Fatal(err)
	}

	withDocs, err := svc.GetPatientMedicalSummary(41, []string{"medical_records.read", "consultations.read"})
	if err != nil {
		t.Fatal(err)
	}
	if len(withDocs.Documents) == 0 {
		t.Fatal("S01: expected generated document projection with consultations.read")
	}
	for _, d := range withDocs.Documents {
		if d.URL == "" {
			t.Fatal("S01: projected document missing URL")
		}
	}

	without, err := svc.GetPatientMedicalSummary(41, []string{"medical_records.read"})
	if err != nil {
		t.Fatal(err)
	}
	if without.MedicalRecord.ID == 0 {
		t.Fatal("S02: summary medical record must remain readable")
	}
	if len(without.Documents) != 0 {
		t.Fatalf("S02: expected no PDF URLs without consultations.read, got %d", len(without.Documents))
	}

	shellOnly, err := svc.GetPatientMedicalSummary(41, []string{"patients.360.read"})
	if err != nil {
		t.Fatal(err)
	}
	if len(shellOnly.Documents) != 0 {
		t.Fatal("S03: patients.360.read alone must not project documents")
	}
}

func TestLot28EC1_S04_SummaryRoutePermissionUnchanged(t *testing.T) {
	db := c1TestDB(t)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		rbac.SetUser(c, 7, "staff", []string{"patients.360.read"})
		c.Next()
	})
	api := r.Group("/api")
	RegisterRoutes(api, NewHandler(NewService(NewRepository(db))))

	req := httptest.NewRequest(http.MethodGet, "/api/patients/1/medical-summary", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("S04: expected 403 without medical_records.read, got %d", w.Code)
	}
}

func TestLot28EC1_M01_M06_DocumentConsultationAssociation(t *testing.T) {
	db := c1TestDB(t)
	repo := NewRepository(db)

	recordA := MedicalRecord{PatientID: 10, RecordNumber: "A", Status: "active"}
	recordB := MedicalRecord{PatientID: 20, RecordNumber: "B", Status: "active"}
	if err := db.Create(&recordA).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&recordB).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&stubConsultationRow{ID: 501, PatientID: 10}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&stubConsultationRow{ID: 502, PatientID: 20}).Error; err != nil {
		t.Fatal(err)
	}

	sameID := uint(501)
	otherID := uint(502)
	unknownID := uint(99999)
	label := "Doc"
	typ := "REPORT"

	// M01 same patient
	err := repo.SaveCommonMedicalRecord(&recordA, UpdateCommonMedicalRecordRequest{
		Documents: PatchCollection[MedicalDocumentRequest]{
			Present: true,
			Upsert: []MedicalDocumentRequest{{
				Label:          &label,
				Type:           &typ,
				ConsultationID: NullableUintPatch{Set: true, Value: &sameID},
			}},
		},
	}, 7)
	if err != nil {
		t.Fatalf("M01: %v", err)
	}

	// M02 other patient
	err = repo.SaveCommonMedicalRecord(&recordA, UpdateCommonMedicalRecordRequest{
		Documents: PatchCollection[MedicalDocumentRequest]{
			Present: true,
			Upsert: []MedicalDocumentRequest{{
				Label:          &label,
				Type:           &typ,
				ConsultationID: NullableUintPatch{Set: true, Value: &otherID},
			}},
		},
	}, 7)
	if !errors.Is(err, ErrCommonMedicalRecordInvalid) {
		t.Fatalf("M02: want invalid, got %v", err)
	}

	// M03 unknown
	err = repo.SaveCommonMedicalRecord(&recordA, UpdateCommonMedicalRecordRequest{
		Documents: PatchCollection[MedicalDocumentRequest]{
			Present: true,
			Upsert: []MedicalDocumentRequest{{
				Label:          &label,
				Type:           &typ,
				ConsultationID: NullableUintPatch{Set: true, Value: &unknownID},
			}},
		},
	}, 7)
	if !errors.Is(err, ErrCommonMedicalRecordInvalid) {
		t.Fatalf("M03: want invalid, got %v", err)
	}

	// M04 null consultation_id
	err = repo.SaveCommonMedicalRecord(&recordA, UpdateCommonMedicalRecordRequest{
		Documents: PatchCollection[MedicalDocumentRequest]{
			Present: true,
			Upsert: []MedicalDocumentRequest{{
				Label:          &label,
				Type:           &typ,
				ConsultationID: NullableUintPatch{Set: true, Value: nil},
			}},
		},
	}, 7)
	if err != nil {
		t.Fatalf("M04: %v", err)
	}

	// M05 update cannot reassociate cross-patient
	var doc MedicalDocument
	if err := db.Where("medical_record_id = ?", recordA.ID).Order("id desc").First(&doc).Error; err != nil {
		t.Fatal(err)
	}
	err = repo.SaveCommonMedicalRecord(&recordA, UpdateCommonMedicalRecordRequest{
		Documents: PatchCollection[MedicalDocumentRequest]{
			Present: true,
			Upsert: []MedicalDocumentRequest{{
				ID:             doc.ID,
				ConsultationID: NullableUintPatch{Set: true, Value: &otherID},
			}},
		},
	}, 7)
	if !errors.Is(err, ErrCommonMedicalRecordInvalid) {
		t.Fatalf("M05: want invalid, got %v", err)
	}

	// M06 other record cannot attach consultation of A via its patient path
	err = repo.SaveCommonMedicalRecord(&recordB, UpdateCommonMedicalRecordRequest{
		Documents: PatchCollection[MedicalDocumentRequest]{
			Present: true,
			Upsert: []MedicalDocumentRequest{{
				Label:          &label,
				Type:           &typ,
				ConsultationID: NullableUintPatch{Set: true, Value: &sameID},
			}},
		},
	}, 7)
	if !errors.Is(err, ErrCommonMedicalRecordInvalid) {
		t.Fatalf("M06: want invalid, got %v", err)
	}
}

func TestLot28EC1_SummaryHandlerOmitsPDFURLsWithoutConsultationsRead(t *testing.T) {
	db := c1TestDB(t)
	record := &MedicalRecord{PatientID: 55, RecordNumber: "MR-55", Status: "active"}
	if err := db.Create(record).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&stubConsultationRow{ID: 700, PatientID: 55}).Error; err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		rbac.SetUser(c, 3, "staff", []string{"medical_records.read"})
		c.Next()
	})
	api := r.Group("/api")
	RegisterRoutes(api, NewHandler(NewService(NewRepository(db))))

	req := httptest.NewRequest(http.MethodGet, "/api/patients/55/medical-summary", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d body=%s", w.Code, w.Body.String())
	}
	var body PatientMedicalSummaryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Documents) != 0 {
		t.Fatalf("handler must omit documents without consultations.read, got %#v", body.Documents)
	}
}
