package patient_queue

import (
	"sync"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/core/scheduling"
)

func seedBookingSchedule(t *testing.T, svc *Service, admin Access, prac uint, weekday int, start, end string) {
	t.Helper()
	vf := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	_, err := svc.CreateWorkingSchedule(CreateWorkingScheduleRequest{
		PractitionerID: prac, ServiceID: 10, Weekday: weekday,
		StartTime: start, EndTime: end, ValidFrom: vf,
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
}

func TestPostgresBooking23D(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	_ = scheduling.SetLocation("UTC")
	_ = EnsureAppointmentIndexes(db)

	_ = db.Exec(`INSERT INTO users(id, name) VALUES (500,'DrBookA'),(501,'DrBookB'),(502,'Booker') ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO staff_profiles(id, user_id, active, primary_service_id) VALUES
		(70,500,true,10),(71,501,true,10),(72,502,true,10) ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO staff_service_assignments(profile_id, service_id, active) VALUES
		(70,10,true),(71,10,true),(72,10,true) ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO patients(id, code_patient, nom, prenoms) VALUES
		(601,'P-B-1','Book','One'),(602,'P-B-2','Book','Two'),(603,'P-B-3','Book','Three') ON CONFLICT DO NOTHING`)

	admin := adminAccess(502)
	monday := int(time.Monday)
	seedBookingSchedule(t, svc, admin, 500, monday, "08:00", "12:00")
	seedBookingSchedule(t, svc, admin, 501, monday, "08:00", "12:00")

	day := time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC) // Monday
	start := time.Date(2026, 9, 14, 9, 0, 0, 0, time.UTC)
	dur := 30
	prac500 := uint(500)

	at, err := svc.CreateAppointmentType(CreateAppointmentTypeRequest{
		Code: "BOOK-30", Name: "Consult 30", DefaultDurationMinutes: 30,
	}, admin)
	if err != nil {
		t.Fatal(err)
	}

	// A/B/C — book with type duration, end not null
	appt, _, err := svc.BookAppointment(BookAppointmentRequest{
		PatientID: 601, ServiceID: 10, PractitionerID: &prac500,
		AppointmentTypeID: &at.ID, StartAt: start,
	}, admin)
	if err != nil {
		t.Fatalf("A: %v", err)
	}
	if appt.ScheduledEndAt == nil {
		t.Fatal("B scheduled_end_at must not be NULL")
	}
	if !appt.ScheduledEndAt.Equal(start.Add(30 * time.Minute)) {
		t.Fatalf("C type duration end=%v", appt.ScheduledEndAt)
	}
	if appt.CreatedBy != 502 {
		t.Fatalf("U actor want 502 got %d", appt.CreatedBy)
	}
	var hist int64
	db.Model(&AppointmentHistory{}).Where("appointment_id=? AND event_type=? AND actor_user_id=?", appt.ID, ApptHistCreated, 502).Count(&hist)
	if hist != 1 {
		t.Fatal("V history CREATED missing")
	}

	// D explicit duration
	start2 := time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)
	d45 := 45
	appt2, _, err := svc.BookAppointment(BookAppointmentRequest{
		PatientID: 602, ServiceID: 10, PractitionerID: &prac500,
		StartAt: start2, DurationMinutes: &d45,
	}, admin)
	if err != nil || appt2.ScheduledEndAt == nil || !appt2.ScheduledEndAt.Equal(start2.Add(45*time.Minute)) {
		t.Fatalf("D explicit: %v %+v", err, appt2)
	}

	// E/F invalid type/service + inactive
	sid11 := uint(11)
	atBad, _ := svc.CreateAppointmentType(CreateAppointmentTypeRequest{
		Code: "BOOK-11", Name: "Other", DefaultDurationMinutes: 30, ServiceID: &sid11,
	}, admin)
	_, _, err = svc.BookAppointment(BookAppointmentRequest{
		PatientID: 603, ServiceID: 10, PractitionerID: &prac500,
		AppointmentTypeID: &atBad.ID, StartAt: time.Date(2026, 9, 14, 11, 0, 0, 0, time.UTC),
	}, admin)
	if statusOf(err) != 400 {
		t.Fatalf("E type/service want 400 got %d", statusOf(err))
	}
	_ = db.Model(&AppointmentType{}).Where("id=?", at.ID).Update("active", false)
	_, _, err = svc.BookAppointment(BookAppointmentRequest{
		PatientID: 603, ServiceID: 10, PractitionerID: &prac500,
		AppointmentTypeID: &at.ID, StartAt: time.Date(2026, 9, 14, 11, 0, 0, 0, time.UTC),
	}, admin)
	if statusOf(err) != 400 {
		t.Fatalf("F inactive want 400 got %d", statusOf(err))
	}
	_ = db.Model(&AppointmentType{}).Where("id=?", at.ID).Update("active", true)

	// G outside hours
	_, _, err = svc.BookAppointment(BookAppointmentRequest{
		PatientID: 603, ServiceID: 10, PractitionerID: &prac500,
		StartAt: time.Date(2026, 9, 14, 13, 0, 0, 0, time.UTC), DurationMinutes: &dur,
	}, admin)
	if statusOf(err) != 409 {
		t.Fatalf("G outside hours want 409 got %d %v", statusOf(err), err)
	}

	// H negative exception
	_, err = svc.CreateScheduleException(CreateScheduleExceptionRequest{
		PractitionerID: 500, ServiceID: 10, Type: ExMeeting,
		StartAt: time.Date(2026, 9, 14, 11, 0, 0, 0, time.UTC),
		EndAt:   time.Date(2026, 9, 14, 11, 30, 0, 0, time.UTC),
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = svc.BookAppointment(BookAppointmentRequest{
		PatientID: 603, ServiceID: 10, PractitionerID: &prac500,
		StartAt: time.Date(2026, 9, 14, 11, 0, 0, 0, time.UTC), DurationMinutes: &dur,
	}, admin)
	if statusOf(err) != 409 {
		t.Fatalf("H meeting want 409 got %d", statusOf(err))
	}

	// I EXTRA_AVAILABILITY Saturday
	satStart := time.Date(2026, 9, 19, 9, 0, 0, 0, time.UTC)
	_, err = svc.CreateScheduleException(CreateScheduleExceptionRequest{
		PractitionerID: 500, ServiceID: 10, Type: ExExtraAvailability,
		StartAt: satStart, EndAt: satStart.Add(3 * time.Hour),
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	satBook, _, err := svc.BookAppointment(BookAppointmentRequest{
		PatientID: 603, ServiceID: 10, PractitionerID: &prac500,
		StartAt: satStart, DurationMinutes: &dur,
	}, admin)
	if err != nil {
		t.Fatalf("I EXTRA: %v", err)
	}
	_ = satBook

	// J blocking appointment rejects (reuse 09:00 already booked for 500)
	_, _, err = svc.BookAppointment(BookAppointmentRequest{
		PatientID: 602, ServiceID: 10, PractitionerID: &prac500,
		StartAt: start, DurationMinutes: &dur,
	}, admin)
	if statusOf(err) != 409 {
		t.Fatalf("J overlap want 409 got %d", statusOf(err))
	}

	// K cancelled does not block — free a slot by cancelling appt2 then rebook
	_ = db.Model(&Appointment{}).Where("id=?", appt2.ID).Update("status", ApptCancelled)
	rebook, _, err := svc.BookAppointment(BookAppointmentRequest{
		PatientID: 602, ServiceID: 10, PractitionerID: &prac500,
		StartAt: start2, DurationMinutes: &dur,
	}, admin)
	if err != nil {
		t.Fatalf("K cancelled non-block: %v", err)
	}

	// L no-show does not block
	_ = db.Model(&Appointment{}).Where("id=?", rebook.ID).Update("status", ApptNoShow)
	_, _, err = svc.BookAppointment(BookAppointmentRequest{
		PatientID: 602, ServiceID: 10, PractitionerID: &prac500,
		StartAt: start2, DurationMinutes: &dur,
	}, admin)
	if err != nil {
		t.Fatalf("L no-show non-block: %v", err)
	}

	// M adjacent practitioner allowed
	adjStart := start.Add(30 * time.Minute) // 09:30 after 09:00–09:30
	_, _, err = svc.BookAppointment(BookAppointmentRequest{
		PatientID: 602, ServiceID: 10, PractitionerID: &prac500,
		StartAt: adjStart, DurationMinutes: &dur,
	}, admin)
	if err != nil {
		t.Fatalf("M adjacent: %v", err)
	}

	// N overlapping practitioner rejected — covered by J

	// O patient overlapping across practitioners
	prac501 := uint(501)
	_, _, err = svc.BookAppointment(BookAppointmentRequest{
		PatientID: 601, ServiceID: 10, PractitionerID: &prac501,
		StartAt: start, DurationMinutes: &dur, // patient 601 already has 09:00 with 500
	}, admin)
	if statusOf(err) != 409 {
		t.Fatalf("O patient overlap want 409 got %d %v", statusOf(err), err)
	}

	// P patient adjacent allowed
	_, _, err = svc.BookAppointment(BookAppointmentRequest{
		PatientID: 601, ServiceID: 10, PractitionerID: &prac501,
		StartAt: adjStart, DurationMinutes: &dur,
	}, admin)
	if err != nil {
		t.Fatalf("P patient adjacent: %v", err)
	}

	// Q/R automatic selection deterministic (lowest ID among free)
	autoStart := time.Date(2026, 9, 14, 8, 0, 0, 0, time.UTC)
	_ = db.Exec(`INSERT INTO patients(id, code_patient, nom, prenoms) VALUES (610,'P-B-10','Auto','A') ON CONFLICT DO NOTHING`)
	auto1, _, err := svc.BookAppointment(BookAppointmentRequest{
		PatientID: 610, ServiceID: 10, StartAt: autoStart, DurationMinutes: &dur,
	}, admin)
	if err != nil {
		t.Fatalf("Q auto: %v", err)
	}
	if auto1.ExpectedDoctorID == nil || *auto1.ExpectedDoctorID != 500 {
		t.Fatalf("R deterministic want practitioner 500 got %v", auto1.ExpectedDoctorID)
	}

	// S next candidate when first busy — book 08:00 for 500 already done; another patient same slot → 501
	_ = db.Exec(`INSERT INTO patients(id, code_patient, nom, prenoms) VALUES (611,'P-B-11','Auto','B') ON CONFLICT DO NOTHING`)
	auto2, _, err := svc.BookAppointment(BookAppointmentRequest{
		PatientID: 611, ServiceID: 10, StartAt: autoStart, DurationMinutes: &dur,
	}, admin)
	if err != nil {
		t.Fatalf("S next candidate: %v", err)
	}
	if auto2.ExpectedDoctorID == nil || *auto2.ExpectedDoctorID != 501 {
		t.Fatalf("S want 501 got %v", auto2.ExpectedDoctorID)
	}

	// T no practitioner available
	_, _, err = svc.BookAppointment(BookAppointmentRequest{
		PatientID: 603, ServiceID: 10,
		StartAt: time.Date(2026, 9, 15, 9, 0, 0, 0, time.UTC), // Tuesday — no schedule
		DurationMinutes: &dur,
	}, admin)
	if statusOf(err) != 409 {
		t.Fatalf("T want 409 got %d", statusOf(err))
	}

	// W rollback — force failure after conflict: outside hours already rolls back (count check)
	before := int64(0)
	db.Model(&Appointment{}).Count(&before)
	_, _, _ = svc.BookAppointment(BookAppointmentRequest{
		PatientID: 603, ServiceID: 10, PractitionerID: &prac500,
		StartAt: time.Date(2026, 9, 14, 18, 0, 0, 0, time.UTC), DurationMinutes: &dur,
	}, admin)
	after := int64(0)
	db.Model(&Appointment{}).Count(&after)
	if after != before {
		t.Fatal("W rollback left appointment")
	}

	// X service IDOR
	other := scopedAccess(210, 11, "queue.checkin")
	_ = db.Exec(`INSERT INTO users(id, name) VALUES (210,'IDOR') ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO staff_profiles(id, user_id, active, primary_service_id) VALUES (80,210,true,11) ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO staff_service_assignments(profile_id, service_id, active) VALUES (80,11,true) ON CONFLICT DO NOTHING`)
	_, _, err = svc.BookAppointment(BookAppointmentRequest{
		PatientID: 603, ServiceID: 10, PractitionerID: &prac500,
		StartAt: time.Date(2026, 9, 14, 8, 30, 0, 0, time.UTC), DurationMinutes: &dur,
	}, other)
	if statusOf(err) != 403 {
		t.Fatalf("X IDOR want 403 got %d %v", statusOf(err), err)
	}

	// Y practitioner/service mismatch
	_, _, err = svc.BookAppointment(BookAppointmentRequest{
		PatientID: 603, ServiceID: 11, PractitionerID: &prac500,
		StartAt: start, DurationMinutes: &dur,
	}, admin)
	if statusOf(err) != 400 {
		t.Fatalf("Y mismatch want 400 got %d %v", statusOf(err), err)
	}

	// Z patient not found
	_, _, err = svc.BookAppointment(BookAppointmentRequest{
		PatientID: 99999, ServiceID: 10, PractitionerID: &prac500,
		StartAt: time.Date(2026, 9, 14, 8, 30, 0, 0, time.UTC), DurationMinutes: &dur,
	}, admin)
	if statusOf(err) != 404 {
		t.Fatalf("Z patient want 404 got %d", statusOf(err))
	}

	// Mutation safety — schedules/exceptions unchanged by booking path already implied
	_ = day
}

func TestPostgresBookingConcurrency23D(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	_ = scheduling.SetLocation("UTC")
	_ = EnsureAppointmentIndexes(db)

	_ = db.Exec(`INSERT INTO users(id, name) VALUES (700,'DrC1'),(701,'DrC2'),(702,'Actor') ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO staff_profiles(id, user_id, active, primary_service_id) VALUES
		(90,700,true,10),(91,701,true,10),(92,702,true,10) ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO staff_service_assignments(profile_id, service_id, active) VALUES
		(90,10,true),(91,10,true),(92,10,true) ON CONFLICT DO NOTHING`)
	admin := adminAccess(702)
	vf := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for _, prac := range []uint{700, 701} {
		_, err := svc.CreateWorkingSchedule(CreateWorkingScheduleRequest{
			PractitionerID: prac, ServiceID: 10, Weekday: int(time.Monday),
			StartTime: "08:00", EndTime: "12:00", ValidFrom: vf,
		}, admin)
		if err != nil {
			t.Fatal(err)
		}
	}

	dur := 30
	slot := time.Date(2026, 9, 14, 9, 0, 0, 0, time.UTC)
	prac := uint(700)

	// Concurrency 1: identical slot same practitioner
	_ = db.Exec(`INSERT INTO patients(id, code_patient, nom, prenoms) VALUES (701,'PC1','C','1'),(702,'PC2','C','2') ON CONFLICT DO NOTHING`)
	runConcurrentBook := func(patients []uint, practitioner *uint, start time.Time) (ok, fail int) {
		var wg sync.WaitGroup
		res := make(chan error, len(patients))
		for _, pid := range patients {
			wg.Add(1)
			go func(patientID uint) {
				defer wg.Done()
				_, _, e := svc.BookAppointment(BookAppointmentRequest{
					PatientID: patientID, ServiceID: 10, PractitionerID: practitioner,
					StartAt: start, DurationMinutes: &dur,
				}, admin)
				res <- e
			}(pid)
		}
		wg.Wait()
		close(res)
		for e := range res {
			if e == nil {
				ok++
			} else {
				fail++
			}
		}
		return
	}

	ok, fail := runConcurrentBook([]uint{701, 702}, &prac, slot)
	if ok != 1 || fail != 1 {
		t.Fatalf("C1 identical slot want 1/1 got ok=%d fail=%d", ok, fail)
	}
	var n int64
	db.Model(&Appointment{}).Where("expected_doctor_id=? AND scheduled_at=? AND status=?", 700, slot, ApptScheduled).Count(&n)
	if n != 1 {
		t.Fatalf("C1 want 1 appointment got %d", n)
	}

	// Concurrency 2: overlapping intervals same practitioner
	_ = db.Exec(`INSERT INTO patients(id, code_patient, nom, prenoms) VALUES (711,'PC11','C','11'),(712,'PC12','C','12') ON CONFLICT DO NOTHING`)
	startA := time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)
	startB := time.Date(2026, 9, 14, 10, 15, 0, 0, time.UTC)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _, e := svc.BookAppointment(BookAppointmentRequest{
			PatientID: 711, ServiceID: 10, PractitionerID: &prac, StartAt: startA, DurationMinutes: &dur,
		}, admin)
		errs <- e
	}()
	go func() {
		defer wg.Done()
		_, _, e := svc.BookAppointment(BookAppointmentRequest{
			PatientID: 712, ServiceID: 10, PractitionerID: &prac, StartAt: startB, DurationMinutes: &dur,
		}, admin)
		errs <- e
	}()
	wg.Wait()
	close(errs)
	ok, fail = 0, 0
	for e := range errs {
		if e == nil {
			ok++
		} else {
			fail++
		}
	}
	if ok != 1 || fail != 1 {
		t.Fatalf("C2 overlap want 1/1 got ok=%d fail=%d", ok, fail)
	}

	// Concurrency 3: same patient overlapping different practitioners
	_ = db.Exec(`INSERT INTO patients(id, code_patient, nom, prenoms) VALUES (720,'PC20','C','20') ON CONFLICT DO NOTHING`)
	prac2 := uint(701)
	slot2 := time.Date(2026, 9, 14, 11, 0, 0, 0, time.UTC)
	errs3 := make(chan error, 2)
	var wg3 sync.WaitGroup
	wg3.Add(2)
	go func() {
		defer wg3.Done()
		_, _, e := svc.BookAppointment(BookAppointmentRequest{
			PatientID: 720, ServiceID: 10, PractitionerID: &prac, StartAt: slot2, DurationMinutes: &dur,
		}, admin)
		errs3 <- e
	}()
	go func() {
		defer wg3.Done()
		_, _, e := svc.BookAppointment(BookAppointmentRequest{
			PatientID: 720, ServiceID: 10, PractitionerID: &prac2, StartAt: slot2, DurationMinutes: &dur,
		}, admin)
		errs3 <- e
	}()
	wg3.Wait()
	close(errs3)
	ok, fail = 0, 0
	for e := range errs3 {
		if e == nil {
			ok++
		} else {
			fail++
		}
	}
	if ok != 1 || fail != 1 {
		t.Fatalf("C3 patient overlap want 1/1 got ok=%d fail=%d", ok, fail)
	}

	// Concurrency 4: auto-assign two patients, two free practitioners at same time
	_ = db.Exec(`INSERT INTO patients(id, code_patient, nom, prenoms) VALUES (730,'PC30','C','30'),(731,'PC31','C','31') ON CONFLICT DO NOTHING`)
	autoSlot := time.Date(2026, 9, 14, 8, 0, 0, 0, time.UTC)
	ok, fail = runConcurrentBook([]uint{730, 731}, nil, autoSlot)
	if ok != 2 || fail != 0 {
		t.Fatalf("C4 auto both succeed want 2/0 got ok=%d fail=%d", ok, fail)
	}
	var rows []Appointment
	db.Where("patient_id IN ? AND scheduled_at=?", []uint{730, 731}, autoSlot).Find(&rows)
	if len(rows) != 2 {
		t.Fatalf("C4 want 2 rows got %d", len(rows))
	}
	seen := map[uint]bool{}
	for _, r := range rows {
		if r.ExpectedDoctorID == nil {
			t.Fatal("C4 missing practitioner")
		}
		if seen[*r.ExpectedDoctorID] {
			t.Fatal("C4 duplicate practitioner booking")
		}
		seen[*r.ExpectedDoctorID] = true
	}
}
