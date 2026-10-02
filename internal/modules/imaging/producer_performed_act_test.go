package imaging

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	sqlite3 "github.com/mattn/go-sqlite3"
	"github.com/lallene/medcore-his/backend/internal/modules/act_catalog"
	"github.com/lallene/medcore-his/backend/internal/modules/consultations"
	"github.com/lallene/medcore-his/backend/internal/modules/medical_records"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
	"github.com/lallene/medcore-his/backend/internal/modules/performed_acts"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func init() {
	sql.Register("sqlite3_img_producer", &sqlite3.SQLiteDriver{
		ConnectHook: func(conn *sqlite3.SQLiteConn) error {
			return conn.RegisterFunc("BTRIM", func(s string) string { return strings.TrimSpace(s) }, true)
		},
	})
}

func imagingProducerDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Dialector{
		DriverName: "sqlite3_img_producer",
		DSN:        "file:img_prod_" + t.Name() + "?mode=memory&cache=shared",
	}, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&patients.Patient{}, &consultations.Consultation{}, &consultations.MedicalExam{},
		&consultations.ConsultationExamRequest{}, &medical_records.MedicalRecord{},
		&medical_records.MedicalTimelineEvent{},
		&Order{}, &Report{}, &act_catalog.Entry{}, &performed_acts.Act{}, &performed_acts.ProducerMap{},
	); err != nil {
		t.Fatal(err)
	}
	return db
}

func seedImagingPatientMR(t *testing.T, db *gorm.DB, code, dossier, nom string) patients.Patient {
	t.Helper()
	p := patients.Patient{CodePatient: code, NumeroDossier: dossier, Nom: nom}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	rec := medical_records.MedicalRecord{PatientID: p.ID, RecordNumber: "MR-" + code, Status: "active"}
	if err := db.Create(&rec).Error; err != nil {
		t.Fatal(err)
	}
	return p
}

func TestImagingStartCreatesPerformedAct(t *testing.T) {
	db := imagingProducerDB(t)
	p := seedImagingPatientMR(t, db, "IP1", "ID1", "I")
	c := consultations.Consultation{PatientID: p.ID, DoctorName: "Dr", Service: "Radio", Status: "in_progress"}
	if err := db.Create(&c).Error; err != nil {
		t.Fatal(err)
	}
	exam := consultations.MedicalExam{Code: "XR-CHEST", Name: "Radio", Category: "Imagerie", IsActive: true}
	if err := db.Create(&exam).Error; err != nil {
		t.Fatal(err)
	}
	cat := act_catalog.Entry{
		Code: "XR-CHEST", Label: "Radio thorax", Category: "IMAGING", BasePrice: 12000, Currency: "XOF",
		Billable: true, InsuranceEligible: true, IsActive: true, CreatedBy: 1, UpdatedBy: 1,
	}
	if err := db.Create(&cat).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&performed_acts.ProducerMap{
		SourceType: performed_acts.SourceImaging, ClinicalKey: "XR-CHEST", ActCatalogEntryID: cat.ID,
		IsActive: true, CreatedBy: 1, UpdatedBy: 1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	o := Order{
		OrderNumber: "IMG-1", AccessionNumber: "ACC-1", ConsultationID: c.ID, MedicalExamID: exam.ID,
		PatientID: p.ID, Modality: "XR", Priority: "ROUTINE", Status: StatusOrdered, CreatedBy: 1, UpdatedBy: 1,
	}
	if err := db.Create(&o).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewService(NewRepository(db)).WithPerformedActs(performed_acts.NewService(db))
	out, err := svc.Start(o.ID, Access{UserID: 9, Permissions: map[string]bool{"*": true}}, StartRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != StatusInProgress || out.PerformedAt == nil {
		t.Fatalf("order=%+v", out)
	}
	var act performed_acts.Act
	if err := db.Where("source_type=? AND source_id=?", performed_acts.SourceImaging, o.ID).First(&act).Error; err != nil {
		t.Fatal(err)
	}
	if act.ActCode != "XR-CHEST" || act.BasePrice != 12000 || act.PerformedBy != 9 {
		t.Fatalf("act=%+v", act)
	}
}

func TestImagingScheduleDoesNotCreatePerformedAct(t *testing.T) {
	db := imagingProducerDB(t)
	p := patients.Patient{CodePatient: "IP2", NumeroDossier: "ID2", Nom: "I"}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	c := consultations.Consultation{PatientID: p.ID, DoctorName: "Dr", Service: "Radio", Status: "in_progress"}
	if err := db.Create(&c).Error; err != nil {
		t.Fatal(err)
	}
	exam := consultations.MedicalExam{Code: "XR-2", Name: "Radio", Category: "Imagerie", IsActive: true}
	if err := db.Create(&exam).Error; err != nil {
		t.Fatal(err)
	}
	cat := act_catalog.Entry{
		Code: "XR-2", Label: "Radio", Category: "IMAGING", BasePrice: 1, Currency: "XOF",
		Billable: true, InsuranceEligible: true, IsActive: true, CreatedBy: 1, UpdatedBy: 1,
	}
	if err := db.Create(&cat).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&performed_acts.ProducerMap{
		SourceType: performed_acts.SourceImaging, ClinicalKey: "XR-2", ActCatalogEntryID: cat.ID,
		IsActive: true, CreatedBy: 1, UpdatedBy: 1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	o := Order{
		OrderNumber: "IMG-2", AccessionNumber: "ACC-2", ConsultationID: c.ID, MedicalExamID: exam.ID,
		PatientID: p.ID, Modality: "XR", Priority: "ROUTINE", Status: StatusOrdered, CreatedBy: 1, UpdatedBy: 1,
	}
	if err := db.Create(&o).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewService(NewRepository(db)).WithPerformedActs(performed_acts.NewService(db))
	if _, err := svc.Schedule(o.ID, Access{UserID: 9, Permissions: map[string]bool{"*": true}}, ScheduleRequest{ScheduledAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	var n int64
	_ = db.Model(&performed_acts.Act{}).Count(&n)
	if n != 0 {
		t.Fatalf("n=%d", n)
	}
}

func TestImagingStartMissingMapRollsBack(t *testing.T) {
	db := imagingProducerDB(t)
	p := patients.Patient{CodePatient: "IP3", NumeroDossier: "ID3", Nom: "I"}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	c := consultations.Consultation{PatientID: p.ID, DoctorName: "Dr", Service: "Radio", Status: "in_progress"}
	if err := db.Create(&c).Error; err != nil {
		t.Fatal(err)
	}
	exam := consultations.MedicalExam{Code: "XR-3", Name: "Radio", Category: "Imagerie", IsActive: true}
	if err := db.Create(&exam).Error; err != nil {
		t.Fatal(err)
	}
	o := Order{
		OrderNumber: "IMG-3", AccessionNumber: "ACC-3", ConsultationID: c.ID, MedicalExamID: exam.ID,
		PatientID: p.ID, Modality: "XR", Priority: "ROUTINE", Status: StatusOrdered, CreatedBy: 1, UpdatedBy: 1,
	}
	if err := db.Create(&o).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewService(NewRepository(db)).WithPerformedActs(performed_acts.NewService(db))
	_, err := svc.Start(o.ID, Access{UserID: 9, Permissions: map[string]bool{"*": true}}, StartRequest{})
	if err == nil {
		t.Fatal("expected failure")
	}
	var status string
	_ = db.Raw(`SELECT status FROM imaging_orders WHERE id=?`, o.ID).Scan(&status)
	if status != StatusOrdered {
		t.Fatalf("status=%s", status)
	}
}
