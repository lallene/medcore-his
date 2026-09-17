package patient_queue

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/core/scheduling"
)

func seedSeriesUpdateFixture(t *testing.T) (*Service, Access, uint, uint, *AppointmentType) {
	t.Helper()
	db := queuePostgres(t)
	svc := NewService(db)
	_ = scheduling.SetLocation("UTC")

	_ = db.Exec(`INSERT INTO users(id, name) VALUES
		(950,'DrSerUpA'),(951,'DrSerUpB'),(952,'ActorSerUp'),(953,'OutSerUp') ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO staff_profiles(id, user_id, active, primary_service_id) VALUES
		(950,950,true,10),(951,951,true,10),(952,952,true,10),(953,953,true,11) ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO staff_service_assignments(profile_id, service_id, active) VALUES
		(950,10,true),(951,10,true),(952,10,true),(953,11,true) ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO patients(id, code_patient, nom, prenoms) VALUES
		(950,'PUP','Up','One'),(951,'PUP2','Up','Two') ON CONFLICT DO NOTHING`)

	admin := adminAccess(952)
	pracA := uint(950)
	pracB := uint(951)
	seedSeriesWideSchedule(t, svc, admin, pracA)
	seedSeriesWideSchedule(t, svc, admin, pracB)
	at, err := svc.CreateAppointmentType(CreateAppointmentTypeRequest{
		Code: "SER-UP", Name: "Series Up", DefaultDurationMinutes: 30,
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	return svc, admin, pracA, pracB, at
}

func createSeriesForUpdate(t *testing.T, svc *Service, admin Access, prac uint, atID uint, patientID uint, anchor time.Time, count int, key string) *AppointmentSeriesDTO {
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

func TestPostgresUpdateSeriesFutureScheduled23OC(t *testing.T) {
	svc, admin, pracA, pracB, at := seedSeriesUpdateFixture(t)
	db := svc.db
	anchor := time.Date(2026, 12, 7, 9, 0, 0, 0, time.UTC)
	dto := createSeriesForUpdate(t, svc, admin, pracA, at.ID, 950, anchor, 4, "up-ok-1")

	// Past / operational / exception fixtures
	pastID := dto.Occurrences[0].ID
	pastAt := dto.Occurrences[0].ScheduledAt
	if err := db.Model(&Appointment{}).Where("id = ?", pastID).Update("status", ApptCompleted).Error; err != nil {
		t.Fatal(err)
	}
	arrivedID := dto.Occurrences[1].ID
	arrivedAt := dto.Occurrences[1].ScheduledAt
	if err := db.Model(&Appointment{}).Where("id = ?", arrivedID).Update("status", ApptArrived).Error; err != nil {
		t.Fatal(err)
	}

	// Individually cancel occurrence 3
	if _, err := svc.CancelAppointment(dto.Occurrences[2].ID, CancelAppointmentRequest{Reason: "one-off"}, admin); err != nil {
		t.Fatal(err)
	}
	cancelledAt := dto.Occurrences[2].ScheduledAt

	// Individually reschedule occurrence 4 (exception)
	occ4 := dto.Occurrences[3]
	excStart := occ4.ScheduledAt.Add(2 * time.Hour)
	end := occ4.ScheduledEndAt
	if _, err := svc.RescheduleAppointment(occ4.ID, RescheduleAppointmentRequest{
		StartAt:             excStart,
		ExpectedScheduledAt: occ4.ScheduledAt, ExpectedScheduledEndAt: end,
		Reason: "exception",
	}, admin); err != nil {
		t.Fatal(err)
	}

	// Rebuild a clean 4-occ series for the happy path of updating remaining SCHEDULED aligned rows.
	dto2 := createSeriesForUpdate(t, svc, admin, pracA, at.ID, 951,
		time.Date(2026, 12, 8, 9, 0, 0, 0, time.UTC), 4, "up-ok-2")
	from := 2
	newAnchor := time.Date(2026, 12, 15, 10, 0, 0, 0, time.UTC) // Tuesday 10:00 — segment for indexes 2..4
	out, err := svc.UpdateAppointmentSeries(dto2.ID, UpdateAppointmentSeriesRequest{
		ExpectedVersion: dto2.Version, FromOccurrenceIndex: &from,
		PractitionerID: &pracB, AnchorStartAt: &newAnchor, Reason: "move future",
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	if out.Version != dto2.Version+1 {
		t.Fatalf("version want %d got %d", dto2.Version+1, out.Version)
	}
	if out.PractitionerID != pracB {
		t.Fatalf("series practitioner want %d got %d", pracB, out.PractitionerID)
	}
	if !out.AnchorStartAt.Equal(newAnchor) {
		t.Fatalf("anchor want %v got %v", newAnchor, out.AnchorStartAt)
	}
	orig1 := dto2.Occurrences[0]
	byIdx := map[int]SeriesOccurrenceDTO{}
	for _, o := range out.Occurrences {
		byIdx[o.Index] = o
	}
	if !byIdx[1].ScheduledAt.Equal(orig1.ScheduledAt) || byIdx[1].PractitionerID != pracA {
		t.Fatalf("index 1 must stay original: got %+v want at=%v prac=%d", byIdx[1], orig1.ScheduledAt, pracA)
	}
	if !byIdx[2].ScheduledAt.Equal(newAnchor) {
		t.Fatalf("index 2 want %v got %v", newAnchor, byIdx[2].ScheduledAt)
	}
	if byIdx[2].PractitionerID != pracB {
		t.Fatalf("index 2 practitioner want %d got %d", pracB, byIdx[2].PractitionerID)
	}

	// Verify first series preserved operational/exception rows
	got, err := svc.GetAppointmentSeries(dto.ID, admin)
	if err != nil {
		t.Fatal(err)
	}
	g1 := got.Occurrences[0]
	g2 := got.Occurrences[1]
	g3 := got.Occurrences[2]
	g4 := got.Occurrences[3]
	if g1.Status != ApptCompleted || !g1.ScheduledAt.Equal(pastAt) {
		t.Fatalf("completed past mutated: %+v", g1)
	}
	if g2.Status != ApptArrived || !g2.ScheduledAt.Equal(arrivedAt) {
		t.Fatalf("arrived mutated: %+v", g2)
	}
	if g3.Status != ApptCancelled || !g3.ScheduledAt.Equal(cancelledAt) {
		t.Fatalf("cancelled exception mutated: %+v", g3)
	}
	if g4.Status != ApptScheduled || !g4.ScheduledAt.Equal(excStart) {
		t.Fatalf("reschedule exception mutated: %+v", g4)
	}

	// Preserve exception under series update from index 1 on dto (only SCHEDULED non-diverged would move —
	// here index 4 is diverged; indexes 1–2 operational; 3 cancelled → no updatable; still allow meta? need a change)
	// Meta-only practitioner on dto should not touch exceptions/operational.
	from1 := 1
	outMeta, err := svc.UpdateAppointmentSeries(dto.ID, UpdateAppointmentSeriesRequest{
		ExpectedVersion: dto.Version, FromOccurrenceIndex: &from1, PractitionerID: &pracB,
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	if outMeta.Version != dto.Version+1 {
		t.Fatalf("meta version bump failed")
	}
	reload, _ := svc.GetAppointmentSeries(dto.ID, admin)
	for _, o := range reload.Occurrences {
		switch o.Index {
		case 1:
			if o.Status != ApptCompleted || !o.ScheduledAt.Equal(pastAt) {
				t.Fatalf("completed changed after meta update")
			}
		case 2:
			if o.Status != ApptArrived {
				t.Fatalf("arrived changed after meta update")
			}
		case 3:
			if o.Status != ApptCancelled {
				t.Fatalf("cancelled changed after meta update")
			}
		case 4:
			if !o.ScheduledAt.Equal(excStart) {
				t.Fatalf("exception time changed after meta update")
			}
			// exception keeps its own practitioner from 23E reschedule (same pracA)
			if o.PractitionerID != pracA {
				t.Fatalf("exception practitioner should remain %d", pracA)
			}
		}
	}
}

func TestPostgresUpdateSeriesOCCCancelledConflictRBAC23OC(t *testing.T) {
	svc, admin, pracA, _, at := seedSeriesUpdateFixture(t)
	db := svc.db
	anchor := time.Date(2026, 12, 9, 9, 0, 0, 0, time.UTC)
	dto := createSeriesForUpdate(t, svc, admin, pracA, at.ID, 950, anchor, 3, "up-occ-1")

	from := 2
	newAnchor := time.Date(2026, 12, 16, 11, 0, 0, 0, time.UTC)

	// Stale version
	_, err := svc.UpdateAppointmentSeries(dto.ID, UpdateAppointmentSeriesRequest{
		ExpectedVersion: dto.Version + 10, FromOccurrenceIndex: &from, AnchorStartAt: &newAnchor,
	}, admin)
	if statusOf(err) != 409 {
		t.Fatalf("stale version want 409 got %d %v", statusOf(err), err)
	}
	var series AppointmentSeries
	_ = db.First(&series, dto.ID)
	if series.Version != dto.Version {
		t.Fatalf("stale update mutated version")
	}

	// Unauthorized roles
	readOnly := Access{UserID: 952, Permissions: map[string]bool{"schedule.read.all": true}}
	if _, err := svc.UpdateAppointmentSeries(dto.ID, UpdateAppointmentSeriesRequest{
		ExpectedVersion: dto.Version, FromOccurrenceIndex: &from, AnchorStartAt: &newAnchor,
	}, readOnly); statusOf(err) != 403 {
		t.Fatalf("read want 403 got %d", statusOf(err))
	}
	cancelOnly := Access{UserID: 952, Permissions: map[string]bool{"appointment.cancel.service": true}}
	if _, err := svc.UpdateAppointmentSeries(dto.ID, UpdateAppointmentSeriesRequest{
		ExpectedVersion: dto.Version, FromOccurrenceIndex: &from, AnchorStartAt: &newAnchor,
	}, cancelOnly); statusOf(err) != 403 {
		t.Fatalf("cancel-only want 403 got %d", statusOf(err))
	}
	queueOnly := Access{UserID: 952, Permissions: map[string]bool{"queue.checkin": true}}
	if _, err := svc.UpdateAppointmentSeries(dto.ID, UpdateAppointmentSeriesRequest{
		ExpectedVersion: dto.Version, FromOccurrenceIndex: &from, AnchorStartAt: &newAnchor,
	}, queueOnly); statusOf(err) != 403 {
		t.Fatalf("queue want 403 got %d", statusOf(err))
	}
	outScope := Access{UserID: 953, Permissions: map[string]bool{"appointment.reschedule.service": true}}
	if _, err := svc.UpdateAppointmentSeries(dto.ID, UpdateAppointmentSeriesRequest{
		ExpectedVersion: dto.Version, FromOccurrenceIndex: &from, AnchorStartAt: &newAnchor,
	}, outScope); statusOf(err) != 404 {
		t.Fatalf("out-of-scope want 404 got %d", statusOf(err))
	}

	// Cancel series then refuse update
	cancelled, err := svc.CancelAppointmentSeries(dto.ID, CancelAppointmentSeriesRequest{ExpectedVersion: dto.Version}, admin)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.UpdateAppointmentSeries(dto.ID, UpdateAppointmentSeriesRequest{
		ExpectedVersion: cancelled.Version, FromOccurrenceIndex: &from, AnchorStartAt: &newAnchor,
	}, admin)
	if statusOf(err) != 409 {
		t.Fatalf("cancelled series update want 409 got %d", statusOf(err))
	}
}

func TestPostgresUpdateSeriesConflictRollbackAnd23N23OC(t *testing.T) {
	svc, admin, pracA, _, at := seedSeriesUpdateFixture(t)
	db := svc.db
	anchor := time.Date(2026, 12, 10, 9, 0, 0, 0, time.UTC)
	dto := createSeriesForUpdate(t, svc, admin, pracA, at.ID, 950, anchor, 3, "up-rb-1")

	var beforeHist, beforeIntents int64
	db.Model(&AppointmentHistory{}).Where("event_type = ?", ApptHistRescheduled).Count(&beforeHist)
	db.Model(&AppointmentNotificationIntent{}).Where("kind = ?", NotifKindRescheduled).Count(&beforeIntents)

	// Block the second regenerated slot (index 2 under new anchor)
	block := time.Date(2026, 12, 24, 14, 0, 0, 0, time.UTC)
	_, _, err := svc.BookAppointment(BookAppointmentRequest{
		PatientID: 951, ServiceID: 10, PractitionerID: &pracA,
		AppointmentTypeID: &at.ID, StartAt: block, IdempotencyKey: "up-block",
	}, admin)
	if err != nil {
		t.Fatal(err)
	}

	from := 2
	newAnchor := time.Date(2026, 12, 17, 14, 0, 0, 0, time.UTC) // → occ2=17th, occ3=24th conflicts
	_, err = svc.UpdateAppointmentSeries(dto.ID, UpdateAppointmentSeriesRequest{
		ExpectedVersion: dto.Version, FromOccurrenceIndex: &from, AnchorStartAt: &newAnchor,
	}, admin)
	if statusOf(err) != 409 {
		t.Fatalf("conflict want 409 got %d %v", statusOf(err), err)
	}

	var series AppointmentSeries
	_ = db.First(&series, dto.ID)
	if series.Version != dto.Version || series.Status != SeriesStatusActive {
		t.Fatalf("rollback series state: v=%d status=%s", series.Version, series.Status)
	}
	reload, _ := svc.GetAppointmentSeries(dto.ID, admin)
	for i, o := range reload.Occurrences {
		if !o.ScheduledAt.Equal(dto.Occurrences[i].ScheduledAt) {
			t.Fatalf("partial occurrence mutation at index %d", o.Index)
		}
	}
	var afterHist, afterIntents int64
	db.Model(&AppointmentHistory{}).Where("event_type = ?", ApptHistRescheduled).Count(&afterHist)
	db.Model(&AppointmentNotificationIntent{}).Where("kind = ?", NotifKindRescheduled).Count(&afterIntents)
	if afterHist != beforeHist || afterIntents != beforeIntents {
		t.Fatalf("23N/history side effects leaked: hist %d→%d intents %d→%d", beforeHist, afterHist, beforeIntents, afterIntents)
	}
}

func TestPostgresUpdateSeriesConcurrent23OC(t *testing.T) {
	svc, admin, pracA, _, at := seedSeriesUpdateFixture(t)
	dto := createSeriesForUpdate(t, svc, admin, pracA, at.ID, 950,
		time.Date(2026, 12, 11, 9, 0, 0, 0, time.UTC), 2, "up-conc-1")
	from := 1
	newAnchor := time.Date(2026, 12, 11, 11, 0, 0, 0, time.UTC)

	var okN, conflictN int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := svc.UpdateAppointmentSeries(dto.ID, UpdateAppointmentSeriesRequest{
				ExpectedVersion: dto.Version, FromOccurrenceIndex: &from, AnchorStartAt: &newAnchor,
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
		t.Fatal("concurrent update: no success")
	}
	got, err := svc.GetAppointmentSeries(dto.ID, admin)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != dto.Version+1 {
		t.Fatalf("version must bump exactly once: want %d got %d (ok=%d conflict=%d)", dto.Version+1, got.Version, okN, conflictN)
	}
	if !got.AnchorStartAt.Equal(newAnchor) {
		t.Fatalf("anchor after race: %v", got.AnchorStartAt)
	}
}

func TestPostgresUpdateSeriesPreserves23EExceptionOnRegen23OC(t *testing.T) {
	svc, admin, pracA, _, at := seedSeriesUpdateFixture(t)
	dto := createSeriesForUpdate(t, svc, admin, pracA, at.ID, 950,
		time.Date(2026, 12, 14, 9, 0, 0, 0, time.UTC), 3, "up-exc-1")

	occ2 := dto.Occurrences[1]
	excStart := occ2.ScheduledAt.Add(3 * time.Hour)
	if _, err := svc.RescheduleAppointment(occ2.ID, RescheduleAppointmentRequest{
		StartAt:             excStart,
		ExpectedScheduledAt: occ2.ScheduledAt, ExpectedScheduledEndAt: occ2.ScheduledEndAt,
		Reason: "keep me",
	}, admin); err != nil {
		t.Fatal(err)
	}

	from := 1
	newAnchor := time.Date(2026, 12, 14, 10, 0, 0, 0, time.UTC)
	out, err := svc.UpdateAppointmentSeries(dto.ID, UpdateAppointmentSeriesRequest{
		ExpectedVersion: dto.Version, FromOccurrenceIndex: &from, AnchorStartAt: &newAnchor,
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	byIdx := map[int]SeriesOccurrenceDTO{}
	for _, o := range out.Occurrences {
		byIdx[o.Index] = o
	}
	if !byIdx[2].ScheduledAt.Equal(excStart) {
		t.Fatalf("exception index 2 must be preserved, got %v", byIdx[2].ScheduledAt)
	}
	if !byIdx[1].ScheduledAt.Equal(newAnchor) {
		t.Fatalf("index 1 want regenerated %v got %v", newAnchor, byIdx[1].ScheduledAt)
	}
	// Expansion: anchor=14@10 → starts for remaining count; exception at idx2 forces alternate index for that slot.
	if byIdx[3].ScheduledAt.Equal(excStart) {
		t.Fatal("index 3 should not be exception time")
	}
}
