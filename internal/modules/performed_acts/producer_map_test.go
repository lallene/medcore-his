package performed_acts

import (
	"testing"

	"github.com/lallene/medcore-his/backend/internal/modules/act_catalog"
	"github.com/lallene/medcore-his/backend/internal/modules/medical_records"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func mapDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:map_"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&patients.Patient{}, &act_catalog.Entry{}, &Act{}, &ProducerMap{},
		&medical_records.MedicalRecord{}, &medical_records.MedicalTimelineEvent{},
	); err != nil {
		t.Fatal(err)
	}
	_ = db.Exec(`CREATE TABLE IF NOT EXISTS medical_exams (
		id INTEGER PRIMARY KEY, code TEXT, category TEXT, is_active INTEGER, name TEXT)`)
	return db
}

func seedCatalog(t *testing.T, db *gorm.DB, code, category string, active bool) act_catalog.Entry {
	t.Helper()
	e := act_catalog.Entry{
		Code: code, Label: code, Category: category, BasePrice: 1000, Currency: "XOF",
		Billable: true, InsuranceEligible: true, IsActive: true, CreatedBy: 1, UpdatedBy: 1,
	}
	if err := db.Create(&e).Error; err != nil {
		t.Fatal(err)
	}
	if !active {
		if err := db.Model(&e).Update("is_active", false).Error; err != nil {
			t.Fatal(err)
		}
		e.IsActive = false
	}
	return e
}

func TestProducerMapCRUDAndValidation(t *testing.T) {
	db := mapDB(t)
	s := NewService(db)
	active := seedCatalog(t, db, "CONSULTATION", "CONSULTATION", true)
	inactive := seedCatalog(t, db, "INACTIVE", "CONSULTATION", false)
	lab := seedCatalog(t, db, "NFS", "LABORATORY", true)
	img := seedCatalog(t, db, "XR", "IMAGING", true)

	_, err := s.UpsertProducerMap(UpsertProducerMapRequest{
		SourceType: "UNKNOWN", ActCatalogEntryID: active.ID,
	}, 1)
	if err == nil {
		t.Fatal("unknown source accepted")
	}

	m1, err := s.UpsertProducerMap(UpsertProducerMapRequest{
		SourceType: SourceConsultation, ClinicalKey: "", ActCatalogEntryID: active.ID,
	}, 2)
	if err != nil {
		t.Fatal(err)
	}
	m2, err := s.UpsertProducerMap(UpsertProducerMapRequest{
		SourceType: SourceLaboratory, ClinicalKey: "NFS", ActCatalogEntryID: lab.ID,
	}, 2)
	if err != nil || m2.ClinicalKey != "NFS" {
		t.Fatalf("%+v %v", m2, err)
	}
	m3, err := s.UpsertProducerMap(UpsertProducerMapRequest{
		SourceType: SourceImaging, ClinicalKey: "XR", ActCatalogEntryID: img.ID,
	}, 2)
	if err != nil {
		t.Fatal(err)
	}
	_ = m3

	_, err = s.UpsertProducerMap(UpsertProducerMapRequest{
		SourceType: SourceConsultation, ClinicalKey: "x", ActCatalogEntryID: active.ID,
	}, 2)
	if err == nil {
		t.Fatal("non-empty consultation key accepted")
	}

	_, err = s.UpsertProducerMap(UpsertProducerMapRequest{
		SourceType: SourceConsultation, ActCatalogEntryID: 99999,
	}, 2)
	if err == nil {
		t.Fatal("missing catalogue accepted")
	}

	on := true
	_, err = s.UpsertProducerMap(UpsertProducerMapRequest{
		SourceType: SourceConsultation, ActCatalogEntryID: inactive.ID, IsActive: &on,
	}, 2)
	if err == nil {
		t.Fatal("active map to inactive catalogue accepted")
	}

	updated, err := s.UpdateProducerMap(m1.ID, UpdateProducerMapRequest{
		ActCatalogEntryID: active.ID, IsActive: &on,
	}, 3)
	if err != nil || updated.UpdatedBy != 3 {
		t.Fatalf("%+v %v", updated, err)
	}

	deact, err := s.DeactivateProducerMap(m1.ID, 4)
	if err != nil || deact.IsActive {
		t.Fatalf("%+v %v", deact, err)
	}

	list, err := s.ListProducerMaps()
	if err != nil || len(list) < 3 {
		t.Fatalf("list=%d err=%v", len(list), err)
	}
}

