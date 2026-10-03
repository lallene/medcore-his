package performed_acts

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/lallene/medcore-his/backend/internal/modules/act_catalog"
	"github.com/lallene/medcore-his/backend/internal/modules/billing"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/authorization"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/company"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/coverage"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/guarantor"
	"github.com/lallene/medcore-his/backend/internal/modules/medical_records"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type voidConsultStub struct {
	ID, PatientID uint
	ServiceID     *uint
	Service       string
}

func (voidConsultStub) TableName() string { return "consultations" }

type voidOrgServiceStub struct {
	ID   uint
	Name string
}

func (voidOrgServiceStub) TableName() string { return "organization_services" }

// voidReconDB is a multi-conn schema-isolated harness for LOT27H Void reconciliation.
func voidReconDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL absent: tests PostgreSQL Void reconciliation ignorés")
	}
	dsn = strings.Replace(dsn, "-pooler", "", 1)
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("void_recon_%d", time.Now().UnixNano())
	schemaIdent := pgx.Identifier{schema}.Sanitize()
	if err = admin.Exec("CREATE SCHEMA " + schemaIdent).Error; err != nil {
		t.Fatal(err)
	}
	pgConfig, err := pgx.ParseConfig(dsn)
	if err != nil {
		_ = admin.Exec("DROP SCHEMA IF EXISTS " + schemaIdent + " CASCADE")
		t.Fatal(err)
	}
	if pgConfig.RuntimeParams == nil {
		pgConfig.RuntimeParams = map[string]string{}
	}
	pgConfig.RuntimeParams["search_path"] = schemaIdent
	sqlDB := stdlib.OpenDB(*pgConfig, stdlib.OptionAfterConnect(func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET search_path TO "+schemaIdent)
		return err
	}))
	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetMaxIdleConns(10)
	pingCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {
		_ = sqlDB.Close()
		_ = admin.Exec("DROP SCHEMA IF EXISTS " + schemaIdent + " CASCADE")
		t.Fatal(err)
	}
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		_ = sqlDB.Close()
		_ = admin.Exec("DROP SCHEMA IF EXISTS " + schemaIdent + " CASCADE")
		t.Fatal(err)
	}
	adminSQL, err := admin.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
		_ = admin.Exec("DROP SCHEMA IF EXISTS " + schemaIdent + " CASCADE").Error
		_ = adminSQL.Close()
	})
	if err = db.AutoMigrate(
		&patients.Patient{}, &act_catalog.Entry{}, &Act{}, &ProducerMap{}, &voidConsultStub{}, &voidOrgServiceStub{},
		&company.InsuranceCompany{}, &guarantor.InsuranceGuarantor{}, &coverage.PatientCoverage{},
		&medical_records.MedicalRecord{}, &medical_records.MedicalTimelineEvent{},
		&authorization.InsuranceAuthorization{}, &authorization.InsuranceAuthorizationAct{},
		&billing.Tariff{}, &billing.Invoice{}, &billing.InvoiceLine{}, &billing.AuthorizationAllocation{}, &billing.Payment{}, &billing.PaymentReversal{}, &billing.CreditNote{},
	); err != nil {
		t.Fatal(err)
	}
	if err = db.Exec("CREATE UNIQUE INDEX ux_billing_active_billable_key ON billing_invoice_lines (billable_key) WHERE is_active=true").Error; err != nil {
		t.Fatal(err)
	}
	if err = authorization.EnsureAuthorizationIndexes(db); err != nil {
		t.Fatal(err)
	}
	return db
}

type voidFixture struct {
	patient   patients.Patient
	catalog   act_catalog.Entry
	record    medical_records.MedicalRecord
	coverage  coverage.PatientCoverage
	company   company.InsuranceCompany
	guarantor guarantor.InsuranceGuarantor
}

