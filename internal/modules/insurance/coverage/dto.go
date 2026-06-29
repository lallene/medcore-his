package coverage

type CreateCoverageRequest struct {
	PatientID    uint    `json:"patientId" binding:"required"`
	CompanyID    uint    `json:"companyId" binding:"required"`
	GuarantorID  uint    `json:"guarantorId" binding:"required"`
	MemberNumber string  `json:"memberNumber" binding:"required,max=100"`
	Subscriber   string  `json:"subscriber" binding:"max=150"`
	Beneficiary  string  `json:"beneficiary" binding:"max=150"`
	CoverageRate float64 `json:"coverageRate" binding:"gte=0,lte=100"`
	ValidFrom    string  `json:"validFrom"`
	ValidTo      string  `json:"validTo"`
	IsPrincipal  bool    `json:"isPrincipal"`
}

type UpdateCoverageRequest struct {
	CompanyID    uint    `json:"companyId"`
	GuarantorID  uint    `json:"guarantorId"`
	MemberNumber string  `json:"memberNumber" binding:"omitempty,max=100"`
	Subscriber   string  `json:"subscriber" binding:"max=150"`
	Beneficiary  string  `json:"beneficiary" binding:"max=150"`
	CoverageRate float64 `json:"coverageRate" binding:"gte=0,lte=100"`
	ValidFrom    string  `json:"validFrom"`
	ValidTo      string  `json:"validTo"`
	IsPrincipal  *bool   `json:"isPrincipal"`
	IsActive     *bool   `json:"isActive"`
}

type CoverageResponse struct {
	ID            uint   `json:"id"`
	UUID          string `json:"uuid"`
	PatientID     uint   `json:"patientId"`
	PatientName   string `json:"patientName,omitempty"`
	CompanyID     uint   `json:"companyId"`
	CompanyName   string `json:"companyName,omitempty"`
	GuarantorID   uint   `json:"guarantorId"`
	GuarantorName string `json:"guarantorName,omitempty"`

	MemberNumber string  `json:"memberNumber"`
	Subscriber   string  `json:"subscriber"`
	Beneficiary  string  `json:"beneficiary"`
	CoverageRate float64 `json:"coverageRate"`
	ValidFrom    string  `json:"validFrom,omitempty"`
	ValidTo      string  `json:"validTo,omitempty"`
	IsPrincipal  bool    `json:"isPrincipal"`
	IsActive     bool    `json:"isActive"`
}

type CoverageSummary struct {
	ID            uint    `json:"id"`
	PatientID     uint    `json:"patientId"`
	PatientName   string  `json:"patientName,omitempty"`
	CompanyName   string  `json:"companyName,omitempty"`
	GuarantorName string  `json:"guarantorName,omitempty"`
	MemberNumber  string  `json:"memberNumber"`
	CoverageRate  float64 `json:"coverageRate"`
	IsPrincipal   bool    `json:"isPrincipal"`
	IsActive      bool    `json:"isActive"`
}
