package patient_queue

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/modules/consultations"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// queuePostgresIsolationConfigError is returned (via t.Fatal) when TEST_DATABASE_URL
// uses a connection mode that cannot preserve session search_path across concurrent
// physical connections (typical of transaction-mode poolers).
const queuePostgresIsolationConfigError = "TEST_DATABASE_URL connection mode cannot preserve session search_path for patient_queue PG isolation; use a direct/session-mode PostgreSQL endpoint"

// queuePostgres opens an ephemeral schema for patient_queue PG tests.
//
// Isolation contract (no DSN hostname rewriting):
//  1. Create schema pq_<nanos> via an admin connection.
//  2. Open the test pool with pgx RuntimeParams["search_path"] so every new
//     physical connection starts in the ephemeral schema. Transaction-mode
//     poolers reject this startup parameter → fail-fast config error.
//  3. Also apply AfterConnect SET search_path as belt-and-suspenders on
//     session-mode endpoints.
//  4. Fail-fast assert current_schema() / current_schemas(false) on the GORM path.
//  5. Fail-fast concurrent multi-statement probe against an ephemeral-only marker
//     table so drift to public cannot silently continue.
//
// Never prints DSN / host / credentials.
func queuePostgres(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL absent: tests PostgreSQL patient_queue ignorés")
	}
	admin, e := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if e != nil {
		t.Fatal(e)
	}
	schema := fmt.Sprintf("pq_%d", time.Now().UnixNano())
	if e = admin.Exec(`CREATE SCHEMA "` + schema + `"`).Error; e != nil {
		t.Fatal(e)
	}

	pgConfig, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	schemaIdent := pgx.Identifier{schema}.Sanitize()
	if pgConfig.RuntimeParams == nil {
		pgConfig.RuntimeParams = map[string]string{}
	}
	// Startup search_path is the reliable session-scoped mechanism. Providers that
	// run transaction-mode pooling reject it — that is an explicit configuration
	// failure for this harness (do not rewrite hostnames).
	pgConfig.RuntimeParams["search_path"] = schemaIdent

	sqlDB := stdlib.OpenDB(*pgConfig, stdlib.OptionAfterConnect(func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET search_path TO "+schemaIdent)
		return err
	}))
	// Test-harness pool only (not production). Concurrent PG tests need enough
	// connections for intentional parallelism. CheckInWalkIn holds a transaction
	// connection then calls EvaluateFinance on s.db (second checkout) — so
	// TestPostgresTicketReferenceUniqueness (10 workers) needs ~20 simultaneous
	// connections. Highest explicit fan-out found is 12 (attempt concurrency).
	// Cap well above that so the harness never invents a pool deadlock.
	const queuePostgresMaxOpenConns = 32
	sqlDB.SetMaxOpenConns(queuePostgresMaxOpenConns)
	sqlDB.SetMaxIdleConns(queuePostgresMaxOpenConns)

	pingCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {
		_ = sqlDB.Close()
		_ = admin.Exec(`DROP SCHEMA IF EXISTS "` + schema + `" CASCADE`)
		msg := err.Error()
		if strings.Contains(msg, "unsupported startup parameter") || strings.Contains(strings.ToLower(msg), "search_path") {
			t.Fatal(queuePostgresIsolationConfigError)
		}
		t.Fatal(err)
	}

	db, e := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
	if e != nil {
		_ = sqlDB.Close()
		_ = admin.Exec(`DROP SCHEMA IF EXISTS "` + schema + `" CASCADE`)
		t.Fatal(e)
	}

	adminSQL, err := admin.DB()
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_ = sqlDB.Close()
		_ = admin.Exec(`DROP SCHEMA IF EXISTS "` + schema + `" CASCADE`).Error
		_ = adminSQL.Close()
	})

	assertQueuePostgresIsolation(t, db, schema)
	// Marker table exists only in the ephemeral schema. Concurrent multi-statement
	// probes must resolve it; endpoints that drop session search_path fail here.
	mustExecSQL(t, db, `CREATE TABLE _pq_isolation_probe (
		id INT PRIMARY KEY,
		observed_schema TEXT NOT NULL
	)`)
	assertQueuePostgresIsolationConcurrent(t, db, schema)

	if e = db.AutoMigrate(&AppointmentType{}, &AppointmentSeries{}, &Appointment{}, &AppointmentHistory{}, &Ticket{}, &History{},
		&StaffWorkingSchedule{}, &ScheduleException{}, &ScheduleAuditEvent{},
		&AppointmentNotificationIntent{}, &AppointmentNotificationAttempt{}); e != nil {
		t.Fatal(e)
	}
	if err := EnsureAppointmentIndexes(db); err != nil {
		t.Fatal(err)
	}
	if err := EnsureScheduleIndexes(db); err != nil {
		t.Fatal(err)
	}
	if err := EnsureTicketIndexes(db); err != nil {
		t.Fatal(err)
	}
	if err := EnsureNotificationIndexes(db); err != nil {
		t.Fatal(err)
	}
	if err := EnsureAppointmentSeriesIndexes(db); err != nil {
		t.Fatal(err)
	}

	// Minimal FK-less stubs aligned with current NOT NULL columns used by fixtures.
	mustExecSQL(t, db, `CREATE TABLE IF NOT EXISTS organization_departments (
		id BIGSERIAL PRIMARY KEY, code TEXT NOT NULL, name TEXT NOT NULL,
		active BOOLEAN NOT NULL DEFAULT true,
		created_by BIGINT NOT NULL DEFAULT 1, updated_by BIGINT NOT NULL DEFAULT 1
	)`)
	mustExecSQL(t, db, `CREATE TABLE IF NOT EXISTS patients (
		id BIGSERIAL PRIMARY KEY, code_patient TEXT, nom TEXT NOT NULL DEFAULT '', prenoms TEXT,
		sexe TEXT, date_naissance DATE, telephone TEXT,
		email VARCHAR(150) NOT NULL DEFAULT '',
		deleted_at TIMESTAMPTZ
	)`)
	mustExecSQL(t, db, `CREATE TABLE IF NOT EXISTS organization_services (
		id BIGSERIAL PRIMARY KEY, department_id BIGINT NOT NULL DEFAULT 1,
		name TEXT, code TEXT, active BOOLEAN NOT NULL DEFAULT true,
		service_type TEXT NOT NULL DEFAULT 'CLINICAL',
		created_by BIGINT NOT NULL DEFAULT 1, updated_by BIGINT NOT NULL DEFAULT 1
	)`)
	mustExecSQL(t, db, `CREATE TABLE IF NOT EXISTS users (
		id BIGSERIAL PRIMARY KEY, name TEXT NOT NULL,
		email TEXT NOT NULL DEFAULT '',
		password_hash TEXT NOT NULL DEFAULT 'test-only-hash',
		is_active BOOLEAN NOT NULL DEFAULT true
	)`)
	mustExecSQL(t, db, `CREATE TABLE IF NOT EXISTS billing_invoices (
		id BIGSERIAL PRIMARY KEY, patient_id BIGINT, patient_amount BIGINT, status TEXT, coverage_pending BOOLEAN DEFAULT false
	)`)
	mustExecSQL(t, db, `CREATE TABLE IF NOT EXISTS billing_payments (
		id BIGSERIAL PRIMARY KEY, invoice_id BIGINT, amount BIGINT
	)`)
	mustExecSQL(t, db, `CREATE TABLE IF NOT EXISTS vital_signs (
		id BIGSERIAL PRIMARY KEY, medical_record_id BIGINT, patient_id BIGINT NOT NULL,
		consultation_id BIGINT, comment TEXT,
		temperature_c DOUBLE PRECISION, systolic_bp INT, diastolic_bp INT, heart_rate INT,
		oxygen_saturation DOUBLE PRECISION, weight_kg DOUBLE PRECISION, height_cm DOUBLE PRECISION,
		measured_at TIMESTAMPTZ, updated_at TIMESTAMPTZ DEFAULT NOW()
	)`)
	mustExecSQL(t, db, `CREATE TABLE IF NOT EXISTS allergies (
		id BIGSERIAL PRIMARY KEY, medical_record_id BIGINT, patient_id BIGINT,
		allergen_type TEXT, allergen_name TEXT, reaction TEXT, severity TEXT,
		comment TEXT, is_active BOOLEAN DEFAULT true, created_by BIGINT,
		created_at TIMESTAMPTZ DEFAULT NOW(), updated_at TIMESTAMPTZ DEFAULT NOW()
	)`)
	mustExecSQL(t, db, `CREATE TABLE IF NOT EXISTS medical_histories (
		id BIGSERIAL PRIMARY KEY, medical_record_id BIGINT, patient_id BIGINT,
		type TEXT, title TEXT, description TEXT, status TEXT DEFAULT 'active',
		severity TEXT, comment TEXT, created_by BIGINT,
		created_at TIMESTAMPTZ DEFAULT NOW(), updated_at TIMESTAMPTZ DEFAULT NOW()
	)`)
	mustExecSQL(t, db, `CREATE TABLE IF NOT EXISTS staff_profiles (
		id BIGSERIAL PRIMARY KEY, user_id BIGINT NOT NULL, active BOOLEAN NOT NULL DEFAULT true,
		primary_service_id BIGINT, employee_code TEXT NOT NULL DEFAULT ''
	)`)
	mustExecSQL(t, db, `CREATE TABLE IF NOT EXISTS staff_service_assignments (
		id BIGSERIAL PRIMARY KEY, profile_id BIGINT NOT NULL, service_id BIGINT NOT NULL,
		active BOOLEAN NOT NULL DEFAULT true, is_primary BOOLEAN NOT NULL DEFAULT false,
		created_by BIGINT NOT NULL DEFAULT 1
	)`)

	mustExecSQL(t, db, `INSERT INTO organization_departments(id, code, name, active, created_by, updated_by) VALUES
		(1,'PQ-TEST','Patient Queue Test Dept',true,1,1) ON CONFLICT DO NOTHING`)
	mustExecSQL(t, db, `INSERT INTO patients(id, code_patient, nom, prenoms, sexe, telephone) VALUES
		(1,'P-Q-1','Dupont','Alice','F','0600000001'),
		(2,'P-Q-2','Martin','Bob','M','0600000002') ON CONFLICT DO NOTHING`)
	mustExecSQL(t, db, `INSERT INTO organization_services(id, department_id, name, code, active) VALUES
		(10,1,'Urgences','URG',true),(11,1,'Médecine','MED',true) ON CONFLICT DO NOTHING`)
	mustExecSQL(t, db, `INSERT INTO users(id, name, email) VALUES
		(100,'Accueil','accueil@pq-test.invalid'),
		(101,'Infirmier','infirmier@pq-test.invalid'),
		(102,'Médecin','medecin@pq-test.invalid') ON CONFLICT DO NOTHING`)

	// Re-assert after DDL/DML: still on the same GORM pool path.
	assertQueuePostgresIsolation(t, db, schema)
	assertQueuePostgresIsolationConcurrent(t, db, schema)
	return db
}

