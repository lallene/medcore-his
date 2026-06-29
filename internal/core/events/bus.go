package events

type Bus interface {
	Publisher
	Subscriber
}
