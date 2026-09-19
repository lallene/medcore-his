package email

import (
	"context"
	"sync"
)

// Fake is an in-memory Transport for tests (LOT 26D / future 26F).
//
// Exported (not _test.go only) so other packages can inject it without network.
// It is not intended for production delivery.
type Fake struct {
	mu sync.Mutex

	// Err, when non-nil, is returned from Send after context and Validate checks.
	Err error

	// NextProviderMessageID is returned on the next successful Send (then cleared).
	// When empty, successful sends use "fake".
	NextProviderMessageID string

	sent []Message
}

// NewFake returns a ready Fake Transport (ProviderName "fake").
func NewFake() *Fake {
	return &Fake{}
}

func (f *Fake) ProviderName() string { return "fake" }

// Send records msg on success. Honors ctx cancellation; validates msg;
// returns f.Err when set.
func (f *Fake) Send(ctx context.Context, msg Message) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if err := msg.Validate(); err != nil {
		return Result{}, err
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	if f.Err != nil {
		return Result{}, f.Err
	}

	id := f.NextProviderMessageID
	if id == "" {
		id = "fake"
	}
	f.NextProviderMessageID = ""
	f.sent = append(f.sent, msg)
	return Result{ProviderMessageID: id}, nil
}

// Sent returns a copy of successfully delivered messages (failed/cancelled sends omitted).
func (f *Fake) Sent() []Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Message, len(f.sent))
	copy(out, f.sent)
	return out
}

// Reset clears recorded messages and configured error / next message id.
func (f *Fake) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = nil
	f.Err = nil
	f.NextProviderMessageID = ""
}
