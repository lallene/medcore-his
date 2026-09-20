package microsoft365

import (
	"strconv"
	"strings"
)

// Stable Graph internet message header for MedCore notification correlation.
// Not a Graph idempotency / duplicate-suppression primitive (LOT 26H-1 / 26H-3).
const notificationIntentHeaderName = "x-medcore-notification-intent-id"

const notificationIntentKeyPrefix = "notification-intent:"

// parseNotificationIntentID derives the opaque intent id from email.Message.IdempotencyKey
// when it matches the exact canonical form "notification-intent:<positive integer>".
//
// No leading/trailing whitespace, no spaces around the id, no leading zeros.
// Non-canonical keys return ok=false without error so unrelated emails stay valid.
func parseNotificationIntentID(idempotencyKey string) (id string, ok bool) {
	if !strings.HasPrefix(idempotencyKey, notificationIntentKeyPrefix) {
		return "", false
	}
	rest := strings.TrimPrefix(idempotencyKey, notificationIntentKeyPrefix)
	if rest == "" {
		return "", false
	}
	// Digits only; reject signs, whitespace, decimals, and leading zeros (including "0").
	if rest[0] == '0' {
		return "", false
	}
	for _, r := range rest {
		if r < '0' || r > '9' {
			return "", false
		}
	}
	n, err := strconv.ParseUint(rest, 10, 64)
	if err != nil || n == 0 {
		return "", false
	}
	return strconv.FormatUint(n, 10), true
}
