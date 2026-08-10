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
	ListRecentTimelineEvents(recordID uint, limit int) ([]MedicalTimelineEvent, error)
	ListRecentConsultations(patientID uint, limit int) ([]MedicalSummaryConsultationItem, error)
	GetCommonMedicalRecord(recordID uint) (*CommonMedicalRecordResponse, error)
	GetPatientSummaryIdentity(
		patientID uint,
	) (*PatientSummaryPatient, error)

	GetLastConsultationSummary(
		patientID uint,
	) (*LastConsultationSummary, error)

	GetPatientSummaryStatistics(
		patientID uint,
		recordID uint,
	) (*PatientSummaryStatistics, error)

	SaveCommonMedicalRecord(
		record *MedicalRecord,
		req UpdateCommonMedicalRecordRequest,
		authorID uint,
	) error
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

func (r *repository) ListRecentTimelineEvents(recordID uint, limit int) ([]MedicalTimelineEvent, error) {
	var events []MedicalTimelineEvent

	err := r.db.
		Where("medical_record_id = ?", recordID).
		Order("event_date DESC, id DESC").
		Limit(limit).
		Find(&events).Error

	return events, err
}

func (r *repository) ListRecentConsultations(
	patientID uint,
	limit int,
) ([]MedicalSummaryConsultationItem, error) {
	var items []MedicalSummaryConsultationItem

	err := r.db.
		Table("consultations c").
		Select(`
			c.id,
			c.patient_id,
			c.doctor_name,
			c.service,
			c.status,
			c.diagnosis,
			c.observations,
			c.treatment,
			c.created_at,
			c.sick_leave_required,
			c.hospitalization_required,
			EXISTS (
				SELECT 1
				FROM consultation_exam_requests cer
				WHERE cer.consultation_id = c.id
			) AS has_exams,
			EXISTS (
				SELECT 1
				FROM consultation_prescriptions cp
				WHERE cp.consultation_id = c.id
			) AS has_prescriptions
		`).
		Where("c.patient_id = ?", patientID).
		Order("c.created_at DESC").
		Limit(limit).
		Scan(&items).Error

	return items, err
}

func (r *repository) GetCommonMedicalRecord(
	recordID uint,
) (*CommonMedicalRecordResponse, error) {
	var record MedicalRecord

	err := r.db.
		Preload("Profile").
		Preload("Allergies", "is_active = ?", true).
		Preload("MedicalHistories").
		Preload("SurgicalHistories").
		Preload("FamilyMedicalHistories").
		Preload("RegularTreatments").
		Preload("Vaccinations").
		Preload("Disabilities").
		Preload("Lifestyle").
		Preload("MedicalDevices").
		Preload("VitalSigns", func(db *gorm.DB) *gorm.DB {
			return db.Order("measured_at DESC")
		}).
		Preload("Documents", func(db *gorm.DB) *gorm.DB {
			return db.Order("created_at DESC")
		}).
		First(&record, recordID).
		Error

	if err != nil {
		return nil, err
	}

	profile := record.Profile
	allergies := record.Allergies
	medicalHistories := record.MedicalHistories
	surgicalHistories := record.SurgicalHistories
	familyMedicalHistories := record.FamilyMedicalHistories
	regularTreatments := record.RegularTreatments
	vaccinations := record.Vaccinations
	disabilities := record.Disabilities
	lifestyle := record.Lifestyle
	medicalDevices := record.MedicalDevices
	vitalSigns := record.VitalSigns
	documents := record.Documents

	record.Profile = nil
	record.Allergies = nil
	record.MedicalHistories = nil
	record.SurgicalHistories = nil
	record.FamilyMedicalHistories = nil
	record.RegularTreatments = nil
	record.Vaccinations = nil
	record.Disabilities = nil
	record.Lifestyle = nil
	record.MedicalDevices = nil
	record.VitalSigns = nil
	record.Documents = nil

	return &CommonMedicalRecordResponse{
		MedicalRecord: record,

		Profile: profile,

		Allergies:        allergies,
		MedicalHistories: medicalHistories,

		SurgicalHistories:      surgicalHistories,
		FamilyMedicalHistories: familyMedicalHistories,
		RegularTreatments:      regularTreatments,
		Vaccinations:           vaccinations,
		Disabilities:           disabilities,

		Lifestyle: lifestyle,

		MedicalDevices: medicalDevices,
		VitalSigns:     vitalSigns,
		Documents:      documents,
	}, nil
}

