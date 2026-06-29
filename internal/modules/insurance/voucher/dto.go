package voucher

type CreateVoucherRequest struct {
	CoverageID     uint    `json:"coverageId" binding:"required"`
	ConsultationID *uint   `json:"consultationId"`
	IssueDate      string  `json:"issueDate"`
	GrossAmount    float64 `json:"grossAmount" binding:"gte=0"`
	Notes          string  `json:"notes"`
}

type UpdateVoucherRequest struct {
	IssueDate   string  `json:"issueDate"`
	GrossAmount float64 `json:"grossAmount" binding:"gte=0"`
	Notes       string  `json:"notes"`
}

type WorkflowActionRequest struct {
	Action string `json:"action" binding:"required"`
	Reason string `json:"reason"`
}

type VoucherResponse struct {
	ID            uint    `json:"id"`
	UUID          string  `json:"uuid"`
	VoucherNumber string  `json:"voucherNumber"`
	CoverageID    uint    `json:"coverageId"`
	PatientID     uint    `json:"patientId"`
	CompanyID     uint    `json:"companyId"`
	GuarantorID   uint    `json:"guarantorId"`
	Status        string  `json:"status"`
	IssueDate     string  `json:"issueDate,omitempty"`
	GrossAmount   float64 `json:"grossAmount"`
	CoveredAmount float64 `json:"coveredAmount"`
	PatientAmount float64 `json:"patientAmount"`
	CoverageRate  float64 `json:"coverageRate"`
	Notes         string  `json:"notes"`
}

type VoucherSummary struct {
	ID            uint    `json:"id"`
	VoucherNumber string  `json:"voucherNumber"`
	PatientID     uint    `json:"patientId"`
	Status        string  `json:"status"`
	GrossAmount   float64 `json:"grossAmount"`
	CoveredAmount float64 `json:"coveredAmount"`
	PatientAmount float64 `json:"patientAmount"`
}
