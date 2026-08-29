package patient_queue

import (
	"testing"
	"time"
)

func TestResolveAppointmentIntervalHalfOpen(t *testing.T) {
	svc := &Service{}
	start := time.Date(2026, 8, 29, 9, 0, 0, 0, time.UTC)
	endOK := start.Add(30 * time.Minute)
	endBad := start

	_, end, _, err := svc.resolveAppointmentInterval(CreateAppointmentRequest{
		ServiceID: 1, ScheduledAt: start, ScheduledEndAt: &endOK,
	})
	// resolve hits DB only when type ID set; end-only path is pure
	if err != nil {
		t.Fatal(err)
	}
	if end == nil || !end.Equal(endOK) {
		t.Fatalf("end=%v", end)
	}

	_, _, _, err = svc.resolveAppointmentInterval(CreateAppointmentRequest{
		ServiceID: 1, ScheduledAt: start, ScheduledEndAt: &endBad,
	})
	if err == nil {
		t.Fatal("expected end<=start rejection")
	}
}
