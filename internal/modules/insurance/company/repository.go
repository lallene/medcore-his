package company

import (
	"github.com/lallene/medcore-his/backend/internal/core/repository"
	"gorm.io/gorm"
)

type Repository interface {
	repository.Repository[InsuranceCompany]

	FindByCode(code string) (*InsuranceCompany, error)
	FindByName(name string) (*InsuranceCompany, error)
}

type companyRepository struct {
	repository.Repository[InsuranceCompany]
	db *gorm.DB
}

func NewRepository(db *gorm.DB) Repository {
	return &companyRepository{
		Repository: repository.New[InsuranceCompany](db),
		db:         db,
	}
}

func (r *companyRepository) FindByCode(code string) (*InsuranceCompany, error) {
	var item InsuranceCompany

	if err := r.db.Where("code = ?", code).First(&item).Error; err != nil {
		return nil, err
	}

	return &item, nil
}

func (r *companyRepository) FindByName(name string) (*InsuranceCompany, error) {
	var item InsuranceCompany

	if err := r.db.Where("name = ?", name).First(&item).Error; err != nil {
		return nil, err
	}

	return &item, nil
}
