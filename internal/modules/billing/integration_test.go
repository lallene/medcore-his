package billing

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/authorization"
	"github.com/lallene/medcore-his/backend/internal/modules/medical_records"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type billingConsultation struct {
	ID, PatientID   uint
	Service, Status string
	CreatedAt       time.Time
}

func (billingConsultation) TableName() string { return "consultations" }

type billingCoverage struct {
	ID, PatientID, CompanyID, GuarantorID uint
	MemberNumber                          string
	CoverageRate                          float64
	ValidFrom, ValidTo                    *time.Time
	IsActive                              bool
	DeletedAt                             gorm.DeletedAt
}

func (billingCoverage) TableName() string { return "patient_coverages" }

type billingCompany struct {
	ID   uint
	Name string
}

func (billingCompany) TableName() string { return "insurance_companies" }

type billingGuarantor struct {
	ID   uint
	Name string
}

func (billingGuarantor) TableName() string { return "insurance_guarantors" }

type billingExam struct {
	ID   uint
	Name string
}

func (billingExam) TableName() string { return "medical_exams" }

type billingLabOrder struct {
	ID, PatientID, MedicalExamID uint
	RequestNumber, Status        string
	CreatedAt                    time.Time
}

func (billingLabOrder) TableName() string { return "laboratory_orders" }

type billingImagingOrder struct {
	ID, PatientID, MedicalExamID uint
	OrderNumber, Status          string
	CreatedAt                    time.Time
}

func (billingImagingOrder) TableName() string { return "imaging_orders" }

type billingMedication struct {
	ID   uint
	Name string
}

func (billingMedication) TableName() string { return "medications" }

type billingPresentation struct {
	ID, MedicationID uint
	Dosage           string
}

func (billingPresentation) TableName() string { return "medication_presentations" }

type billingDispensation struct {
	ID, PresentationID uint
	Quantity           float64
	Status             string
	PatientID          *uint
	ReferenceID        *uint
	CreatedAt          time.Time
}

func (billingDispensation) TableName() string { return "pharmacy_dispensations" }

func schemaDSN(dsn, schema string) string {
	if strings.Contains(dsn, "://") {
		sep := "?"
		if strings.Contains(dsn, "?") {
			sep = "&"
		}
		return dsn + sep + "search_path=" + url.QueryEscape(schema)
	}
	return dsn + " search_path=" + schema
}
func billingDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL absent: tests PostgreSQL Billing ignorés")
	}
	admin, e := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if e != nil {
		t.Fatal(e)
	}
	schema := fmt.Sprintf("billing_%d", time.Now().UnixNano())
	if e = admin.Exec(`CREATE SCHEMA "` + schema + `"`).Error; e != nil {
		t.Fatal(e)
	}
	db, e := gorm.Open(postgres.Open(schemaDSN(dsn, schema)), &gorm.Config{})
	if e != nil {
		t.Fatal(e)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(10)
	t.Cleanup(func() { _ = sqlDB.Close(); _ = admin.Exec(`DROP SCHEMA IF EXISTS "` + schema + `" CASCADE`).Error })
	models := []any{&patients.Patient{}, &billingCoverage{}, &billingCompany{}, &billingGuarantor{}, &billingExam{}, &billingLabOrder{}, &billingImagingOrder{}, &billingMedication{}, &billingPresentation{}, &billingDispensation{}, &medical_records.MedicalRecord{}, &medical_records.MedicalTimelineEvent{}, &billingConsultation{}, &authorization.InsuranceAuthorization{}, &authorization.InsuranceAuthorizationAct{}, &Tariff{}, &Invoice{}, &InvoiceLine{}, &AuthorizationAllocation{}, &Payment{}}
	if e = db.AutoMigrate(models...); e != nil {
		t.Fatal(e)
	}
	if e = db.Exec("CREATE UNIQUE INDEX ux_billing_active_billable_key ON billing_invoice_lines (billable_key) WHERE is_active=true").Error; e != nil {
		t.Fatal(e)
	}
	return db
}
func seedPatient(t *testing.T, db *gorm.DB, suffix string) (uint, uint) {
	t.Helper()
	p := patients.Patient{CodePatient: "BILL-P-" + suffix, NumeroDossier: "BILL-D-" + suffix, Nom: "Test", Prenoms: "Billing"}
	if e := db.Create(&p).Error; e != nil {
		t.Fatal(e)
	}
	m := medical_records.MedicalRecord{PatientID: p.ID, RecordNumber: "BILL-MR-" + suffix}
	if e := db.Create(&m).Error; e != nil {
		t.Fatal(e)
	}
	return p.ID, m.ID
}
func consultation(t *testing.T, db *gorm.DB, patient uint, service string) uint {
	t.Helper()
	c := billingConsultation{PatientID: patient, Service: service, Status: "completed", CreatedAt: time.Now()}
	if e := db.Create(&c).Error; e != nil {
		t.Fatal(e)
	}
	return c.ID
}
func tariff(t *testing.T, db *gorm.DB, typ, code string, price int64) uint {
	t.Helper()
	x := Tariff{ActType: typ, Code: code, Label: code, UnitPrice: price, Currency: "XOF", EffectiveFrom: time.Now().Add(-time.Hour), IsActive: true, CreatedBy: 7, UpdatedBy: 7}
	if e := db.Create(&x).Error; e != nil {
		t.Fatal(e)
	}
	return x.ID
}
func seedBilling(t *testing.T, db *gorm.DB) (*Service, uint, uint, uint) {
	p, _ := seedPatient(t, db, "BASE")
	return NewService(db), p, consultation(t, db, p, "Médecine"), tariff(t, db, "CONSULTATION", "CONS", 20000)
}

