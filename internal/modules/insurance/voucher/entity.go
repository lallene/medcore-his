package voucher

import (
	"time"

	"github.com/lallene/medcore-his/backend/internal/core/entity"
	"github.com/lallene/medcore-his/backend/internal/core/workflow"
	company "github.com/lallene/medcore-his/backend/internal/modules/insurance/company"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/coverage"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
)

type InsuranceVoucher struct {
	entity.BaseEntity

	VoucherNumber string `gorm:"size:100;uniqueIndex;not null" json:"voucherNumber"`

	CoverageID uint                     `gorm:"index;not null" json:"coverageId"`
	Coverage   coverage.PatientCoverage `gorm:"foreignKey:CoverageID" json:"coverage"`

	PatientID uint             `gorm:"index;not null" json:"patientId"`
	Patient   patients.Patient `gorm:"foreignKey:PatientID" json:"patient"`

	CompanyID uint                     `gorm:"index;not null" json:"companyId"`
	Company   company.InsuranceCompany `gorm:"foreignKey:CompanyID" json:"company"`

	GuarantorID uint `gorm:"index;not null" json:"guarantorId"`

	ConsultationID *uint `gorm:"index" json:"consultationId"`

	Status string `gorm:"size:50;default:draft;index" json:"status"`

	IssueDate *time.Time `json:"issueDate"`

	GrossAmount   float64 `gorm:"default:0" json:"grossAmount"`
	CoveredAmount float64 `gorm:"default:0" json:"coveredAmount"`
	PatientAmount float64 `gorm:"default:0" json:"patientAmount"`
	CoverageRate  float64 `gorm:"default:0" json:"coverageRate"`

	Notes string `gorm:"type:text" json:"notes"`
}

func (InsuranceVoucher) TableName() string {
	return "insurance_vouchers"
}

func (v *InsuranceVoucher) GetWorkflowState() workflow.State {
	return workflow.State(v.Status)
}

func (v *InsuranceVoucher) SetWorkflowState(state workflow.State) {
	v.Status = string(state)
}

func (v *InsuranceVoucher) GetWorkflowID() uint {
	return v.ID
}
