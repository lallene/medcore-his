package email

import "context"

// Result holds provider-neutral delivery metadata after a successful Send.
type Result struct {
	ProviderMessageID string
}

// Transport sends outbound email without exposing provider SDKs to callers.
//
// ProviderName identifies the implementation (e.g. "fake", "microsoft365") and
// must never include credentials or secrets.
type Transport interface {
	ProviderName() string
	Send(ctx context.Context, msg Message) (Result, error)
}
