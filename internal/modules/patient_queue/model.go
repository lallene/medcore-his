package patient_queue

import "time"

// AppointmentType is a scheduling catalog entry (duration / code), not a clinical reason.
// Distinct from consultations.ConsultationReason.
type AppointmentType struct {
	ID                     uint      `gorm:"primaryKey" json:"id"`
	Code                   string    `gorm:"size:64;not null;uniqueIndex" json:"code"`
	Name                   string    `gorm:"size:160;not null" json:"name"`
	DefaultDurationMinutes int       `gorm:"column:default_duration_minutes;not null" json:"defaultDurationMinutes"` // must be > 0
	ServiceID              *uint     `gorm:"index" json:"serviceId,omitempty"`                                       // optional service constraint
	Active                 bool      `gorm:"not null;default:true;index" json:"active"`
	CreatedAt              time.Time `gorm:"not null" json:"createdAt"`
	UpdatedAt              time.Time `gorm:"not null" json:"updatedAt"`
}

func (AppointmentType) TableName() string { return "patient_queue_appointment_types" }

// Appointment is the canonical medical appointment row (LOT 23 Option A).
// It is the schedule source for queue check-in — there is no second appointments table.
// Timing: ScheduledAt = start (inclusive); ScheduledEndAt = end (exclusive) half-open [start, end).
// ScheduledEndAt may be nil on legacy rows; new creates should set it when duration/type is known.
type Appointment struct {
	ID                uint       `gorm:"primaryKey" json:"id"`
	PatientID         uint       `gorm:"not null;index" json:"patientId"`
	ServiceID         uint       `gorm:"not null;index" json:"serviceId"`
	ExpectedDoctorID  *uint      `gorm:"index" json:"expectedDoctorId"` // scheduled practitioner = users.id
	AppointmentTypeID *uint      `gorm:"index" json:"appointmentTypeId,omitempty"`
	ScheduledAt       time.Time  `gorm:"not null;index" json:"scheduledAt"`     // start inclusive
	ScheduledEndAt    *time.Time `gorm:"index" json:"scheduledEndAt,omitempty"` // end exclusive; nil = legacy
	Reason            string     `gorm:"size:200" json:"reason"`
	Status            string     `gorm:"size:24;not null;index" json:"status"`
	ArrivedAt         *time.Time `json:"arrivedAt"`
	CheckedInAt       *time.Time `json:"checkedInAt"`
	QueueTicketID     *uint      `gorm:"index" json:"queueTicketId"`
	CreatedBy         uint       `gorm:"not null" json:"createdBy"`
	CreatedAt         time.Time  `gorm:"not null" json:"createdAt"`
	UpdatedAt         time.Time  `gorm:"not null" json:"updatedAt"`
}

func (Appointment) TableName() string { return "patient_queue_appointments" }

// AppointmentHistory is an append-only audit log for scheduling lifecycle events.
// Distinct from patient_queue_history (ticket operational transitions).
type AppointmentHistory struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	AppointmentID uint      `gorm:"not null;index" json:"appointmentId"`
	ActorUserID   uint      `gorm:"not null;index" json:"actorUserId"`
	EventType     string    `gorm:"size:40;not null;index" json:"eventType"`
	FromStatus    string    `gorm:"size:24" json:"fromStatus"`
	ToStatus      string    `gorm:"size:24" json:"toStatus"`
	Reason        string    `gorm:"type:text" json:"reason"`
	Payload       string    `gorm:"type:text" json:"payload"` // optional JSON context
	CreatedAt     time.Time `gorm:"not null;index" json:"createdAt"`
}

func (AppointmentHistory) TableName() string { return "patient_queue_appointment_history" }

