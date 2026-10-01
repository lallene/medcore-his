package billing

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/insurance/authorization"
	"gorm.io/gorm"
)

func newBillingPerformedAct(patient, catalogID uint, code string, billable, eligible bool, qty float64, status string, basePrice int64) billingPerformedAct {
	return billingPerformedAct{
		PatientID: patient, ActCatalogEntryID: catalogID, ActCode: code, ActLabel: code, ActCategory: "OTHER",
		BasePrice: basePrice, Currency: "XOF", Billable: billable, InsuranceEligible: eligible, Quantity: qty,
		PerformedAt: time.Now(), PerformedBy: 1, Status: status, CreatedBy: 1, UpdatedBy: 1,
	}
}

func tariffPerformed(t *testing.T, db *gorm.DB, catalogID *uint, code string, price int64) uint {
	t.Helper()
	x := Tariff{
		ActType: authorization.ReferencePerformedAct, ReferenceID: catalogID, Code: code, Label: code,
		UnitPrice: price, Currency: "XOF", EffectiveFrom: time.Now().Add(-time.Hour), IsActive: true, CreatedBy: 7, UpdatedBy: 7,
	}
	if e := db.Create(&x).Error; e != nil {
		t.Fatal(e)
	}
	return x.ID
}

func seedPerformedActAuth(t *testing.T, db *gorm.DB, patient, record, actID, covID, companyID, guarantorID uint, status string, rate *float64, insurance float64) uint {
	t.Helper()
	requested := insurance
	if requested == 0 {
		requested = 1
	}
	auth := authorization.InsuranceAuthorization{
		AuthorizationNumber: fmt.Sprintf("PEC-PA-%d", actID), PatientID: patient, MedicalRecordID: record,
		PatientCoverageID: covID, InsuranceCompanyID: companyID, GuarantorID: guarantorID,
		ReferenceType: authorization.ReferencePerformedAct, ReferenceID: actID,
		RequestedAmount: &requested, RequestedAt: time.Now(), RequestedBy: 1, Status: status,
		ApprovedRate: rate, InsuranceAmount: &insurance, PatientAmount: ptr(0), CreatedBy: 1, UpdatedBy: 1,
	}
	if e := db.Create(&auth).Error; e != nil {
		t.Fatal(e)
	}
	return auth.ID
}

func ensureActiveCoverage(t *testing.T, db *gorm.DB, patient uint) (covID, companyID, guarantorID uint) {
	t.Helper()
	co := billingCompany{Name: "AssureurPA"}
	db.Create(&co)
	g := billingGuarantor{Name: "GarantPA"}
	db.Create(&g)
	cov := billingCoverage{PatientID: patient, CompanyID: co.ID, GuarantorID: g.ID, MemberNumber: "PA-MEM", CoverageRate: 80, IsActive: true}
	if e := db.Create(&cov).Error; e != nil {
		t.Fatal(e)
	}
	return cov.ID, co.ID, g.ID
}

func TestPostgresPerformedActSelfPayUsesTariffNotBasePrice(t *testing.T) {
	db := billingDB(t)
	p, _ := seedPatient(t, db, "PA-SELF")
	act := newBillingPerformedAct(p, 42, "CONS-PA", true, true, 2, "PERFORMED", 999999)
	if e := db.Create(&act).Error; e != nil {
		t.Fatal(e)
	}
	catalogID := act.ActCatalogEntryID
	tariffID := tariffPerformed(t, db, &catalogID, "PA-TARIFF", 15000)
	s := NewService(db)
	inv, e := s.CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{
		{ActType: authorization.ReferencePerformedAct, ReferenceID: act.ID, TariffID: tariffID},
	}}, 11)
	if e != nil {
		t.Fatal(e)
	}
	line := inv.Lines[0]
	if line.ActType != authorization.ReferencePerformedAct || line.ReferenceID != act.ID {
		t.Fatalf("refs=%+v", line)
	}
	if line.Quantity != 2 || line.UnitPrice != 15000 || line.GrossAmount != 30000 {
		t.Fatalf("pricing from tariff expected qty*15000; got qty=%v unit=%d gross=%d (basePrice=%d must not apply)", line.Quantity, line.UnitPrice, line.GrossAmount, act.BasePrice)
	}
	if inv.InsuranceAmount != 0 || inv.PatientAmount != 30000 || line.AuthorizationID != nil {
		t.Fatalf("self-pay=%+v line=%+v", inv, line)
	}
	if line.BillableKey != fmt.Sprintf("%s:%d", authorization.ReferencePerformedAct, act.ID) {
		t.Fatalf("billableKey=%q", line.BillableKey)
	}
}

