package ticketing

type CreateRequest struct {
	Type              string `json:"type" binding:"required"`
	CategoryCode      string `json:"categoryCode"`
	Subcategory       string `json:"subcategory"`
	Title             string `json:"title" binding:"required"`
	Description       string `json:"description" binding:"required"`
	Impact            string `json:"impact"`
	Urgency           string `json:"urgency"`
	ApplicationModule string `json:"applicationModule"`
	PageURL           string `json:"pageUrl"`
	RequestID         string `json:"requestId"`
	FrontendVersion   string `json:"frontendVersion"`
	PatientRef        string `json:"patientRef"`
	EncounterRef      string `json:"encounterRef"`
}
type UpdateRequest struct {
	CategoryCode *string `json:"categoryCode"`
	Subcategory  *string `json:"subcategory"`
	Impact       *string `json:"impact"`
	Urgency      *string `json:"urgency"`
	Priority     *string `json:"priority"`
}
type AssignRequest struct {
	UserID *uint  `json:"userId"`
	Queue  string `json:"queue"`
}
type WorkflowRequest struct {
	Status            string `json:"status" binding:"required"`
	ResolutionSummary string `json:"resolutionSummary"`
	ResolutionCode    string `json:"resolutionCode"`
}
type CommentRequest struct {
	Content    string `json:"content" binding:"required"`
	Visibility string `json:"visibility"`
}
type Filter struct {
	Search, Status, Priority, Type, Category, Service, Assigned, Requester, SLABreached string
	Page, Limit                                                                         int
}
type Page struct {
	Items      []TicketView `json:"items"`
	Page       int          `json:"page"`
	Limit      int          `json:"limit"`
	Total      int64        `json:"total"`
	TotalPages int          `json:"totalPages"`
}
type TicketView struct {
	Ticket
	RequesterName         string `json:"requesterName"`
	AssignedName          string `json:"assignedName"`
	ServiceName           string `json:"serviceName"`
	ResponseSLABreached   bool   `json:"responseSlaBreached"`
	ResolutionSLABreached bool   `json:"resolutionSlaBreached"`
}
type Detail struct {
	TicketView
	Comments    []Comment    `json:"comments"`
	Attachments []Attachment `json:"attachments"`
	History     []History    `json:"history"`
	Assignments []Assignment `json:"assignments"`
}
type KPIs struct {
	Open                        int64   `json:"open"`
	NewToday                    int64   `json:"newToday"`
	P1P2                        int64   `json:"p1p2"`
	SLABreached                 int64   `json:"slaBreached"`
	Resolved                    int64   `json:"resolved"`
	Reopened                    int64   `json:"reopened"`
	AverageFirstResponseMinutes float64 `json:"averageFirstResponseMinutes"`
	MTTRMinutes                 float64 `json:"mttrMinutes"`
}
type AgentOption struct {
	UserID      uint   `json:"userId"`
	Name        string `json:"name"`
	ServiceName string `json:"serviceName"`
}
