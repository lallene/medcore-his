package medical_records

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func integrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL absent: tests PostgreSQL d'intégrité ignorés")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("connexion PostgreSQL de test: %v", err)
	}
	schemaName := fmt.Sprintf("medical_records_%d", time.Now().UnixNano())
	if err := admin.Exec(`CREATE SCHEMA "` + schemaName + `"`).Error; err != nil {
		t.Fatalf("création du schéma: %v", err)
	}
	t.Cleanup(func() { _ = admin.Exec(`DROP SCHEMA IF EXISTS "` + schemaName + `" CASCADE`).Error })

	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("TEST_DATABASE_URL invalide: %v", err)
	}
	db, err := gorm.Open(postgres.Open(u.String()), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{TablePrefix: `"` + schemaName + `".`},
	})
	if err != nil {
		t.Fatalf("connexion au schéma de test: %v", err)
	}
	if err := db.AutoMigrate(
		&MedicalRecord{}, &PatientMedicalProfile{}, &Allergy{}, &MedicalHistory{},
		&SurgicalHistory{}, &FamilyMedicalHistory{}, &RegularTreatment{}, &Vaccination{},
		&Disability{}, &Lifestyle{}, &MedicalDevice{}, &VitalSign{}, &MedicalDocument{},
		&MedicalTimelineEvent{},
	); err != nil {
		t.Fatalf("migration du schéma de test: %v", err)
	}
	return db
}

func str(value string) *string { return &value }
func boolean(value bool) *bool { return &value }

type commonFixture struct {
	record     MedicalRecord
	profile    PatientMedicalProfile
	allergy    Allergy
	history    MedicalHistory
	surgery    SurgicalHistory
	family     FamilyMedicalHistory
	treatment  RegularTreatment
	vaccine    Vaccination
	disability Disability
	lifestyle  Lifestyle
	device     MedicalDevice
	vital      VitalSign
	document   MedicalDocument
}

func seedCommonFixture(t *testing.T, db *gorm.DB, patientID uint) commonFixture {
	t.Helper()
	now := time.Date(2026, 1, 2, 10, 30, 0, 0, time.UTC)
	date := time.Date(2020, 4, 5, 0, 0, 0, 0, time.UTC)
	consultationID := patientID + 900
	weight := 71.5
	active := true
	f := commonFixture{
		record: MedicalRecord{PatientID: patientID, RecordNumber: fmt.Sprintf("TEST-%d", patientID), Status: "active", UpdatedAt: now},
	}
	if err := db.Create(&f.record).Error; err != nil {
		t.Fatal(err)
	}
	f.profile = PatientMedicalProfile{MedicalRecordID: f.record.ID, PatientID: patientID, Email: "distinct@example.test", Address: "Adresse distinctive", Profession: "Ancienne profession", BloodGroup: "AB", Rhesus: "negative", UpdatedBy: 41}
	f.allergy = Allergy{MedicalRecordID: f.record.ID, PatientID: patientID, AllergenType: "rare-type", AllergenName: "Allergène distinctif", Reaction: "Réaction", Severity: "rare-severity", Comment: "Commentaire allergie", IsActive: active, CreatedBy: 42}
	f.history = MedicalHistory{MedicalRecordID: f.record.ID, PatientID: patientID, Type: "chronic", Title: "Historique distinctif", Description: "Description historique", StartDate: &date, Status: "active", Severity: "critical-distinctive", Comment: "Commentaire historique", CreatedBy: 43}
	f.surgery = SurgicalHistory{MedicalRecordID: f.record.ID, PatientID: patientID, ProcedureName: "Intervention distinctive", ProcedureDate: &date, Facility: "Hôpital", Complications: "Aucune", Comment: "Commentaire chirurgie", CreatedBy: 44}
	f.family = FamilyMedicalHistory{MedicalRecordID: f.record.ID, PatientID: patientID, Disease: "Maladie familiale", Relationship: "parent", Comment: "Commentaire famille", CreatedBy: 45}
	f.treatment = RegularTreatment{MedicalRecordID: f.record.ID, PatientID: patientID, MedicationName: "Traitement distinctif", Dosage: "5 mg", Frequency: "matin", StartDate: &date, Prescriber: "Dr Test", IsActive: false, CreatedBy: 46}
	f.vaccine = Vaccination{MedicalRecordID: f.record.ID, PatientID: patientID, VaccineName: "Vaccin distinctif", Dose: "dose 2", VaccinationDate: &date, Status: "custom", CreatedBy: 47}
	f.disability = Disability{MedicalRecordID: f.record.ID, PatientID: patientID, Type: "Handicap distinctif", Level: "modéré", SpecialNeeds: "Besoin distinctif", CreatedBy: 48}
	f.lifestyle = Lifestyle{MedicalRecordID: f.record.ID, PatientID: patientID, Tobacco: "never", Alcohol: "rare", PhysicalActivity: "weekly", Diet: "Régime distinctif", UpdatedBy: 49}
	f.device = MedicalDevice{MedicalRecordID: f.record.ID, PatientID: patientID, Type: "custom-device", Name: "Dispositif distinctif", Reference: "REF-1", ImplantationDate: &date, Comment: "Commentaire dispositif", IsActive: false, CreatedBy: 50}
	f.vital = VitalSign{MedicalRecordID: f.record.ID, PatientID: patientID, ConsultationID: &consultationID, WeightKg: &weight, Comment: "Commentaire constante", MeasuredBy: 51, MeasuredAt: now}
	f.document = MedicalDocument{MedicalRecordID: f.record.ID, PatientID: patientID, ConsultationID: &consultationID, Type: "pdf", Label: "Document distinctif", FileName: "distinct.pdf", MimeType: "application/pdf", FileURL: "/distinct.pdf", Description: "Description document", DocumentDate: nil, UploadedBy: 52}
	for _, entity := range []any{&f.profile, &f.allergy, &f.history, &f.surgery, &f.family, &f.treatment, &f.vaccine, &f.disability, &f.lifestyle, &f.device, &f.vital, &f.document} {
		if err := db.Create(entity).Error; err != nil {
			t.Fatalf("création fixture %T: %v", entity, err)
		}
	}
	return f
}

