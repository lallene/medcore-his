package voucher

import (
	"github.com/lallene/medcore-his/backend/internal/core/repository"
	"gorm.io/gorm"
)

type Repository interface {
	repository.Repository[InsuranceVoucher]

	FindByNumber(number string) (*InsuranceVoucher, error)
}

type voucherRepository struct {
	repository.Repository[InsuranceVoucher]
	db *gorm.DB
}

func NewRepository(db *gorm.DB) Repository {
	return &voucherRepository{
		Repository: repository.New[InsuranceVoucher](db),
		db:         db,
	}
}

func (r *voucherRepository) FindByNumber(number string) (*InsuranceVoucher, error) {
	var item InsuranceVoucher

	if err := r.db.Where("voucher_number = ?", number).First(&item).Error; err != nil {
		return nil, err
	}

	return &item, nil
}
