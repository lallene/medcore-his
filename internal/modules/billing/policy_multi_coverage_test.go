package billing

import (
	"strings"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/insurance/authorization"
	"gorm.io/gorm"
)

func createDatedCoverage(t *testing.T, db *gorm.DB, patient uint, member string, validFrom time.Time) (covID, companyID, guarantorID uint) {
	t.Helper()
	co := billingCompany{Name: "Co-" + member}
	if e := db.Create(&co).Error; e != nil {
		t.Fatal(e)
	}
	g := billingGuarantor{Name: "G-" + member}
	if e := db.Create(&g).Error; e != nil {
		t.Fatal(e)
	}
	vf := validFrom
	cov := billingCoverage{
		PatientID: patient, CompanyID: co.ID, GuarantorID: g.ID,
		MemberNumber: member, CoverageRate: 80, IsActive: true, ValidFrom: &vf,
	}
	if e := db.Create(&cov).Error; e != nil {
		t.Fatal(e)
	}
	return cov.ID, co.ID, g.ID
}

func TestPostgresMultiCoverageAuthFirst_A_NoPECSelfPay(t *testing.T) {
	db := billingDB(t)
	p, _ := seedPatient(t, db, "MC-A")
	act := newBillingPerformedAct(p, 1, "MC-A", true, true, 1, "PERFORMED", 1)
	if e := db.Create(&act).Error; e != nil {
		t.Fatal(e)
	}
	// Newer coverage exists but must not be consulted without a PEC.
	_, _, _ = createDatedCoverage(t, db, p, "NEW", time.Now().Add(-time.Hour))
	catalogID := act.ActCatalogEntryID
	tariffID := tariffPerformed(t, db, &catalogID, "MC-A-T", 10000)
	inv, e := NewService(db).CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{
		{ActType: authorization.ReferencePerformedAct, ReferenceID: act.ID, TariffID: tariffID},
	}}, 1)
	if e != nil {
		t.Fatal(e)
	}
	if inv.InsuranceAmount != 0 || inv.PatientAmount != 10000 || inv.Lines[0].AuthorizationID != nil {
		t.Fatalf("self-pay expected: %+v", inv)
	}
}

func TestPostgresMultiCoverageAuthFirst_B_OneApprovedUsesPECCoverage(t *testing.T) {
	db := billingDB(t)
	p, m := seedPatient(t, db, "MC-B")
	act := newBillingPerformedAct(p, 2, "MC-B", true, true, 1, "PERFORMED", 1)
	db.Create(&act)
	covID, coID, gID := createDatedCoverage(t, db, p, "A", time.Now().Add(-48*time.Hour))
	rate := 80.0
	authID := seedPerformedActAuth(t, db, p, m, act.ID, covID, coID, gID, authorization.StatusApproved, &rate, 8000)
	catalogID := act.ActCatalogEntryID
	tariffID := tariffPerformed(t, db, &catalogID, "MC-B-T", 10000)
	inv, e := NewService(db).CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{
		{ActType: authorization.ReferencePerformedAct, ReferenceID: act.ID, TariffID: tariffID},
	}}, 1)
	if e != nil {
		t.Fatal(e)
	}
	if inv.InsuranceAmount != 8000 || inv.PatientAmount != 2000 {
		t.Fatalf("split=%+v", inv)
	}
	if inv.Lines[0].AuthorizationID == nil || *inv.Lines[0].AuthorizationID != authID {
		t.Fatalf("auth=%+v", inv.Lines[0])
	}
}

func TestPostgresMultiCoverageAuthFirst_C_NewerCoverageIgnoredWhenPECOnOlder(t *testing.T) {
	db := billingDB(t)
	p, m := seedPatient(t, db, "MC-C")
	act := newBillingPerformedAct(p, 3, "MC-C", true, true, 1, "PERFORMED", 1)
	db.Create(&act)
	covA, coA, gA := createDatedCoverage(t, db, p, "OLD-A", time.Now().Add(-72*time.Hour))
	_, _, _ = createDatedCoverage(t, db, p, "NEW-B", time.Now().Add(-time.Hour)) // newer — must not win
	rate := 50.0
	authID := seedPerformedActAuth(t, db, p, m, act.ID, covA, coA, gA, authorization.StatusApproved, &rate, 5000)
	catalogID := act.ActCatalogEntryID
	tariffID := tariffPerformed(t, db, &catalogID, "MC-C-T", 10000)
	inv, e := NewService(db).CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{
		{ActType: authorization.ReferencePerformedAct, ReferenceID: act.ID, TariffID: tariffID},
	}}, 1)
	if e != nil {
		t.Fatal(e)
	}
	if inv.InsuranceAmount != 5000 || *inv.Lines[0].AuthorizationID != authID {
		t.Fatalf("must use PEC on coverage A: %+v", inv)
	}
}

