package patient_queue

import (
	"sync"
	"testing"
	"time"
)

func TestPostgresWorkingSchedules23B(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)

	_ = db.Exec(`INSERT INTO users(id, name) VALUES (200,'DrA'),(201,'DrB'),(202,'Mgr') ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO staff_profiles(id, user_id, active, primary_service_id) VALUES
		(20,200,true,10),(21,201,true,11),(22,202,true,10) ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO staff_service_assignments(profile_id, service_id, active) VALUES
		(20,10,true),(20,11,true),(21,11,true),(22,10,true) ON CONFLICT DO NOTHING`)

	admin := adminAccess(202)
	vf := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

	// A. create working schedule
	s1, err := svc.CreateWorkingSchedule(CreateWorkingScheduleRequest{
		PractitionerID: 200, ServiceID: 10, Weekday: int(time.Monday),
		StartTime: "08:00", EndTime: "12:00", ValidFrom: vf,
	}, admin)
	if err != nil {
		t.Fatalf("A create: %v", err)
	}
	if s1.StartTime != "08:00:00" || !s1.Active {
		t.Fatalf("unexpected schedule %+v", s1)
	}

	// B. overlapping rejected
	_, err = svc.CreateWorkingSchedule(CreateWorkingScheduleRequest{
		PractitionerID: 200, ServiceID: 10, Weekday: int(time.Monday),
		StartTime: "10:00", EndTime: "14:00", ValidFrom: vf,
	}, admin)
	if statusOf(err) != 409 {
		t.Fatalf("B overlap want 409 got %d %v", statusOf(err), err)
	}

	// C. adjacent accepted
	s2, err := svc.CreateWorkingSchedule(CreateWorkingScheduleRequest{
		PractitionerID: 200, ServiceID: 10, Weekday: int(time.Monday),
		StartTime: "12:00", EndTime: "16:00", ValidFrom: vf,
	}, admin)
	if err != nil {
		t.Fatalf("C adjacent: %v", err)
	}

	// E. multiple daily windows (already have morning+afternoon)
	_ = s2

	// F. valid_from / valid_until — non-overlapping validity allows same clock window
	until := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	_, err = svc.UpdateWorkingSchedule(s1.ID, UpdateWorkingScheduleRequest{ValidUntil: &until}, admin)
	if err != nil {
		t.Fatalf("F update until: %v", err)
	}
	nextFrom := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	_, err = svc.CreateWorkingSchedule(CreateWorkingScheduleRequest{
		PractitionerID: 200, ServiceID: 10, Weekday: int(time.Monday),
		StartTime: "08:00", EndTime: "12:00", ValidFrom: nextFrom,
	}, admin)
	if err != nil {
		t.Fatalf("F non-overlap validity: %v", err)
	}

	// Different service same day allowed
	_, err = svc.CreateWorkingSchedule(CreateWorkingScheduleRequest{
		PractitionerID: 200, ServiceID: 11, Weekday: int(time.Monday),
		StartTime: "08:00", EndTime: "12:00", ValidFrom: vf,
	}, admin)
	if err != nil {
		t.Fatalf("different service: %v", err)
	}

	// Invalid practitioner
	_, err = svc.CreateWorkingSchedule(CreateWorkingScheduleRequest{
		PractitionerID: 9999, ServiceID: 10, Weekday: int(time.Tuesday),
		StartTime: "08:00", EndTime: "12:00", ValidFrom: vf,
	}, admin)
	if statusOf(err) != 400 {
		t.Fatalf("invalid practitioner want 400 got %d %v", statusOf(err), err)
	}

	// Not assigned to service
	_, err = svc.CreateWorkingSchedule(CreateWorkingScheduleRequest{
		PractitionerID: 201, ServiceID: 10, Weekday: int(time.Tuesday),
		StartTime: "08:00", EndTime: "12:00", ValidFrom: vf,
	}, admin)
	if statusOf(err) != 400 {
		t.Fatalf("not assigned want 400 got %d %v", statusOf(err), err)
	}

	// G. negative exception
	start := time.Date(2026, 9, 14, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 9, 14, 10, 30, 0, 0, time.UTC)
	ex, err := svc.CreateScheduleException(CreateScheduleExceptionRequest{
		PractitionerID: 200, ServiceID: 10, Type: ExMeeting,
		StartAt: start, EndAt: end, Reason: "staff meeting",
	}, admin)
	if err != nil {
		t.Fatalf("G exception: %v", err)
	}
	if !IsNegativeException(ex.Type) {
		t.Fatal("expected negative")
	}

	// H. EXTRA_AVAILABILITY
	satStart := time.Date(2026, 9, 19, 9, 0, 0, 0, time.UTC)
	satEnd := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	pos, err := svc.CreateScheduleException(CreateScheduleExceptionRequest{
		PractitionerID: 200, ServiceID: 10, Type: ExExtraAvailability,
		StartAt: satStart, EndAt: satEnd,
	}, admin)
	if err != nil {
		t.Fatalf("H extra: %v", err)
	}
	if !IsPositiveException(pos.Type) {
		t.Fatal("expected positive")
	}

	// Zero-length exception
	_, err = svc.CreateScheduleException(CreateScheduleExceptionRequest{
		PractitionerID: 200, ServiceID: 10, Type: ExBlocked,
		StartAt: start, EndAt: start,
	}, admin)
	if statusOf(err) != 400 {
		t.Fatalf("zero length want 400 got %d", statusOf(err))
	}

	// Same-polarity overlap rejected; cross-polarity allowed
	_, err = svc.CreateScheduleException(CreateScheduleExceptionRequest{
		PractitionerID: 200, ServiceID: 10, Type: ExLeave,
		StartAt: start.Add(30 * time.Minute), EndAt: end.Add(time.Hour),
	}, admin)
	if statusOf(err) != 409 {
		t.Fatalf("neg+neg overlap want 409 got %d %v", statusOf(err), err)
	}
	_, err = svc.CreateScheduleException(CreateScheduleExceptionRequest{
		PractitionerID: 200, ServiceID: 10, Type: ExExtraAvailability,
		StartAt: start, EndAt: end, // overlaps negative MEETING — allowed; negative wins in 23C
	}, admin)
	if err != nil {
		t.Fatalf("pos+neg overlap should allow: %v", err)
	}

	// N. audit actor from authenticated user
	var audits []ScheduleAuditEvent
	if err := db.Where("entity_type=? AND entity_id=?", EntitySchedule, s1.ID).Find(&audits).Error; err != nil {
		t.Fatal(err)
	}
	if len(audits) == 0 || audits[0].ActorUserID != 202 {
		t.Fatalf("audit actor want 202 got %+v", audits)
	}

	// I / J / K / L / M — service scope + IDOR
	scoped := scopedAccess(201, 11, "schedule.read.service", "schedule.manage.service")
	// seed assignment for 201 on 11 already; manager scoped to 11 only via primary + assignment
	_ = db.Exec(`INSERT INTO staff_profiles(id, user_id, active, primary_service_id) VALUES (30,210,true,10) ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO staff_service_assignments(profile_id, service_id, active) VALUES (30,10,true) ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO users(id, name) VALUES (210,'Other') ON CONFLICT DO NOTHING`)

	other := scopedAccess(210, 10, "schedule.read.service", "schedule.manage.service")
	// Create schedule on service 11 as admin
	s11, err := svc.CreateWorkingSchedule(CreateWorkingScheduleRequest{
		PractitionerID: 201, ServiceID: 11, Weekday: int(time.Wednesday),
		StartTime: "09:00", EndTime: "11:00", ValidFrom: vf,
	}, admin)
	if err != nil {
		t.Fatalf("setup s11: %v", err)
	}

	// J unauthorized GET by ID (user 210 only scope service 10)
	_, err = svc.GetWorkingSchedule(s11.ID, other)
	if statusOf(err) != 404 {
		t.Fatalf("J IDOR GET want 404 got %d %v", statusOf(err), err)
	}
	// K unauthorized UPDATE
	active := false
	_, err = svc.UpdateWorkingSchedule(s11.ID, UpdateWorkingScheduleRequest{Active: &active}, other)
	if statusOf(err) != 404 && statusOf(err) != 403 {
		t.Fatalf("K IDOR UPDATE want 403/404 got %d %v", statusOf(err), err)
	}
	// L unauthorized DELETE/CANCEL
	_, err = svc.DisableWorkingSchedule(s11.ID, other)
	if statusOf(err) != 404 && statusOf(err) != 403 {
		t.Fatalf("L IDOR DELETE want 403/404 got %d %v", statusOf(err), err)
	}

	// M changing service requires target authorization
	target := uint(10)
	_, err = svc.UpdateWorkingSchedule(s11.ID, UpdateWorkingScheduleRequest{ServiceID: &target}, scoped)
	// scoped (201) has manage.service on 11; moving to 10 — need authorize target; 201 assigned to 11 only
	if statusOf(err) != 404 && statusOf(err) != 403 && statusOf(err) != 400 {
		t.Fatalf("M retarget want deny got %d %v", statusOf(err), err)
	}

	// Own schedule from auth context
	own := scopedAccess(200, 10, "schedule.read.own")
	mine, err := svc.ListMyWorkingSchedules(own, true)
	if err != nil {
		t.Fatalf("mine: %v", err)
	}
	for _, row := range mine {
		if row.PractitionerID != 200 {
			t.Fatalf("own schedule leaked other practitioner %d", row.PractitionerID)
		}
	}
	if len(mine) == 0 {
		t.Fatal("expected own schedules")
	}

	// 23C contract methods
	wins, err := svc.ListApplicableWorkingWindows(200, 10, vf, vf.AddDate(0, 1, 0))
	if err != nil || len(wins) == 0 {
		t.Fatalf("23C windows: %v len=%d", err, len(wins))
	}
	exs, err := svc.ListApplicableExceptions(200, 10, start.Add(-time.Hour), end.Add(time.Hour))
	if err != nil || len(exs) == 0 {
		t.Fatalf("23C exceptions: %v len=%d", err, len(exs))
	}

	// O. legacy appointment/check-in still works
	_ = db.Exec(`INSERT INTO patients(id, code_patient, nom, prenoms) VALUES (501,'P-S-1','Sched','Test') ON CONFLICT DO NOTHING`)
	tk, err := svc.CheckInWalkIn(WalkInCheckInRequest{
		PatientID: 501, ServiceID: 10, IdentityConfirmed: true,
	}, adminAccess(100))
	if err != nil {
		t.Fatalf("O walk-in: %v", err)
	}
	if tk.Reference == "" {
		t.Fatal("O missing ticket reference")
	}

