package imaging

import (
	"fmt"
	"github.com/lallene/medcore-his/backend/internal/modules/consultations"
	"github.com/lallene/medcore-his/backend/internal/modules/medical_records"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"net/url"
	"os"
	"testing"
	"time"
)

func imagingDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL absent: tests PostgreSQL imagerie ignorés")
	}
	admin, e := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if e != nil {
		t.Fatal(e)
	}
	schema := fmt.Sprintf("imaging_%d", time.Now().UnixNano())
	if e = admin.Exec(`CREATE SCHEMA "` + schema + `"`).Error; e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = admin.Exec(`DROP SCHEMA IF EXISTS "` + schema + `" CASCADE`).Error })
	u, _ := url.Parse(dsn)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, e := gorm.Open(postgres.Open(u.String()), &gorm.Config{})
	if e != nil {
		t.Fatal(e)
	}
	if e = db.AutoMigrate(&patients.Patient{}, &consultations.MedicalExam{}, &consultations.Consultation{}, &consultations.ConsultationExamRequest{}, &medical_records.MedicalRecord{}, &medical_records.MedicalTimelineEvent{}, &Order{}, &Report{}); e != nil {
		t.Fatal(e)
	}
	return db
}

func seedImagingOrder(t *testing.T, db *gorm.DB) (*Service, uint) {
	t.Helper()
	p := patients.Patient{CodePatient: "LOT9-P", NumeroDossier: "LOT9-D", Nom: "Imagerie"}
	if e := db.Create(&p).Error; e != nil {
		t.Fatal(e)
	}
	mr := medical_records.MedicalRecord{PatientID: p.ID, RecordNumber: "LOT9-MR"}
	if e := db.Create(&mr).Error; e != nil {
		t.Fatal(e)
	}
	c := consultations.Consultation{PatientID: p.ID, DoctorName: "Dr Image", Service: "Radiologie", Status: "draft"}
	if e := db.Create(&c).Error; e != nil {
		t.Fatal(e)
	}
	exam := consultations.MedicalExam{Code: "CHEST_XRAY", Name: "Radiographie thoracique", Category: "Imagerie", IsActive: true}
	if e := db.Create(&exam).Error; e != nil {
		t.Fatal(e)
	}
	req := consultations.ConsultationExamRequest{ConsultationID: c.ID, MedicalExamID: exam.ID, Status: "requested", Priority: "URGENT", PrescribedBy: 77}
	if e := db.Create(&req).Error; e != nil {
		t.Fatal(e)
	}
	s := NewService(NewRepository(db))
	list, e := s.List(ListFilter{Page: 1, Limit: 20}, 99)
	if e != nil || len(list.Data) != 1 {
		t.Fatalf("matérialisation: %#v %v", list, e)
	}
	return s, list.Data[0].ID
}