func TestPostgresPerformedActInsuranceIneligibleStillSelfPay(t *testing.T) {
	db := billingDB(t)
	p, _ := seedPatient(t, db, "PA-INEL")
	act := newBillingPerformedAct(p, 7, "NO-PEC", true, false, 1, "PERFORMED", 5000)
	db.Create(&act)
	catalogID := act.ActCatalogEntryID
	tariffID := tariffPerformed(t, db, &catalogID, "PA-INEL-T", 8000)
	inv, e := NewService(db).CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{
		{ActType: authorization.ReferencePerformedAct, ReferenceID: act.ID, TariffID: tariffID},
	}}, 1)
	if e != nil {
		t.Fatal(e)
	}
	if inv.InsuranceAmount != 0 || inv.PatientAmount != 8000 {
		t.Fatalf("ineligible still self-pay: %+v", inv)
	}
}

func TestPostgresPerformedActApprovedPECAllocation(t *testing.T) {
	db := billingDB(t)
	p, m := seedPatient(t, db, "PA-APP")
	act := newBillingPerformedAct(p, 11, "SURG", true, true, 1, "PERFORMED", 1)
	db.Create(&act)
	covID, companyID, guarantorID := ensureActiveCoverage(t, db, p)
	rate := 70.0
	authID := seedPerformedActAuth(t, db, p, m, act.ID, covID, companyID, guarantorID, authorization.StatusApproved, &rate, 35000)
	catalogID := act.ActCatalogEntryID
	tariffID := tariffPerformed(t, db, &catalogID, "PA-APP-T", 50000)
	inv, e := NewService(db).CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{
		{ActType: authorization.ReferencePerformedAct, ReferenceID: act.ID, TariffID: tariffID},
	}}, 2)
	if e != nil {
		t.Fatal(e)
	}
	// min(round(50000*0.7)=35000, remaining 35000, gross 50000) = 35000
	if inv.InsuranceAmount != 35000 || inv.PatientAmount != 15000 {
		t.Fatalf("approved split=%+v", inv)
	}
	if inv.Lines[0].AuthorizationID == nil || *inv.Lines[0].AuthorizationID != authID {
		t.Fatalf("auth link=%+v", inv.Lines[0])
	}
	var alloc int64
	db.Model(&AuthorizationAllocation{}).Where("authorization_id=?", authID).Select("COALESCE(SUM(amount),0)").Scan(&alloc)
	if alloc != 35000 {
		t.Fatalf("allocation=%d", alloc)
	}
}

func TestPostgresPerformedActPartiallyApprovedPEC(t *testing.T) {
	db := billingDB(t)
	p, m := seedPatient(t, db, "PA-PART")
	act := newBillingPerformedAct(p, 12, "PART", true, true, 1, "PERFORMED", 1)
	db.Create(&act)
	covID, companyID, guarantorID := ensureActiveCoverage(t, db, p)
	rate := 80.0
	_ = seedPerformedActAuth(t, db, p, m, act.ID, covID, companyID, guarantorID, authorization.StatusPartiallyApproved, &rate, 20000)
	catalogID := act.ActCatalogEntryID
	tariffID := tariffPerformed(t, db, &catalogID, "PA-PART-T", 50000)
	inv, e := NewService(db).CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{
		{ActType: authorization.ReferencePerformedAct, ReferenceID: act.ID, TariffID: tariffID},
	}}, 2)
	if e != nil {
		t.Fatal(e)
	}
	// rate would give 40000 but cap remaining=20000
	if inv.InsuranceAmount != 20000 || inv.PatientAmount != 30000 {
		t.Fatalf("partial=%+v", inv)
	}
}