func TestPostgresInvoiceSnapshotAntiDuplicateAndPayments(t *testing.T) {
	db := billingDB(t)
	s, p, c, tariffID := seedBilling(t, db)
	invoice, e := s.CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{{ActType: "CONSULTATION", ReferenceID: c, TariffID: tariffID}}}, 42)
	if e != nil {
		t.Fatal(e)
	}
	if invoice.GrossAmount != 20000 || invoice.InsuranceAmount != 0 || invoice.PatientAmount != 20000 || invoice.CreatedBy != 42 {
		t.Fatalf("snapshot incorrect: %+v", invoice)
	}
	if _, e = s.CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{{ActType: "CONSULTATION", ReferenceID: c, TariffID: tariffID}}}, 42); !isConflict(e) {
		t.Fatalf("double billing error=%v", e)
	}
	duplicate := invoice.Lines[0]
	duplicate.ID = 0
	duplicate.InvoiceID = invoice.ID
	if e = db.Create(&duplicate).Error; e == nil {
		t.Fatal("PostgreSQL accepted a duplicate billable key")
	}
	invoice, e = s.Issue(invoice.ID, 43)
	if e != nil || invoice.IssuedBy == nil || *invoice.IssuedBy != 43 {
		t.Fatalf("issue/JWT: %+v %v", invoice, e)
	}
	invoice, e = s.Pay(invoice.ID, PaymentRequest{Amount: 10000, PaymentMethod: "CASH", IdempotencyKey: "LOT12-PAYMENT-001"}, 44)
	if e != nil || invoice.Status != InvoicePartiallyPaid || invoice.BalanceAmount != 10000 {
		t.Fatalf("partial: %+v %v", invoice, e)
	}
	invoice, e = s.Pay(invoice.ID, PaymentRequest{Amount: 10000, PaymentMethod: "CASH", IdempotencyKey: "LOT12-PAYMENT-001"}, 44)
	if e != nil || invoice.PaidAmount != 10000 {
		t.Fatalf("retry: %+v %v", invoice, e)
	}
	var paymentCount int64
	db.Model(&Payment{}).Count(&paymentCount)
	if paymentCount != 1 {
		t.Fatalf("payments=%d", paymentCount)
	}
	var paid Payment
	db.Where("idempotency_key=?", "LOT12-PAYMENT-001").First(&paid)
	if paid.ReceivedBy != 44 {
		t.Fatalf("received_by=%d", paid.ReceivedBy)
	}
	if _, e = s.Pay(invoice.ID, PaymentRequest{Amount: 10001, PaymentMethod: "CASH", IdempotencyKey: "pay-over"}, 44); !isConflict(e) {
		t.Fatalf("overpayment=%v", e)
	}
	invoice, e = s.Pay(invoice.ID, PaymentRequest{Amount: 10000, PaymentMethod: "CARD", IdempotencyKey: "pay-final"}, 45)
	if e != nil || invoice.Status != InvoicePaid || invoice.BalanceAmount != 0 {
		t.Fatalf("final: %+v %v", invoice, e)
	}
	if _, e = s.Cancel(invoice.ID, "Interdit", 46); !isConflict(e) {
		t.Fatalf("paid cancellation=%v", e)
	}
	var events int64
	db.Model(&medical_records.MedicalTimelineEvent{}).Where("patient_id=? AND event_type IN ?", p, []string{"invoice_issued", "payment_received", "invoice_paid"}).Count(&events)
	if events != 3 {
		t.Fatalf("timeline=%d", events)
	}
}