func TestListTimelineEventsFiltersAndOrdersBackendEvents(t *testing.T) {
	db := integrationDB(t)
	repo := NewRepository(db)
	record := MedicalRecord{PatientID: 91001, RecordNumber: "TIMELINE-91001", Status: "active"}
	otherRecord := MedicalRecord{PatientID: 91002, RecordNumber: "TIMELINE-91002", Status: "active"}
	if err := db.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&otherRecord).Error; err != nil {
		t.Fatal(err)
	}

	older := time.Date(2026, 8, 1, 8, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 8, 2, 8, 0, 0, 0, time.UTC)
	events := []MedicalTimelineEvent{
		{MedicalRecordID: record.ID, PatientID: record.PatientID, EventType: "allergy_added", Category: "allergy", Title: "Allergie", EventDate: older},
		{MedicalRecordID: record.ID, PatientID: record.PatientID, EventType: "soap_updated", Category: "consultation", Title: "SOAP", EventDate: newer},
		{MedicalRecordID: record.ID, PatientID: record.PatientID, EventType: "exam_requested", Category: "exam", Title: "Examen", EventDate: newer},
		{MedicalRecordID: otherRecord.ID, PatientID: otherRecord.PatientID, EventType: "document_uploaded", Category: "document", Title: "Document", EventDate: newer},
	}
	for index := range events {
		if err := db.Create(&events[index]).Error; err != nil {
			t.Fatal(err)
		}
	}

	result, err := repo.ListTimelineEvents(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 3 {
		t.Fatalf("événements retournés = %d, attendu 3", len(result))
	}
	if result[0].ID != events[2].ID || result[1].ID != events[1].ID || result[2].ID != events[0].ID {
		t.Fatalf("ordre timeline incorrect: ids=%v", []uint{result[0].ID, result[1].ID, result[2].ID})
	}
	seen := map[uint]bool{}
	for _, event := range result {
		if seen[event.ID] {
			t.Fatalf("événement dupliqué: %d", event.ID)
		}
		seen[event.ID] = true
	}

	empty, err := repo.ListTimelineEvents(otherRecord.ID + 1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Fatalf("timeline vide attendue, reçu %d événements", len(empty))
	}
}

