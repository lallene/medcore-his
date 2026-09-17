package patient_queue

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/core/scheduling"
)

func seedSeriesWideSchedule(t *testing.T, svc *Service, admin Access, prac uint) {
	t.Helper()
	vf := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for wd := 0; wd <= 6; wd++ {
		_, err := svc.CreateWorkingSchedule(CreateWorkingScheduleRequest{
			PractitionerID: prac, ServiceID: 10, Weekday: wd,
			StartTime: "07:00", EndTime: "18:00", ValidFrom: vf,
		}, admin)
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestPostgresAppointmentSeriesAtomicCreate23OA(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	_ = scheduling.SetLocation("UTC")

	_ = db.Exec(`INSERT INTO users(id, name) VALUES (910,'DrSeries'),(911,'SeriesActor') ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO staff_profiles(id, user_id, active, primary_service_id) VALUES
		(910,910,true,10),(911,911,true,10) ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO staff_service_assignments(profile_id, service_id, active) VALUES
		(910,10,true),(911,10,true) ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO patients(id, code_patient, nom, prenoms) VALUES
		(910,'PS1','Series','One'),(911,'PS2','Series','Two') ON CONFLICT DO NOTHING`)

	admin := adminAccess(911)
	prac := uint(910)
	seedSeriesWideSchedule(t, svc, admin, prac)
	at, err := svc.CreateAppointmentType(CreateAppointmentTypeRequest{
		Code: "SER-30", Name: "Series 30", DefaultDurationMinutes: 30,
	}, admin)
	if err != nil {
		t.Fatal(err)
	}

	anchor := time.Date(2026, 12, 7, 9, 0, 0, 0, time.UTC) // Monday far enough for T24H reminders
	count := 3
	dto, reused, err := svc.CreateAppointmentSeries(CreateAppointmentSeriesRequest{
		PatientID: 910, ServiceID: 10, PractitionerID: prac,
		AppointmentTypeID: &at.ID, Freq: SeriesFreqWeekly, IntervalWeeks: 1,
		ByWeekdays: []int{int(time.Monday)}, Count: &count,
		Timezone: "UTC", AnchorStartAt: anchor, IdempotencyKey: "ser-ok-1",
	}, admin)
	if err != nil || reused {
		t.Fatalf("create: %v reused=%v", err, reused)
	}
	if len(dto.Occurrences) != 3 {
		t.Fatalf("want 3 occ got %d", len(dto.Occurrences))
	}
	var nSeries, nAppt int64
	db.Model(&AppointmentSeries{}).Count(&nSeries)
	db.Model(&Appointment{}).Where("series_id = ?", dto.ID).Count(&nAppt)
	if nSeries != 1 || nAppt != 3 {
		t.Fatalf("series=%d appts=%d", nSeries, nAppt)
	}
	var hist int64
	db.Model(&AppointmentHistory{}).Where("event_type = ?", ApptHistCreated).Count(&hist)
	if hist != 3 {
		t.Fatalf("history want 3 got %d", hist)
	}
	var booked int64
	db.Model(&AppointmentNotificationIntent{}).Where("kind = ?", NotifKindBooked).Count(&booked)
	if booked != 3 {
		t.Fatalf("BOOKED intents want 3 got %d", booked)
	}
	// All starts > now+24h → REMINDER_T24H each
	var rem int64
	db.Model(&AppointmentNotificationIntent{}).Where("kind = ?", NotifKindReminderT24H).Count(&rem)
	if rem != 3 {
		t.Fatalf("REMINDER intents want 3 got %d", rem)
	}
	keys := map[string]struct{}{}
	var intents []AppointmentNotificationIntent
	db.Find(&intents)
	for _, in := range intents {
		k := fmt.Sprintf("%d|%s|%s", in.AppointmentID, in.Kind, in.OccurrenceKey)
		if _, ok := keys[k]; ok {
			t.Fatalf("duplicate notif key %s", k)
		}
		keys[k] = struct{}{}
	}

	// Collision at occurrence k → full rollback
	blockStart := time.Date(2026, 12, 14, 15, 0, 0, 0, time.UTC) // 2nd weekly Monday @ 15:00
	_, _, err = svc.BookAppointment(BookAppointmentRequest{
		PatientID: 911, ServiceID: 10, PractitionerID: &prac,
		AppointmentTypeID: &at.ID, StartAt: blockStart, IdempotencyKey: "block-slot",
	}, admin)
	if err != nil {
		t.Fatalf("blocker book: %v", err)
	}
	count2 := 3
	_, _, err = svc.CreateAppointmentSeries(CreateAppointmentSeriesRequest{
		PatientID: 910, ServiceID: 10, PractitionerID: prac,
		AppointmentTypeID: &at.ID, Freq: SeriesFreqWeekly, IntervalWeeks: 1,
		ByWeekdays: []int{int(time.Monday)}, Count: &count2,
		Timezone: "UTC", AnchorStartAt: time.Date(2026, 12, 7, 15, 0, 0, 0, time.UTC),
		IdempotencyKey: "ser-fail-collision",
	}, admin)
	if statusOf(err) != 409 {
		t.Fatalf("collision want 409 got %d %v", statusOf(err), err)
	}
	var orphanSeries int64
	db.Model(&AppointmentSeries{}).Where("idempotency_key = ?", "ser-fail-collision").Count(&orphanSeries)
	if orphanSeries != 0 {
		t.Fatal("partial series must not persist")
	}
	var orphanAppt int64
	db.Model(&Appointment{}).Where("patient_id = ? AND scheduled_at = ?", 910, time.Date(2026, 12, 7, 15, 0, 0, 0, time.UTC)).Count(&orphanAppt)
	if orphanAppt != 0 {
		t.Fatal("partial occurrences must not persist")
	}
}

func TestPostgresAppointmentSeriesIdempotency23OA(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	_ = scheduling.SetLocation("UTC")

	_ = db.Exec(`INSERT INTO users(id, name) VALUES (920,'DrSerId'),(921,'ActorSerId') ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO staff_profiles(id, user_id, active, primary_service_id) VALUES
		(920,920,true,10),(921,921,true,10) ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO staff_service_assignments(profile_id, service_id, active) VALUES
		(920,10,true),(921,10,true) ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO patients(id, code_patient, nom, prenoms) VALUES (920,'PSI','Id','Ser') ON CONFLICT DO NOTHING`)

	admin := adminAccess(921)
	prac := uint(920)
	seedSeriesWideSchedule(t, svc, admin, prac)
	at, err := svc.CreateAppointmentType(CreateAppointmentTypeRequest{
		Code: "SER-ID", Name: "Series Id", DefaultDurationMinutes: 30,
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	anchor := time.Date(2026, 9, 15, 9, 0, 0, 0, time.UTC) // Tuesday
	count := 2
	req := CreateAppointmentSeriesRequest{
		PatientID: 920, ServiceID: 10, PractitionerID: prac,
		AppointmentTypeID: &at.ID, Freq: SeriesFreqWeekly, IntervalWeeks: 1,
		ByWeekdays: []int{int(time.Tuesday)}, Count: &count,
		Timezone: "UTC", AnchorStartAt: anchor, IdempotencyKey: "ser-idem-1",
	}
	first, reused, err := svc.CreateAppointmentSeries(req, admin)
	if err != nil || reused {
		t.Fatalf("first: %v reused=%v", err, reused)
	}
	second, reused, err := svc.CreateAppointmentSeries(req, admin)
	if err != nil || !reused || second.ID != first.ID {
		t.Fatalf("replay: %v reused=%v id %d vs %d", err, reused, second.ID, first.ID)
	}
	var n int64
	db.Model(&AppointmentSeries{}).Count(&n)
	if n != 1 {
		t.Fatalf("want 1 series got %d", n)
	}

	req2 := req
	req2.IntervalWeeks = 2
	_, _, err = svc.CreateAppointmentSeries(req2, admin)
	if statusOf(err) != 409 {
		t.Fatalf("semantic conflict want 409 got %d %v", statusOf(err), err)
	}

	// Concurrent duplicates → exactly one series
	var okCount int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := req
			r.IdempotencyKey = "ser-concurrent"
			r.AnchorStartAt = time.Date(2026, 9, 16, 9, 0, 0, 0, time.UTC) // Wed
			r.ByWeekdays = []int{int(time.Wednesday)}
			_, _, e := svc.CreateAppointmentSeries(r, admin)
			if e == nil {
				atomic.AddInt32(&okCount, 1)
			}
		}(i)
	}
	wg.Wait()
	if okCount < 1 {
		t.Fatal("concurrent: no success")
	}
	var nConc int64
	db.Model(&AppointmentSeries{}).Where("idempotency_key = ?", "ser-concurrent").Count(&nConc)
	if nConc != 1 {
		t.Fatalf("concurrent want 1 series got %d (ok=%d)", nConc, okCount)
	}
}

func TestPostgresAppointmentSeriesRBAC23OA(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	_ = scheduling.SetLocation("UTC")

	_ = db.Exec(`INSERT INTO users(id, name) VALUES (930,'DrRBAC'),(931,'ActorIn'),(932,'ActorOut'),(933,'PracOwn') ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO staff_profiles(id, user_id, active, primary_service_id) VALUES
		(930,930,true,10),(931,931,true,10),(932,932,true,11),(933,933,true,10) ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO staff_service_assignments(profile_id, service_id, active) VALUES
		(930,10,true),(931,10,true),(932,11,true),(933,10,true) ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO organization_services(id, name, code, active) VALUES (11,'Other','OTH',true) ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO patients(id, code_patient, nom, prenoms) VALUES (930,'PR','Rbac','Ser') ON CONFLICT DO NOTHING`)

	admin := adminAccess(931)
	prac := uint(930)
	seedSeriesWideSchedule(t, svc, admin, prac)
	at, err := svc.CreateAppointmentType(CreateAppointmentTypeRequest{
		Code: "SER-RBAC", Name: "Series RBAC", DefaultDurationMinutes: 30,
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	anchor := time.Date(2026, 9, 14, 11, 0, 0, 0, time.UTC)
	count := 1
	base := CreateAppointmentSeriesRequest{
		PatientID: 930, ServiceID: 10, PractitionerID: prac,
		AppointmentTypeID: &at.ID, Freq: SeriesFreqWeekly, IntervalWeeks: 1,
		ByWeekdays: []int{int(time.Monday)}, Count: &count,
		Timezone: "UTC", AnchorStartAt: anchor,
	}

	inScope := Access{UserID: 931, ServiceID: ptrU(10), Permissions: map[string]bool{"appointment.create.service": true}}
	dto, _, err := svc.CreateAppointmentSeries(base, inScope)
	if err != nil {
		t.Fatalf("in-scope create: %v", err)
	}

	outScope := Access{UserID: 932, ServiceID: ptrU(11), Permissions: map[string]bool{"appointment.create.service": true}}
	_, _, err = svc.CreateAppointmentSeries(base, outScope)
	if statusOf(err) != 403 {
		t.Fatalf("out-of-scope create want 403 got %d", statusOf(err))
	}

	createAll := Access{UserID: 931, Permissions: map[string]bool{"appointment.create.all": true}}
	_, _, err = svc.CreateAppointmentSeries(CreateAppointmentSeriesRequest{
		PatientID: 930, ServiceID: 10, PractitionerID: prac,
		AppointmentTypeID: &at.ID, Freq: SeriesFreqWeekly, IntervalWeeks: 1,
		ByWeekdays: []int{int(time.Monday)}, Count: &count,
		Timezone: "UTC", AnchorStartAt: time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC),
	}, createAll)
	if err != nil {
		t.Fatalf("create.all: %v", err)
	}

	manage := Access{UserID: 931, Permissions: map[string]bool{"schedule.manage.service": true}}
	_, _, err = svc.CreateAppointmentSeries(CreateAppointmentSeriesRequest{
		PatientID: 930, ServiceID: 10, PractitionerID: prac,
		AppointmentTypeID: &at.ID, Freq: SeriesFreqWeekly, IntervalWeeks: 1,
		ByWeekdays: []int{int(time.Monday)}, Count: &count,
		Timezone: "UTC", AnchorStartAt: time.Date(2026, 9, 14, 13, 0, 0, 0, time.UTC),
	}, manage)
	if err != nil {
		t.Fatalf("manage.service: %v", err)
	}

	readOnly := Access{UserID: 931, Permissions: map[string]bool{"schedule.read.service": true}}
	if svc.canBookAppointments(readOnly) {
		t.Fatal("schedule.read must not book")
	}
	_, _, err = svc.CreateAppointmentSeries(base, readOnly)
	if statusOf(err) != 403 {
		t.Fatalf("read-only POST want 403 got %d", statusOf(err))
	}

	queueOnly := Access{UserID: 931, Permissions: map[string]bool{"queue.checkin": true, "queue.reception.read": true}}
	_, _, err = svc.CreateAppointmentSeries(base, queueOnly)
	if statusOf(err) != 403 {
		t.Fatalf("queue-only POST want 403 got %d", statusOf(err))
	}

	// GET isolation
	allRead := Access{UserID: 931, Permissions: map[string]bool{"schedule.read.all": true}}
	got, err := svc.GetAppointmentSeries(dto.ID, allRead)
	if err != nil || got.ID != dto.ID {
		t.Fatalf("read.all get: %v", err)
	}
	svcRead := Access{UserID: 931, Permissions: map[string]bool{"schedule.read.service": true}}
	if _, err := svc.GetAppointmentSeries(dto.ID, svcRead); err != nil {
		t.Fatalf("read.service in-scope: %v", err)
	}
	svcOut := Access{UserID: 932, Permissions: map[string]bool{"schedule.read.service": true}}
	if _, err := svc.GetAppointmentSeries(dto.ID, svcOut); statusOf(err) != 404 {
		t.Fatalf("out-of-scope get want 404 got %d", statusOf(err))
	}
	own := Access{UserID: 933, Permissions: map[string]bool{"schedule.read.own": true}}
	if _, err := svc.GetAppointmentSeries(dto.ID, own); statusOf(err) != 404 {
		t.Fatalf("own other prac want 404 got %d", statusOf(err))
	}
	ownPrac := Access{UserID: 930, Permissions: map[string]bool{"schedule.read.own": true}}
	if _, err := svc.GetAppointmentSeries(dto.ID, ownPrac); err != nil {
		t.Fatalf("own matching prac: %v", err)
	}
}

func ptrU(v uint) *uint { return &v }
