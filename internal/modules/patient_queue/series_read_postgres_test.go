package patient_queue

import (
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/core/scheduling"
)

func TestPostgresSeriesReadDetailAndOccurrences23OD(t *testing.T) {
	svc, admin, prac, at := seedSeriesLifecycleFixture(t)
	db := svc.db
	_ = scheduling.SetLocation("UTC")

	anchor := time.Date(2026, 12, 7, 9, 0, 0, 0, time.UTC)
	dto := createSeriesForLifecycle(t, svc, admin, prac, at.ID, 940, anchor, 3, "read-od-1")

	got, err := svc.GetAppointmentSeries(dto.ID, admin)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != SeriesStatusActive || got.Version < 1 {
		t.Fatalf("active detail: status=%s version=%d", got.Status, got.Version)
	}
	if len(got.Occurrences) != 3 {
		t.Fatalf("want 3 occ got %d", len(got.Occurrences))
	}
	for _, o := range got.Occurrences {
		if o.Kind != SeriesOccKindRule {
			t.Fatalf("index %d want RULE got %s", o.Index, o.Kind)
		}
	}

	list, err := svc.ListAppointmentSeriesOccurrences(dto.ID, admin)
	if err != nil {
		t.Fatal(err)
	}
	if list.SeriesID != dto.ID || len(list.Items) != 3 {
		t.Fatalf("occurrences list: %+v", list)
	}

	// 23E reschedule exception
	occ2 := got.Occurrences[1]
	exc := occ2.ScheduledAt.Add(2 * time.Hour)
	if _, err := svc.RescheduleAppointment(occ2.ID, RescheduleAppointmentRequest{
		StartAt:             exc,
		ExpectedScheduledAt: occ2.ScheduledAt, ExpectedScheduledEndAt: occ2.ScheduledEndAt,
		Reason: "od-exc",
	}, admin); err != nil {
		t.Fatal(err)
	}
	// Operational
	if err := db.Model(&Appointment{}).Where("id = ?", got.Occurrences[0].ID).Update("status", ApptCompleted).Error; err != nil {
		t.Fatal(err)
	}
	// Individual cancel
	if _, err := svc.CancelAppointment(got.Occurrences[2].ID, CancelAppointmentRequest{Reason: "one"}, admin); err != nil {
		t.Fatal(err)
	}

	reload, err := svc.GetAppointmentSeries(dto.ID, admin)
	if err != nil {
		t.Fatal(err)
	}
	byIdx := map[int]string{}
	for _, o := range reload.Occurrences {
		byIdx[o.Index] = o.Kind
	}
	if byIdx[1] != SeriesOccKindOperational {
		t.Fatalf("index 1 want OPERATIONAL got %s", byIdx[1])
	}
	if byIdx[2] != SeriesOccKindExceptionRescheduled {
		t.Fatalf("index 2 want EXCEPTION_RESCHEDULED got %s", byIdx[2])
	}
	if byIdx[3] != SeriesOccKindExceptionCancelled {
		t.Fatalf("index 3 want EXCEPTION_CANCELLED got %s", byIdx[3])
	}

	// Cancelled series still readable
	cancelled, err := svc.CancelAppointmentSeries(dto.ID, CancelAppointmentSeriesRequest{ExpectedVersion: reload.Version}, admin)
	if err != nil {
		t.Fatal(err)
	}
	again, err := svc.GetAppointmentSeries(cancelled.ID, admin)
	if err != nil || again.Status != SeriesStatusCancelled {
		t.Fatalf("cancelled detail: %v status=%v", err, again)
	}
	for _, o := range again.Occurrences {
		if o.Kind != SeriesOccKindExceptionCancelled && o.Kind != SeriesOccKindOperational {
			// completed stays operational; cancelled rows are EXCEPTION_CANCELLED
			if o.Status == ApptCompleted && o.Kind == SeriesOccKindOperational {
				continue
			}
			if o.Status == ApptCancelled && o.Kind == SeriesOccKindExceptionCancelled {
				continue
			}
			t.Fatalf("unexpected kind after series cancel: %+v", o)
		}
	}

	readOnly := Access{UserID: 941, Permissions: map[string]bool{"schedule.read.all": true}}
	if _, err := svc.GetAppointmentSeries(dto.ID, readOnly); err != nil {
		t.Fatalf("read.all must see series: %v", err)
	}
	outScope := Access{UserID: 942, Permissions: map[string]bool{"schedule.read.service": true}}
	if _, err := svc.GetAppointmentSeries(dto.ID, outScope); statusOf(err) != 404 {
		t.Fatalf("out-of-scope want 404 got %d", statusOf(err))
	}
	if _, err := svc.ListAppointmentSeriesOccurrences(dto.ID, outScope); statusOf(err) != 404 {
		t.Fatalf("occurrences out-of-scope want 404 got %d", statusOf(err))
	}
	noRead := Access{UserID: 941, Permissions: map[string]bool{"appointment.create.service": true}}
	if _, err := svc.GetAppointmentSeries(dto.ID, noRead); statusOf(err) != 404 {
		t.Fatalf("no read perm want 404 got %d", statusOf(err))
	}
}

func TestClassifySeriesOccurrenceUnit23OD(t *testing.T) {
	prac := uint(1)
	typeID := uint(2)
	idx := 1
	start := time.Date(2026, 12, 7, 9, 0, 0, 0, time.UTC)
	end := start.Add(30 * time.Minute)
	series := &AppointmentSeries{PractitionerID: prac, AppointmentTypeID: &typeID, DurationMinutes: 30}
	expected := map[int]time.Time{1: start}
	ap := Appointment{
		SeriesOccurrenceIndex: &idx, Status: ApptScheduled,
		ScheduledAt: start, ScheduledEndAt: &end,
		ExpectedDoctorID: &prac, AppointmentTypeID: &typeID,
	}
	if classifySeriesOccurrence(ap, series, expected) != SeriesOccKindRule {
		t.Fatal("aligned scheduled")
	}
	ap.ScheduledAt = start.Add(time.Hour)
	if classifySeriesOccurrence(ap, series, expected) != SeriesOccKindExceptionRescheduled {
		t.Fatal("rescheduled")
	}
	ap.Status = ApptCancelled
	if classifySeriesOccurrence(ap, series, expected) != SeriesOccKindExceptionCancelled {
		t.Fatal("cancelled")
	}
	ap.Status = ApptArrived
	if classifySeriesOccurrence(ap, series, expected) != SeriesOccKindOperational {
		t.Fatal("operational")
	}
}
