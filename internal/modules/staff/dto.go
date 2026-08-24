package staff

type Filter struct {
	Search, Function, Specialty, Active string
	Page, Limit                         int
}
type UpsertRequest struct {
	UserID             uint     `json:"userId"`
	EmployeeCode       string   `json:"employeeCode"`
	JobTitle           string   `json:"jobTitle"`
	PrimaryDepartment  string   `json:"primaryDepartment"`
	ProfessionalNumber string   `json:"professionalNumber"`
	Active             *bool    `json:"active"`
	Functions          []string `json:"functions"`
	Specialties        []string `json:"specialties"`
	Capabilities       []string `json:"capabilities"`
}
type View struct {
	Profile
	Name                 string   `json:"name"`
	Email                string   `json:"email"`
	LegacyRole           string   `json:"legacyRole"`
	Functions            []string `json:"functions"`
	Specialties          []string `json:"specialties"`
	Capabilities         []string `json:"capabilities"`
	EffectivePermissions []string `json:"effectivePermissions"`
}
type Page struct {
	Items       []View `json:"items"`
	Page, Limit int
	Total       int64 `json:"total"`
	TotalPages  int   `json:"totalPages"`
}
type MatrixRow struct {
	Code        string   `json:"code"`
	Label       string   `json:"label"`
	Permissions []string `json:"permissions"`
}
type Catalog struct {
	Functions    map[string]string `json:"functions"`
	Specialties  map[string]string `json:"specialties"`
	Capabilities map[string]string `json:"capabilities"`
	Matrix       []MatrixRow       `json:"matrix"`
}
type UserOption struct {
	ID         uint   `json:"id"`
	Name       string `json:"name"`
	Email      string `json:"email"`
	HasProfile bool   `json:"hasProfile"`
}
