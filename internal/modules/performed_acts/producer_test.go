package performed_acts

import (
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/act_catalog"
	"github.com/lallene/medcore-his/backend/internal/modules/billing"
	"github.com/lallene/medcore-his/backend/internal/modules/medical_records"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func producerSQLite(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:producer_"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&patients.Patient{},
		&act_catalog.Entry{},
		&Act{},
		&ProducerMap{},
		&billing.Invoice{},
		&billing.InvoiceLine{},
		&medical_records.MedicalRecord{},
		&medical_records.MedicalTimelineEvent{},
	); err != nil {
		t.Fatal(err)
	}
	// Minimal stub so optional ConsultationID validation can succeed without importing consultations.
	if err := db.Exec(`CREATE TABLE IF NOT EXISTS consultations (id INTEGER PRIMARY KEY, patient_id INTEGER)`).Error; err != nil {
		t.Fatal(err)
	}
	return db
}

func seedProducerBase(t *testing.T, db *gorm.DB, sourceType, clinicalKey string) (patients.Patient, act_catalog.Entry) {
	t.Helper()
	p := patients.Patient{CodePatient: "PR-P", NumeroDossier: "PR-D", Nom: "Producer"}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	cat := act_catalog.Entry{
		Code: "CONSULT-GEN", Label: "Consultation générale", Category: "CONSULTATION",
		BasePrice: 5000, Currency: "XOF", Billable: true, InsuranceEligible: true, IsActive: true,
		CreatedBy: 1, UpdatedBy: 1,
	}
	if clinicalKey != "" {
		cat.Code = clinicalKey
		cat.Label = "Exam " + clinicalKey
		cat.Category = "LABORATORY"
		if sourceType == SourceImaging {
			cat.Category = "IMAGING"
		}
	}
	if err := db.Create(&cat).Error; err != nil {
		t.Fatal(err)
	}
	m := ProducerMap{
		SourceType: sourceType, ClinicalKey: clinicalKey, ActCatalogEntryID: cat.ID,
		IsActive: true, CreatedBy: 1, UpdatedBy: 1,
	}
	if err := db.Create(&m).Error; err != nil {
		t.Fatal(err)
	}
	rec := medical_records.MedicalRecord{PatientID: p.ID, RecordNumber: "PR-MR", Status: "active"}
	if err := db.Create(&rec).Error; err != nil {
		t.Fatal(err)
	}
	return p, cat
}

func TestProducerHappyPathSnapshotAndContext(t *testing.T) {
	db := producerSQLite(t)
	p, cat := seedProducerBase(t, db, SourceConsultation, ConsultationClinicalKey)
	if err := db.Exec(`INSERT INTO consultations (id, patient_id) VALUES (42, ?)`, p.ID).Error; err != nil {
		t.Fatal(err)
	}
	s := NewService(db)
	cid := uint(42)
	now := time.Now().UTC().Truncate(time.Second)
	act, err := s.EnsureFromProducer(db, ProducerCreateRequest{
		SourceType: SourceConsultation, SourceID: 42, PatientID: p.ID,
		ClinicalKey: ConsultationClinicalKey, ConsultationID: &cid, PerformedAt: &now, ActorID: 42,
	})
	if err != nil {
		t.Fatal(err)
	}
	if act.ActCode != cat.Code || act.BasePrice != cat.BasePrice || act.ActLabel != cat.Label {
		t.Fatalf("snapshot mismatch: %+v vs %+v", act, cat)
	}
	if act.PatientID != p.ID || act.SourceType != SourceConsultation || act.SourceID == nil || *act.SourceID != 42 {
		t.Fatalf("context mismatch: %+v", act)
	}
	if act.ConsultationID == nil || *act.ConsultationID != 42 || act.PerformedBy != 42 {
		t.Fatalf("actor/context: %+v", act)
	}
}