func seedVoidFixture(t *testing.T, db *gorm.DB, suffix string) voidFixture {
	t.Helper()
	p := patients.Patient{CodePatient: "VR-P-" + suffix, NumeroDossier: "VR-D-" + suffix, Nom: "Void", Prenoms: "Recon"}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	cat := act_catalog.Entry{
		Code: "VR-" + suffix, Label: "Void Recon", Category: "OTHER", BasePrice: 10000,
		Currency: "XOF", Billable: true, InsuranceEligible: true, IsActive: true, CreatedBy: 1, UpdatedBy: 1,
	}
	if err := db.Create(&cat).Error; err != nil {
		t.Fatal(err)
	}
	rec := medical_records.MedicalRecord{PatientID: p.ID, RecordNumber: "VR-MR-" + suffix}
	if err := db.Create(&rec).Error; err != nil {
		t.Fatal(err)
	}
	co := company.InsuranceCompany{Code: "VR-C-" + suffix, Name: "Assureur VR", IsActive: true}
	db.Create(&co)
	g := guarantor.InsuranceGuarantor{CompanyID: co.ID, Code: "VR-G-" + suffix, Name: "Garant VR", IsActive: true}
	db.Create(&g)
	cov := coverage.PatientCoverage{
		PatientID: p.ID, CompanyID: co.ID, GuarantorID: g.ID, MemberNumber: "VR-" + suffix,
		CoverageRate: 80, IsPrincipal: true, IsActive: true,
	}
	if err := db.Create(&cov).Error; err != nil {
		t.Fatal(err)
	}
	return voidFixture{p, cat, rec, cov, co, g}
}

func createPerformed(t *testing.T, db *gorm.DB, f voidFixture, sourceType string, sourceID *uint) *Act {
	t.Helper()
	if sourceType == "" && sourceID == nil {
		act, err := NewService(db).Create(CreateRequest{
			PatientID: f.patient.ID, ActCatalogEntryID: f.catalog.ID,
		}, 7)
		if err != nil {
			t.Fatal(err)
		}
		return act
	}
	// Producer identity is not settable via HTTP Create — insert ledger row directly for Void tests.
	act := Act{
		PatientID: f.patient.ID, ActCatalogEntryID: f.catalog.ID, Quantity: 1,
		PerformedAt: time.Now(), PerformedBy: 7, Status: StatusPerformed,
		SourceType: sourceType, SourceID: sourceID, CreatedBy: 7, UpdatedBy: 7,
	}
	applyCatalogSnapshot(&act, f.catalog)
	if err := db.Create(&act).Error; err != nil {
		t.Fatal(err)
	}
	return &act
}

func seedPAAuth(t *testing.T, db *gorm.DB, f voidFixture, actID uint, status string, rate *float64, insurance float64) uint {
	t.Helper()
	req := insurance
	if req == 0 {
		req = 1
	}
	auth := authorization.InsuranceAuthorization{
		AuthorizationNumber: fmt.Sprintf("PEC-VR-%d", actID), PatientID: f.patient.ID, MedicalRecordID: f.record.ID,
		PatientCoverageID: f.coverage.ID, InsuranceCompanyID: f.company.ID, GuarantorID: f.guarantor.ID,
		ReferenceType: authorization.ReferencePerformedAct, ReferenceID: actID,
		RequestedAmount: &req, RequestedAt: time.Now(), RequestedBy: 1, Status: status,
		ApprovedRate: rate, InsuranceAmount: &insurance, PatientAmount: floatPtr(0), CreatedBy: 1, UpdatedBy: 1,
	}
	if status == authorization.StatusApproved || status == authorization.StatusPartiallyApproved || status == authorization.StatusRejected {
		now := time.Now()
		auth.ExternalReference = "EXT-VR"
		auth.ExternalDecisionDate = &now
		auth.DecidedBy = uintPtr(9)
		if status == authorization.StatusRejected {
			auth.ApprovedRate = nil
			auth.InsuranceAmount = floatPtr(0)
			auth.PatientAmount = &req
			auth.RejectionReason = "refus"
		}
	}
	if err := db.Create(&auth).Error; err != nil {
		t.Fatal(err)
	}
	return auth.ID
}

func floatPtr(v float64) *float64 { return &v }
func uintPtr(v uint) *uint        { return &v }

func isConflictErr(err error) bool {
	return authorization.IsConflict(err)
}