func snapshotCommonRecord(t *testing.T, repo Repository, recordID uint) CommonMedicalRecordResponse {
	t.Helper()
	response, err := repo.GetCommonMedicalRecord(recordID)
	if err != nil {
		t.Fatal(err)
	}
	return *response
}

func TestCommonMedicalRecordProfileOnlyRoundTripPreservesEveryCollection(t *testing.T) {
	db := integrationDB(t)
	f := seedCommonFixture(t, db, 101)
	repo := NewRepository(db)
	before := snapshotCommonRecord(t, repo, f.record.ID)

	profession := "Nouvelle profession"
	req := UpdateCommonMedicalRecordRequest{ExpectedUpdatedAt: &before.MedicalRecord.UpdatedAt, Profile: &PatientMedicalProfileRequest{Profession: &profession}}
	if err := repo.SaveCommonMedicalRecord(&before.MedicalRecord, req, 99); err != nil {
		t.Fatal(err)
	}
	after := snapshotCommonRecord(t, repo, f.record.ID)
	if after.Profile.Profession != profession {
		t.Fatalf("profession = %q", after.Profile.Profession)
	}
	before.Profile.Profession = profession
	before.Profile.UpdatedAt = after.Profile.UpdatedAt
	before.Profile.UpdatedBy = after.Profile.UpdatedBy
	before.MedicalRecord.UpdatedAt = after.MedicalRecord.UpdatedAt
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("des données hors profession ont changé\navant: %#v\naprès: %#v", before, after)
	}
	if after.HistoryRiskFields() != (historyRisk{f.history.ID, f.history.CreatedAt, f.history.CreatedBy, f.history.Severity, f.history.Description, f.history.Comment}) {
		t.Fatal("champs historiques à risque altérés")
	}
	if after.VitalSigns[0].ConsultationID == nil || *after.VitalSigns[0].ConsultationID != *f.vital.ConsultationID || after.VitalSigns[0].Comment != f.vital.Comment {
		t.Fatal("consultation_id/comment des constantes altérés")
	}
	if after.Documents[0].DocumentDate != nil || after.Documents[0].FileName != f.document.FileName || after.Documents[0].MimeType != f.document.MimeType {
		t.Fatal("métadonnées documentaires altérées")
	}
}

type historyRisk struct {
	id        uint
	createdAt time.Time
	createdBy uint
	severity  string
	desc      string
	comment   string
}

func (r CommonMedicalRecordResponse) HistoryRiskFields() historyRisk {
	h := r.MedicalHistories[0]
	return historyRisk{h.ID, h.CreatedAt, h.CreatedBy, h.Severity, h.Description, h.Comment}
}

func TestCommonMedicalRecordAbsentAndEmptyCollectionsAreNoOp(t *testing.T) {
	db := integrationDB(t)
	f := seedCommonFixture(t, db, 102)
	repo := NewRepository(db)
	before := snapshotCommonRecord(t, repo, f.record.ID)
	if err := repo.SaveCommonMedicalRecord(&before.MedicalRecord, UpdateCommonMedicalRecordRequest{}, 99); err != nil {
		t.Fatal(err)
	}
	emptyJSON := []string{"allergies", "medical_histories", "surgical_histories", "family_medical_histories", "regular_treatments", "vaccinations", "disabilities", "medical_devices", "vital_signs", "documents"}
	for _, field := range emptyJSON {
		t.Run(field, func(t *testing.T) {
			var req UpdateCommonMedicalRecordRequest
			if err := json.Unmarshal([]byte(fmt.Sprintf(`{"%s":{"upsert":[],"delete_ids":[]}}`, field)), &req); err != nil {
				t.Fatal(err)
			}
			if err := repo.SaveCommonMedicalRecord(&before.MedicalRecord, req, 99); err != nil {
				t.Fatal(err)
			}
		})
	}
	after := snapshotCommonRecord(t, repo, f.record.ID)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("une collection absente ou vide a modifié le dossier")
	}
}

