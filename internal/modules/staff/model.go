package staff

import (
	"github.com/lallene/medcore-his/backend/internal/modules/auth"
	"github.com/lallene/medcore-his/backend/internal/modules/organization"
	"time"
)

type Profile struct {
	ID                 uint                  `gorm:"primaryKey" json:"id"`
	UserID             uint                  `gorm:"not null;uniqueIndex" json:"userId"`
	EmployeeCode       string                `gorm:"size:40;not null;uniqueIndex" json:"employeeCode"`
	JobTitle           string                `gorm:"size:120" json:"jobTitle"`
	PrimaryDepartment  string                `gorm:"size:100" json:"primaryDepartment"`
	PrimaryServiceID   *uint                 `gorm:"index" json:"primaryServiceId"`
	PrimaryService     *organization.Service `gorm:"foreignKey:PrimaryServiceID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"primaryService,omitempty"`
	ProfessionalNumber string                `gorm:"size:80" json:"professionalNumber"`
	Active             bool                  `gorm:"not null;default:true;index" json:"active"`
	CreatedAt          time.Time             `json:"createdAt"`
	UpdatedAt          time.Time             `json:"updatedAt"`
	User               auth.User             `gorm:"foreignKey:UserID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
}

func (Profile) TableName() string { return "staff_profiles" }

type Function struct {
	ID         uint       `gorm:"primaryKey" json:"id"`
	ProfileID  uint       `gorm:"not null;index;uniqueIndex:ux_staff_function" json:"profileId"`
	Code       string     `gorm:"size:60;not null;index;uniqueIndex:ux_staff_function" json:"code"`
	Active     bool       `gorm:"not null;default:true;index" json:"active"`
	AssignedBy uint       `gorm:"not null;index" json:"assignedBy"`
	AssignedAt time.Time  `gorm:"not null" json:"assignedAt"`
	RemovedBy  *uint      `gorm:"index" json:"removedBy"`
	RemovedAt  *time.Time `json:"removedAt"`
	Profile    Profile    `gorm:"foreignKey:ProfileID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
}

func (Function) TableName() string { return "staff_functions" }

type Specialty struct {
	ID         uint       `gorm:"primaryKey" json:"id"`
	ProfileID  uint       `gorm:"not null;index;uniqueIndex:ux_staff_specialty" json:"profileId"`
	Code       string     `gorm:"size:60;not null;index;uniqueIndex:ux_staff_specialty" json:"code"`
	Active     bool       `gorm:"not null;default:true;index" json:"active"`
	AssignedBy uint       `gorm:"not null;index" json:"assignedBy"`
	AssignedAt time.Time  `gorm:"not null" json:"assignedAt"`
	RemovedBy  *uint      `gorm:"index" json:"removedBy"`
	RemovedAt  *time.Time `json:"removedAt"`
	Profile    Profile    `gorm:"foreignKey:ProfileID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
}

func (Specialty) TableName() string { return "staff_specialties" }

type Capability struct {
	ID         uint       `gorm:"primaryKey" json:"id"`
	ProfileID  uint       `gorm:"not null;index;uniqueIndex:ux_staff_capability" json:"profileId"`
	Code       string     `gorm:"size:60;not null;index;uniqueIndex:ux_staff_capability" json:"code"`
	Active     bool       `gorm:"not null;default:true;index" json:"active"`
	AssignedBy uint       `gorm:"not null;index" json:"assignedBy"`
	AssignedAt time.Time  `gorm:"not null" json:"assignedAt"`
	RemovedBy  *uint      `gorm:"index" json:"removedBy"`
	RemovedAt  *time.Time `json:"removedAt"`
	Profile    Profile    `gorm:"foreignKey:ProfileID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
}

func (Capability) TableName() string { return "staff_capabilities" }

type AuditEvent struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	ProfileID uint      `gorm:"not null;index" json:"profileId"`
	ActorID   uint      `gorm:"not null;index" json:"actorId"`
	Action    string    `gorm:"size:50;not null;index" json:"action"`
	Dimension string    `gorm:"size:40;not null" json:"dimension"`
	Value     string    `gorm:"size:120" json:"value"`
	CreatedAt time.Time `gorm:"not null;index" json:"createdAt"`
	Profile   Profile   `gorm:"foreignKey:ProfileID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
}

func (AuditEvent) TableName() string { return "staff_audit_events" }
