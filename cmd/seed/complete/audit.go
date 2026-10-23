package complete

import (
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/lallene/medcore-his/backend/internal/core/audit"
	"github.com/lallene/medcore-his/backend/internal/core/events"
)

// auditSink replaces the API's audit listener for the seed run. The event bus delivers
// asynchronously (one goroutine per handler); the sink stamps created_at with the event
// instant (inside the step window) and lets Flush wait for in-flight writes, so audit rows
// are re-anchored like every other row.
type auditSink struct {
	db      *gorm.DB
	pending sync.WaitGroup
}

var registerAuditOnce sync.Once
var sink = &auditSink{}

func registerAudit(db *gorm.DB) *auditSink {
	sink.db = db
	registerAuditOnce.Do(func() {
		events.DefaultBus.Subscribe("audit.event", sink)
	})
	return sink
}

func (s *auditSink) Handle(e events.Event) error {
	ev, ok := e.(audit.AuditEvent)
	if !ok || s.db == nil {
		return nil
	}
	s.pending.Add(1)
	defer s.pending.Done()
	at := ev.OccurredAt()
	row := audit.AuditLog{Module: ev.Module, Action: ev.Action, RecordID: ev.RecordID, UserID: ev.UserID, IP: ev.IP, Agent: ev.Agent, OldValue: ev.OldValue, NewValue: ev.NewValue, OccurredAt: at}
	row.CreatedAt, row.UpdatedAt = at, at
	return s.db.Create(&row).Error
}

// settle gives in-flight asynchronous handlers the time to start, then waits for them.
func (s *auditSink) settle() {
	time.Sleep(15 * time.Millisecond)
	s.pending.Wait()
}
