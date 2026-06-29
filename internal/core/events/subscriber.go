package events

type Handler interface {
	Handle(Event) error
}

type Subscriber interface {
	Subscribe(eventName string, handler Handler)
}
