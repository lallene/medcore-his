package laboratory

import (
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/consultations"
	"github.com/lallene/medcore-his/backend/internal/modules/medical_records"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func laboratoryDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL absent: tests PostgreSQL laboratoire ignorés")
	}
	admin, e := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if e != nil {
		t.Fatal(e)
	}
	schemaName := fmt.Sprintf("laboratory_%d", time.Now().UnixNano())
	if e = admin.Exec(`CREATE SCHEMA "` + schemaName + `"`).Error; e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = admin.Exec(`DROP SCHEMA IF EXISTS "` + schemaName + `" CASCADE`).Error })
	u, _ := url.Parse(dsn)
	q := u.Query()
	q.Set("search_path", schemaName)
	u.RawQuery = q.Encode()
	db, e := gorm.Open(postgres.Open(u.String()), &gorm.Config{})
	if e != nil {
		t.Fatal(e)
	}
	if e = db.AutoMigrate(&patients.Patient{}, &consultations.MedicalExam{}, &consultations.Consultation{}, &consultations.ConsultationExamRequest{}, &medical_records.MedicalRecord{}, &medical_records.MedicalTimelineEvent{}, &medical_records.MedicalAlert{}, &Order{}, &Sample{}, &Result{}); e != nil {
		t.Fatal(e)
	}
	return db
}

func seedOrder(t *testing.T, db *gorm.DB) (*Service, uint) {
	t.Helper()
	p := patients.Patient{CodePatient: "LOT8-P", NumeroDossier: "LOT8-D", Nom: "Laboratoire"}
	if e := db.Create(&p).Error; e != nil {
		t.Fatal(e)
	}
	mr := medical_records.MedicalRecord{PatientID: p.ID, RecordNumber: "LOT8-MR"}
	if e := db.Create(&mr).Error; e != nil {
		t.Fatal(e)
	}
	c := consultations.Consultation{PatientID: p.ID, DoctorName: "Dr Test", Service: "Médecine", Status: "draft"}
	if e := db.Create(&c).Error; e != nil {
		t.Fatal(e)
	}
	exam := consultations.MedicalExam{Code: "NFS-T", Name: "NFS", Category: "Laboratoire", IsActive: true}
	if e := db.Create(&exam).Error; e != nil {
		t.Fatal(e)
	}
	request := consultations.ConsultationExamRequest{ConsultationID: c.ID, MedicalExamID: exam.ID, Status: "requested", Priority: "URGENT", PrescribedBy: 77}
	if e := db.Create(&request).Error; e != nil {
		t.Fatal(e)
	}
	s := NewService(NewRepository(db))
	list, e := s.List(ListFilter{Page: 1, Limit: 20}, 99)
	if e != nil || len(list.Data) != 1 {
		t.Fatalf("materialisation: %#v %v", list, e)
	}
	return s, list.Data[0].ID
}

func TestLaboratoryWorkflowJWTFlagsImmutabilityAndTimeline(t *testing.T) {
	db := laboratoryDB(t)
	s, id := seedOrder(t, db)
	if _, e := s.PrepareSample(id, 80); e != nil {
		t.Fatal(e)
	}
	beforeCollection := time.Now()
	o, e := s.Collect(id, 81, CollectRequest{SampleType: "Sang"})
	if e != nil || o.Sample == nil || o.Sample.CollectedBy != 81 || o.Status != StatusSampleCollected || o.Sample.SampleIdentifier != fmt.Sprintf("SMP-%06d", id) || o.Sample.CollectedAt.Before(beforeCollection) || o.Sample.CollectedAt.After(time.Now()) {
		t.Fatalf("collecte: %#v %v", o, e)
	}
	duplicate := Sample{OrderID: id + 100000, SampleIdentifier: o.Sample.SampleIdentifier, SampleType: "Urine", Status: "COLLECTED", CollectedBy: 82, CollectedAt: time.Now()}
	if e := db.Create(&duplicate).Error; e == nil {
		t.Fatal("identifiant de prélèvement dupliqué accepté")
	}
	if _, e = s.Collect(id, 82, CollectRequest{SampleType: "Sang"}); e == nil {
		t.Fatal("double prélèvement accepté")
	}
	if _, e = s.Start(id, 82); e != nil {
		t.Fatal(e)
	}
	low := 10.0
	high := 15.0
	critical := 3.0
	o, e = s.EnterResults(id, 83, EnterResultsRequest{Results: []ResultInput{{Parameter: "Hb", Value: "8", Unit: "g/dL", ReferenceMin: &low, ReferenceMax: &high, CriticalMin: &critical}, {Parameter: "K", Value: "2", Unit: "mmol/L", CriticalMin: &critical}}})
	if e != nil || len(o.Results) != 2 || o.Results[0].Flag != "LOW" || o.Results[0].EnteredBy != 83 {
		t.Fatalf("résultat: %#v %v", o, e)
	}
	var alert medical_records.MedicalAlert
	if e := db.Where("type=? AND created_by=?", "critical_result", 83).First(&alert).Error; e != nil || alert.Severity != "critical" {
		t.Fatalf("alerte critique: %#v %v", alert, e)
	}
	o, e = s.Validate(id, 84)
	if e != nil || o.Status != StatusValidated || o.ValidatedBy == nil || *o.ValidatedBy != 84 {
		t.Fatalf("validation: %#v %v", o, e)
	}
	if _, e = s.EnterResults(id, 999, EnterResultsRequest{Results: []ResultInput{{Parameter: "Hb", Value: "9"}}}); e == nil {
		t.Fatal("édition après validation acceptée")
	}
	if _, e = s.Validate(id, 999); e == nil {
		t.Fatal("double validation acceptée")
	}
	var events []medical_records.MedicalTimelineEvent
	db.Where("reference_type=? AND reference_id=?", "laboratory_order", id).Order("id").Find(&events)
	expected := []string{"lab_order_created", "lab_sample_collected", "lab_analysis_started", "lab_result_entered", "lab_result_validated"}
	if len(events) != len(expected) {
		t.Fatalf("timeline=%#v", events)
	}
	for i, event := range events {
		if event.EventType != expected[i] {
			t.Fatalf("timeline[%d]=%s", i, event.EventType)
		}
	}
}

