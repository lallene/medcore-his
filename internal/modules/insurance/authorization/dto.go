package authorization

type CreateRequest struct {
	PatientID         uint     `json:"patientId" binding:"required"`
	PatientCoverageID uint     `json:"patientCoverageId" binding:"required"`
	ReferenceType     string   `json:"referenceType" binding:"required"`
	ReferenceID       uint     `json:"referenceId" binding:"required"`
	Service           string   `json:"service"`
	RequestedAmount   *float64 `json:"requestedAmount"`
	Comment           string   `json:"comment"`
}

type UpdateRequest struct {
	PatientCoverageID uint     `json:"patientCoverageId"`
	Service           string   `json:"service"`
	RequestedAmount   *float64 `json:"requestedAmount"`
	Comment           string   `json:"comment"`
}

type SubmitRequest struct {
	ExternalReference string `json:"externalReference"`
	SubmittedAt       string `json:"submittedAt"`
}

type DecisionRequest struct {
	Status               string   `json:"status" binding:"required"`
	ExternalReference    string   `json:"externalReference" binding:"required"`
	ExternalDecisionDate string   `json:"externalDecisionDate" binding:"required"`
	ApprovedRate         *float64 `json:"approvedRate"`
	ApprovedAmount       *float64 `json:"approvedAmount"`
	PatientAmount        *float64 `json:"patientAmount"`
	CeilingAmount        *float64 `json:"ceilingAmount"`
	RejectionReason      string   `json:"rejectionReason"`
	Comment              string   `json:"comment"`
}

type ListQuery struct {
	Search        string
	Status        string
	ReferenceType string
	Service       string
	CompanyID     uint
	PatientID     uint
	DateFrom      string
	DateTo        string
	Page          int
	PageSize      int
}

type Response struct {
	InsuranceAuthorization
	PatientName    string  `json:"patientName"`
	PatientCode    string  `json:"patientCode"`
	CompanyName    string  `json:"companyName"`
	MemberNumber   string  `json:"memberNumber"`
	ContractRate   float64 `json:"contractRate"`
	GuarantorName  string  `json:"guarantorName"`
	ReferenceLabel string  `json:"referenceLabel"`
}

type Page struct {
	Items      []Response `json:"items"`
	Page       int        `json:"page"`
	PageSize   int        `json:"pageSize"`
	Total      int64      `json:"total"`
	TotalPages int        `json:"totalPages"`
}
