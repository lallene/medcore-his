package insurance_receivables

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/billing"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type insPatient struct {
	ID                                       uint `gorm:"primaryKey"`
	CodePatient, NumeroDossier, Nom, Prenoms string
}

func (insPatient) TableName() string { return "patients" }

type insCompany struct {
	ID         uint `gorm:"primaryKey"`
	Code, Name string
}

func (insCompany) TableName() string { return "insurance_companies" }

type insAuthorization struct {
	ID                  uint `gorm:"primaryKey"`
	AuthorizationNumber string
	InsuranceCompanyID  uint
	Status              string
	ExternalReference   string
}

func (insAuthorization) TableName() string { return "insurance_authorizations" }

func insDSN(dsn, schema string) string {
	if strings.Contains(dsn, "://") {
		sep := "?"
		if strings.Contains(dsn, "?") {
			sep = "&"
		}
		return dsn + sep + "search_path=" + url.QueryEscape(schema)
	}
	return dsn + " search_path=" + schema
}
func insDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL absent: tests PostgreSQL Insurance Receivables ignorés")
	}
	admin, e := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if e != nil {
		t.Fatal(e)
	}
	schema := fmt.Sprintf("insurance_receivables_%d", time.Now().UnixNano())
	if e = admin.Exec(`CREATE SCHEMA "` + schema + `"`).Error; e != nil {
		t.Fatal(e)
	}
	db, e := gorm.Open(postgres.Open(insDSN(dsn, schema)), &gorm.Config{})
	if e != nil {
		t.Fatal(e)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(10)
	t.Cleanup(func() { _ = sqlDB.Close(); _ = admin.Exec(`DROP SCHEMA IF EXISTS "` + schema + `" CASCADE`).Error })
	if e = db.AutoMigrate(&insPatient{}, &insCompany{}, &insAuthorization{}, &billing.Invoice{}, &billing.InvoiceLine{}, &billing.Payment{}, &Settlement{}, &SettlementAllocation{}, &ReceivableMetadata{}, &FollowUp{}, &SubmissionBatch{}, &SubmissionBatchItem{}); e != nil {
		t.Fatal(e)
	}
	return db
}
func insuredLine(t *testing.T, db *gorm.DB, company insCompany, patient insPatient, number string, gross, insurance, patientAmount int64) (billing.Invoice, billing.InvoiceLine) {
	t.Helper()
	db.FirstOrCreate(&company, insCompany{Code: company.Code})
	db.FirstOrCreate(&patient, insPatient{CodePatient: patient.CodePatient})
	auth := insAuthorization{AuthorizationNumber: "PEC-" + number, InsuranceCompanyID: company.ID, Status: "APPROVED"}
	if e := db.Create(&auth).Error; e != nil {
		t.Fatal(e)
	}
	now := time.Now()
	inv := billing.Invoice{Number: number, PatientID: patient.ID, Status: billing.InvoicePaid, GrossAmount: gross, InsuranceAmount: insurance, PatientAmount: patientAmount, PaidAmount: patientAmount, BalanceAmount: 0, IssuedAt: &now, CreatedBy: 1, UpdatedBy: 1}
	if e := db.Create(&inv).Error; e != nil {
		t.Fatal(e)
	}
	line := billing.InvoiceLine{InvoiceID: inv.ID, TariffID: 1, ActType: "CONSULTATION", ReferenceID: inv.ID, ClinicalReferenceID: inv.ID, BillableKey: "INS-" + number, Description: "Acte assuré", Quantity: 1, UnitPrice: gross, GrossAmount: gross, InsuranceAmount: insurance, PatientAmount: patientAmount, AuthorizationID: &auth.ID, AuthorizationNumber: auth.AuthorizationNumber, CoverageResolution: "DIRECT", CoverageStatus: "APPROVED", IsActive: true}
	if e := db.Create(&line).Error; e != nil {
		t.Fatal(e)
	}
	if patientAmount > 0 {
		p := billing.Payment{InvoiceID: inv.ID, Amount: patientAmount, PaymentMethod: "CASH", IdempotencyKey: "PAT-" + number, PaidAt: now, ReceivedBy: 1}
		if e := db.Create(&p).Error; e != nil {
			t.Fatal(e)
		}
	}
	return inv, line
}
func TestPostgresPatientAndInsuranceCircuitsStayIndependent(t *testing.T) {
	db := insDB(t)
	service := NewService(db)
	company := insCompany{Code: "ALLIANZ", Name: "Allianz"}
	patient := insPatient{CodePatient: "INS-P1", NumeroDossier: "INS-D1", Nom: "Assure", Prenoms: "Patient"}
	inv, line := insuredLine(t, db, company, patient, "INV-INS-1", 50000, 35000, 15000)
	if e := db.Where("code=?", company.Code).First(&company).Error; e != nil {
		t.Fatal(e)
	}
	page, e := service.List(Filter{Page: 1, Limit: 20})
	if e != nil || len(page.Items) != 1 || page.Items[0].InsuranceBalance != 35000 || page.Items[0].Status != "UNPAID" {
		t.Fatalf("initial=%+v %v", page, e)
	}
	request := SettlementRequest{InsuranceCompanyID: company.ID, ExternalReference: "VIR-20K", ReceivedAt: time.Now().Format("2006-01-02"), TotalAmount: 20000, PaymentMethod: "BANK_TRANSFER", IdempotencyKey: "INS-IDEM-20K"}
	first, e := service.CreateSettlement(request, 41)
	if e != nil || first.CreatedBy != 41 {
		t.Fatalf("create=%+v %v", first, e)
	}
	retry, e := service.CreateSettlement(request, 99)
	if e != nil || retry.ID != first.ID {
		t.Fatalf("idempotence=%+v %v", retry, e)
	}
	if _, e = service.Allocate(first.ID, AllocationRequest{InvoiceLineID: line.ID, Amount: 20000}, 42); e != nil {
		t.Fatal(e)
	}
	posted, e := service.Post(first.ID, 43)
	if e != nil || posted.PostedBy == nil || *posted.PostedBy != 43 {
		t.Fatalf("post=%+v %v", posted, e)
	}
	page, _ = service.List(Filter{Page: 1, Limit: 20})
	if page.Items[0].InsuranceBalance != 15000 || page.Items[0].Status != "PARTIALLY_PAID" {
		t.Fatalf("partial=%+v", page.Items[0])
	}
	second, e := service.CreateSettlement(SettlementRequest{InsuranceCompanyID: company.ID, ExternalReference: "VIR-15K", ReceivedAt: time.Now().Format("2006-01-02"), TotalAmount: 15000, PaymentMethod: "CHECK", IdempotencyKey: "INS-IDEM-15K"}, 44)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = service.Allocate(second.ID, AllocationRequest{InvoiceLineID: line.ID, Amount: 15000}, 45); e != nil {
		t.Fatal(e)
	}
	if _, e = service.Post(second.ID, 46); e != nil {
		t.Fatal(e)
	}
	page, _ = service.List(Filter{Page: 1, Limit: 20})
	if page.Items[0].InsuranceBalance != 0 || page.Items[0].Status != "PAID" {
		t.Fatalf("paid=%+v", page.Items[0])
	}
	var patientPaid, count int64
	db.Model(&billing.Payment{}).Where("invoice_id=?", inv.ID).Select("COALESCE(SUM(amount),0)").Scan(&patientPaid)
	db.Model(&billing.Payment{}).Count(&count)
	if patientPaid != 15000 || count != 1 {
		t.Fatalf("patient circuit mutated paid=%d count=%d", patientPaid, count)
	}
}

