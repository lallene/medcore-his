package main

import (
	"strings"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/patient_queue"
)

func TestParseNotificationWorkerPoll(t *testing.T) {
	t.Parallel()
	fallback := patient_queue.NotificationWorkerPollDefault

	cases := []struct {
		name    string
		raw     string
		want    time.Duration
		wantErr bool
	}{
		{"unset", "", fallback, false},
		{"empty", "", fallback, false},
		{"whitespace", "  \t  ", fallback, false},
		{"5s", "5s", 5 * time.Second, false},
		{"padded_5s", "  5s  ", 5 * time.Second, false},
		{"malformed", "not-a-duration", 0, true},
		{"zero", "0", 0, true},
		{"zero_s", "0s", 0, true},
		{"negative", "-1s", 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseNotificationWorkerPoll(tc.raw, fallback)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				if !strings.Contains(err.Error(), EnvNotificationWorkerPoll) {
					t.Fatalf("error should name key: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}