func TestProducerReadiness(t *testing.T) {
	db := mapDB(t)
	s := NewService(db).WithProducersEnabled(false)

	r, err := s.ProducerReadinessReport()
	if err != nil {
		t.Fatal(err)
	}
	if r.Ready || r.ProducersEnabled {
		t.Fatalf("%+v", r)
	}
	if len(r.Missing) == 0 {
		t.Fatal("expected consultation missing")
	}

	cat := seedCatalog(t, db, "CONSULTATION", "CONSULTATION", true)
	if _, err := s.UpsertProducerMap(UpsertProducerMapRequest{
		SourceType: SourceConsultation, ActCatalogEntryID: cat.ID,
	}, 1); err != nil {
		t.Fatal(err)
	}
	r, err = s.ProducerReadinessReport()
	if err != nil || !r.Ready {
		t.Fatalf("consult-only ready expected when no lab/img exams: %+v %v", r, err)
	}
	if !r.Producers[SourceLaboratory].Ready || r.Producers[SourceLaboratory].RequiredCount != 0 {
		t.Fatalf("zero lab exams must be ready: %+v", r.Producers[SourceLaboratory])
	}
	if !r.Producers[SourceImaging].Ready || r.Producers[SourceImaging].RequiredCount != 0 {
		t.Fatalf("zero imaging exams must be ready: %+v", r.Producers[SourceImaging])
	}

	if err := db.Exec(`INSERT INTO medical_exams(code, category, is_active, name) VALUES ('NFS','Laboratoire',1,'NFS')`).Error; err != nil {
		t.Fatal(err)
	}
	r, err = s.ProducerReadinessReport()
	if err != nil || r.Ready {
		t.Fatalf("lab missing should block: %+v %v", r, err)
	}
	labCat := seedCatalog(t, db, "NFS", "LABORATORY", true)
	if _, err := s.UpsertProducerMap(UpsertProducerMapRequest{
		SourceType: SourceLaboratory, ClinicalKey: "NFS", ActCatalogEntryID: labCat.ID,
	}, 1); err != nil {
		t.Fatal(err)
	}

	if err := db.Exec(`INSERT INTO medical_exams(code, category, is_active, name) VALUES ('XR','Imagerie',1,'XR')`).Error; err != nil {
		t.Fatal(err)
	}
	r, err = s.ProducerReadinessReport()
	if err != nil || r.Ready {
		t.Fatalf("imaging missing: %+v %v", r, err)
	}
	imgCat := seedCatalog(t, db, "XR", "IMAGING", true)
	if _, err := s.UpsertProducerMap(UpsertProducerMapRequest{
		SourceType: SourceImaging, ClinicalKey: "XR", ActCatalogEntryID: imgCat.ID,
	}, 1); err != nil {
		t.Fatal(err)
	}
	r, err = s.ProducerReadinessReport()
	if err != nil || !r.Ready {
		t.Fatalf("want ready: %+v %v", r, err)
	}

	// inactive map
	off := false
	maps, _ := s.ListProducerMaps()
	for _, m := range maps {
		if m.SourceType == SourceLaboratory {
			_, _ = s.UpdateProducerMap(m.ID, UpdateProducerMapRequest{ActCatalogEntryID: labCat.ID, IsActive: &off}, 1)
		}
	}
	r, err = s.ProducerReadinessReport()
	if err != nil || r.Ready {
		t.Fatalf("inactive map: %+v %v", r, err)
	}
}

func TestSnapshotHelperParityManualVsProducer(t *testing.T) {
	db := mapDB(t)
	s := NewService(db)
	p := patients.Patient{CodePatient: "S1", NumeroDossier: "S1", Nom: "Snap"}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	rec := medical_records.MedicalRecord{PatientID: p.ID, RecordNumber: "S1-MR", Status: "active"}
	if err := db.Create(&rec).Error; err != nil {
		t.Fatal(err)
	}
	cat := seedCatalog(t, db, "SNAP", "PROCEDURE", true)
	if _, err := s.UpsertProducerMap(UpsertProducerMapRequest{
		SourceType: SourceConsultation, ActCatalogEntryID: cat.ID,
	}, 1); err != nil {
		t.Fatal(err)
	}
	manual, err := s.Create(CreateRequest{PatientID: p.ID, ActCatalogEntryID: cat.ID, Quantity: 1}, 9)
	if err != nil {
		t.Fatal(err)
	}
	prod, err := s.EnsureFromProducer(db, ProducerCreateRequest{
		SourceType: SourceConsultation, SourceID: 55, PatientID: p.ID, ActorID: 9,
	})
	if err != nil {
		t.Fatal(err)
	}
	if manual.ActCode != prod.ActCode || manual.ActLabel != prod.ActLabel ||
		manual.BasePrice != prod.BasePrice || manual.ActCategory != prod.ActCategory ||
		manual.Currency != prod.Currency || manual.Billable != prod.Billable ||
		manual.InsuranceEligible != prod.InsuranceEligible || manual.ActDescription != prod.ActDescription {
		t.Fatalf("snapshot mismatch manual=%+v prod=%+v", manual, prod)
	}
}

func TestEnablementDisabledSkipsProducerWiringSemantics(t *testing.T) {
	// When performedActs is nil on clinical services, no acts are created — covered by module gate.
	// Here prove WithProducersEnabled default and readiness flag.
	s := NewService(nil)
	if s.ProducersEnabled() {
		t.Fatal("default enabled")
	}
	if !s.WithProducersEnabled(true).ProducersEnabled() {
		t.Fatal("want enabled")
	}
}
