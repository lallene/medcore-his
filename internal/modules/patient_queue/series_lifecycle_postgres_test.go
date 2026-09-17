package patient_queue

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/core/scheduling"
)

func TestSeriesAdvisoryLockNamespacesSeparated23OB(t *testing.T) {
	if bookingLockNSLifecycle != 230404 {
		t.Fatalf("lifecycle NS want 230404 got %d", bookingLockNSLifecycle)
	}
	if bookingLockNSSeriesCreateIdempotency != 230405 {
		t.Fatalf("series create NS want 230405 got %d", bookingLockNSSeriesCreateIdempotency)
	}
	if bookingLockNSSeriesLifecycle != 230406 {
		t.Fatalf("series lifecycle NS want 230406 got %d", bookingLockNSSeriesLifecycle)
	}
	if bookingLockNSSeriesCreateIdempotency == bookingLockNSLifecycle ||
		bookingLockNSSeriesLifecycle == bookingLockNSLifecycle ||
		bookingLockNSSeriesCreateIdempotency == bookingLockNSSeriesLifecycle {
		t.Fatal("series create/lifecycle must not share namespaces with each other or occurrence lifecycle")
	}
}

func seedSeriesLifecycleFixture(t *testing.T) (*Service, Access, uint, *AppointmentType) {
	t.Helper()
	db := queuePostgres(t)
	svc := NewService(db)
	_ = scheduling.SetLocation("UTC")

	_ = db.Exec(`INSERT INTO users(id, name) VALUES (940,'DrSerLC'),(941,'ActorSerLC'),(942,'OutSerLC') ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO staff_profiles(id, user_id, active, primary_service_id) VALUES
		(940,940,true,10),(941,941,true,10),(942,942,true,11) ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO staff_service_assignments(profile_id, service_id, active) VALUES
		(940,10,true),(941,10,true),(942,11,true) ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO patients(id, code_patient, nom, prenoms) VALUES
		(940,'PLC','Life','Cycle'),(941,'PLC2','Life','Two') ON CONFLICT DO NOTHING`)

	admin := adminAccess(941)
	prac := uint(940)
	seedSeriesWideSchedule(t, svc, admin, prac)
	at, err := svc.CreateAppointmentType(CreateAppointmentTypeRequest{
		Code: "SER-LC", Name: "Series LC", DefaultDurationMinutes: 30,
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	return svc, admin, prac, at
}

func createSeriesForLifecycle(t *testing.T, svc *Service, admin Access, prac uint, atID uint, patientID uint, anchor time.Time, count int, key string) *AppointmentSeriesDTO {
	t.Helper()
	dto, reused, err := svc.CreateAppointmentSeries(CreateAppointmentSeriesRequest{
		PatientID: patientID, ServiceID: 10, PractitionerID: prac,
		AppointmentTypeID: &atID, Freq: SeriesFreqWeekly, IntervalWeeks: 1,
		ByWeekdays: []int{int(anchor.Weekday())}, Count: &count,
		Timezone: "UTC", AnchorStartAt: anchor, IdempotencyKey: key,
	}, admin)
	if err != nil || reused {
		t.Fatalf("create series: %v reused=%v", err, reused)
	}
	return dto
}

func TestPostgresCancelEntireSeries23OB(t *testing.T) {
	svc, admin, prac, at := seedSeriesLifecycleFixture(t)
	db := svc.db
	anchor := time.Date(2026, 12, 7, 9, 0, 0, 0, time.UTC)
	dto := createSeriesForLifecycle(t, svc, admin, prac, at.ID, 940, anchor, 3, "lc-entire-1")

	out, err := svc.CancelAppointmentSeries(dto.ID, CancelAppointmentSeriesRequest{
		ExpectedVersion: dto.Version, Reason: "patient request",
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != SeriesStatusCancelled {
		t.Fatalf("status want CANCELLED got %s", out.Status)
	}
	if out.Version != dto.Version+1 {
		t.Fatalf("version want %d got %d", dto.Version+1, out.Version)
	}
	for _, occ := range out.Occurrences {
		if occ.Status != ApptCancelled {
			t.Fatalf("occ %d status=%s", occ.Index, occ.Status)
		}
	}
	var hist, cancelledIntents, remindersCancelled int64
	db.Model(&AppointmentHistory{}).Where("event_type = ?", ApptHistCancelled).Count(&hist)
	if hist != 3 {
		t.Fatalf("cancel histories want 3 got %d", hist)
	}
	db.Model(&AppointmentNotificationIntent{}).Where("kind = ?", NotifKindCancelled).Count(&cancelledIntents)
	if cancelledIntents != 3 {
		t.Fatalf("CANCELLED intents want 3 got %d", cancelledIntents)
	}
	db.Model(&AppointmentNotificationIntent{}).
		Where("kind = ? AND status = ?", NotifKindReminderT24H, NotifStatusCancelled).
		Count(&remindersCancelled)
	if remindersCancelled < 1 {
		t.Fatalf("expected suppressed reminders, got %d", remindersCancelled)
	}

	// Idempotent replay
	again, err := svc.CancelAppointmentSeries(dto.ID, CancelAppointmentSeriesRequest{
		ExpectedVersion: out.Version, Reason: "again",
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	if again.Version != out.Version {
		t.Fatalf("idempotent cancel must not bump version: %d vs %d", again.Version, out.Version)
	}
	db.Model(&AppointmentHistory{}).Where("event_type = ?", ApptHistCancelled).Count(&hist)
	if hist != 3 {
		t.Fatalf("idempotent cancel must not add histories, got %d", hist)
	}
}

func TestPostgresCancelFutureAndOCC23OB(t *testing.T) {
	svc, admin, prac, at := seedSeriesLifecycleFixture(t)
	db := svc.db
	anchor := time.Date(2026, 12, 8, 10, 0, 0, 0, time.UTC) // Tuesday
	dto := createSeriesForLifecycle(t, svc, admin, prac, at.ID, 940, anchor, 4, "lc-future-1")

	// Mark first occurrence COMPLETED to prove mixed statuses are preserved.
	firstID := dto.Occurrences[0].ID
	if err := db.Model(&Appointment{}).Where("id = ?", firstID).Update("status", ApptCompleted).Error; err != nil {
		t.Fatal(err)
	}

	from := 3
	out, err := svc.CancelAppointmentSeriesFuture(dto.ID, CancelAppointmentSeriesFutureRequest{
		ExpectedVersion: dto.Version, FromOccurrenceIndex: &from, Reason: "cut",
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	if out.Version != dto.Version+1 {
		t.Fatalf("version bump want %d got %d", dto.Version+1, out.Version)
	}
	// index 2 still SCHEDULED → series ACTIVE
	if out.Status != SeriesStatusActive {
		t.Fatalf("partial future cancel want ACTIVE got %s", out.Status)
	}
	byIdx := map[int]string{}
	for _, o := range out.Occurrences {
		byIdx[o.Index] = o.Status
	}
	if byIdx[1] != ApptCompleted {
		t.Fatalf("completed preserved got %s", byIdx[1])
	}
	if byIdx[2] != ApptScheduled {
		t.Fatalf("index 2 want SCHEDULED got %s", byIdx[2])
	}
	if byIdx[3] != ApptCancelled || byIdx[4] != ApptCancelled {
		t.Fatalf("indexes 3–4 want CANCELLED got %v", byIdx)
	}

	// Stale version
	_, err = svc.CancelAppointmentSeriesFuture(dto.ID, CancelAppointmentSeriesFutureRequest{
		ExpectedVersion: dto.Version, FromOccurrenceIndex: &from,
	}, admin)
	if statusOf(err) != 409 {
		t.Fatalf("stale version want 409 got %d %v", statusOf(err), err)
	}

	// Cancel remaining scheduled (index 2) → series CANCELLED
	from2 := 2
	out2, err := svc.CancelAppointmentSeriesFuture(dto.ID, CancelAppointmentSeriesFutureRequest{
		ExpectedVersion: out.Version, FromOccurrenceIndex: &from2,
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	if out2.Status != SeriesStatusCancelled {
		t.Fatalf("want CANCELLED got %s", out2.Status)
	}

	// Mutate cancelled series (cancel-future) → 409
	_, err = svc.CancelAppointmentSeriesFuture(dto.ID, CancelAppointmentSeriesFutureRequest{
		ExpectedVersion: out2.Version, FromOccurrenceIndex: &from2,
	}, admin)
	if statusOf(err) != 409 {
		t.Fatalf("cancelled series future want 409 got %d", statusOf(err))
	}
}

func TestPostgresCancelSeriesRBACAndRollback23OB(t *testing.T) {
	svc, admin, prac, at := seedSeriesLifecycleFixture(t)
	db := svc.db
	anchor := time.Date(2026, 12, 9, 11, 0, 0, 0, time.UTC)
	dto := createSeriesForLifecycle(t, svc, admin, prac, at.ID, 940, anchor, 3, "lc-rbac-1")

	readOnly := Access{UserID: 941, Permissions: map[string]bool{"schedule.read.all": true}}
	if _, err := svc.CancelAppointmentSeries(dto.ID, CancelAppointmentSeriesRequest{ExpectedVersion: dto.Version}, readOnly); statusOf(err) != 403 {
		t.Fatalf("read-only want 403 got %d", statusOf(err))
	}
	manageOnly := Access{UserID: 941, Permissions: map[string]bool{"schedule.manage.all": true}}
	if _, err := svc.CancelAppointmentSeries(dto.ID, CancelAppointmentSeriesRequest{ExpectedVersion: dto.Version}, manageOnly); statusOf(err) != 403 {
		t.Fatalf("manage-only want 403 got %d", statusOf(err))
	}
	queueOnly := Access{UserID: 941, Permissions: map[string]bool{"queue.checkin": true}}
	if _, err := svc.CancelAppointmentSeries(dto.ID, CancelAppointmentSeriesRequest{ExpectedVersion: dto.Version}, queueOnly); statusOf(err) != 403 {
		t.Fatalf("queue want 403 got %d", statusOf(err))
	}
	outScope := Access{UserID: 942, Permissions: map[string]bool{"appointment.cancel.service": true}}
	if _, err := svc.CancelAppointmentSeries(dto.ID, CancelAppointmentSeriesRequest{ExpectedVersion: dto.Version}, outScope); statusOf(err) != 404 {
		t.Fatalf("out-of-scope want 404 got %d", statusOf(err))
	}

	canceller := Access{UserID: 941, Permissions: map[string]bool{"appointment.cancel.service": true}}
	// Force rollback: SCHEDULED occurrence with active queue ticket link.
	mid := dto.Occurrences[1].ID
	ticketID := uint(999001)
	now := time.Now().UTC()
	if err := db.Exec(`INSERT INTO patient_queue_tickets(
		id, reference, patient_id, appointment_id, source, service_id, arrived_at, checked_in_at,
		stage, status, priority, finance_status, identity_confirmed, version, created_by, created_at, updated_at
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,true,1,?,?,?)`,
		ticketID, "Q-LC-1", 940, mid, SourceAppointment, 10, now, now,
		StageWaitingTriage, StatusActive, PriorityNormal, FinanceClear, 941, now, now).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&Appointment{}).Where("id = ?", mid).Update("queue_ticket_id", ticketID).Error; err != nil {
		t.Fatal(err)
	}

	_, err := svc.CancelAppointmentSeries(dto.ID, CancelAppointmentSeriesRequest{ExpectedVersion: dto.Version}, canceller)
	if statusOf(err) != 409 {
		t.Fatalf("queue-linked scheduled want 409 got %d %v", statusOf(err), err)
	}
	var series AppointmentSeries
	_ = db.First(&series, dto.ID)
	if series.Status != SeriesStatusActive || series.Version != dto.Version {
		t.Fatalf("rollback want ACTIVE v=%d got %s v=%d", dto.Version, series.Status, series.Version)
	}
	var cancelled int64
	db.Model(&Appointment{}).Where("series_id = ? AND status = ?", dto.ID, ApptCancelled).Count(&cancelled)
	if cancelled != 0 {
		t.Fatalf("partial cancels must roll back, got %d", cancelled)
	}

	// Clear blocker and succeed with cancel.service
	_ = db.Model(&Appointment{}).Where("id = ?", mid).Update("queue_ticket_id", nil)
	ok, err := svc.CancelAppointmentSeries(dto.ID, CancelAppointmentSeriesRequest{ExpectedVersion: dto.Version}, canceller)
	if err != nil || ok.Status != SeriesStatusCancelled {
		t.Fatalf("cancel.service: %v status=%v", err, ok)
	}
}

func TestPostgresCancelSeriesConcurrentAndRescheduleGuard23OB(t *testing.T) {
	svc, admin, prac, at := seedSeriesLifecycleFixture(t)
	anchor := time.Date(2026, 12, 10, 9, 0, 0, 0, time.UTC)
	dto := createSeriesForLifecycle(t, svc, admin, prac, at.ID, 940, anchor, 2, "lc-conc-1")

	var okN, conflictN int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := svc.CancelAppointmentSeries(dto.ID, CancelAppointmentSeriesRequest{
				ExpectedVersion: dto.Version, Reason: "race",
			}, admin)
			if e == nil {
				atomic.AddInt32(&okN, 1)
			} else if statusOf(e) == 409 {
				atomic.AddInt32(&conflictN, 1)
			}
		}()
	}
	wg.Wait()
	if okN < 1 {
		t.Fatal("concurrent cancel: no success")
	}
	got, err := svc.GetAppointmentSeries(dto.ID, admin)
	if err != nil || got.Status != SeriesStatusCancelled {
		t.Fatalf("after race: %v status=%v", err, got)
	}

	// Fresh series for reschedule-after-cancel guard
	dto2 := createSeriesForLifecycle(t, svc, admin, prac, at.ID, 941, time.Date(2026, 12, 11, 9, 0, 0, 0, time.UTC), 2, "lc-rs-guard")
	cancelled, err := svc.CancelAppointmentSeries(dto2.ID, CancelAppointmentSeriesRequest{ExpectedVersion: dto2.Version}, admin)
	if err != nil {
		t.Fatal(err)
	}
	occ := cancelled.Occurrences[0]
	// Force one occ back to SCHEDULED to attempt reschedule against CANCELLED series (repair edge).
	_ = svc.db.Model(&Appointment{}).Where("id = ?", occ.ID).Update("status", ApptScheduled)
	end := occ.ScheduledEndAt
	_, err = svc.RescheduleAppointment(occ.ID, RescheduleAppointmentRequest{
		StartAt:             occ.ScheduledAt.Add(time.Hour),
		ExpectedScheduledAt: occ.ScheduledAt, ExpectedScheduledEndAt: end,
		Reason: "move",
	}, admin)
	if statusOf(err) != 409 {
		t.Fatalf("reschedule on cancelled series want 409 got %d %v", statusOf(err), err)
	}
}

func TestPostgresCancelFutureFromAppointmentID23OB(t *testing.T) {
	svc, admin, prac, at := seedSeriesLifecycleFixture(t)
	dto := createSeriesForLifecycle(t, svc, admin, prac, at.ID, 940,
		time.Date(2026, 12, 14, 9, 0, 0, 0, time.UTC), 3, "lc-from-appt")
	apptID := dto.Occurrences[1].ID
	out, err := svc.CancelAppointmentSeriesFuture(dto.ID, CancelAppointmentSeriesFutureRequest{
		ExpectedVersion: dto.Version, FromAppointmentID: &apptID,
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	if out.Occurrences[0].Status != ApptScheduled {
		t.Fatal("index 1 must remain scheduled")
	}
	if out.Occurrences[1].Status != ApptCancelled || out.Occurrences[2].Status != ApptCancelled {
		t.Fatal("from appointment id inclusive cancel failed")
	}
}
