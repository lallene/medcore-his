package patient_queue

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/core/scheduling"
	"gorm.io/gorm"
)

func lifeSetup(t *testing.T) (*gorm.DB, *Service, Access, uint, uint, *AppointmentType) {
	t.Helper()
	db := queuePostgres(t)
	svc := NewService(db)
	_ = scheduling.SetLocation("UTC")
	_ = EnsureAppointmentIndexes(db)

	_ = db.Exec(`INSERT INTO users(id, name) VALUES (900,'DrLife'),(901,'DrLife2'),(902,'LifeActor') ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO staff_profiles(id, user_id, active, primary_service_id) VALUES
		(200,900,true,10),(201,901,true,10),(202,902,true,10) ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO staff_service_assignments(profile_id, service_id, active) VALUES
		(200,10,true),(201,10,true),(202,10,true) ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO patients(id, code_patient, nom, prenoms) VALUES
		(901,'PLF1','Life','One'),(902,'PLF2','Life','Two'),(903,'PLF3','Life','Three'),
		(904,'PLF4','Life','Four'),(905,'PLF5','Life','Five') ON CONFLICT DO NOTHING`)

	admin := adminAccess(902)
	vf := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for _, prac := range []uint{900, 901} {
		if _, err := svc.CreateWorkingSchedule(CreateWorkingScheduleRequest{
			PractitionerID: prac, ServiceID: 10, Weekday: int(time.Monday),
			StartTime: "08:00", EndTime: "16:00", ValidFrom: vf,
		}, admin); err != nil {
			t.Fatal(err)
		}
	}
	at, err := svc.CreateAppointmentType(CreateAppointmentTypeRequest{
		Code: "LIFE-30", Name: "Life 30", DefaultDurationMinutes: 30,
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	return db, svc, admin, 900, 901, at
}

func bookLife(t *testing.T, svc *Service, admin Access, patient, prac uint, start time.Time, atID uint) *Appointment {
	t.Helper()
	appt, _, err := svc.BookAppointment(BookAppointmentRequest{
		PatientID: patient, ServiceID: 10, PractitionerID: &prac,
		AppointmentTypeID: &atID, StartAt: start,
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	return appt
}

func mustReload(t *testing.T, db *gorm.DB, id uint) Appointment {
	t.Helper()
	var a Appointment
	if err := db.First(&a, id).Error; err != nil {
		t.Fatal(err)
	}
	return a
}

func rsReq(cur Appointment, start time.Time) RescheduleAppointmentRequest {
	r := RescheduleAppointmentRequest{
		StartAt:             start,
		ExpectedScheduledAt: cur.ScheduledAt,
	}
	if cur.ScheduledEndAt != nil {
		r.ExpectedScheduledEndAt = *cur.ScheduledEndAt
	}
	return r
}

func TestPostgresLifecycleRBAC23E(t *testing.T) {
	db, svc, admin, prac900, _, at := lifeSetup(t)
	start := time.Date(2026, 9, 14, 9, 0, 0, 0, time.UTC)
	appt := bookLife(t, svc, admin, 901, prac900, start, at.ID)
	cur := mustReload(t, db, appt.ID)

	checkinOnly := scopedAccess(902, 10, "queue.checkin")
	mgr := scopedAccess(902, 10, "appointment.reschedule.service", "appointment.cancel.service", "schedule.manage.service")
	noshowRole := scopedAccess(902, 10, "appointment.no_show.service")
	cross := scopedAccess(210, 11, "appointment.reschedule.service", "appointment.cancel.service", "appointment.no_show.service")
	_ = db.Exec(`INSERT INTO users(id, name) VALUES (210,'IDOR') ON CONFLICT DO NOTHING`)

	// A/B/C — queue.checkin alone cannot reschedule / cancel / change practitioner
	req := rsReq(cur, time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC))
	if _, err := svc.RescheduleAppointment(appt.ID, req, checkinOnly); statusOf(err) != 403 {
		t.Fatalf("A checkin reschedule want 403 got %d %v", statusOf(err), err)
	}
	prac901 := uint(901)
	req.PractitionerID = &prac901
	if _, err := svc.RescheduleAppointment(appt.ID, req, checkinOnly); statusOf(err) != 403 {
		t.Fatalf("C checkin practitioner want 403 got %d", statusOf(err))
	}
	if _, err := svc.CancelAppointment(appt.ID, CancelAppointmentRequest{}, checkinOnly); statusOf(err) != 403 {
		t.Fatalf("B checkin cancel want 403 got %d", statusOf(err))
	}

	// D — checkin cannot no-show without appointment.no_show.*
	past := bookLife(t, svc, admin, 902, prac900, time.Date(2026, 9, 14, 8, 0, 0, 0, time.UTC), at.ID)
	_ = db.Model(&Appointment{}).Where("id=?", past.ID).Update("scheduled_at", time.Now().UTC().Add(-time.Hour))
	if _, err := svc.MarkNoShow(past.ID, NoShowAppointmentRequest{}, checkinOnly); statusOf(err) != 403 {
		t.Fatalf("D checkin noshow want 403 got %d", statusOf(err))
	}

	// E — service manager can reschedule/cancel
	req = rsReq(cur, time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC))
	if _, err := svc.RescheduleAppointment(appt.ID, req, mgr); err != nil {
		t.Fatalf("E reschedule: %v", err)
	}
	cur = mustReload(t, db, appt.ID)
	open := bookLife(t, svc, admin, 903, prac900, time.Date(2026, 9, 14, 11, 0, 0, 0, time.UTC), at.ID)
	if _, err := svc.CancelAppointment(open.ID, CancelAppointmentRequest{Reason: "mgr"}, mgr); err != nil {
		t.Fatalf("E cancel: %v", err)
	}

	// F — no-show role can mark no-show
	if _, err := svc.MarkNoShow(past.ID, NoShowAppointmentRequest{}, noshowRole); err != nil {
		t.Fatalf("F noshow: %v", err)
	}

	// G — cross-service denied
	cur = mustReload(t, db, appt.ID)
	req = rsReq(cur, time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC))
	if _, err := svc.RescheduleAppointment(appt.ID, req, cross); statusOf(err) != 404 {
		t.Fatalf("G cross want 404 got %d", statusOf(err))
	}
}

func TestPostgresLifecycleReschedule23E(t *testing.T) {
	db, svc, admin, prac900, prac901, at := lifeSetup(t)
	start := time.Date(2026, 9, 14, 9, 0, 0, 0, time.UTC)
	appt := bookLife(t, svc, admin, 901, prac900, start, at.ID)
	origID := appt.ID
	cur := mustReload(t, db, appt.ID)

	newStart := time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)
	moved, err := svc.RescheduleAppointment(appt.ID, rsReq(cur, newStart), admin)
	if err != nil {
		t.Fatalf("A: %v", err)
	}
	if moved.ID != origID {
		t.Fatal("B ID must be preserved")
	}
	if moved.ScheduledEndAt == nil || !moved.ScheduledEndAt.Equal(newStart.Add(30*time.Minute)) {
		t.Fatalf("E end=%v", moved.ScheduledEndAt)
	}

	dur := 30
	from := time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)
	pid := prac900
	res, err := svc.ComputeAvailability(AvailabilityQuery{
		ServiceID: 10, PractitionerID: &pid, DurationMinutes: &dur, From: from, To: from.Add(24 * time.Hour),
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	oldFree, newBlocked := false, true
	for _, sl := range res.Slots {
		if sl.StartAt.Equal(start) {
			oldFree = true
		}
		if sl.StartAt.Equal(newStart) {
			newBlocked = false
		}
	}
	if !oldFree || !newBlocked {
		t.Fatalf("C/D oldFree=%v newBlocked=%v", oldFree, newBlocked)
	}

	at45, err := svc.CreateAppointmentType(CreateAppointmentTypeRequest{
		Code: "LIFE-45", Name: "Life 45", DefaultDurationMinutes: 45,
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	cur = mustReload(t, db, appt.ID)
	req := rsReq(cur, time.Date(2026, 9, 14, 11, 0, 0, 0, time.UTC))
	req.AppointmentTypeID = &at45.ID
	moved2, err := svc.RescheduleAppointment(appt.ID, req, admin)
	if err != nil {
		t.Fatalf("G: %v", err)
	}
	if !moved2.ScheduledEndAt.Equal(time.Date(2026, 9, 14, 11, 45, 0, 0, time.UTC)) {
		t.Fatalf("G end=%v", moved2.ScheduledEndAt)
	}

	cur = mustReload(t, db, appt.ID)
	d10 := 10
	req = rsReq(cur, time.Date(2026, 9, 14, 13, 0, 0, 0, time.UTC))
	req.DurationMinutes = &d10
	req.AppointmentTypeID = &at45.ID
	if _, err = svc.RescheduleAppointment(appt.ID, req, admin); statusOf(err) != 400 {
		t.Fatalf("H want 400 got %d", statusOf(err))
	}

	cur = mustReload(t, db, appt.ID)
	if _, err = svc.RescheduleAppointment(appt.ID, rsReq(cur, time.Date(2026, 9, 14, 17, 0, 0, 0, time.UTC)), admin); statusOf(err) != 409 {
		t.Fatalf("I outside want 409 got %d", statusOf(err))
	}

	_, err = svc.CreateScheduleException(CreateScheduleExceptionRequest{
		PractitionerID: prac900, ServiceID: 10, Type: ExMeeting,
		StartAt: time.Date(2026, 9, 14, 14, 0, 0, 0, time.UTC),
		EndAt:   time.Date(2026, 9, 14, 15, 0, 0, 0, time.UTC),
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	cur = mustReload(t, db, appt.ID)
	if _, err = svc.RescheduleAppointment(appt.ID, rsReq(cur, time.Date(2026, 9, 14, 14, 0, 0, 0, time.UTC)), admin); statusOf(err) != 409 {
		t.Fatalf("J meeting want 409 got %d", statusOf(err))
	}

	sat := time.Date(2026, 9, 19, 9, 0, 0, 0, time.UTC)
	_, err = svc.CreateScheduleException(CreateScheduleExceptionRequest{
		PractitionerID: prac900, ServiceID: 10, Type: ExExtraAvailability,
		StartAt: sat, EndAt: sat.Add(3 * time.Hour),
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	cur = mustReload(t, db, appt.ID)
	if _, err = svc.RescheduleAppointment(appt.ID, rsReq(cur, sat), admin); err != nil {
		t.Fatalf("K EXTRA: %v", err)
	}

	mon := time.Date(2026, 9, 14, 8, 0, 0, 0, time.UTC)
	cur = mustReload(t, db, appt.ID)
	if _, err = svc.RescheduleAppointment(appt.ID, rsReq(cur, mon), admin); err != nil {
		t.Fatal(err)
	}

	_ = bookLife(t, svc, admin, 902, prac900, time.Date(2026, 9, 14, 9, 30, 0, 0, time.UTC), at.ID)
	cur = mustReload(t, db, appt.ID)
	if _, err = svc.RescheduleAppointment(appt.ID, rsReq(cur, time.Date(2026, 9, 14, 9, 30, 0, 0, time.UTC)), admin); statusOf(err) != 409 {
		t.Fatalf("L overlap want 409 got %d", statusOf(err))
	}

	_ = bookLife(t, svc, admin, 901, prac901, time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC), at.ID)
	cur = mustReload(t, db, appt.ID)
	req = rsReq(cur, time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC))
	req.PractitionerID = &prac901
	if _, err = svc.RescheduleAppointment(appt.ID, req, admin); statusOf(err) != 409 {
		t.Fatalf("M patient overlap want 409 got %d", statusOf(err))
	}

	adj := time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)
	cur = mustReload(t, db, appt.ID)
	if _, err = svc.RescheduleAppointment(appt.ID, rsReq(cur, adj), admin); err != nil {
		t.Fatalf("N adjacent: %v", err)
	}
	cur = mustReload(t, db, appt.ID)
	req = rsReq(cur, adj)
	req.Reason = "noop"
	if _, err = svc.RescheduleAppointment(appt.ID, req, admin); err != nil {
		t.Fatalf("O self: %v", err)
	}

	target := time.Date(2026, 9, 14, 15, 0, 0, 0, time.UTC)
	cur = mustReload(t, db, appt.ID)
	req = rsReq(cur, target)
	req.PractitionerID = &prac901
	movedP, err := svc.RescheduleAppointment(appt.ID, req, admin)
	if err != nil || movedP.ExpectedDoctorID == nil || *movedP.ExpectedDoctorID != prac901 {
		t.Fatalf("P: %v %+v", err, movedP)
	}

	_ = db.Exec(`INSERT INTO users(id, name) VALUES (910,'Dr11') ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO staff_profiles(id, user_id, active, primary_service_id) VALUES (210,910,true,11) ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO staff_service_assignments(profile_id, service_id, active) VALUES (210,11,true) ON CONFLICT DO NOTHING`)
	bad := uint(910)
	cur = mustReload(t, db, appt.ID)
	req = rsReq(cur, target)
	req.PractitionerID = &bad
	if _, err = svc.RescheduleAppointment(appt.ID, req, admin); statusOf(err) != 400 && statusOf(err) != 403 && statusOf(err) != 409 {
		t.Fatalf("Q want 4xx got %d %v", statusOf(err), err)
	}

	_ = db.Model(&AppointmentType{}).Where("id=?", at45.ID).Update("active", false)
	cur = mustReload(t, db, appt.ID)
	req = rsReq(cur, time.Date(2026, 9, 14, 8, 30, 0, 0, time.UTC))
	req.AppointmentTypeID = &at45.ID
	req.PractitionerID = &prac900
	if _, err = svc.RescheduleAppointment(appt.ID, req, admin); statusOf(err) != 400 {
		t.Fatalf("R inactive want 400 got %d", statusOf(err))
	}
	_ = db.Model(&AppointmentType{}).Where("id=?", at45.ID).Update("active", true)

	sid11 := uint(11)
	atBad, _ := svc.CreateAppointmentType(CreateAppointmentTypeRequest{
		Code: "LIFE-11", Name: "Svc11", DefaultDurationMinutes: 30, ServiceID: &sid11,
	}, admin)
	cur = mustReload(t, db, appt.ID)
	req = rsReq(cur, time.Date(2026, 9, 14, 8, 30, 0, 0, time.UTC))
	req.AppointmentTypeID = &atBad.ID
	req.PractitionerID = &prac900
	if _, err = svc.RescheduleAppointment(appt.ID, req, admin); statusOf(err) != 400 {
		t.Fatalf("S type scope want 400 got %d", statusOf(err))
	}

	var hist AppointmentHistory
	_ = db.Where("appointment_id=? AND event_type=?", appt.ID, ApptHistRescheduled).Order("id DESC").First(&hist)
	if hist.ActorUserID != 902 {
		t.Fatalf("T actor=%d", hist.ActorUserID)
	}
	var payload lifecycleHistoryPayload
	if err := json.Unmarshal([]byte(hist.Payload), &payload); err != nil || payload.Old == nil || payload.New == nil {
		t.Fatalf("U payload: %v %s", err, hist.Payload)
	}

	beforeAt := mustReload(t, db, appt.ID).ScheduledAt
	beforePrac := *mustReload(t, db, appt.ID).ExpectedDoctorID
	cur = mustReload(t, db, appt.ID)
	req = rsReq(cur, time.Date(2026, 9, 14, 18, 0, 0, 0, time.UTC))
	req.PractitionerID = &prac900
	_, _ = svc.RescheduleAppointment(appt.ID, req, admin)
	after := mustReload(t, db, appt.ID)
	if !after.ScheduledAt.Equal(beforeAt) || after.ExpectedDoctorID == nil || *after.ExpectedDoctorID != beforePrac {
		t.Fatalf("V rollback mutated: %+v", after)
	}
}

func TestPostgresLifecycleCancelNoShow23E(t *testing.T) {
	db, svc, admin, prac900, _, at := lifeSetup(t)
	start := time.Date(2026, 9, 14, 9, 0, 0, 0, time.UTC)
	appt := bookLife(t, svc, admin, 901, prac900, start, at.ID)
	id := appt.ID

	cancelled, err := svc.CancelAppointment(id, CancelAppointmentRequest{Reason: "Patient unavailable"}, admin)
	if err != nil || cancelled.Status != ApptCancelled {
		t.Fatalf("cancel: %v %+v", err, cancelled)
	}
	var nBefore int64
	db.Model(&AppointmentHistory{}).Where("appointment_id=? AND event_type=?", id, ApptHistCancelled).Count(&nBefore)
	if _, err = svc.CancelAppointment(id, CancelAppointmentRequest{Reason: "again"}, admin); err != nil {
		t.Fatal(err)
	}
	var nAfter int64
	db.Model(&AppointmentHistory{}).Where("appointment_id=? AND event_type=?", id, ApptHistCancelled).Count(&nAfter)
	if nAfter != nBefore {
		t.Fatalf("terminal cancel without key must not duplicate history %d→%d", nBefore, nAfter)
	}

	past := bookLife(t, svc, admin, 902, prac900, time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC), at.ID)
	_ = db.Model(&Appointment{}).Where("id=?", past.ID).Update("scheduled_at", time.Now().UTC().Add(-2*time.Hour))
	ns, err := svc.MarkNoShow(past.ID, NoShowAppointmentRequest{Reason: "absent"}, admin)
	if err != nil || ns.Status != ApptNoShow {
		t.Fatalf("noshow: %v", err)
	}
	db.Model(&AppointmentHistory{}).Where("appointment_id=? AND event_type=?", past.ID, ApptHistNoShow).Count(&nBefore)
	if _, err = svc.MarkNoShow(past.ID, NoShowAppointmentRequest{}, admin); err != nil {
		t.Fatal(err)
	}
	db.Model(&AppointmentHistory{}).Where("appointment_id=? AND event_type=?", past.ID, ApptHistNoShow).Count(&nAfter)
	if nAfter != nBefore {
		t.Fatalf("terminal noshow without key must not duplicate history")
	}

	fut := bookLife(t, svc, admin, 903, prac900, time.Date(2026, 9, 14, 11, 0, 0, 0, time.UTC), at.ID)
	_ = db.Model(&Appointment{}).Where("id=?", fut.ID).Update("scheduled_at", time.Now().UTC().Add(2*time.Hour))
	if _, err := svc.MarkNoShow(fut.ID, NoShowAppointmentRequest{}, admin); statusOf(err) != 400 {
		t.Fatalf("future noshow want 400 got %d", statusOf(err))
	}
}

func TestPostgresLifecycleIdempotencyExact23E(t *testing.T) {
	db, svc, admin, prac900, prac901, at := lifeSetup(t)
	at45, err := svc.CreateAppointmentType(CreateAppointmentTypeRequest{
		Code: "IDEM-45", Name: "Idem45", DefaultDurationMinutes: 45,
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 9, 14, 9, 0, 0, 0, time.UTC)
	appt := bookLife(t, svc, admin, 901, prac900, start, at.ID)
	cur := mustReload(t, db, appt.ID)
	target := time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)
	req := rsReq(cur, target)
	req.IdempotencyKey = "abc"
	req.Reason = "r1"
	if _, err := svc.RescheduleAppointment(appt.ID, req, admin); err != nil {
		t.Fatal(err)
	}
	cur = mustReload(t, db, appt.ID)

	// abc vs abc2 — distinct keys, second is fresh reschedule needing current expected
	req2 := rsReq(cur, time.Date(2026, 9, 14, 11, 0, 0, 0, time.UTC))
	req2.IdempotencyKey = "abc2"
	req2.Reason = "r1"
	if _, err := svc.RescheduleAppointment(appt.ID, req2, admin); err != nil {
		t.Fatalf("abc2 must not collide with abc: %v", err)
	}
	cur = mustReload(t, db, appt.ID)

	// same key different reason → 409
	req3 := rsReq(cur, time.Date(2026, 9, 14, 11, 0, 0, 0, time.UTC))
	req3.IdempotencyKey = "abc2"
	req3.Reason = "other"
	if _, err := svc.RescheduleAppointment(appt.ID, req3, admin); statusOf(err) != 409 {
		t.Fatalf("different reason want 409 got %d", statusOf(err))
	}

	// same key + same semantics retry OK
	req3.Reason = "r1"
	if _, err := svc.RescheduleAppointment(appt.ID, req3, admin); err != nil {
		t.Fatalf("identical retry: %v", err)
	}

	// same key different type same duration → 409 (use new appointment)
	apptB := bookLife(t, svc, admin, 902, prac900, time.Date(2026, 9, 14, 8, 0, 0, 0, time.UTC), at.ID)
	curB := mustReload(t, db, apptB.ID)
	rb := rsReq(curB, time.Date(2026, 9, 14, 8, 30, 0, 0, time.UTC))
	rb.IdempotencyKey = "typekey"
	rb.AppointmentTypeID = &at.ID
	if _, err := svc.RescheduleAppointment(apptB.ID, rb, admin); err != nil {
		t.Fatal(err)
	}
	curB = mustReload(t, db, apptB.ID)
	// force type to stay 30min but request different type id with matching duration via explicit minutes
	// at45 is 45 — different duration. Create twin 30-min type:
	at30b, err := svc.CreateAppointmentType(CreateAppointmentTypeRequest{
		Code: "IDEM-30B", Name: "Idem30B", DefaultDurationMinutes: 30,
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	rb = rsReq(curB, curB.ScheduledAt)
	rb.IdempotencyKey = "typekey"
	rb.AppointmentTypeID = &at30b.ID
	if _, err := svc.RescheduleAppointment(apptB.ID, rb, admin); statusOf(err) != 409 {
		t.Fatalf("different type same duration want 409 got %d %v", statusOf(err), err)
	}

	// different practitioner
	rb = rsReq(curB, curB.ScheduledAt)
	rb.IdempotencyKey = "typekey"
	rb.AppointmentTypeID = &at.ID
	rb.PractitionerID = &prac901
	if _, err := svc.RescheduleAppointment(apptB.ID, rb, admin); statusOf(err) != 409 {
		t.Fatalf("different prac want 409 got %d", statusOf(err))
	}

	// different time
	rb = rsReq(curB, time.Date(2026, 9, 14, 13, 0, 0, 0, time.UTC))
	rb.IdempotencyKey = "typekey"
	rb.AppointmentTypeID = &at.ID
	if _, err := svc.RescheduleAppointment(apptB.ID, rb, admin); statusOf(err) != 409 {
		t.Fatalf("different time want 409 got %d", statusOf(err))
	}

	// malformed unrelated payload must not match key
	_ = db.Exec(`INSERT INTO patient_queue_appointment_history
		(appointment_id, actor_user_id, event_type, from_status, to_status, reason, payload, created_at)
		VALUES (?, ?, ?, '', '', '', 'not-json {{{', NOW())`, apptB.ID, 902, ApptHistRescheduled)
	rb = rsReq(curB, curB.ScheduledAt)
	rb.IdempotencyKey = "typekey"
	rb.AppointmentTypeID = &at.ID
	rb.Reason = "" // prior reason was empty on first? first had no reason set — empty
	if _, err := svc.RescheduleAppointment(apptB.ID, rb, admin); err != nil {
		t.Fatalf("malformed payload must be skipped: %v", err)
	}

	_ = at45
}

func TestPostgresLifecycleConcurrency23E(t *testing.T) {
	db, svc, admin, prac900, prac901, at := lifeSetup(t)

	// C1 — concurrent reschedules from same expected original → 1 success + 1 stale 409 + 1 history
	appt := bookLife(t, svc, admin, 901, prac900, time.Date(2026, 9, 14, 9, 0, 0, 0, time.UTC), at.ID)
	cur := mustReload(t, db, appt.ID)
	r1 := rsReq(cur, time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC))
	r2 := rsReq(cur, time.Date(2026, 9, 14, 11, 0, 0, 0, time.UTC))
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _, e := svc.RescheduleAppointment(appt.ID, r1, admin); errs <- e }()
	go func() { defer wg.Done(); _, e := svc.RescheduleAppointment(appt.ID, r2, admin); errs <- e }()
	wg.Wait()
	close(errs)
	ok, stale := 0, 0
	for e := range errs {
		if e == nil {
			ok++
		} else if statusOf(e) == 409 {
			stale++
		} else {
			t.Fatalf("C1 unexpected: %v", e)
		}
	}
	if ok != 1 || stale != 1 {
		t.Fatalf("C1 want 1 ok + 1 stale got ok=%d stale=%d", ok, stale)
	}
	var histN int64
	db.Model(&AppointmentHistory{}).Where("appointment_id=? AND event_type=?", appt.ID, ApptHistRescheduled).Count(&histN)
	if histN != 1 {
		t.Fatalf("C1 want 1 RESCHEDULED history got %d", histN)
	}

	// C2 — fresh second reschedule after reading updated state succeeds
	cur = mustReload(t, db, appt.ID)
	if _, err := svc.RescheduleAppointment(appt.ID, rsReq(cur, time.Date(2026, 9, 14, 11, 0, 0, 0, time.UTC)), admin); err != nil {
		t.Fatalf("C2: %v", err)
	}

	// C3 reschedule vs booking practitioner
	a2 := bookLife(t, svc, admin, 902, prac900, time.Date(2026, 9, 14, 8, 0, 0, 0, time.UTC), at.ID)
	cur2 := mustReload(t, db, a2.ID)
	slot := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	errs2 := make(chan error, 2)
	var wg2 sync.WaitGroup
	wg2.Add(2)
	go func() {
		defer wg2.Done()
		_, e := svc.RescheduleAppointment(a2.ID, rsReq(cur2, slot), admin)
		errs2 <- e
	}()
	go func() {
		defer wg2.Done()
		_, _, e := svc.BookAppointment(BookAppointmentRequest{
			PatientID: 903, ServiceID: 10, PractitionerID: &prac900,
			AppointmentTypeID: &at.ID, StartAt: slot,
		}, admin)
		errs2 <- e
	}()
	wg2.Wait()
	close(errs2)
	ok, fail := 0, 0
	for e := range errs2 {
		if e == nil {
			ok++
		} else {
			fail++
		}
	}
	if ok != 1 || fail != 1 {
		t.Fatalf("C3 want 1/1 got ok=%d fail=%d", ok, fail)
	}

	// C4 patient concurrency
	_ = db.Exec(`INSERT INTO patients(id, code_patient, nom, prenoms) VALUES (910,'PLF10','P','10') ON CONFLICT DO NOTHING`)
	a4 := bookLife(t, svc, admin, 910, prac900, time.Date(2026, 9, 14, 14, 0, 0, 0, time.UTC), at.ID)
	cur4 := mustReload(t, db, a4.ID)
	slot4 := time.Date(2026, 9, 14, 15, 0, 0, 0, time.UTC)
	errs4 := make(chan error, 2)
	var wg4 sync.WaitGroup
	wg4.Add(2)
	go func() {
		defer wg4.Done()
		_, e := svc.RescheduleAppointment(a4.ID, rsReq(cur4, slot4), admin)
		errs4 <- e
	}()
	go func() {
		defer wg4.Done()
		_, _, e := svc.BookAppointment(BookAppointmentRequest{
			PatientID: 910, ServiceID: 10, PractitionerID: &prac901,
			AppointmentTypeID: &at.ID, StartAt: slot4,
		}, admin)
		errs4 <- e
	}()
	wg4.Wait()
	close(errs4)
	ok, fail = 0, 0
	for e := range errs4 {
		if e == nil {
			ok++
		} else {
			fail++
		}
	}
	if ok != 1 || fail != 1 {
		t.Fatalf("C4 want 1/1 got ok=%d fail=%d", ok, fail)
	}

	// C5 practitioner-change vs booking
	a3 := bookLife(t, svc, admin, 904, prac900, time.Date(2026, 9, 14, 8, 30, 0, 0, time.UTC), at.ID)
	cur3 := mustReload(t, db, a3.ID)
	slot3 := time.Date(2026, 9, 14, 13, 0, 0, 0, time.UTC)
	req3 := rsReq(cur3, slot3)
	req3.PractitionerID = &prac901
	errs3 := make(chan error, 2)
	var wg3 sync.WaitGroup
	wg3.Add(2)
	go func() { defer wg3.Done(); _, e := svc.RescheduleAppointment(a3.ID, req3, admin); errs3 <- e }()
	go func() {
		defer wg3.Done()
		_, _, e := svc.BookAppointment(BookAppointmentRequest{
			PatientID: 905, ServiceID: 10, PractitionerID: &prac901,
			AppointmentTypeID: &at.ID, StartAt: slot3,
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
		t.Fatalf("C5 want 1/1 got ok=%d fail=%d", ok, fail)
	}

	// C6 cancel vs booking
	_ = db.Exec(`INSERT INTO patients(id, code_patient, nom, prenoms) VALUES (911,'PLF11','P','11'),(912,'PLF12','P','12') ON CONFLICT DO NOTHING`)
	slot5 := time.Date(2026, 9, 14, 10, 30, 0, 0, time.UTC)
	a5b, _, err := svc.BookAppointment(BookAppointmentRequest{
		PatientID: 911, ServiceID: 10, PractitionerID: &prac900,
		AppointmentTypeID: &at.ID, StartAt: slot5,
	}, admin)
	if err != nil {
		slot5 = time.Date(2026, 9, 14, 11, 30, 0, 0, time.UTC)
		a5b, _, err = svc.BookAppointment(BookAppointmentRequest{
			PatientID: 911, ServiceID: 10, PractitionerID: &prac900,
			AppointmentTypeID: &at.ID, StartAt: slot5,
		}, admin)
		if err != nil {
			t.Fatalf("C6 setup: %v", err)
		}
	}
	errs5 := make(chan error, 2)
	var wg5 sync.WaitGroup
	wg5.Add(2)
	go func() {
		defer wg5.Done()
		_, e := svc.CancelAppointment(a5b.ID, CancelAppointmentRequest{Reason: "c5"}, admin)
		errs5 <- e
	}()
	go func() {
		defer wg5.Done()
		_, _, e := svc.BookAppointment(BookAppointmentRequest{
			PatientID: 912, ServiceID: 10, PractitionerID: &prac900,
			AppointmentTypeID: &at.ID, StartAt: slot5,
		}, admin)
		errs5 <- e
	}()
	wg5.Wait()
	close(errs5)
	var blockers int64
	db.Model(&Appointment{}).Where("expected_doctor_id=? AND scheduled_at=? AND status IN ?", prac900, slot5,
		[]string{ApptScheduled, ApptArrived, ApptCheckedIn, ApptInProgress, ApptCompleted}).Count(&blockers)
	if blockers > 1 {
		t.Fatalf("C6 double book blockers=%d", blockers)
	}

	// C7 concurrent identical idempotent cancel — one history
	a6, _, err := svc.BookAppointment(BookAppointmentRequest{
		PatientID: 912, ServiceID: 10, PractitionerID: &prac901,
		AppointmentTypeID: &at.ID, StartAt: time.Date(2026, 9, 14, 8, 0, 0, 0, time.UTC),
	}, admin)
	if err != nil {
		t.Fatalf("C7 setup: %v", err)
	}
	errs6 := make(chan error, 2)
	var wg6 sync.WaitGroup
	wg6.Add(2)
	go func() {
		defer wg6.Done()
		_, e := svc.CancelAppointment(a6.ID, CancelAppointmentRequest{Reason: "c6", IdempotencyKey: "cancel-c6"}, admin)
		errs6 <- e
	}()
	go func() {
		defer wg6.Done()
		_, e := svc.CancelAppointment(a6.ID, CancelAppointmentRequest{Reason: "c6", IdempotencyKey: "cancel-c6"}, admin)
		errs6 <- e
	}()
	wg6.Wait()
	close(errs6)
	for e := range errs6 {
		if e != nil {
			t.Fatalf("C7: %v", e)
		}
	}
	var cancelHist int64
	db.Model(&AppointmentHistory{}).Where("appointment_id=? AND event_type=?", a6.ID, ApptHistCancelled).Count(&cancelHist)
	if cancelHist != 1 {
		t.Fatalf("C7 want 1 history got %d", cancelHist)
	}
}
