package patient_queue

import (
	"strings"
	"testing"
	"time"
)

func TestCanTransition_AllowedPaths(t *testing.T) {
	cases := []struct {
		from, to string
	}{
		{StageReception, StageWaitingTriage},
		{StageWaitingTriage, StageTriageInProgress},
		{StageTriageInProgress, StageWaitingDoctor},
		{StageWaitingDoctor, StageDoctorInProgress},
		{StageDoctorInProgress, StageCompleted},
		{StageOnHold, StageWaitingDoctor},
	}
	for _, tc := range cases {
		if !CanTransition(tc.from, tc.to) {
			t.Fatalf("expected allowed transition %s → %s", tc.from, tc.to)
		}
	}
}

func TestCanTransition_ForbiddenPaths(t *testing.T) {
	cases := []struct {
		from, to string
	}{
		{StageReception, StageCompleted},
		{StageWaitingTriage, StageDoctorInProgress},
		{StageCompleted, StageWaitingTriage},
		{"UNKNOWN", StageWaitingTriage},
	}
	for _, tc := range cases {
		if CanTransition(tc.from, tc.to) {
			t.Fatalf("expected forbidden transition %s → %s", tc.from, tc.to)
		}
	}
}

func TestAssertTransition(t *testing.T) {
	if err := AssertTransition(StageDoctorInProgress, StageCompleted); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	err := AssertTransition(StageReception, StageCompleted)
	if err == nil {
		t.Fatal("expected error for forbidden transition")
	}
	if !strings.Contains(err.Error(), StageReception) || !strings.Contains(err.Error(), StageCompleted) {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestPriorityRank(t *testing.T) {
	if PriorityRank(PriorityUrgent) >= PriorityRank(PriorityHigh) {
		t.Fatal("URGENT must rank above HIGH")
	}
	if PriorityRank(PriorityHigh) >= PriorityRank(PriorityNormal) {
		t.Fatal("HIGH must rank above NORMAL")
	}
	if PriorityRank(PriorityNormal) >= PriorityRank(PriorityLow) {
		t.Fatal("NORMAL must rank above LOW")
	}
	if PriorityRank("INVALID") != 99 {
		t.Fatal("unknown priority must rank 99")
	}
}

func TestPunctuality(t *testing.T) {
	scheduled := time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC)
	if Punctuality(scheduled, scheduled.Add(-20*time.Minute)) != PunctualEarly {
		t.Fatal("expected EARLY for arrival 20 minutes before scheduled time")
	}
	if Punctuality(scheduled, scheduled.Add(5*time.Minute)) != PunctualOnTime {
		t.Fatal("expected ON_TIME within appointment window")
	}
	if Punctuality(scheduled, scheduled.Add(20*time.Minute)) != PunctualLate {
		t.Fatal("expected LATE for arrival 20 minutes after scheduled time")
	}
}