// Ticket is the active clinical arrival journey after validated check-in.
type Ticket struct {
	ID                  uint       `gorm:"primaryKey" json:"id"`
	Reference           string     `gorm:"size:32;not null;uniqueIndex" json:"reference"`
	PatientID           uint       `gorm:"not null;index" json:"patientId"`
	AppointmentID       *uint      `gorm:"index" json:"appointmentId"`
	Source              string     `gorm:"size:16;not null;index" json:"source"` // APPOINTMENT | WALK_IN
	ServiceID           uint       `gorm:"not null;index" json:"serviceId"`
	ExpectedDoctorID    *uint      `gorm:"index" json:"expectedDoctorId"`
	ArrivedAt           time.Time  `gorm:"not null;index" json:"arrivedAt"`
	CheckedInAt         time.Time  `gorm:"not null;index" json:"checkedInAt"`
	Stage               string     `gorm:"size:32;not null;index" json:"stage"`
	Status              string     `gorm:"size:24;not null;index" json:"status"` // ACTIVE | CANCELLED | COMPLETED | NO_SHOW | ON_HOLD
	Priority            string     `gorm:"size:16;not null;index" json:"priority"`
	FinanceStatus       string     `gorm:"size:24;not null;index" json:"financeStatus"`
	FinanceOverride     bool       `gorm:"not null;default:false" json:"financeOverride"`
	FinanceOverrideNote string     `gorm:"type:text" json:"financeOverrideNote"`
	IdentityConfirmed   bool       `gorm:"not null;default:false" json:"identityConfirmed"`
	TriageTakenBy       *uint      `gorm:"index" json:"triageTakenBy"`
	TriageTakenAt       *time.Time `json:"triageTakenAt"`
	TriageCompletedBy   *uint      `gorm:"index" json:"triageCompletedBy"`
	TriageCompletedAt   *time.Time `json:"triageCompletedAt"`
	DoctorTakenBy       *uint      `gorm:"index" json:"doctorTakenBy"`
	DoctorTakenAt       *time.Time `json:"doctorTakenAt"`
	ConsultationID      *uint      `gorm:"index" json:"consultationId"`
	VitalSignsID        *uint      `gorm:"index" json:"vitalSignsId"`
	Version             int        `gorm:"not null;default:1" json:"version"`
	CreatedBy           uint       `gorm:"not null;index" json:"createdBy"`
	CreatedAt           time.Time  `gorm:"not null;index" json:"createdAt"`
	UpdatedAt           time.Time  `gorm:"not null" json:"updatedAt"`
}

func (Ticket) TableName() string { return "patient_queue_tickets" }

type History struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	TicketID    uint      `gorm:"not null;index" json:"ticketId"`
	ActorUserID uint      `gorm:"not null;index" json:"actorUserID"`
	FromStage   string    `gorm:"size:32" json:"fromStage"`
	ToStage     string    `gorm:"size:32" json:"toStage"`
	EventType   string    `gorm:"size:40;not null;index" json:"eventType"`
	Reason      string    `gorm:"type:text" json:"reason"`
	CreatedAt   time.Time `gorm:"not null;index" json:"createdAt"`
}

func (History) TableName() string { return "patient_queue_history" }

// Stages
const (
	StageReception        = "RECEPTION"
	StageWaitingTriage    = "WAITING_TRIAGE"
	StageTriageInProgress = "TRIAGE_IN_PROGRESS"
	StageWaitingDoctor    = "WAITING_DOCTOR"
	StageDoctorInProgress = "DOCTOR_IN_PROGRESS"
	StageCompleted        = "COMPLETED"
	StageCancelled        = "CANCELLED"
	StageNoShow           = "NO_SHOW"
	StageOnHold           = "ON_HOLD"
	StageRedirected       = "REDIRECTED"
)

const (
	StatusActive    = "ACTIVE"
	StatusCancelled = "CANCELLED"
	StatusCompleted = "COMPLETED"
	StatusNoShow    = "NO_SHOW"
	StatusOnHold    = "ON_HOLD"
)

const (
	SourceAppointment = "APPOINTMENT"
	SourceWalkIn      = "WALK_IN"
)

const (
	ApptScheduled  = "SCHEDULED"
	ApptArrived    = "ARRIVED"
	ApptCheckedIn  = "CHECKED_IN"
	ApptInProgress = "IN_PROGRESS"
	ApptCompleted  = "COMPLETED"
	ApptCancelled  = "CANCELLED"
	ApptNoShow     = "NO_SHOW"
)

// Appointment history event types (append-only scheduling audit).
const (
	ApptHistCreated             = "CREATED"
	ApptHistConfirmed           = "CONFIRMED"
	ApptHistRescheduled         = "RESCHEDULED"
	ApptHistPractitionerChanged = "PRACTITIONER_CHANGED"
	ApptHistCancelled           = "CANCELLED"
	ApptHistCheckedIn           = "CHECKED_IN"
	ApptHistNoShow              = "NO_SHOW"
	ApptHistInProgress          = "IN_PROGRESS"
	ApptHistCompleted           = "COMPLETED"
)

const (
	PriorityUrgent = "URGENT"
	PriorityHigh   = "HIGH"
	PriorityNormal = "NORMAL"
	PriorityLow    = "LOW"
)

const (
	FinanceClear            = "CLEAR"
	FinancePaymentRequired  = "PAYMENT_REQUIRED"
	FinanceInsurancePending = "INSURANCE_PENDING"
	FinanceExempt           = "EXEMPT"
	FinanceBlocked          = "BLOCKED"
)

// Punctuality relative to appointment window (±15 min default).
const (
	PunctualEarly  = "EARLY"
	PunctualOnTime = "ON_TIME"
	PunctualLate   = "LATE"
)

var AppointmentWindow = 15 * time.Minute
