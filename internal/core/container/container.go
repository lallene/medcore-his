package container

import (
	"fmt"
	"reflect"
	"sync"
)

type Factory func(c *Container) (any, error)

type binding struct {
	factory   Factory
	instance  any
	singleton bool
	resolved  bool
}

type Container struct {
	bindings map[reflect.Type]*binding
	mu       sync.RWMutex
}

func New() *Container {
	return &Container{
		bindings: make(map[reflect.Type]*binding),
	}
}

func Singleton[T any](c *Container, factory func(c *Container) (T, error)) {
	c.mu.Lock()
	defer c.mu.Unlock()

	t := typeOf[T]()

	c.bindings[t] = &binding{
		singleton: true,
		factory: func(c *Container) (any, error) {
			return factory(c)
		},
	}
}

func FactoryBind[T any](c *Container, factory func(c *Container) (T, error)) {
	c.mu.Lock()
	defer c.mu.Unlock()

	t := typeOf[T]()

	c.bindings[t] = &binding{
		singleton: false,
		factory: func(c *Container) (any, error) {
			return factory(c)
		},
	}
}

func Instance[T any](c *Container, service T) {
	c.mu.Lock()
	defer c.mu.Unlock()

	t := typeOf[T]()

	c.bindings[t] = &binding{
		instance:  service,
		singleton: true,
		resolved:  true,
	}
}

func Make[T any](c *Container) (T, error) {
	t := typeOf[T]()

	c.mu.RLock()
	b, ok := c.bindings[t]
	c.mu.RUnlock()

	if !ok {
		var zero T
		return zero, fmt.Errorf("service non enregistré: %s", t.String())
	}

	if b.singleton && b.resolved {
		resolved, ok := b.instance.(T)

		if !ok {
			var zero T
			return zero, fmt.Errorf("service invalide: %s", t.String())
		}

		return resolved, nil
	}

	service, err := b.factory(c)

	if err != nil {
		var zero T
		return zero, err
	}

	resolved, ok := service.(T)

	if !ok {
		var zero T
		return zero, fmt.Errorf("factory invalide pour: %s", t.String())
	}

	if b.singleton {
		c.mu.Lock()
		b.instance = resolved
		b.resolved = true
		c.mu.Unlock()
	}

	return resolved, nil
}

func MustMake[T any](c *Container) T {
	service, err := Make[T](c)

	if err != nil {
		panic(err)
	}

	return service
}

func typeOf[T any]() reflect.Type {
	return reflect.TypeOf((*T)(nil)).Elem()
}
