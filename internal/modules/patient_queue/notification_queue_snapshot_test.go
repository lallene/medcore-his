package patient_queue

import (
	"testing"
	"time"
)

func TestOldestDueAgeFromSendAfter(t *testing.T) {
	t.Parallel()
	asOf := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	if got := oldestDueAgeFromSendAfter(asOf, nil); got != 0 {
		t.Fatalf("nil oldest → 0, got %s", got)
	}

	due := asOf.Add(-90 * time.Second)
	if got := oldestDueAgeFromSendAfter(asOf, &due); got != 90*time.Second {
		t.Fatalf("age=%s want 90s", got)
	}

	// Future send_after relative to asOf (should not happen for due rows) → clamp to 0.
	future := asOf.Add(time.Minute)
	if got := oldestDueAgeFromSendAfter(asOf, &future); got != 0 {
		t.Fatalf("negative age must clamp to 0, got %s", got)
	}

	// Exact asOf → age 0.
	eq := asOf
	if got := oldestDueAgeFromSendAfter(asOf, &eq); got != 0 {
		t.Fatalf("exact asOf age=%s want 0", got)
	}
}

func TestQueueMetricChannelFromDomain(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want MetricChannel
		ok   bool
	}{
		{NotifChannelLog, MetricChannelLog, true},
		{NotifChannelEmail, MetricChannelEmail, true},
		{NotifChannelSMS, MetricChannelSMS, true},
		{"FAX", "", false},
		{"log", "", false},
		{"", "", false},
		{"OTHER", "", false},
	}
	for _, tc := range cases {
		got, ok := QueueMetricChannelFromDomain(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("in=%q got=(%q,%v) want=(%q,%v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestMetricChannelFromDomainStillDropsSMS(t *testing.T) {
	t.Parallel()
	// 5C delivery map must remain unchanged (SMS not a delivery metric channel).
	if _, ok := MetricChannelFromDomain(NotifChannelSMS); ok {
		t.Fatal("5C MetricChannelFromDomain must still drop SMS")
	}
}

func TestNotificationStaleProcessingCanonical(t *testing.T) {
	t.Parallel()
	if NotificationStaleProcessing != 15*time.Minute {
		t.Fatalf("Stale=%s want 15m", NotificationStaleProcessing)
	}
}
