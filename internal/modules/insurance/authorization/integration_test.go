package authorization

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/company"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/coverage"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/guarantor"
	"github.com/lallene/medcore-his/backend/internal/modules/medical_records"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type authorizationConsultation struct {
	ID, PatientID               uint
	Service, DoctorName, Status string
	CreatedAt                   time.Time
}

func (authorizationConsultation) TableName() string { return "consultations" }

type authorizationMedicalExam struct {
	ID             uint
	Name, Category string
}

func (authorizationMedicalExam) TableName() string { return "medical_exams" }

type authorizationLaboratoryOrder struct {
	ID, PatientID, MedicalExamID uint
	RequestNumber, Status        string
	CreatedAt                    time.Time
}

func (authorizationLaboratoryOrder) TableName() string { return "laboratory_orders" }

type authorizationImagingOrder struct {
	ID, PatientID, MedicalExamID  uint
	OrderNumber, Modality, Status string
	CreatedAt                     time.Time
}

func (authorizationImagingOrder) TableName() string { return "imaging_orders" }

type authorizationHospitalization struct {
	ID, PatientID                                        uint
	AdmissionNumber, Department, AdmissionReason, Status string
	CreatedAt                                            time.Time
}

func (authorizationHospitalization) TableName() string { return "hospitalizations" }

type authorizationPrescription struct {
	ID, ConsultationID     uint
	MedicationName, Dosage string
	CreatedAt              time.Time
}

func (authorizationPrescription) TableName() string { return "consultation_prescriptions" }

type authorizationPharmacyRow struct{ ID uint }

func authorizationDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL absent: tests PostgreSQL PEC ignorés")
	}
	admin, e := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if e != nil {
		t.Fatal(e)
	}
	schema := fmt.Sprintf("authorization_%d", time.Now().UnixNano())
	if e = admin.Exec(`CREATE SCHEMA "` + schema + `"`).Error; e != nil {
		t.Fatal(e)
	}
	db, e := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if e != nil {
		t.Fatal(e)
	}
	sqlDB, e := db.DB()
	if e != nil {
		t.Fatal(e)
	}
	sqlDB.SetMaxOpenConns(1)
	if e = db.Exec(`SET search_path TO "` + schema + `"`).Error; e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		_ = db.Exec("RESET search_path").Error
		_ = sqlDB.Close()
		_ = admin.Exec(`DROP SCHEMA IF EXISTS "` + schema + `" CASCADE`).Error
	})
	if e = db.AutoMigrate(&patients.Patient{}, &company.InsuranceCompany{}, &guarantor.InsuranceGuarantor{}, &coverage.PatientCoverage{}, &medical_records.MedicalRecord{}, &medical_records.MedicalTimelineEvent{}, &authorizationConsultation{}, &authorizationMedicalExam{}, &authorizationLaboratoryOrder{}, &authorizationImagingOrder{}, &authorizationHospitalization{}, &authorizationPrescription{}, &InsuranceAuthorization{}, &InsuranceAuthorizationAct{}); e != nil {
		t.Fatal(e)
	}
	return db
}