func tariffPA(t *testing.T, db *gorm.DB, catalogID uint, code string, price int64) uint {
	t.Helper()
	ref := catalogID
	x := billing.Tariff{
		ActType: authorization.ReferencePerformedAct, ReferenceID: &ref, Code: code, Label: code,
		UnitPrice: price, Currency: "XOF", EffectiveFrom: time.Now().Add(-time.Hour), IsActive: true, CreatedBy: 1, UpdatedBy: 1,
	}
	if err := db.Create(&x).Error; err != nil {
		t.Fatal(err)
	}
	return x.ID
}

func TestPostgresVoidNoDependency(t *testing.T) {
	db := voidReconDB(t)
	f := seedVoidFixture(t, db, "NODEP")
	act := createPerformed(t, db, f, "", nil)
	voided, err := NewService(db).Void(act.ID, VoidRequest{Reason: "saisie erronée"}, 11)
	if err != nil {
		t.Fatal(err)
	}
	if voided.Status != StatusVoided || voided.VoidedAt == nil || voided.VoidedBy == nil || *voided.VoidedBy != 11 || voided.VoidReason == "" {
		t.Fatalf("voided=%+v", voided)
	}
	if _, err := NewService(db).Void(act.ID, VoidRequest{Reason: "again"}, 12); !isConflictErr(err) {
		t.Fatalf("repeat void=%v", err)
	}
}

func TestPostgresVoidThenPECCreateRejected(t *testing.T) {
	db := voidReconDB(t)
	f := seedVoidFixture(t, db, "THENPEC")
	act := createPerformed(t, db, f, "", nil)
	if _, err := NewService(db).Void(act.ID, VoidRequest{Reason: "annule"}, 1); err != nil {
		t.Fatal(err)
	}
	amount := 10000.0
	_, err := authorization.NewService(db).Create(authorization.CreateRequest{
		PatientID: f.patient.ID, PatientCoverageID: f.coverage.ID,
		ReferenceType: authorization.ReferencePerformedAct, ReferenceID: act.ID, RequestedAmount: &amount,
	}, 2)
	if !isConflictErr(err) {
		t.Fatalf("pec after void=%v", err)
	}
	var n int64
	db.Model(&authorization.InsuranceAuthorization{}).Where("reference_type=? AND reference_id=?", authorization.ReferencePerformedAct, act.ID).Count(&n)
	if n != 0 {
		t.Fatalf("auth rows=%d", n)
	}
}

func TestPostgresVoidOpenPECMatrix(t *testing.T) {
	for _, status := range []string{authorization.StatusDraft, authorization.StatusSubmitted, authorization.StatusPending} {
		status := status
		t.Run(status, func(t *testing.T) {
			db := voidReconDB(t)
			f := seedVoidFixture(t, db, status)
			act := createPerformed(t, db, f, "", nil)
			authID := seedPAAuth(t, db, f, act.ID, status, nil, 0)
			if _, err := NewService(db).Void(act.ID, VoidRequest{Reason: "open pec"}, 3); err != nil {
				t.Fatal(err)
			}
			var auth authorization.InsuranceAuthorization
			db.First(&auth, authID)
			if auth.Status != authorization.StatusCancelled {
				t.Fatalf("auth status=%s", auth.Status)
			}
			var pa Act
			db.First(&pa, act.ID)
			if pa.Status != StatusVoided {
				t.Fatalf("pa=%s", pa.Status)
			}
			var cancelEvents int64
			db.Model(&medical_records.MedicalTimelineEvent{}).
				Where("event_type=? AND reference_id=?", "insurance_authorization_cancelled", authID).
				Count(&cancelEvents)
			if cancelEvents != 1 {
				t.Fatalf("cancel timeline=%d", cancelEvents)
			}
		})
	}
}