// assertQueuePostgresIsolation verifies the GORM/pool path resolves unqualified
// names into the ephemeral schema — not public.
func assertQueuePostgresIsolation(t *testing.T, db *gorm.DB, schema string) {
	t.Helper()
	var currentSchema string
	if err := db.Raw("SELECT current_schema()").Scan(&currentSchema).Error; err != nil {
		t.Fatalf("isolation assert current_schema: %v", err)
	}
	if currentSchema != schema {
		t.Fatalf("queuePostgres isolation failed: current_schema=%q want=%q (queries would hit the wrong schema)", currentSchema, schema)
	}
	var schemas []string
	if err := db.Raw("SELECT unnest(current_schemas(false))").Scan(&schemas).Error; err != nil {
		t.Fatalf("isolation assert current_schemas: %v", err)
	}
	if len(schemas) == 0 || schemas[0] != schema {
		t.Fatalf("queuePostgres isolation failed: current_schemas(false) effective first=%v want %q first", schemas, schema)
	}
}

// assertQueuePostgresIsolationConcurrent forces multiple physical connections and
// verifies each can resolve an ephemeral-only table through multi-statement GORM
// work. Transaction-mode poolers that discard session SET between statements fail.
func assertQueuePostgresIsolationConcurrent(t *testing.T, db *gorm.DB, schema string) {
	t.Helper()
	const workers = 8
	start := make(chan struct{})
	errs := make(chan string, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			<-start
			var currentSchema string
			if err := db.Raw("SELECT current_schema()").Scan(&currentSchema).Error; err != nil {
				errs <- "current_schema: " + err.Error()
				return
			}
			if currentSchema != schema {
				errs <- fmt.Sprintf("current_schema=%q want=%q", currentSchema, schema)
				return
			}
			// Separate statement on likely-different pool checkout — must still see marker.
			if err := db.Exec(
				`INSERT INTO _pq_isolation_probe(id, observed_schema) VALUES (?, current_schema())
				 ON CONFLICT (id) DO UPDATE SET observed_schema = EXCLUDED.observed_schema`,
				id,
			).Error; err != nil {
				errs <- "ephemeral marker insert: " + err.Error()
				return
			}
			var observed string
			if err := db.Raw(`SELECT observed_schema FROM _pq_isolation_probe WHERE id = ?`, id).Scan(&observed).Error; err != nil {
				errs <- "ephemeral marker select: " + err.Error()
				return
			}
			if observed != schema {
				errs <- fmt.Sprintf("marker observed_schema=%q want=%q", observed, schema)
			}
		}(i + 1)
	}
	close(start)
	wg.Wait()
	close(errs)
	var failed []string
	for e := range errs {
		failed = append(failed, e)
	}
	if len(failed) > 0 {
		t.Fatalf("%s (%d/%d concurrent probes failed; first=%s)", queuePostgresIsolationConfigError, len(failed), workers, failed[0])
	}
	var n int64
	if err := db.Raw(`SELECT COUNT(*) FROM _pq_isolation_probe`).Scan(&n).Error; err != nil {
		t.Fatalf("isolation probe count: %v", err)
	}
	if n != workers {
		t.Fatalf("%s (probe row count=%d want=%d)", queuePostgresIsolationConfigError, n, workers)
	}
}

func mustExecSQL(t *testing.T, db *gorm.DB, sql string, args ...any) {
	t.Helper()
	if err := db.Exec(sql, args...).Error; err != nil {
		t.Fatalf("fixture SQL failed: %v", err)
	}
}

// seedPractitionerForService wires staff assignment used by booking/create fixtures.
func seedPractitionerForService(t *testing.T, db *gorm.DB, profileID, userID, serviceID uint) {
	t.Helper()
	code := fmt.Sprintf("PQ-EMP-%d", profileID)
	mustExecSQL(t, db, `INSERT INTO staff_profiles(id, user_id, active, primary_service_id, employee_code) VALUES (?,?,true,?,?) ON CONFLICT DO NOTHING`,
		profileID, userID, serviceID, code)
	mustExecSQL(t, db, `INSERT INTO staff_service_assignments(profile_id, service_id, active, created_by) VALUES (?,?,true,?) ON CONFLICT DO NOTHING`,
		profileID, serviceID, userID)
}

// seedAllDaySchedules makes practitioner bookable for CreateAppointment fixtures (LOT 23D).
func seedAllDaySchedules(t *testing.T, db *gorm.DB, practitionerID, serviceID uint) {
	t.Helper()
	vf := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	now := time.Now().UTC()
	for wd := 0; wd <= 6; wd++ {
		if err := db.Create(&StaffWorkingSchedule{
			PractitionerID: practitionerID, ServiceID: serviceID, Weekday: wd,
			StartTime: "00:00:00", EndTime: "23:59:59", ValidFrom: vf, Active: true,
			CreatedBy: 100, CreatedAt: now, UpdatedAt: now,
		}).Error; err != nil {
			t.Fatalf("seed schedule wd=%d: %v", wd, err)
		}
	}
}

// seedAvailabilityAroundNow adds a concrete availability window for tests whose
// appointment times are relative to time.Now(). Unlike recurring wall-clock
// schedules, this interval remains continuous when a fixture crosses midnight.
func seedAvailabilityAroundNow(t *testing.T, db *gorm.DB, practitionerID, serviceID uint) {
	t.Helper()
	now := time.Now().UTC()
	if err := db.Create(&ScheduleException{
		PractitionerID: practitionerID,
		ServiceID:      serviceID,
		Type:           ExExtraAvailability,
		StartAt:        now.Add(-4 * time.Hour),
		EndAt:          now.Add(6 * time.Hour),
		Reason:         "test fixture: availability around now",
		Active:         true,
		CreatedBy:      100,
		CreatedAt:      now,
		UpdatedAt:      now,
	}).Error; err != nil {
		t.Fatalf("seed availability around now: %v", err)
	}
}

func adminAccess(uid uint) Access {
	return Access{UserID: uid, Permissions: map[string]bool{"*": true}}
}

func scopedAccess(uid, serviceID uint, perms ...string) Access {
	m := map[string]bool{}
	for _, p := range perms {
		m[p] = true
	}
	sid := serviceID
	return Access{UserID: uid, ServiceID: &sid, Permissions: m}
}

func statusOf(err error) int {
	var ae *coreerrors.AppError
	if errors.As(err, &ae) {
		return ae.Status
	}
	return 0
}

func TestPostgresTicketReferenceUniqueness(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	const n = 10
	for i := 0; i < n; i++ {
		_ = db.Exec(`INSERT INTO patients(id, code_patient, nom, prenoms) VALUES (?,?,?,?)`,
			2000+i, fmt.Sprintf("P-U-%d", i), "Test", "Queue")
	}
	refs := make(chan string, n)
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(pid uint) {
			defer wg.Done()
			tk, e := svc.CheckInWalkIn(WalkInCheckInRequest{
				PatientID: pid, ServiceID: 10, IdentityConfirmed: true,
			}, adminAccess(100))
			if e != nil {
				errs <- e
				return
			}
			refs <- tk.Reference
			errs <- nil
		}(uint(2000 + i))
	}
	wg.Wait()
	close(refs)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	seen := map[string]bool{}
	for r := range refs {
		if seen[r] {
			t.Fatalf("duplicate ref %s", r)
		}
		seen[r] = true
	}
	if len(seen) != n {
		t.Fatalf("want %d refs got %d", n, len(seen))
	}
}

