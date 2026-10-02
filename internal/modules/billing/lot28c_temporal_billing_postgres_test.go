package billing

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/insurance/authorization"
	"gorm.io/gorm"
)

func seedLabOrder(t *testing.T, db *gorm.DB, patient uint, status string) uint {
	t.Helper()
	exam := billingExam{Name: "NFS"}
	if e := db.Create(&exam).Error; e != nil {
		t.Fatal(e)
	}
	lab := billingLabOrder{
		PatientID: patient, MedicalExamID: exam.ID,
		RequestNumber: fmt.Sprintf("LAB-%d", time.Now().UnixNano()),
		Status:        status, CreatedAt: time.Now(),
	}
	if e := db.Create(&lab).Error; e != nil {
		t.Fatal(e)
	}
	return lab.ID
}

func linkPAForSource(t *testing.T, db *gorm.DB, patient, catalogID, sourceID uint, sourceType, code string) billingPerformedAct {
	t.Helper()
	return linkPA(t, db, patient, catalogID, sourceID, sourceType, code)
}

// LOT28C B01–B14 temporal cross-identity billing.
func TestPostgresLOT28CTemporalCrossIdentityBilling(t *testing.T) {
	db := billingDB(t)
	s := NewService(db)
	p, _ := seedPatient(t, db, "L28C")
	labID := seedLabOrder(t, db, p, "VALIDATED")
	legacyTariff := tariff(t, db, "LABORATORY", "L28C-LAB-T", 8000)

	// B01: no PA → legacy path works
	inv, e := s.CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{
		{ActType: "LABORATORY", ReferenceID: labID, TariffID: legacyTariff},
	}}, 1)
	if e != nil {
		t.Fatalf("B01: %v", e)
	}
	if inv.Status != InvoiceDraft {
		t.Fatalf("B01 status=%s", inv.Status)
	}
	grossBefore := inv.GrossAmount

	// B04 path uses DRAFT: PA then PA invoice rejected
	act := linkPAForSource(t, db, p, 200, labID, "LABORATORY", "PA-L28C")
	catalogID := act.ActCatalogEntryID
	paTariff := tariffPerformed(t, db, &catalogID, "PA-L28C-T", 9000)

	rows, e := s.BillableActs(p)
	if e != nil {
		t.Fatal(e)
	}
	keys := billableTypes(rows)
	if keys[fmt.Sprintf("LABORATORY:%d", labID)] != 0 {
		t.Fatalf("B02/B08: legacy must stay suppressed with PA: %+v", keys)
	}
	if keys[fmt.Sprintf("%s:%d", authorization.ReferencePerformedAct, act.ID)] != 0 {
		t.Fatalf("B04 ListBillable: PA must not appear while DRAFT legacy active: %+v", keys)
	}

	// B03: direct legacy rejected
	if _, e = s.CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{
		{ActType: "LABORATORY", ReferenceID: labID, TariffID: legacyTariff},
	}}, 1); !isConflict(e) {
		t.Fatalf("B03 want conflict got %v", e)
	}

	// B04: PA CreateInvoice rejected while DRAFT legacy active
	if _, e = s.CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{
		{ActType: authorization.ReferencePerformedAct, ReferenceID: act.ID, TariffID: paTariff},
	}}, 1); !isConflict(e) {
		t.Fatalf("B04 want conflict got %v", e)
	}

	// B12: historical DRAFT unchanged
	var hist Invoice
	if e := db.First(&hist, inv.ID).Error; e != nil {
		t.Fatal(e)
	}
	if hist.GrossAmount != grossBefore || hist.Status != InvoiceDraft {
		t.Fatalf("B12 mutated: %+v", hist)
	}

	// Issue → B05
	if _, e = s.Issue(inv.ID, 1); e != nil {
		t.Fatal(e)
	}
	if _, e = s.CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{
		{ActType: authorization.ReferencePerformedAct, ReferenceID: act.ID, TariffID: paTariff},
	}}, 1); !isConflict(e) {
		t.Fatalf("B05 want conflict got %v", e)
	}

	// Cancel → B07 PA billing allowed; B08 legacy still suppressed
	if _, e = s.Cancel(inv.ID, "lot28c reset", 1); e != nil {
		t.Fatal(e)
	}
	rows, e = s.BillableActs(p)
	if e != nil {
		t.Fatal(e)
	}
	keys = billableTypes(rows)
	if keys[fmt.Sprintf("LABORATORY:%d", labID)] != 0 {
		t.Fatalf("B08 legacy resurrected: %+v", keys)
	}
	if keys[fmt.Sprintf("%s:%d", authorization.ReferencePerformedAct, act.ID)] != 1 {
		t.Fatalf("B07 PA candidate missing: %+v", keys)
	}
	invPA, e := s.CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{
		{ActType: authorization.ReferencePerformedAct, ReferenceID: act.ID, TariffID: paTariff},
	}}, 1)
	if e != nil {
		t.Fatalf("B07: %v", e)
	}
	if invPA.GrossAmount != 9000 {
		t.Fatalf("B07 amount=%d", invPA.GrossAmount)
	}
}