func TestCommonMedicalRecordTargetedUpdatesPreserveIdentityAndAudit(t *testing.T) {
	db := integrationDB(t)
	f := seedCommonFixture(t, db, 103)
	repo := NewRepository(db)
	tests := []struct {
		name string
		req  UpdateCommonMedicalRecordRequest
		load func() (uint, time.Time, uint, string)
	}{
		{"allergy", UpdateCommonMedicalRecordRequest{Allergies: PatchCollection[AllergyRequest]{Present: true, Upsert: []AllergyRequest{{ID: f.allergy.ID, Comment: str("modifié")}}}}, func() (uint, time.Time, uint, string) {
			var v Allergy
			db.First(&v, f.allergy.ID)
			return v.ID, v.CreatedAt, v.CreatedBy, v.Comment
		}},
		{"medical_history", UpdateCommonMedicalRecordRequest{MedicalHistories: PatchCollection[MedicalHistoryRequest]{Present: true, Upsert: []MedicalHistoryRequest{{ID: f.history.ID, Comment: str("modifié")}}}}, func() (uint, time.Time, uint, string) {
			var v MedicalHistory
			db.First(&v, f.history.ID)
			return v.ID, v.CreatedAt, v.CreatedBy, v.Comment
		}},
		{"surgical_history", UpdateCommonMedicalRecordRequest{SurgicalHistories: PatchCollection[SurgicalHistoryRequest]{Present: true, Upsert: []SurgicalHistoryRequest{{ID: f.surgery.ID, Comment: str("modifié")}}}}, func() (uint, time.Time, uint, string) {
			var v SurgicalHistory
			db.First(&v, f.surgery.ID)
			return v.ID, v.CreatedAt, v.CreatedBy, v.Comment
		}},
		{"family_history", UpdateCommonMedicalRecordRequest{FamilyMedicalHistories: PatchCollection[FamilyMedicalHistoryRequest]{Present: true, Upsert: []FamilyMedicalHistoryRequest{{ID: f.family.ID, Comment: str("modifié")}}}}, func() (uint, time.Time, uint, string) {
			var v FamilyMedicalHistory
			db.First(&v, f.family.ID)
			return v.ID, v.CreatedAt, v.CreatedBy, v.Comment
		}},
		{"treatment", UpdateCommonMedicalRecordRequest{RegularTreatments: PatchCollection[RegularTreatmentRequest]{Present: true, Upsert: []RegularTreatmentRequest{{ID: f.treatment.ID, Dosage: str("10 mg")}}}}, func() (uint, time.Time, uint, string) {
			var v RegularTreatment
			db.First(&v, f.treatment.ID)
			return v.ID, v.CreatedAt, v.CreatedBy, v.Dosage
		}},
		{"vaccination", UpdateCommonMedicalRecordRequest{Vaccinations: PatchCollection[VaccinationRequest]{Present: true, Upsert: []VaccinationRequest{{ID: f.vaccine.ID, Dose: str("dose 3")}}}}, func() (uint, time.Time, uint, string) {
			var v Vaccination
			db.First(&v, f.vaccine.ID)
			return v.ID, v.CreatedAt, v.CreatedBy, v.Dose
		}},
		{"disability", UpdateCommonMedicalRecordRequest{Disabilities: PatchCollection[DisabilityRequest]{Present: true, Upsert: []DisabilityRequest{{ID: f.disability.ID, Level: str("fort")}}}}, func() (uint, time.Time, uint, string) {
			var v Disability
			db.First(&v, f.disability.ID)
			return v.ID, v.CreatedAt, v.CreatedBy, v.Level
		}},
		{"device", UpdateCommonMedicalRecordRequest{MedicalDevices: PatchCollection[MedicalDeviceRequest]{Present: true, Upsert: []MedicalDeviceRequest{{ID: f.device.ID, Comment: str("modifié")}}}}, func() (uint, time.Time, uint, string) {
			var v MedicalDevice
			db.First(&v, f.device.ID)
			return v.ID, v.CreatedAt, v.CreatedBy, v.Comment
		}},
		{"vital", UpdateCommonMedicalRecordRequest{VitalSigns: PatchCollection[VitalSignRequest]{Present: true, Upsert: []VitalSignRequest{{ID: f.vital.ID, Comment: str("modifié")}}}}, func() (uint, time.Time, uint, string) {
			var v VitalSign
			db.First(&v, f.vital.ID)
			return v.ID, v.CreatedAt, v.MeasuredBy, v.Comment
		}},
		{"document", UpdateCommonMedicalRecordRequest{Documents: PatchCollection[MedicalDocumentRequest]{Present: true, Upsert: []MedicalDocumentRequest{{ID: f.document.ID, Description: str("modifié")}}}}, func() (uint, time.Time, uint, string) {
			var v MedicalDocument
			db.First(&v, f.document.ID)
			return v.ID, v.CreatedAt, v.UploadedBy, v.Description
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			beforeID, beforeCreated, beforeBy, _ := tc.load()
			if err := repo.SaveCommonMedicalRecord(&f.record, tc.req, 99); err != nil {
				t.Fatal(err)
			}
			afterID, afterCreated, afterBy, value := tc.load()
			if beforeID != afterID || !beforeCreated.Equal(afterCreated) || beforeBy != afterBy || value == "" {
				t.Fatal("identité/audit non préservé ou valeur non enregistrée")
			}
		})
	}
}

