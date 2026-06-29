package domain

type AggregateRoot struct {
	events []DomainEvent
}

func (a *AggregateRoot) AddEvent(event DomainEvent) {
	a.events = append(a.events, event)
}

func (a *AggregateRoot) DomainEvents() []DomainEvent {
	return a.events
}

func (a *AggregateRoot) ClearEvents() {
	a.events = []DomainEvent{}
}
