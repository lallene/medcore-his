package patients

import (
	"github.com/lallene/medcore-his/backend/internal/core/repository"
	"gorm.io/gorm"
)

type Repository interface {
	repository.Repository[Patient]

	FindByTelephone(telephone string) (*Patient, error)
	FindByNumeroDossier(numeroDossier string) (*Patient, error)
}

type patientRepository struct {
	repository.Repository[Patient]
	db *gorm.DB
}

func NewRepository(db *gorm.DB) Repository {
	return &patientRepository{
		Repository: repository.New[Patient](db),
		db:         db,
	}
}

func (r *patientRepository) FindByTelephone(telephone string) (*Patient, error) {
	var patient Patient

	err := r.db.Where("telephone = ?", telephone).First(&patient).Error

	if err != nil {
		return nil, err
	}

	return &patient, nil
}

func (r *patientRepository) FindByNumeroDossier(numeroDossier string) (*Patient, error) {
	var patient Patient

	err := r.db.Where("numero_dossier = ?", numeroDossier).First(&patient).Error

	if err != nil {
		return nil, err
	}

	return &patient, nil
}