func TestCommonMedicalRecordCreateAndExplicitDelete(t *testing.T) {
	db := integrationDB(t)
	f := seedCommonFixture(t, db, 104)
	repo := NewRepository(db)
	req := UpdateCommonMedicalRecordRequest{Allergies: PatchCollection[AllergyRequest]{Present: true, Upsert: []AllergyRequest{{AllergenType: str("food"), AllergenName: str("Nouvelle allergie")}}}}
	if err := repo.SaveCommonMedicalRecord(&f.record, req, 77); err != nil {
		t.Fatal(err)
	}
	var allergies []Allergy
	db.Where("medical_record_id = ?", f.record.ID).Order("id").Find(&allergies)
	if len(allergies) != 2 || allergies[1].ID == 0 || allergies[0].ID != f.allergy.ID {
		t.Fatalf("création incorrecte: %#v", allergies)
	}
	if err := repo.SaveCommonMedicalRecord(&f.record, UpdateCommonMedicalRecordRequest{Allergies: PatchCollection[AllergyRequest]{Present: true, DeleteIDs: []uint{allergies[1].ID}}}, 77); err != nil {
		t.Fatal(err)
	}
	db.Where("medical_record_id = ?", f.record.ID).Find(&allergies)
	if len(allergies) != 1 || allergies[0].ID != f.allergy.ID {
		t.Fatalf("suppression non ciblée: %#v", allergies)
	}
}

func TestCommonMedicalRecordForeignIDRollsBack(t *testing.T) {
	db := integrationDB(t)
	a := seedCommonFixture(t, db, 105)
	b := seedCommonFixture(t, db, 106)
	repo := NewRepository(db)
	beforeA := snapshotCommonRecord(t, repo, a.record.ID)
	beforeB := snapshotCommonRecord(t, repo, b.record.ID)
	req := UpdateCommonMedicalRecordRequest{
		Profile:   &PatientMedicalProfileRequest{Profession: str("ne doit pas persister")},
		Allergies: PatchCollection[AllergyRequest]{Present: true, DeleteIDs: []uint{b.allergy.ID}},
	}
	err := repo.SaveCommonMedicalRecord(&a.record, req, 99)
	if !errors.Is(err, ErrCommonMedicalRecordChild) {
		t.Fatalf("erreur = %v", err)
	}
	if !reflect.DeepEqual(beforeA, snapshotCommonRecord(t, repo, a.record.ID)) || !reflect.DeepEqual(beforeB, snapshotCommonRecord(t, repo, b.record.ID)) {
		t.Fatal("rollback foreign-ID incomplet")
	}

	req.Allergies = PatchCollection[AllergyRequest]{Present: true, Upsert: []AllergyRequest{{ID: b.allergy.ID, Comment: str("intrusion")}}}
	err = repo.SaveCommonMedicalRecord(&a.record, req, 99)
	if !errors.Is(err, ErrCommonMedicalRecordChild) {
		t.Fatalf("erreur update étranger = %v", err)
	}
}