func TestPostgresVoidFinalPECPreserved(t *testing.T) {
	rate := 70.0
	cases := []struct {
		status    string
		insurance float64
	}{
		{authorization.StatusApproved, 7000},
		{authorization.StatusPartiallyApproved, 5000},
		{authorization.StatusRejected, 0},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.status, func(t *testing.T) {
			db := voidReconDB(t)
			f := seedVoidFixture(t, db, tc.status)
			act := createPerformed(t, db, f, "", nil)
			var r *float64
			if tc.status != authorization.StatusRejected {
				r = &rate
			}
			authID := seedPAAuth(t, db, f, act.ID, tc.status, r, tc.insurance)
			var before authorization.InsuranceAuthorization
			db.First(&before, authID)
			if _, err := NewService(db).Void(act.ID, VoidRequest{Reason: "final preserved"}, 4); err != nil {
				t.Fatal(err)
			}
			var after authorization.InsuranceAuthorization
			db.First(&after, authID)
			if after.Status != before.Status || after.ExternalReference != before.ExternalReference {
				t.Fatalf("mutated status/ref before=%+v after=%+v", before, after)
			}
			if (before.ApprovedRate == nil) != (after.ApprovedRate == nil) {
				t.Fatal("approved rate mutated")
			}
			if before.ApprovedRate != nil && after.ApprovedRate != nil && *before.ApprovedRate != *after.ApprovedRate {
				t.Fatal("approved rate value mutated")
			}
			if (before.InsuranceAmount == nil) != (after.InsuranceAmount == nil) {
				t.Fatal("insurance amount mutated")
			}
			if before.InsuranceAmount != nil && after.InsuranceAmount != nil && *before.InsuranceAmount != *after.InsuranceAmount {
				t.Fatal("insurance amount value mutated")
			}
			var cancelEvents int64
			db.Model(&medical_records.MedicalTimelineEvent{}).
				Where("event_type=? AND reference_id=?", "insurance_authorization_cancelled", authID).
				Count(&cancelEvents)
			if cancelEvents != 0 {
				t.Fatalf("unexpected cancel timeline=%d", cancelEvents)
			}
			var pa Act
			db.First(&pa, act.ID)
			if pa.Status != StatusVoided {
				t.Fatalf("pa=%s", pa.Status)
			}
		})
	}
}

func TestPostgresVoidAlreadyCancelledPEC(t *testing.T) {
	db := voidReconDB(t)
	f := seedVoidFixture(t, db, "CXL")
	act := createPerformed(t, db, f, "", nil)
	authID := seedPAAuth(t, db, f, act.ID, authorization.StatusCancelled, nil, 0)
	var beforeEvents int64
	db.Model(&medical_records.MedicalTimelineEvent{}).
		Where("event_type=? AND reference_id=?", "insurance_authorization_cancelled", authID).
		Count(&beforeEvents)
	if _, err := NewService(db).Void(act.ID, VoidRequest{Reason: "already cancelled"}, 5); err != nil {
		t.Fatal(err)
	}
	var auth authorization.InsuranceAuthorization
	db.First(&auth, authID)
	if auth.Status != authorization.StatusCancelled {
		t.Fatalf("auth=%s", auth.Status)
	}
	var afterEvents int64
	db.Model(&medical_records.MedicalTimelineEvent{}).
		Where("event_type=? AND reference_id=?", "insurance_authorization_cancelled", authID).
		Count(&afterEvents)
	if afterEvents != beforeEvents {
		t.Fatalf("duplicate cancel side effects before=%d after=%d", beforeEvents, afterEvents)
	}
}

func TestPostgresVoidBlockedByActiveInvoice(t *testing.T) {
	db := voidReconDB(t)
	f := seedVoidFixture(t, db, "ACTIVEINV")
	act := createPerformed(t, db, f, "", nil)
	tariffID := tariffPA(t, db, f.catalog.ID, "VR-ACTIVE", 12000)
	inv, err := billing.NewService(db).CreateInvoice(billing.CreateInvoiceRequest{
		PatientID: f.patient.ID,
		Lines:     []billing.InvoiceLineRequest{{ActType: authorization.ReferencePerformedAct, ReferenceID: act.ID, TariffID: tariffID}},
	}, 8)
	if err != nil {
		t.Fatal(err)
	}
	authID := seedPAAuth(t, db, f, act.ID, authorization.StatusDraft, nil, 0)
	var allocBefore int64
	db.Model(&billing.AuthorizationAllocation{}).Count(&allocBefore)
	if _, err := NewService(db).Void(act.ID, VoidRequest{Reason: "blocked"}, 9); !isConflictErr(err) {
		t.Fatalf("void=%v", err)
	}
	var pa Act
	db.First(&pa, act.ID)
	if pa.Status != StatusPerformed {
		t.Fatalf("pa mutated=%s", pa.Status)
	}
	var auth authorization.InsuranceAuthorization
	db.First(&auth, authID)
	if auth.Status != authorization.StatusDraft {
		t.Fatalf("auth mutated=%s", auth.Status)
	}
	var line billing.InvoiceLine
	db.Where("invoice_id=?", inv.ID).First(&line)
	if !line.IsActive {
		t.Fatal("line deactivated")
	}
	var allocAfter int64
	db.Model(&billing.AuthorizationAllocation{}).Count(&allocAfter)
	if allocAfter != allocBefore {
		t.Fatalf("allocations changed %d→%d", allocBefore, allocAfter)
	}
}

