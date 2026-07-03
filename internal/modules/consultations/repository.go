package consultations

import (
	"github.com/lallene/medcore-his/backend/internal/modules/pharmacy"
	"gorm.io/gorm"
)

type Repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) FindReasons() ([]ConsultationReason, error) {
	var reasons []ConsultationReason
	err := r.db.Where("is_active = ?", true).Order("name ASC").Find(&reasons).Error
	return reasons, err
}

func (r *Repository) FindExams() ([]MedicalExam, error) {
	var exams []MedicalExam
	err := r.db.Where("is_active = ?", true).Order("name ASC").Find(&exams).Error
	return exams, err
}

func (r *Repository) Create(consultation *Consultation) error {
	return r.db.Create(consultation).Error
}

func (r *Repository) FindByID(id uint) (*Consultation, error) {
	var consultation Consultation
	err := r.db.
		Preload("Patient").
		Preload("Vitals").
		Preload("Reasons").
		Preload("Exams").
		Preload("Prescriptions").
		First(&consultation, id).Error

	return &consultation, err
}

func (r *Repository) FindByPatientID(patientID uint) ([]Consultation, error) {
	var consultations []Consultation

	err := r.db.
		Preload("Patient").
		Preload("Vitals").
		Preload("Reasons").
		Preload("Exams").
		Preload("Prescriptions").
		Where("patient_id = ?", patientID).
		Order("created_at DESC").
		Find(&consultations).Error

	return consultations, err
}

func (r *Repository) FindReasonsByIDs(ids []uint) ([]ConsultationReason, error) {
	var reasons []ConsultationReason
	err := r.db.Where("id IN ? AND is_active = ?", ids, true).Find(&reasons).Error
	return reasons, err
}

func (r *Repository) FindExamsByIDs(ids []uint) ([]MedicalExam, error) {
	var exams []MedicalExam
	err := r.db.Where("id IN ? AND is_active = ?", ids, true).Find(&exams).Error
	return exams, err
}

func (r *Repository) CreateReason(reason *ConsultationReason) error {
	return r.db.Create(reason).Error
}

func (r *Repository) UpdateReason(id uint, req UpdateReferenceRequest) error {
	updates := map[string]interface{}{}

	if req.Code != "" {
		updates["code"] = req.Code
	}
	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.Category != "" {
		updates["category"] = req.Category
	}
	if req.IsActive != nil {
		updates["is_active"] = *req.IsActive
	}

	return r.db.Model(&ConsultationReason{}).Where("id = ?", id).Updates(updates).Error
}

func (r *Repository) DeleteReason(id uint) error {
	return r.db.Model(&ConsultationReason{}).
		Where("id = ?", id).
		Update("is_active", false).Error
}

func (r *Repository) CreateExam(exam *MedicalExam) error {
	return r.db.Create(exam).Error
}

func (r *Repository) UpdateExam(id uint, req UpdateReferenceRequest) error {
	updates := map[string]interface{}{}

	if req.Code != "" {
		updates["code"] = req.Code
	}
	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.Category != "" {
		updates["category"] = req.Category
	}
	if req.IsActive != nil {
		updates["is_active"] = *req.IsActive
	}

	return r.db.Model(&MedicalExam{}).Where("id = ?", id).Updates(updates).Error
}

func (r *Repository) DeleteExam(id uint) error {
	return r.db.Model(&MedicalExam{}).
		Where("id = ?", id).
		Update("is_active", false).Error
}

func (r *Repository) UpdateStatus(
	id uint,
	updates map[string]interface{},
) error {
	result := r.db.
		Model(&Consultation{}).
		Where("id = ?", id).
		Updates(updates)

	return result.Error
}

func (r *Repository) UpdateConsultation(
	id uint,
	updates map[string]interface{},
	vitals *ConsultationVitalsRequest,
	reasons []ConsultationReason,
	updateReasons bool,
	exams []MedicalExam,
	updateExams bool,
	prescriptions []ConsultationPrescription,
	updatePrescriptions bool,
) error {
	return r.db.Transaction(func(tx *gorm.DB) error {

		if len(updates) > 0 {
			if err := tx.
				Model(&Consultation{}).
				Where("id = ?", id).
				Updates(updates).Error; err != nil {
				return err
			}
		}

		if vitals != nil {
			vitalUpdates := map[string]interface{}{}

			if vitals.Temperature != nil {
				vitalUpdates["temperature"] = *vitals.Temperature
			}

			if vitals.BloodPressureSystolic != nil {
				vitalUpdates["blood_pressure_systolic"] = *vitals.BloodPressureSystolic
			}

			if vitals.BloodPressureDiastolic != nil {
				vitalUpdates["blood_pressure_diastolic"] = *vitals.BloodPressureDiastolic
			}

			if vitals.HeartRate != nil {
				vitalUpdates["heart_rate"] = *vitals.HeartRate
			}

			if vitals.RespiratoryRate != nil {
				vitalUpdates["respiratory_rate"] = *vitals.RespiratoryRate
			}

			if vitals.OxygenSaturation != nil {
				vitalUpdates["oxygen_saturation"] = *vitals.OxygenSaturation
			}

			if vitals.Weight != nil {
				vitalUpdates["weight"] = *vitals.Weight
			}

			if vitals.Height != nil {
				vitalUpdates["height"] = *vitals.Height
			}

			if vitals.BloodGlucose != nil {
				vitalUpdates["blood_glucose"] = *vitals.BloodGlucose
			}

			if vitals.PainScore != nil {
				vitalUpdates["pain_score"] = *vitals.PainScore
			}

			if len(vitalUpdates) > 0 {
				if err := tx.
					Model(&ConsultationVitals{}).
					Where("consultation_id = ?", id).
					Updates(vitalUpdates).Error; err != nil {
					return err
				}
			}
		}

		if updateReasons {
			var consultation Consultation

			if err := tx.First(&consultation, id).Error; err != nil {
				return err
			}

			if err := tx.
				Model(&consultation).
				Association("Reasons").
				Replace(reasons); err != nil {
				return err
			}
		}

		if updateExams {
			var consultation Consultation

			if err := tx.First(&consultation, id).Error; err != nil {
				return err
			}

			if err := tx.
				Model(&consultation).
				Association("Exams").
				Replace(exams); err != nil {
				return err
			}
		}

		if updatePrescriptions {
			if err := tx.
				Where("consultation_id = ?", id).
				Delete(&ConsultationPrescription{}).Error; err != nil {
				return err
			}

			for i := range prescriptions {
				prescriptions[i].ConsultationID = id
			}

			if len(prescriptions) > 0 {
				if err := tx.Create(&prescriptions).Error; err != nil {
					return err
				}
			}
		}

		return nil
	})
}

func (r *Repository) FindMedicationPresentationByID(
	id uint,
) (*pharmacy.MedicationPresentation, error) {
	var presentation pharmacy.MedicationPresentation

	if err := r.db.
		Preload("Medication").
		Preload("Medication.Family").
		First(&presentation, id).Error; err != nil {
		return nil, err
	}

	return &presentation, nil
}
