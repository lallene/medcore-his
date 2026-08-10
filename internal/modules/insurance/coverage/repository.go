package coverage

import (
	"time"

	"github.com/lallene/medcore-his/backend/internal/core/repository"
	"gorm.io/gorm"
)

type Repository interface {
	repository.Repository[PatientCoverage]

	FindActiveByPatient(patientID uint) ([]PatientCoverage, error)
	FindPrincipalByPatient(patientID uint) (*PatientCoverage, error)
}

type coverageRepository struct {
	repository.Repository[PatientCoverage]
	db *gorm.DB
}

func NewRepository(db *gorm.DB) Repository {
	return &coverageRepository{
		Repository: repository.New[PatientCoverage](db),
		db:         db,
	}
}

func (r *coverageRepository) FindActiveByPatient(patientID uint) ([]PatientCoverage, error) {
	var items []PatientCoverage
	today := time.Now().Format("2006-01-02")

	err := r.db.
		Preload("Patient").
		Preload("Company").
		Preload("Guarantor").
		Where(
			"patient_id = ? AND is_active = ? AND (valid_from IS NULL OR valid_from <= ?) AND (valid_to IS NULL OR valid_to >= ?)",
			patientID,
			true,
			today,
			today,
		).
		Order("is_principal DESC, valid_from DESC, id DESC").
		Find(&items).Error

	return items, err
}

func (r *coverageRepository) FindPrincipalByPatient(patientID uint) (*PatientCoverage, error) {
	var item PatientCoverage

	err := r.db.
		Preload("Patient").
		Preload("Company").
		Preload("Guarantor").
		Where("patient_id = ? AND is_principal = ? AND is_active = ?", patientID, true, true).
		First(&item).Error

	if err != nil {
		return nil, err
	}

	return &item, nil
}
