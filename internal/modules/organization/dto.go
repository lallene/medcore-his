package organization

type DepartmentRequest struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Active      *bool  `json:"active"`
	SortOrder   int    `json:"sortOrder"`
}

type ServiceRequest struct {
	DepartmentID            uint   `json:"departmentId"`
	Code                    string `json:"code"`
	Name                    string `json:"name"`
	ShortName               string `json:"shortName"`
	ServiceType             string `json:"serviceType"`
	Active                  *bool  `json:"active"`
	Clinical                bool   `json:"clinical"`
	SupportsHospitalization bool   `json:"supportsHospitalization"`
	SupportsConsultation    bool   `json:"supportsConsultation"`
	SupportsBeds            bool   `json:"supportsBeds"`
	SortOrder               int    `json:"sortOrder"`
}

type Catalog struct {
	Departments []Department `json:"departments"`
}