func TestPostgresMultiInvoiceCapsUnallocatedBatchAndConcurrency(t *testing.T) {
	db := insDB(t)
	service := NewService(db)
	company := insCompany{Code: "NSIA", Name: "NSIA"}
	_, a := insuredLine(t, db, company, insPatient{CodePatient: "M1", NumeroDossier: "MD1", Nom: "A"}, "INV-M1", 35000, 35000, 0)
	_, b := insuredLine(t, db, company, insPatient{CodePatient: "M2", NumeroDossier: "MD2", Nom: "B"}, "INV-M2", 25000, 25000, 0)
	_, c := insuredLine(t, db, company, insPatient{CodePatient: "M3", NumeroDossier: "MD3", Nom: "C"}, "INV-M3", 60000, 60000, 0)
	if e := db.Where("code=?", company.Code).First(&company).Error; e != nil {
		t.Fatal(e)
	}
	set, e := service.CreateSettlement(SettlementRequest{InsuranceCompanyID: company.ID, ExternalReference: "VIR-100K", ReceivedAt: time.Now().Format("2006-01-02"), TotalAmount: 100000, PaymentMethod: "BANK_TRANSFER", IdempotencyKey: "MULTI-100K"}, 1)
	if e != nil {
		t.Fatal(e)
	}
	for _, x := range []AllocationRequest{{a.ID, 35000}, {b.ID, 25000}, {c.ID, 40000}} {
		if _, e = service.Allocate(set.ID, x, 2); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = service.Allocate(set.ID, AllocationRequest{InvoiceLineID: c.ID, Amount: 1}, 2); e == nil {
		t.Fatal("settlement over-allocation accepted")
	}
	if _, e = service.Post(set.ID, 3); e != nil {
		t.Fatal(e)
	}
	page, _ := service.List(Filter{Page: 1, Limit: 20})
	var cBalance int64
	for _, x := range page.Items {
		if x.InvoiceLineID == c.ID {
			cBalance = x.InsuranceBalance
		}
	}
	if cBalance != 20000 {
		t.Fatalf("remaining C=%d", cBalance)
	}
	unallocated, e := service.CreateSettlement(SettlementRequest{InsuranceCompanyID: company.ID, ExternalReference: "VIR-UNALLOC", ReceivedAt: time.Now().Format("2006-01-02"), TotalAmount: 30000, PaymentMethod: "OTHER", IdempotencyKey: "UNALLOC-30K"}, 4)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = service.Allocate(unallocated.ID, AllocationRequest{InvoiceLineID: c.ID, Amount: 10000}, 4); e != nil {
		t.Fatal(e)
	}
	unallocated, e = service.Post(unallocated.ID, 4)
	if e != nil || unallocated.UnallocatedAmount != 20000 {
		t.Fatalf("unallocated=%+v %v", unallocated, e)
	}
	_, batchA := insuredLine(t, db, company, insPatient{CodePatient: "BATCH-A", NumeroDossier: "BATCH-AD", Nom: "Batch A"}, "INV-BATCH-A", 12000, 12000, 0)
	_, batchB := insuredLine(t, db, company, insPatient{CodePatient: "BATCH-B", NumeroDossier: "BATCH-BD", Nom: "Batch B"}, "INV-BATCH-B", 13000, 13000, 0)
	batch, e := service.CreateBatch(BatchRequest{InsuranceCompanyID: company.ID, InvoiceLineIDs: []uint{batchA.ID, batchB.ID}, Comment: "DEMO"}, 5)
	if e != nil || batch.CreatedBy != 5 {
		t.Fatalf("batch=%+v %v", batch, e)
	}
	if _, e = service.CreateBatch(BatchRequest{InsuranceCompanyID: company.ID, InvoiceLineIDs: []uint{batchA.ID}}, 5); e == nil {
		t.Fatal("double active batch accepted")
	}
	submitted, e := service.SubmitBatch(batch.ID, 6)
	if e != nil || submitted.SubmittedBy == nil || *submitted.SubmittedBy != 6 {
		t.Fatalf("submit=%+v %v", submitted, e)
	}
	_, line := insuredLine(t, db, company, insPatient{CodePatient: "CONC", NumeroDossier: "CONC-D", Nom: "Concurrent"}, "INV-CONC", 10000, 10000, 0)
	s1, _ := service.CreateSettlement(SettlementRequest{InsuranceCompanyID: company.ID, ExternalReference: "C1", ReceivedAt: time.Now().Format("2006-01-02"), TotalAmount: 10000, PaymentMethod: "CHECK", IdempotencyKey: "C1"}, 1)
	s2, _ := service.CreateSettlement(SettlementRequest{InsuranceCompanyID: company.ID, ExternalReference: "C2", ReceivedAt: time.Now().Format("2006-01-02"), TotalAmount: 10000, PaymentMethod: "CHECK", IdempotencyKey: "C2"}, 1)
	var wg sync.WaitGroup
	wg.Add(2)
	errs := make(chan error, 2)
	go func() {
		defer wg.Done()
		_, e := service.Allocate(s1.ID, AllocationRequest{InvoiceLineID: line.ID, Amount: 10000}, 1)
		errs <- e
	}()
	go func() {
		defer wg.Done()
		_, e := service.Allocate(s2.ID, AllocationRequest{InvoiceLineID: line.ID, Amount: 10000}, 1)
		errs <- e
	}()
	wg.Wait()
	close(errs)
	success, failed := 0, 0
	for e := range errs {
		if e == nil {
			success++
		} else {
			failed++
		}
	}
	if success != 1 || failed != 1 {
		t.Fatalf("concurrency success=%d failed=%d", success, failed)
	}
}

func TestPostgresOnlyApprovedDirectOrCoveredLinesBecomeReceivables(t *testing.T) {
	db := insDB(t)
	service := NewService(db)
	company := insCompany{Code: "FILTER", Name: "Filter Assurance"}
	patient := insPatient{CodePatient: "FILTER-P", NumeroDossier: "FILTER-D", Nom: "Filter"}
	_, direct := insuredLine(t, db, company, patient, "INV-DIRECT", 10000, 7000, 3000)
	_, covered := insuredLine(t, db, company, insPatient{CodePatient: "FILTER-C", NumeroDossier: "FILTER-CD", Nom: "Covered"}, "INV-COVERED", 10000, 7000, 3000)
	if e := db.Model(&covered).Update("coverage_resolution", "COVERED").Error; e != nil {
		t.Fatal(e)
	}
	_, none := insuredLine(t, db, company, insPatient{CodePatient: "FILTER-N", NumeroDossier: "FILTER-ND", Nom: "None"}, "INV-NONE", 10000, 7000, 3000)
	if e := db.Model(&none).Update("coverage_resolution", "NONE").Error; e != nil {
		t.Fatal(e)
	}
	_, rejected := insuredLine(t, db, company, insPatient{CodePatient: "FILTER-R", NumeroDossier: "FILTER-RD", Nom: "Rejected"}, "INV-REJECTED", 10000, 7000, 3000)
	if e := db.Model(&insAuthorization{}).Where("id=?", rejected.AuthorizationID).Update("status", "REJECTED").Error; e != nil {
		t.Fatal(e)
	}
	page, e := service.List(Filter{Page: 1, Limit: 20})
	if e != nil {
		t.Fatal(e)
	}
	if len(page.Items) != 2 {
		t.Fatalf("expected DIRECT+COVERED only, got %+v", page.Items)
	}
	ids := map[uint]bool{}
	for _, item := range page.Items {
		ids[item.InvoiceLineID] = true
	}
	if !ids[direct.ID] || !ids[covered.ID] || ids[none.ID] || ids[rejected.ID] {
		t.Fatalf("wrong projection ids=%v", ids)
	}
	follow, e := service.AddFollowUp(direct.ID, FollowUpRequest{Type: "REMINDER", Note: "Relance test"}, 77)
	if e != nil || follow.CreatedBy != 77 {
		t.Fatalf("followup=%+v err=%v", follow, e)
	}
	detail, e := service.ReceivableDetail(direct.ID)
	if e != nil || len(detail.FollowUps) != 1 || detail.FollowUps[0].Note != "Relance test" {
		t.Fatalf("detail=%+v err=%v", detail, e)
	}
}