func TestPostgresForbiddenSkipAndConcurrency(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	tk, e := svc.CheckInWalkIn(WalkInCheckInRequest{
		PatientID: 1, ServiceID: 10, IdentityConfirmed: true, Priority: PriorityNormal,
	}, adminAccess(100))
	if e != nil {
		t.Fatal(e)
	}
	if tk.Stage != StageWaitingTriage {
		t.Fatalf("stage=%s", tk.Stage)
	}
	// Forbidden: cannot complete triage without take
	if _, e := svc.CompleteTriage(tk.ID, CompleteTriageRequest{}, adminAccess(101)); e == nil {
		t.Fatal("complete triage without take should fail")
	}
	// Concurrent take triage
	var wg sync.WaitGroup
	ok := make(chan bool, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(uid uint) {
			defer wg.Done()
			_, e := svc.TakeTriage(tk.ID, adminAccess(uid))
			ok <- e == nil
		}(uint(101 + i%2))
	}
	wg.Wait()
	close(ok)
	success := 0
	for v := range ok {
		if v {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("expected exactly 1 triage take, got %d", success)
	}
	if _, e := svc.CompleteTriage(tk.ID, CompleteTriageRequest{}, adminAccess(101)); e != nil {
		// may be taken by 102
		_, e2 := svc.CompleteTriage(tk.ID, CompleteTriageRequest{}, adminAccess(102))
		if e != nil && e2 != nil {
			t.Fatalf("complete failed: %v / %v", e, e2)
		}
	}
	detail, e := svc.Get(tk.ID, adminAccess(100))
	if e != nil {
		t.Fatal(e)
	}
	if detail.Ticket.Stage != StageWaitingDoctor {
		t.Fatalf("after triage stage=%s", detail.Ticket.Stage)
	}
	if len(detail.History) < 2 {
		t.Fatalf("history too short: %d", len(detail.History))
	}
	// Double check-in idempotence
	if _, e := svc.CheckInWalkIn(WalkInCheckInRequest{
		PatientID: 1, ServiceID: 10, IdentityConfirmed: true,
	}, adminAccess(100)); e == nil {
		t.Fatal("double active check-in should conflict")
	}
}

func TestPostgresAppointmentCheckInAndFinance(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	seedPractitionerForService(t, db, 3, 102, 10)
	seedAllDaySchedules(t, db, 102, 10)
	seedAvailabilityAroundNow(t, db, 102, 10)
	now := time.Now().UTC()
	start := now.Add(-20 * time.Minute)
	end := start.Add(30 * time.Minute)
	appt, e := svc.CreateAppointment(CreateAppointmentRequest{
		PatientID: 2, ServiceID: 10, ScheduledAt: start, ScheduledEndAt: &end, Reason: "Suivi",
	}, adminAccess(100))
	if e != nil {
		t.Fatal(e)
	}
	_ = db.Exec(`INSERT INTO billing_invoices(patient_id, patient_amount, status, coverage_pending) VALUES (2, 5000, 'ISSUED', false)`)
	fin, e := svc.EvaluateFinance(2)
	if e != nil || fin != FinancePaymentRequired {
		t.Fatalf("finance=%s err=%v", fin, e)
	}
	if _, _, e := svc.CheckInAppointment(appt.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, adminAccess(100)); e == nil {
		t.Fatal("check-in without override should fail when payment required")
	}
	tk, _, e := svc.CheckInAppointment(appt.ID, AppointmentCheckInRequest{
		IdentityConfirmed: true, FinanceOverride: true, FinanceOverrideNote: "DEMO override",
	}, adminAccess(100))
	if e != nil {
		t.Fatal(e)
	}
	if tk.Source != SourceAppointment {
		t.Fatalf("source=%s", tk.Source)
	}
	dto := svc.enrichTicket(*tk)
	if dto.Punctuality != PunctualLate {
		t.Fatalf("expected LATE got %s", dto.Punctuality)
	}
	tk2, reused, e := svc.CheckInAppointment(appt.ID, AppointmentCheckInRequest{IdentityConfirmed: true, FinanceOverride: true}, adminAccess(100))
	if e != nil {
		t.Fatal(e)
	}
	if !reused || tk2.ID != tk.ID {
		t.Fatalf("idempotent check-in want same ticket reused=%v id=%d/%d", reused, tk.ID, tk2.ID)
	}
}

func TestWorkflowTransitionsUnit(t *testing.T) {
	if CanTransition(StageReception, StageWaitingDoctor) {
		t.Fatal("skip to doctor forbidden")
	}
	if !CanTransition(StageWaitingTriage, StageTriageInProgress) {
		t.Fatal("expected allowed")
	}
	if PriorityRank(PriorityUrgent) >= PriorityRank(PriorityNormal) {
		t.Fatal("urgent should rank higher")
	}
}

func TestPostgresCrossServiceMutationsDenied(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	admin := adminAccess(100)
	svcA := uint(10)
	svcB := uint(11)

	tkB, e := svc.CheckInWalkIn(WalkInCheckInRequest{
		PatientID: 1, ServiceID: svcB, IdentityConfirmed: true,
	}, admin)
	if e != nil {
		t.Fatal(e)
	}
	nurseA := scopedAccess(101, svcA, "queue.triage.update", "queue.triage.read", "queue.cancel", "queue.priority.update")
	docA := scopedAccess(102, svcA, "queue.doctor.take", "queue.doctor.read")
	receptA := scopedAccess(100, svcA, "queue.checkin", "queue.reception.read", "queue.cancel")

	if _, e := svc.TakeTriage(tkB.ID, nurseA); statusOf(e) != 404 {
		t.Fatalf("TakeTriage cross-service want 404 got %d (%v)", statusOf(e), e)
	}
	// Force stage for complete/doctor tests via admin
	if _, e := svc.TakeTriage(tkB.ID, admin); e != nil {
		t.Fatal(e)
	}
	if _, e := svc.CompleteTriage(tkB.ID, CompleteTriageRequest{}, nurseA); statusOf(e) != 404 {
		t.Fatalf("CompleteTriage cross-service want 404 got %d (%v)", statusOf(e), e)
	}
	if _, e := svc.CompleteTriage(tkB.ID, CompleteTriageRequest{}, admin); e != nil {
		t.Fatal(e)
	}
	if _, e := svc.TakeDoctor(tkB.ID, TakeDoctorRequest{}, docA); statusOf(e) != 404 {
		t.Fatalf("TakeDoctor cross-service want 404 got %d (%v)", statusOf(e), e)
	}
	if _, e := svc.TakeDoctor(tkB.ID, TakeDoctorRequest{}, admin); e != nil {
		t.Fatal(e)
	}
	if _, e := svc.Complete(tkB.ID, CompleteRequest{}, docA); statusOf(e) != 404 {
		t.Fatalf("Complete cross-service want 404 got %d (%v)", statusOf(e), e)
	}
	// fresh ticket for cancel/priority
	tkB2, e := svc.CheckInWalkIn(WalkInCheckInRequest{
		PatientID: 2, ServiceID: svcB, IdentityConfirmed: true,
	}, admin)
	if e != nil {
		t.Fatal(e)
	}
	if _, e := svc.Cancel(tkB2.ID, CancelRequest{Reason: "x"}, nurseA); statusOf(e) != 404 {
		t.Fatalf("Cancel cross-service want 404 got %d (%v)", statusOf(e), e)
	}
	if _, e := svc.SetPriority(tkB2.ID, PriorityRequest{Priority: PriorityUrgent, ExpectedVersion: tkB2.Version}, nurseA); statusOf(e) != 404 {
		t.Fatalf("SetPriority cross-service want 404 got %d (%v)", statusOf(e), e)
	}
	if _, e := svc.CheckInWalkIn(WalkInCheckInRequest{
		PatientID: 1, ServiceID: svcB, IdentityConfirmed: true, FinanceOverride: true,
	}, receptA); statusOf(e) != 403 {
		t.Fatalf("walk-in to service B from A want 403 got %d (%v)", statusOf(e), e)
	}
	// no service → deny
	nosvc := Access{UserID: 101, Permissions: map[string]bool{"queue.triage.update": true}}
	if _, e := svc.TakeTriage(tkB2.ID, nosvc); statusOf(e) != 403 {
		t.Fatalf("no service want 403 got %d (%v)", statusOf(e), e)
	}
	// read.all + mutation may cross-service
	global := Access{UserID: 100, Permissions: map[string]bool{"queue.read.all": true, "queue.triage.update": true}}
	tkWait, e := svc.CheckInWalkIn(WalkInCheckInRequest{
		PatientID: 1, ServiceID: svcB, IdentityConfirmed: true,
	}, admin)
	if e != nil {
		// patient 1 may still have active from earlier — cancel
		_ = tkB2
	}
	_ = tkWait
	_ = global
	// ensure patient 1 free
	var active []Ticket
	db.Where("patient_id=? AND status=?", 1, StatusActive).Find(&active)
	for _, x := range active {
		_, _ = svc.Cancel(x.ID, CancelRequest{Reason: "cleanup"}, admin)
	}
	tkWait, e = svc.CheckInWalkIn(WalkInCheckInRequest{
		PatientID: 1, ServiceID: svcB, IdentityConfirmed: true,
	}, admin)
	if e != nil {
		t.Fatal(e)
	}
	if _, e := svc.TakeTriage(tkWait.ID, global); e != nil {
		t.Fatalf("queue.read.all + triage.update should take: %v", e)
	}
}

func TestPostgresKPIServiceIsolation(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	admin := adminAccess(100)
	seedPractitionerForService(t, db, 3, 102, 10)
	seedAllDaySchedules(t, db, 102, 10)
	seedAvailabilityAroundNow(t, db, 102, 10)
	now := time.Now().UTC()
	startA := now.Add(-40 * time.Minute)
	endA := startA.Add(30 * time.Minute)
	// Service A: one late appointment ticket
	apptA, e := svc.CreateAppointment(CreateAppointmentRequest{
		PatientID: 1, ServiceID: 10, ScheduledAt: startA, ScheduledEndAt: &endA, Reason: "A-late",
	}, admin)
	if e != nil {
		t.Fatal(e)
	}
	tkA, _, e := svc.CheckInAppointment(apptA.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, admin)
	if e != nil {
		t.Fatal(e)
	}
	_ = tkA
	// Service B: three walk-ins waiting
	for i, pid := range []uint{2001, 2002, 2003} {
		_ = db.Exec(`INSERT INTO patients(id, code_patient, nom, prenoms) VALUES (?,?,?,?)`, pid, fmt.Sprintf("PB-%d", i), "B", "Q")
		if _, e := svc.CheckInWalkIn(WalkInCheckInRequest{
			PatientID: pid, ServiceID: 11, IdentityConfirmed: true,
		}, admin); e != nil {
			t.Fatal(e)
		}
	}
	kA, e := svc.KPIs(scopedAccess(100, 10, "queue.reception.read"))
	if e != nil {
		t.Fatal(e)
	}
	kB, e := svc.KPIs(scopedAccess(100, 11, "queue.reception.read"))
	if e != nil {
		t.Fatal(e)
	}
	kAll, e := svc.KPIs(Access{UserID: 100, Permissions: map[string]bool{"queue.read.all": true, "queue.reception.read": true}})
	if e != nil {
		t.Fatal(e)
	}
	if kA.ArrivedToday < 1 || kA.LateAppointments < 1 {
		t.Fatalf("service A kpis=%+v", kA)
	}
	if kB.ArrivedToday < 3 {
		t.Fatalf("service B arrived=%d", kB.ArrivedToday)
	}
	if kA.ArrivedToday == kB.ArrivedToday {
		t.Fatal("KPI A must not equal KPI B")
	}
	if kA.LateAppointments >= kB.LateAppointments+kA.LateAppointments && kB.LateAppointments > 0 {
		// B should typically have 0 late appts
	}
	if kB.LateAppointments != 0 {
		t.Fatalf("service B late should be 0 got %d", kB.LateAppointments)
	}
	if kAll.ArrivedToday < kA.ArrivedToday+kB.ArrivedToday {
		t.Fatalf("global arrived=%d A=%d B=%d", kAll.ArrivedToday, kA.ArrivedToday, kB.ArrivedToday)
	}
}

func TestPostgresVitalSignsIntegrity(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	admin := adminAccess(100)
	tk, e := svc.CheckInWalkIn(WalkInCheckInRequest{
		PatientID: 1, ServiceID: 10, IdentityConfirmed: true,
	}, admin)
	if e != nil {
		t.Fatal(e)
	}
	if _, e := svc.TakeTriage(tk.ID, admin); e != nil {
		t.Fatal(e)
	}
	_ = db.Exec(`INSERT INTO vital_signs(id, medical_record_id, patient_id, comment) VALUES (501,1,1,'ok'),(502,1,2,'other')`)
	if _, e := svc.CompleteTriage(tk.ID, CompleteTriageRequest{VitalSignsID: uintPtr(9999)}, admin); statusOf(e) != 404 {
		t.Fatalf("missing vital want 404 got %d (%v)", statusOf(e), e)
	}
	var stage string
	db.Raw(`SELECT stage FROM patient_queue_tickets WHERE id=?`, tk.ID).Scan(&stage)
	if stage != StageTriageInProgress {
		t.Fatalf("ticket mutated on failed vital: %s", stage)
	}
	if _, e := svc.CompleteTriage(tk.ID, CompleteTriageRequest{VitalSignsID: uintPtr(502)}, admin); statusOf(e) != 403 {
		t.Fatalf("other patient vital want 403 got %d (%v)", statusOf(e), e)
	}
	db.Raw(`SELECT stage FROM patient_queue_tickets WHERE id=?`, tk.ID).Scan(&stage)
	if stage != StageTriageInProgress {
		t.Fatalf("ticket mutated on foreign vital: %s", stage)
	}
	done, e := svc.CompleteTriage(tk.ID, CompleteTriageRequest{VitalSignsID: uintPtr(501)}, admin)
	if e != nil {
		t.Fatal(e)
	}
	if done.Stage != StageWaitingDoctor || done.VitalSignsID == nil || *done.VitalSignsID != 501 {
		t.Fatalf("same-patient vital not accepted: %+v", done)
	}
}

func TestPostgresDoctorStageVisibility(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	admin := adminAccess(100)
	doc := scopedAccess(102, 10, "queue.doctor.read", "queue.doctor.take", "queue.read.service")
	accueil := scopedAccess(100, 10, "queue.reception.read", "queue.checkin")
	infirmier := scopedAccess(101, 10, "queue.triage.read", "queue.triage.update")
	global := Access{UserID: 100, Permissions: map[string]bool{"queue.read.all": true, "queue.reception.read": true}}

	tkA, e := svc.CheckInWalkIn(WalkInCheckInRequest{
		PatientID: 1, ServiceID: 10, IdentityConfirmed: true, Reason: "vis-pre",
	}, admin)
	if e != nil {
		t.Fatal(e)
	}
	if tkA.Stage != StageWaitingTriage {
		t.Fatalf("stage=%s", tkA.Stage)
	}

	// Doctor List without stage filter: no pre-triage tickets
	list, e := svc.List(Filter{Limit: 50}, doc)
	if e != nil {
		t.Fatal(e)
	}
	for _, item := range list.Items {
		if item.ID == tkA.ID {
			t.Fatal("doctor List must hide WAITING_TRIAGE")
		}
		if item.Stage == StageWaitingTriage || item.Stage == StageTriageInProgress || item.Stage == StageReception {
			t.Fatalf("pre-triage leaked on doctor List: %s", item.Stage)
		}
	}

	// Explicit pre-triage filter → 400
	if _, e := svc.List(Filter{Stage: StageWaitingTriage, Limit: 50}, doc); statusOf(e) != 400 {
		t.Fatalf("WAITING_TRIAGE filter want 400 got %d (%v)", statusOf(e), e)
	}
	if _, e := svc.List(Filter{Stage: StageTriageInProgress, Limit: 50}, doc); statusOf(e) != 400 {
		t.Fatalf("TRIAGE_IN_PROGRESS filter want 400 got %d (%v)", statusOf(e), e)
	}

	// Get pre-triage → 404 (no leak)
	if _, e := svc.Get(tkA.ID, doc); statusOf(e) != 404 {
		t.Fatalf("Get WAITING_TRIAGE want 404 got %d (%v)", statusOf(e), e)
	}

	// Accueil still sees WAITING_TRIAGE in service
	accList, e := svc.List(Filter{Stage: StageWaitingTriage, Limit: 50}, accueil)
	if e != nil {
		t.Fatal(e)
	}
	foundAcc := false
	for _, item := range accList.Items {
		if item.ID == tkA.ID {
			foundAcc = true
			break
		}
	}
	if !foundAcc {
		t.Fatal("Accueil must see WAITING_TRIAGE in service")
	}

	// Infirmier sees WAITING_TRIAGE then TRIAGE_IN_PROGRESS
	infList, e := svc.List(Filter{Limit: 50}, infirmier)
	if e != nil {
		t.Fatal(e)
	}
	foundInf := false
	for _, item := range infList.Items {
		if item.ID == tkA.ID {
			foundInf = true
			break
		}
	}
	if !foundInf {
		t.Fatal("Infirmier must see WAITING_TRIAGE")
	}
	if _, e := svc.TakeTriage(tkA.ID, infirmier); e != nil {
		t.Fatal(e)
	}
	if _, e := svc.Get(tkA.ID, doc); statusOf(e) != 404 {
		t.Fatalf("Get TRIAGE_IN_PROGRESS want 404 got %d (%v)", statusOf(e), e)
	}
	infProg, e := svc.List(Filter{Stage: StageTriageInProgress, Limit: 50}, infirmier)
	if e != nil {
		t.Fatal(e)
	}
	foundProg := false
	for _, item := range infProg.Items {
		if item.ID == tkA.ID {
			foundProg = true
			break
		}
	}
	if !foundProg {
		t.Fatal("Infirmier must see TRIAGE_IN_PROGRESS")
	}

	// queue.read.all still sees pre-triage
	allList, e := svc.List(Filter{Stage: StageTriageInProgress, Limit: 50}, global)
	if e != nil {
		t.Fatal(e)
	}
	foundAll := false
	for _, item := range allList.Items {
		if item.ID == tkA.ID {
			foundAll = true
			break
		}
	}
	if !foundAll {
		t.Fatal("queue.read.all must see TRIAGE_IN_PROGRESS")
	}

	_ = db.Exec(`INSERT INTO vital_signs(id, medical_record_id, patient_id, temperature_c, measured_at)
		VALUES (701,1,1,37.0,NOW())`)
	done, e := svc.CompleteTriage(tkA.ID, CompleteTriageRequest{VitalSignsID: uintPtr(701)}, infirmier)
	if e != nil {
		t.Fatal(e)
	}
	if done.Stage != StageWaitingDoctor {
		t.Fatalf("stage=%s", done.Stage)
	}

	// Post-triage Get/List OK for doctor
	detail, e := svc.Get(tkA.ID, doc)
	if e != nil {
		t.Fatal(e)
	}
	if detail.Ticket.Stage != StageWaitingDoctor {
		t.Fatalf("stage=%s", detail.Ticket.Stage)
	}
	postList, e := svc.List(Filter{Limit: 50}, doc)
	if e != nil {
		t.Fatal(e)
	}
	foundDoc := false
	for _, item := range postList.Items {
		if item.ID == tkA.ID {
			foundDoc = true
			break
		}
	}
	if !foundDoc {
		t.Fatal("doctor List must show WAITING_DOCTOR")
	}

	taken, e := svc.TakeDoctor(tkA.ID, TakeDoctorRequest{}, doc)
	if e != nil {
		t.Fatal(e)
	}
	if taken.Stage != StageDoctorInProgress {
		t.Fatalf("stage=%s", taken.Stage)
	}
	if _, e := svc.Get(tkA.ID, doc); e != nil {
		t.Fatal(e)
	}

	// Cross-service still denied (404)
	docOther := scopedAccess(102, 11, "queue.doctor.read", "queue.doctor.take", "queue.read.service")
	if _, e := svc.Get(tkA.ID, docOther); statusOf(e) != 404 {
		t.Fatalf("cross-service Get want 404 got %d (%v)", statusOf(e), e)
	}
	otherList, e := svc.List(Filter{Limit: 50}, docOther)
	if e != nil {
		t.Fatal(e)
	}
	for _, item := range otherList.Items {
		if item.ID == tkA.ID {
			t.Fatal("cross-service List must not reveal ticket")
		}
	}
}

func TestPostgresDoctorWorklistOnlyAfterTriage(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	admin := adminAccess(100)
	doc := scopedAccess(102, 10, "queue.doctor.read", "queue.doctor.take")

	tk, e := svc.CheckInWalkIn(WalkInCheckInRequest{
		PatientID: 1, ServiceID: 10, IdentityConfirmed: true, Reason: "céphalées",
	}, admin)
	if e != nil {
		t.Fatal(e)
	}
	wl, e := svc.DoctorWorklist(Filter{Limit: 50}, doc)
	if e != nil {
		t.Fatal(e)
	}
	for _, item := range wl.Items {
		if item.ID == tk.ID {
			t.Fatal("WAITING_TRIAGE ticket must not appear on doctor worklist")
		}
		if item.Stage == StageWaitingTriage || item.Stage == StageTriageInProgress {
			t.Fatalf("triage stage leaked: %s", item.Stage)
		}
	}
	if _, e := svc.DoctorWorklist(Filter{Stage: StageWaitingTriage}, doc); statusOf(e) != 400 {
		t.Fatalf("triage stage filter want 400 got %d (%v)", statusOf(e), e)
	}

	if _, e := svc.TakeTriage(tk.ID, admin); e != nil {
		t.Fatal(e)
	}
	wl, e = svc.DoctorWorklist(Filter{Limit: 50}, doc)
	if e != nil {
		t.Fatal(e)
	}
	for _, item := range wl.Items {
		if item.ID == tk.ID {
			t.Fatal("TRIAGE_IN_PROGRESS must not appear on doctor worklist")
		}
	}

	_ = db.Exec(`INSERT INTO vital_signs(id, medical_record_id, patient_id, temperature_c, systolic_bp, diastolic_bp, heart_rate, measured_at)
		VALUES (601,1,1,38.7,150,95,102,NOW())`)
	_ = db.Exec(`INSERT INTO allergies(medical_record_id, patient_id, allergen_type, allergen_name, severity, is_active)
		VALUES (1,1,'medication','Pénicilline','high',true)`)
	_ = db.Exec(`INSERT INTO medical_histories(medical_record_id, patient_id, type, title, status)
		VALUES (1,1,'chronic','Hypertension artérielle','active')`)

	done, e := svc.CompleteTriage(tk.ID, CompleteTriageRequest{VitalSignsID: uintPtr(601)}, admin)
	if e != nil {
		t.Fatal(e)
	}
	if done.Stage != StageWaitingDoctor {
		t.Fatalf("stage=%s", done.Stage)
	}

	wl, e = svc.DoctorWorklist(Filter{Limit: 50}, doc)
	if e != nil {
		t.Fatal(e)
	}
	var found *TicketDTO
	for i := range wl.Items {
		if wl.Items[i].ID == tk.ID {
			found = &wl.Items[i]
			break
		}
	}
	if found == nil {
		t.Fatal("WAITING_DOCTOR ticket must appear on doctor worklist")
	}
	if found.Reason != "céphalées" {
		t.Fatalf("reason=%q", found.Reason)
	}
	if found.VitalSigns == nil || found.VitalSigns.TemperatureC == nil || *found.VitalSigns.TemperatureC != 38.7 {
		t.Fatalf("vitals missing: %+v", found.VitalSigns)
	}
	if !found.VitalSigns.AbnormalTemp || !found.VitalSigns.AbnormalBP || !found.VitalSigns.AbnormalHR {
		t.Fatalf("abnormal flags: %+v", found.VitalSigns)
	}
	if wl.KPIs.ToTreat < 1 {
		t.Fatalf("kpi toTreat=%d", wl.KPIs.ToTreat)
	}

	taken, e := svc.TakeDoctor(tk.ID, TakeDoctorRequest{}, doc)
	if e != nil {
		t.Fatal(e)
	}
	if taken.DoctorTakenBy == nil || *taken.DoctorTakenBy != 102 {
		t.Fatalf("doctorTakenBy=%v", taken.DoctorTakenBy)
	}
	// second doctor cannot silently take
	docB := scopedAccess(103, 10, "queue.doctor.read", "queue.doctor.take")
	mustExecSQL(t, db, `INSERT INTO users(id, name, email) VALUES (103,'Médecin B','medecin-b@pq-test.invalid') ON CONFLICT DO NOTHING`)
	if _, e := svc.TakeDoctor(tk.ID, TakeDoctorRequest{}, docB); statusOf(e) != 409 {
		t.Fatalf("concurrent take want 409 got %d (%v)", statusOf(e), e)
	}

	detail, e := svc.Get(tk.ID, doc)
	if e != nil {
		t.Fatal(e)
	}
	if detail.Ticket.DoctorTakenByName == "" {
		t.Fatal("doctorTakenByName required when in progress")
	}
	if len(detail.Allergies) == 0 || detail.Allergies[0].Label != "Pénicilline" {
		t.Fatalf("allergies=%+v", detail.Allergies)
	}
	if len(detail.Histories) == 0 {
		t.Fatalf("histories=%+v", detail.Histories)
	}

	completed, e := svc.Complete(tk.ID, CompleteRequest{}, doc)
	if e != nil {
		t.Fatal(e)
	}
	if completed.Stage != StageCompleted {
		t.Fatalf("stage=%s", completed.Stage)
	}
	wl, e = svc.DoctorWorklist(Filter{Limit: 50}, doc)
	if e != nil {
		t.Fatal(e)
	}
	for _, item := range wl.Items {
		if item.ID == tk.ID {
			t.Fatal("completed ticket must leave active doctor worklist")
		}
	}
}

func migrateClinicalFlowTables(db *gorm.DB) {
	_ = db.Exec(`CREATE TABLE IF NOT EXISTS consultations (
		id BIGSERIAL PRIMARY KEY, patient_id BIGINT NOT NULL, doctor_name TEXT, service TEXT,
		service_id BIGINT, doctor_user_id BIGINT NULL, status TEXT DEFAULT 'draft', version INT NOT NULL DEFAULT 1,
		started_at TIMESTAMPTZ, completed_at TIMESTAMPTZ,
		cancelled_at TIMESTAMPTZ, cancellation_reason TEXT, diagnosis TEXT, observations TEXT,
		treatment TEXT, sick_leave_required BOOLEAN DEFAULT false, sick_leave_days INT DEFAULT 0,
		sick_leave_start_date TIMESTAMPTZ, sick_leave_end_date TIMESTAMPTZ,
		hospitalization_required BOOLEAN DEFAULT false, hospitalization_reason TEXT,
		hospitalization_type TEXT, hospitalization_duration INT DEFAULT 0,
		created_at TIMESTAMPTZ DEFAULT NOW(), updated_at TIMESTAMPTZ DEFAULT NOW()
	)`)
	_ = db.Exec(`CREATE TABLE IF NOT EXISTS consultation_soaps (
		id BIGSERIAL PRIMARY KEY, consultation_id BIGINT UNIQUE, disposition TEXT,
		patient_advice TEXT, created_by BIGINT, updated_by BIGINT,
		created_at TIMESTAMPTZ DEFAULT NOW(), updated_at TIMESTAMPTZ DEFAULT NOW()
	)`)
}

func clinicalFlowReadyTicket(t *testing.T, db *gorm.DB, svc *Service) *Ticket {
	t.Helper()
	migrateClinicalFlowTables(db)
	admin := adminAccess(100)
	doc := scopedAccess(102, 10, "queue.doctor.read", "queue.doctor.take")
	tk, e := svc.CheckInWalkIn(WalkInCheckInRequest{
		PatientID: 1, ServiceID: 10, IdentityConfirmed: true, Reason: "sync-test",
	}, admin)
	if e != nil {
		t.Fatal(e)
	}
	if _, e := svc.TakeTriage(tk.ID, admin); e != nil {
		t.Fatal(e)
	}
	_ = db.Exec(`INSERT INTO vital_signs(id, medical_record_id, patient_id, temperature_c, measured_at)
		VALUES (701,1,1,37.2,NOW()) ON CONFLICT DO NOTHING`)
	if _, e := svc.CompleteTriage(tk.ID, CompleteTriageRequest{VitalSignsID: uintPtr(701)}, admin); e != nil {
		t.Fatal(e)
	}
	taken, e := svc.TakeDoctor(tk.ID, TakeDoctorRequest{CreateConsultation: true}, doc)
	if e != nil {
		t.Fatal(e)
	}
	if taken.ConsultationID == nil {
		t.Fatal("consultation expected")
	}
	return taken
}

// Characterization F24-02: TakeDoctor(CreateConsultation) must persist structural
// doctor identity (doctor_user_id), not only DoctorName.
func TestPostgresClinicalFlowTakeDoctorPersistsConsultationDoctorUserID(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	migrateClinicalFlowTables(db)
	taken := clinicalFlowReadyTicket(t, db, svc)
	if taken.ConsultationID == nil {
		t.Fatal("ConsultationID expected after TakeDoctor CreateConsultation")
	}

	var doctorUserID *uint
	if err := db.Raw(
		`SELECT doctor_user_id FROM consultations WHERE id=?`,
		*taken.ConsultationID,
	).Scan(&doctorUserID).Error; err != nil {
		t.Fatal(err)
	}
	if doctorUserID == nil {
		t.Fatal("consultations.doctor_user_id is NULL; TakeDoctor must persist structural doctor identity")
	}
	if *doctorUserID != 102 {
		t.Fatalf("consultations.doctor_user_id=%d want 102", *doctorUserID)
	}
}

func TestPostgresClinicalFlowConsultationSyncOnComplete(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	migrateClinicalFlowTables(db)
	taken := clinicalFlowReadyTicket(t, db, svc)
	doc := scopedAccess(102, 10, "queue.doctor.read", "queue.doctor.take")

	var status string
	if err := db.Raw(`SELECT status FROM consultations WHERE id=?`, *taken.ConsultationID).Scan(&status).Error; err != nil {
		t.Fatal(err)
	}
	if status != "in_progress" {
		t.Fatalf("consultation status after take want in_progress got %s", status)
	}

	var linkedConsultationID *uint
	if err := db.Raw(`SELECT consultation_id FROM vital_signs WHERE id=701`).Scan(&linkedConsultationID).Error; err != nil {
		t.Fatal(err)
	}
	if linkedConsultationID == nil || *linkedConsultationID != *taken.ConsultationID {
		t.Fatalf("vital_signs consultation link=%v want %d", linkedConsultationID, *taken.ConsultationID)
	}

	completed, e := svc.Complete(taken.ID, CompleteRequest{Disposition: "DISCHARGED"}, doc)
	if e != nil {
		t.Fatal(e)
	}
	if completed.Stage != StageCompleted {
		t.Fatalf("stage=%s", completed.Stage)
	}
	if err := db.Raw(`SELECT status FROM consultations WHERE id=?`, *taken.ConsultationID).Scan(&status).Error; err != nil {
		t.Fatal(err)
	}
	if status != "completed" {
		t.Fatalf("consultation not completed: %s", status)
	}
}

func TestPostgresClinicalFlowDoctorBOtherCannotComplete(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	taken := clinicalFlowReadyTicket(t, db, svc)
	docB := scopedAccess(103, 10, "queue.doctor.read", "queue.doctor.take")
	mustExecSQL(t, db, `INSERT INTO users(id, name, email) VALUES (103,'Médecin B','medecin-b@pq-test.invalid') ON CONFLICT DO NOTHING`)
	if _, e := svc.Complete(taken.ID, CompleteRequest{}, docB); statusOf(e) != 403 {
		t.Fatalf("other doctor complete want 403 got %d (%v)", statusOf(e), e)
	}
}

// Characterization F24-11: failure to persist the clinical closing SOAP data
// must abort the whole completion transaction.
func TestPostgresClinicalFlowCompleteRollsBackWhenSOAPClosingPersistenceFails(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	migrateClinicalFlowTables(db)

	taken := clinicalFlowReadyTicket(t, db, svc)
	doc := scopedAccess(102, 10, "queue.doctor.read", "queue.doctor.take")

	if taken.ConsultationID == nil {
		t.Fatal("consultation expected")
	}

	consultationID := *taken.ConsultationID

	if err := db.Exec(`
	INSERT INTO consultation_soaps(
		consultation_id,
		disposition,
		patient_advice,
		created_by,
		updated_by,
		created_at,
		updated_at
	)
	VALUES (?, 'OBSERVATION', 'Conseil initial', ?, ?, NOW(), NOW())
`, consultationID, doc.UserID, doc.UserID).Error; err != nil {
		t.Fatal(err)
	}

	// Break only the SOAP closing write path after the clinical-flow fixture
	// has been prepared.
	if err := db.Exec(`
		ALTER TABLE consultation_soaps
		RENAME COLUMN patient_advice TO patient_advice_broken
	`).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Exec(`
			ALTER TABLE consultation_soaps
			RENAME COLUMN patient_advice_broken TO patient_advice
		`).Error
	})

	_, err := svc.Complete(taken.ID, CompleteRequest{
		Disposition:     "DISCHARGED",
		DispositionNote: "Consignes de sortie",
	}, doc)
	if err == nil {
		t.Fatal("Complete should fail when SOAP closing persistence fails")
	}

	var ticket Ticket
	if err := db.First(&ticket, taken.ID).Error; err != nil {
		t.Fatal(err)
	}
	if ticket.Stage != StageDoctorInProgress {
		t.Fatalf("ticket stage after rollback=%s want %s", ticket.Stage, StageDoctorInProgress)
	}
	if ticket.Status != StatusActive {
		t.Fatalf("ticket status after rollback=%s want %s", ticket.Status, StatusActive)
	}

	var consultationStatus string
	if err := db.Raw(
		`SELECT status FROM consultations WHERE id=?`,
		consultationID,
	).Scan(&consultationStatus).Error; err != nil {
		t.Fatal(err)
	}
	if consultationStatus != consultations.ConsultationStatusInProgress {
		t.Fatalf(
			"consultation status after rollback=%s want %s",
			consultationStatus,
			consultations.ConsultationStatusInProgress,
		)
	}
}

