package company

import "github.com/lallene/medcore-his/backend/internal/core/entity"

type InsuranceCompany struct {
	entity.BaseEntity

	Code        string `gorm:"size:50;uniqueIndex;not null" json:"code"`
	Name        string `gorm:"size:150;uniqueIndex;not null" json:"name"`
	Description string `gorm:"type:text" json:"description"`
	Phone       string `gorm:"size:50" json:"phone"`
	Email       string `gorm:"size:150" json:"email"`
	Address     string `gorm:"type:text" json:"address"`
	City        string `gorm:"size:100" json:"city"`
	Country     string `gorm:"size:100;default:Côte d'Ivoire" json:"country"`
	IsActive    bool   `gorm:"default:true" json:"isActive"`
}

func (InsuranceCompany) TableName() string {
	return "insurance_companies"
}
