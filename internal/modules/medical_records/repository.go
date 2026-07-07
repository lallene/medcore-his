package medical_records

import "gorm.io/gorm"

type Repository interface {
	CreateMedicalRecord(record *MedicalRecord) error
	GetMedicalRecordByPatientID(patientID uint) (*MedicalRecord, error)
	GetMedicalRecordByID(id uint) (*MedicalRecord, error)

	CreateAlert(alert *MedicalAlert) error
	ListAlerts(recordID uint) ([]MedicalAlert, error)

	CreateAllergy(allergy *Allergy) error
	ListAllergies(recordID uint) ([]Allergy, error)

	CreateMedicalHistory(history *MedicalHistory) error
	ListMedicalHistories(recordID uint) ([]MedicalHistory, error)

	CreateVitalSign(vital *VitalSign) error
	GetLastVitalSign(recordID uint) (*VitalSign, error)
	ListVitalSigns(recordID uint) ([]VitalSign, error)

	CreateTimelineEvent(event *MedicalTimelineEvent) error
	ListTimelineEvents(recordID uint) ([]MedicalTimelineEvent, error)
}

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) Repository {
	return &repository{db: db}
}

func (r *repository) CreateMedicalRecord(record *MedicalRecord) error {
	return r.db.Create(record).Error
}

func (r *repository) GetMedicalRecordByPatientID(patientID uint) (*MedicalRecord, error) {
	var record MedicalRecord
	err := r.db.Where("patient_id = ?", patientID).First(&record).Error
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (r *repository) GetMedicalRecordByID(id uint) (*MedicalRecord, error) {
	var record MedicalRecord
	err := r.db.First(&record, id).Error
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (r *repository) CreateAlert(alert *MedicalAlert) error {
	return r.db.Create(alert).Error
}

func (r *repository) ListAlerts(recordID uint) ([]MedicalAlert, error) {
	var alerts []MedicalAlert
	err := r.db.
		Where("medical_record_id = ? AND is_active = ?", recordID, true).
		Order("created_at DESC").
		Find(&alerts).Error
	return alerts, err
}

func (r *repository) CreateAllergy(allergy *Allergy) error {
	return r.db.Create(allergy).Error
}

func (r *repository) ListAllergies(recordID uint) ([]Allergy, error) {
	var allergies []Allergy
	err := r.db.
		Where("medical_record_id = ? AND is_active = ?", recordID, true).
		Order("created_at DESC").
		Find(&allergies).Error
	return allergies, err
}

func (r *repository) CreateMedicalHistory(history *MedicalHistory) error {
	return r.db.Create(history).Error
}

func (r *repository) ListMedicalHistories(recordID uint) ([]MedicalHistory, error) {
	var histories []MedicalHistory
	err := r.db.
		Where("medical_record_id = ?", recordID).
		Order("created_at DESC").
		Find(&histories).Error
	return histories, err
}

func (r *repository) CreateVitalSign(vital *VitalSign) error {
	return r.db.Create(vital).Error
}

func (r *repository) GetLastVitalSign(recordID uint) (*VitalSign, error) {
	var vital VitalSign
	err := r.db.
		Where("medical_record_id = ?", recordID).
		Order("measured_at DESC").
		First(&vital).Error

	if err != nil {
		return nil, err
	}

	return &vital, nil
}

func (r *repository) ListVitalSigns(recordID uint) ([]VitalSign, error) {
	var vitals []VitalSign
	err := r.db.
		Where("medical_record_id = ?", recordID).
		Order("measured_at DESC").
		Find(&vitals).Error
	return vitals, err
}

func (r *repository) CreateTimelineEvent(event *MedicalTimelineEvent) error {
	return r.db.Create(event).Error
}

func (r *repository) ListTimelineEvents(recordID uint) ([]MedicalTimelineEvent, error) {
	var events []MedicalTimelineEvent

	err := r.db.
		Where("medical_record_id = ?", recordID).
		Order("event_date DESC, id DESC").
		Find(&events).Error

	return events, err
}
