package billing

import "testing"

func TestNormalizePaymentIdempotencyKey(t *testing.T) {
	t.Parallel()
	if _, e := NormalizePaymentIdempotencyKey(""); !isBadRequest(e) {
		t.Fatalf("empty key: %v", e)
	}
	long := make([]byte, MaxPaymentIdempotencyKeyLen+1)
	for i := range long {
		long[i] = 'a'
	}
	if _, e := NormalizePaymentIdempotencyKey(string(long)); !isBadRequest(e) {
		t.Fatalf("long key: %v", e)
	}
	key, e := NormalizePaymentIdempotencyKey("  ok-key  ")
	if e != nil || key != "ok-key" {
		t.Fatalf("trim: %q %v", key, e)
	}
}
