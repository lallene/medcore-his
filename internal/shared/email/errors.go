package email

import (
	"errors"
	"fmt"
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

// NotConfigured wraps an optional cause as ErrNotConfigured.
func NotConfigured(cause error) error {
	if cause == nil {
		return ErrNotConfigured
	}
	return fmt.Errorf("%w: %w", ErrNotConfigured, cause)
}
