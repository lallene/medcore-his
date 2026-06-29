package coverage

import (
	"time"

	"github.com/lallene/medcore-his/backend/internal/core/entity"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/company"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/guarantor"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
)

type PatientCoverage struct {
	entity.BaseEntity

	PatientID uint             `gorm:"index;not null" json:"patientId"`
	Patient   patients.Patient `gorm:"foreignKey:PatientID" json:"patient"`

	CompanyID uint                     `gorm:"index;not null" json:"companyId"`
	Company   company.InsuranceCompany `gorm:"foreignKey:CompanyID" json:"company"`

	GuarantorID uint                         `gorm:"index;not null" json:"guarantorId"`
	Guarantor   guarantor.InsuranceGuarantor `gorm:"foreignKey:GuarantorID" json:"guarantor"`

	MemberNumber string `gorm:"size:100;index" json:"memberNumber"`
	Subscriber   string `gorm:"size:150" json:"subscriber"`
	Beneficiary  string `gorm:"size:150" json:"beneficiary"`

	CoverageRate float64 `gorm:"default:0" json:"coverageRate"`

	ValidFrom *time.Time `json:"validFrom"`
	ValidTo   *time.Time `json:"validTo"`

	IsPrincipal bool `gorm:"default:false" json:"isPrincipal"`
	IsActive    bool `gorm:"default:true" json:"isActive"`
}

func (PatientCoverage) TableName() string {
	return "patient_coverages"
}
