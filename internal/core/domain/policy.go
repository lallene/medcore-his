package domain

type Policy[T any] interface {
	Allowed(actor any, resource T) bool
}

type PolicyFunc[T any] func(actor any, resource T) bool

func (f PolicyFunc[T]) Allowed(actor any, resource T) bool {
	return f(actor, resource)
}