func TestProducerIdempotentRetry(t *testing.T) {
	db := producerSQLite(t)
	p, _ := seedProducerBase(t, db, SourceConsultation, ConsultationClinicalKey)
	s := NewService(db)
	req := ProducerCreateRequest{
		SourceType: SourceConsultation, SourceID: 77, PatientID: p.ID,
		ClinicalKey: ConsultationClinicalKey, ActorID: 1,
	}
	a1, err := s.EnsureFromProducer(db, req)
	if err != nil {
		t.Fatal(err)
	}
	a2, err := s.EnsureFromProducer(db, req)
	if err != nil {
		t.Fatal(err)
	}
	if a1.ID != a2.ID {
		t.Fatalf("duplicate acts: %d vs %d", a1.ID, a2.ID)
	}
	var n int64
	if err := db.Model(&Act{}).Where("source_type=? AND source_id=?", SourceConsultation, 77).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("count=%d", n)
	}
}

func TestProducerMissingMappingFailsClosed(t *testing.T) {
	db := producerSQLite(t)
	p := patients.Patient{CodePatient: "PR-M", NumeroDossier: "PR-MD", Nom: "Map"}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	s := NewService(db)
	_, err := s.EnsureFromProducer(db, ProducerCreateRequest{
		SourceType: SourceLaboratory, SourceID: 1, PatientID: p.ID, ClinicalKey: "MISSING", ActorID: 1,
	})
	if err == nil {
		t.Fatal("expected missing map error")
	}
	var n int64
	_ = db.Model(&Act{}).Count(&n)
	if n != 0 {
		t.Fatalf("partial create n=%d", n)
	}
}

func TestProducerInactiveCatalogFailsClosed(t *testing.T) {
	db := producerSQLite(t)
	p, cat := seedProducerBase(t, db, SourceConsultation, ConsultationClinicalKey)
	cat.IsActive = false
	if err := db.Save(&cat).Error; err != nil {
		t.Fatal(err)
	}
	s := NewService(db)
	_, err := s.EnsureFromProducer(db, ProducerCreateRequest{
		SourceType: SourceConsultation, SourceID: 9, PatientID: p.ID, ActorID: 1,
	})
	if err == nil {
		t.Fatal("expected inactive catalog error")
	}
}

func TestProducerInvalidSourceTypeRejected(t *testing.T) {
	db := producerSQLite(t)
	p, _ := seedProducerBase(t, db, SourceConsultation, ConsultationClinicalKey)
	s := NewService(db)
	_, err := s.EnsureFromProducer(db, ProducerCreateRequest{
		SourceType: "HOSPITALIZATION", SourceID: 1, PatientID: p.ID, ActorID: 1,
	})
	if err == nil {
		t.Fatal("expected invalid source type")
	}
}

func TestProducerDoesNotCreateInvoiceOrAuth(t *testing.T) {
	db := producerSQLite(t)
	p, _ := seedProducerBase(t, db, SourceConsultation, ConsultationClinicalKey)
	s := NewService(db)
	if _, err := s.EnsureFromProducer(db, ProducerCreateRequest{
		SourceType: SourceConsultation, SourceID: 3, PatientID: p.ID, ActorID: 1,
	}); err != nil {
		t.Fatal(err)
	}
	var inv, lines int64
	_ = db.Model(&billing.Invoice{}).Count(&inv)
	_ = db.Model(&billing.InvoiceLine{}).Count(&lines)
	if inv != 0 || lines != 0 {
		t.Fatalf("billing mutated inv=%d lines=%d", inv, lines)
	}
}

func TestProducerConcurrentEnsureSameSource(t *testing.T) {
	t.Skip("SQLite locks under concurrent writers; concurrency proven on PostgreSQL (TestPostgresProducerSourceUniquenessAndConcurrency)")
}

func TestSourceTypesBounded(t *testing.T) {
	t.Parallel()
	if !ValidSourceTypes[SourceConsultation] || !ValidSourceTypes[SourceLaboratory] || !ValidSourceTypes[SourceImaging] {
		t.Fatal("missing source types")
	}
	if ValidSourceTypes["MEDICATION"] || ValidSourceTypes["HOSPITALIZATION"] {
		t.Fatal("future sources must not be registered yet")
	}
}