func TestImagingWorkflowJWTImmutabilityTimelineAndConcurrencyGuards(t *testing.T) {
	db := imagingDB(t)
	s, id := seedImagingOrder(t, db)
	scheduled := time.Now().Add(2 * time.Hour)
	o, e := s.Schedule(id, 81, ScheduleRequest{ScheduledAt: scheduled, Comment: "LOT9"})
	if e != nil || o.Status != StatusScheduled || o.ScheduledBy == nil || *o.ScheduledBy != 81 {
		t.Fatalf("schedule: %#v %v", o, e)
	}
	if _, e = s.Schedule(id, 999, ScheduleRequest{ScheduledAt: scheduled}); e == nil {
		t.Fatal("double planification acceptée")
	}
	o, e = s.Start(id, 82, StartRequest{TechnicalNotes: "réalisé", ContrastUsed: true, ContrastProduct: "iode"})
	if e != nil || o.Status != StatusInProgress || o.PerformedBy == nil || *o.PerformedBy != 82 {
		t.Fatalf("start: %#v %v", o, e)
	}
	o, e = s.SaveReport(id, 83, ReportRequest{ClinicalIndication: "toux", Technique: "face", Findings: "opacité", Conclusion: "pneumopathie", Recommendation: "contrôle"})
	if e != nil || o.Status != StatusReportDrafted || o.Report == nil || o.Report.DraftedBy != 83 {
		t.Fatalf("report: %#v %v", o, e)
	}
	o, e = s.Validate(id, 84)
	if e != nil || o.Status != StatusValidated || o.Report.ValidatedBy == nil || *o.Report.ValidatedBy != 84 {
		t.Fatalf("validate: %#v %v", o, e)
	}
	if _, e = s.SaveReport(id, 999, ReportRequest{Findings: "fraude", Conclusion: "fraude"}); e == nil {
		t.Fatal("édition après validation acceptée")
	}
	if _, e = s.Validate(id, 999); e == nil {
		t.Fatal("double validation acceptée")
	}
	var events []medical_records.MedicalTimelineEvent
	db.Where("reference_type=? AND reference_id=?", "imaging_order", id).Order("id").Find(&events)
	want := []string{"imaging_order_created", "imaging_scheduled", "imaging_started", "imaging_report_drafted", "imaging_report_validated"}
	if len(events) != len(want) {
		t.Fatalf("timeline=%#v", events)
	}
	for i, event := range events {
		if event.EventType != want[i] || event.CreatedBy == 0 || event.Category != "imaging" {
			t.Fatalf("event[%d]=%#v", i, event)
		}
	}
}

func TestMaterializeOnlyImagingAndIdempotentlyPreservesPrescriptions(t *testing.T) {
	db := imagingDB(t)
	p := patients.Patient{CodePatient: "LOT9-CAT-P", NumeroDossier: "LOT9-CAT-D", Nom: "Catégories"}
	db.Create(&p)
	mr := medical_records.MedicalRecord{PatientID: p.ID, RecordNumber: "LOT9-CAT-MR"}
	db.Create(&mr)
	c := consultations.Consultation{PatientID: p.ID, DoctorName: "Dr Cat", Service: "Imagerie", Status: "draft"}
	db.Create(&c)
	exams := []consultations.MedicalExam{{Code: "XR", Name: "Radio", Category: "Imagerie", IsActive: true}, {Code: "CRP", Name: "CRP", Category: "Laboratoire", IsActive: true}, {Code: "ECG", Name: "ECG", Category: "Cardiologie", IsActive: true}, {Code: "AUDIO", Name: "Audio", Category: "ORL", IsActive: true}}
	db.Create(&exams)
	for _, exam := range exams {
		db.Create(&consultations.ConsultationExamRequest{ConsultationID: c.ID, MedicalExamID: exam.ID, Status: "requested", Priority: "ROUTINE", PrescribedBy: 73})
	}
	s := NewService(NewRepository(db))
	first, e := s.List(ListFilter{Page: 1, Limit: 20}, 99)
	if e != nil || len(first.Data) != 1 || first.Data[0].Category != "Imagerie" {
		t.Fatalf("file: %#v %v", first, e)
	}
	second, e := s.List(ListFilter{Page: 1, Limit: 20}, 99)
	if e != nil || len(second.Data) != 1 {
		t.Fatalf("idempotence: %#v %v", second, e)
	}
	var orders, prescriptions int64
	db.Model(&Order{}).Count(&orders)
	db.Model(&consultations.ConsultationExamRequest{}).Count(&prescriptions)
	if orders != 1 || prescriptions != 4 {
		t.Fatalf("orders=%d prescriptions=%d", orders, prescriptions)
	}
}

func TestCancellationAndInvalidTransitions(t *testing.T) {
	db := imagingDB(t)
	s, id := seedImagingOrder(t, db)
	o, e := s.Cancel(id, 91, "erreur prescription")
	if e != nil || o.Status != StatusCancelled {
		t.Fatalf("cancel: %#v %v", o, e)
	}
	if _, e = s.Start(id, 92, StartRequest{}); e == nil {
		t.Fatal("démarrage annulé accepté")
	}
}
