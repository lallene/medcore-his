package patient_queue

import (
	"testing"
	"time"
)

func TestLifecycleTransitionMatrix23E(t *testing.T) {
	// 1 SCHEDULED → reschedule
	if err := canTransitionAppointment(ApptScheduled, LifecycleOpReschedule); err != nil {
		t.Fatal(err)
	}
	// 2 SCHEDULED → cancel
	if err := canTransitionAppointment(ApptScheduled, LifecycleOpCancel); err != nil {
		t.Fatal(err)
	}
	// 3 SCHEDULED → no-show (eligibility time checked separately)
	if err := canTransitionAppointment(ApptScheduled, LifecycleOpNoShow); err != nil {
		t.Fatal(err)
	}
	// 5 ARRIVED reschedule rejected
	if err := canTransitionAppointment(ApptArrived, LifecycleOpReschedule); err == nil {
		t.Fatal("arrived reschedule")
	}
	// 6 CHECKED_IN reschedule
	if err := canTransitionAppointment(ApptCheckedIn, LifecycleOpReschedule); err == nil {
		t.Fatal("checked-in reschedule")
	}
	// 7 CHECKED_IN cancel
	if err := canTransitionAppointment(ApptCheckedIn, LifecycleOpCancel); err == nil {
		t.Fatal("checked-in cancel")
	}
	// 8–10 IN_PROGRESS
	if err := canTransitionAppointment(ApptInProgress, LifecycleOpReschedule); err == nil {
		t.Fatal("in-progress reschedule")
	}
	if err := canTransitionAppointment(ApptInProgress, LifecycleOpCancel); err == nil {
		t.Fatal("in-progress cancel")
	}
	if err := canTransitionAppointment(ApptInProgress, LifecycleOpNoShow); err == nil {
		t.Fatal("in-progress no-show")
	}
	// 11–13 COMPLETED terminal
	if err := canTransitionAppointment(ApptCompleted, LifecycleOpReschedule); err == nil {
		t.Fatal("completed reschedule")
	}
	if err := canTransitionAppointment(ApptCompleted, LifecycleOpCancel); err == nil {
		t.Fatal("completed cancel")
	}
	if err := canTransitionAppointment(ApptCompleted, LifecycleOpNoShow); err == nil {
		t.Fatal("completed no-show")
	}
	// 14 CANCELLED terminal (cancel idempotent allowed at transition layer)
	if err := canTransitionAppointment(ApptCancelled, LifecycleOpReschedule); err == nil {
		t.Fatal("cancelled reschedule")
	}
	if err := canTransitionAppointment(ApptCancelled, LifecycleOpNoShow); err == nil {
		t.Fatal("cancelled no-show")
	}
	// 15 NO_SHOW terminal
	if err := canTransitionAppointment(ApptNoShow, LifecycleOpReschedule); err == nil {
		t.Fatal("noshow reschedule")
	}
	if err := canTransitionAppointment(ApptNoShow, LifecycleOpCancel); err == nil {
		t.Fatal("noshow cancel")
	}
}

func TestNoShowEligibleTime23E(t *testing.T) {
	now := mustParse("2026-09-14T12:00:00Z")
	if noShowEligibleTime(mustParse("2026-09-14T13:00:00Z"), now) {
		t.Fatal("4 future must reject")
	}
	if !noShowEligibleTime(mustParse("2026-09-14T12:00:00Z"), now) {
		t.Fatal("at start allowed")
	}
	if !noShowEligibleTime(mustParse("2026-09-14T11:00:00Z"), now) {
		t.Fatal("past allowed")
	}
}

func mustParse(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}