func TestPostgresClinicalFlowReuseExistingConsultation(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	migrateClinicalFlowTables(db)
	admin := adminAccess(100)
	doc := scopedAccess(102, 10, "queue.doctor.read", "queue.doctor.take")
	tk, e := svc.CheckInWalkIn(WalkInCheckInRequest{
		PatientID: 1, ServiceID: 10, IdentityConfirmed: true,
	}, admin)
	if e != nil {
		t.Fatal(e)
	}
	if _, e := svc.TakeTriage(tk.ID, admin); e != nil {
		t.Fatal(e)
	}
	if _, e := svc.CompleteTriage(tk.ID, CompleteTriageRequest{}, admin); e != nil {
		t.Fatal(e)
	}
	_ = db.Exec(`INSERT INTO consultations(id, patient_id, doctor_name, service, service_id, status, diagnosis)
		VALUES (9001, 1, 'Dr Test', 'Urgences', 10, 'draft', 'existante')`)
	_ = db.Model(&Ticket{}).Where("id=?", tk.ID).Update("consultation_id", 9001).Error

	taken, e := svc.TakeDoctor(tk.ID, TakeDoctorRequest{CreateConsultation: true}, doc)
	if e != nil {
		t.Fatal(e)
	}
	if taken.ConsultationID == nil || *taken.ConsultationID != 9001 {
		t.Fatalf("consultation reuse failed: %v", taken.ConsultationID)
	}
	var status string
	_ = db.Raw(`SELECT status FROM consultations WHERE id=9001`).Scan(&status)
	if status != "in_progress" {
		t.Fatalf("existing consultation not activated: %s", status)
	}
	var doctorUserID *uint
	if err := db.Raw(`SELECT doctor_user_id FROM consultations WHERE id=9001`).Scan(&doctorUserID).Error; err != nil {
		t.Fatal(err)
	}
	if doctorUserID == nil || *doctorUserID != 102 {
		t.Fatalf("NULL doctor_user_id must bind to taking doctor 102, got %v", doctorUserID)
	}
}

