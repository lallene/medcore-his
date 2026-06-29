package audit

import (
	"github.com/lallene/medcore-his/backend/internal/core/events"
	"gorm.io/gorm"
)

func Register(db *gorm.DB) {
	events.DefaultBus.Subscribe(
		"audit.event",
		NewListener(db),
	)
}