func TestPostgresVoidAfterCancelledInvoice(t *testing.T) {
	db := voidReconDB(t)
	f := seedVoidFixture(t, db, "CXLINV")
	act := createPerformed(t, db, f, "", nil)
	tariffID := tariffPA(t, db, f.catalog.ID, "VR-CXL-INV", 9000)
	bill := billing.NewService(db)
	inv, err := bill.CreateInvoice(billing.CreateInvoiceRequest{
		PatientID: f.patient.ID,
		Lines:     []billing.InvoiceLineRequest{{ActType: authorization.ReferencePerformedAct, ReferenceID: act.ID, TariffID: tariffID}},
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bill.Cancel(inv.ID, "erreur facture", 2); err != nil {
		t.Fatal(err)
	}
	var active int64
	db.Model(&billing.InvoiceLine{}).Where("billable_key=? AND is_active", fmt.Sprintf("%s:%d", authorization.ReferencePerformedAct, act.ID)).Count(&active)
	if active != 0 {
		t.Fatalf("active lines=%d", active)
	}
	var alloc int64
	db.Model(&billing.AuthorizationAllocation{}).Count(&alloc)
	if alloc != 0 {
		t.Fatalf("allocations=%d", alloc)
	}
	if _, err := NewService(db).Void(act.ID, VoidRequest{Reason: "after cancel"}, 3); err != nil {
		t.Fatal(err)
	}
	var lines int64
	db.Model(&billing.InvoiceLine{}).Where("invoice_id=?", inv.ID).Count(&lines)
	if lines == 0 {
		t.Fatal("history deleted")
	}
	var pa Act
	db.First(&pa, act.ID)
	if pa.Status != StatusVoided {
		t.Fatalf("pa=%s", pa.Status)
	}
}

func TestPostgresVoidBlockedByPaidInvoice(t *testing.T) {
	db := voidReconDB(t)
	f := seedVoidFixture(t, db, "PAID")
	act := createPerformed(t, db, f, "", nil)
	tariffID := tariffPA(t, db, f.catalog.ID, "VR-PAID", 5000)
	bill := billing.NewService(db)
	inv, err := bill.CreateInvoice(billing.CreateInvoiceRequest{
		PatientID: f.patient.ID,
		Lines:     []billing.InvoiceLineRequest{{ActType: authorization.ReferencePerformedAct, ReferenceID: act.ID, TariffID: tariffID}},
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bill.Issue(inv.ID, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := bill.Pay(inv.ID, billing.PaymentRequest{Amount: 5000, PaymentMethod: "CASH", IdempotencyKey: "vr-pay-1"}, 3); err != nil {
		t.Fatal(err)
	}
	var payBefore int64
	db.Model(&billing.Payment{}).Count(&payBefore)
	if _, err := NewService(db).Void(act.ID, VoidRequest{Reason: "paid block"}, 4); !isConflictErr(err) {
		t.Fatalf("void=%v", err)
	}
	var pa Act
	db.First(&pa, act.ID)
	if pa.Status != StatusPerformed {
		t.Fatalf("pa=%s", pa.Status)
	}
	var payAfter int64
	db.Model(&billing.Payment{}).Count(&payAfter)
	if payAfter != payBefore {
		t.Fatalf("payments mutated %d→%d", payBefore, payAfter)
	}
}

// LOT29F-B: closes audit evidence gap — sessionless pay → reverse → cancel → void.
// CreditNote is intentionally NOT required for this sequence.
func TestPostgresSessionlessReverseCancelVoidSequence(t *testing.T) {
	db := voidReconDB(t)
	f := seedVoidFixture(t, db, "RCVSEQ")
	act := createPerformed(t, db, f, "", nil)
	tariffID := tariffPA(t, db, f.catalog.ID, "VR-RCV", 8000)
	bill := billing.NewService(db)
	inv, err := bill.CreateInvoice(billing.CreateInvoiceRequest{
		PatientID: f.patient.ID,
		Lines:     []billing.InvoiceLineRequest{{ActType: authorization.ReferencePerformedAct, ReferenceID: act.ID, TariffID: tariffID}},
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	issued, err := bill.Issue(inv.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	paid, err := bill.Pay(issued.ID, billing.PaymentRequest{
		Amount: issued.BalanceAmount, PaymentMethod: "CASH", IdempotencyKey: "rcv-seq-pay",
	}, 3)
	if err != nil || len(paid.Payments) != 1 {
		t.Fatalf("pay %+v %v", paid, err)
	}
	if _, err := NewService(db).Void(act.ID, VoidRequest{Reason: "still billed"}, 4); !isConflictErr(err) {
		t.Fatalf("void while paid want conflict got %v", err)
	}
	restored, err := bill.ReversePayment(paid.Payments[0].ID, billing.ReversePaymentRequest{
		Reason: "Erreur de saisie sequence", IdempotencyKey: "rcv-seq-rev",
	}, 5)
	if err != nil || restored.Status != billing.InvoiceIssued || restored.PaidAmount != 0 {
		t.Fatalf("reverse %+v %v", restored, err)
	}
	cancelled, err := bill.Cancel(issued.ID, "Après contrepassation", 6)
	if err != nil || cancelled.Status != billing.InvoiceCancelled {
		t.Fatalf("cancel %+v %v", cancelled, err)
	}
	var active int64
	db.Model(&billing.InvoiceLine{}).Where("billable_key=? AND is_active", fmt.Sprintf("%s:%d", authorization.ReferencePerformedAct, act.ID)).Count(&active)
	if active != 0 {
		t.Fatalf("active lines after cancel=%d", active)
	}
	if _, err := NewService(db).Void(act.ID, VoidRequest{Reason: "after reverse cancel"}, 7); err != nil {
		t.Fatal(err)
	}
	var pa Act
	db.First(&pa, act.ID)
	if pa.Status != StatusVoided {
		t.Fatalf("pa=%s", pa.Status)
	}
	var payN, revN, lineN int64
	db.Model(&billing.Payment{}).Where("invoice_id=?", issued.ID).Count(&payN)
	db.Model(&billing.PaymentReversal{}).Count(&revN)
	db.Model(&billing.InvoiceLine{}).Where("invoice_id=?", issued.ID).Count(&lineN)
	if payN != 1 || revN != 1 || lineN == 0 {
		t.Fatalf("history pay=%d rev=%d lines=%d", payN, revN, lineN)
	}
	var cnN int64
	db.Model(&billing.CreditNote{}).Count(&cnN)
	if cnN != 0 {
		t.Fatal("sequence must not require CreditNote")
	}
}

func TestPostgresVoidLegacyClinicalPECUntouched(t *testing.T) {
	db := voidReconDB(t)
	f := seedVoidFixture(t, db, "LEGACY")
	consult := voidConsultStub{PatientID: f.patient.ID, Service: "Médecine"}
	if err := db.Create(&consult).Error; err != nil {
		t.Fatal(err)
	}
	srcID := consult.ID
	act := createPerformed(t, db, f, "CONSULTATION", &srcID)
	legacy := authorization.InsuranceAuthorization{
		AuthorizationNumber: "PEC-LEGACY-1", PatientID: f.patient.ID, MedicalRecordID: f.record.ID,
		PatientCoverageID: f.coverage.ID, InsuranceCompanyID: f.company.ID, GuarantorID: f.guarantor.ID,
		ReferenceType: "CONSULTATION", ReferenceID: consult.ID,
		RequestedAt: time.Now(), RequestedBy: 1, Status: authorization.StatusPending, CreatedBy: 1, UpdatedBy: 1,
	}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := NewService(db).Void(act.ID, VoidRequest{Reason: "legacy boundary"}, 6); err != nil {
		t.Fatal(err)
	}
	var after authorization.InsuranceAuthorization
	db.First(&after, legacy.ID)
	if after.Status != authorization.StatusPending {
		t.Fatalf("legacy mutated=%s", after.Status)
	}
}

func TestPostgresVoidCoveredLinkOnly(t *testing.T) {
	db := voidReconDB(t)
	f := seedVoidFixture(t, db, "COVER")
	consult := voidConsultStub{PatientID: f.patient.ID, Service: "Chir"}
	db.Create(&consult)
	act := createPerformed(t, db, f, "", nil)
	other := createPerformed(t, db, f, "", nil)
	parent := authorization.InsuranceAuthorization{
		AuthorizationNumber: "PEC-PARENT-1", PatientID: f.patient.ID, MedicalRecordID: f.record.ID,
		PatientCoverageID: f.coverage.ID, InsuranceCompanyID: f.company.ID, GuarantorID: f.guarantor.ID,
		ReferenceType: "CONSULTATION", ReferenceID: consult.ID,
		RequestedAt: time.Now(), RequestedBy: 1, Status: authorization.StatusDraft, CreatedBy: 1, UpdatedBy: 1,
	}
	if err := db.Create(&parent).Error; err != nil {
		t.Fatal(err)
	}
	linkPA := authorization.InsuranceAuthorizationAct{
		InsuranceAuthorizationID: parent.ID, PatientID: f.patient.ID, PatientCoverageID: f.coverage.ID,
		ReferenceType: authorization.ReferencePerformedAct, ReferenceID: act.ID,
		RelationType: authorization.RelationCovered, IsActive: true, CreatedBy: 1,
	}
	linkOther := authorization.InsuranceAuthorizationAct{
		InsuranceAuthorizationID: parent.ID, PatientID: f.patient.ID, PatientCoverageID: f.coverage.ID,
		ReferenceType: authorization.ReferencePerformedAct, ReferenceID: other.ID,
		RelationType: authorization.RelationCovered, IsActive: true, CreatedBy: 1,
	}
	db.Create(&linkPA)
	db.Create(&linkOther)
	if _, err := NewService(db).Void(act.ID, VoidRequest{Reason: "covered only"}, 7); err != nil {
		t.Fatal(err)
	}
	var parentAfter authorization.InsuranceAuthorization
	db.First(&parentAfter, parent.ID)
	if parentAfter.Status != authorization.StatusDraft {
		t.Fatalf("parent cancelled=%s", parentAfter.Status)
	}
	var paLink, otherLink authorization.InsuranceAuthorizationAct
	db.First(&paLink, linkPA.ID)
	db.First(&otherLink, linkOther.ID)
	if paLink.IsActive {
		t.Fatal("PA covered link still active")
	}
	if !otherLink.IsActive {
		t.Fatal("unrelated covered link deactivated")
	}
}

func TestPostgresConcurrentVoidVsCreateInvoice(t *testing.T) {
	db := voidReconDB(t)
	f := seedVoidFixture(t, db, "CONCINV")
	act := createPerformed(t, db, f, "", nil)
	tariffID := tariffPA(t, db, f.catalog.ID, "VR-CONC-INV", 4000)
	paSvc := NewService(db)
	billSvc := billing.NewService(db)
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, err := paSvc.Void(act.ID, VoidRequest{Reason: "race invoice"}, 1)
		errs <- err
	}()
	go func() {
		defer wg.Done()
		<-start
		_, err := billSvc.CreateInvoice(billing.CreateInvoiceRequest{
			PatientID: f.patient.ID,
			Lines:     []billing.InvoiceLineRequest{{ActType: authorization.ReferencePerformedAct, ReferenceID: act.ID, TariffID: tariffID}},
		}, 2)
		errs <- err
	}()
	close(start)
	wg.Wait()
	close(errs)
	var voidErr, invErr error
	// Order of errs is nondeterministic; classify by side effects.
	for err := range errs {
		_ = err
	}
	var pa Act
	db.First(&pa, act.ID)
	var active int64
	db.Model(&billing.InvoiceLine{}).Where("billable_key=? AND is_active", fmt.Sprintf("%s:%d", authorization.ReferencePerformedAct, act.ID)).Count(&active)
	if pa.Status == StatusVoided && active > 0 {
		t.Fatalf("forbidden: VOIDED with active billing lines=%d voidErr=%v invErr=%v", active, voidErr, invErr)
	}
	if pa.Status == StatusPerformed && active != 1 {
		t.Fatalf("invoice-win expected 1 active line, got %d pa=%s", active, pa.Status)
	}
	if pa.Status == StatusVoided && active != 0 {
		t.Fatalf("void-win expected 0 active lines, got %d", active)
	}
	if pa.Status != StatusVoided && pa.Status != StatusPerformed {
		t.Fatalf("unexpected pa status=%s", pa.Status)
	}
}

func TestPostgresConcurrentVoidVsCreatePEC(t *testing.T) {
	db := voidReconDB(t)
	f := seedVoidFixture(t, db, "CONCPEC")
	act := createPerformed(t, db, f, "", nil)
	paSvc := NewService(db)
	authSvc := authorization.NewService(db)
	amount := 10000.0
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, err := paSvc.Void(act.ID, VoidRequest{Reason: "race pec"}, 1)
		errs <- err
	}()
	go func() {
		defer wg.Done()
		<-start
		_, err := authSvc.Create(authorization.CreateRequest{
			PatientID: f.patient.ID, PatientCoverageID: f.coverage.ID,
			ReferenceType: authorization.ReferencePerformedAct, ReferenceID: act.ID, RequestedAmount: &amount,
		}, 2)
		errs <- err
	}()
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		_ = err
	}
	var pa Act
	db.First(&pa, act.ID)
	if pa.Status != StatusVoided {
		t.Fatalf("expected VOIDED after race, got %s", pa.Status)
	}
	var open int64
	db.Model(&authorization.InsuranceAuthorization{}).
		Where("reference_type=? AND reference_id=? AND status IN ?", authorization.ReferencePerformedAct, act.ID,
			[]string{authorization.StatusDraft, authorization.StatusSubmitted, authorization.StatusPending}).
		Count(&open)
	if open != 0 {
		t.Fatalf("forbidden open PEC count=%d", open)
	}
}

func TestPostgresConcurrentVoidVsDecide(t *testing.T) {
	db := voidReconDB(t)
	f := seedVoidFixture(t, db, "CONCDEC")
	act := createPerformed(t, db, f, "", nil)
	authID := seedPAAuth(t, db, f, act.ID, authorization.StatusPending, nil, 10000)
	rate := 80.0
	paSvc := NewService(db)
	authSvc := authorization.NewService(db)
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, err := paSvc.Void(act.ID, VoidRequest{Reason: "race decide"}, 3)
		errs <- err
	}()
	go func() {
		defer wg.Done()
		<-start
		_, err := authSvc.Decide(authID, authorization.DecisionRequest{
			Status: authorization.StatusApproved, ExternalReference: "RACE-DEC", ExternalDecisionDate: "2026-10-01", ApprovedRate: &rate,
		}, 4, authorization.UnrestrictedAccess(4))
		errs <- err
	}()
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		_ = err
	}
	var pa Act
	db.First(&pa, act.ID)
	if pa.Status != StatusVoided {
		t.Fatalf("pa=%s", pa.Status)
	}
	var auth authorization.InsuranceAuthorization
	db.First(&auth, authID)
	switch auth.Status {
	case authorization.StatusCancelled, authorization.StatusApproved:
		// both serial outcomes valid
	default:
		t.Fatalf("forbidden open/other status=%s", auth.Status)
	}
	if auth.Status == authorization.StatusDraft || auth.Status == authorization.StatusSubmitted || auth.Status == authorization.StatusPending {
		t.Fatalf("open pec survived=%s", auth.Status)
	}
}
