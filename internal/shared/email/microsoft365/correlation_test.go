package microsoft365

import "testing"

func TestParseNotificationIntentID(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		key  string
		id   string
		ok   bool
	}{
		{"canonical", "notification-intent:42", "42", true},
		{"large", "notification-intent:18446744073709551615", "18446744073709551615", true}, // MaxUint64
		{"empty", "", "", false},
		{"unrelated", "intent-42-attempt-1", "", false},
		{"malformed_prefix", "notification-intent", "", false},
		{"empty_id", "notification-intent:", "", false},
		{"zero", "notification-intent:0", "", false},
		{"leading_zero", "notification-intent:042", "", false},
		{"negative", "notification-intent:-1", "", false},
		{"alpha", "notification-intent:4a2", "", false},
		{"extra_suffix", "notification-intent:42:extra", "", false},
		{"trailing_garbage", "notification-intent:42x", "", false},
		{"space_after_colon", "notification-intent: 42", "", false},
		{"trailing_space", "notification-intent:42 ", "", false},
		{"leading_space", " notification-intent:42", "", false},
		{"outer_whitespace", "  notification-intent:99  ", "", false},
		{"overflow", "notification-intent:18446744073709551616", "", false}, // MaxUint64+1
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			id, ok := parseNotificationIntentID(tc.key)
			if ok != tc.ok || id != tc.id {
				t.Fatalf("parse(%q)=(%q,%v) want (%q,%v)", tc.key, id, ok, tc.id, tc.ok)
			}
		})
	}
}
