package access

import "github.com/lallene/medcore-his/backend/internal/core/rbac"

type KPIs struct {
	Users          int64 `json:"users"`
	Active         int64 `json:"active"`
	Disabled       int64 `json:"disabled"`
	Privileged     int64 `json:"privileged"`
	WithoutService int64 `json:"withoutService"`
	WithOverrides  int64 `json:"withOverrides"`
}

type ServiceRef struct {
	ID        uint   `json:"id"`
	Code      string `json:"code"`
	Name      string `json:"name"`
	IsPrimary bool   `json:"isPrimary"`
}

type UserSummary struct {
	ProfileID     uint         `json:"profileId"`
	UserID        uint         `json:"userId"`
	Name          string       `json:"name"`
	Email         string       `json:"email"`
	EmployeeCode  string       `json:"employeeCode"`
	Active        bool         `json:"active"`
	Functions     []string     `json:"functions"`
	Specialties   []string     `json:"specialties"`
	Services      []ServiceRef `json:"services"`
	AccessLevel   string       `json:"accessLevel"`
	OverrideCount int          `json:"overrideCount"`
	Privileged    bool         `json:"privileged"`
	UpdatedAt     string       `json:"updatedAt"`
}

type UserList struct {
	Items      []UserSummary `json:"items"`
	Page       int           `json:"page"`
	Limit      int           `json:"limit"`
	Total      int64         `json:"total"`
	TotalPages int           `json:"totalPages"`
}

type UserDetail struct {
	UserSummary
	JobTitle             string                 `json:"jobTitle"`
	PrimaryDepartment    string                 `json:"primaryDepartment"`
	ProfessionalNumber   string                 `json:"professionalNumber"`
	PrimaryServiceID     *uint                  `json:"primaryServiceId"`
	Effective            []rbac.EffectiveEntry  `json:"effective"`
	EffectiveCodes       []string               `json:"effectiveCodes"`
	Overrides            []PermissionOverride   `json:"overrides"`
}

type MatrixCell struct {
	FunctionCode string `json:"functionCode"`
	Permission   string `json:"permission"`
	Allowed      bool   `json:"allowed"`
}

type MatrixResponse struct {
	Functions   []string             `json:"functions"`
	Permissions []string             `json:"permissions"`
	Cells       []MatrixCell         `json:"cells"`
	Overlays    []rbac.MatrixOverlay `json:"overlays"`
}

type PermissionCatalogItem struct {
	Key        string   `json:"key"`
	Label      string   `json:"label"`
	Domain     string   `json:"domain"`
	ScopeHint  string   `json:"scopeHint"`
	Sensitive  bool     `json:"sensitive"`
	Functions  []string `json:"functions"`
}

type SimNavItem struct {
	Title    string `json:"title"`
	Href     string `json:"href"`
	Visible  bool   `json:"visible"`
}

type SimAction struct {
	Code    string `json:"code"`
	Label   string `json:"label"`
	Allowed bool   `json:"allowed"`
}

type Simulation struct {
	UserID      uint         `json:"userId"`
	ProfileID   uint         `json:"profileId"`
	Name        string       `json:"name"`
	Navigation  []SimNavItem `json:"navigation"`
	Actions     []SimAction  `json:"actions"`
	Services    []ServiceRef `json:"services"`
	Permissions []string     `json:"permissions"`
	Note        string       `json:"note"`
}
