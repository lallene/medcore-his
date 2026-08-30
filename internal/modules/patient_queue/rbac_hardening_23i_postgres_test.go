package patient_queue

import (
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/core/scheduling"
)

// LOT 23I — RBAC hardening: read.all must not enlarge manage.service;
// queue.read.all must not globalize appointment.*.service / create.service.

func TestPostgresRBAC01_ReadAllDoesNotEnlargeManageService(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	_ = scheduling.SetLocation("UTC")

	_ = db.Exec(`INSERT INTO users(id, name) VALUES (2301,'DirMedLike'),(2302,'PracA'),(2303,'PracB') ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO staff_profiles(id, user_id, active, primary_service_id) VALUES
		(2301,2301,true,10),(2302,2302,true,10),(2303,2303,true,11) ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO staff_service_assignments(profile_id, service_id, active) VALUES
		(2301,10,true),(2302,10,true),(2303,11,true) ON CONFLICT DO NOTHING`)

	admin := adminAccess(2301)
	vf := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

	schedA, err := svc.CreateWorkingSchedule(CreateWorkingScheduleRequest{
		PractitionerID: 2302, ServiceID: 10, Weekday: int(time.Monday),
		StartTime: "08:00", EndTime: "12:00", ValidFrom: vf,
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	schedB, err := svc.CreateWorkingSchedule(CreateWorkingScheduleRequest{
		PractitionerID: 2303, ServiceID: 11, Weekday: int(time.Monday),
		StartTime: "08:00", EndTime: "12:00", ValidFrom: vf,
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	exB, err := svc.CreateScheduleException(CreateScheduleExceptionRequest{
		PractitionerID: 2303, ServiceID: 11,
		StartAt: time.Date(2026, 9, 14, 8, 0, 0, 0, time.UTC),
		EndAt:   time.Date(2026, 9, 14, 9, 0, 0, 0, time.UTC),
		Type:    ExAbsence,
	}, admin)
	if err != nil {
		t.Fatal(err)
	}

	actor := scopedAccess(2301, 10, "schedule.read.all", "schedule.manage.service")

	// Global READ still allowed
	if _, err := svc.GetWorkingSchedule(schedB.ID, actor); err != nil {
		t.Fatalf("read.all must read schedule B: %v", err)
	}
	if _, err := svc.GetScheduleException(exB.ID, actor); err != nil {
		t.Fatalf("read.all must read exception B: %v", err)
	}

	// Mutations on service B denied
	_, err = svc.CreateWorkingSchedule(CreateWorkingScheduleRequest{
		PractitionerID: 2303, ServiceID: 11, Weekday: int(time.Tuesday),
		StartTime: "08:00", EndTime: "12:00", ValidFrom: vf,
	}, actor)
	if st := statusOf(err); st != 404 && st != 403 {
		t.Fatalf("create schedule B want 403/404 got %d %v", st, err)
	}

	active := false
	_, err = svc.UpdateWorkingSchedule(schedB.ID, UpdateWorkingScheduleRequest{Active: &active}, actor)
	if st := statusOf(err); st != 404 && st != 403 {
		t.Fatalf("update schedule B want 403/404 got %d %v", st, err)
	}
	_, err = svc.DisableWorkingSchedule(schedB.ID, actor)
	if st := statusOf(err); st != 404 && st != 403 {
		t.Fatalf("disable schedule B want 403/404 got %d %v", st, err)
	}

	_, err = svc.CreateScheduleException(CreateScheduleExceptionRequest{
		PractitionerID: 2303, ServiceID: 11,
		StartAt: time.Date(2026, 9, 15, 8, 0, 0, 0, time.UTC),
		EndAt:   time.Date(2026, 9, 15, 9, 0, 0, 0, time.UTC),
		Type:    ExAbsence,
	}, actor)
	if st := statusOf(err); st != 404 && st != 403 {
		t.Fatalf("create exception B want 403/404 got %d %v", st, err)
	}
	_, err = svc.UpdateScheduleException(exB.ID, UpdateScheduleExceptionRequest{Reason: strPtr("x")}, actor)
	if st := statusOf(err); st != 404 && st != 403 {
		t.Fatalf("update exception B want 403/404 got %d %v", st, err)
	}
	_, err = svc.CancelScheduleException(exB.ID, actor)
	if st := statusOf(err); st != 404 && st != 403 {
		t.Fatalf("cancel exception B want 403/404 got %d %v", st, err)
	}

	// Still manage service A
	_, err = svc.CreateWorkingSchedule(CreateWorkingScheduleRequest{
		PractitionerID: 2302, ServiceID: 10, Weekday: int(time.Wednesday),
		StartTime: "08:00", EndTime: "12:00", ValidFrom: vf,
	}, actor)
	if err != nil {
		t.Fatalf("manage service A must succeed: %v", err)
	}
	_, err = svc.UpdateWorkingSchedule(schedA.ID, UpdateWorkingScheduleRequest{Active: &active}, actor)
	if err != nil {
		t.Fatalf("update schedule A: %v", err)
	}
}

func TestPostgresRBAC02_QueueReadAllDoesNotGlobalizeLifecycle(t *testing.T) {
	db, svc, admin, prac900, _, at := lifeSetup(t)
	_ = EnsureAppointmentIndexes(db)

	// Appointment on service 10 (A)
	apptA := bookLife(t, svc, admin, 901, prac900, time.Date(2026, 9, 14, 9, 0, 0, 0, time.UTC), at.ID)

	// Schedule + appointment on service 11 (B)
	_ = db.Exec(`INSERT INTO staff_service_assignments(profile_id, service_id, active) VALUES (200,11,true) ON CONFLICT DO NOTHING`)
	vf := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	if _, err := svc.CreateWorkingSchedule(CreateWorkingScheduleRequest{
		PractitionerID: 900, ServiceID: 11, Weekday: int(time.Monday),
		StartTime: "08:00", EndTime: "16:00", ValidFrom: vf,
	}, admin); err != nil {
		t.Fatal(err)
	}
	prac := uint(900)
	apptB, _, err := svc.BookAppointment(BookAppointmentRequest{
		PatientID: 904, ServiceID: 11, PractitionerID: &prac,
		AppointmentTypeID: &at.ID, StartAt: time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC),
	}, admin)
	if err != nil {
		t.Fatal(err)
	}

	actor := scopedAccess(902, 10,
		"queue.read.all",
		"appointment.cancel.service",
		"appointment.no_show.service",
		"appointment.reschedule.service",
	)

	curB := mustReload(t, db, apptB.ID)
	req := rsReq(curB, time.Date(2026, 9, 14, 11, 0, 0, 0, time.UTC))
	if _, err := svc.RescheduleAppointment(apptB.ID, req, actor); statusOf(err) != 404 {
		t.Fatalf("reschedule B want 404 got %d %v", statusOf(err), err)
	}
	if _, err := svc.CancelAppointment(apptB.ID, CancelAppointmentRequest{Reason: "x"}, actor); statusOf(err) != 404 {
		t.Fatalf("cancel B want 404 got %d %v", statusOf(err), err)
	}
	_ = db.Model(&Appointment{}).Where("id=?", apptB.ID).Update("scheduled_at", time.Now().UTC().Add(-time.Hour))
	if _, err := svc.MarkNoShow(apptB.ID, NoShowAppointmentRequest{}, actor); statusOf(err) != 404 {
		t.Fatalf("no-show B want 404 got %d %v", statusOf(err), err)
	}

	// Service A still allowed
	curA := mustReload(t, db, apptA.ID)
	reqA := rsReq(curA, time.Date(2026, 9, 14, 10, 30, 0, 0, time.UTC))
	if _, err := svc.RescheduleAppointment(apptA.ID, reqA, actor); err != nil {
		t.Fatalf("reschedule A: %v", err)
	}
	open := bookLife(t, svc, admin, 903, prac900, time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC), at.ID)
	if _, err := svc.CancelAppointment(open.ID, CancelAppointmentRequest{Reason: "ok"}, actor); err != nil {
		t.Fatalf("cancel A: %v", err)
	}
}

func TestPostgresBookingPermissions23I(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	_ = scheduling.SetLocation("UTC")
	_ = EnsureAppointmentIndexes(db)

	_ = db.Exec(`INSERT INTO users(id, name) VALUES (2400,'DrBook'),(2401,'BookerA'),(2402,'BookerB') ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO staff_profiles(id, user_id, active, primary_service_id) VALUES
		(2400,2400,true,10),(2401,2401,true,10),(2402,2402,true,11) ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO staff_service_assignments(profile_id, service_id, active) VALUES
		(2400,10,true),(2400,11,true),(2401,10,true),(2402,11,true) ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO patients(id, code_patient, nom, prenoms) VALUES
		(2401,'P23I1','Book','A'),(2402,'P23I2','Book','B') ON CONFLICT DO NOTHING`)

	admin := adminAccess(2401)
	vf := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for _, svcID := range []uint{10, 11} {
		if _, err := svc.CreateWorkingSchedule(CreateWorkingScheduleRequest{
			PractitionerID: 2400, ServiceID: svcID, Weekday: int(time.Monday),
			StartTime: "08:00", EndTime: "16:00", ValidFrom: vf,
		}, admin); err != nil {
			t.Fatal(err)
		}
	}
	at, err := svc.CreateAppointmentType(CreateAppointmentTypeRequest{
		Code: "B23I-30", Name: "Book 30", DefaultDurationMinutes: 30,
	}, admin)
	if err != nil {
		t.Fatal(err)
	}

	prac := uint(2400)
	startA := time.Date(2026, 9, 14, 9, 0, 0, 0, time.UTC)
	startB := time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)
	reqA := BookAppointmentRequest{
		PatientID: 2401, ServiceID: 10, PractitionerID: &prac,
		AppointmentTypeID: &at.ID, StartAt: startA,
	}
	reqB := BookAppointmentRequest{
		PatientID: 2402, ServiceID: 11, PractitionerID: &prac,
		AppointmentTypeID: &at.ID, StartAt: startB,
	}

	none := scopedAccess(2401, 10, "queue.checkin", "schedule.read.service")
	if _, _, err := svc.BookAppointment(reqA, none); statusOf(err) != 403 {
		t.Fatalf("no create perm want 403 got %d %v", statusOf(err), err)
	}
	checkinOnly := scopedAccess(2401, 10, "queue.checkin")
	if _, _, err := svc.BookAppointment(reqA, checkinOnly); statusOf(err) != 403 {
		t.Fatalf("POST /api/appointments equivalent: queue.checkin alone want 403 got %d %v", statusOf(err), err)
	}
	// Legacy CreateAppointment (POST /api/queue/appointments) — same service authority
	legacyStart := time.Date(2026, 9, 14, 9, 30, 0, 0, time.UTC)
	legacyEnd := legacyStart.Add(30 * time.Minute)
	if _, err := svc.CreateAppointment(CreateAppointmentRequest{
		PatientID: 2401, ServiceID: 10, ExpectedDoctorID: &prac,
		ScheduledAt: legacyStart, ScheduledEndAt: &legacyEnd,
	}, checkinOnly); statusOf(err) != 403 {
		t.Fatalf("POST /api/queue/appointments equivalent: queue.checkin alone want 403 got %d %v", statusOf(err), err)
	}

	createSvc := scopedAccess(2401, 10, "appointment.create.service")
	if _, _, err := svc.BookAppointment(reqA, createSvc); err != nil {
		t.Fatalf("create.service A: %v", err)
	}
	if _, _, err := svc.BookAppointment(reqB, createSvc); statusOf(err) != 403 {
		t.Fatalf("create.service B want 403 got %d %v", statusOf(err), err)
	}

	createPlusQueueAll := scopedAccess(2401, 10, "appointment.create.service", "queue.read.all")
	startB2 := time.Date(2026, 9, 14, 11, 0, 0, 0, time.UTC)
	reqB2 := reqB
	reqB2.StartAt = startB2
	if _, _, err := svc.BookAppointment(reqB2, createPlusQueueAll); statusOf(err) != 403 {
		t.Fatalf("create.service+queue.read.all B want 403 got %d %v", statusOf(err), err)
	}

	createAll := scopedAccess(2401, 10, "appointment.create.all")
	if _, _, err := svc.BookAppointment(reqB2, createAll); err != nil {
		t.Fatalf("create.all B: %v", err)
	}

	// Check-in still works with queue.checkin only
	booked, _, err := svc.BookAppointment(BookAppointmentRequest{
		PatientID: 2401, ServiceID: 10, PractitionerID: &prac,
		AppointmentTypeID: &at.ID, StartAt: time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC),
	}, createSvc)
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Model(&Appointment{}).Where("id=?", booked.ID).Updates(map[string]any{
		"scheduled_at":     time.Now().UTC().Add(-15 * time.Minute),
		"scheduled_end_at": time.Now().UTC().Add(15 * time.Minute),
	})
	if _, _, err := svc.CheckInAppointment(booked.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, checkinOnly); err != nil {
		t.Fatalf("check-in with queue.checkin: %v", err)
	}
}

func TestPostgresAppointmentReadDenyNonSchedulePerms23I(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	from := time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	f := AppointmentListFilter{From: from, To: to}

	for _, perm := range []string{"patients:read", "consultations.read", "queue.checkin"} {
		a := scopedAccess(2500, 10, perm)
		if _, err := svc.ListAppointments(f, a); statusOf(err) != 403 {
			t.Fatalf("%s list want 403 got %d %v", perm, statusOf(err), err)
		}
		if _, err := svc.GetAppointment(1, a); statusOf(err) != 403 && statusOf(err) != 404 {
			t.Fatalf("%s get want 403/404 got %d %v", perm, statusOf(err), err)
		}
	}
}

func strPtr(s string) *string { return &s }
