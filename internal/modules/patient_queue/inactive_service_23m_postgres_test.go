package patient_queue

import (
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/core/scheduling"
)

// LOT 23M-A — inactive organization_services must not accept new scheduling activity.
func TestPostgresInactiveOrganizationServiceHardening23M(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	_ = scheduling.SetLocation("UTC")

	_ = db.Exec(`INSERT INTO users(id, name) VALUES (800,'DrInact'),(801,'MgrInact') ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO staff_profiles(id, user_id, active, primary_service_id) VALUES
		(80,800,true,10),(81,801,true,10) ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO staff_service_assignments(profile_id, service_id, active) VALUES
		(80,10,true),(81,10,true) ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO patients(id, code_patient, nom, prenoms) VALUES
		(801,'P-INACT','Inact','One') ON CONFLICT DO NOTHING`)

	admin := adminAccess(801)
	prac := uint(800)
	monday := int(time.Monday)
	vf := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	_, err := svc.CreateWorkingSchedule(CreateWorkingScheduleRequest{
		PractitionerID: prac, ServiceID: 10, Weekday: monday,
		StartTime: "08:00", EndTime: "12:00", ValidFrom: vf,
	}, admin)
	if err != nil {
		t.Fatal(err)
	}

	at, err := svc.CreateAppointmentType(CreateAppointmentTypeRequest{
		Code: "INACT-SVC-30", Name: "Inact Svc 30", DefaultDurationMinutes: 30,
	}, admin)
	if err != nil {
		t.Fatal(err)
	}

	start := time.Date(2026, 9, 14, 9, 0, 0, 0, time.UTC)
	appt, _, err := svc.BookAppointment(BookAppointmentRequest{
		PatientID: 801, ServiceID: 10, PractitionerID: &prac,
		AppointmentTypeID: &at.ID, StartAt: start,
	}, admin)
	if err != nil {
		t.Fatalf("seed book: %v", err)
	}

	// Deactivate organization service 10 — historical appointment must remain readable.
	if err := db.Exec(`UPDATE organization_services SET active=false WHERE id=10`).Error; err != nil {
		t.Fatal(err)
	}

	if _, err := svc.GetAppointment(appt.ID, admin); err != nil {
		t.Fatalf("historical read must succeed after service deactivation: %v", err)
	}

	dur := 30
	day := time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)
	_, err = svc.ComputeAvailability(AvailabilityQuery{
		ServiceID: 10, PractitionerID: &prac, DurationMinutes: &dur,
		From: day, To: day.Add(24 * time.Hour),
	}, admin)
	if statusOf(err) != 400 {
		t.Fatalf("availability inactive service want 400 got %d (%v)", statusOf(err), err)
	}

	_, _, err = svc.BookAppointment(BookAppointmentRequest{
		PatientID: 801, ServiceID: 10, PractitionerID: &prac,
		AppointmentTypeID: &at.ID, StartAt: time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC),
	}, admin)
	if statusOf(err) != 400 {
		t.Fatalf("booking inactive service want 400 got %d (%v)", statusOf(err), err)
	}

	_, err = svc.RescheduleAppointment(appt.ID, RescheduleAppointmentRequest{
		StartAt:                time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC),
		ExpectedScheduledAt:    appt.ScheduledAt,
		ExpectedScheduledEndAt: *appt.ScheduledEndAt,
		PractitionerID:         &prac,
	}, admin)
	if statusOf(err) != 400 {
		t.Fatalf("reschedule inactive service want 400 got %d (%v)", statusOf(err), err)
	}

	_, err = svc.CreateWorkingSchedule(CreateWorkingScheduleRequest{
		PractitionerID: prac, ServiceID: 10, Weekday: int(time.Tuesday),
		StartTime: "08:00", EndTime: "10:00", ValidFrom: vf,
	}, admin)
	if statusOf(err) != 400 {
		t.Fatalf("create schedule inactive service want 400 got %d (%v)", statusOf(err), err)
	}

	_, err = svc.CreateScheduleException(CreateScheduleExceptionRequest{
		PractitionerID: prac, ServiceID: 10, Type: ExBlocked,
		StartAt: time.Date(2026, 9, 15, 9, 0, 0, 0, time.UTC),
		EndAt:   time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC),
		Reason:  "blocked-inactive-svc",
	}, admin)
	if statusOf(err) != 400 {
		t.Fatalf("create exception inactive service want 400 got %d (%v)", statusOf(err), err)
	}
}
