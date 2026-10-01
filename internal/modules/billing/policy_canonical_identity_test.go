package billing

import (
	"fmt"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/insurance/authorization"
	"gorm.io/gorm"
)

func linkPA(t *testing.T, db *gorm.DB, patient, catalogID, sourceID uint, sourceType, code string) billingPerformedAct {
	t.Helper()
	sid := sourceID
	act := newBillingPerformedAct(patient, catalogID, code, true, true, 1, "PERFORMED", 1000)
	act.SourceType = sourceType
	act.SourceID = &sid
	if e := db.Create(&act).Error; e != nil {
		t.Fatal(e)
	}
	return act
}

func billableTypes(acts []BillableAct) map[string]int {
	m := map[string]int{}
	for _, a := range acts {
		m[fmt.Sprintf("%s:%d", a.ActType, a.ReferenceID)]++
	}
	return m
}

func TestPostgresCanonicalIdentity_Consultation(t *testing.T) {
	db := billingDB(t)
	s := NewService(db)
	p, _ := seedPatient(t, db, "CI-C")
	c := consultation(t, db, p, "Médecine")
	tariffLegacy := tariff(t, db, "CONSULTATION", "CONS-L", 20000)

	// K: without PA → legacy billable
	rows, e := s.BillableActs(p)
	if e != nil {
		t.Fatal(e)
	}
	keys := billableTypes(rows)
	if keys[fmt.Sprintf("CONSULTATION:%d", c)] != 1 {
		t.Fatalf("legacy consultation missing: %+v", keys)
	}
	inv, e := s.CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{
		{ActType: "CONSULTATION", ReferenceID: c, TariffID: tariffLegacy},
	}}, 1)
	if e != nil {
		t.Fatal(e)
	}
	if inv.GrossAmount != 20000 {
		t.Fatalf("legacy invoice=%+v", inv)
	}
	// Cancel so we can test re-billing rules with PA (S).
	if _, e = s.Cancel(inv.ID, "reset for PA test", 1); e != nil {
		t.Fatal(e)
	}

	// L: with linked PA → legacy suppressed, PA available
	act := linkPA(t, db, p, 100, c, "CONSULTATION", "PA-C")
	catalogID := act.ActCatalogEntryID
	tariffPA := tariffPerformed(t, db, &catalogID, "PA-C-T", 25000)
	rows, e = s.BillableActs(p)
	if e != nil {
		t.Fatal(e)
	}
	keys = billableTypes(rows)
	if keys[fmt.Sprintf("CONSULTATION:%d", c)] != 0 {
		t.Fatalf("legacy must be suppressed: %+v", keys)
	}
	if keys[fmt.Sprintf("%s:%d", authorization.ReferencePerformedAct, act.ID)] != 1 {
		t.Fatalf("PA must be billable: %+v", keys)
	}

	// Q: direct legacy CreateInvoice blocked
	if _, e = s.CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{
		{ActType: "CONSULTATION", ReferenceID: c, TariffID: tariffLegacy},
	}}, 1); !isConflict(e) {
		t.Fatalf("legacy CreateInvoice must conflict with linked PA: %v", e)
	}

	// R + S: PA CreateInvoice allowed; cancelled legacy does not re-enable legacy
	invPA, e := s.CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{
		{ActType: authorization.ReferencePerformedAct, ReferenceID: act.ID, TariffID: tariffPA},
	}}, 1)
	if e != nil {
		t.Fatal(e)
	}
	if invPA.GrossAmount != 25000 {
		t.Fatalf("PA invoice=%+v", invPA)
	}
	// T: historical legacy invoice row untouched
	var hist Invoice
	if e := db.First(&hist, inv.ID).Error; e != nil {
		t.Fatal(e)
	}
	if hist.GrossAmount != 20000 || len(hist.Number) == 0 {
		t.Fatalf("historical legacy mutated: %+v", hist)
	}
}