func TestCommonMedicalRecordAbsentFalseEmptyAndNullSemantics(t *testing.T) {
	db := integrationDB(t)
	f := seedCommonFixture(t, db, 107)
	repo := NewRepository(db)
	if err := repo.SaveCommonMedicalRecord(&f.record, UpdateCommonMedicalRecordRequest{Allergies: PatchCollection[AllergyRequest]{Present: true, Upsert: []AllergyRequest{{ID: f.allergy.ID, Comment: str(""), IsActive: boolean(false)}}}, Documents: PatchCollection[MedicalDocumentRequest]{Present: true, Upsert: []MedicalDocumentRequest{{ID: f.document.ID, DocumentDate: NullableTimePatch{Set: true, Value: nil}}}}}, 99); err != nil {
		t.Fatal(err)
	}
	var allergy Allergy
	var document MedicalDocument
	db.First(&allergy, f.allergy.ID)
	db.First(&document, f.document.ID)
	if allergy.IsActive || allergy.Comment != "" || allergy.Reaction != f.allergy.Reaction {
		t.Fatalf("sémantique absent/false/vide incorrecte: %#v", allergy)
	}
	if document.DocumentDate != nil || document.FileName != f.document.FileName {
		t.Fatal("sémantique date null/absente incorrecte")
	}
}

func TestCommonMedicalRecordOptimisticConflictAndNoOp(t *testing.T) {
	db := integrationDB(t)
	f := seedCommonFixture(t, db, 108)
	repo := NewRepository(db)
	original := f.record.UpdatedAt
	noOp := UpdateCommonMedicalRecordRequest{ExpectedUpdatedAt: &original}
	if err := repo.SaveCommonMedicalRecord(&f.record, noOp, 99); err != nil || !f.record.UpdatedAt.Equal(original) {
		t.Fatalf("no-op: err=%v updated_at=%v", err, f.record.UpdatedAt)
	}
	if err := repo.SaveCommonMedicalRecord(&f.record, UpdateCommonMedicalRecordRequest{ExpectedUpdatedAt: &original, Profile: &PatientMedicalProfileRequest{Profession: str("première modification")}}, 99); err != nil {
		t.Fatal(err)
	}
	if f.record.UpdatedAt.Equal(original) {
		t.Fatal("updated_at inchangé après modification réelle")
	}
	err := repo.SaveCommonMedicalRecord(&f.record, UpdateCommonMedicalRecordRequest{ExpectedUpdatedAt: &original, Profile: &PatientMedicalProfileRequest{Profession: str("écriture obsolète")}}, 99)
	if !errors.Is(err, ErrCommonMedicalRecordConflict) {
		t.Fatalf("conflit attendu, reçu %v", err)
	}
	var profile PatientMedicalProfile
	db.Where("medical_record_id = ?", f.record.ID).First(&profile)
	if profile.Profession != "première modification" {
		t.Fatal("écriture conflictuelle persistée")
	}
}

func TestCommonMedicalRecordTimelineOnlyForCommittedChange(t *testing.T) {
	db := integrationDB(t)
	f := seedCommonFixture(t, db, 109)
	repo := NewRepository(db)
	service := NewService(repo)
	if _, err := service.UpdateCommonMedicalRecord(f.record.PatientID, UpdateCommonMedicalRecordRequest{ExpectedUpdatedAt: &f.record.UpdatedAt}, 88); err != nil {
		t.Fatal(err)
	}
	var count int64
	db.Model(&MedicalTimelineEvent{}).Count(&count)
	if count != 0 {
		t.Fatal("timeline créée pour un no-op")
	}
	response, err := service.UpdateCommonMedicalRecord(f.record.PatientID, UpdateCommonMedicalRecordRequest{ExpectedUpdatedAt: &f.record.UpdatedAt, Profile: &PatientMedicalProfileRequest{Profession: str("timeline")}}, 88)
	if err != nil {
		t.Fatal(err)
	}
	db.Model(&MedicalTimelineEvent{}).Count(&count)
	if count != 1 {
		t.Fatalf("événements après modification = %d", count)
	}
	_, err = service.UpdateCommonMedicalRecord(f.record.PatientID, UpdateCommonMedicalRecordRequest{ExpectedUpdatedAt: &response.MedicalRecord.UpdatedAt, Profile: &PatientMedicalProfileRequest{Profession: str("rollback")}, Allergies: PatchCollection[AllergyRequest]{Present: true, DeleteIDs: []uint{999999}}}, 88)
	if !errors.Is(err, ErrCommonMedicalRecordChild) {
		t.Fatal("rollback attendu")
	}
	db.Model(&MedicalTimelineEvent{}).Count(&count)
	if count != 1 {
		t.Fatal("timeline créée pour une transaction rollback")
	}
}

