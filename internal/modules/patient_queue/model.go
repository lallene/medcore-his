package patient_queue

import "time"

// Appointment is the schedule source for queue check-in (no separate agenda module).
type Appointment struct {
	ID                uint       `gorm:"primaryKey" json:"id"`
	PatientID         uint       `gorm:"not null;index" json:"patientId"`
	ServiceID         uint       `gorm:"not null;index" json:"serviceId"`
	ExpectedDoctorID  *uint      `gorm:"index" json:"expectedDoctorId"`
	ScheduledAt       time.Time  `gorm:"not null;index" json:"scheduledAt"`
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
	StageReception         = "RECEPTION"
	StageWaitingTriage     = "WAITING_TRIAGE"
	StageTriageInProgress  = "TRIAGE_IN_PROGRESS"
	StageWaitingDoctor     = "WAITING_DOCTOR"
	StageDoctorInProgress  = "DOCTOR_IN_PROGRESS"
	StageCompleted         = "COMPLETED"
	StageCancelled         = "CANCELLED"
	StageNoShow            = "NO_SHOW"
	StageOnHold            = "ON_HOLD"
	StageRedirected        = "REDIRECTED"
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
