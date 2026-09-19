package email

import (
	"fmt"
	"net/mail"
	"strings"
)

// Address is a single outbound recipient (provider-neutral).
// Address must be an addr-spec only (e.g. "john@example.com").
// Display names belong in DisplayName, never inside Address.
type Address struct {
	Address     string
	DisplayName string
}

// Message is a pure outbound email value. It must not carry clinical domain
// models, notification payloads, credentials, or provider-specific fields.
//
// IdempotencyKey may be used by providers for correlation or deduplication when
// supported. It does not guarantee exactly-once delivery.
type Message struct {
	To             Address
	Subject        string
	TextBody       string
	HTMLBody       string
	IdempotencyKey string
}

// Validate checks transport-independent structural invariants.
//
// Rules (minimal; not a full RFC5322 framework):
//   - To.Address is a non-empty addr-spec only (no mailbox/display-name form);
//     DisplayName is independent and is not parsed as part of the address
//   - subject non-empty
//   - at least one of TextBody or HTMLBody non-empty
//
// Leading/trailing whitespace on To.Address is tolerated for the check
// (compared against the trimmed value) but Message is never mutated.
//
// Callers and Transport implementations should validate before/at Send.
// This package does not inspect content for PHI.
func (m Message) Validate() error {
	raw := strings.TrimSpace(m.To.Address)
	if raw == "" {
		return fmt.Errorf("%w: recipient address required", ErrInvalidMessage)
	}
	parsed, err := mail.ParseAddress(raw)
	if err != nil || parsed.Name != "" || parsed.Address != raw {
		return fmt.Errorf("%w: recipient address invalid", ErrInvalidMessage)
	}
	if strings.TrimSpace(m.Subject) == "" {
		return fmt.Errorf("%w: subject required", ErrInvalidMessage)
	}
	if strings.TrimSpace(m.TextBody) == "" && strings.TrimSpace(m.HTMLBody) == "" {
		return fmt.Errorf("%w: text or HTML body required", ErrInvalidMessage)
	}
	return nil
}