func TestPostgresTariffSnapshotRollbackAndSQLConstraints(t *testing.T) {
	db := billingDB(t)
	s, p, c, tariffID := seedBilling(t, db)
	created, e := s.CreateTariff(TariffRequest{ActType: "CONSULTATION", Code: "JWT-T", Label: "JWT tariff", UnitPrice: 1000}, 77)
	if e != nil || created.CreatedBy != 77 || created.UpdatedBy != 77 {
		t.Fatalf("tariff create JWT=%+v %v", created, e)
	}
	active := true
	updated, e := s.UpdateTariff(created.ID, TariffRequest{Label: "JWT tariff updated", UnitPrice: 1200, IsActive: &active}, 78)
	if e != nil || updated.UpdatedBy != 78 {
		t.Fatalf("tariff update JWT=%+v %v", updated, e)
	}
	first, e := s.CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{{ActType: "CONSULTATION", ReferenceID: c, TariffID: tariffID}}}, 7)
	if e != nil {
		t.Fatal(e)
	}
	var current Tariff
	db.First(&current, tariffID)
	current.UnitPrice = 25000
	current.UpdatedBy = 8
	if e = db.Save(&current).Error; e != nil {
		t.Fatal(e)
	}
	c2 := consultation(t, db, p, "Urgences")
	second, e := s.CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{{ActType: "CONSULTATION", ReferenceID: c2, TariffID: tariffID}}}, 8)
	if e != nil {
		t.Fatal(e)
	}
	if first.Lines[0].UnitPrice != 20000 || second.Lines[0].UnitPrice != 25000 {
		t.Fatalf("snapshots %d/%d", first.Lines[0].UnitPrice, second.Lines[0].UnitPrice)
	}
	before := int64(0)
	db.Model(&Invoice{}).Count(&before)
	c3 := consultation(t, db, p, "Pédiatrie")
	_, e = s.CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{{ActType: "CONSULTATION", ReferenceID: c3, TariffID: tariffID}, {ActType: "CONSULTATION", ReferenceID: 999999, TariffID: tariffID}}}, 9)
	if e == nil {
		t.Fatal("invalid multi-line accepted")
	}
	var after int64
	db.Model(&Invoice{}).Count(&after)
	if after != before {
		t.Fatalf("partial invoice persisted %d/%d", before, after)
	}
	var rollbackEvents int64
	db.Model(&medical_records.MedicalTimelineEvent{}).Where("reference_type='billing_invoice' AND reference_id NOT IN (SELECT id FROM billing_invoices)").Count(&rollbackEvents)
	if rollbackEvents != 0 {
		t.Fatalf("rollback timeline events=%d", rollbackEvents)
	}
	if e = db.Create(&Payment{InvoiceID: first.ID, Amount: -1, PaymentMethod: "CASH", IdempotencyKey: "negative", PaidAt: time.Now(), ReceivedBy: 1}).Error; e == nil {
		t.Fatal("negative payment accepted by PostgreSQL")
	}
	var indexCount int64
	db.Raw("SELECT count(*) FROM pg_indexes WHERE schemaname=current_schema() AND indexname IN ('ux_billing_active_billable_key','idx_billing_invoices_number','idx_billing_payments_idempotency_key','idx_billing_authorization_allocations_invoice_line_id')").Scan(&indexCount)
	if indexCount < 4 {
		t.Fatalf("financial indexes=%d", indexCount)
	}
}