func TestPostgresMultiCoverageAuthFirst_D_TwoCoveragesOnePEC(t *testing.T) {
	db := billingDB(t)
	p, m := seedPatient(t, db, "MC-D")
	act := newBillingPerformedAct(p, 4, "MC-D", true, true, 1, "PERFORMED", 1)
	db.Create(&act)
	covA, coA, gA := createDatedCoverage(t, db, p, "D-A", time.Now().Add(-48*time.Hour))
	_, _, _ = createDatedCoverage(t, db, p, "D-B", time.Now().Add(-time.Hour))
	rate := 60.0
	authID := seedPerformedActAuth(t, db, p, m, act.ID, covA, coA, gA, authorization.StatusApproved, &rate, 6000)
	catalogID := act.ActCatalogEntryID
	tariffID := tariffPerformed(t, db, &catalogID, "MC-D-T", 10000)
	inv, e := NewService(db).CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{
		{ActType: authorization.ReferencePerformedAct, ReferenceID: act.ID, TariffID: tariffID},
	}}, 1)
	if e != nil {
		t.Fatal(e)
	}
	if *inv.Lines[0].AuthorizationID != authID || inv.InsuranceAmount != 6000 {
		t.Fatalf("PEC coverage A expected: %+v", inv)
	}
}

func TestPostgresMultiCoverageAuthFirst_E_TwoFinalPECsConflict(t *testing.T) {
	db := billingDB(t)
	p, m := seedPatient(t, db, "MC-E")
	act := newBillingPerformedAct(p, 5, "MC-E", true, true, 1, "PERFORMED", 1)
	db.Create(&act)
	covA, coA, gA := createDatedCoverage(t, db, p, "E-A", time.Now().Add(-48*time.Hour))
	covB, coB, gB := createDatedCoverage(t, db, p, "E-B", time.Now().Add(-time.Hour))
	rate := 80.0
	_ = seedPerformedActAuth(t, db, p, m, act.ID, covA, coA, gA, authorization.StatusApproved, &rate, 8000)
	authB := authorization.InsuranceAuthorization{
		AuthorizationNumber: "PEC-E-B", PatientID: p, MedicalRecordID: m,
		PatientCoverageID: covB, InsuranceCompanyID: coB, GuarantorID: gB,
		ReferenceType: authorization.ReferencePerformedAct, ReferenceID: act.ID,
		RequestedAmount: ptr(8000), RequestedAt: time.Now(), RequestedBy: 1,
		Status: authorization.StatusApproved, ApprovedRate: &rate, InsuranceAmount: ptr(8000),
		PatientAmount: ptr(0), CreatedBy: 1, UpdatedBy: 1,
	}
	if e := db.Create(&authB).Error; e != nil {
		t.Fatal(e)
	}
	catalogID := act.ActCatalogEntryID
	tariffID := tariffPerformed(t, db, &catalogID, "MC-E-T", 10000)
	_, e := NewService(db).CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{
		{ActType: authorization.ReferencePerformedAct, ReferenceID: act.ID, TariffID: tariffID},
	}}, 1)
	if e == nil || !isConflict(e) {
		t.Fatalf("expected ambiguous conflict, got %v", e)
	}
	if !strings.Contains(e.Error(), "autorisations") {
		t.Fatalf("unexpected conflict message: %v", e)
	}
}

