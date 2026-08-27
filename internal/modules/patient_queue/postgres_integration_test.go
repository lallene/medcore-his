package patient_queue

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
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

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
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	db, e := gorm.Open(postgres.Open(dsn+sep+"search_path="+url.QueryEscape(schema)), &gorm.Config{})
	if e != nil {
		t.Fatal(e)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { sqlDB.Close(); admin.Exec(`DROP SCHEMA IF EXISTS "` + schema + `" CASCADE`) })
	if e = db.AutoMigrate(&Appointment{}, &Ticket{}, &History{}); e != nil {
		t.Fatal(e)
	}
	// Minimal patients / services / users for FK-less raw lookups
	_ = db.Exec(`CREATE TABLE IF NOT EXISTS patients (
		id BIGSERIAL PRIMARY KEY, code_patient TEXT, nom TEXT, prenoms TEXT,
		sexe TEXT, date_naissance DATE, telephone TEXT
	)`)
	_ = db.Exec(`CREATE TABLE IF NOT EXISTS organization_services (
		id BIGSERIAL PRIMARY KEY, name TEXT, code TEXT
	)`)
	_ = db.Exec(`CREATE TABLE IF NOT EXISTS users (
		id BIGSERIAL PRIMARY KEY, name TEXT
	)`)
	_ = db.Exec(`CREATE TABLE IF NOT EXISTS billing_invoices (
		id BIGSERIAL PRIMARY KEY, patient_id BIGINT, patient_amount BIGINT, status TEXT, coverage_pending BOOLEAN DEFAULT false
	)`)
	_ = db.Exec(`CREATE TABLE IF NOT EXISTS billing_payments (
		id BIGSERIAL PRIMARY KEY, invoice_id BIGINT, amount BIGINT
	)`)
	_ = db.Exec(`CREATE TABLE IF NOT EXISTS vital_signs (
		id BIGSERIAL PRIMARY KEY, medical_record_id BIGINT, patient_id BIGINT NOT NULL,
		consultation_id BIGINT, comment TEXT,
		temperature_c DOUBLE PRECISION, systolic_bp INT, diastolic_bp INT, heart_rate INT,
		oxygen_saturation DOUBLE PRECISION, weight_kg DOUBLE PRECISION, height_cm DOUBLE PRECISION,
		measured_at TIMESTAMPTZ
	)`)
	_ = db.Exec(`CREATE TABLE IF NOT EXISTS allergies (
		id BIGSERIAL PRIMARY KEY, medical_record_id BIGINT, patient_id BIGINT,
		allergen_type TEXT, allergen_name TEXT, reaction TEXT, severity TEXT,
		comment TEXT, is_active BOOLEAN DEFAULT true, created_by BIGINT,
		created_at TIMESTAMPTZ DEFAULT NOW(), updated_at TIMESTAMPTZ DEFAULT NOW()
	)`)
	_ = db.Exec(`CREATE TABLE IF NOT EXISTS medical_histories (
		id BIGSERIAL PRIMARY KEY, medical_record_id BIGINT, patient_id BIGINT,
		type TEXT, title TEXT, description TEXT, status TEXT DEFAULT 'active',
		severity TEXT, comment TEXT, created_by BIGINT,
		created_at TIMESTAMPTZ DEFAULT NOW(), updated_at TIMESTAMPTZ DEFAULT NOW()
	)`)
	_ = db.Exec(`CREATE TABLE IF NOT EXISTS staff_profiles (
		id BIGSERIAL PRIMARY KEY, user_id BIGINT, active BOOLEAN DEFAULT true, primary_service_id BIGINT
	)`)
	_ = db.Exec(`CREATE TABLE IF NOT EXISTS staff_service_assignments (
		id BIGSERIAL PRIMARY KEY, profile_id BIGINT, service_id BIGINT, active BOOLEAN DEFAULT true
	)`)
	_ = db.Exec(`INSERT INTO patients(id, code_patient, nom, prenoms, sexe, telephone) VALUES
		(1,'P-Q-1','Dupont','Alice','F','0600000001'),
		(2,'P-Q-2','Martin','Bob','M','0600000002') ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO organization_services(id, name, code) VALUES (10,'Urgences','URG'),(11,'Médecine','MED') ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO users(id, name) VALUES (100,'Accueil'),(101,'Infirmier'),(102,'Médecin') ON CONFLICT DO NOTHING`)
	return db
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
	now := time.Now().UTC()
	appt, e := svc.CreateAppointment(CreateAppointmentRequest{
		PatientID: 2, ServiceID: 10, ScheduledAt: now.Add(-20 * time.Minute), Reason: "Suivi",
	}, adminAccess(100))
	if e != nil {
		t.Fatal(e)
	}
	_ = db.Exec(`INSERT INTO billing_invoices(patient_id, patient_amount, status, coverage_pending) VALUES (2, 5000, 'ISSUED', false)`)
	fin, e := svc.EvaluateFinance(2)
	if e != nil || fin != FinancePaymentRequired {
		t.Fatalf("finance=%s err=%v", fin, e)
	}
	if _, e := svc.CheckInAppointment(appt.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, adminAccess(100)); e == nil {
		t.Fatal("check-in without override should fail when payment required")
	}
	tk, e := svc.CheckInAppointment(appt.ID, AppointmentCheckInRequest{
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
	if _, e := svc.CheckInAppointment(appt.ID, AppointmentCheckInRequest{IdentityConfirmed: true, FinanceOverride: true}, adminAccess(100)); e == nil {
		t.Fatal("double appointment check-in should conflict")
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
	if _, e := svc.Complete(tkB.ID, docA); statusOf(e) != 404 {
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
	if _, e := svc.SetPriority(tkB2.ID, PriorityRequest{Priority: PriorityUrgent}, nurseA); statusOf(e) != 404 {
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
	now := time.Now().UTC()
	// Service A: one late appointment ticket
	apptA, e := svc.CreateAppointment(CreateAppointmentRequest{
		PatientID: 1, ServiceID: 10, ScheduledAt: now.Add(-40 * time.Minute), Reason: "A-late",
	}, admin)
	if e != nil {
		t.Fatal(e)
	}
	tkA, e := svc.CheckInAppointment(apptA.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, admin)
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
	_ = db.Exec(`INSERT INTO users(id, name) VALUES (103,'Médecin B') ON CONFLICT DO NOTHING`)
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

	completed, e := svc.Complete(tk.ID, doc)
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

func uintPtr(v uint) *uint { return &v }
