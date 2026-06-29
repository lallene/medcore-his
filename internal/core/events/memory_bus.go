package events

import "sync"

type MemoryBus struct {
	mu       sync.RWMutex
	handlers map[string][]Handler
}

func NewMemoryBus() *MemoryBus {
	return &MemoryBus{
		handlers: make(map[string][]Handler),
	}
}

func (b *MemoryBus) Subscribe(event string, handler Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.handlers[event] = append(b.handlers[event], handler)
}

func (b *MemoryBus) Publish(event Event) error {
	b.mu.RLock()

	handlers := b.handlers[event.Name()]

	b.mu.RUnlock()

	for _, h := range handlers {

		go func(handler Handler) {
			_ = handler.Handle(event)
		}(h)

	}

	return nil
}