func TestCommonMedicalRecordLegacyArraysAreUpsertOnly(t *testing.T) {
	db := integrationDB(t)
	f := seedCommonFixture(t, db, 110)
	repo := NewRepository(db)
	payload := fmt.Sprintf(`{"allergies":[{"id":%d,"comment":"legacy"}],"medical_histories":[{"id":%d,"comment":"legacy"}]}`, f.allergy.ID, f.history.ID)
	var req UpdateCommonMedicalRecordRequest
	if err := json.Unmarshal([]byte(payload), &req); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveCommonMedicalRecord(&f.record, req, 99); err != nil {
		t.Fatal(err)
	}
	var allergyCount, historyCount int64
	db.Model(&Allergy{}).Where("medical_record_id = ?", f.record.ID).Count(&allergyCount)
	db.Model(&MedicalHistory{}).Where("medical_record_id = ?", f.record.ID).Count(&historyCount)
	if allergyCount != 1 || historyCount != 1 {
		t.Fatal("un payload legacy a supprimé des lignes")
	}
}

func TestUpdateCommonMedicalRecordHandlerReturns409(t *testing.T) {
	db := integrationDB(t)
	f := seedCommonFixture(t, db, 111)
	repo := NewRepository(db)
	service := NewService(repo)
	if _, err := service.UpdateCommonMedicalRecord(f.record.PatientID, UpdateCommonMedicalRecordRequest{ExpectedUpdatedAt: &f.record.UpdatedAt, Profile: &PatientMedicalProfileRequest{Profession: str("courante")}}, 99); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"expected_updated_at": f.record.UpdatedAt, "profile": map[string]string{"profession": "obsolète"}})
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		rbac.SetUser(c, 99, "doctor", nil)
		c.Next()
	})
	router.PUT("/api/patients/:id/common-medical-record", NewHandler(service).UpdateCommonMedicalRecord)
	request := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/patients/%d/common-medical-record", f.record.PatientID), bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), ErrCommonMedicalRecordConflict.Error()) {
		t.Fatalf("réponse = %d %s", response.Code, response.Body.String())
	}
}

func TestCommonMedicalRecordIgnoresSpoofedAuthorsAndUsesJWTAuthor(t *testing.T) {
	db := integrationDB(t)
	f := seedCommonFixture(t, db, 112)
	repo := NewRepository(db)
	payload := `{
		"updated_by":999,
		"profile":{"profession":"JWT only"},
		"allergies":{"upsert":[{"allergen_type":"food","allergen_name":"JWT allergy","created_by":999}]},
		"vital_signs":{"upsert":[{"comment":"JWT vital","measured_by":999}]},
		"documents":{"upsert":[{"type":"pdf","label":"JWT document","uploaded_by":999}]}
	}`
	var req UpdateCommonMedicalRecordRequest
	if err := json.Unmarshal([]byte(payload), &req); err != nil {
		t.Fatal(err)
	}
	const jwtUserID uint = 77
	if err := repo.SaveCommonMedicalRecord(&f.record, req, jwtUserID); err != nil {
		t.Fatal(err)
	}
	var profile PatientMedicalProfile
	var allergy Allergy
	var vital VitalSign
	var document MedicalDocument
	db.Where("medical_record_id = ?", f.record.ID).First(&profile)
	db.Where("medical_record_id = ? AND allergen_name = ?", f.record.ID, "JWT allergy").First(&allergy)
	db.Where("medical_record_id = ? AND comment = ?", f.record.ID, "JWT vital").First(&vital)
	db.Where("medical_record_id = ? AND label = ?", f.record.ID, "JWT document").First(&document)
	if profile.UpdatedBy != jwtUserID || allergy.CreatedBy != jwtUserID || vital.MeasuredBy != jwtUserID || document.UploadedBy != jwtUserID {
		t.Fatalf("auteurs non JWT: profile=%d allergy=%d vital=%d document=%d", profile.UpdatedBy, allergy.CreatedBy, vital.MeasuredBy, document.UploadedBy)
	}
}
