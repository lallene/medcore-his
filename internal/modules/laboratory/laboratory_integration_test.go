package laboratory

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

// testAccess builds trusted Access for workflow tests (organizational bypass via "*").
func testAccess(userID uint) Access {
	return Access{UserID: userID, Permissions: map[string]bool{"*": true}}
}

func staffAccess(userID uint) Access {
	return Access{UserID: userID, Permissions: map[string]bool{}}
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
	list, e := s.List(ListFilter{Page: 1, Limit: 20}, testAccess(99))
	if e != nil || len(list.Data) != 1 {
		t.Fatalf("materialisation: %#v %v", list, e)
	}
	return s, list.Data[0].ID
}

func TestLaboratoryWorkflowJWTFlagsImmutabilityAndTimeline(t *testing.T) {
	db := laboratoryDB(t)
	s, id := seedOrder(t, db)
	if _, e := s.PrepareSample(id, testAccess(80)); e != nil {
		t.Fatal(e)
	}
	beforeCollection := time.Now()
	o, e := s.Collect(id, testAccess(81), CollectRequest{SampleType: "Sang"})
	if e != nil || o.Sample == nil || o.Sample.CollectedBy != 81 || o.Status != StatusSampleCollected || o.Sample.SampleIdentifier != fmt.Sprintf("SMP-%06d", id) || o.Sample.CollectedAt.Before(beforeCollection) || o.Sample.CollectedAt.After(time.Now()) {
		t.Fatalf("collecte: %#v %v", o, e)
	}
	duplicate := Sample{OrderID: id + 100000, SampleIdentifier: o.Sample.SampleIdentifier, SampleType: "Urine", Status: "COLLECTED", CollectedBy: 82, CollectedAt: time.Now()}
	if e := db.Create(&duplicate).Error; e == nil {
		t.Fatal("identifiant de prélèvement dupliqué accepté")
	}
	if _, e = s.Collect(id, testAccess(82), CollectRequest{SampleType: "Sang"}); e == nil {
		t.Fatal("double prélèvement accepté")
	}
	if _, e = s.Start(id, testAccess(82)); e != nil {
		t.Fatal(e)
	}
	low := 10.0
	high := 15.0
	critical := 3.0
	o, e = s.EnterResults(id, testAccess(83), EnterResultsRequest{Results: []ResultInput{{Parameter: "Hb", Value: "8", Unit: "g/dL", ReferenceMin: &low, ReferenceMax: &high, CriticalMin: &critical}, {Parameter: "K", Value: "2", Unit: "mmol/L", CriticalMin: &critical}}})
	if e != nil || len(o.Results) != 2 || o.Results[0].Flag != "LOW" || o.Results[0].EnteredBy != 83 {
		t.Fatalf("résultat: %#v %v", o, e)
	}
	var alert medical_records.MedicalAlert
	if e := db.Where("type=? AND created_by=?", "critical_result", 83).First(&alert).Error; e != nil || alert.Severity != "critical" {
		t.Fatalf("alerte critique: %#v %v", alert, e)
	}
	o, e = s.Validate(id, testAccess(84))
	if e != nil || o.Status != StatusValidated || o.ValidatedBy == nil || *o.ValidatedBy != 84 {
		t.Fatalf("validation: %#v %v", o, e)
	}
	if _, e = s.EnterResults(id, testAccess(999), EnterResultsRequest{Results: []ResultInput{{Parameter: "Hb", Value: "9"}}}); e == nil {
		t.Fatal("édition après validation acceptée")
	}
	if _, e = s.Validate(id, testAccess(999)); e == nil {
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
	result, err := NewService(NewRepository(db)).List(ListFilter{Page: 1, Limit: 20}, testAccess(99))
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
func TestPostgresLabExecutingServiceIsolationF2408(t *testing.T) {
	db := laboratoryDB(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	seedOrgAndStaffIsolationTables(t, db)

	if err := db.Exec(`INSERT INTO organization_services(id, department_id, name, code, service_type, active, created_by, updated_by) VALUES
		(14, 1, 'Laboratoire', 'LAB', 'DIAGNOSTIC', true, 1, 1),
		(2, 1, 'Médecine générale', 'GEN', 'CLINICAL', true, 1, 1)`).Error; err != nil {
		t.Fatal(err)
	}
	const userA, userB uint = 501, 502
	if err := db.Exec(`INSERT INTO staff_profiles(id, user_id, active, primary_service_id, employee_code) VALUES
		(1, ?, true, 14, 'LAB-A'),
		(2, ?, true, 2, 'GEN-B')`, userA, userB).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO staff_service_assignments(profile_id, service_id, active, created_by) VALUES
		(1, 14, true, 1),
		(2, 2, true, 1)`).Error; err != nil {
		t.Fatal(err)
	}

	p := patients.Patient{CodePatient: "F2408-LAB-P", NumeroDossier: "F2408-LAB-D", Nom: "Isolation"}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	mr := medical_records.MedicalRecord{PatientID: p.ID, RecordNumber: "F2408-LAB-MR"}
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
	exam := consultations.MedicalExam{Code: "NFS-F2408", Name: "NFS", Category: "Laboratoire", IsActive: true}
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
		t.Fatalf("materialize LAB order: %#v %v", listA, listErr)
	}
	orderID := listA.Data[0].ID

	order, getAErr := svc.Get(orderID, accessA)
	if getAErr != nil {
		t.Fatalf("user A (LAB assignment) Get: %v", getAErr)
	}
	if order.ExecutingServiceID == nil || *order.ExecutingServiceID != 14 {
		t.Fatalf("ExecutingServiceID want LAB=14 got %v", order.ExecutingServiceID)
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
		t.Fatalf("List must not expose LAB order to GEN-only user: %#v", listB.Data)
	}
	genIDFilter := uint(2)
	broaden, broadenErr := svc.List(ListFilter{Page: 1, Limit: 20, ServiceID: &genIDFilter}, accessB)
	if broadenErr != nil {
		t.Fatal(broadenErr)
	}
	if len(broaden.Data) != 0 {
		t.Fatalf("client serviceId must not broaden executing scope: %#v", broaden.Data)
	}
	labIDFilter := uint(14)
	narrowA, narrowErr := svc.List(ListFilter{Page: 1, Limit: 20, ServiceID: &labIDFilter}, accessA)
	if narrowErr != nil || len(narrowA.Data) != 1 {
		t.Fatalf("same-service List with executing serviceId: %#v %v", narrowA, narrowErr)
	}

	cancelled, cancelAErr := svc.Cancel(orderID, accessA, "same-service cancel")
	if cancelAErr != nil || cancelled.Status != StatusCancelled {
		t.Fatalf("same-service Cancel: %#v %v", cancelled, cancelAErr)
	}
}

// LOT 25E-3 AUTH-01c: A enters, B corrects, EnteredBy stays A; timeline + order.UpdatedBy attribute B; C validates.
func TestLaboratoryResultCorrectionPreservesEnteredBy(t *testing.T) {
	db := laboratoryDB(t)
	s, id := seedOrder(t, db)
	const entererA, correctorB, validatorC uint = 83, 88, 84
	if _, e := s.PrepareSample(id, testAccess(80)); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Collect(id, testAccess(81), CollectRequest{SampleType: "Sang"}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Start(id, testAccess(82)); e != nil {
		t.Fatal(e)
	}
	o, e := s.EnterResults(id, testAccess(entererA), EnterResultsRequest{Results: []ResultInput{{Parameter: "Hb", Value: "8", Unit: "g/dL"}}})
	if e != nil || len(o.Results) != 1 || o.Results[0].EnteredBy != entererA {
		t.Fatalf("saisie initiale: %#v %v", o, e)
	}
	resultID := o.Results[0].ID
	o, e = s.EnterResults(id, testAccess(correctorB), EnterResultsRequest{Results: []ResultInput{{Parameter: "Hb", Value: "9.5", Unit: "g/dL"}}})
	if e != nil {
		t.Fatal(e)
	}
	var hb Result
	for _, r := range o.Results {
		if r.Parameter == "Hb" {
			hb = r
		}
	}
	if hb.ID != resultID {
		t.Fatalf("identité résultat altérée: got %d want %d", hb.ID, resultID)
	}
	if hb.Value != "9.5" {
		t.Fatalf("valeur non corrigée: %q", hb.Value)
	}
	if hb.EnteredBy != entererA {
		t.Fatalf("EnteredBy overwritten: got %d want %d", hb.EnteredBy, entererA)
	}
	if o.UpdatedBy != correctorB {
		t.Fatalf("order UpdatedBy=%d want %d", o.UpdatedBy, correctorB)
	}
	var correction medical_records.MedicalTimelineEvent
	if e := db.Where("event_type=? AND reference_id=? AND created_by=?", "lab_result_entered", id, correctorB).
		Order("id DESC").First(&correction).Error; e != nil {
		t.Fatalf("timeline correcteur absente: %v", e)
	}
	o, e = s.Validate(id, testAccess(validatorC))
	if e != nil || o.Status != StatusValidated || o.ValidatedBy == nil || *o.ValidatedBy != validatorC {
		t.Fatalf("validation: %#v %v", o, e)
	}
	for _, r := range o.Results {
		if r.Parameter == "Hb" && (r.EnteredBy != entererA || r.Value != "9.5") {
			t.Fatalf("après validation: %#v", r)
		}
	}
	if _, e = s.EnterResults(id, testAccess(999), EnterResultsRequest{Results: []ResultInput{{Parameter: "Hb", Value: "10"}}}); e == nil {
		t.Fatal("édition après validation acceptée")
	}
}
