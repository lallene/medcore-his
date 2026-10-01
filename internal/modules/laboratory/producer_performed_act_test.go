package laboratory

import (
	"database/sql"
	"strings"
	"testing"

	sqlite3 "github.com/mattn/go-sqlite3"
	"github.com/lallene/medcore-his/backend/internal/modules/act_catalog"
	"github.com/lallene/medcore-his/backend/internal/modules/billing"
	"github.com/lallene/medcore-his/backend/internal/modules/consultations"
	"github.com/lallene/medcore-his/backend/internal/modules/medical_records"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
	"github.com/lallene/medcore-his/backend/internal/modules/performed_acts"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func init() {
	sql.Register("sqlite3_lab_producer", &sqlite3.SQLiteDriver{
		ConnectHook: func(conn *sqlite3.SQLiteConn) error {
			return conn.RegisterFunc("BTRIM", func(s string) string { return strings.TrimSpace(s) }, true)
		},
	})
}

func labProducerDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Dialector{
		DriverName: "sqlite3_lab_producer",
		DSN:        "file:lab_prod_" + t.Name() + "?mode=memory&cache=shared",
	}, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&patients.Patient{}, &consultations.Consultation{}, &consultations.MedicalExam{},
		&consultations.ConsultationExamRequest{}, &medical_records.MedicalRecord{},
		&Order{}, &Sample{}, &Result{}, &act_catalog.Entry{}, &performed_acts.Act{}, &performed_acts.ProducerMap{},
		&billing.Invoice{}, &billing.InvoiceLine{},
	); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestLabValidateCreatesPerformedAct(t *testing.T) {
	db := labProducerDB(t)
	p := patients.Patient{CodePatient: "LP1", NumeroDossier: "LD1", Nom: "L"}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	c := consultations.Consultation{PatientID: p.ID, DoctorName: "Dr", Service: "Lab", Status: "in_progress"}
	if err := db.Create(&c).Error; err != nil {
		t.Fatal(err)
	}
	exam := consultations.MedicalExam{Code: "NFS-OK", Name: "NFS", Category: "Laboratoire", IsActive: true}
	if err := db.Create(&exam).Error; err != nil {
		t.Fatal(err)
	}
	cat := act_catalog.Entry{
		Code: "NFS-OK", Label: "NFS", Category: "LABORATORY", BasePrice: 3500, Currency: "XOF",
		Billable: true, InsuranceEligible: true, IsActive: true, CreatedBy: 1, UpdatedBy: 1,
	}
	if err := db.Create(&cat).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&performed_acts.ProducerMap{
		SourceType: performed_acts.SourceLaboratory, ClinicalKey: "NFS-OK", ActCatalogEntryID: cat.ID,
		IsActive: true, CreatedBy: 1, UpdatedBy: 1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	o := Order{
		RequestNumber: "REQ-1", ConsultationID: c.ID, MedicalExamID: exam.ID, PatientID: p.ID,
		Priority: "ROUTINE", Status: StatusResultEntered, CreatedBy: 1, UpdatedBy: 1,
	}
	if err := db.Create(&o).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&Result{OrderID: o.ID, Parameter: "Hb", Value: "13", Flag: "NORMAL", EnteredBy: 1}).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewService(NewRepository(db)).WithPerformedActs(performed_acts.NewService(db))
	out, err := svc.Validate(o.ID, Access{UserID: 8, Permissions: map[string]bool{"*": true}})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != StatusValidated {
		t.Fatalf("status=%s", out.Status)
	}
	var act performed_acts.Act
	if err := db.Where("source_type=? AND source_id=?", performed_acts.SourceLaboratory, o.ID).First(&act).Error; err != nil {
		t.Fatal(err)
	}
	if act.ActCode != "NFS-OK" || act.BasePrice != 3500 || act.PerformedBy != 8 {
		t.Fatalf("act=%+v", act)
	}
	var inv, lines int64
	_ = db.Model(&billing.Invoice{}).Count(&inv)
	_ = db.Model(&billing.InvoiceLine{}).Count(&lines)
	if inv != 0 || lines != 0 {
		t.Fatalf("billing mutated")
	}
}

func TestLabValidateMissingMapRollsBack(t *testing.T) {
	db := labProducerDB(t)
	p := patients.Patient{CodePatient: "LP2", NumeroDossier: "LD2", Nom: "L"}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	c := consultations.Consultation{PatientID: p.ID, DoctorName: "Dr", Service: "Lab", Status: "in_progress"}
	if err := db.Create(&c).Error; err != nil {
		t.Fatal(err)
	}
	exam := consultations.MedicalExam{Code: "NFS-X", Name: "NFS", Category: "Laboratoire", IsActive: true}
	if err := db.Create(&exam).Error; err != nil {
		t.Fatal(err)
	}
	o := Order{
		RequestNumber: "REQ-2", ConsultationID: c.ID, MedicalExamID: exam.ID, PatientID: p.ID,
		Priority: "ROUTINE", Status: StatusResultEntered, CreatedBy: 1, UpdatedBy: 1,
	}
	if err := db.Create(&o).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&Result{OrderID: o.ID, Parameter: "Hb", Value: "12", Flag: "NORMAL", EnteredBy: 1}).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewService(NewRepository(db)).WithPerformedActs(performed_acts.NewService(db))
	_, err := svc.Validate(o.ID, Access{UserID: 5, Permissions: map[string]bool{"*": true}})
	if err == nil {
		t.Fatal("expected failure")
	}
	var status string
	_ = db.Raw(`SELECT status FROM laboratory_orders WHERE id=?`, o.ID).Scan(&status)
	if status != StatusResultEntered {
		t.Fatalf("status=%s", status)
	}
}

func TestLabPrepareSampleDoesNotCreatePerformedAct(t *testing.T) {
	db := labProducerDB(t)
	p := patients.Patient{CodePatient: "LP3", NumeroDossier: "LD3", Nom: "L"}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	c := consultations.Consultation{PatientID: p.ID, DoctorName: "Dr", Service: "Lab", Status: "in_progress"}
	if err := db.Create(&c).Error; err != nil {
		t.Fatal(err)
	}
	exam := consultations.MedicalExam{Code: "NFS-Y", Name: "NFS", Category: "Laboratoire", IsActive: true}
	if err := db.Create(&exam).Error; err != nil {
		t.Fatal(err)
	}
	cat := act_catalog.Entry{
		Code: "NFS-Y", Label: "NFS", Category: "LABORATORY", BasePrice: 1, Currency: "XOF",
		Billable: true, InsuranceEligible: true, IsActive: true, CreatedBy: 1, UpdatedBy: 1,
	}
	if err := db.Create(&cat).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&performed_acts.ProducerMap{
		SourceType: performed_acts.SourceLaboratory, ClinicalKey: "NFS-Y", ActCatalogEntryID: cat.ID,
		IsActive: true, CreatedBy: 1, UpdatedBy: 1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	o := Order{
		RequestNumber: "REQ-3", ConsultationID: c.ID, MedicalExamID: exam.ID, PatientID: p.ID,
		Priority: "ROUTINE", Status: StatusOrdered, CreatedBy: 1, UpdatedBy: 1,
	}
	if err := db.Create(&o).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewService(NewRepository(db)).WithPerformedActs(performed_acts.NewService(db))
	if _, err := svc.PrepareSample(o.ID, Access{UserID: 8, Permissions: map[string]bool{"*": true}}); err != nil {
		t.Fatal(err)
	}
	var n int64
	_ = db.Model(&performed_acts.Act{}).Count(&n)
	if n != 0 {
		t.Fatalf("n=%d", n)
	}
}