func TestEligibleActsCoversSupportedClinicalSources(t *testing.T) {
	db := authorizationDB(t)
	f := seedAuthorization(t, db)
	exam := authorizationMedicalExam{Name: "Radiographie thoracique", Category: "Imagerie"}
	db.Create(&exam)
	lab := authorizationLaboratoryOrder{PatientID: f.patient.ID, MedicalExamID: exam.ID, RequestNumber: "LAB-1", Status: "VALIDATED"}
	db.Create(&lab)
	img := authorizationImagingOrder{PatientID: f.patient.ID, MedicalExamID: exam.ID, OrderNumber: "IMG-1", Modality: "XR", Status: "VALIDATED"}
	db.Create(&img)
	hosp := authorizationHospitalization{PatientID: f.patient.ID, AdmissionNumber: "HOSP-1", Department: "Urgences", AdmissionReason: "Test", Status: "DISCHARGED"}
	db.Create(&hosp)
	prescription := authorizationPrescription{ConsultationID: f.act.ID, MedicationName: "DOLIPRANE", Dosage: "1 g"}
	db.Create(&prescription)
	s := NewService(db)
	for typ, expected := range map[string]string{"CONSULTATION": "#", "LABORATORY": "LAB-1", "IMAGING": "IMG-1", "HOSPITALIZATION": "HOSP-1", "MEDICATION": "DOLIPRANE"} {
		rows, err := s.EligibleActs(f.patient.ID, f.coverage.ID, typ, expected)
		if err != nil || len(rows) != 1 || rows[0].AuthorizationResolution != "NONE" || !strings.Contains(rows[0].Label, expected) {
			t.Fatalf("%s rows=%#v err=%v", typ, rows, err)
		}
	}
	if _, err := s.EligibleActs(f.other.ID, f.coverage.ID, "MEDICATION", ""); !IsConflict(err) {
		t.Fatalf("foreign coverage=%v", err)
	}
	if _, err := s.EligibleActs(f.patient.ID, f.coverage.ID, "OTHER", ""); err == nil {
		t.Fatal("invalid type accepted")
	}
}

