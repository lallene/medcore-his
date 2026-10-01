package consultations

import (
	"testing"

	"github.com/lallene/medcore-his/backend/internal/modules/act_catalog"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
	"github.com/lallene/medcore-his/backend/internal/modules/performed_acts"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func consultProducerDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:consult_prod_"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&patients.Patient{},
		&Consultation{},
		&ConsultationVitals{},
		&ConsultationReason{},
		&MedicalExam{},
		&ConsultationPrescription{},
		&ConsultationAntecedent{},
		&ConsultationPhysicalExam{},
		&PhysicalExamArea{},
		&ConsultationAdministeredTreatment{},
		&ConsultationPreviousMedication{},
		&ConsultationSurgicalHistory{},
		&ConsultationGynecoObstetricHistory{},
		&ConsultationSOAP{},
		&ConsultationSpecialtyData{},
		&act_catalog.Entry{},
		&performed_acts.Act{},
		&performed_acts.ProducerMap{},
	); err != nil {
		t.Fatal(err)
	}
	_ = db.Exec(`CREATE TABLE IF NOT EXISTS patient_queue_tickets (id INTEGER PRIMARY KEY, consultation_id INTEGER, appointment_id INTEGER)`)
	return db
}

func TestConsultationCompletionCreatesPerformedAct(t *testing.T) {
	db := consultProducerDB(t)
	p := patients.Patient{CodePatient: "CP1", NumeroDossier: "CD1", Nom: "C"}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	cat := act_catalog.Entry{
		Code: "CONSULTATION", Label: "Consultation", Category: "CONSULTATION",
		BasePrice: 8000, Currency: "XOF", Billable: true, InsuranceEligible: true, IsActive: true,
		CreatedBy: 1, UpdatedBy: 1,
	}
	if err := db.Create(&cat).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&performed_acts.ProducerMap{
		SourceType: performed_acts.SourceConsultation, ClinicalKey: performed_acts.ConsultationClinicalKey,
		ActCatalogEntryID: cat.ID, IsActive: true, CreatedBy: 1, UpdatedBy: 1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	c := Consultation{PatientID: p.ID, DoctorName: "Dr", Service: "Med", Status: ConsultationStatusInProgress}
	if err := db.Create(&c).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewService(NewRepository(db), nil).WithPerformedActs(performed_acts.NewService(db))
	out, err := svc.UpdateStatus(c.ID, UpdateConsultationStatusRequest{Status: ConsultationStatusCompleted}, 12, Access{
		UserID: 12, Permissions: map[string]bool{"*": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != ConsultationStatusCompleted {
		t.Fatalf("status=%s", out.Status)
	}
	var act performed_acts.Act
	if err := db.Where("source_type=? AND source_id=?", performed_acts.SourceConsultation, c.ID).First(&act).Error; err != nil {
		t.Fatal(err)
	}
	if act.ActCode != "CONSULTATION" || act.BasePrice != 8000 || act.PerformedBy != 12 {
		t.Fatalf("act=%+v", act)
	}
}

func TestConsultationCompletionMissingMapRollsBack(t *testing.T) {
	db := consultProducerDB(t)
	p := patients.Patient{CodePatient: "CP2", NumeroDossier: "CD2", Nom: "C"}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	c := Consultation{PatientID: p.ID, DoctorName: "Dr", Service: "Med", Status: ConsultationStatusInProgress}
	if err := db.Create(&c).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewService(NewRepository(db), nil).WithPerformedActs(performed_acts.NewService(db))
	_, err := svc.UpdateStatus(c.ID, UpdateConsultationStatusRequest{Status: ConsultationStatusCompleted}, 1, Access{
		UserID: 1, Permissions: map[string]bool{"*": true},
	})
	if err == nil {
		t.Fatal("expected missing map failure")
	}
	var status string
	_ = db.Raw(`SELECT status FROM consultations WHERE id=?`, c.ID).Scan(&status)
	if status != ConsultationStatusInProgress {
		t.Fatalf("status=%s", status)
	}
}

func TestConsultationInProgressDoesNotCreatePerformedAct(t *testing.T) {
	db := consultProducerDB(t)
	p := patients.Patient{CodePatient: "CP3", NumeroDossier: "CD3", Nom: "C"}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	cat := act_catalog.Entry{
		Code: "CONSULTATION", Label: "Consultation", Category: "CONSULTATION",
		BasePrice: 8000, Currency: "XOF", Billable: true, InsuranceEligible: true, IsActive: true,
		CreatedBy: 1, UpdatedBy: 1,
	}
	if err := db.Create(&cat).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&performed_acts.ProducerMap{
		SourceType: performed_acts.SourceConsultation, ClinicalKey: "",
		ActCatalogEntryID: cat.ID, IsActive: true, CreatedBy: 1, UpdatedBy: 1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	c := Consultation{PatientID: p.ID, DoctorName: "Dr", Service: "Med", Status: ConsultationStatusDraft}
	if err := db.Create(&c).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewService(NewRepository(db), nil).WithPerformedActs(performed_acts.NewService(db))
	if _, err := svc.UpdateStatus(c.ID, UpdateConsultationStatusRequest{Status: ConsultationStatusInProgress}, 1, Access{
		UserID: 1, Permissions: map[string]bool{"*": true},
	}); err != nil {
		t.Fatal(err)
	}
	var n int64
	_ = db.Model(&performed_acts.Act{}).Count(&n)
	if n != 0 {
		t.Fatalf("n=%d", n)
	}
}