func TestPostgresPerformedActRejectedAndPendingPEC(t *testing.T) {
	t.Run("REJECTED", func(t *testing.T) {
		db := billingDB(t)
		p, m := seedPatient(t, db, "PA-REJ")
		act := newBillingPerformedAct(p, 13, "REJ", true, true, 1, "PERFORMED", 1)
		db.Create(&act)
		covID, companyID, guarantorID := ensureActiveCoverage(t, db, p)
		authID := seedPerformedActAuth(t, db, p, m, act.ID, covID, companyID, guarantorID, authorization.StatusRejected, nil, 0)
		catalogID := act.ActCatalogEntryID
		tariffID := tariffPerformed(t, db, &catalogID, "PA-REJ-T", 12000)
		inv, e := NewService(db).CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{
			{ActType: authorization.ReferencePerformedAct, ReferenceID: act.ID, TariffID: tariffID},
		}}, 2)
		if e != nil {
			t.Fatal(e)
		}
		if inv.InsuranceAmount != 0 || inv.PatientAmount != 12000 {
			t.Fatalf("rejected=%+v", inv)
		}
		// Existing contract links authorization even when insurance=0.
		if inv.Lines[0].AuthorizationID == nil || *inv.Lines[0].AuthorizationID != authID {
			t.Fatalf("rejected auth link=%+v", inv.Lines[0])
		}
		if inv.CoveragePending {
			t.Fatal("rejected must not be coverage-pending")
		}
	})
	t.Run("PENDING", func(t *testing.T) {
		db := billingDB(t)
		p, m := seedPatient(t, db, "PA-PEND")
		act := newBillingPerformedAct(p, 14, "PEND", true, true, 1, "PERFORMED", 1)
		db.Create(&act)
		covID, companyID, guarantorID := ensureActiveCoverage(t, db, p)
		_ = seedPerformedActAuth(t, db, p, m, act.ID, covID, companyID, guarantorID, authorization.StatusPending, nil, 0)
		catalogID := act.ActCatalogEntryID
		tariffID := tariffPerformed(t, db, &catalogID, "PA-PEND-T", 9000)
		inv, e := NewService(db).CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{
			{ActType: authorization.ReferencePerformedAct, ReferenceID: act.ID, TariffID: tariffID},
		}}, 2)
		if e != nil {
			t.Fatal(e)
		}
		if inv.InsuranceAmount != 0 || !inv.CoveragePending || !inv.Lines[0].CoveragePending {
			t.Fatalf("pending=%+v line=%+v", inv, inv.Lines[0])
		}
	})
}

