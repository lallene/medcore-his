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
	PatientID         uint       `json:"patientId" binding:"required"`
	ServiceID         uint       `json:"serviceId" binding:"required"`
	ExpectedDoctorID  *uint      `json:"expectedDoctorId"`
	AppointmentTypeID *uint      `json:"appointmentTypeId"`
	ScheduledAt       time.Time  `json:"scheduledAt" binding:"required"`
	ScheduledEndAt    *time.Time `json:"scheduledEndAt"` // optional; derived from type duration when omitted
	Reason            string     `json:"reason"`
}

type CreateAppointmentTypeRequest struct {
	Code                   string `json:"code" binding:"required"`
	Name                   string `json:"name" binding:"required"`
	DefaultDurationMinutes int    `json:"defaultDurationMinutes" binding:"required"`
	ServiceID              *uint  `json:"serviceId"`
	Active                 *bool  `json:"active"`
}

type WalkInCheckInRequest struct {
	PatientID           uint   `json:"patientId" binding:"required"`
	ServiceID           uint   `json:"serviceId" binding:"required"`
	ExpectedDoctorID    *uint  `json:"expectedDoctorId"`
	IdentityConfirmed   bool   `json:"identityConfirmed"`
	FinanceOverride     bool   `json:"financeOverride"`
	FinanceOverrideNote string `json:"financeOverrideNote"`
	Priority            string `json:"priority"`
	Reason              string `json:"reason"`
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

// CompleteRequest — clôture médecin (décision médicale optionnelle, stockée sur SOAP existant).
type CompleteRequest struct {
	Disposition     string `json:"disposition"`
	DispositionNote string `json:"dispositionNote"`
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

// VitalSummary is a read-only projection of existing vital_signs (no duplication).
type VitalSummary struct {
	ID               uint     `json:"id"`
	TemperatureC     *float64 `json:"temperatureC,omitempty"`
	SystolicBP       *int     `json:"systolicBp,omitempty"`
	DiastolicBP      *int     `json:"diastolicBp,omitempty"`
	HeartRate        *int     `json:"heartRate,omitempty"`
	OxygenSaturation *float64 `json:"oxygenSaturation,omitempty"`
	WeightKg         *float64 `json:"weightKg,omitempty"`
	HeightCm         *float64 `json:"heightCm,omitempty"`
	MeasuredAt       *string  `json:"measuredAt,omitempty"`
	AbnormalTemp     bool     `json:"abnormalTemp"`
	AbnormalBP       bool     `json:"abnormalBp"`
	AbnormalHR       bool     `json:"abnormalHr"`
	AbnormalSpO2     bool     `json:"abnormalSpo2"`
}

type ClinicalSnippet struct {
	Label    string `json:"label"`
	Severity string `json:"severity,omitempty"`
}

type TicketDTO struct {
	Ticket
	PatientCode        string        `json:"patientCode"`
	PatientName        string        `json:"patientName"`
	PatientSex         string        `json:"patientSex,omitempty"`
	PatientAgeYears    *int          `json:"patientAgeYears,omitempty"`
	PatientDob         *string       `json:"patientDob,omitempty"`
	PatientPhone       string        `json:"patientPhone,omitempty"`
	ServiceName        string        `json:"serviceName"`
	ExpectedDoctorName string        `json:"expectedDoctorName"`
	DoctorTakenByName  string        `json:"doctorTakenByName,omitempty"`
	Reason             string        `json:"reason,omitempty"`
	WaitMinutes        int           `json:"waitMinutes"`
	Punctuality        string        `json:"punctuality,omitempty"`
	AppointmentTime    *string       `json:"appointmentTime,omitempty"`
	VitalSigns         *VitalSummary `json:"vitalSigns,omitempty"`
}

type DoctorWorklistKPIs struct {
	ToTreat                int64   `json:"toTreat"`
	Urgent                 int64   `json:"urgent"`
	InConsultation         int64   `json:"inConsultation"`
	AvgWaitMinutes         float64 `json:"avgWaitMinutes"`
	CompletedToday         int64   `json:"completedToday"`
	AvgConsultationMinutes float64 `json:"avgConsultationMinutes"`
	LastCompletedAt        *string `json:"lastCompletedAt,omitempty"`
}

type DoctorWorklistResponse struct {
	Items []TicketDTO        `json:"items"`
	Total int64              `json:"total"`
	Page  int                `json:"page"`
	Limit int                `json:"limit"`
	KPIs  DoctorWorklistKPIs `json:"kpis"`
}

type AppointmentDTO struct {
	Appointment
	PatientCode         string `json:"patientCode"`
	PatientName         string `json:"patientName"`
	ServiceName         string `json:"serviceName"`
	ExpectedDoctorName  string `json:"expectedDoctorName"`
	AppointmentTypeCode string `json:"appointmentTypeCode,omitempty"`
	AppointmentTypeName string `json:"appointmentTypeName,omitempty"`
	DurationMinutes     int    `json:"durationMinutes,omitempty"`
	Punctuality         string `json:"punctuality,omitempty"`
	HasActiveTicket     bool   `json:"hasActiveTicket"`
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
	ArrivedToday     int64   `json:"arrivedToday"`
	WaitingReception int64   `json:"waitingReception"`
	WaitingTriage    int64   `json:"waitingTriage"`
	WaitingDoctor    int64   `json:"waitingDoctor"`
	InProgress       int64   `json:"inProgress"`
	CompletedToday   int64   `json:"completedToday"`
	AvgWaitMinutes   float64 `json:"avgWaitMinutes"`
	LateAppointments int64   `json:"lateAppointments"`
}

type DetailResponse struct {
	Ticket    TicketDTO         `json:"ticket"`
	History   []History         `json:"history"`
	Allergies []ClinicalSnippet `json:"allergies,omitempty"`
	Histories []ClinicalSnippet `json:"histories,omitempty"`
}
