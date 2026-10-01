package laboratory

import (
	"testing"

	"github.com/lallene/medcore-his/backend/internal/modules/consultations"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
	"github.com/lallene/medcore-his/backend/internal/modules/performed_acts"
)

func TestLabValidateDisabledNoPerformedAct(t *testing.T) {
	db := labProducerDB(t)
	p := patients.Patient{CodePatient: "LD1", NumeroDossier: "LD1", Nom: "L"}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	c := consultations.Consultation{PatientID: p.ID, DoctorName: "Dr", Service: "Lab", Status: "in_progress"}
	if err := db.Create(&c).Error; err != nil {
		t.Fatal(err)
	}
	exam := consultations.MedicalExam{Code: "NFS-D", Name: "NFS", Category: "Laboratoire", IsActive: true}
	if err := db.Create(&exam).Error; err != nil {
		t.Fatal(err)
	}
	o := Order{
		RequestNumber: "REQ-D1", ConsultationID: c.ID, MedicalExamID: exam.ID, PatientID: p.ID,
		Priority: "ROUTINE", Status: StatusResultEntered, CreatedBy: 1, UpdatedBy: 1,
	}
	if err := db.Create(&o).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&Result{OrderID: o.ID, Parameter: "Hb", Value: "13", Flag: "NORMAL", EnteredBy: 1}).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewService(NewRepository(db)) // producers disabled
	out, err := svc.Validate(o.ID, Access{UserID: 8, Permissions: map[string]bool{"*": true}})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != StatusValidated {
		t.Fatalf("status=%s", out.Status)
	}
	var n int64
	_ = db.Model(&performed_acts.Act{}).Count(&n)
	if n != 0 {
		t.Fatalf("n=%d", n)
	}
}