func TestPostgresPerformedActInvalidAndTariffValidation(t *testing.T) {
	db := billingDB(t)
	p, _ := seedPatient(t, db, "PA-INV")
	other, _ := seedPatient(t, db, "PA-OTH")
	s := NewService(db)
	ok := newBillingPerformedAct(p, 20, "OK", true, true, 1, "PERFORMED", 100)
	db.Create(&ok)
	catalogID := ok.ActCatalogEntryID
	goodTariff := tariffPerformed(t, db, &catalogID, "PA-OK-T", 1000)

	if _, e := s.CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{
		{ActType: authorization.ReferencePerformedAct, ReferenceID: 999999, TariffID: goodTariff},
	}}, 1); !isConflict(e) {
		t.Fatalf("missing act=%v", e)
	}

	foreign := newBillingPerformedAct(other, 20, "FOREIGN", true, true, 1, "PERFORMED", 100)
	db.Create(&foreign)
	if _, e := s.CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{
		{ActType: authorization.ReferencePerformedAct, ReferenceID: foreign.ID, TariffID: goodTariff},
	}}, 1); !isConflict(e) {
		t.Fatalf("cross-patient=%v", e)
	}

	voided := newBillingPerformedAct(p, 20, "VOID", true, true, 1, "VOIDED", 100)
	db.Create(&voided)
	if _, e := s.CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{
		{ActType: authorization.ReferencePerformedAct, ReferenceID: voided.ID, TariffID: goodTariff},
	}}, 1); !isConflict(e) {
		t.Fatalf("voided=%v", e)
	}

	unbillable := newBillingPerformedAct(p, 20, "NB", false, true, 1, "PERFORMED", 100)
	db.Create(&unbillable)
	if e := db.Model(&unbillable).Update("billable", false).Error; e != nil {
		t.Fatal(e)
	}
	if _, e := s.CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{
		{ActType: authorization.ReferencePerformedAct, ReferenceID: unbillable.ID, TariffID: goodTariff},
	}}, 1); !isConflict(e) {
		t.Fatalf("billable=false=%v", e)
	}

	wrongType := tariff(t, db, "CONSULTATION", "WRONG-TYPE", 1000)
	if _, e := s.CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{
		{ActType: authorization.ReferencePerformedAct, ReferenceID: ok.ID, TariffID: wrongType},
	}}, 1); !isConflict(e) {
		t.Fatalf("wrong actType tariff=%v", e)
	}

	otherCatalog := uint(99)
	wrongRef := tariffPerformed(t, db, &otherCatalog, "PA-WRONG-REF", 1000)
	if _, e := s.CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{
		{ActType: authorization.ReferencePerformedAct, ReferenceID: ok.ID, TariffID: wrongRef},
	}}, 1); !isConflict(e) {
		t.Fatalf("wrong catalogue tariff ref=%v", e)
	}

	inactive := Tariff{
		ActType: authorization.ReferencePerformedAct, ReferenceID: &catalogID, Code: "PA-OFF", Label: "off",
		UnitPrice: 1000, Currency: "XOF", EffectiveFrom: time.Now().Add(-time.Hour), IsActive: true, CreatedBy: 1, UpdatedBy: 1,
	}
	db.Create(&inactive)
	if e := db.Model(&inactive).Update("is_active", false).Error; e != nil {
		t.Fatal(e)
	}
	if _, e := s.CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{
		{ActType: authorization.ReferencePerformedAct, ReferenceID: ok.ID, TariffID: inactive.ID},
	}}, 1); !isConflict(e) {
		t.Fatalf("inactive tariff=%v", e)
	}
}

func TestPostgresPerformedActDuplicateAndCancelRebill(t *testing.T) {
	db := billingDB(t)
	p, _ := seedPatient(t, db, "PA-DUP")
	act := newBillingPerformedAct(p, 30, "DUP", true, true, 1, "PERFORMED", 100)
	db.Create(&act)
	catalogID := act.ActCatalogEntryID
	tariffID := tariffPerformed(t, db, &catalogID, "PA-DUP-T", 4000)
	s := NewService(db)
	first, e := s.CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{
		{ActType: authorization.ReferencePerformedAct, ReferenceID: act.ID, TariffID: tariffID},
	}}, 1)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{
		{ActType: authorization.ReferencePerformedAct, ReferenceID: act.ID, TariffID: tariffID},
	}}, 1); !isConflict(e) {
		t.Fatalf("duplicate=%v", e)
	}
	if _, e = s.Cancel(first.ID, "annulation test", 2); e != nil {
		t.Fatal(e)
	}
	var active int64
	db.Model(&InvoiceLine{}).Where("billable_key=? AND is_active", fmt.Sprintf("%s:%d", authorization.ReferencePerformedAct, act.ID)).Count(&active)
	if active != 0 {
		t.Fatalf("active after cancel=%d", active)
	}
	second, e := s.CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{
		{ActType: authorization.ReferencePerformedAct, ReferenceID: act.ID, TariffID: tariffID},
	}}, 3)
	if e != nil {
		t.Fatal(e)
	}
	if second.ID == first.ID {
		t.Fatal("rebill must create new invoice")
	}
}

