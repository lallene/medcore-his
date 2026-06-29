package domain

type Specification[T any] interface {
	IsSatisfiedBy(candidate T) bool
}

type SpecificationFunc[T any] func(candidate T) bool

func (f SpecificationFunc[T]) IsSatisfiedBy(candidate T) bool {
	return f(candidate)
}

func And[T any](left Specification[T], right Specification[T]) Specification[T] {
	return SpecificationFunc[T](func(candidate T) bool {
		return left.IsSatisfiedBy(candidate) && right.IsSatisfiedBy(candidate)
	})
}

func Or[T any](left Specification[T], right Specification[T]) Specification[T] {
	return SpecificationFunc[T](func(candidate T) bool {
		return left.IsSatisfiedBy(candidate) || right.IsSatisfiedBy(candidate)
	})
}

func Not[T any](spec Specification[T]) Specification[T] {
	return SpecificationFunc[T](func(candidate T) bool {
		return !spec.IsSatisfiedBy(candidate)
	})
}
