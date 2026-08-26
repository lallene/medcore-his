package ticketing

import (
	"testing"
	"time"
)

func TestPriorityMatrix(t *testing.T) {
	cases := []struct{ impact, urgency, want string }{
		{"INDIVIDUAL", "LOW", "P4"}, {"SERVICE", "MEDIUM", "P3"},
		{"DEPARTMENT", "HIGH", "P2"}, {"FACILITY", "CRITICAL", "P1"},
	}
	for _, tc := range cases {
		if got := priorityFor(tc.impact, tc.urgency); got != tc.want {
			t.Fatalf("%s/%s: got %s want %s", tc.impact, tc.urgency, got, tc.want)
		}
	}
}

func TestSLADefaultsDeterministic(t *testing.T) {
	for _, tc := range []struct {
		p    string
		r, x int
	}{{"P1", 15, 120}, {"P2", 30, 240}, {"P3", 240, 1440}, {"P4", 1440, 4320}} {
		r, x := defaults(tc.p)
		if r != tc.r || x != tc.x {
			t.Fatalf("%s: %d/%d", tc.p, r, x)
		}
		now := time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC)
		if !now.Add(time.Duration(r) * time.Minute).After(now) {
			t.Fatal("invalid response deadline")
		}
	}
}

func TestWorkflowTransitions(t *testing.T) {
	if transitions["CLOSED"]["IN_PROGRESS"] {
		t.Fatal("closed ticket must require explicit reopen")
	}
	if !transitions["RESOLVED"]["REOPENED"] {
		t.Fatal("resolved ticket must be reopenable")
	}
	if !transitions["REOPENED"]["IN_PROGRESS"] {
		t.Fatal("reopened ticket must resume")
	}
}

func TestAccessNeverLeaksAnotherRequester(t *testing.T) {
	ticket := Ticket{RequesterUserID: 2}
	if (&Service{}).permitted(&ticket, Access{UserID: 3, Permissions: map[string]bool{"ticket.read.own": true}}) {
		t.Fatal("another requester's ticket leaked")
	}
	if !(&Service{}).permitted(&ticket, Access{UserID: 2, Permissions: map[string]bool{"ticket.read.own": true}}) {
		t.Fatal("owner denied")
	}
}
