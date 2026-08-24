package receivables

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/billing"
	"github.com/lallene/medcore-his/backend/internal/modules/cash"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type receivablePatient struct {
	ID            uint `gorm:"primaryKey"`
	CodePatient   string
	NumeroDossier string
	Nom           string
	Prenoms       string
}

func (receivablePatient) TableName() string { return "patients" }

func receivableSchemaDSN(dsn, schema string) string {
	if strings.Contains(dsn, "://") {
		separator := "?"
		if strings.Contains(dsn, "?") {
			separator = "&"
		}
		return dsn + separator + "search_path=" + url.QueryEscape(schema)
	}
	return dsn + " search_path=" + schema
}

func receivablePostgres(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL absent: tests PostgreSQL Receivables ignorés")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("receivables_%d", time.Now().UnixNano())
	if err = admin.Exec(`CREATE SCHEMA "` + schema + `"`).Error; err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(postgres.Open(receivableSchemaDSN(dsn, schema)), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() {
		_ = sqlDB.Close()
		_ = admin.Exec(`DROP SCHEMA IF EXISTS "` + schema + `" CASCADE`).Error
	})
	if err = db.AutoMigrate(&receivablePatient{}, &billing.Invoice{}, &billing.InvoiceLine{}, &billing.Payment{}, &cash.Receipt{}, &Metadata{}, &FollowUp{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func createReceivableInvoice(t *testing.T, db *gorm.DB, patient receivablePatient, number, status string, gross, insurance, patientAmount int64, coveragePending bool) billing.Invoice {
	t.Helper()
	if err := db.Create(&patient).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	inv := billing.Invoice{Number: number, PatientID: patient.ID, Status: status, GrossAmount: gross, InsuranceAmount: insurance, PatientAmount: patientAmount, BalanceAmount: patientAmount, CoveragePending: coveragePending, IssuedAt: &now, IssuedBy: &patient.ID, CreatedBy: 1, UpdatedBy: 1}
	if err := db.Create(&inv).Error; err != nil {
		t.Fatal(err)
	}
	line := billing.InvoiceLine{InvoiceID: inv.ID, TariffID: 1, ActType: "CONSULTATION", ReferenceID: inv.ID, ClinicalReferenceID: inv.ID, BillableKey: "REC-" + number, Description: "Acte test", Quantity: 1, UnitPrice: gross, GrossAmount: gross, InsuranceAmount: insurance, PatientAmount: patientAmount, CoverageResolution: "DIRECT", IsActive: true}
	if err := db.Create(&line).Error; err != nil {
		t.Fatal(err)
	}
	return inv
}

func TestPostgresPatientDebtLifecycleAndInsuranceExclusion(t *testing.T) {
	db := receivablePostgres(t)
	service := NewService(db)
	uninsured := createReceivableInvoice(t, db, receivablePatient{CodePatient: "REC-P1", NumeroDossier: "REC-D1", Nom: "Sans", Prenoms: "Assurance"}, "REC-INV-1", billing.InvoiceIssued, 20000, 0, 20000, false)
	insured := createReceivableInvoice(t, db, receivablePatient{CodePatient: "REC-P2", NumeroDossier: "REC-D2", Nom: "Avec", Prenoms: "Assurance"}, "REC-INV-2", billing.InvoicePartiallyPaid, 50000, 35000, 15000, false)
	_ = createReceivableInvoice(t, db, receivablePatient{CodePatient: "REC-P3", NumeroDossier: "REC-D3", Nom: "Couverture", Prenoms: "Attente"}, "REC-INV-3", billing.InvoiceIssued, 10000, 0, 10000, true)
	_ = createReceivableInvoice(t, db, receivablePatient{CodePatient: "REC-P4", NumeroDossier: "REC-D4", Nom: "Facture", Prenoms: "Annulee"}, "REC-INV-4", billing.InvoiceCancelled, 10000, 0, 10000, false)

	payment := billing.Payment{InvoiceID: insured.ID, Amount: 5000, PaymentMethod: "CASH", IdempotencyKey: "REC-PAY-1", PaidAt: time.Now(), ReceivedBy: 42}
	if err := db.Create(&payment).Error; err != nil {
		t.Fatal(err)
	}
	past := time.Now().AddDate(0, 0, -1)
	if err := db.Create(&Metadata{InvoiceID: insured.ID, PatientID: insured.PatientID, DueDate: &past, UpdatedBy: 42}).Error; err != nil {
		t.Fatal(err)
	}
	futureText := time.Now().AddDate(0, 0, 10).Format("2006-01-02")
	if _, err := service.SetDueDate(uninsured.ID, DueDateRequest{DueDate: &futureText}, 98); err != nil {
		t.Fatal(err)
	}
	var metadata Metadata
	if err := db.Where("invoice_id=?", uninsured.ID).First(&metadata).Error; err != nil || metadata.UpdatedBy != 98 {
		t.Fatalf("due-date JWT metadata=%+v err=%v", metadata, err)
	}

	page, err := service.List(Filter{Page: 1, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.Items) != 2 {
		t.Fatalf("active receivables=%d/%d, want 2", len(page.Items), page.Total)
	}
	byID := map[uint]Item{}
	for _, item := range page.Items {
		byID[item.InvoiceID] = item
	}
	if got := byID[uninsured.ID]; got.PatientBalance != 20000 || got.Status != "DUE" {
		t.Fatalf("uninsured debt=%+v", got)
	}
	if byID[uninsured.ID].PatientCode != "REC-P1" {
		t.Fatalf("patient code missing from projection: %+v", byID[uninsured.ID])
	}
	if got := byID[insured.ID]; got.PatientDue != 15000 || got.PatientPaid != 5000 || got.PatientBalance != 10000 || got.Status != "OVERDUE" {
		t.Fatalf("insured debt includes insurance or has wrong status: %+v", got)
	}

	before := byID[insured.ID].PatientBalance
	promised := int64(7000)
	follow, err := service.AddFollowUp(insured.ID, FollowUpRequest{ActionType: "PAYMENT_PROMISE", Note: "Promesse patient", PromisedAmount: &promised}, 99)
	if err != nil || follow.CreatedBy != 99 {
		t.Fatalf("follow-up JWT=%+v err=%v", follow, err)
	}
	detail, err := service.Detail(insured.ID)
	if err != nil || detail.PatientBalance != before || len(detail.FollowUps) != 1 {
		t.Fatalf("promise changed balance: %+v err=%v", detail, err)
	}

	final := billing.Payment{InvoiceID: insured.ID, Amount: 10000, PaymentMethod: "CARD", IdempotencyKey: "REC-PAY-2", PaidAt: time.Now(), ReceivedBy: 43}
	if err = db.Create(&final).Error; err != nil {
		t.Fatal(err)
	}
	page, err = service.List(Filter{Page: 1, Limit: 20})
	if err != nil || page.Total != 1 || page.Items[0].InvoiceID != uninsured.ID {
		t.Fatalf("paid receivable remained active: %+v err=%v", page, err)
	}
}
