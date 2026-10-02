package authorization

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
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/company"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/coverage"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/guarantor"
	"github.com/lallene/medcore-his/backend/internal/modules/medical_records"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type authorizationPerformedAct struct {
	ID                uint
	PatientID         uint
	ActCatalogEntryID uint
	ActCode           string
	ActLabel          string
	ActDescription    string
	ActCategory       string
	BasePrice         int64
	Currency          string
	Billable          bool
	InsuranceEligible bool
	Quantity          float64
	PerformedAt       time.Time
	PerformedBy       uint
	Status            string
	ConsultationID    *uint
	CreatedBy         uint
	UpdatedBy         uint
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

func (authorizationPerformedAct) TableName() string { return "performed_acts" }

func preparePerformedActDB(t *testing.T) (*gorm.DB, fixture) {
	t.Helper()
	db := authorizationDB(t)
	if err := db.AutoMigrate(&authorizationPerformedAct{}); err != nil {
		t.Fatal(err)
	}
	if err := EnsureAuthorizationIndexes(db); err != nil {
		t.Fatal(err)
	}
	return db, seedAuthorization(t, db)
}

// authorizationDBConcurrent opens a schema-isolated PG pool with multiple
// connections and per-connection search_path (same pattern as performed_acts).
// Required for real concurrent Create races; MaxOpenConns(1) would serialize.
func authorizationDBConcurrent(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL absent: tests PostgreSQL PEC ignorés")
	}
	dsn = strings.Replace(dsn, "-pooler", "", 1)
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("authorization_conc_%d", time.Now().UnixNano())
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
	sqlDB.SetMaxOpenConns(8)
	sqlDB.SetMaxIdleConns(8)
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
	// Same stub tables as authorizationDB: Create→FindByID→query LEFT JOINs
	// imaging_orders / laboratory_orders even for PERFORMED_ACT rows.
	if err = db.AutoMigrate(
		&patients.Patient{}, &company.InsuranceCompany{}, &guarantor.InsuranceGuarantor{},
		&coverage.PatientCoverage{}, &medical_records.MedicalRecord{}, &medical_records.MedicalTimelineEvent{},
		&authorizationConsultation{}, &authorizationMedicalExam{}, &authorizationLaboratoryOrder{},
		&authorizationImagingOrder{}, &authorizationHospitalization{}, &authorizationPrescription{},
		&InsuranceAuthorization{}, &InsuranceAuthorizationAct{}, &authorizationPerformedAct{},
	); err != nil {
		t.Fatal(err)
	}
	if err := EnsureAuthorizationIndexes(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func newPerformedAct(patientID uint, code string, eligible bool, status string) authorizationPerformedAct {
	return authorizationPerformedAct{
		PatientID: patientID, ActCatalogEntryID: 1, ActCode: code, ActLabel: code, ActCategory: "OTHER",
		BasePrice: 15000, Currency: "XOF", Billable: true, InsuranceEligible: eligible, Quantity: 1,
		PerformedAt: time.Now(), PerformedBy: 1, Status: status, CreatedBy: 1, UpdatedBy: 1,
	}
}

func TestPerformedActPECCreateHappyPath(t *testing.T) {
	db, f := preparePerformedActDB(t)
	act := newPerformedAct(f.patient.ID, "CONSULT-STD", true, "PERFORMED")
	act.ActLabel = "Consultation"
	act.ActCategory = "CONSULTATION"
	if err := db.Create(&act).Error; err != nil {
		t.Fatal(err)
	}
	s := NewService(db)
	amount := 15000.0
	created, err := s.Create(CreateRequest{
		PatientID: f.patient.ID, PatientCoverageID: f.coverage.ID,
		ReferenceType: ReferencePerformedAct, ReferenceID: act.ID, RequestedAmount: &amount,
	}, 51)
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != StatusDraft || created.ReferenceType != ReferencePerformedAct || created.ReferenceID != act.ID {
		t.Fatalf("created=%#v", created)
	}
	if created.InsuranceCompanyID != f.coverage.CompanyID || created.GuarantorID != f.coverage.GuarantorID {
		t.Fatalf("coverage snapshot company=%d guarantor=%d", created.InsuranceCompanyID, created.GuarantorID)
	}
	if created.PatientCoverageID != f.coverage.ID {
		t.Fatalf("coverage not explicit: %d", created.PatientCoverageID)
	}
	match, err := s.FindAuthorizationForAct(f.patient.ID, f.coverage.ID, ReferencePerformedAct, act.ID)
	if err != nil || match.MatchType != "DIRECT" || match.Authorization.ID != created.ID {
		t.Fatalf("for-act=%#v err=%v", match, err)
	}
}

func TestPerformedActPECEligibilityRejected(t *testing.T) {
	db, f := preparePerformedActDB(t)
	s := NewService(db)
	amount := 1000.0

	ineligible := newPerformedAct(f.patient.ID, "X", false, "PERFORMED")
	if err := db.Create(&ineligible).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&ineligible).Update("insurance_eligible", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(CreateRequest{
		PatientID: f.patient.ID, PatientCoverageID: f.coverage.ID,
		ReferenceType: ReferencePerformedAct, ReferenceID: ineligible.ID, RequestedAmount: &amount,
	}, 1); !IsConflict(err) {
		t.Fatalf("ineligible=%v", err)
	}

	voided := newPerformedAct(f.patient.ID, "Y", true, "VOIDED")
	db.Create(&voided)
	if _, err := s.Create(CreateRequest{
		PatientID: f.patient.ID, PatientCoverageID: f.coverage.ID,
		ReferenceType: ReferencePerformedAct, ReferenceID: voided.ID, RequestedAmount: &amount,
	}, 1); !IsConflict(err) {
		t.Fatalf("voided=%v", err)
	}

	if _, err := s.Create(CreateRequest{
		PatientID: f.patient.ID, PatientCoverageID: f.coverage.ID,
		ReferenceType: ReferencePerformedAct, ReferenceID: 999999, RequestedAmount: &amount,
	}, 1); !IsConflict(err) {
		t.Fatalf("missing=%v", err)
	}
}

func TestPerformedActPECOwnershipAndCoverage(t *testing.T) {
	db, f := preparePerformedActDB(t)
	s := NewService(db)
	amount := 1000.0
	act := newPerformedAct(f.patient.ID, "A", true, "PERFORMED")
	db.Create(&act)

	foreignCov := coverage.PatientCoverage{
		PatientID: f.other.ID, CompanyID: f.coverage.CompanyID, GuarantorID: f.coverage.GuarantorID,
		MemberNumber: "FOREIGN", IsActive: true,
	}
	db.Create(&foreignCov)
	if _, err := s.Create(CreateRequest{
		PatientID: f.patient.ID, PatientCoverageID: foreignCov.ID,
		ReferenceType: ReferencePerformedAct, ReferenceID: act.ID, RequestedAmount: &amount,
	}, 1); !IsConflict(err) {
		t.Fatalf("foreign coverage=%v", err)
	}

	foreignAct := newPerformedAct(f.other.ID, "B", true, "PERFORMED")
	db.Create(&foreignAct)
	if _, err := s.Create(CreateRequest{
		PatientID: f.patient.ID, PatientCoverageID: f.coverage.ID,
		ReferenceType: ReferencePerformedAct, ReferenceID: foreignAct.ID, RequestedAmount: &amount,
	}, 1); !IsConflict(err) {
		t.Fatalf("foreign act=%v", err)
	}

	inactive := coverage.PatientCoverage{
		PatientID: f.patient.ID, CompanyID: f.coverage.CompanyID, GuarantorID: f.coverage.GuarantorID,
		MemberNumber: "INACTIVE", IsActive: true,
	}
	db.Create(&inactive)
	if err := db.Model(&inactive).Update("is_active", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(CreateRequest{
		PatientID: f.patient.ID, PatientCoverageID: inactive.ID,
		ReferenceType: ReferencePerformedAct, ReferenceID: act.ID, RequestedAmount: &amount,
	}, 1); !IsConflict(err) {
		t.Fatalf("inactive coverage=%v", err)
	}

	past := time.Now().Add(-48 * time.Hour)
	expired := coverage.PatientCoverage{
		PatientID: f.patient.ID, CompanyID: f.coverage.CompanyID, GuarantorID: f.coverage.GuarantorID,
		MemberNumber: "EXPIRED", IsActive: true, ValidTo: &past,
	}
	db.Create(&expired)
	if _, err := s.Create(CreateRequest{
		PatientID: f.patient.ID, PatientCoverageID: expired.ID,
		ReferenceType: ReferencePerformedAct, ReferenceID: act.ID, RequestedAmount: &amount,
	}, 1); !IsConflict(err) {
		t.Fatalf("expired coverage=%v", err)
	}

	secondary := coverage.PatientCoverage{
		PatientID: f.patient.ID, CompanyID: f.coverage.CompanyID, GuarantorID: f.coverage.GuarantorID,
		MemberNumber: "SECONDARY", CoverageRate: 50, IsPrincipal: false, IsActive: true,
	}
	db.Create(&secondary)
	created, err := s.Create(CreateRequest{
		PatientID: f.patient.ID, PatientCoverageID: secondary.ID,
		ReferenceType: ReferencePerformedAct, ReferenceID: act.ID, RequestedAmount: &amount,
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if created.PatientCoverageID != secondary.ID || created.ContractRate != 50 {
		t.Fatalf("must use caller-selected secondary coverage: %#v", created)
	}
}

func TestPerformedActPECIdempotencyAndCancelledRecreate(t *testing.T) {
	db, f := preparePerformedActDB(t)
	act := newPerformedAct(f.patient.ID, "C", true, "PERFORMED")
	db.Create(&act)
	s := NewService(db)
	amount := 2000.0
	first, err := s.Create(CreateRequest{
		PatientID: f.patient.ID, PatientCoverageID: f.coverage.ID,
		ReferenceType: ReferencePerformedAct, ReferenceID: act.ID, RequestedAmount: &amount,
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(CreateRequest{
		PatientID: f.patient.ID, PatientCoverageID: f.coverage.ID,
		ReferenceType: ReferencePerformedAct, ReferenceID: act.ID, RequestedAmount: &amount,
	}, 1); !IsConflict(err) {
		t.Fatalf("duplicate=%v", err)
	}
	cancelled, err := s.Cancel(first.ID, 2, UnrestrictedAccess(2))
	if err != nil || cancelled.Status != StatusCancelled {
		t.Fatalf("cancel=%#v err=%v", cancelled, err)
	}
	second, err := s.Create(CreateRequest{
		PatientID: f.patient.ID, PatientCoverageID: f.coverage.ID,
		ReferenceType: ReferencePerformedAct, ReferenceID: act.ID, RequestedAmount: &amount,
	}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID == first.ID || second.Status != StatusDraft {
		t.Fatalf("cancelled recreate=%#v", second)
	}
}

func TestPostgresPerformedActPECConcurrentCreate(t *testing.T) {
	db := authorizationDBConcurrent(t)
	f := seedAuthorization(t, db)
	act := newPerformedAct(f.patient.ID, "D", true, "PERFORMED")
	db.Create(&act)
	s := NewService(db)
	amount := 3000.0
	const workers = 10
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, e := s.Create(CreateRequest{
				PatientID: f.patient.ID, PatientCoverageID: f.coverage.ID,
				ReferenceType: ReferencePerformedAct, ReferenceID: act.ID, RequestedAmount: &amount,
			}, 9)
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
		} else if IsConflict(e) {
			conflict++
		} else {
			t.Fatalf("unexpected err=%v", e)
		}
	}
	if ok != 1 || conflict != workers-1 {
		t.Fatalf("concurrent create ok=%d conflict=%d", ok, conflict)
	}
	var n int64
	if err := db.Model(&InsuranceAuthorization{}).
		Where("reference_type=? AND reference_id=? AND status<>?", ReferencePerformedAct, act.ID, StatusCancelled).
		Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("row count=%d", n)
	}
}

func TestPostgresPerformedActActiveReferenceUniqueIndex(t *testing.T) {
	db, f := preparePerformedActDB(t)
	var indexExists bool
	if err := db.Raw(`SELECT EXISTS (
		SELECT 1 FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relkind = 'i' AND c.relname = 'ux_insurance_authorizations_active_reference'
		  AND n.nspname = current_schema()
	)`).Scan(&indexExists).Error; err != nil || !indexExists {
		t.Fatalf("ux_insurance_authorizations_active_reference missing: exists=%v err=%v", indexExists, err)
	}
	if err := EnsureAuthorizationIndexes(db); err != nil {
		t.Fatalf("EnsureAuthorizationIndexes must be idempotent: %v", err)
	}
	act := newPerformedAct(f.patient.ID, "E", true, "PERFORMED")
	db.Create(&act)
	var mr medical_records.MedicalRecord
	if err := db.Where("patient_id=?", f.patient.ID).First(&mr).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	row := InsuranceAuthorization{
		AuthorizationNumber: "PEC-IDX-1", PatientID: f.patient.ID, MedicalRecordID: mr.ID,
		PatientCoverageID: f.coverage.ID, InsuranceCompanyID: f.coverage.CompanyID, GuarantorID: f.coverage.GuarantorID,
		ReferenceType: ReferencePerformedAct, ReferenceID: act.ID, RequestedAt: now, RequestedBy: 1,
		Status: StatusDraft, CreatedBy: 1, UpdatedBy: 1,
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	dup := row
	dup.ID = 0
	dup.AuthorizationNumber = "PEC-IDX-2"
	if err := db.Create(&dup).Error; err == nil {
		t.Fatal("partial unique index allowed duplicate active reference")
	}
	if err := db.Model(&row).Update("status", StatusCancelled).Error; err != nil {
		t.Fatal(err)
	}
	dup.ID = 0
	dup.AuthorizationNumber = "PEC-IDX-3"
	dup.Status = StatusDraft
	if err := db.Create(&dup).Error; err != nil {
		t.Fatalf("cancelled recreate via index: %v", err)
	}
}

func TestPerformedActPECLifecycleCompatible(t *testing.T) {
	db, f := preparePerformedActDB(t)
	act := newPerformedAct(f.patient.ID, "F", true, "PERFORMED")
	db.Create(&act)
	s := NewService(db)
	amount := 10000.0
	created, err := s.Create(CreateRequest{
		PatientID: f.patient.ID, PatientCoverageID: f.coverage.ID,
		ReferenceType: ReferencePerformedAct, ReferenceID: act.ID, RequestedAmount: &amount,
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	submitted, err := s.Submit(created.ID, SubmitRequest{}, 2, UnrestrictedAccess(2))
	if err != nil || submitted.Status != StatusSubmitted {
		t.Fatalf("submit=%#v err=%v", submitted, err)
	}
	pending, err := s.MarkPending(created.ID, 2, UnrestrictedAccess(2))
	if err != nil || pending.Status != StatusPending {
		t.Fatalf("pending=%#v err=%v", pending, err)
	}
	rate := 80.0
	decided, err := s.Decide(created.ID, DecisionRequest{
		Status: StatusApproved, ExternalReference: "ASSUR-PA-1", ExternalDecisionDate: "2026-10-01", ApprovedRate: &rate,
	}, 3, UnrestrictedAccess(3))
	if err != nil {
		t.Fatal(err)
	}
	if *decided.InsuranceAmount != 8000 || *decided.PatientAmount != 2000 {
		t.Fatalf("decide amounts=%#v", decided)
	}
}

func TestPerformedActPECDoesNotMutateBillingTables(t *testing.T) {
	db, f := preparePerformedActDB(t)
	// Real persistence tables from billing / insurance_receivables / cash modules.
	tables := []string{
		"billing_invoices",
		"billing_invoice_lines",
		"billing_authorization_allocations",
		"billing_payments",
		"insurance_receivable_followups",
		"insurance_receivable_metadata",
		"cash_receipts",
	}
	for _, table := range tables {
		if err := db.Exec("CREATE TABLE IF NOT EXISTS " + table + " (id bigserial primary key)").Error; err != nil {
			t.Fatal(err)
		}
	}
	act := newPerformedAct(f.patient.ID, "G", true, "PERFORMED")
	db.Create(&act)
	amount := 5000.0
	if _, err := NewService(db).Create(CreateRequest{
		PatientID: f.patient.ID, PatientCoverageID: f.coverage.ID,
		ReferenceType: ReferencePerformedAct, ReferenceID: act.ID, RequestedAmount: &amount,
	}, 1); err != nil {
		t.Fatal(err)
	}
	for _, table := range tables {
		var n int64
		if err := db.Table(table).Count(&n).Error; err != nil || n != 0 {
			t.Fatalf("%s mutated n=%d err=%v", table, n, err)
		}
	}
}

// TestMedicationPECStillUsesPrescriptionNotPerformedAct locks the pre-LOT27E
// MEDICATION contract: prescription-based ownership via consultation, never performed_acts.
func TestMedicationPECStillUsesPrescriptionNotPerformedAct(t *testing.T) {
	db, f := preparePerformedActDB(t)
	pa := newPerformedAct(f.patient.ID, "NOT-RX", true, "PERFORMED")
	if err := db.Create(&pa).Error; err != nil {
		t.Fatal(err)
	}
	rx := authorizationPrescription{ConsultationID: f.act.ID, MedicationName: "AMOXICILLINE", Dosage: "500 mg"}
	if err := db.Create(&rx).Error; err != nil {
		t.Fatal(err)
	}
	otherConsult := authorizationConsultation{PatientID: f.other.ID, Service: "Autre"}
	if err := db.Create(&otherConsult).Error; err != nil {
		t.Fatal(err)
	}
	foreignRx := authorizationPrescription{ConsultationID: otherConsult.ID, MedicationName: "FOREIGN", Dosage: "1"}
	if err := db.Create(&foreignRx).Error; err != nil {
		t.Fatal(err)
	}
	s := NewService(db)
	amount := 2500.0
	created, err := s.Create(CreateRequest{
		PatientID: f.patient.ID, PatientCoverageID: f.coverage.ID,
		ReferenceType: "MEDICATION", ReferenceID: rx.ID, RequestedAmount: &amount,
	}, 17)
	if err != nil {
		t.Fatal(err)
	}
	if created.ReferenceType != "MEDICATION" || created.ReferenceID != rx.ID {
		t.Fatalf("MEDICATION must persist prescription reference: %#v", created)
	}
	if _, err := s.Create(CreateRequest{
		PatientID: f.patient.ID, PatientCoverageID: f.coverage.ID,
		ReferenceType: "MEDICATION", ReferenceID: foreignRx.ID, RequestedAmount: &amount,
	}, 17); !IsConflict(err) {
		t.Fatalf("foreign prescription ownership must reject: %v", err)
	}
	if _, err := s.Create(CreateRequest{
		PatientID: f.patient.ID, PatientCoverageID: f.coverage.ID,
		ReferenceType: "MEDICATION", ReferenceID: pa.ID, RequestedAmount: &amount,
	}, 17); !IsConflict(err) {
		t.Fatalf("MEDICATION must not resolve performed_acts.id=%d: %v", pa.ID, err)
	}
}

func TestPerformedActEligibleActsDiscovery(t *testing.T) {
	db, f := preparePerformedActDB(t)
	eligible := newPerformedAct(f.patient.ID, "CONSULT-STD", true, "PERFORMED")
	eligible.ActLabel = "Consultation"
	eligible.ActCategory = "CONSULTATION"
	db.Create(&eligible)
	nope := newPerformedAct(f.patient.ID, "NOPE", true, "PERFORMED")
	db.Create(&nope)
	if err := db.Model(&nope).Update("insurance_eligible", false).Error; err != nil {
		t.Fatal(err)
	}
	voided := newPerformedAct(f.patient.ID, "VOID", true, "VOIDED")
	db.Create(&voided)
	s := NewService(db)
	rows, err := s.EligibleActs(f.patient.ID, f.coverage.ID, ReferencePerformedAct, "CONSULT-STD")
	if err != nil || len(rows) != 1 || rows[0].ReferenceID != eligible.ID {
		t.Fatalf("eligible=%#v err=%v", rows, err)
	}
}

func TestRBACPerformedActsCreateDoesNotImplyInsuranceCreate(t *testing.T) {
	has := func(perms []string, want string) bool {
		for _, p := range perms {
			if p == want {
				return true
			}
		}
		return false
	}
	for _, role := range []string{"INFIRMIER", "BIOLOGISTE", "RADIOLOGIE"} {
		perms := rbac.EffectiveStaffPermissions("", []string{role}, nil)
		if !has(perms, "performed_acts.create") {
			t.Fatalf("%s missing performed_acts.create", role)
		}
		if has(perms, "insurance.authorization.create") {
			t.Fatalf("%s must not gain insurance.authorization.create via performed_acts.create", role)
		}
	}
	phys := rbac.StaffPhysicianPermissions
	if !has(phys, "insurance.authorization.create") || !has(phys, "performed_acts.create") {
		t.Fatalf("physician matrix unexpected: %v", phys)
	}
}