func TestMaterializeOnlyBiologicalExamCategories(t *testing.T) {
	db := laboratoryDB(t)
	patient := patients.Patient{CodePatient: "LOT8-CAT-P", NumeroDossier: "LOT8-CAT-D", Nom: "Catégories"}
	if err := db.Create(&patient).Error; err != nil {
		t.Fatal(err)
	}
	record := medical_records.MedicalRecord{PatientID: patient.ID, RecordNumber: "LOT8-CAT-MR"}
	if err := db.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	consultation := consultations.Consultation{PatientID: patient.ID, DoctorName: "Dr Catégorie", Service: "Médecine", Status: "draft"}
	if err := db.Create(&consultation).Error; err != nil {
		t.Fatal(err)
	}
	exams := []consultations.MedicalExam{
		{Code: "CRP-CAT", Name: "CRP", Category: "Laboratoire", IsActive: true},
		{Code: "NFS-CAT", Name: "NFS", Category: "Biologie", IsActive: true},
		{Code: "XR-CAT", Name: "Radiographie", Category: "Imagerie", IsActive: true},
		{Code: "ECG-CAT", Name: "ECG", Category: "Cardiologie", IsActive: true},
	}
	if err := db.Create(&exams).Error; err != nil {
		t.Fatal(err)
	}
	for _, exam := range exams {
		request := consultations.ConsultationExamRequest{ConsultationID: consultation.ID, MedicalExamID: exam.ID, Status: "requested", Priority: "ROUTINE", PrescribedBy: 73}
		if err := db.Create(&request).Error; err != nil {
			t.Fatal(err)
		}
	}
	result, err := NewService(NewRepository(db)).List(ListFilter{Page: 1, Limit: 20}, 99)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Data) != 2 || result.Data[0].Category == "Imagerie" || result.Data[1].Category == "Cardiologie" {
		t.Fatalf("file laboratoire incorrecte: %#v", result.Data)
	}
	var orderCount, prescriptionCount int64
	db.Model(&Order{}).Count(&orderCount)
	db.Model(&consultations.ConsultationExamRequest{}).Where("consultation_id=?", consultation.ID).Count(&prescriptionCount)
	if orderCount != 2 || prescriptionCount != 4 {
		t.Fatalf("orders=%d prescriptions=%d", orderCount, prescriptionCount)
	}
}

func TestComputeFlagCriticalAndText(t *testing.T) {
	min, max, cmin := 10.0, 20.0, 5.0
	cases := []struct{ value, want string }{{"4", "CRITICAL"}, {"8", "LOW"}, {"15", "NORMAL"}, {"22", "HIGH"}, {"positif", "NORMAL"}}
	for _, tc := range cases {
		got, _ := computeFlag(ResultInput{Value: tc.value, ReferenceMin: &min, ReferenceMax: &max, CriticalMin: &cmin})
		if got != tc.want {
			t.Errorf("%s: %s", tc.value, got)
		}
	}
}