func seedAuthorization(t *testing.T, db *gorm.DB, patient, record, primary uint, status string, rate *float64, insurance float64) (uint, uint) {
	t.Helper()
	co := billingCompany{Name: "Assureur" + fmt.Sprint(primary)}
	db.Create(&co)
	g := billingGuarantor{Name: "Garant" + fmt.Sprint(primary)}
	db.Create(&g)
	cov := billingCoverage{PatientID: patient, CompanyID: co.ID, GuarantorID: g.ID, MemberNumber: "MEM", CoverageRate: 70, IsActive: true}
	if e := db.Create(&cov).Error; e != nil {
		t.Fatal(e)
	}
	requested := insurance
	auth := authorization.InsuranceAuthorization{AuthorizationNumber: fmt.Sprintf("PEC-T-%d", primary), PatientID: patient, MedicalRecordID: record, PatientCoverageID: cov.ID, InsuranceCompanyID: co.ID, GuarantorID: g.ID, ReferenceType: "CONSULTATION", ReferenceID: primary, RequestedAmount: &requested, RequestedAt: time.Now(), RequestedBy: 1, Status: status, ApprovedRate: rate, InsuranceAmount: &insurance, PatientAmount: ptr(0), CreatedBy: 1, UpdatedBy: 1}
	if e := db.Create(&auth).Error; e != nil {
		t.Fatal(e)
	}
	return auth.ID, cov.ID
}
func invoiceFor(t *testing.T, s *Service, p, c, tariffID uint) *Invoice {
	t.Helper()
	x, e := s.CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{{ActType: "CONSULTATION", ReferenceID: c, TariffID: tariffID}}}, 50)
	if e != nil {
		t.Fatal(e)
	}
	return x
}

func TestPostgresPECDecisionsCoveredPendingAndGlobalEnvelope(t *testing.T) {
	t.Run("DIRECT 70 percent", func(t *testing.T) {
		db := billingDB(t)
		p, m := seedPatient(t, db, "DIRECT")
		c := consultation(t, db, p, "A")
		rate := 70.0
		auth, _ := seedAuthorization(t, db, p, m, c, authorization.StatusApproved, &rate, 35000)
		x := invoiceFor(t, NewService(db), p, c, tariff(t, db, "CONSULTATION", "D", 50000))
		if x.InsuranceAmount != 35000 || x.PatientAmount != 15000 || x.Lines[0].AuthorizationID == nil || *x.Lines[0].AuthorizationID != auth || x.Lines[0].CoverageResolution != "DIRECT" {
			t.Fatalf("direct=%+v", x)
		}
	})
	t.Run("REJECTED", func(t *testing.T) {
		db := billingDB(t)
		p, m := seedPatient(t, db, "REJ")
		c := consultation(t, db, p, "A")
		seedAuthorization(t, db, p, m, c, authorization.StatusRejected, nil, 0)
		x := invoiceFor(t, NewService(db), p, c, tariff(t, db, "CONSULTATION", "R", 40000))
		if x.InsuranceAmount != 0 || x.PatientAmount != 40000 {
			t.Fatalf("rejected=%+v", x)
		}
	})
	t.Run("PARTIAL ceiling", func(t *testing.T) {
		db := billingDB(t)
		p, m := seedPatient(t, db, "PART")
		c := consultation(t, db, p, "A")
		rate := 80.0
		seedAuthorization(t, db, p, m, c, authorization.StatusPartiallyApproved, &rate, 70000)
		x := invoiceFor(t, NewService(db), p, c, tariff(t, db, "CONSULTATION", "P", 120000))
		if x.InsuranceAmount != 70000 || x.PatientAmount != 50000 {
			t.Fatalf("partial=%+v", x)
		}
	})
	t.Run("COVERED and envelope", func(t *testing.T) {
		db := billingDB(t)
		p, m := seedPatient(t, db, "COV")
		a := consultation(t, db, p, "A")
		b := consultation(t, db, p, "B")
		auth, cov := seedAuthorization(t, db, p, m, a, authorization.StatusApproved, nil, 100000)
		if e := db.Create(&authorization.InsuranceAuthorizationAct{InsuranceAuthorizationID: auth, PatientID: p, PatientCoverageID: cov, ReferenceType: "CONSULTATION", ReferenceID: b, RelationType: authorization.RelationCovered, IsActive: true, CreatedBy: 1}).Error; e != nil {
			t.Fatal(e)
		}
		s := NewService(db)
		ta := tariff(t, db, "CONSULTATION", "A", 60000)
		tb := tariff(t, db, "CONSULTATION", "B", 70000)
		x := invoiceFor(t, s, p, a, ta)
		y := invoiceFor(t, s, p, b, tb)
		if x.InsuranceAmount != 60000 || y.InsuranceAmount != 40000 || y.PatientAmount != 30000 || y.Lines[0].CoverageResolution != "COVERED" {
			t.Fatalf("envelope %+v %+v", x, y)
		}
		var sum int64
		db.Model(&AuthorizationAllocation{}).Where("authorization_id=?", auth).Select("COALESCE(SUM(amount),0)").Scan(&sum)
		if sum != 100000 {
			t.Fatalf("allocation=%d", sum)
		}
	})
	t.Run("COVERED reverse envelope", func(t *testing.T) {
		db := billingDB(t)
		p, m := seedPatient(t, db, "REVERSE")
		a := consultation(t, db, p, "A")
		b := consultation(t, db, p, "B")
		auth, cov := seedAuthorization(t, db, p, m, a, authorization.StatusApproved, nil, 100000)
		if e := db.Create(&authorization.InsuranceAuthorizationAct{InsuranceAuthorizationID: auth, PatientID: p, PatientCoverageID: cov, ReferenceType: "CONSULTATION", ReferenceID: b, RelationType: authorization.RelationCovered, IsActive: true, CreatedBy: 1}).Error; e != nil {
			t.Fatal(e)
		}
		s := NewService(db)
		ta := tariff(t, db, "CONSULTATION", "RA", 60000)
		tb := tariff(t, db, "CONSULTATION", "RB", 70000)
		y := invoiceFor(t, s, p, b, tb)
		x := invoiceFor(t, s, p, a, ta)
		if y.InsuranceAmount != 70000 || x.InsuranceAmount != 30000 || x.PatientAmount != 30000 {
			t.Fatalf("reverse envelope %+v %+v", y, x)
		}
	})
	t.Run("PENDING blocks issue", func(t *testing.T) {
		db := billingDB(t)
		p, m := seedPatient(t, db, "PEND")
		c := consultation(t, db, p, "A")
		seedAuthorization(t, db, p, m, c, authorization.StatusPending, nil, 0)
		s := NewService(db)
		x := invoiceFor(t, s, p, c, tariff(t, db, "CONSULTATION", "WAIT", 30000))
		if !x.CoveragePending || x.InsuranceAmount != 0 {
			t.Fatalf("pending=%+v", x)
		}
		if _, e := s.Issue(x.ID, 3); !isConflict(e) {
			t.Fatalf("pending issue=%v", e)
		}
	})
}

