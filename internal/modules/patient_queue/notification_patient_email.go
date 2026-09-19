package patient_queue

import (
	"context"

	"github.com/lallene/medcore-his/backend/internal/modules/patients"
	"gorm.io/gorm"
)

// PatientEmailReader resolves the canonical patient communication email at delivery time.
// Implementations must read patients.email only — never medical profile or other contact sources.
type PatientEmailReader interface {
	FindPatientEmail(ctx context.Context, patientID uint) (string, error)
}

// GormPatientEmailReader loads patients.email by primary key.
type GormPatientEmailReader struct {
	db *gorm.DB
}

// NewGormPatientEmailReader returns a reader backed by db. db must be non-nil.
func NewGormPatientEmailReader(db *gorm.DB) *GormPatientEmailReader {
	if db == nil {
		panic("patient email reader: db required")
	}
	return &GormPatientEmailReader{db: db}
}

// FindPatientEmail returns patients.email for patientID.
// gorm.ErrRecordNotFound when the patient row is missing (or soft-deleted).
func (r *GormPatientEmailReader) FindPatientEmail(ctx context.Context, patientID uint) (string, error) {
	if patientID == 0 {
		return "", gorm.ErrRecordNotFound
	}
	var row patients.Patient
	err := r.db.WithContext(ctx).Select("email").First(&row, patientID).Error
	if err != nil {
		return "", err
	}
	return row.Email, nil
}