func TestPostgresLOT28CLegacyPaidBlocksPABilling(t *testing.T) {
	db := billingDB(t)
	s := NewService(db)
	p, _ := seedPatient(t, db, "L28C-PAID")
	labID := seedLabOrder(t, db, p, "VALIDATED")
	legacyTariff := tariff(t, db, "LABORATORY", "L28C-PAID-T", 5000)
	inv, e := s.CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{
		{ActType: "LABORATORY", ReferenceID: labID, TariffID: legacyTariff},
	}}, 1)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Issue(inv.ID, 1); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Pay(inv.ID, PaymentRequest{Amount: inv.PatientAmount, PaymentMethod: "CASH", IdempotencyKey: "l28c-paid"}, 1); e != nil {
		t.Fatal(e)
	}
	act := linkPAForSource(t, db, p, 201, labID, "LABORATORY", "PA-PAID")
	catalogID := act.ActCatalogEntryID
	paTariff := tariffPerformed(t, db, &catalogID, "PA-PAID-T", 6000)
	if _, e = s.CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{
		{ActType: authorization.ReferencePerformedAct, ReferenceID: act.ID, TariffID: paTariff},
	}}, 1); !isConflict(e) {
		t.Fatalf("B06 want conflict got %v", e)
	}
	var status string
	if e := db.Raw(`SELECT status FROM billing_invoices WHERE id=?`, inv.ID).Scan(&status).Error; e != nil {
		t.Fatal(e)
	}
	if status != InvoicePaid {
		t.Fatalf("B12 paid mutated to %s", status)
	}
}

func TestPostgresLOT28CPAFirstSuppressesLegacy(t *testing.T) {
	db := billingDB(t)
	s := NewService(db)
	p, _ := seedPatient(t, db, "L28C-PA1")
	labID := seedLabOrder(t, db, p, "ORDERED")
	_ = linkPAForSource(t, db, p, 202, labID, "LABORATORY", "PA-FIRST")
	legacyTariff := tariff(t, db, "LABORATORY", "L28C-FIRST-T", 4000)
	if _, e := s.CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{
		{ActType: "LABORATORY", ReferenceID: labID, TariffID: legacyTariff},
	}}, 1); !isConflict(e) {
		t.Fatalf("B03/F want conflict got %v", e)
	}
	rows, e := s.BillableActs(p)
	if e != nil {
		t.Fatal(e)
	}
	keys := billableTypes(rows)
	if keys[fmt.Sprintf("LABORATORY:%d", labID)] != 0 {
		t.Fatalf("B02 legacy visible: %+v", keys)
	}
}

func TestPostgresLOT28CLegacyVsPACreateInvoiceRace(t *testing.T) {
	db := billingDB(t)
	s := NewService(db)
	p, _ := seedPatient(t, db, "L28C-RACE")
	labID := seedLabOrder(t, db, p, "VALIDATED")
	act := linkPAForSource(t, db, p, 203, labID, "LABORATORY", "PA-RACE")
	catalogID := act.ActCatalogEntryID
	legacyTariff := tariff(t, db, "LABORATORY", "L28C-RACE-L", 7000)
	paTariff := tariffPerformed(t, db, &catalogID, "L28C-RACE-P", 7500)

	var wg sync.WaitGroup
	type res struct {
		path string
		err  error
		id   uint
	}
	out := make(chan res, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		inv, err := s.CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{
			{ActType: "LABORATORY", ReferenceID: labID, TariffID: legacyTariff},
		}}, 1)
		id := uint(0)
		if inv != nil {
			id = inv.ID
		}
		out <- res{path: "legacy", err: err, id: id}
	}()
	go func() {
		defer wg.Done()
		inv, err := s.CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{
			{ActType: authorization.ReferencePerformedAct, ReferenceID: act.ID, TariffID: paTariff},
		}}, 1)
		id := uint(0)
		if inv != nil {
			id = inv.ID
		}
		out <- res{path: "pa", err: err, id: id}
	}()
	wg.Wait()
	close(out)

	var ok []res
	for r := range out {
		if r.err == nil {
			ok = append(ok, r)
			continue
		}
		if !isConflict(r.err) {
			t.Fatalf("%s unexpected: %v", r.path, r.err)
		}
	}
	if len(ok) != 1 {
		t.Fatalf("B10/B11 want exactly one financial winner, got %#v", ok)
	}

	var active int64
	if e := db.Model(&InvoiceLine{}).Where("is_active = ?", true).Count(&active).Error; e != nil {
		t.Fatal(e)
	}
	if active != 1 {
		t.Fatalf("B11 active lines=%d", active)
	}
}

