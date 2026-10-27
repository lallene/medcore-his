package events

import (
	"testing"
	"time"
)

type testEvent struct {
	name string
}

func (e testEvent) Name() string { return e.name }

func (e testEvent) OccurredAt() time.Time { return time.Unix(0, 0).UTC() }

type fnHandler struct {
	fn func(Event) error
}

func (h fnHandler) Handle(event Event) error { return h.fn(event) }

func TestPublishRecoversPanickingHandler(t *testing.T) {
	bus := NewMemoryBus()
	bus.Subscribe("audit.event", fnHandler{fn: func(Event) error {
		panic("subscriber boom")
	}})
	done := make(chan struct{})
	bus.Subscribe("audit.event", fnHandler{fn: func(Event) error {
		close(done)
		return nil
	}})

	if err := bus.Publish(testEvent{name: "audit.event"}); err != nil {
		t.Fatal(err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("a panicking subscriber stopped the other handler")
	}
}
