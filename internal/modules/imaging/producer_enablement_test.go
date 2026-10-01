package imaging

import (
	"testing"

	"github.com/lallene/medcore-his/backend/internal/modules/consultations"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
	"github.com/lallene/medcore-his/backend/internal/modules/performed_acts"
)

func TestImagingStartDisabledNoPerformedAct(t *testing.T) {
	db := imagingProducerDB(t)
	p := patients.Patient{CodePatient: "ID1", NumeroDossier: "ID1", Nom: "I"}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	c := consultations.Consultation{PatientID: p.ID, DoctorName: "Dr", Service: "Radio", Status: "in_progress"}
	if err := db.Create(&c).Error; err != nil {
		t.Fatal(err)
	}
	exam := consultations.MedicalExam{Code: "XR-D", Name: "Radio", Category: "Imagerie", IsActive: true}
	if err := db.Create(&exam).Error; err != nil {
		t.Fatal(err)
	}
	o := Order{
		OrderNumber: "IMG-D1", AccessionNumber: "ACC-D1", ConsultationID: c.ID, MedicalExamID: exam.ID,
		PatientID: p.ID, Modality: "XR", Priority: "ROUTINE", Status: StatusOrdered, CreatedBy: 1, UpdatedBy: 1,
	}
	if err := db.Create(&o).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewService(NewRepository(db)) // producers disabled
	out, err := svc.Start(o.ID, Access{UserID: 9, Permissions: map[string]bool{"*": true}}, StartRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != StatusInProgress {
		t.Fatalf("status=%s", out.Status)
	}
	var n int64
	_ = db.Model(&performed_acts.Act{}).Count(&n)
	if n != 0 {
		t.Fatalf("n=%d", n)
	}
}