func TestPostgresPharmacyPartialDispensations(t *testing.T) {
	db := billingDB(t)
	p, _ := seedPatient(t, db, "PHARM")
	med := billingMedication{Name: "DOLIPRANE"}
	db.Create(&med)
	pres := billingPresentation{MedicationID: med.ID, Dosage: "500 mg"}
	db.Create(&pres)
	rx := uint(99)
	makeDisp := func(q float64) uint {
		d := billingDispensation{PresentationID: pres.ID, Quantity: q, Status: "COMPLETED", PatientID: &p, ReferenceID: &rx, CreatedAt: time.Now()}
		if e := db.Create(&d).Error; e != nil {
			t.Fatal(e)
		}
		return d.ID
	}
	a := makeDisp(8)
	b := makeDisp(4)
	tariffID := tariff(t, db, "MEDICATION", "MED", 1000)
	s := NewService(db)
	x, e := s.CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{{ActType: "MEDICATION", ReferenceID: a, TariffID: tariffID}}}, 1)
	if e != nil {
		t.Fatal(e)
	}
	y, e := s.CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{{ActType: "MEDICATION", ReferenceID: b, TariffID: tariffID}}}, 1)
	if e != nil {
		t.Fatal(e)
	}
	if x.Lines[0].Quantity != 8 || x.GrossAmount != 8000 || y.Lines[0].Quantity != 4 || y.GrossAmount != 4000 || x.Lines[0].BillableKey == y.Lines[0].BillableKey {
		t.Fatalf("pharmacy %+v %+v", x, y)
	}
	if _, e = s.CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{{ActType: "MEDICATION", ReferenceID: a, TariffID: tariffID}}}, 1); !isConflict(e) {
		t.Fatalf("dispensation rebilled=%v", e)
	}
}

