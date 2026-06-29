package domain

import "time"

type DomainEvent interface {
	Name() string
	OccurredAt() time.Time
}