func TestPostgresMultiCoverageAuthFirst_F_OpenPECPending(t *testing.T) {
	db := billingDB(t)
	p, m := seedPatient(t, db, "MC-F")
	act := newBillingPerformedAct(p, 6, "MC-F", true, true, 1, "PERFORMED", 1)
	db.Create(&act)
	covID, coID, gID := createDatedCoverage(t, db, p, "F", time.Now().Add(-time.Hour))
	_ = seedPerformedActAuth(t, db, p, m, act.ID, covID, coID, gID, authorization.StatusPending, nil, 0)
	catalogID := act.ActCatalogEntryID
	tariffID := tariffPerformed(t, db, &catalogID, "MC-F-T", 10000)
	inv, e := NewService(db).CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{
		{ActType: authorization.ReferencePerformedAct, ReferenceID: act.ID, TariffID: tariffID},
	}}, 1)
	if e != nil {
		t.Fatal(e)
	}
	if !inv.CoveragePending || inv.InsuranceAmount != 0 {
		t.Fatalf("pending expected: %+v", inv)
	}
	if _, e := NewService(db).Issue(inv.ID, 1); e == nil || !isConflict(e) {
		t.Fatalf("issue must block while pending: %v", e)
	}
}

func TestPostgresMultiCoverageAuthFirst_G_CancelledIgnored(t *testing.T) {
	db := billingDB(t)
	p, m := seedPatient(t, db, "MC-G")
	act := newBillingPerformedAct(p, 7, "MC-G", true, true, 1, "PERFORMED", 1)
	db.Create(&act)
	covID, coID, gID := createDatedCoverage(t, db, p, "G", time.Now().Add(-time.Hour))
	_ = seedPerformedActAuth(t, db, p, m, act.ID, covID, coID, gID, authorization.StatusCancelled, nil, 0)
	catalogID := act.ActCatalogEntryID
	tariffID := tariffPerformed(t, db, &catalogID, "MC-G-T", 9000)
	inv, e := NewService(db).CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{
		{ActType: authorization.ReferencePerformedAct, ReferenceID: act.ID, TariffID: tariffID},
	}}, 1)
	if e != nil {
		t.Fatal(e)
	}
	if inv.InsuranceAmount != 0 || inv.Lines[0].AuthorizationID != nil {
		t.Fatalf("cancelled must be ignored: %+v", inv)
	}
}

func TestPostgresMultiCoverageAuthFirst_H_RejectedNoAllocation(t *testing.T) {
	db := billingDB(t)
	p, m := seedPatient(t, db, "MC-H")
	act := newBillingPerformedAct(p, 8, "MC-H", true, true, 1, "PERFORMED", 1)
	db.Create(&act)
	covID, coID, gID := createDatedCoverage(t, db, p, "H", time.Now().Add(-time.Hour))
	authID := seedPerformedActAuth(t, db, p, m, act.ID, covID, coID, gID, authorization.StatusRejected, nil, 0)
	catalogID := act.ActCatalogEntryID
	tariffID := tariffPerformed(t, db, &catalogID, "MC-H-T", 11000)
	inv, e := NewService(db).CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{
		{ActType: authorization.ReferencePerformedAct, ReferenceID: act.ID, TariffID: tariffID},
	}}, 1)
	if e != nil {
		t.Fatal(e)
	}
	if inv.InsuranceAmount != 0 || inv.PatientAmount != 11000 {
		t.Fatalf("rejected self-pay: %+v", inv)
	}
	if inv.Lines[0].AuthorizationID == nil || *inv.Lines[0].AuthorizationID != authID {
		t.Fatalf("rejected auth still linked for history: %+v", inv.Lines[0])
	}
}

func TestPostgresMultiCoverageAuthFirst_I_CrossPatientPARejected(t *testing.T) {
	db := billingDB(t)
	p1, _ := seedPatient(t, db, "MC-I1")
	p2, _ := seedPatient(t, db, "MC-I2")
	act := newBillingPerformedAct(p1, 9, "MC-I", true, true, 1, "PERFORMED", 1)
	db.Create(&act)
	catalogID := act.ActCatalogEntryID
	tariffID := tariffPerformed(t, db, &catalogID, "MC-I-T", 5000)
	_, e := NewService(db).CreateInvoice(CreateInvoiceRequest{PatientID: p2, Lines: []InvoiceLineRequest{
		{ActType: authorization.ReferencePerformedAct, ReferenceID: act.ID, TariffID: tariffID},
	}}, 1)
	if e == nil || !isConflict(e) {
		t.Fatalf("cross-patient PA must conflict: %v", e)
	}
}
