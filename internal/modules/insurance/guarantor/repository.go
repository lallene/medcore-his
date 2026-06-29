package guarantor

import (
	"github.com/lallene/medcore-his/backend/internal/core/repository"
	"gorm.io/gorm"
)

type Repository interface {
	repository.Repository[InsuranceGuarantor]

	FindByCompanyAndCode(companyID uint, code string) (*InsuranceGuarantor, error)
}

type guarantorRepository struct {
	repository.Repository[InsuranceGuarantor]
	db *gorm.DB
}

func NewRepository(db *gorm.DB) Repository {
	return &guarantorRepository{
		Repository: repository.New[InsuranceGuarantor](db),
		db:         db,
	}
}

func (r *guarantorRepository) FindByCompanyAndCode(companyID uint, code string) (*InsuranceGuarantor, error) {
	var item InsuranceGuarantor

	if err := r.db.
		Where("company_id = ? AND code = ?", companyID, code).
		First(&item).Error; err != nil {
		return nil, err
	}

	return &item, nil
}
