package audit

import "time"

type AuditEvent struct {
	Module   string
	Action   string
	RecordID uint

	UserID *uint
	IP     string
	Agent  string

	OldValue string
	NewValue string

	At time.Time
}

func (e AuditEvent) Name() string {
	return "audit.event"
}

func (e AuditEvent) OccurredAt() time.Time {
	if e.At.IsZero() {
		return time.Now()
	}

	return e.At
}
