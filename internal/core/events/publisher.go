package events

type Publisher interface {
	Publish(Event) error
}
