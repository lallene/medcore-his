package guarantor

import (
	"github.com/lallene/medcore-his/backend/internal/core/entity"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/company"
)

type InsuranceGuarantor struct {
	entity.BaseEntity

	CompanyID uint                     `gorm:"index;not null" json:"companyId"`
	Company   company.InsuranceCompany `gorm:"foreignKey:CompanyID" json:"company"`

	Code                string  `gorm:"size:50;index;not null" json:"code"`
	Name                string  `gorm:"size:150;not null" json:"name"`
	Description         string  `gorm:"type:text" json:"description"`
	DefaultCoverageRate float64 `gorm:"default:0" json:"defaultCoverageRate"`
	PaymentDelayDays    int     `gorm:"default:0" json:"paymentDelayDays"`
	IsActive            bool    `gorm:"default:true" json:"isActive"`
}

func (InsuranceGuarantor) TableName() string {
	return "insurance_guarantors"
}
