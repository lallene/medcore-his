package consultations

import (
	"testing"

	"github.com/lallene/medcore-his/backend/internal/modules/act_catalog"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
	"github.com/lallene/medcore-his/backend/internal/modules/performed_acts"
)

func TestConsultationCompletionWithoutProducerWhenDisabled(t *testing.T) {
	db := consultProducerDB(t)
	p := patients.Patient{CodePatient: "DIS1", NumeroDossier: "DIS1", Nom: "D"}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	c := Consultation{PatientID: p.ID, DoctorName: "Dr", Service: "Med", Status: ConsultationStatusInProgress}
	if err := db.Create(&c).Error; err != nil {
		t.Fatal(err)
	}
	// No WithPerformedActs → producers disabled path.
	svc := NewService(NewRepository(db), nil)
	out, err := svc.UpdateStatus(c.ID, UpdateConsultationStatusRequest{ExpectedVersion: 1, Status: ConsultationStatusCompleted}, 1, Access{
		UserID: 1, Permissions: map[string]bool{"*": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != ConsultationStatusCompleted {
		t.Fatalf("status=%s", out.Status)
	}
	var n int64
	_ = db.Model(&performed_acts.Act{}).Count(&n)
	if n != 0 {
		t.Fatalf("unexpected performed acts n=%d", n)
	}
}

func TestConsultationCompletionEnabledMissingMapRollsBack(t *testing.T) {
	db := consultProducerDB(t)
	p := patients.Patient{CodePatient: "EN1", NumeroDossier: "EN1", Nom: "E"}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	c := Consultation{PatientID: p.ID, DoctorName: "Dr", Service: "Med", Status: ConsultationStatusInProgress}
	if err := db.Create(&c).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewService(NewRepository(db), nil).WithPerformedActs(performed_acts.NewService(db))
	_, err := svc.UpdateStatus(c.ID, UpdateConsultationStatusRequest{ExpectedVersion: 1, Status: ConsultationStatusCompleted}, 1, Access{
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

func TestConsultationCompletionEnabledHappyPath(t *testing.T) {
	db := consultProducerDB(t)
	p := patients.Patient{CodePatient: "EN2", NumeroDossier: "EN2", Nom: "E"}
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
	c := Consultation{PatientID: p.ID, DoctorName: "Dr", Service: "Med", Status: ConsultationStatusInProgress}
	if err := db.Create(&c).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewService(NewRepository(db), nil).WithPerformedActs(performed_acts.NewService(db))
	if _, err := svc.UpdateStatus(c.ID, UpdateConsultationStatusRequest{ExpectedVersion: 1, Status: ConsultationStatusCompleted}, 5, Access{
		UserID: 5, Permissions: map[string]bool{"*": true},
	}); err != nil {
		t.Fatal(err)
	}
	var n int64
	_ = db.Model(&performed_acts.Act{}).Count(&n)
	if n != 1 {
		t.Fatalf("n=%d", n)
	}
}