func TestPostgresConcurrentAllocationAndPayment(t *testing.T) {
	t.Run("allocation", func(t *testing.T) {
		db := billingDB(t)
		p, m := seedPatient(t, db, "CONCA")
		a := consultation(t, db, p, "A")
		b := consultation(t, db, p, "B")
		auth, cov := seedAuthorization(t, db, p, m, a, authorization.StatusApproved, nil, 100000)
		if e := db.Create(&authorization.InsuranceAuthorizationAct{InsuranceAuthorizationID: auth, PatientID: p, PatientCoverageID: cov, ReferenceType: "CONSULTATION", ReferenceID: b, RelationType: authorization.RelationCovered, IsActive: true, CreatedBy: 1}).Error; e != nil {
			t.Fatal(e)
		}
		ta := tariff(t, db, "CONSULTATION", "CA", 60000)
		tb := tariff(t, db, "CONSULTATION", "CB", 70000)
		s := NewService(db)
		start := make(chan struct{})
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		for _, v := range []struct{ c, t uint }{{a, ta}, {b, tb}} {
			wg.Add(1)
			go func(c, tar uint) {
				defer wg.Done()
				<-start
				_, e := s.CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{{ActType: "CONSULTATION", ReferenceID: c, TariffID: tar}}}, 1)
				errs <- e
			}(v.c, v.t)
		}
		close(start)
		wg.Wait()
		close(errs)
		for e := range errs {
			if e != nil {
				t.Fatal(e)
			}
		}
		var sum int64
		db.Model(&AuthorizationAllocation{}).Where("authorization_id=?", auth).Select("COALESCE(SUM(amount),0)").Scan(&sum)
		if sum != 100000 {
			t.Fatalf("concurrent allocation=%d", sum)
		}
	})
	t.Run("payment", func(t *testing.T) {
		db := billingDB(t)
		s, p, c, tariffID := seedBilling(t, db)
		x := invoiceFor(t, s, p, c, tariffID)
		x, _ = s.Issue(x.ID, 2)
		start := make(chan struct{})
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				<-start
				_, e := s.Pay(x.ID, PaymentRequest{Amount: 20000, PaymentMethod: "CASH", IdempotencyKey: fmt.Sprintf("concurrent-%d", n)}, 3)
				errs <- e
			}(i)
		}
		close(start)
		wg.Wait()
		close(errs)
		success, conflict := 0, 0
		for e := range errs {
			if e == nil {
				success++
			} else if isConflict(e) {
				conflict++
			} else {
				t.Fatal(e)
			}
		}
		var sum int64
		db.Model(&Payment{}).Where("invoice_id=?", x.ID).Select("COALESCE(SUM(amount),0)").Scan(&sum)
		if success != 1 || conflict != 1 || sum != 20000 {
			t.Fatalf("payment success=%d conflict=%d sum=%d", success, conflict, sum)
		}
	})
}

func TestPostgresCancelledDraftReleasesActAndAllocation(t *testing.T) {
	db := billingDB(t)
	p, m := seedPatient(t, db, "CANCEL")
	c := consultation(t, db, p, "A")
	seedAuthorization(t, db, p, m, c, authorization.StatusApproved, nil, 20000)
	s := NewService(db)
	tariffID := tariff(t, db, "CONSULTATION", "CANCEL", 20000)
	x := invoiceFor(t, s, p, c, tariffID)
	if _, e := s.Cancel(x.ID, "Erreur", 66); e != nil {
		t.Fatal(e)
	}
	x, _ = s.GetInvoice(x.ID)
	if x.Status != InvoiceCancelled || x.CancelledBy == nil || *x.CancelledBy != 66 {
		t.Fatalf("cancel JWT=%+v", x)
	}
	var allocations int64
	db.Model(&AuthorizationAllocation{}).Count(&allocations)
	if allocations != 0 {
		t.Fatalf("allocations=%d", allocations)
	}
	var cancelledEvents int64
	db.Model(&medical_records.MedicalTimelineEvent{}).Where("event_type='invoice_cancelled' AND created_by=?", 66).Count(&cancelledEvents)
	if cancelledEvents != 1 {
		t.Fatalf("cancel timeline=%d", cancelledEvents)
	}
	if _, e := s.CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{{ActType: "CONSULTATION", ReferenceID: c, TariffID: tariffID}}}, 1); e != nil {
		t.Fatalf("act not released: %v", e)
	}
}

func isConflict(e error) bool {
	var app *coreerrors.AppError
	return e != nil && errors.As(e, &app) && app.Status == 409
}