func TestAuthorizationActReuseAndExplicitCoverage(t *testing.T) {
	db := authorizationDB(t)
	for _, table := range []string{"pharmacy_dispensations", "pharmacy_vouchers", "pharmacy_stocks", "pharmacy_batches", "stock_movements"} {
		if err := db.Exec("CREATE TABLE " + table + " (id bigserial primary key)").Error; err != nil {
			t.Fatal(err)
		}
	}
	f := seedAuthorization(t, db)
	s := NewService(db)
	amount := 1000.0
	primary, err := s.Create(CreateRequest{PatientID: f.patient.ID, PatientCoverageID: f.coverage.ID, ReferenceType: "CONSULTATION", ReferenceID: f.act.ID, RequestedAmount: &amount}, 11)
	if err != nil {
		t.Fatal(err)
	}
	direct, err := s.FindAuthorizationForAct(f.patient.ID, f.coverage.ID, "CONSULTATION", f.act.ID)
	if err != nil || direct.MatchType != "DIRECT" || direct.Authorization.ID != primary.ID {
		t.Fatalf("direct=%#v err=%v", direct, err)
	}
	exam := authorizationMedicalExam{Name: "Radiographie thoracique"}
	db.Create(&exam)
	imaging := authorizationImagingOrder{PatientID: f.patient.ID, MedicalExamID: exam.ID}
	db.Create(&imaging)
	none, err := s.FindAuthorizationForAct(f.patient.ID, f.coverage.ID, "IMAGING", imaging.ID)
	if err != nil || none.MatchType != "NONE" {
		t.Fatalf("none=%#v err=%v", none, err)
	}
	linked, err := s.LinkAct(primary.ID, ActRequest{ReferenceType: "IMAGING", ReferenceID: imaging.ID}, 72)
	if err != nil || linked.CreatedBy != 72 || linked.ReferenceLabel != exam.Name {
		t.Fatalf("linked=%#v err=%v", linked, err)
	}
	covered, err := s.FindAuthorizationForAct(f.patient.ID, f.coverage.ID, "IMAGING", imaging.ID)
	if err != nil || covered.MatchType != "COVERED" || covered.Authorization.ID != primary.ID || len(covered.Authorization.CoveredActs) != 1 {
		t.Fatalf("covered=%#v err=%v", covered, err)
	}
	again, err := s.LinkAct(primary.ID, ActRequest{ReferenceType: "IMAGING", ReferenceID: imaging.ID}, 99)
	if err != nil || again.ID != linked.ID {
		t.Fatalf("idempotent=%#v err=%v", again, err)
	}
	foreignImaging := authorizationImagingOrder{PatientID: f.other.ID, MedicalExamID: exam.ID}
	db.Create(&foreignImaging)
	if _, err = s.LinkAct(primary.ID, ActRequest{ReferenceType: "IMAGING", ReferenceID: foreignImaging.ID}, 72); !IsConflict(err) {
		t.Fatalf("foreign act=%v", err)
	}
	var events []medical_records.MedicalTimelineEvent
	db.Where("event_type=? AND reference_id=?", "insurance_authorization_act_linked", primary.ID).Find(&events)
	if len(events) != 1 || events[0].CreatedBy != 72 {
		t.Fatalf("events=%#v", events)
	}
	if _, err = s.Create(CreateRequest{PatientID: f.patient.ID, PatientCoverageID: f.coverage.ID, ReferenceType: "IMAGING", ReferenceID: imaging.ID, RequestedAmount: &amount}, 11); !IsConflict(err) {
		t.Fatalf("covered act duplicate=%v", err)
	}
	if err = db.Model(&InsuranceAuthorization{}).Where("id=?", primary.ID).Update("status", StatusRejected).Error; err != nil {
		t.Fatal(err)
	}
	rejected, err := s.FindAuthorizationForAct(f.patient.ID, f.coverage.ID, "CONSULTATION", f.act.ID)
	if err != nil || rejected.MatchType != "DIRECT" || rejected.Authorization.Status != StatusRejected {
		t.Fatalf("rejected decision not reused: %#v err=%v", rejected, err)
	}
	secondAct := authorizationConsultation{PatientID: f.patient.ID, Service: "Urgences"}
	db.Create(&secondAct)
	secondAuthorization, err := s.Create(CreateRequest{PatientID: f.patient.ID, PatientCoverageID: f.coverage.ID, ReferenceType: "CONSULTATION", ReferenceID: secondAct.ID, RequestedAmount: &amount}, 11)
	if err != nil {
		t.Fatal(err)
	}
	concurrentImaging := authorizationImagingOrder{PatientID: f.patient.ID, MedicalExamID: exam.ID}
	db.Create(&concurrentImaging)
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, authorizationID := range []uint{primary.ID, secondAuthorization.ID} {
		wg.Add(1)
		go func(candidate uint) {
			defer wg.Done()
			_, linkErr := s.LinkAct(candidate, ActRequest{ReferenceType: "IMAGING", ReferenceID: concurrentImaging.ID}, 72)
			results <- linkErr
		}(authorizationID)
	}
	wg.Wait()
	close(results)
	successes := 0
	conflicts := 0
	for linkErr := range results {
		if linkErr == nil {
			successes++
		} else if IsConflict(linkErr) {
			conflicts++
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent links: successes=%d conflicts=%d", successes, conflicts)
	}
	for _, table := range []string{"pharmacy_dispensations", "pharmacy_vouchers", "pharmacy_stocks", "pharmacy_batches", "stock_movements"} {
		var count int64
		if err := db.Table(table).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("unexpected pharmacy mutation %s: count=%d err=%v", table, count, err)
		}
	}
}