func TestPostgresLOT28CLegacyInvoiceVsProducerSerialize(t *testing.T) {
	db := billingDB(t)
	s := NewService(db)
	p, _ := seedPatient(t, db, "L28C-PROD")
	labID := seedLabOrder(t, db, p, "RESULT_ENTERED")
	legacyTariff := tariff(t, db, "LABORATORY", "L28C-PROD-T", 3000)

	// Simulate producer after legacy invoice under shared clinical lock.
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, err := s.CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{
			{ActType: "LABORATORY", ReferenceID: labID, TariffID: legacyTariff},
		}}, 1)
		errs <- err
	}()
	go func() {
		defer wg.Done()
		err := db.Transaction(func(tx *gorm.DB) error {
			if err := s.lockClinicalSource(tx, "LABORATORY", labID); err != nil {
				return err
			}
			// clinical PA create (producer-equivalent) after lock
			sid := labID
			act := newBillingPerformedAct(p, 300, "PA-PROD", true, true, 1, "PERFORMED", 1000)
			act.SourceType = "LABORATORY"
			act.SourceID = &sid
			return tx.Create(&act).Error
		})
		errs <- err
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil && !isConflict(err) {
			t.Fatalf("B09 unexpected: %v", err)
		}
	}

	var pa billingPerformedAct
	if e := db.Where("source_type=? AND source_id=?", "LABORATORY", labID).First(&pa).Error; e != nil {
		t.Fatalf("B09 PA should exist clinically: %v", e)
	}
	catalogID := pa.ActCatalogEntryID
	paTariff := tariffPerformed(t, db, &catalogID, "L28C-PROD-PA", 3500)

	var activeLegacy int64
	_ = db.Model(&InvoiceLine{}).Where("billable_key=? AND is_active", fmt.Sprintf("LABORATORY:%d", labID)).Count(&activeLegacy)
	if activeLegacy > 0 {
		if _, e := s.CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{
			{ActType: authorization.ReferencePerformedAct, ReferenceID: pa.ID, TariffID: paTariff},
		}}, 1); !isConflict(e) {
			t.Fatalf("B09 PA billable despite legacy: %v", e)
		}
	}
}

func TestPostgresLOT28CMedicationHospitalizationUnchanged(t *testing.T) {
	db := billingDB(t)
	s := NewService(db)
	p, _ := seedPatient(t, db, "L28C-MH")

	med := billingMedication{Name: "Para28C"}
	db.Create(&med)
	pres := billingPresentation{MedicationID: med.ID, Dosage: "500mg"}
	db.Create(&pres)
	ref := uint(1)
	d := billingDispensation{
		PresentationID: pres.ID, Quantity: 1, Status: "COMPLETED",
		PatientID: &p, ReferenceID: &ref, CreatedAt: time.Now(),
	}
	if e := db.Create(&d).Error; e != nil {
		t.Fatal(e)
	}
	tariffMed := Tariff{
		ActType: "MEDICATION", ReferenceID: &pres.ID, Code: "MED-28C", Label: "MED-28C",
		UnitPrice: 1000, Currency: "XOF", EffectiveFrom: time.Now().Add(-time.Hour), IsActive: true, CreatedBy: 1, UpdatedBy: 1,
	}
	if e := db.Create(&tariffMed).Error; e != nil {
		t.Fatal(e)
	}
	if _, e := s.CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{
		{ActType: "MEDICATION", ReferenceID: d.ID, TariffID: tariffMed.ID},
	}}, 1); e != nil {
		t.Fatalf("B13 medication: %v", e)
	}

	h := billingHospitalization{
		PatientID: p, AdmissionNumber: "H-28C", Department: "Med", Status: "ADMITTED", CreatedAt: time.Now(),
	}
	if e := db.Create(&h).Error; e != nil {
		t.Fatal(e)
	}
	ht := tariff(t, db, "HOSPITALIZATION", "HOSP-28C", 20000)
	if _, e := s.CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{
		{ActType: "HOSPITALIZATION", ReferenceID: h.ID, TariffID: ht},
	}}, 1); e != nil {
		t.Fatalf("B14 hospitalization: %v", e)
	}
}
