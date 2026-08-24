package organization

import "time"

const (
	TypeClinical       = "CLINICAL"
	TypeSurgical       = "SURGICAL"
	TypeMaternity      = "MATERNITY"
	TypeDiagnostic     = "DIAGNOSTIC"
	TypePharmacy       = "PHARMACY"
	TypeAdministrative = "ADMINISTRATIVE"
	TypeFinancial      = "FINANCIAL"
	TypeEmergency      = "EMERGENCY"
	TypeOther          = "OTHER"
)

type Department struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	Code        string    `gorm:"size:40;not null;uniqueIndex" json:"code"`
	Name        string    `gorm:"size:150;not null" json:"name"`
	Description string    `gorm:"type:text" json:"description"`
	Active      bool      `gorm:"not null;default:true;index" json:"active"`
	SortOrder   int       `gorm:"not null;default:0" json:"sortOrder"`
	CreatedBy   uint      `gorm:"not null;index" json:"createdBy"`
	UpdatedBy   uint      `gorm:"not null;index" json:"updatedBy"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	Services    []Service `gorm:"foreignKey:DepartmentID" json:"services,omitempty"`
}

func (Department) TableName() string { return "organization_departments" }

type Service struct {
	ID                      uint       `gorm:"primaryKey" json:"id"`
	DepartmentID            uint       `gorm:"not null;index" json:"departmentId"`
	Department              Department `gorm:"foreignKey:DepartmentID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"department"`
	Code                    string     `gorm:"size:40;not null;uniqueIndex" json:"code"`
	Name                    string     `gorm:"size:150;not null" json:"name"`
	ShortName               string     `gorm:"size:80" json:"shortName"`
	ServiceType             string     `gorm:"size:30;not null;index" json:"serviceType"`
	Active                  bool       `gorm:"not null;default:true;index" json:"active"`
	Clinical                bool       `gorm:"not null;default:false" json:"clinical"`
	SupportsHospitalization bool       `gorm:"not null;default:false" json:"supportsHospitalization"`
	SupportsConsultation    bool       `gorm:"not null;default:false" json:"supportsConsultation"`
	SupportsBeds            bool       `gorm:"not null;default:false" json:"supportsBeds"`
	SortOrder               int        `gorm:"not null;default:0" json:"sortOrder"`
	CreatedBy               uint       `gorm:"not null;index" json:"createdBy"`
	UpdatedBy               uint       `gorm:"not null;index" json:"updatedBy"`
	CreatedAt               time.Time  `json:"createdAt"`
	UpdatedAt               time.Time  `json:"updatedAt"`
}

func (Service) TableName() string { return "organization_services" }

type StaffServiceAssignment struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	ProfileID uint      `gorm:"not null;index;uniqueIndex:ux_staff_service" json:"profileId"`
	ServiceID uint      `gorm:"not null;index;uniqueIndex:ux_staff_service" json:"serviceId"`
	IsPrimary bool      `gorm:"not null;default:false;index" json:"isPrimary"`
	Active    bool      `gorm:"not null;default:true;index" json:"active"`
	CreatedBy uint      `gorm:"not null;index" json:"createdBy"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	Service   Service   `gorm:"foreignKey:ServiceID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"service"`
}

func (StaffServiceAssignment) TableName() string { return "staff_service_assignments" }