// waitingDoctorTicketWithConsultation prepares a WAITING_DOCTOR ticket linked to consultationID.
func waitingDoctorTicketWithConsultation(t *testing.T, db *gorm.DB, svc *Service, consultationID uint) uint {
	t.Helper()
	migrateClinicalFlowTables(db)
	admin := adminAccess(100)
	tk, e := svc.CheckInWalkIn(WalkInCheckInRequest{
		PatientID: 1, ServiceID: 10, IdentityConfirmed: true, Reason: "doctor-user-bind",
	}, admin)
	if e != nil {
		t.Fatal(e)
	}
	if _, e := svc.TakeTriage(tk.ID, admin); e != nil {
		t.Fatal(e)
	}
	if _, e := svc.CompleteTriage(tk.ID, CompleteTriageRequest{}, admin); e != nil {
		t.Fatal(e)
	}
	if err := db.Model(&Ticket{}).Where("id=?", tk.ID).Update("consultation_id", consultationID).Error; err != nil {
		t.Fatal(err)
	}
	waiting, ge := svc.Get(tk.ID, admin)
	if ge != nil {
		t.Fatal(ge)
	}
	if waiting.Ticket.Stage != StageWaitingDoctor {
		t.Fatalf("precondition stage want WAITING_DOCTOR got %s", waiting.Ticket.Stage)
	}
	return waiting.Ticket.ID
}

