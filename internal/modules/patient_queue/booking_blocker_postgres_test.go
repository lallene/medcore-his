package patient_queue

import (
	"sync"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/core/scheduling"
)

// LOT 23D blocker-fix: legacy CreateAppointment + caller-scoped idempotency + concurrent retries.
func TestPostgresLegacyRouteBookingGuarantees23D(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	_ = scheduling.SetLocation("UTC")
	_ = EnsureAppointmentIndexes(db)

	_ = db.Exec(`INSERT INTO users(id, name) VALUES (800,'DrLeg'),(801,'LegActor') ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO staff_profiles(id, user_id, active, primary_service_id) VALUES
		(100,800,true,10),(101,801,true,10) ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO staff_service_assignments(profile_id, service_id, active) VALUES
		(100,10,true),(101,10,true) ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO patients(id, code_patient, nom, prenoms) VALUES
		(801,'PL1','Leg','One'),(802,'PL2','Leg','Two'),(803,'PL3','Leg','Three') ON CONFLICT DO NOTHING`)

	admin := adminAccess(801)
	prac := uint(800)
	vf := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	_, err := svc.CreateWorkingSchedule(CreateWorkingScheduleRequest{
		PractitionerID: prac, ServiceID: 10, Weekday: int(time.Monday),
		StartTime: "08:00", EndTime: "12:00", ValidFrom: vf,
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	at, err := svc.CreateAppointmentType(CreateAppointmentTypeRequest{
		Code: "LEG-30", Name: "Legacy 30", DefaultDurationMinutes: 30,
	}, admin)
	if err != nil {
		t.Fatal(err)
	}

	start := time.Date(2026, 9, 14, 9, 0, 0, 0, time.UTC)

	// A — legacy CreateAppointment uses booking guarantees (succeeds in schedule)
	appt, err := svc.CreateAppointment(CreateAppointmentRequest{
		PatientID: 801, ServiceID: 10, ExpectedDoctorID: &prac,
		AppointmentTypeID: &at.ID, ScheduledAt: start, Reason: "legacy-ok",
	}, admin)
	if err != nil {
		t.Fatalf("A: %v", err)
	}

	// E — persists scheduled_end_at
	if appt.ScheduledEndAt == nil || !appt.ScheduledEndAt.Equal(start.Add(30*time.Minute)) {
		t.Fatalf("E scheduled_end_at=%v", appt.ScheduledEndAt)
	}

	// B — outside working schedule rejected
	_, err = svc.CreateAppointment(CreateAppointmentRequest{
		PatientID: 802, ServiceID: 10, ExpectedDoctorID: &prac,
		AppointmentTypeID: &at.ID,
		ScheduledAt:       time.Date(2026, 9, 14, 14, 0, 0, 0, time.UTC),
	}, admin)
	if statusOf(err) != 409 {
		t.Fatalf("B outside schedule want 409 got %d %v", statusOf(err), err)
	}

	// C — practitioner overlap rejected
	_, err = svc.CreateAppointment(CreateAppointmentRequest{
		PatientID: 802, ServiceID: 10, ExpectedDoctorID: &prac,
		AppointmentTypeID: &at.ID, ScheduledAt: start,
	}, admin)
	if statusOf(err) != 409 {
		t.Fatalf("C practitioner overlap want 409 got %d %v", statusOf(err), err)
	}

	// D — patient overlap rejected (same patient, other practitioner free slot)
	_ = db.Exec(`INSERT INTO users(id, name) VALUES (802,'DrLeg2') ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO staff_profiles(id, user_id, active, primary_service_id) VALUES (102,802,true,10) ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO staff_service_assignments(profile_id, service_id, active) VALUES (102,10,true) ON CONFLICT DO NOTHING`)
	prac2 := uint(802)
	_, err = svc.CreateWorkingSchedule(CreateWorkingScheduleRequest{
		PractitionerID: prac2, ServiceID: 10, Weekday: int(time.Monday),
		StartTime: "08:00", EndTime: "12:00", ValidFrom: vf,
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.CreateAppointment(CreateAppointmentRequest{
		PatientID: 801, ServiceID: 10, ExpectedDoctorID: &prac2,
		AppointmentTypeID: &at.ID, ScheduledAt: start,
	}, admin)
	if statusOf(err) != 409 {
		t.Fatalf("D patient overlap want 409 got %d %v", statusOf(err), err)
	}
}

func TestPostgresIdempotencyCallerScopedAndSemantics23D(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	_ = scheduling.SetLocation("UTC")
	_ = EnsureAppointmentIndexes(db)

	_ = db.Exec(`INSERT INTO users(id, name) VALUES (810,'DrIdem'),(811,'UserA'),(812,'UserB') ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO staff_profiles(id, user_id, active, primary_service_id) VALUES
		(110,810,true,10),(111,811,true,10),(112,812,true,10) ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO staff_service_assignments(profile_id, service_id, active) VALUES
		(110,10,true),(111,10,true),(112,10,true) ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO patients(id, code_patient, nom, prenoms) VALUES
		(811,'PI1','Id','One'),(812,'PI2','Id','Two'),(813,'PI3','Id','Three'),(814,'PI4','Id','Four') ON CONFLICT DO NOTHING`)

	userA := adminAccess(811)
	userB := adminAccess(812)
	prac := uint(810)
	vf := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	_, err := svc.CreateWorkingSchedule(CreateWorkingScheduleRequest{
		PractitionerID: prac, ServiceID: 10, Weekday: int(time.Monday),
		StartTime: "08:00", EndTime: "16:00", ValidFrom: vf,
	}, userA)
	if err != nil {
		t.Fatal(err)
	}
	at30, err := svc.CreateAppointmentType(CreateAppointmentTypeRequest{
		Code: "IDEM-30", Name: "Idem 30", DefaultDurationMinutes: 30,
	}, userA)
	if err != nil {
		t.Fatal(err)
	}
	at45, err := svc.CreateAppointmentType(CreateAppointmentTypeRequest{
		Code: "IDEM-45", Name: "Idem 45", DefaultDurationMinutes: 45,
	}, userA)
	if err != nil {
		t.Fatal(err)
	}

	key := "abc"
	startA := time.Date(2026, 9, 14, 9, 0, 0, 0, time.UTC)
	startB := time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)

	// F — User A key abc + User B key abc both succeed independently
	a1, reused, err := svc.BookAppointment(BookAppointmentRequest{
		PatientID: 811, ServiceID: 10, PractitionerID: &prac,
		AppointmentTypeID: &at30.ID, StartAt: startA, IdempotencyKey: key, Reason: "a",
	}, userA)
	if err != nil || reused {
		t.Fatalf("F userA: err=%v reused=%v", err, reused)
	}
	b1, reused, err := svc.BookAppointment(BookAppointmentRequest{
		PatientID: 812, ServiceID: 10, PractitionerID: &prac,
		AppointmentTypeID: &at30.ID, StartAt: startB, IdempotencyKey: key, Reason: "b",
	}, userB)
	if err != nil || reused {
		t.Fatalf("F userB: err=%v reused=%v", err, reused)
	}
	if a1.ID == b1.ID {
		t.Fatal("F distinct callers must create distinct appointments")
	}

	// G — same caller/key/same semantic → same appointment
	a2, reused, err := svc.BookAppointment(BookAppointmentRequest{
		PatientID: 811, ServiceID: 10, PractitionerID: &prac,
		AppointmentTypeID: &at30.ID, StartAt: startA, IdempotencyKey: key, Reason: "a",
	}, userA)
	if err != nil || !reused || a2.ID != a1.ID {
		t.Fatalf("G reuse want same id=%d got id=%d reused=%v err=%v", a1.ID, a2.ID, reused, err)
	}

	// H — different patient → 409
	_, _, err = svc.BookAppointment(BookAppointmentRequest{
		PatientID: 813, ServiceID: 10, PractitionerID: &prac,
		AppointmentTypeID: &at30.ID, StartAt: startA, IdempotencyKey: key, Reason: "a",
	}, userA)
	if statusOf(err) != 409 {
		t.Fatalf("H patient want 409 got %d %v", statusOf(err), err)
	}

	// I — different service → 409 (need schedule on service 11)
	_ = db.Exec(`INSERT INTO staff_service_assignments(profile_id, service_id, active) VALUES (110,11,true) ON CONFLICT DO NOTHING`)
	_, err = svc.CreateWorkingSchedule(CreateWorkingScheduleRequest{
		PractitionerID: prac, ServiceID: 11, Weekday: int(time.Monday),
		StartTime: "08:00", EndTime: "16:00", ValidFrom: vf,
	}, userA)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = svc.BookAppointment(BookAppointmentRequest{
		PatientID: 811, ServiceID: 11, PractitionerID: &prac,
		AppointmentTypeID: &at30.ID, StartAt: startA, IdempotencyKey: key, Reason: "a",
	}, userA)
	if statusOf(err) != 409 {
		t.Fatalf("I service want 409 got %d %v", statusOf(err), err)
	}

	// J — different start → 409
	_, _, err = svc.BookAppointment(BookAppointmentRequest{
		PatientID: 811, ServiceID: 10, PractitionerID: &prac,
		AppointmentTypeID: &at30.ID, StartAt: startB, IdempotencyKey: key, Reason: "a",
	}, userA)
	if statusOf(err) != 409 {
		t.Fatalf("J start want 409 got %d %v", statusOf(err), err)
	}

	// K — different appointment type → 409
	_, _, err = svc.BookAppointment(BookAppointmentRequest{
		PatientID: 811, ServiceID: 10, PractitionerID: &prac,
		AppointmentTypeID: &at45.ID, StartAt: startA, IdempotencyKey: key, Reason: "a",
	}, userA)
	if statusOf(err) != 409 {
		t.Fatalf("K type want 409 got %d %v", statusOf(err), err)
	}

	// L — specific practitioner changed → 409
	_ = db.Exec(`INSERT INTO users(id, name) VALUES (813,'DrIdem2') ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO staff_profiles(id, user_id, active, primary_service_id) VALUES (113,813,true,10) ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO staff_service_assignments(profile_id, service_id, active) VALUES (113,10,true) ON CONFLICT DO NOTHING`)
	prac2 := uint(813)
	_, err = svc.CreateWorkingSchedule(CreateWorkingScheduleRequest{
		PractitionerID: prac2, ServiceID: 10, Weekday: int(time.Monday),
		StartTime: "08:00", EndTime: "16:00", ValidFrom: vf,
	}, userA)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = svc.BookAppointment(BookAppointmentRequest{
		PatientID: 811, ServiceID: 10, PractitionerID: &prac2,
		AppointmentTypeID: &at30.ID, StartAt: startA, IdempotencyKey: key, Reason: "a",
	}, userA)
	if statusOf(err) != 409 {
		t.Fatalf("L practitioner want 409 got %d %v", statusOf(err), err)
	}

	// Index shape: caller-scoped unique present; global gone
	var idxCaller, idxGlobal int64
	_ = db.Raw(`SELECT COUNT(1) FROM pg_indexes WHERE schemaname=current_schema() AND indexname='ux_pq_appt_idempotency_caller'`).Scan(&idxCaller)
	_ = db.Raw(`SELECT COUNT(1) FROM pg_indexes WHERE schemaname=current_schema() AND indexname='ux_pq_appt_idempotency'`).Scan(&idxGlobal)
	if idxCaller != 1 {
		t.Fatalf("caller-scoped index missing: %d", idxCaller)
	}
	if idxGlobal != 0 {
		t.Fatalf("old global index still present: %d", idxGlobal)
	}
}

func TestPostgresIdempotencyConcurrentRetries23D(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	_ = scheduling.SetLocation("UTC")
	_ = EnsureAppointmentIndexes(db)

	_ = db.Exec(`INSERT INTO users(id, name) VALUES (820,'DrConc'),(821,'ConcActor') ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO staff_profiles(id, user_id, active, primary_service_id) VALUES
		(120,820,true,10),(121,821,true,10) ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO staff_service_assignments(profile_id, service_id, active) VALUES
		(120,10,true),(121,10,true) ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO patients(id, code_patient, nom, prenoms) VALUES
		(821,'PCX1','Conc','One'),(822,'PCX2','Conc','Two') ON CONFLICT DO NOTHING`)

	admin := adminAccess(821)
	prac := uint(820)
	vf := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	_, err := svc.CreateWorkingSchedule(CreateWorkingScheduleRequest{
		PractitionerID: prac, ServiceID: 10, Weekday: int(time.Monday),
		StartTime: "08:00", EndTime: "12:00", ValidFrom: vf,
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	at, err := svc.CreateAppointmentType(CreateAppointmentTypeRequest{
		Code: "CONC-30", Name: "Conc 30", DefaultDurationMinutes: 30,
	}, admin)
	if err != nil {
		t.Fatal(err)
	}

	// M — concurrent identical idempotent requests → both success, same ID, one row
	key := "concurrent-same"
	start := time.Date(2026, 9, 14, 9, 0, 0, 0, time.UTC)
	type result struct {
		appt *Appointment
		err  error
	}
	ch := make(chan result, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			appt, _, e := svc.BookAppointment(BookAppointmentRequest{
				PatientID: 821, ServiceID: 10, PractitionerID: &prac,
				AppointmentTypeID: &at.ID, StartAt: start, IdempotencyKey: key, Reason: "same",
			}, admin)
			ch <- result{appt, e}
		}()
	}
	wg.Wait()
	close(ch)
	var ids []uint
	for r := range ch {
		if r.err != nil {
			t.Fatalf("M concurrent identical err: %v", r.err)
		}
		ids = append(ids, r.appt.ID)
	}
	if len(ids) != 2 || ids[0] != ids[1] {
		t.Fatalf("M want same appointment ids got %v", ids)
	}
	var n int64
	db.Model(&Appointment{}).Where("idempotency_key=? AND created_by=?", key, 821).Count(&n)
	if n != 1 {
		t.Fatalf("M want exactly 1 row got %d", n)
	}

	// N — concurrent different semantic same caller/key → one created, other 409
	key2 := "concurrent-diff"
	start2 := time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)
	type resN struct {
		ok  bool
		st  int
		id  uint
	}
	chN := make(chan resN, 2)
	var wgN sync.WaitGroup
	wgN.Add(2)
	go func() {
		defer wgN.Done()
		appt, _, e := svc.BookAppointment(BookAppointmentRequest{
			PatientID: 821, ServiceID: 10, PractitionerID: &prac,
			AppointmentTypeID: &at.ID, StartAt: start2, IdempotencyKey: key2, Reason: "n1",
		}, admin)
		if e != nil {
			chN <- resN{ok: false, st: statusOf(e)}
			return
		}
		chN <- resN{ok: true, id: appt.ID}
	}()
	go func() {
		defer wgN.Done()
		appt, _, e := svc.BookAppointment(BookAppointmentRequest{
			PatientID: 822, ServiceID: 10, PractitionerID: &prac, // different patient = different semantics
			AppointmentTypeID: &at.ID, StartAt: start2, IdempotencyKey: key2, Reason: "n1",
		}, admin)
		if e != nil {
			chN <- resN{ok: false, st: statusOf(e)}
			return
		}
		chN <- resN{ok: true, id: appt.ID}
	}()
	wgN.Wait()
	close(chN)
	okN, fail409 := 0, 0
	for r := range chN {
		if r.ok {
			okN++
		} else if r.st == 409 {
			fail409++
		} else {
			t.Fatalf("N unexpected status=%d", r.st)
		}
	}
	if okN != 1 || fail409 != 1 {
		t.Fatalf("N want 1 success + 1x409 got ok=%d fail409=%d", okN, fail409)
	}
	db.Model(&Appointment{}).Where("idempotency_key=? AND created_by=?", key2, 821).Count(&n)
	if n != 1 {
		t.Fatalf("N want exactly 1 created got %d", n)
	}
}
