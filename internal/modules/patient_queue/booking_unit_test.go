package patient_queue

import (
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/core/scheduling"
)

func TestResolveBookingDurationPolicies(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	admin := adminAccess(100)
	at, err := svc.CreateAppointmentType(CreateAppointmentTypeRequest{
		Code: "BOOK-DUR", Name: "Dur", DefaultDurationMinutes: 30,
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 9, 14, 9, 0, 0, 0, time.UTC)

	r, err := svc.resolveBookingDuration(10, &at.ID, nil, start)
	if err != nil || r.DurationMinutes != 30 || !r.End.Equal(start.Add(30*time.Minute)) {
		t.Fatalf("type only: %+v %v", r, err)
	}
	d45 := 45
	r, err = svc.resolveBookingDuration(10, nil, &d45, start)
	if err != nil || r.DurationMinutes != 45 {
		t.Fatalf("explicit: %+v %v", r, err)
	}
	d30 := 30
	r, err = svc.resolveBookingDuration(10, &at.ID, &d30, start)
	if err != nil || r.DurationMinutes != 30 {
		t.Fatalf("matching: %+v %v", r, err)
	}
	_, err = svc.resolveBookingDuration(10, &at.ID, &d45, start)
	if statusOf(err) != 400 {
		t.Fatalf("mismatch want 400 got %d", statusOf(err))
	}
	_, err = svc.resolveBookingDuration(10, nil, nil, start)
	if statusOf(err) != 400 {
		t.Fatalf("neither want 400 got %d", statusOf(err))
	}

	sid := uint(11)
	atSvc, err := svc.CreateAppointmentType(CreateAppointmentTypeRequest{
		Code: "BOOK-SVC11", Name: "S11", DefaultDurationMinutes: 20, ServiceID: &sid,
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.resolveBookingDuration(10, &atSvc.ID, nil, start)
	if statusOf(err) != 400 {
		t.Fatalf("service mismatch want 400 got %d", statusOf(err))
	}
	_ = db.Model(&AppointmentType{}).Where("id=?", at.ID).Update("active", false)
	_, err = svc.resolveBookingDuration(10, &at.ID, nil, start)
	if statusOf(err) != 400 {
		t.Fatalf("inactive want 400 got %d", statusOf(err))
	}
}

func TestBookingOverlapHelpers(t *testing.T) {
	start := time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)
	end := start.Add(30 * time.Minute)
	typeDur := map[uint]int{}

	adj := apptBlockRow{
		ScheduledAt: end, ScheduledEndAt: timePtr(end.Add(30 * time.Minute)), Status: ApptScheduled,
	}
	if appointmentOverlapsRequested(adj, start, end, typeDur) {
		t.Fatal("adjacent must not overlap")
	}
	over := apptBlockRow{
		ScheduledAt: start.Add(15 * time.Minute),
		ScheduledEndAt: timePtr(end.Add(15 * time.Minute)), Status: ApptScheduled,
	}
	if !appointmentOverlapsRequested(over, start, end, typeDur) {
		t.Fatal("overlap expected")
	}
	cancelled := over
	cancelled.Status = ApptCancelled
	if appointmentOverlapsRequested(cancelled, start, end, typeDur) {
		t.Fatal("cancelled must not block")
	}
	noshow := over
	noshow.Status = ApptNoShow
	if appointmentOverlapsRequested(noshow, start, end, typeDur) {
		t.Fatal("no-show must not block")
	}
	unknown := over
	unknown.Status = "WEIRD"
	if !appointmentOverlapsRequested(unknown, start, end, typeDur) {
		t.Fatal("unknown fail closed")
	}
	legacy := apptBlockRow{ScheduledAt: start, Status: ApptScheduled}
	if !appointmentOverlapsRequested(legacy, start, end, typeDur) {
		t.Fatal("legacy NULL end should block via fallback")
	}

	// Partial containment: free 09–10, request 09:45–10:15
	free := []scheduling.Interval{{
		Start: time.Date(2026, 9, 14, 9, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC),
	}}
	reqStart := time.Date(2026, 9, 14, 9, 45, 0, 0, time.UTC)
	reqEnd := time.Date(2026, 9, 14, 10, 15, 0, 0, time.UTC)
	contained := false
	for _, iv := range free {
		if !iv.Start.After(reqStart) && !iv.End.Before(reqEnd) {
			contained = true
		}
	}
	if contained {
		t.Fatal("partial interval must not be fully contained")
	}
	fullStart := time.Date(2026, 9, 14, 9, 0, 0, 0, time.UTC)
	fullEnd := time.Date(2026, 9, 14, 9, 30, 0, 0, time.UTC)
	for _, iv := range free {
		if !iv.Start.After(fullStart) && !iv.End.Before(fullEnd) {
			contained = true
		}
	}
	if !contained {
		t.Fatal("fully contained interval should pass")
	}
}

func timePtr(t time.Time) *time.Time { return &t }