// F24-02: DRAFT already assigned to the same doctor may be activated.
func TestPostgresClinicalFlowTakeDoctorAcceptsSameDoctorAssignment(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	migrateClinicalFlowTables(db)
	doc := scopedAccess(102, 10, "queue.doctor.read", "queue.doctor.take")
	docUID := uint(102)
	_ = db.Exec(`INSERT INTO consultations(id, patient_id, doctor_name, doctor_user_id, service, service_id, status, diagnosis)
		VALUES (9201, 1, 'Médecin', ?, 'Urgences', 10, 'draft', 'même médecin')`, docUID)

	ticketID := waitingDoctorTicketWithConsultation(t, db, svc, 9201)
	taken, e := svc.TakeDoctor(ticketID, TakeDoctorRequest{CreateConsultation: true}, doc)
	if e != nil {
		t.Fatal(e)
	}
	if taken.ConsultationID == nil || *taken.ConsultationID != 9201 {
		t.Fatalf("consultation=%v want 9201", taken.ConsultationID)
	}
	var status string
	var doctorUserID *uint
	if err := db.Raw(`SELECT status FROM consultations WHERE id=9201`).Scan(&status).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Raw(`SELECT doctor_user_id FROM consultations WHERE id=9201`).Scan(&doctorUserID).Error; err != nil {
		t.Fatal(err)
	}
	if status != "in_progress" {
		t.Fatalf("status=%s want in_progress", status)
	}
	if doctorUserID == nil || *doctorUserID != 102 {
		t.Fatalf("doctor_user_id=%v want 102", doctorUserID)
	}
}