func TestAuthorizationUsesClinicalExamLabels(t *testing.T) {
	db := authorizationDB(t)
	f := seedAuthorization(t, db)
	examLab := authorizationMedicalExam{Name: "Numération formule sanguine"}
	examImaging := authorizationMedicalExam{Name: "Radiographie thoracique"}
	db.Create(&examLab)
	db.Create(&examImaging)
	lab := authorizationLaboratoryOrder{PatientID: f.patient.ID, MedicalExamID: examLab.ID}
	imaging := authorizationImagingOrder{PatientID: f.patient.ID, MedicalExamID: examImaging.ID}
	db.Create(&lab)
	db.Create(&imaging)
	s := NewService(db)
	amount := 1000.0
	labAuthorization, err := s.Create(CreateRequest{PatientID: f.patient.ID, PatientCoverageID: f.coverage.ID, ReferenceType: "LABORATORY", ReferenceID: lab.ID, RequestedAmount: &amount}, 1)
	if err != nil {
		t.Fatal(err)
	}
	imagingAuthorization, err := s.Create(CreateRequest{PatientID: f.patient.ID, PatientCoverageID: f.coverage.ID, ReferenceType: "IMAGING", ReferenceID: imaging.ID, RequestedAmount: &amount}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if labAuthorization.ReferenceLabel != examLab.Name || imagingAuthorization.ReferenceLabel != examImaging.Name {
		t.Fatalf("labels: laboratory=%q imaging=%q", labAuthorization.ReferenceLabel, imagingAuthorization.ReferenceLabel)
	}
}

type fixture struct {
	patient, other patients.Patient
	coverage       coverage.PatientCoverage
	act            authorizationConsultation
}

func seedAuthorization(t *testing.T, db *gorm.DB) fixture {
	t.Helper()
	p := patients.Patient{CodePatient: "PEC-P", NumeroDossier: "PEC-D", Nom: "Patient", Prenoms: "Assuré"}
	other := patients.Patient{CodePatient: "PEC-O", NumeroDossier: "PEC-OD", Nom: "Autre"}
	db.Create(&p)
	db.Create(&other)
	db.Create(&medical_records.MedicalRecord{PatientID: p.ID, RecordNumber: "PEC-MR"})
	db.Create(&medical_records.MedicalRecord{PatientID: other.ID, RecordNumber: "PEC-OMR"})
	co := company.InsuranceCompany{Code: "PEC-C", Name: "Assureur PEC", IsActive: true}
	db.Create(&co)
	g := guarantor.InsuranceGuarantor{CompanyID: co.ID, Code: "PEC-G", Name: "Garant PEC", IsActive: true}
	db.Create(&g)
	cov := coverage.PatientCoverage{PatientID: p.ID, CompanyID: co.ID, GuarantorID: g.ID, MemberNumber: "PEC-001", CoverageRate: 80, IsPrincipal: true, IsActive: true}
	db.Create(&cov)
	act := authorizationConsultation{PatientID: p.ID, Service: "Médecine"}
	db.Create(&act)
	return fixture{p, other, cov, act}
}

func TestAuthorizationWorkflowCoverageSeparationJWTAndTimeline(t *testing.T) {
	db := authorizationDB(t)
	f := seedAuthorization(t, db)
	s := NewService(db)
	amount := 50000.0
	created, e := s.Create(CreateRequest{PatientID: f.patient.ID, PatientCoverageID: f.coverage.ID, ReferenceType: "CONSULTATION", ReferenceID: f.act.ID, Service: "Médecine", RequestedAmount: &amount}, 41)
	if e != nil {
		t.Fatal(e)
	}
	if created.AuthorizationNumber != fmt.Sprintf("PEC-%06d", created.ID) || created.ContractRate != 80 || created.ApprovedRate != nil || created.CreatedBy != 41 {
		t.Fatalf("created=%#v", created)
	}
	if _, e = s.Create(CreateRequest{PatientID: f.patient.ID, PatientCoverageID: f.coverage.ID, ReferenceType: "CONSULTATION", ReferenceID: f.act.ID, RequestedAmount: &amount}, 41); !IsConflict(e) {
		t.Fatalf("duplicate=%v", e)
	}
	submitted, e := s.Submit(created.ID, SubmitRequest{}, 42)
	if e != nil || submitted.SubmittedBy == nil || *submitted.SubmittedBy != 42 {
		t.Fatalf("submitted=%#v %v", submitted, e)
	}
	pending, e := s.MarkPending(created.ID, 42)
	if e != nil || pending.Status != StatusPending {
		t.Fatalf("pending=%#v %v", pending, e)
	}
	rate := 70.0
	decided, e := s.Decide(created.ID, DecisionRequest{Status: StatusApproved, ExternalReference: "DEMO-ASSUR-000001", ExternalDecisionDate: "2026-08-11", ApprovedRate: &rate}, 43)
	if e != nil {
		t.Fatal(e)
	}
	if *decided.InsuranceAmount != 35000 || *decided.PatientAmount != 15000 || *decided.ApprovedRate != 70 || decided.ContractRate != 80 || decided.DecidedBy == nil || *decided.DecidedBy != 43 {
		t.Fatalf("decision=%#v", decided)
	}
	if _, e = s.Decide(created.ID, DecisionRequest{Status: StatusRejected, ExternalReference: "X", ExternalDecisionDate: "2026-08-11", RejectionReason: "X"}, 44); !IsConflict(e) {
		t.Fatalf("final mutation=%v", e)
	}
	var events []medical_records.MedicalTimelineEvent
	db.Where("reference_type=? AND reference_id=?", "insurance_authorization", created.ID).Order("id").Find(&events)
	if len(events) != 3 || events[0].CreatedBy != 41 || events[1].CreatedBy != 42 || events[2].CreatedBy != 43 {
		t.Fatalf("events=%#v", events)
	}
}

func TestAuthorizationRejectsForeignActAndCoverage(t *testing.T) {
	db := authorizationDB(t)
	f := seedAuthorization(t, db)
	s := NewService(db)
	amount := 100.0
	if _, e := s.Create(CreateRequest{PatientID: f.other.ID, PatientCoverageID: f.coverage.ID, ReferenceType: "CONSULTATION", ReferenceID: f.act.ID, RequestedAmount: &amount}, 1); !IsConflict(e) {
		t.Fatalf("foreign coverage=%v", e)
	}
	foreign := coverage.PatientCoverage{PatientID: f.other.ID, CompanyID: f.coverage.CompanyID, GuarantorID: f.coverage.GuarantorID, MemberNumber: "OTHER", IsActive: true}
	db.Create(&foreign)
	if _, e := s.Create(CreateRequest{PatientID: f.other.ID, PatientCoverageID: foreign.ID, ReferenceType: "CONSULTATION", ReferenceID: f.act.ID, RequestedAmount: &amount}, 1); !IsConflict(e) {
		t.Fatalf("foreign act=%v", e)
	}
}

func TestConcurrentFinalDecisionProducesOneWinner(t *testing.T) {
	db := authorizationDB(t)
	f := seedAuthorization(t, db)
	s := NewService(db)
	amount := 1000.0
	item, e := s.Create(CreateRequest{PatientID: f.patient.ID, PatientCoverageID: f.coverage.ID, ReferenceType: "CONSULTATION", ReferenceID: f.act.ID, RequestedAmount: &amount}, 1)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Submit(item.ID, SubmitRequest{}, 1); e != nil {
		t.Fatal(e)
	}
	rate := 50.0
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(user uint) {
			defer wg.Done()
			_, e := s.Decide(item.ID, DecisionRequest{Status: StatusApproved, ExternalReference: fmt.Sprintf("EXT-%d", user), ExternalDecisionDate: "2026-08-11", ApprovedRate: &rate}, user)
			results <- e
		}(uint(i + 2))
	}
	wg.Wait()
	close(results)
	success := 0
	for e := range results {
		if e == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("winners=%d", success)
	}
}

func TestAuthorizationTableNameAndRoutes(t *testing.T) {
	if (InsuranceAuthorization{}).TableName() != "insurance_authorizations" {
		t.Fatal("table name")
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterRoutes(router.Group("/api"), NewHandler(nil))
	want := map[string]bool{"GET /api/insurance/authorizations": false, "GET /api/insurance/authorizations/for-act": false, "GET /api/insurance/authorizations/eligible-acts": false, "GET /api/insurance/authorizations/:id": false, "POST /api/insurance/authorizations": false, "POST /api/insurance/authorizations/:id/submit": false, "POST /api/insurance/authorizations/:id/pending": false, "POST /api/insurance/authorizations/:id/decision": false, "POST /api/insurance/authorizations/:id/cancel": false, "POST /api/insurance/authorizations/:id/acts": false}
	for _, r := range router.Routes() {
		want[r.Method+" "+r.Path] = true
	}
	for route, ok := range want {
		if !ok {
			t.Errorf("route absente %s", route)
		}
	}
}
