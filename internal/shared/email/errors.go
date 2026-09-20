package email

import (
	"errors"
	"fmt"
	"time"
)

// Sentinel classifications for provider-neutral transport failures.
// Use errors.Is to classify; do not string-match provider responses here.
var (
	// ErrNotConfigured means no usable transport/credentials are available.
	ErrNotConfigured = errors.New("email: transport not configured")

	// ErrTransient means a later retry may succeed (network blip, throttle, etc.).
	// Retry policy belongs to the worker/reliability layer (LOT 26H), not here.
	ErrTransient = errors.New("email: transient failure")

	// ErrPermanent means retrying the same message is not expected to succeed
	// without changing inputs (invalid recipient, rejected content policy, etc.).
	ErrPermanent = errors.New("email: permanent failure")

	// ErrAmbiguousDelivery means MedCore cannot establish whether the provider
	// already accepted the outbound message (e.g. timeout after dispatch may
	// have reached the provider). Unlike ErrTransient, automatic retry risks
	// duplicate patient email and must be treated as terminal by the worker
	// (LOT 26H-2). This is not provider idempotency and does not imply
	// exactly-once delivery.
	ErrAmbiguousDelivery = errors.New("email: delivery outcome ambiguous")

	// ErrInvalidMessage means Message failed structural Validate().
	ErrInvalidMessage = errors.New("email: invalid message")
)

// Transient wraps cause as a classified transient failure.
func Transient(cause error) error {
	if cause == nil {
		return ErrTransient
	}
	return fmt.Errorf("%w: %w", ErrTransient, cause)
}

// Permanent wraps cause as a classified permanent failure.
func Permanent(cause error) error {
	if cause == nil {
		return ErrPermanent
	}
	return fmt.Errorf("%w: %w", ErrPermanent, cause)
}

// AmbiguousDelivery wraps cause as a classified ambiguous-delivery failure.
// Callers must not treat this as ErrTransient.
func AmbiguousDelivery(cause error) error {
	if cause == nil {
		return ErrAmbiguousDelivery
	}
	return fmt.Errorf("%w: %w", ErrAmbiguousDelivery, cause)
}

// NotConfigured wraps an optional cause as ErrNotConfigured.
func NotConfigured(cause error) error {
	if cause == nil {
		return ErrNotConfigured
	}
	return fmt.Errorf("%w: %w", ErrNotConfigured, cause)
}

// TransientRetryAfter wraps a transient failure with an optional provider retry
// delay floor (LOT 26H-4). The delay is a hint for the worker: do not retry
// sooner than this duration. It does not replace MedCore backoff policy.
// after <= 0 yields a plain Transient error (no hint).
func TransientRetryAfter(cause error, after time.Duration) error {
	base := Transient(cause)
	if after <= 0 {
		return base
	}
	return &retryAfterError{err: base, after: after}
}

type retryAfterError struct {
	err   error
	after time.Duration
}

func (e *retryAfterError) Error() string { return e.err.Error() }
func (e *retryAfterError) Unwrap() error { return e.err }
func (e *retryAfterError) RetryAfter() time.Duration {
	if e == nil {
		return 0
	}
	return e.after
}

// RetryAfter extracts a positive provider retry-delay hint from err, if present.
func RetryAfter(err error) (time.Duration, bool) {
	var ra interface{ RetryAfter() time.Duration }
	if !errors.As(err, &ra) {
		return 0, false
	}
	d := ra.RetryAfter()
	if d <= 0 {
		return 0, false
	}
	return d, true
}
