package authorization

import (
	"fmt"
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
	"gorm.io/gorm"
)

type authorizationConsultation struct {
	ID, PatientID               uint
	ServiceID                   *uint `gorm:"index"`
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
	ConsultationID               uint  `gorm:"not null;index"`
	RequestingServiceID          *uint `gorm:"index"`
	RequestNumber, Status        string
	CreatedAt                    time.Time
}

func (authorizationLaboratoryOrder) TableName() string { return "laboratory_orders" }

type authorizationImagingOrder struct {
	ID, PatientID, MedicalExamID  uint
	ConsultationID                uint  `gorm:"not null;index"`
	RequestingServiceID           *uint `gorm:"index"`
	OrderNumber, Modality, Status string
	CreatedAt                     time.Time
}

func (authorizationImagingOrder) TableName() string { return "imaging_orders" }

type authorizationHospitalization struct {
	ID, PatientID                                        uint
	ServiceID                                            *uint `gorm:"index"`
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
	db := openAuthorizationPool(t, "authorization")
	migrateAuthorizationSchema(t, db)
	return db
}

func TestEligibleActsCoversSupportedClinicalSources(t *testing.T) {
	db := authorizationDB(t)
	f := seedAuthorization(t, db)
	exam := authorizationMedicalExam{Name: "Radiographie thoracique", Category: "Imagerie"}
	db.Create(&exam)
	lab := authorizationLaboratoryOrder{PatientID: f.patient.ID, MedicalExamID: exam.ID, ConsultationID: f.act.ID, RequestNumber: "LAB-1", Status: "VALIDATED"}
	db.Create(&lab)
	img := authorizationImagingOrder{PatientID: f.patient.ID, MedicalExamID: exam.ID, ConsultationID: f.act.ID, OrderNumber: "IMG-1", Modality: "XR", Status: "VALIDATED"}
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
	if primary.ServiceID != nil || primary.Service != "Médecine" {
		t.Fatalf("nullable ServiceID fallback: ServiceID=%v Service=%q", primary.ServiceID, primary.Service)
	}
	direct, err := s.FindAuthorizationForAct(f.patient.ID, f.coverage.ID, "CONSULTATION", f.act.ID)
	if err != nil || direct.MatchType != "DIRECT" || direct.Authorization.ID != primary.ID {
		t.Fatalf("direct=%#v err=%v", direct, err)
	}
	exam := authorizationMedicalExam{Name: "Radiographie thoracique"}
	db.Create(&exam)
	imaging := authorizationImagingOrder{PatientID: f.patient.ID, MedicalExamID: exam.ID, ConsultationID: f.act.ID}
	db.Create(&imaging)
	none, err := s.FindAuthorizationForAct(f.patient.ID, f.coverage.ID, "IMAGING", imaging.ID)
	if err != nil || none.MatchType != "NONE" {
		t.Fatalf("none=%#v err=%v", none, err)
	}
	linked, err := s.LinkAct(primary.ID, ActRequest{ReferenceType: "IMAGING", ReferenceID: imaging.ID}, 72, UnrestrictedAccess(72))
	if err != nil || linked.CreatedBy != 72 || linked.ReferenceLabel != exam.Name {
		t.Fatalf("linked=%#v err=%v", linked, err)
	}
	covered, err := s.FindAuthorizationForAct(f.patient.ID, f.coverage.ID, "IMAGING", imaging.ID)
	if err != nil || covered.MatchType != "COVERED" || covered.Authorization.ID != primary.ID || len(covered.Authorization.CoveredActs) != 1 {
		t.Fatalf("covered=%#v err=%v", covered, err)
	}
	again, err := s.LinkAct(primary.ID, ActRequest{ReferenceType: "IMAGING", ReferenceID: imaging.ID}, 99, UnrestrictedAccess(99))
	if err != nil || again.ID != linked.ID {
		t.Fatalf("idempotent=%#v err=%v", again, err)
	}
	foreignImaging := authorizationImagingOrder{PatientID: f.other.ID, MedicalExamID: exam.ID, ConsultationID: f.act.ID}
	db.Create(&foreignImaging)
	if _, err = s.LinkAct(primary.ID, ActRequest{ReferenceType: "IMAGING", ReferenceID: foreignImaging.ID}, 72, UnrestrictedAccess(72)); !IsConflict(err) {
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
	concurrentImaging := authorizationImagingOrder{PatientID: f.patient.ID, MedicalExamID: exam.ID, ConsultationID: f.act.ID}
	db.Create(&concurrentImaging)
	results := make(chan error, 2)
	var wg sync.WaitGroup
	linkStart := make(chan struct{})
	for _, authorizationID := range []uint{primary.ID, secondAuthorization.ID} {
		wg.Add(1)
		go func(candidate uint) {
			defer wg.Done()
			<-linkStart
			_, linkErr := s.LinkAct(candidate, ActRequest{ReferenceType: "IMAGING", ReferenceID: concurrentImaging.ID}, 72, UnrestrictedAccess(72))
			results <- linkErr
		}(authorizationID)
	}
	close(linkStart)
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

func TestCreateAuthorizationWithCoveredActsIsAtomic(t *testing.T) {
	db := authorizationDB(t)
	for _, table := range []string{"pharmacy_dispensations", "pharmacy_vouchers", "pharmacy_stocks", "pharmacy_batches", "stock_movements"} {
		if err := db.Exec("CREATE TABLE " + table + " (id bigserial primary key)").Error; err != nil {
			t.Fatal(err)
		}
	}
	f := seedAuthorization(t, db)
	exam := authorizationMedicalExam{Name: "Scanner multi-actes", Category: "Imagerie"}
	db.Create(&exam)
	imaging := authorizationImagingOrder{PatientID: f.patient.ID, MedicalExamID: exam.ID, ConsultationID: f.act.ID}
	db.Create(&imaging)
	amount := 42000.0
	created, err := NewService(db).Create(CreateRequest{PatientID: f.patient.ID, PatientCoverageID: f.coverage.ID, ReferenceType: "CONSULTATION", ReferenceID: f.act.ID, RequestedAmount: &amount, CoveredActs: []ActRequest{{ReferenceType: "IMAGING", ReferenceID: imaging.ID}}}, 88)
	if err != nil {
		t.Fatal(err)
	}
	if len(created.CoveredActs) != 1 || created.CoveredActs[0].ReferenceID != imaging.ID || created.CoveredActs[0].CreatedBy != 88 {
		t.Fatalf("covered acts=%#v", created.CoveredActs)
	}
	foreign := authorizationImagingOrder{PatientID: f.other.ID, MedicalExamID: exam.ID, ConsultationID: f.act.ID}
	db.Create(&foreign)
	otherAct := authorizationConsultation{PatientID: f.patient.ID, Service: "ORL"}
	db.Create(&otherAct)
	if _, err = NewService(db).Create(CreateRequest{PatientID: f.patient.ID, PatientCoverageID: f.coverage.ID, ReferenceType: "CONSULTATION", ReferenceID: otherAct.ID, CoveredActs: []ActRequest{{ReferenceType: "IMAGING", ReferenceID: foreign.ID}}}, 88); err == nil {
		t.Fatal("foreign covered act accepted")
	}
	var leaked int64
	db.Model(&InsuranceAuthorization{}).Where("reference_type='CONSULTATION' AND reference_id=?", otherAct.ID).Count(&leaked)
	if leaked != 0 {
		t.Fatalf("transaction was not rolled back: %d", leaked)
	}
}

func TestAuthorizationUsesClinicalExamLabels(t *testing.T) {
	db := authorizationDB(t)
	f := seedAuthorization(t, db)
	examLab := authorizationMedicalExam{Name: "Numération formule sanguine"}
	examImaging := authorizationMedicalExam{Name: "Radiographie thoracique"}
	db.Create(&examLab)
	db.Create(&examImaging)
	lab := authorizationLaboratoryOrder{PatientID: f.patient.ID, MedicalExamID: examLab.ID, ConsultationID: f.act.ID}
	imaging := authorizationImagingOrder{PatientID: f.patient.ID, MedicalExamID: examImaging.ID, ConsultationID: f.act.ID}
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
	if labAuthorization.Service != "Médecine" || imagingAuthorization.Service != "Médecine" {
		t.Fatalf("service fallback: laboratory=%q imaging=%q", labAuthorization.Service, imagingAuthorization.Service)
	}
	if labAuthorization.ServiceID != nil || imagingAuthorization.ServiceID != nil {
		t.Fatalf("expected nil RequestingServiceID fallback: lab=%v img=%v", labAuthorization.ServiceID, imagingAuthorization.ServiceID)
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
	submitted, e := s.Submit(created.ID, SubmitRequest{}, 42, UnrestrictedAccess(42))
	if e != nil || submitted.SubmittedBy == nil || *submitted.SubmittedBy != 42 {
		t.Fatalf("submitted=%#v %v", submitted, e)
	}
	pending, e := s.MarkPending(created.ID, 42, UnrestrictedAccess(42))
	if e != nil || pending.Status != StatusPending {
		t.Fatalf("pending=%#v %v", pending, e)
	}
	rate := 70.0
	decided, e := s.Decide(created.ID, DecisionRequest{Status: StatusApproved, ExternalReference: "DEMO-ASSUR-000001", ExternalDecisionDate: "2026-08-11", ApprovedRate: &rate}, 43, UnrestrictedAccess(43))
	if e != nil {
		t.Fatal(e)
	}
	if *decided.InsuranceAmount != 35000 || *decided.PatientAmount != 15000 || *decided.ApprovedRate != 70 || decided.ContractRate != 80 || decided.DecidedBy == nil || *decided.DecidedBy != 43 {
		t.Fatalf("decision=%#v", decided)
	}
	if _, e = s.Decide(created.ID, DecisionRequest{Status: StatusRejected, ExternalReference: "X", ExternalDecisionDate: "2026-08-11", RejectionReason: "X"}, 44, UnrestrictedAccess(44)); !IsConflict(e) {
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
	if _, e = s.Submit(item.ID, SubmitRequest{}, 1, UnrestrictedAccess(1)); e != nil {
		t.Fatal(e)
	}
	assertMultiConnectionPool(t, db)
	// LOT30-A′: real race — N deciders released together on a multi-connection pool.
	const deciders = 6
	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make(chan error, deciders)
	for i := 0; i < deciders; i++ {
		wg.Add(1)
		go func(user uint) {
			defer wg.Done()
			rate := float64(10 * user)
			<-start
			_, e := s.Decide(item.ID, DecisionRequest{Status: StatusApproved, ExternalReference: fmt.Sprintf("EXT-%d", user), ExternalDecisionDate: "2026-08-11", ApprovedRate: &rate}, user, UnrestrictedAccess(user))
			results <- e
		}(uint(i + 2))
	}
	close(start)
	wg.Wait()
	close(results)
	success, conflicts := 0, 0
	for e := range results {
		switch {
		case e == nil:
			success++
		case IsConflict(e):
			conflicts++
		default:
			t.Fatalf("unexpected decision error: %v", e)
		}
	}
	if success != 1 || conflicts != deciders-1 {
		t.Fatalf("winners=%d conflicts=%d", success, conflicts)
	}
	// Exactly one economically valid decision persisted: winner's rate, amount and reference agree.
	var stored InsuranceAuthorization
	if e := db.First(&stored, item.ID).Error; e != nil {
		t.Fatal(e)
	}
	if stored.Status != StatusApproved || stored.DecidedBy == nil || stored.ApprovedRate == nil || stored.InsuranceAmount == nil {
		t.Fatalf("stored decision=%#v", stored)
	}
	winner := *stored.DecidedBy
	if *stored.ApprovedRate != float64(10*winner) || stored.ExternalReference != fmt.Sprintf("EXT-%d", winner) || *stored.InsuranceAmount != amount*float64(10*winner)/100 {
		t.Fatalf("mixed decision fields: %#v", stored)
	}
	var decided int64
	db.Model(&medical_records.MedicalTimelineEvent{}).Where("event_type=? AND reference_id=?", "insurance_authorization_approved", item.ID).Count(&decided)
	if decided != 1 {
		t.Fatalf("decision timeline events=%d", decided)
	}
}

// A′-PG07: concurrent Create on the same act yields exactly one active PEC.
func TestConcurrentCreateSameActProducesOneAuthorization(t *testing.T) {
	db := authorizationDB(t)
	assertMultiConnectionPool(t, db)
	f := seedAuthorization(t, db)
	s := NewService(db)
	amount := 1000.0
	const creators = 6
	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make(chan error, creators)
	for i := 0; i < creators; i++ {
		wg.Add(1)
		go func(user uint) {
			defer wg.Done()
			<-start
			_, e := s.Create(CreateRequest{PatientID: f.patient.ID, PatientCoverageID: f.coverage.ID, ReferenceType: "CONSULTATION", ReferenceID: f.act.ID, RequestedAmount: &amount}, user)
			results <- e
		}(uint(i + 10))
	}
	close(start)
	wg.Wait()
	close(results)
	success, conflicts := 0, 0
	for e := range results {
		switch {
		case e == nil:
			success++
		case IsConflict(e):
			conflicts++
		default:
			t.Fatalf("unexpected create error: %v", e)
		}
	}
	if success != 1 || conflicts != creators-1 {
		t.Fatalf("creates success=%d conflicts=%d", success, conflicts)
	}
	var active int64
	db.Model(&InsuranceAuthorization{}).Where("reference_type=? AND reference_id=? AND status<>?", "CONSULTATION", f.act.ID, StatusCancelled).Count(&active)
	if active != 1 {
		t.Fatalf("active authorizations=%d", active)
	}
}

// A′-PG07: concurrent primary Create vs covered-act Link on the same act — one winner, never both.
func TestConcurrentCreateVersusLinkSameAct(t *testing.T) {
	db := authorizationDB(t)
	assertMultiConnectionPool(t, db)
	f := seedAuthorization(t, db)
	s := NewService(db)
	amount := 1000.0
	parent, err := s.Create(CreateRequest{PatientID: f.patient.ID, PatientCoverageID: f.coverage.ID, ReferenceType: "CONSULTATION", ReferenceID: f.act.ID, RequestedAmount: &amount}, 11)
	if err != nil {
		t.Fatal(err)
	}
	exam := authorizationMedicalExam{Name: "Echographie"}
	db.Create(&exam)
	for round := 0; round < 5; round++ {
		target := authorizationImagingOrder{PatientID: f.patient.ID, MedicalExamID: exam.ID, ConsultationID: f.act.ID}
		db.Create(&target)
		start := make(chan struct{})
		var wg sync.WaitGroup
		var createErr, linkErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, createErr = s.Create(CreateRequest{PatientID: f.patient.ID, PatientCoverageID: f.coverage.ID, ReferenceType: "IMAGING", ReferenceID: target.ID, RequestedAmount: &amount}, 21)
		}()
		go func() {
			defer wg.Done()
			<-start
			_, linkErr = s.LinkAct(parent.ID, ActRequest{ReferenceType: "IMAGING", ReferenceID: target.ID}, 22, UnrestrictedAccess(22))
		}()
		close(start)
		wg.Wait()
		if (createErr == nil) == (linkErr == nil) {
			t.Fatalf("round %d: exactly one must win create=%v link=%v", round, createErr, linkErr)
		}
		for _, e := range []error{createErr, linkErr} {
			if e != nil && !IsConflict(e) {
				t.Fatalf("round %d: unexpected error %v", round, e)
			}
		}
		var primaries, links int64
		db.Model(&InsuranceAuthorization{}).Where("reference_type=? AND reference_id=? AND status<>?", "IMAGING", target.ID, StatusCancelled).Count(&primaries)
		db.Model(&InsuranceAuthorizationAct{}).Where("reference_type=? AND reference_id=? AND is_active", "IMAGING", target.ID).Count(&links)
		if primaries+links != 1 {
			t.Fatalf("round %d: primaries=%d links=%d", round, primaries, links)
		}
	}
}

func assertMultiConnectionPool(t *testing.T, db *gorm.DB) {
	t.Helper()
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if max := sqlDB.Stats().MaxOpenConnections; max < 2 {
		t.Fatalf("concurrency harness must allow several connections, MaxOpenConnections=%d", max)
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
