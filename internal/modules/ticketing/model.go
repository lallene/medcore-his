package ticketing

import "time"

type Ticket struct {
	ID                      uint       `gorm:"primaryKey" json:"id"`
	Reference               string     `gorm:"size:32;not null;uniqueIndex" json:"reference"`
	Type                    string     `gorm:"size:32;not null;index" json:"type"`
	CategoryID              *uint      `gorm:"index" json:"categoryId"`
	CategoryCode            string     `gorm:"size:60;index" json:"categoryCode"`
	Subcategory             string     `gorm:"size:80;index" json:"subcategory"`
	Title                   string     `gorm:"size:180;not null" json:"title"`
	Description             string     `gorm:"type:text;not null" json:"description"`
	Status                  string     `gorm:"size:32;not null;index" json:"status"`
	Priority                string     `gorm:"size:16;not null;index" json:"priority"`
	Impact                  string     `gorm:"size:24;not null" json:"impact"`
	Urgency                 string     `gorm:"size:24;not null" json:"urgency"`
	RequesterUserID         uint       `gorm:"not null;index" json:"requesterUserId"`
	RequesterStaffProfileID *uint      `gorm:"index" json:"requesterStaffProfileId"`
	DepartmentID            *uint      `gorm:"index" json:"departmentId"`
	ServiceID               *uint      `gorm:"index" json:"serviceId"`
	AssignedToUserID        *uint      `gorm:"index" json:"assignedToUserId"`
	AssignedQueue           string     `gorm:"size:60;index" json:"assignedQueue"`
	ApplicationModule       string     `gorm:"size:80;index" json:"applicationModule"`
	PageURL                 string     `gorm:"size:500" json:"pageUrl"`
	RequestID               string     `gorm:"size:120" json:"requestId"`
	FrontendVersion         string     `gorm:"size:80" json:"frontendVersion"`
	BackendVersion          string     `gorm:"size:80" json:"backendVersion"`
	PatientRef              string     `gorm:"size:80" json:"patientRef,omitempty"`
	EncounterRef            string     `gorm:"size:80" json:"encounterRef,omitempty"`
	CommitSHA               string     `gorm:"size:64" json:"commitSha,omitempty"`
	QARunID                 string     `gorm:"size:80" json:"qaRunId,omitempty"`
	ReleaseVersion          string     `gorm:"size:80" json:"releaseVersion,omitempty"`
	AssignedAt              *time.Time `json:"assignedAt"`
	FirstResponseAt         *time.Time `json:"firstResponseAt"`
	ResolvedAt              *time.Time `json:"resolvedAt"`
	ClosedAt                *time.Time `json:"closedAt"`
	ResponseDueAt           time.Time  `gorm:"not null;index" json:"responseDueAt"`
	ResolutionDueAt         time.Time  `gorm:"not null;index" json:"resolutionDueAt"`
	ResolutionSummary       string     `gorm:"type:text" json:"resolutionSummary"`
	ResolutionCode          string     `gorm:"size:60" json:"resolutionCode"`
	CreatedAt               time.Time  `gorm:"not null;index" json:"createdAt"`
	UpdatedAt               time.Time  `gorm:"not null" json:"updatedAt"`
}

func (Ticket) TableName() string { return "ticketing_tickets" }

type Comment struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	TicketID     uint      `gorm:"not null;index" json:"ticketId"`
	AuthorUserID uint      `gorm:"not null;index" json:"authorUserId"`
	Visibility   string    `gorm:"size:16;not null;index" json:"visibility"`
	Content      string    `gorm:"type:text;not null" json:"content"`
	CreatedAt    time.Time `gorm:"not null" json:"createdAt"`
}

func (Comment) TableName() string { return "ticketing_comments" }

type Attachment struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	TicketID     uint      `gorm:"not null;index" json:"ticketId"`
	UploadedBy   uint      `gorm:"not null;index" json:"uploadedBy"`
	OriginalName string    `gorm:"size:255;not null" json:"originalName"`
	StoredName   string    `gorm:"size:255;not null;uniqueIndex" json:"-"`
	MIMEType     string    `gorm:"size:100;not null" json:"mimeType"`
	Size         int64     `gorm:"not null" json:"size"`
	CreatedAt    time.Time `gorm:"not null" json:"createdAt"`
}

func (Attachment) TableName() string { return "ticketing_attachments" }

type Assignment struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	TicketID    uint      `gorm:"not null;index" json:"ticketId"`
	FromUserID  *uint     `gorm:"index" json:"fromUserId"`
	ToUserID    *uint     `gorm:"index" json:"toUserId"`
	FromQueue   string    `gorm:"size:60" json:"fromQueue"`
	ToQueue     string    `gorm:"size:60" json:"toQueue"`
	ActorUserID uint      `gorm:"not null;index" json:"actorUserId"`
	CreatedAt   time.Time `gorm:"not null" json:"createdAt"`
}

func (Assignment) TableName() string { return "ticketing_assignments" }

type History struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	TicketID    uint      `gorm:"not null;index" json:"ticketId"`
	ActorUserID uint      `gorm:"not null;index" json:"actorUserId"`
	EventType   string    `gorm:"size:40;not null;index" json:"eventType"`
	Field       string    `gorm:"size:60" json:"field"`
	OldValue    string    `gorm:"type:text" json:"oldValue"`
	NewValue    string    `gorm:"type:text" json:"newValue"`
	CreatedAt   time.Time `gorm:"not null;index" json:"createdAt"`
}

func (History) TableName() string { return "ticketing_history" }

type Category struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Code      string    `gorm:"size:60;not null;uniqueIndex" json:"code"`
	Name      string    `gorm:"size:120;not null" json:"name"`
	Type      string    `gorm:"size:32;not null;index" json:"type"`
	ParentID  *uint     `gorm:"index" json:"parentId"`
	Active    bool      `gorm:"not null;default:true;index" json:"active"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (Category) TableName() string { return "ticketing_categories" }

type SLA struct {
	ID                uint      `gorm:"primaryKey" json:"id"`
	Priority          string    `gorm:"size:16;not null;uniqueIndex" json:"priority"`
	ResponseMinutes   int       `gorm:"not null" json:"responseMinutes"`
	ResolutionMinutes int       `gorm:"not null" json:"resolutionMinutes"`
	Active            bool      `gorm:"not null;default:true" json:"active"`
	UpdatedBy         uint      `gorm:"not null" json:"updatedBy"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

func (SLA) TableName() string { return "ticketing_slas" }

type Notification struct {
	ID        uint       `gorm:"primaryKey" json:"id"`
	UserID    uint       `gorm:"not null;index" json:"userId"`
	TicketID  uint       `gorm:"not null;index" json:"ticketId"`
	EventType string     `gorm:"size:40;not null;index" json:"eventType"`
	Message   string     `gorm:"size:255;not null" json:"message"`
	ReadAt    *time.Time `json:"readAt"`
	CreatedAt time.Time  `gorm:"not null;index" json:"createdAt"`
}

func (Notification) TableName() string { return "ticketing_notifications" }