// F24-02: DRAFT assigned to a different doctor must Conflict and roll back TakeDoctor.
func TestPostgresClinicalFlowTakeDoctorRejectsDifferentDoctorAssignment(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	migrateClinicalFlowTables(db)
	mustExecSQL(t, db, `INSERT INTO users(id, name, email) VALUES (103,'Médecin B','medecin-b@pq-test.invalid') ON CONFLICT DO NOTHING`)
	doc := scopedAccess(102, 10, "queue.doctor.read", "queue.doctor.take")
	otherUID := uint(103)
	_ = db.Exec(`INSERT INTO consultations(id, patient_id, doctor_name, doctor_user_id, service, service_id, status, diagnosis)
		VALUES (9202, 1, 'Médecin B', ?, 'Urgences', 10, 'draft', 'autre médecin')`, otherUID)

	ticketID := waitingDoctorTicketWithConsultation(t, db, svc, 9202)
	_, takeErr := svc.TakeDoctor(ticketID, TakeDoctorRequest{CreateConsultation: true}, doc)
	if statusOf(takeErr) != 409 {
		t.Fatalf("different doctor assignment want 409 got %d (%v)", statusOf(takeErr), takeErr)
	}

	after, ge := svc.Get(ticketID, adminAccess(100))
	if ge != nil {
		t.Fatal(ge)
	}
	if after.Ticket.Stage != StageWaitingDoctor {
		t.Fatalf("ticket stage after rollback=%s want WAITING_DOCTOR", after.Ticket.Stage)
	}
	if after.Ticket.DoctorTakenBy != nil {
		t.Fatalf("doctor_taken_by must stay unset after rollback, got %v", *after.Ticket.DoctorTakenBy)
	}
	var status string
	var doctorUserID *uint
	if err := db.Raw(`SELECT status FROM consultations WHERE id=9202`).Scan(&status).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Raw(`SELECT doctor_user_id FROM consultations WHERE id=9202`).Scan(&doctorUserID).Error; err != nil {
		t.Fatal(err)
	}
	if status != "draft" {
		t.Fatalf("consultation status after rollback=%s want draft", status)
	}
	if doctorUserID == nil || *doctorUserID != 103 {
		t.Fatalf("doctor ownership must stay 103, got %v", doctorUserID)
	}
}

// F24-02 harden: missing linked consultation must NotFound and roll back TakeDoctor.
func TestPostgresClinicalFlowTakeDoctorRejectsMissingLinkedConsultation(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	migrateClinicalFlowTables(db)
	doc := scopedAccess(102, 10, "queue.doctor.read", "queue.doctor.take")

	const missingConsultationID uint = 999001
	ticketID := waitingDoctorTicketWithConsultation(t, db, svc, missingConsultationID)
	_, takeErr := svc.TakeDoctor(ticketID, TakeDoctorRequest{CreateConsultation: true}, doc)
	if statusOf(takeErr) != 404 {
		t.Fatalf("missing linked consultation want 404 got %d (%v)", statusOf(takeErr), takeErr)
	}

	after, ge := svc.Get(ticketID, adminAccess(100))
	if ge != nil {
		t.Fatal(ge)
	}
	if after.Ticket.Stage != StageWaitingDoctor {
		t.Fatalf("ticket stage after rollback=%s want WAITING_DOCTOR", after.Ticket.Stage)
	}
	if after.Ticket.DoctorTakenBy != nil {
		t.Fatalf("doctor_taken_by must stay unset after rollback, got %v", *after.Ticket.DoctorTakenBy)
	}
}

// F24-02 harden: unsupported linked consultation status must Conflict and roll back TakeDoctor.
func TestPostgresClinicalFlowTakeDoctorRejectsUnsupportedConsultationStatus(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	migrateClinicalFlowTables(db)
	doc := scopedAccess(102, 10, "queue.doctor.read", "queue.doctor.take")
	_ = db.Exec(`INSERT INTO consultations(id, patient_id, doctor_name, service, service_id, status, diagnosis)
		VALUES (9203, 1, 'Médecin', 'Urgences', 10, 'paused', 'statut incompatible')`)

	ticketID := waitingDoctorTicketWithConsultation(t, db, svc, 9203)
	_, takeErr := svc.TakeDoctor(ticketID, TakeDoctorRequest{CreateConsultation: true}, doc)
	if statusOf(takeErr) != 409 {
		t.Fatalf("unsupported consultation status want 409 got %d (%v)", statusOf(takeErr), takeErr)
	}

	after, ge := svc.Get(ticketID, adminAccess(100))
	if ge != nil {
		t.Fatal(ge)
	}
	if after.Ticket.Stage != StageWaitingDoctor {
		t.Fatalf("ticket stage after rollback=%s want WAITING_DOCTOR", after.Ticket.Stage)
	}
	if after.Ticket.DoctorTakenBy != nil {
		t.Fatalf("doctor_taken_by must stay unset after rollback, got %v", *after.Ticket.DoctorTakenBy)
	}
	var status string
	if err := db.Raw(`SELECT status FROM consultations WHERE id=9203`).Scan(&status).Error; err != nil {
		t.Fatal(err)
	}
	if status != "paused" {
		t.Fatalf("consultation status after rollback=%s want paused", status)
	}
}

// Characterization: TakeDoctor(CreateConsultation) must reject when the ticket already
// links a COMPLETED consultation for the same patient/service, and must leave the
// ticket in WAITING_DOCTOR (no silent doctor take / reactivation).
func TestPostgresClinicalFlowRejectTakeDoctorOnCompletedConsultation(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	migrateClinicalFlowTables(db)
	admin := adminAccess(100)
	doc := scopedAccess(102, 10, "queue.doctor.read", "queue.doctor.take")

	tk, e := svc.CheckInWalkIn(WalkInCheckInRequest{
		PatientID: 1, ServiceID: 10, IdentityConfirmed: true, Reason: "completed-consult-guard",
	}, admin)
	if e != nil {
		t.Fatal(e)
	}
	if _, e := svc.TakeTriage(tk.ID, admin); e != nil {
		t.Fatal(e)
	}
	if _, e := svc.CompleteTriage(tk.ID, CompleteTriageRequest{}, admin); e != nil {
		t.Fatal(e)
	}
	waiting, ge := svc.Get(tk.ID, admin)
	if ge != nil {
		t.Fatal(ge)
	}
	if waiting.Ticket.Stage != StageWaitingDoctor {
		t.Fatalf("precondition stage want WAITING_DOCTOR got %s", waiting.Ticket.Stage)
	}

	_ = db.Exec(`INSERT INTO consultations(id, patient_id, doctor_name, service, service_id, status, diagnosis, completed_at)
		VALUES (9101, 1, 'Dr Test', 'Urgences', 10, 'completed', 'déjà clôturée', NOW())`)
	if err := db.Model(&Ticket{}).Where("id=?", waiting.Ticket.ID).Update("consultation_id", 9101).Error; err != nil {
		t.Fatal(err)
	}

	_, takeErr := svc.TakeDoctor(waiting.Ticket.ID, TakeDoctorRequest{CreateConsultation: true}, doc)
	if takeErr == nil {
		t.Fatal("TakeDoctor with CreateConsultation on COMPLETED consultation must be rejected")
	}

	after, ge := svc.Get(waiting.Ticket.ID, admin)
	if ge != nil {
		t.Fatal(ge)
	}
	if after.Ticket.Stage != StageWaitingDoctor {
		t.Fatalf("ticket must remain WAITING_DOCTOR after rejected TakeDoctor, got %s (err=%v)", after.Ticket.Stage, takeErr)
	}
	if after.Ticket.DoctorTakenBy != nil {
		t.Fatalf("doctor_taken_by must stay unset after rejection, got %v", *after.Ticket.DoctorTakenBy)
	}
	var consultStatus string
	if err := db.Raw(`SELECT status FROM consultations WHERE id=9101`).Scan(&consultStatus).Error; err != nil {
		t.Fatal(err)
	}
	if consultStatus != "completed" {
		t.Fatalf("completed consultation must stay completed, got %s", consultStatus)
	}
}