func (r *repository) SaveCommonMedicalRecord(
	record *MedicalRecord,
	req UpdateCommonMedicalRecordRequest,
	authorID uint,
) error {
	return r.saveCommonMedicalRecordNonDestructive(record, req, authorID)
}

func (r *repository) GetPatientSummaryIdentity(
	patientID uint,
) (*PatientSummaryPatient, error) {
	var patient PatientSummaryPatient

	err := r.db.
		Table("patients p").
		Select(`
			p.id,
			p.code_patient,
			p.numero_dossier,
			p.nom AS last_name,
			p.prenoms AS first_names,
			p.sexe AS sex,
			p.date_naissance AS birth_date,
			p.age,
			p.telephone AS phone,
			p.quartier AS address,
			p.is_assure AS is_insured,
			p.taux_couverture AS coverage_rate,
			p.matricule_assure AS insurance_number,
			COALESCE(pmp.blood_group, '') AS blood_group,
			COALESCE(pmp.rhesus, '') AS rhesus,
			COALESCE(pmp.insurance_name, '') AS insurance_name,
			COALESCE(pmp.mutual_name, '') AS mutual_name,
			COALESCE(pmp.photo_url, '') AS photo_url
		`).
		Joins(`
			LEFT JOIN medical_records mr
				ON mr.patient_id = p.id
		`).
		Joins(`
			LEFT JOIN patient_medical_profiles pmp
				ON pmp.medical_record_id = mr.id
		`).
		Where("p.id = ?", patientID).
		Limit(1).
		Scan(&patient).
		Error

	if err != nil {
		return nil, err
	}

	if patient.ID == 0 {
		return nil, gorm.ErrRecordNotFound
	}

	return &patient, nil
}

func (r *repository) GetLastConsultationSummary(
	patientID uint,
) (*LastConsultationSummary, error) {
	var consultation LastConsultationSummary

	err := r.db.
		Table("consultations").
		Select(`
			id,
			created_at AS date,
			service,
			doctor_name,
			diagnosis,
			status
		`).
		Where("patient_id = ?", patientID).
		Order("created_at DESC, id DESC").
		Limit(1).
		Scan(&consultation).
		Error

	if err != nil {
		return nil, err
	}

	if consultation.ID == 0 {
		return nil, gorm.ErrRecordNotFound
	}

	return &consultation, nil
}

func (r *repository) GetPatientSummaryStatistics(
	patientID uint,
	recordID uint,
) (*PatientSummaryStatistics, error) {
	var statistics PatientSummaryStatistics

	err := r.db.Raw(`
		SELECT
			(
				SELECT COUNT(*)
				FROM consultations
				WHERE patient_id = ?
			) AS consultations,

			(
				SELECT COUNT(*)
				FROM hospitalizations
				WHERE patient_id = ?
			) AS hospitalizations,

			(
				SELECT COUNT(*)
				FROM consultation_prescriptions cp
				INNER JOIN consultations c
					ON c.id = cp.consultation_id
				WHERE c.patient_id = ?
			) AS prescriptions,

			(
				SELECT COUNT(*)
				FROM consultation_exam_requests cer
				INNER JOIN consultations c
					ON c.id = cer.consultation_id
				WHERE c.patient_id = ?
			) AS exams,

			(
				SELECT COUNT(*)
				FROM medical_documents
				WHERE medical_record_id = ?
			) AS documents
	`,
		patientID,
		patientID,
		patientID,
		patientID,
		recordID,
	).Scan(&statistics).Error

	if err != nil {
		return nil, err
	}

	return &statistics, nil
}
