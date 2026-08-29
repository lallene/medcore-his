package patient_queue

import "time"

// StaffWorkingSchedule is a recurring weekly wall-clock rule (local business time).
// Not a calendar date row. Weekday uses Go time.Weekday (0=Sunday … 6=Saturday).
// StartTime/EndTime are local wall-clock TIME strings "HH:MM:SS" (not UTC instants).
type StaffWorkingSchedule struct {
	ID             uint       `gorm:"primaryKey" json:"id"`
	PractitionerID uint       `gorm:"not null;index" json:"practitionerId"` // users.id
	ServiceID      uint       `gorm:"not null;index" json:"serviceId"`
	Weekday        int        `gorm:"not null;index" json:"weekday"`     // 0–6 = time.Weekday
	StartTime      string     `gorm:"type:time without time zone;not null" json:"startTime"` // HH:MM:SS local wall-clock
	EndTime        string     `gorm:"type:time without time zone;not null" json:"endTime"`   // HH:MM:SS local, exclusive [start,end)
	ValidFrom      time.Time  `gorm:"type:date;not null;index" json:"validFrom"`             // date inclusive
	ValidUntil     *time.Time `gorm:"type:date;index" json:"validUntil,omitempty"`           // date inclusive; nil = open-ended
	Active         bool       `gorm:"not null;default:true;index" json:"active"`
	CreatedBy      uint       `gorm:"not null" json:"createdBy"`
	CreatedAt      time.Time  `gorm:"not null" json:"createdAt"`
	UpdatedAt      time.Time  `gorm:"not null" json:"updatedAt"`
}

func (StaffWorkingSchedule) TableName() string { return "patient_queue_staff_working_schedules" }

// Schedule exception types (concrete timestamps, IANA-aware at application layer).
const (
	ExAbsence           = "ABSENCE"
	ExLeave             = "LEAVE"
	ExMeeting           = "MEETING"
	ExBlocked           = "BLOCKED"
	ExTraining          = "TRAINING"
	ExOther             = "OTHER"
	ExExtraAvailability = "EXTRA_AVAILABILITY"
)

// ScheduleException is a date-specific override (not recurring).
// Negative types remove capacity; EXTRA_AVAILABILITY adds capacity.
// Precedence for LOT 23C: negative exceptions win over positive ones.
type ScheduleException struct {
	ID             uint       `gorm:"primaryKey" json:"id"`
	PractitionerID uint       `gorm:"not null;index" json:"practitionerId"` // users.id
	ServiceID      uint       `gorm:"not null;index" json:"serviceId"`
	Type           string     `gorm:"size:40;not null;index" json:"type"`
	StartAt        time.Time  `gorm:"not null;index" json:"startAt"` // UTC timestamptz
	EndAt          time.Time  `gorm:"not null;index" json:"endAt"`   // exclusive UTC
	Reason         string     `gorm:"type:text" json:"reason"`
	Active         bool       `gorm:"not null;default:true;index" json:"active"`
	CancelledAt    *time.Time `json:"cancelledAt,omitempty"`
	CreatedBy      uint       `gorm:"not null" json:"createdBy"`
	CreatedAt      time.Time  `gorm:"not null" json:"createdAt"`
	UpdatedAt      time.Time  `gorm:"not null" json:"updatedAt"`
}

func (ScheduleException) TableName() string { return "patient_queue_schedule_exceptions" }

func IsNegativeException(t string) bool {
	return t != ExExtraAvailability
}

func IsPositiveException(t string) bool {
	return t == ExExtraAvailability
}

// ScheduleAuditEvent is append-only administration audit (not appointment history).
type ScheduleAuditEvent struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	ActorUserID    uint      `gorm:"not null;index" json:"actorUserId"`
	EventType      string    `gorm:"size:40;not null;index" json:"eventType"`
	EntityType     string    `gorm:"size:40;not null;index" json:"entityType"` // SCHEDULE | EXCEPTION
	EntityID       uint      `gorm:"not null;index" json:"entityId"`
	PractitionerID uint      `gorm:"index" json:"practitionerId"`
	ServiceID      uint      `gorm:"index" json:"serviceId"`
	Reason         string    `gorm:"type:text" json:"reason"`
	Payload        string    `gorm:"type:text" json:"payload"`
	CreatedAt      time.Time `gorm:"not null;index" json:"createdAt"`
}

func (ScheduleAuditEvent) TableName() string { return "patient_queue_schedule_audit" }

const (
	SchedAuditCreated          = "SCHEDULE_CREATED"
	SchedAuditUpdated          = "SCHEDULE_UPDATED"
	SchedAuditDisabled         = "SCHEDULE_DISABLED"
	ExAuditCreated             = "EXCEPTION_CREATED"
	ExAuditUpdated             = "EXCEPTION_UPDATED"
	ExAuditCancelled           = "EXCEPTION_CANCELLED"
	EntitySchedule             = "SCHEDULE"
	EntityException            = "EXCEPTION"
)
