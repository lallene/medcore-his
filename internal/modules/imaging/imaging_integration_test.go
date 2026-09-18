package imaging

import (
	"errors"
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

func testAccess(userID uint) Access {
	return Access{UserID: userID, Permissions: map[string]bool{"*": true}}
}

func staffAccess(userID uint) Access {
	return Access{UserID: userID, Permissions: map[string]bool{}}
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
	list, e := s.List(ListFilter{Page: 1, Limit: 20}, testAccess(99))
	if e != nil || len(list.Data) != 1 {
		t.Fatalf("matérialisation: %#v %v", list, e)
	}
	return s, list.Data[0].ID
}

func TestImagingWorkflowJWTImmutabilityTimelineAndConcurrencyGuards(t *testing.T) {
	db := imagingDB(t)
	s, id := seedImagingOrder(t, db)
	scheduled := time.Now().Add(2 * time.Hour)
	o, e := s.Schedule(id, testAccess(81), ScheduleRequest{ScheduledAt: scheduled, Comment: "LOT9"})
	if e != nil || o.Status != StatusScheduled || o.ScheduledBy == nil || *o.ScheduledBy != 81 {
		t.Fatalf("schedule: %#v %v", o, e)
	}
	if _, e = s.Schedule(id, testAccess(999), ScheduleRequest{ScheduledAt: scheduled}); e == nil {
		t.Fatal("double planification acceptée")
	}
	o, e = s.Start(id, testAccess(82), StartRequest{TechnicalNotes: "réalisé", ContrastUsed: true, ContrastProduct: "iode"})
	if e != nil || o.Status != StatusInProgress || o.PerformedBy == nil || *o.PerformedBy != 82 {
		t.Fatalf("start: %#v %v", o, e)
	}
	o, e = s.SaveReport(id, testAccess(83), ReportRequest{ClinicalIndication: "toux", Technique: "face", Findings: "opacité", Conclusion: "pneumopathie", Recommendation: "contrôle"})
	if e != nil || o.Status != StatusReportDrafted || o.Report == nil || o.Report.DraftedBy != 83 {
		t.Fatalf("report: %#v %v", o, e)
	}
	o, e = s.Validate(id, testAccess(84))
	if e != nil || o.Status != StatusValidated || o.Report.ValidatedBy == nil || *o.Report.ValidatedBy != 84 {
		t.Fatalf("validate: %#v %v", o, e)
	}
	if _, e = s.SaveReport(id, testAccess(999), ReportRequest{Findings: "fraude", Conclusion: "fraude"}); e == nil {
		t.Fatal("édition après validation acceptée")
	}
	if _, e = s.Validate(id, testAccess(999)); e == nil {
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
	first, e := s.List(ListFilter{Page: 1, Limit: 20}, testAccess(99))
	if e != nil || len(first.Data) != 1 || first.Data[0].Category != "Imagerie" {
		t.Fatalf("file: %#v %v", first, e)
	}
	second, e := s.List(ListFilter{Page: 1, Limit: 20}, testAccess(99))
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
	o, e := s.Cancel(id, testAccess(91), "erreur prescription")
	if e != nil || o.Status != StatusCancelled {
		t.Fatalf("cancel: %#v %v", o, e)
	}
	if _, e = s.Start(id, testAccess(92), StartRequest{}); e == nil {
		t.Fatal("démarrage annulé accepté")
	}
}

// seedOrgAndStaffIsolationTables creates the authority model tables used by F24-08.
func seedOrgAndStaffIsolationTables(t *testing.T, db *gorm.DB) {
	t.Helper()
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS organization_departments (
			id BIGSERIAL PRIMARY KEY, code TEXT, name TEXT, active BOOLEAN NOT NULL DEFAULT true,
			created_by BIGINT NOT NULL DEFAULT 1, updated_by BIGINT NOT NULL DEFAULT 1
		)`,
		`CREATE TABLE IF NOT EXISTS organization_services (
			id BIGSERIAL PRIMARY KEY, department_id BIGINT NOT NULL DEFAULT 1,
			name TEXT, code TEXT, service_type TEXT NOT NULL DEFAULT 'DIAGNOSTIC',
			active BOOLEAN NOT NULL DEFAULT true, clinical BOOLEAN NOT NULL DEFAULT false,
			supports_hospitalization BOOLEAN NOT NULL DEFAULT false,
			supports_consultation BOOLEAN NOT NULL DEFAULT false,
			supports_beds BOOLEAN NOT NULL DEFAULT false,
			sort_order INT NOT NULL DEFAULT 0,
			created_by BIGINT NOT NULL DEFAULT 1, updated_by BIGINT NOT NULL DEFAULT 1
		)`,
		`CREATE TABLE IF NOT EXISTS staff_profiles (
			id BIGSERIAL PRIMARY KEY, user_id BIGINT NOT NULL UNIQUE, active BOOLEAN NOT NULL DEFAULT true,
			primary_service_id BIGINT, employee_code TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS staff_service_assignments (
			id BIGSERIAL PRIMARY KEY, profile_id BIGINT NOT NULL, service_id BIGINT NOT NULL,
			active BOOLEAN NOT NULL DEFAULT true, is_primary BOOLEAN NOT NULL DEFAULT false,
			created_by BIGINT NOT NULL DEFAULT 1
		)`,
	}
	for _, sql := range stmts {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Exec(`INSERT INTO organization_departments(id, code, name, active, created_by, updated_by) VALUES
		(1, 'DIAG', 'Diagnostic', true, 1, 1)
		ON CONFLICT DO NOTHING`).Error; err != nil {
		t.Fatal(err)
	}
}

// F24-08: ExecutingServiceID is the organizational scope; cross-service access is not-found.
func TestPostgresImagingExecutingServiceIsolationF2408(t *testing.T) {
	db := imagingDB(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	seedOrgAndStaffIsolationTables(t, db)

	if err := db.Exec(`INSERT INTO organization_services(id, department_id, name, code, service_type, active, created_by, updated_by) VALUES
		(15, 1, 'Radiologie', 'RAD', 'DIAGNOSTIC', true, 1, 1),
		(2, 1, 'Médecine générale', 'GEN', 'CLINICAL', true, 1, 1)`).Error; err != nil {
		t.Fatal(err)
	}
	const userA, userB uint = 601, 602
	if err := db.Exec(`INSERT INTO staff_profiles(id, user_id, active, primary_service_id, employee_code) VALUES
		(1, ?, true, 15, 'RAD-A'),
		(2, ?, true, 2, 'GEN-B')`, userA, userB).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO staff_service_assignments(profile_id, service_id, active, created_by) VALUES
		(1, 15, true, 1),
		(2, 2, true, 1)`).Error; err != nil {
		t.Fatal(err)
	}

	p := patients.Patient{CodePatient: "F2408-RAD-P", NumeroDossier: "F2408-RAD-D", Nom: "Isolation"}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	mr := medical_records.MedicalRecord{PatientID: p.ID, RecordNumber: "F2408-RAD-MR"}
	if err := db.Create(&mr).Error; err != nil {
		t.Fatal(err)
	}
	genID := uint(2)
	c := consultations.Consultation{
		PatientID: p.ID, DoctorName: "Dr Requesting", Service: "Médecine générale",
		ServiceID: &genID, Status: "draft",
	}
	if err := db.Create(&c).Error; err != nil {
		t.Fatal(err)
	}
	exam := consultations.MedicalExam{Code: "XR-F2408", Name: "Radio thorax", Category: "Imagerie", IsActive: true}
	if err := db.Create(&exam).Error; err != nil {
		t.Fatal(err)
	}
	req := consultations.ConsultationExamRequest{
		ConsultationID: c.ID, MedicalExamID: exam.ID, Status: "requested", Priority: "ROUTINE", PrescribedBy: 77,
	}
	if err := db.Create(&req).Error; err != nil {
		t.Fatal(err)
	}

	svc := NewService(NewRepository(db))
	accessA, accessB := staffAccess(userA), staffAccess(userB)

	listA, listErr := svc.List(ListFilter{Page: 1, Limit: 20}, accessA)
	if listErr != nil || len(listA.Data) != 1 {
		t.Fatalf("materialize RAD order: %#v %v", listA, listErr)
	}
	orderID := listA.Data[0].ID

	order, getAErr := svc.Get(orderID, accessA)
	if getAErr != nil {
		t.Fatalf("user A (RAD assignment) Get: %v", getAErr)
	}
	if order.ExecutingServiceID == nil || *order.ExecutingServiceID != 15 {
		t.Fatalf("ExecutingServiceID want RAD=15 got %v", order.ExecutingServiceID)
	}
	if order.RequestingServiceID == nil || *order.RequestingServiceID != 2 {
		t.Fatalf("RequestingServiceID want GEN=2 got %v (context only, not auth scope)", order.RequestingServiceID)
	}

	if _, getBErr := svc.Get(orderID, accessB); !errors.Is(getBErr, gorm.ErrRecordNotFound) {
		t.Fatalf("cross-service Get want ErrRecordNotFound got %v", getBErr)
	}
	if _, cancelBErr := svc.Cancel(orderID, accessB, "cross-service cancel"); !errors.Is(cancelBErr, gorm.ErrRecordNotFound) {
		t.Fatalf("cross-service Cancel want ErrRecordNotFound got %v", cancelBErr)
	}
	var statusAfter string
	if err := db.Model(&Order{}).Select("status").Where("id=?", orderID).Scan(&statusAfter).Error; err != nil {
		t.Fatal(err)
	}
	if statusAfter != StatusOrdered {
		t.Fatalf("cross-service Cancel must not mutate status: got %s", statusAfter)
	}

	listB, listBErr := svc.List(ListFilter{Page: 1, Limit: 20}, accessB)
	if listBErr != nil {
		t.Fatal(listBErr)
	}
	if len(listB.Data) != 0 {
		t.Fatalf("List must not expose RAD order to GEN-only user: %#v", listB.Data)
	}
	genIDFilter := uint(2)
	broaden, broadenErr := svc.List(ListFilter{Page: 1, Limit: 20, ServiceID: &genIDFilter}, accessB)
	if broadenErr != nil {
		t.Fatal(broadenErr)
	}
	if len(broaden.Data) != 0 {
		t.Fatalf("client serviceId must not broaden executing scope: %#v", broaden.Data)
	}
	radIDFilter := uint(15)
	narrowA, narrowErr := svc.List(ListFilter{Page: 1, Limit: 20, ServiceID: &radIDFilter}, accessA)
	if narrowErr != nil || len(narrowA.Data) != 1 {
		t.Fatalf("same-service List with executing serviceId: %#v %v", narrowA, narrowErr)
	}

	cancelled, cancelAErr := svc.Cancel(orderID, accessA, "same-service cancel")
	if cancelAErr != nil || cancelled.Status != StatusCancelled {
		t.Fatalf("same-service Cancel: %#v %v", cancelled, cancelAErr)
	}
}