// Appointments not mutated by schedule disable
	seedPractitionerForService(t, db, 3, 102, 10)
	seedAllDaySchedules(t, db, 102, 10)
	apptStart := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	apptEnd := apptStart.Add(30 * time.Minute)
	appt, err := svc.CreateAppointment(CreateAppointmentRequest{
		PatientID: 501, ServiceID: 10, ScheduledAt: apptStart, ScheduledEndAt: &apptEnd,
	}, adminAccess(100))
	if err != nil {
		t.Fatalf("create appt: %v", err)
	}
	_, _ = svc.DisableWorkingSchedule(s1.ID, admin)
	var appt2 Appointment
	if err := db.First(&appt2, appt.ID).Error; err != nil || appt2.Status != appt.Status {
		t.Fatalf("schedule disable must not mutate appointment: %+v err=%v", appt2, err)
	}
}

func TestPostgresConcurrentScheduleOverlap23B(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	_ = db.Exec(`INSERT INTO users(id, name) VALUES (300,'DrC') ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO staff_profiles(id, user_id, active, primary_service_id) VALUES (40,300,true,10) ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO staff_service_assignments(profile_id, service_id, active) VALUES (40,10,true) ON CONFLICT DO NOTHING`)

	admin := adminAccess(100)
	vf := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	const n = 8
	ok := make(chan bool, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.CreateWorkingSchedule(CreateWorkingScheduleRequest{
				PractitionerID: 300, ServiceID: 10, Weekday: int(time.Thursday),
				StartTime: "08:00", EndTime: "12:00", ValidFrom: vf,
			}, admin)
			ok <- err == nil
		}()
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
		t.Fatalf("D concurrent overlap: want exactly 1 success, got %d", success)
	}
	var count int64
	db.Model(&StaffWorkingSchedule{}).Where("practitioner_id=? AND service_id=? AND weekday=? AND active", 300, 10, int(time.Thursday)).Count(&count)
	if count != 1 {
		t.Fatalf("D want 1 row got %d", count)
	}
}