func TestPostgresPerformedActConcurrentDuplicateCreate(t *testing.T) {
	db := billingDB(t)
	p, _ := seedPatient(t, db, "PA-CONC")
	act := newBillingPerformedAct(p, 31, "CONC", true, true, 1, "PERFORMED", 100)
	db.Create(&act)
	catalogID := act.ActCatalogEntryID
	tariffID := tariffPerformed(t, db, &catalogID, "PA-CONC-T", 2500)
	s := NewService(db)
	const workers = 8
	start := make(chan struct{})
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, e := s.CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{
				{ActType: authorization.ReferencePerformedAct, ReferenceID: act.ID, TariffID: tariffID},
			}}, 9)
			errs <- e
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	ok, conflict := 0, 0
	for e := range errs {
		if e == nil {
			ok++
		} else if isConflict(e) {
			conflict++
		} else {
			t.Fatalf("unexpected=%v", e)
		}
	}
	if ok != 1 || conflict != workers-1 {
		t.Fatalf("concurrent ok=%d conflict=%d", ok, conflict)
	}
	var n int64
	db.Model(&InvoiceLine{}).Where("billable_key=? AND is_active", fmt.Sprintf("%s:%d", authorization.ReferencePerformedAct, act.ID)).Count(&n)
	if n != 1 {
		t.Fatalf("active lines=%d", n)
	}
}

func TestPostgresPerformedActBillableActsDiscovery(t *testing.T) {
	db := billingDB(t)
	p, _ := seedPatient(t, db, "PA-DISC")
	eligible := newBillingPerformedAct(p, 40, "DISC-OK", true, false, 3, "PERFORMED", 100)
	db.Create(&eligible)
	voided := newBillingPerformedAct(p, 41, "DISC-VOID", true, true, 1, "VOIDED", 100)
	db.Create(&voided)
	hidden := newBillingPerformedAct(p, 42, "DISC-NB", false, true, 1, "PERFORMED", 100)
	db.Create(&hidden)
	if e := db.Model(&hidden).Update("billable", false).Error; e != nil {
		t.Fatal(e)
	}
	rows, e := NewService(db).BillableActs(p)
	if e != nil {
		t.Fatal(e)
	}
	found := 0
	for _, r := range rows {
		if r.ActType != authorization.ReferencePerformedAct {
			continue
		}
		found++
		if r.ReferenceID != eligible.ID || r.Quantity != 3 || r.AlreadyBilled {
			t.Fatalf("discovery row=%+v", r)
		}
	}
	if found != 1 {
		t.Fatalf("expected 1 PERFORMED_ACT discoverable, got %d in %+v", found, rows)
	}
}

func TestPostgresPerformedActCreateDoesNotWritePayments(t *testing.T) {
	db := billingDB(t)
	p, _ := seedPatient(t, db, "PA-PAY")
	act := newBillingPerformedAct(p, 50, "PAY", true, true, 1, "PERFORMED", 100)
	db.Create(&act)
	catalogID := act.ActCatalogEntryID
	tariffID := tariffPerformed(t, db, &catalogID, "PA-PAY-T", 1000)
	if _, e := NewService(db).CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{
		{ActType: authorization.ReferencePerformedAct, ReferenceID: act.ID, TariffID: tariffID},
	}}, 1); e != nil {
		t.Fatal(e)
	}
	var payments int64
	db.Model(&Payment{}).Count(&payments)
	if payments != 0 {
		t.Fatalf("payments mutated=%d", payments)
	}
}
