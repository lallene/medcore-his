package audit

import (
	"github.com/lallene/medcore-his/backend/internal/core/events"
	"gorm.io/gorm"
)

type Listener struct {
	db *gorm.DB
}

func NewListener(db *gorm.DB) Listener {
	return Listener{db: db}
}

func (l Listener) Handle(e events.Event) error {
	event, ok := e.(AuditEvent)

	if !ok {
		return nil
	}

	log := AuditLog{
		Module:     event.Module,
		Action:     event.Action,
		RecordID:   event.RecordID,
		UserID:     event.UserID,
		IP:         event.IP,
		Agent:      event.Agent,
		OldValue:   event.OldValue,
		NewValue:   event.NewValue,
		OccurredAt: event.OccurredAt(),
	}

	if err := l.db.Create(&log).Error; err != nil {
		return err
	}

	return nil
}
