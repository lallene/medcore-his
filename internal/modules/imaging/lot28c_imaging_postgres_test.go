package imaging

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/modules/act_catalog"
	"github.com/lallene/medcore-his/backend/internal/modules/billing"
	"github.com/lallene/medcore-his/backend/internal/modules/consultations"
	"github.com/lallene/medcore-his/backend/internal/modules/medical_records"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
	"github.com/lallene/medcore-his/backend/internal/modules/performed_acts"
	"gorm.io/gorm"
)

func imagingProducerPG(t *testing.T) *gorm.DB {
	t.Helper()
	db := imagingDB(t)
	if err := db.AutoMigrate(&act_catalog.Entry{}, &performed_acts.Act{}, &performed_acts.ProducerMap{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func seedImagingWithProducer(t *testing.T, db *gorm.DB) (*Service, uint, uint) {
	t.Helper()
	suffix := time.Now().UnixNano()
	p := patients.Patient{CodePatient: fmt.Sprintf("L28C-I-%d", suffix%100000), NumeroDossier: fmt.Sprintf("L28C-D-%d", suffix%100000), Nom: "Img"}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	mr := medical_records.MedicalRecord{PatientID: p.ID, RecordNumber: fmt.Sprintf("L28C-MR-%d", suffix)}
	if err := db.Create(&mr).Error; err != nil {
		t.Fatal(err)
	}
	c := consultations.Consultation{PatientID: p.ID, DoctorName: "Dr", Service: "Radio", Status: "in_progress"}
	if err := db.Create(&c).Error; err != nil {
		t.Fatal(err)
	}
	examCode := fmt.Sprintf("XR-%d", suffix%1000000)
	exam := consultations.MedicalExam{Code: examCode, Name: "Radio", Category: "Imagerie", IsActive: true}
	if err := db.Create(&exam).Error; err != nil {
		t.Fatal(err)
	}
	cat := act_catalog.Entry{
		Code: examCode, Label: "Radio", Category: "IMAGING", BasePrice: 10000, Currency: "XOF",
		Billable: true, InsuranceEligible: true, IsActive: true, CreatedBy: 1, UpdatedBy: 1,
	}
	if err := db.Create(&cat).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&performed_acts.ProducerMap{
		SourceType: performed_acts.SourceImaging, ClinicalKey: examCode, ActCatalogEntryID: cat.ID,
		IsActive: true, CreatedBy: 1, UpdatedBy: 1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	o := Order{
		OrderNumber: fmt.Sprintf("IMG-%d", suffix), AccessionNumber: fmt.Sprintf("ACC-%d", suffix),
		ConsultationID: c.ID, MedicalExamID: exam.ID,
		PatientID: p.ID, MedicalRecordID: &mr.ID, Modality: "XR", Priority: "ROUTINE", Status: StatusOrdered,
		CreatedBy: 1, UpdatedBy: 1,
	}
	if err := db.Create(&o).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewService(NewRepository(db)).WithPerformedActs(performed_acts.NewService(db))
	return svc, o.ID, p.ID
}

func TestPostgresLOT28CImagingStartProducerAndCloseReport(t *testing.T) {
	db := imagingProducerPG(t)
	svc, id, patientID := seedImagingWithProducer(t, db)
	a := testAccess(42)

	// I01 pre-Start cancel → no PA
	svc2, id2, _ := seedImagingWithProducer(t, db)
	if _, err := svc2.Cancel(id2, a, "pre-start"); err != nil {
		t.Fatal(err)
	}
	var n int64
	db.Model(&performed_acts.Act{}).Where("source_type=? AND source_id=?", performed_acts.SourceImaging, id2).Count(&n)
	if n != 0 {
		t.Fatalf("I01 PA count=%d", n)
	}

	// I02/I03 Start → evidence + PA
	out, err := svc.Start(id, a, StartRequest{TechnicalNotes: "ok"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != StatusInProgress || out.PerformedAt == nil || out.PerformedBy == nil {
		t.Fatalf("I02 status/evidence %#v", out)
	}
	var act performed_acts.Act
	if err := db.Where("source_type=? AND source_id=?", performed_acts.SourceImaging, id).First(&act).Error; err != nil {
		t.Fatalf("I03: %v", err)
	}
	if act.PatientID != patientID {
		t.Fatalf("I15 patient mismatch")
	}

	// I06 retry Start → conflict, no second PA
	if _, err := svc.Start(id, a, StartRequest{}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("I06 want invalid transition got %v", err)
	}
	db.Model(&performed_acts.Act{}).Where("source_type=? AND source_id=?", performed_acts.SourceImaging, id).Count(&n)
	if n != 1 {
		t.Fatalf("I06 PA count=%d", n)
	}

	// I08 report draft → no extra PA
	if _, err := svc.SaveReport(id, a, ReportRequest{Findings: "f", Conclusion: "c"}); err != nil {
		t.Fatal(err)
	}
	db.Model(&performed_acts.Act{}).Where("source_type=? AND source_id=?", performed_acts.SourceImaging, id).Count(&n)
	if n != 1 {
		t.Fatalf("I08 PA count=%d", n)
	}

	// I10–I12 close report preserves performance + PA, no void
	closed, err := svc.CloseReport(id, a, "patient left")
	if err != nil {
		t.Fatal(err)
	}
	if closed.Status != StatusReportClosed || closed.PerformedAt == nil {
		t.Fatalf("I10 %#v", closed)
	}
	if err := db.First(&act, act.ID).Error; err != nil {
		t.Fatal(err)
	}
	if act.Status != performed_acts.StatusPerformed {
		t.Fatalf("I11/I12 PA status=%s", act.Status)
	}

	// Idempotent close
	if _, err := svc.CloseReport(id, a, "again"); err != nil {
		t.Fatalf("idempotent close: %v", err)
	}
}

func TestPostgresLOT28CImagingProducersDisabled(t *testing.T) {
	db := imagingProducerPG(t)
	_, id, _ := seedImagingWithProducer(t, db)
	svc := NewService(NewRepository(db)) // no WithPerformedActs
	out, err := svc.Start(id, testAccess(1), StartRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != StatusInProgress {
		t.Fatal(out.Status)
	}
	var n int64
	db.Model(&performed_acts.Act{}).Where("source_type=? AND source_id=?", performed_acts.SourceImaging, id).Count(&n)
	if n != 0 {
		t.Fatalf("I04 PA=%d", n)
	}
}

func TestPostgresLOT28CImagingMissingMapRollsBack(t *testing.T) {
	db := imagingProducerPG(t)
	svc, id, _ := seedImagingWithProducer(t, db)
	_ = db.Where("1=1").Delete(&performed_acts.ProducerMap{})
	_, err := svc.Start(id, testAccess(1), StartRequest{})
	if err == nil {
		t.Fatal("I05 expected fail")
	}
	var o Order
	_ = db.First(&o, id)
	if o.Status != StatusOrdered || o.PerformedAt != nil {
		t.Fatalf("I05 rolled back? %#v", o)
	}
}

func TestPostgresLOT28CConcurrentStartOnePA(t *testing.T) {
	db := imagingProducerPG(t)
	svc, id, _ := seedImagingWithProducer(t, db)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			_, err := svc.Start(id, testAccess(7), StartRequest{})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	var ok, fail int
	for err := range errs {
		if err == nil {
			ok++
		} else if errors.Is(err, ErrInvalidTransition) {
			fail++
		} else {
			t.Fatalf("I07 unexpected %v", err)
		}
	}
	if ok != 1 || fail != 1 {
		t.Fatalf("I07 ok=%d fail=%d", ok, fail)
	}
	var n int64
	db.Model(&performed_acts.Act{}).Where("source_type=? AND source_id=?", performed_acts.SourceImaging, id).Count(&n)
	if n != 1 {
		t.Fatalf("I07 PA=%d", n)
	}
}

func TestPostgresLOT28CStartVsCancelRace(t *testing.T) {
	db := imagingProducerPG(t)
	svc, id, _ := seedImagingWithProducer(t, db)
	var wg sync.WaitGroup
	type res struct {
		kind string
		err  error
	}
	out := make(chan res, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, err := svc.Start(id, testAccess(1), StartRequest{})
		out <- res{"start", err}
	}()
	go func() {
		defer wg.Done()
		_, err := svc.Cancel(id, testAccess(1), "race")
		out <- res{"cancel", err}
	}()
	wg.Wait()
	close(out)
	var winners []string
	for r := range out {
		if r.err == nil {
			winners = append(winners, r.kind)
		} else if !errors.Is(r.err, ErrInvalidTransition) {
			t.Fatalf("%s: %v", r.kind, r.err)
		}
	}
	if len(winners) != 1 {
		t.Fatalf("I13 winners=%v", winners)
	}
	var o Order
	_ = db.First(&o, id)
	var n int64
	db.Model(&performed_acts.Act{}).Where("source_type=? AND source_id=?", performed_acts.SourceImaging, id).Count(&n)
	switch winners[0] {
	case "start":
		if o.Status != StatusInProgress || o.PerformedAt == nil || n != 1 {
			t.Fatalf("start won incoherent %#v pa=%d", o, n)
		}
	case "cancel":
		if o.Status != StatusCancelled || o.PerformedAt != nil || n != 0 {
			t.Fatalf("cancel won incoherent %#v pa=%d", o, n)
		}
	}
}

func TestPostgresLOT28CReportValidateNoExtraPA(t *testing.T) {
	db := imagingProducerPG(t)
	svc, id, _ := seedImagingWithProducer(t, db)
	if _, err := svc.Start(id, testAccess(1), StartRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveReport(id, testAccess(1), ReportRequest{Findings: "f", Conclusion: "c"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Validate(id, testAccess(1)); err != nil {
		t.Fatal(err)
	}
	var n int64
	db.Model(&performed_acts.Act{}).Where("source_type=? AND source_id=?", performed_acts.SourceImaging, id).Count(&n)
	if n != 1 {
		t.Fatalf("I09 PA=%d", n)
	}
}

// I14: Start (PA producer) vs legacy CreateInvoice must serialize on imaging source.
func TestPostgresLOT28CStartVsLegacyInvoiceRace(t *testing.T) {
	db := imagingProducerPG(t)
	if err := db.AutoMigrate(&billing.Tariff{}, &billing.Invoice{}, &billing.InvoiceLine{}, &billing.AuthorizationAllocation{}, &billing.Payment{}, &billing.PaymentReversal{}); err != nil {
		t.Fatal(err)
	}
	_ = db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS ux_billing_active_billable_key ON billing_invoice_lines (billable_key) WHERE is_active=true")

	svc, id, patientID := seedImagingWithProducer(t, db)
	var examID uint
	if err := db.Model(&Order{}).Select("medical_exam_id").Where("id=?", id).Scan(&examID).Error; err != nil {
		t.Fatal(err)
	}
	tariff := billing.Tariff{
		ActType: "IMAGING", ReferenceID: &examID, Code: fmt.Sprintf("IMG-L28C-%d", id),
		Label: "Img", UnitPrice: 12000, Currency: "XOF",
		EffectiveFrom: time.Now().Add(-time.Hour), IsActive: true, CreatedBy: 1, UpdatedBy: 1,
	}
	if err := db.Create(&tariff).Error; err != nil {
		t.Fatal(err)
	}
	bill := billing.NewService(db)

	var wg sync.WaitGroup
	type res struct {
		kind string
		err  error
	}
	out := make(chan res, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, err := svc.Start(id, testAccess(1), StartRequest{})
		out <- res{"start", err}
	}()
	go func() {
		defer wg.Done()
		_, err := bill.CreateInvoice(billing.CreateInvoiceRequest{PatientID: patientID, Lines: []billing.InvoiceLineRequest{
			{ActType: "IMAGING", ReferenceID: id, TariffID: tariff.ID},
		}}, 1)
		out <- res{"legacy", err}
	}()
	wg.Wait()
	close(out)

	var startOK, legacyOK bool
	for r := range out {
		switch r.kind {
		case "start":
			if r.err == nil {
				startOK = true
			} else if !errors.Is(r.err, ErrInvalidTransition) {
				// legacy-first: Start still succeeds clinically (PA after legacy is allowed)
				t.Fatalf("start: %v", r.err)
			}
		case "legacy":
			if r.err == nil {
				legacyOK = true
			} else if !billingConflict(r.err) {
				t.Fatalf("legacy: %v", r.err)
			}
		}
	}
	if !startOK {
		t.Fatal("I14 Start must succeed clinically")
	}
	var nPA int64
	db.Model(&performed_acts.Act{}).Where("source_type=? AND source_id=? AND status=?", performed_acts.SourceImaging, id, performed_acts.StatusPerformed).Count(&nPA)
	if nPA != 1 {
		t.Fatalf("I14 PA count=%d", nPA)
	}
	var activeLegacy int64
	_ = db.Table("billing_invoice_lines").Where("billable_key=? AND is_active", fmt.Sprintf("IMAGING:%d", id)).Count(&activeLegacy)

	if legacyOK {
		if activeLegacy != 1 {
			t.Fatalf("I14 legacy won but active=%d", activeLegacy)
		}
		// PA must not be independently invoiceable
		var act performed_acts.Act
		if err := db.Where("source_type=? AND source_id=?", performed_acts.SourceImaging, id).First(&act).Error; err != nil {
			t.Fatal(err)
		}
		paTariff := billing.Tariff{
			ActType: "PERFORMED_ACT", ReferenceID: &act.ActCatalogEntryID, Code: fmt.Sprintf("PA-IMG-%d", id),
			Label: "PA", UnitPrice: 13000, Currency: "XOF",
			EffectiveFrom: time.Now().Add(-time.Hour), IsActive: true, CreatedBy: 1, UpdatedBy: 1,
		}
		if err := db.Create(&paTariff).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := bill.CreateInvoice(billing.CreateInvoiceRequest{PatientID: patientID, Lines: []billing.InvoiceLineRequest{
			{ActType: "PERFORMED_ACT", ReferenceID: act.ID, TariffID: paTariff.ID},
		}}, 1); !billingConflict(err) {
			t.Fatalf("I14 PA billable despite legacy: %v", err)
		}
	} else {
		// PA-first: legacy rejected, no active legacy line
		if activeLegacy != 0 {
			t.Fatalf("I14 PA-first but legacy active=%d", activeLegacy)
		}
	}
}

func billingConflict(err error) bool {
	var app *coreerrors.AppError
	return err != nil && errors.As(err, &app) && app.Status == 409
}