func TestPostgresCanonicalIdentity_LabAndImaging(t *testing.T) {
	db := billingDB(t)
	s := NewService(db)
	p, _ := seedPatient(t, db, "CI-LI")
	exam := billingExam{Name: "NFS"}
	if e := db.Create(&exam).Error; e != nil {
		t.Fatal(e)
	}
	lab := billingLabOrder{PatientID: p, MedicalExamID: exam.ID, RequestNumber: "LAB-1", Status: "VALIDATED", CreatedAt: time.Now()}
	if e := db.Create(&lab).Error; e != nil {
		t.Fatal(e)
	}
	img := billingImagingOrder{PatientID: p, MedicalExamID: exam.ID, OrderNumber: "IMG-1", Status: "PERFORMED", CreatedAt: time.Now()}
	if e := db.Create(&img).Error; e != nil {
		t.Fatal(e)
	}
	_ = tariff(t, db, "LABORATORY", "LAB-T", 5000)
	_ = tariff(t, db, "IMAGING", "IMG-T", 7000)

	// M / O: without PA → legacy billable
	rows, e := s.BillableActs(p)
	if e != nil {
		t.Fatal(e)
	}
	keys := billableTypes(rows)
	if keys[fmt.Sprintf("LABORATORY:%d", lab.ID)] != 1 || keys[fmt.Sprintf("IMAGING:%d", img.ID)] != 1 {
		t.Fatalf("legacy lab/imaging missing: %+v", keys)
	}

	// N / P: with PA → suppressed
	_ = linkPA(t, db, p, exam.ID, lab.ID, "LABORATORY", "PA-LAB")
	_ = linkPA(t, db, p, exam.ID, img.ID, "IMAGING", "PA-IMG")
	rows, e = s.BillableActs(p)
	if e != nil {
		t.Fatal(e)
	}
	keys = billableTypes(rows)
	if keys[fmt.Sprintf("LABORATORY:%d", lab.ID)] != 0 || keys[fmt.Sprintf("IMAGING:%d", img.ID)] != 0 {
		t.Fatalf("legacy lab/imaging must be suppressed: %+v", keys)
	}
	if _, e = s.CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{
		{ActType: "LABORATORY", ReferenceID: lab.ID, TariffID: tariff(t, db, "LABORATORY", "LAB-T2", 5000)},
	}}, 1); !isConflict(e) {
		t.Fatalf("lab CreateInvoice bypass: %v", e)
	}
	if _, e = s.CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{
		{ActType: "IMAGING", ReferenceID: img.ID, TariffID: tariff(t, db, "IMAGING", "IMG-T2", 7000)},
	}}, 1); !isConflict(e) {
		t.Fatalf("imaging CreateInvoice bypass: %v", e)
	}
}

func TestPostgresCanonicalIdentity_UnrelatedPADoesNotSuppress(t *testing.T) {
	db := billingDB(t)
	s := NewService(db)
	p, _ := seedPatient(t, db, "CI-U")
	c1 := consultation(t, db, p, "A")
	c2 := consultation(t, db, p, "B")
	_ = linkPA(t, db, p, 50, c1, "CONSULTATION", "PA-C1")
	// U/V: PA for c1 must not suppress c2; wrong source_type must not suppress
	wrongType := linkPA(t, db, p, 51, c2, "LABORATORY", "PA-WRONG") // source_id=c2 but LABORATORY
	_ = wrongType
	rows, e := s.BillableActs(p)
	if e != nil {
		t.Fatal(e)
	}
	keys := billableTypes(rows)
	if keys[fmt.Sprintf("CONSULTATION:%d", c1)] != 0 {
		t.Fatalf("c1 should be suppressed: %+v", keys)
	}
	if keys[fmt.Sprintf("CONSULTATION:%d", c2)] != 1 {
		t.Fatalf("c2 must remain billable (source_type mismatch): %+v", keys)
	}
}

func TestPostgresCanonicalIdentity_MedicationUnchanged(t *testing.T) {
	db := billingDB(t)
	s := NewService(db)
	p, _ := seedPatient(t, db, "CI-MED")
	med := billingMedication{Name: "Para"}
	db.Create(&med)
	pres := billingPresentation{MedicationID: med.ID, Dosage: "500mg"}
	db.Create(&pres)
	ref := uint(99)
	d := billingDispensation{
		PresentationID: pres.ID, Quantity: 2, Status: "COMPLETED",
		PatientID: &p, ReferenceID: &ref, CreatedAt: time.Now(),
	}
	if e := db.Create(&d).Error; e != nil {
		t.Fatal(e)
	}
	// Linked PA with source MEDICATION is unsupported — inventing none; medication stays billable.
	rows, e := s.BillableActs(p)
	if e != nil {
		t.Fatal(e)
	}
	keys := billableTypes(rows)
	if keys[fmt.Sprintf("MEDICATION:%d", d.ID)] != 1 && keys[fmt.Sprintf("MEDICATION_DISPENSATION:%d", d.ID)] != 1 {
		// BillableActs uses ActType MEDICATION with key MEDICATION_DISPENSATION
		found := false
		for _, a := range rows {
			if a.ActType == "MEDICATION" && a.ReferenceID == d.ID {
				found = true
			}
		}
		if !found {
			t.Fatalf("medication must remain billable: %+v keys=%+v", rows, keys)
		}
	}
	tariffID := tariff(t, db, "MEDICATION", "MED-T", 1500)
	// Reference tariff by presentation — UpdateTariff path uses presentation as tariff ref for meds
	refID := pres.ID
	tariffMed := Tariff{
		ActType: "MEDICATION", ReferenceID: &refID, Code: "MED-T2", Label: "MED-T2",
		UnitPrice: 1500, Currency: "XOF", EffectiveFrom: time.Now().Add(-time.Hour), IsActive: true, CreatedBy: 1, UpdatedBy: 1,
	}
	if e := db.Create(&tariffMed).Error; e != nil {
		t.Fatal(e)
	}
	_ = tariffID
	inv, e := s.CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{
		{ActType: "MEDICATION", ReferenceID: d.ID, TariffID: tariffMed.ID},
	}}, 1)
	if e != nil {
		t.Fatal(e)
	}
	if inv.GrossAmount != 3000 {
		t.Fatalf("medication qty*price: %+v", inv)
	}
}
