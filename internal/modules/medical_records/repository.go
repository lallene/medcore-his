package medical_records

import (
	"gorm.io/gorm"
)

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

	SaveCommonMedicalRecord(
		record *MedicalRecord,
		req UpdateCommonMedicalRecordRequest,
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
) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if req.Profile != nil {
			profile := PatientMedicalProfile{
				MedicalRecordID: record.ID,
				PatientID:       record.PatientID,

				Email:         req.Profile.Email,
				Address:       req.Profile.Address,
				MaritalStatus: req.Profile.MaritalStatus,
				Profession:    req.Profile.Profession,
				PhotoURL:      req.Profile.PhotoURL,

				EmergencyContactFirstName:    req.Profile.EmergencyContactFirstName,
				EmergencyContactLastName:     req.Profile.EmergencyContactLastName,
				EmergencyContactRelationship: req.Profile.EmergencyContactRelationship,
				EmergencyContactPhone:        req.Profile.EmergencyContactPhone,

				LegalGuardianName:         req.Profile.LegalGuardianName,
				LegalGuardianRelationship: req.Profile.LegalGuardianRelationship,
				LegalGuardianPhone:        req.Profile.LegalGuardianPhone,
				LegalGuardianAddress:      req.Profile.LegalGuardianAddress,

				InsuranceName:        req.Profile.InsuranceName,
				MutualName:           req.Profile.MutualName,
				InsuranceNumber:      req.Profile.InsuranceNumber,
				CoverageOrganization: req.Profile.CoverageOrganization,

				BloodGroup: req.Profile.BloodGroup,
				Rhesus:     req.Profile.Rhesus,

				UpdatedBy: req.UpdatedBy,
			}

			if err := tx.
				Where("medical_record_id = ?", record.ID).
				Assign(profile).
				FirstOrCreate(&profile).
				Error; err != nil {
				return err
			}
		}

		if err := tx.
			Where("medical_record_id = ?", record.ID).
			Delete(&SurgicalHistory{}).
			Error; err != nil {
			return err
		}

		for _, item := range req.SurgicalHistories {
			if item.ProcedureName == "" {
				continue
			}

			entity := SurgicalHistory{
				MedicalRecordID: record.ID,
				PatientID:       record.PatientID,
				ProcedureName:   item.ProcedureName,
				ProcedureDate:   item.ProcedureDate,
				Facility:        item.Facility,
				Complications:   item.Complications,
				Comment:         item.Comment,
				CreatedBy:       req.UpdatedBy,
			}

			if err := tx.Create(&entity).Error; err != nil {
				return err
			}
		}

		if err := tx.
			Where("medical_record_id = ?", record.ID).
			Delete(&FamilyMedicalHistory{}).
			Error; err != nil {
			return err
		}

		for _, item := range req.FamilyMedicalHistories {
			if item.Disease == "" {
				continue
			}

			entity := FamilyMedicalHistory{
				MedicalRecordID: record.ID,
				PatientID:       record.PatientID,
				Disease:         item.Disease,
				Relationship:    item.Relationship,
				Comment:         item.Comment,
				CreatedBy:       req.UpdatedBy,
			}

			if err := tx.Create(&entity).Error; err != nil {
				return err
			}
		}

		if err := tx.
			Where("medical_record_id = ?", record.ID).
			Delete(&RegularTreatment{}).
			Error; err != nil {
			return err
		}

		for _, item := range req.RegularTreatments {
			if item.MedicationName == "" {
				continue
			}

			entity := RegularTreatment{
				MedicalRecordID: record.ID,
				PatientID:       record.PatientID,
				MedicationName:  item.MedicationName,
				Dosage:          item.Dosage,
				Frequency:       item.Frequency,
				StartDate:       item.StartDate,
				Prescriber:      item.Prescriber,
				IsActive:        item.IsActive,
				CreatedBy:       req.UpdatedBy,
			}

			if err := tx.Create(&entity).Error; err != nil {
				return err
			}
		}

		if err := tx.
			Where("medical_record_id = ?", record.ID).
			Delete(&Vaccination{}).
			Error; err != nil {
			return err
		}

		for _, item := range req.Vaccinations {
			if item.VaccineName == "" {
				continue
			}

			entity := Vaccination{
				MedicalRecordID: record.ID,
				PatientID:       record.PatientID,
				VaccineName:     item.VaccineName,
				Dose:            item.Dose,
				VaccinationDate: item.VaccinationDate,
				NextBoosterDate: item.NextBoosterDate,
				Status:          item.Status,
				CreatedBy:       req.UpdatedBy,
			}

			if err := tx.Create(&entity).Error; err != nil {
				return err
			}
		}

		if err := tx.
			Where("medical_record_id = ?", record.ID).
			Delete(&Disability{}).
			Error; err != nil {
			return err
		}

		for _, item := range req.Disabilities {
			if item.Type == "" {
				continue
			}

			entity := Disability{
				MedicalRecordID: record.ID,
				PatientID:       record.PatientID,
				Type:            item.Type,
				Level:           item.Level,
				SpecialNeeds:    item.SpecialNeeds,
				CreatedBy:       req.UpdatedBy,
			}

			if err := tx.Create(&entity).Error; err != nil {
				return err
			}
		}

		if req.Lifestyle != nil {
			lifestyle := Lifestyle{
				MedicalRecordID: record.ID,
				PatientID:       record.PatientID,

				Tobacco:          req.Lifestyle.Tobacco,
				Alcohol:          req.Lifestyle.Alcohol,
				PhysicalActivity: req.Lifestyle.PhysicalActivity,
				Diet:             req.Lifestyle.Diet,

				UpdatedBy: req.UpdatedBy,
			}

			if err := tx.
				Where("medical_record_id = ?", record.ID).
				Assign(lifestyle).
				FirstOrCreate(&lifestyle).
				Error; err != nil {
				return err
			}
		}

		if err := tx.
			Where("medical_record_id = ?", record.ID).
			Delete(&MedicalDevice{}).
			Error; err != nil {
			return err
		}

		for _, item := range req.MedicalDevices {
			if item.Type == "" {
				continue
			}

			entity := MedicalDevice{
				MedicalRecordID:  record.ID,
				PatientID:        record.PatientID,
				Type:             item.Type,
				Name:             item.Name,
				Reference:        item.Reference,
				ImplantationDate: item.ImplantationDate,
				Comment:          item.Comment,
				IsActive:         item.IsActive,
				CreatedBy:        req.UpdatedBy,
			}

			if err := tx.Create(&entity).Error; err != nil {
				return err
			}
		}

		return nil
	})
}
