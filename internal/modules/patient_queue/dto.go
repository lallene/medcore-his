package patient_queue

import "time"

type Access struct {
	UserID      uint
	ServiceID   *uint
	Permissions map[string]bool
}

func (a Access) Has(p string) bool {
	return a.Permissions["*"] || a.Permissions[p]
}

type CreateAppointmentRequest struct {
	PatientID        uint      `json:"patientId" binding:"required"`
	ServiceID        uint      `json:"serviceId" binding:"required"`
	ExpectedDoctorID *uint     `json:"expectedDoctorId"`
	ScheduledAt      time.Time `json:"scheduledAt" binding:"required"`
	Reason           string    `json:"reason"`
}

type WalkInCheckInRequest struct {
	PatientID          uint   `json:"patientId" binding:"required"`
	ServiceID          uint   `json:"serviceId" binding:"required"`
	ExpectedDoctorID   *uint  `json:"expectedDoctorId"`
	IdentityConfirmed  bool   `json:"identityConfirmed"`
	FinanceOverride    bool   `json:"financeOverride"`
	FinanceOverrideNote string `json:"financeOverrideNote"`
	Priority           string `json:"priority"`
	Reason             string `json:"reason"`
}

type AppointmentCheckInRequest struct {
	IdentityConfirmed   bool   `json:"identityConfirmed"`
	FinanceOverride     bool   `json:"financeOverride"`
	FinanceOverrideNote string `json:"financeOverrideNote"`
	Priority            string `json:"priority"`
}

type PriorityRequest struct {
	Priority string `json:"priority" binding:"required"`
	Reason   string `json:"reason"`
}

type CancelRequest struct {
	Reason string `json:"reason"`
}

type HoldRequest struct {
	Reason string `json:"reason"`
}

type CompleteTriageRequest struct {
	VitalSignsID *uint `json:"vitalSignsId"`
}

type TakeDoctorRequest struct {
	CreateConsultation bool `json:"createConsultation"`
}

type Filter struct {
	Search   string
	Stage    string
	Status   string
	Service  string
	Priority string
	Source   string
	Doctor   string
	Page     int
	Limit    int
}

type TicketDTO struct {
	Ticket
	PatientCode        string  `json:"patientCode"`
	PatientName        string  `json:"patientName"`
	ServiceName        string  `json:"serviceName"`
	ExpectedDoctorName string  `json:"expectedDoctorName"`
	WaitMinutes        int     `json:"waitMinutes"`
	Punctuality        string  `json:"punctuality,omitempty"`
	AppointmentTime    *string `json:"appointmentTime,omitempty"`
}

type AppointmentDTO struct {
	Appointment
	PatientCode        string `json:"patientCode"`
	PatientName        string `json:"patientName"`
	ServiceName        string `json:"serviceName"`
	ExpectedDoctorName string `json:"expectedDoctorName"`
	Punctuality        string `json:"punctuality,omitempty"`
	HasActiveTicket    bool   `json:"hasActiveTicket"`
}

type ListResponse struct {
	Items []TicketDTO `json:"items"`
	Total int64       `json:"total"`
	Page  int         `json:"page"`
	Limit int         `json:"limit"`
}

type AppointmentListResponse struct {
	Items []AppointmentDTO `json:"items"`
	Total int64            `json:"total"`
	Page  int              `json:"page"`
	Limit int              `json:"limit"`
}

type KPIs struct {
	ArrivedToday      int64   `json:"arrivedToday"`
	WaitingReception  int64   `json:"waitingReception"`
	WaitingTriage     int64   `json:"waitingTriage"`
	WaitingDoctor     int64   `json:"waitingDoctor"`
	InProgress        int64   `json:"inProgress"`
	CompletedToday    int64   `json:"completedToday"`
	AvgWaitMinutes    float64 `json:"avgWaitMinutes"`
	LateAppointments  int64   `json:"lateAppointments"`
}

type DetailResponse struct {
	Ticket  TicketDTO `json:"ticket"`
	History []History `json:"history"`
}