// Characterization: cancelling a queue ticket during an active doctor encounter must not
// leave CANCELLED ticket + IN_PROGRESS consultation linked together.
func TestPostgresClinicalFlowCancelDuringDoctorEncounterConsultationIntegrity(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	taken := clinicalFlowReadyTicket(t, db, svc)
	admin := adminAccess(100)

	if taken.Stage != StageDoctorInProgress {
		t.Fatalf("precondition ticket stage want DOCTOR_IN_PROGRESS got %s", taken.Stage)
	}
	if taken.ConsultationID == nil {
		t.Fatal("precondition: linked consultation required")
	}
	var consultBefore string
	if err := db.Raw(`SELECT status FROM consultations WHERE id=?`, *taken.ConsultationID).Scan(&consultBefore).Error; err != nil {
		t.Fatal(err)
	}
	if consultBefore != "in_progress" {
		t.Fatalf("precondition consultation want in_progress got %s", consultBefore)
	}

	cancelled, e := svc.Cancel(taken.ID, CancelRequest{Reason: "cancel-during-doctor-encounter"}, admin)
	if e != nil {
		t.Fatalf("Queue Cancel with authorized actor: %v", e)
	}

	detail, ge := svc.Get(taken.ID, admin)
	if ge != nil {
		t.Fatal(ge)
	}
	var consultAfter string
	if err := db.Raw(`SELECT status FROM consultations WHERE id=?`, *taken.ConsultationID).Scan(&consultAfter).Error; err != nil {
		t.Fatal(err)
	}
	var apptStatus string
	apptNote := "none"
	if detail.Ticket.AppointmentID != nil {
		if err := db.Raw(`SELECT status FROM patient_queue_appointments WHERE id=?`, *detail.Ticket.AppointmentID).Scan(&apptStatus).Error; err != nil {
			t.Fatal(err)
		}
		apptNote = apptStatus
	}

	t.Logf("after Cancel: ticket.status=%s ticket.stage=%s consultation.status=%s appointment.status=%s",
		detail.Ticket.Status, detail.Ticket.Stage, consultAfter, apptNote)

	if cancelled.Status != StatusCancelled && detail.Ticket.Status != StatusCancelled {
		t.Fatalf("expected cancelled ticket status, got cancelResult=%s detail=%s", cancelled.Status, detail.Ticket.Status)
	}
	if detail.Ticket.Status == StatusCancelled && consultAfter == "in_progress" {
		t.Fatalf("integrity invariant violated: CANCELLED ticket still linked to IN_PROGRESS consultation (ticket stage=%s consult=%s appt=%s)",
			detail.Ticket.Stage, consultAfter, apptNote)
	}
}

func TestPostgresClinicalFlowGetByConsultationAndActivePatient(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	taken := clinicalFlowReadyTicket(t, db, svc)
	doc := scopedAccess(102, 10, "queue.doctor.read", "queue.doctor.take")

	byConsult, e := svc.GetByConsultationID(*taken.ConsultationID, doc)
	if e != nil || byConsult.ID != taken.ID {
		t.Fatalf("GetByConsultationID: %v %+v", e, byConsult)
	}
	active, e := svc.GetActiveTicketForPatient(taken.PatientID, doc)
	if e != nil || active.ID != taken.ID {
		t.Fatalf("GetActiveTicketForPatient: %v %+v", e, active)
	}
}

func TestPostgresAppointmentDomainFoundation23A(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	admin := adminAccess(100)
	docID := uint(102)
	seedPractitionerForService(t, db, 3, 102, 10)
	seedPractitionerForService(t, db, 4, 102, 11)
	seedAllDaySchedules(t, db, 102, 10)
	seedAllDaySchedules(t, db, 102, 11)
	seedAvailabilityAroundNow(t, db, 102, 10)
	seedAvailabilityAroundNow(t, db, 102, 11)

	// A — missing type/end rejected (23D: no silent weaken via legacy CreateAppointment)
	if _, e := svc.CreateAppointment(CreateAppointmentRequest{
		PatientID: 1, ServiceID: 10, ExpectedDoctorID: &docID,
		ScheduledAt: time.Now().UTC().Add(30 * time.Minute), Reason: "no-duration",
	}, admin); statusOf(e) != 400 {
		t.Fatalf("missing type/end want 400 got %d (%v)", statusOf(e), e)
	}

	at, e := svc.CreateAppointmentType(CreateAppointmentTypeRequest{
		Code: "GEN-CONSULT", Name: "Consultation générale", DefaultDurationMinutes: 30,
	}, admin)
	if e != nil {
		t.Fatal(e)
	}
	if _, e := svc.CreateAppointmentType(CreateAppointmentTypeRequest{
		Code: "BAD", Name: "Bad", DefaultDurationMinutes: 0,
	}, admin); statusOf(e) != 400 {
		t.Fatalf("duration<=0 want 400 got %d (%v)", statusOf(e), e)
	}

	// B — typed create + check-in creates ticket; scheduled_end_at always set
	legacy, e := svc.CreateAppointment(CreateAppointmentRequest{
		PatientID: 1, ServiceID: 10, ExpectedDoctorID: &docID, AppointmentTypeID: &at.ID,
		ScheduledAt: time.Now().UTC().Add(30 * time.Minute).Truncate(time.Minute), Reason: "legacy-23a",
	}, admin)
	if e != nil {
		t.Fatal(e)
	}
	if legacy.ScheduledEndAt == nil {
		t.Fatal("CreateAppointment must persist scheduled_end_at")
	}
	if legacy.ExpectedDoctorID == nil || *legacy.ExpectedDoctorID != docID {
		t.Fatalf("practitioner users.id expected 102 got %v", legacy.ExpectedDoctorID)
	}
	var histCount int64
	db.Model(&AppointmentHistory{}).Where("appointment_id=? AND event_type=?", legacy.ID, ApptHistCreated).Count(&histCount)
	if histCount != 1 {
		t.Fatalf("CREATED history want 1 got %d", histCount)
	}

	tk, _, e := svc.CheckInAppointment(legacy.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, admin)
	if e != nil {
		t.Fatal(e)
	}
	if tk.AppointmentID == nil || *tk.AppointmentID != legacy.ID {
		t.Fatal("ticket must link same canonical appointment id")
	}
	var apptAfter Appointment
	_ = db.First(&apptAfter, legacy.ID)
	if apptAfter.QueueTicketID == nil || *apptAfter.QueueTicketID != tk.ID {
		t.Fatal("appointment.queue_ticket_id must link ticket")
	}
	if apptAfter.Status != ApptCheckedIn {
		t.Fatalf("status=%s", apptAfter.Status)
	}
	db.Model(&AppointmentHistory{}).Where("appointment_id=? AND event_type=?", legacy.ID, ApptHistCheckedIn).Count(&histCount)
	if histCount != 1 {
		t.Fatalf("CHECKED_IN history want 1 got %d", histCount)
	}

	// C — walk-in remains functional (no appointment)
	walk, e := svc.CheckInWalkIn(WalkInCheckInRequest{
		PatientID: 2, ServiceID: 10, IdentityConfirmed: true, Reason: "walk-23a",
	}, admin)
	if e != nil {
		t.Fatal(e)
	}
	if walk.AppointmentID != nil {
		t.Fatal("walk-in must have nil appointment_id")
	}

	// D/E/F — rich interval from type
	start := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Minute)
	rich, e := svc.CreateAppointment(CreateAppointmentRequest{
		PatientID: 1, ServiceID: 10, AppointmentTypeID: &at.ID, ScheduledAt: start,
	}, admin)
	if e != nil {
		t.Fatal(e)
	}
	if rich.ScheduledEndAt == nil {
		t.Fatal("typed appointment must derive scheduled_end_at")
	}
	if !rich.ScheduledEndAt.Equal(start.Add(30 * time.Minute)) {
		t.Fatalf("end=%v want start+30m", rich.ScheduledEndAt)
	}
	if !rich.ScheduledEndAt.After(rich.ScheduledAt) {
		t.Fatal("end must be after start")
	}

	badEnd := start.Add(-time.Minute)
	if _, e := svc.CreateAppointment(CreateAppointmentRequest{
		PatientID: 2, ServiceID: 10, ScheduledAt: start, ScheduledEndAt: &badEnd,
	}, admin); statusOf(e) != 400 {
		t.Fatalf("end<=start want 400 got %d (%v)", statusOf(e), e)
	}

	// I — no-show on a past scheduled appointment (future no-show rejected by 23E)
	nsStart := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Minute)
	nsEnd := nsStart.Add(30 * time.Minute)
	ns, e := svc.CreateAppointment(CreateAppointmentRequest{
		PatientID: 2, ServiceID: 11, ScheduledAt: nsStart, ScheduledEndAt: &nsEnd,
	}, admin)
	if e != nil {
		t.Fatal(e)
	}
	if _, e := svc.MarkNoShow(ns.ID, NoShowAppointmentRequest{}, admin); e != nil {
		t.Fatal(e)
	}
	var noshow Appointment
	if err := db.First(&noshow, ns.ID).Error; err != nil {
		t.Fatal(err)
	}
	if noshow.Status != ApptNoShow {
		t.Fatalf("no-show status=%s", noshow.Status)
	}
	db.Model(&AppointmentHistory{}).Where("appointment_id=? AND event_type=?", ns.ID, ApptHistNoShow).Count(&histCount)
	if histCount != 1 {
		t.Fatalf("NO_SHOW history want 1 got %d", histCount)
	}

	// H — history is append-only (service never updates history rows)
	var events []AppointmentHistory
	_ = db.Where("appointment_id=?", ns.ID).Order("id ASC").Find(&events)
	if len(events) < 2 || events[0].EventType != ApptHistCreated || events[1].EventType != ApptHistNoShow {
		t.Fatalf("history sequence=%+v", events)
	}
	if events[0].ActorUserID != 100 || events[1].ActorUserID != 100 {
		t.Fatal("actor must come from JWT/access user, not frontend")
	}

	// Duplicate type code → conflict
	if _, e := svc.CreateAppointmentType(CreateAppointmentTypeRequest{
		Code: "gen-consult", Name: "dup", DefaultDurationMinutes: 20,
	}, admin); statusOf(e) != 409 {
		t.Fatalf("duplicate type code want 409 got %d (%v)", statusOf(e), e)
	}
}

func uintPtr(v uint) *uint { return &v }
