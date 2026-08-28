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

func (r *Repository) Create(consultation *Consultation, authorID uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(consultation).Error; err != nil {
			return err
		}
		return tx.Model(&ConsultationExamRequest{}).
			Where("consultation_id = ?", consultation.ID).
			Updates(map[string]interface{}{"prescribed_by": authorID, "priority": "ROUTINE"}).Error
	})
}

func (r *Repository) List(filter ConsultationListFilter) (*ConsultationListResult, error) {
	consultationsTable := r.db.NamingStrategy.TableName("consultations")
	patientsTable := r.db.NamingStrategy.TableName("patients")
	query := r.db.Table(consultationsTable + " AS c").
		Joins("JOIN " + patientsTable + " AS p ON p.id = c.patient_id")
	if filter.PatientID != nil {
		query = query.Where("c.patient_id = ?", *filter.PatientID)
	}
	if filter.Status != "" {
		query = query.Where("c.status = ?", filter.Status)
	}
	if filter.Service != "" {
		query = query.Where("LOWER(c.service) = LOWER(?)", filter.Service)
	}
	if filter.ServiceID != nil {
		query = query.Where("c.service_id = ?", *filter.ServiceID)
	}
	if search := filter.Search; search != "" {
		like := "%" + search + "%"
		query = query.Where(`
			p.nom ILIKE ? OR p.prenoms ILIKE ? OR p.code_patient ILIKE ? OR
			p.numero_dossier ILIKE ? OR c.doctor_name ILIKE ? OR c.service ILIKE ? OR
			c.diagnosis ILIKE ?
		`, like, like, like, like, like, like, like)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}
	items := make([]ConsultationListItem, 0)
	err := query.Select(`
		c.id, c.patient_id, p.code_patient AS patient_code,
		p.numero_dossier AS patient_record,
		TRIM(CONCAT(p.nom, ' ', p.prenoms)) AS patient_name,
		c.doctor_name, c.service, c.service_id, c.status, c.diagnosis, c.created_at, c.updated_at
	`).Order("c.created_at DESC").
		Offset((filter.Page - 1) * filter.Limit).
		Limit(filter.Limit).
		Scan(&items).Error
	if err != nil {
		return nil, err
	}
	totalPages := int((total + int64(filter.Limit) - 1) / int64(filter.Limit))
	return &ConsultationListResult{Data: items, Page: filter.Page, Limit: filter.Limit, Total: total, TotalPages: totalPages}, nil
}

func (r *Repository) FindByID(id uint) (*Consultation, error) {
	var consultation Consultation

	err := r.db.
		Preload("Patient").
		Preload("OrganizationService").
		Preload("Vitals").
		Preload("Reasons").
		Preload("Exams").
		Preload("Prescriptions").
		Preload("Antecedent").
		Preload("PhysicalExams").
		Preload("PhysicalExams.Area").
		Preload("AdministeredTreatments").
		Preload("PreviousMedications").
		Preload("SurgicalHistories").
		Preload("GynecoObstetricHistories").
		Preload("SOAP").
		Preload("SpecialtyData").
		First(&consultation, id).Error

	if err != nil {
		return &consultation, err
	}

	var queueTicketID *uint
	_ = r.db.Raw(`SELECT id FROM patient_queue_tickets WHERE consultation_id = ? LIMIT 1`, id).Scan(&queueTicketID)
	consultation.QueueTicketID = queueTicketID

	return &consultation, nil
}

func (r *Repository) FindByPatientID(patientID uint) ([]Consultation, error) {
	var consultations []Consultation

	err := r.db.
		Preload("Patient").
		Preload("Vitals").
		Preload("Reasons").
		Preload("Exams").
		Preload("Prescriptions").
		Preload("Antecedent").

		// Examen physique
		Preload("PhysicalExams").
		Preload("PhysicalExams.Area").

		// Traitements administrés
		Preload("AdministeredTreatments").

		// Historique médicamenteux
		Preload("PreviousMedications").

		// Antécédents chirurgicaux
		Preload("SurgicalHistories").

		// Historique gynécologique / obstétrical
		Preload("GynecoObstetricHistories").

		// SOAP
		Preload("SOAP").

		// Données spécifiques à la spécialité
		Preload("SpecialtyData").
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
	authorID uint,
	updates map[string]interface{},
	vitals *ConsultationVitalsRequest,
	reasons []ConsultationReason,
	updateReasons bool,
	exams []MedicalExam,
	updateExams bool,
	prescriptions []ConsultationPrescription,
	updatePrescriptions bool,
	antecedent *ConsultationAntecedent,
	physicalExams *[]ConsultationPhysicalExam,
	administeredTreatments *[]ConsultationAdministeredTreatment,
	previousMedications *[]ConsultationPreviousMedication,
	surgicalHistories *[]ConsultationSurgicalHistory,
	gynecoObstetricHistories *[]ConsultationGynecoObstetricHistory,
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
			if err := tx.Model(&ConsultationExamRequest{}).
				Where("consultation_id = ? AND prescribed_by = 0", id).
				Updates(map[string]interface{}{"prescribed_by": authorID, "priority": "ROUTINE"}).Error; err != nil {
				return err
			}
		}

		if updatePrescriptions {
			var existing []ConsultationPrescription
			if err := tx.Where("consultation_id = ?", id).Find(&existing).Error; err != nil {
				return err
			}
			existingByID := make(map[uint]ConsultationPrescription, len(existing))
			dispensedByID := make(map[uint]float64, len(existing))
			for _, current := range existing {
				existingByID[current.ID] = current
				var dispensed float64
				if err := tx.Model(&pharmacy.PharmacyDispensation{}).Where("reference_type = ? AND reference_id = ?", "CONSULTATION_PRESCRIPTION", current.ID).Select("COALESCE(SUM(quantity),0)").Scan(&dispensed).Error; err != nil {
					return err
				}
				dispensedByID[current.ID] = dispensed
			}
			seen := make(map[uint]bool, len(prescriptions))
			for i := range prescriptions {
				incoming := &prescriptions[i]
				incoming.ConsultationID = id
				if incoming.ID == 0 {
					continue
				}
				current, ok := existingByID[incoming.ID]
				if !ok {
					return ErrDispensedPrescriptionConflict
				}
				seen[incoming.ID] = true
				dispensed := dispensedByID[incoming.ID]
				if dispensed > 0 {
					if incoming.PresentationID == nil || current.PresentationID == nil || *incoming.PresentationID != *current.PresentationID || incoming.Quantity < dispensed {
						return ErrDispensedPrescriptionConflict
					}
					if dispensed >= current.Quantity && incoming.Quantity != current.Quantity {
						return ErrDispensedPrescriptionConflict
					}
				}
			}
			for _, current := range existing {
				if !seen[current.ID] && dispensedByID[current.ID] > 0 {
					return ErrDispensedPrescriptionConflict
				}
			}
			for i := range prescriptions {
				incoming := &prescriptions[i]
				if incoming.ID == 0 {
					if err := tx.Create(incoming).Error; err != nil {
						return err
					}
					continue
				}
				updates := map[string]interface{}{"presentation_id": incoming.PresentationID, "medication_name": incoming.MedicationName, "dosage": incoming.Dosage, "form": incoming.Form, "route": incoming.Route, "quantity": incoming.Quantity, "frequency": incoming.Frequency, "duration": incoming.Duration, "instructions": incoming.Instructions}
				if err := tx.Model(&ConsultationPrescription{}).Where("id = ? AND consultation_id = ?", incoming.ID, id).Updates(updates).Error; err != nil {
					return err
				}
			}
			for _, current := range existing {
				if !seen[current.ID] {
					if err := tx.Delete(&ConsultationPrescription{}, current.ID).Error; err != nil {
						return err
					}
				}
			}
			if err := pharmacy.MaterializeVoucher(tx, id, &authorID); err != nil {
				return err
			}
		}

		if antecedent != nil {
			if err := tx.
				Where("consultation_id = ?", id).
				Delete(&ConsultationAntecedent{}).Error; err != nil {
				return err
			}

			antecedent.ConsultationID = id

			if err := tx.Create(antecedent).Error; err != nil {
				return err
			}
		}

		if physicalExams != nil {
			if err := tx.
				Where("consultation_id = ?", id).
				Delete(&ConsultationPhysicalExam{}).Error; err != nil {
				return err
			}

			for i := range *physicalExams {
				(*physicalExams)[i].ConsultationID = id
			}

			if len(*physicalExams) > 0 {
				if err := tx.Create(physicalExams).Error; err != nil {
					return err
				}
			}
		}

		if previousMedications != nil {
			if err := tx.
				Where("consultation_id = ?", id).
				Delete(&ConsultationPreviousMedication{}).Error; err != nil {
				return err
			}

			for i := range *previousMedications {
				(*previousMedications)[i].ConsultationID = id
			}

			if len(*previousMedications) > 0 {
				if err := tx.Create(previousMedications).Error; err != nil {
					return err
				}
			}
		}

		if surgicalHistories != nil {
			if err := tx.
				Where("consultation_id = ?", id).
				Delete(&ConsultationSurgicalHistory{}).Error; err != nil {
				return err
			}

			for i := range *surgicalHistories {
				(*surgicalHistories)[i].ConsultationID = id
			}

			if len(*surgicalHistories) > 0 {
				if err := tx.Create(surgicalHistories).Error; err != nil {
					return err
				}
			}
		}

		if gynecoObstetricHistories != nil {
			if err := tx.
				Where("consultation_id = ?", id).
				Delete(&ConsultationGynecoObstetricHistory{}).Error; err != nil {
				return err
			}

			for i := range *gynecoObstetricHistories {
				(*gynecoObstetricHistories)[i].ConsultationID = id
			}

			if len(*gynecoObstetricHistories) > 0 {
				if err := tx.Create(gynecoObstetricHistories).Error; err != nil {
					return err
				}
			}
		}

		if administeredTreatments != nil {
			if err := tx.
				Where("consultation_id = ?", id).
				Delete(&ConsultationAdministeredTreatment{}).Error; err != nil {
				return err
			}

			for i := range *administeredTreatments {
				(*administeredTreatments)[i].ConsultationID = id
			}

			if len(*administeredTreatments) > 0 {
				if err := tx.Create(administeredTreatments).Error; err != nil {
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

func (r *Repository) ReplaceClinicalBlocks(
	consultationID uint,
	antecedent *ConsultationAntecedent,
	physicalExams *[]ConsultationPhysicalExam,
	administeredTreatments *[]ConsultationAdministeredTreatment,
) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if antecedent != nil {
			if err := tx.
				Where("consultation_id = ?", consultationID).
				Delete(&ConsultationAntecedent{}).Error; err != nil {
				return err
			}

			antecedent.ConsultationID = consultationID

			if err := tx.Create(antecedent).Error; err != nil {
				return err
			}
		}

		if physicalExams != nil {
			if err := tx.
				Where("consultation_id = ?", consultationID).
				Delete(&ConsultationPhysicalExam{}).Error; err != nil {
				return err
			}

			for i := range *physicalExams {
				(*physicalExams)[i].ConsultationID = consultationID
			}

			if len(*physicalExams) > 0 {
				if err := tx.Create(physicalExams).Error; err != nil {
					return err
				}
			}
		}

		if administeredTreatments != nil {
			if err := tx.
				Where("consultation_id = ?", consultationID).
				Delete(&ConsultationAdministeredTreatment{}).Error; err != nil {
				return err
			}

			for i := range *administeredTreatments {
				(*administeredTreatments)[i].ConsultationID = consultationID
			}

			if len(*administeredTreatments) > 0 {
				if err := tx.Create(administeredTreatments).Error; err != nil {
					return err
				}
			}
		}

		return nil
	})
}

func (r *Repository) FindPhysicalExamAreas() ([]PhysicalExamArea, error) {
	var areas []PhysicalExamArea

	err := r.db.
		Where("is_active = ?", true).
		Order("category ASC").
		Order("name ASC").
		Find(&areas).Error

	return areas, err
}

func (r *Repository) FindPhysicalExamAreaByID(id uint) (*PhysicalExamArea, error) {
	var area PhysicalExamArea

	if err := r.db.First(&area, id).Error; err != nil {
		return nil, err
	}

	return &area, nil
}

func (r *Repository) GetSOAPByConsultationID(consultationID uint) (*ConsultationSOAP, error) {
	var soap ConsultationSOAP

	err := r.db.
		Where("consultation_id = ?", consultationID).
		First(&soap).Error

	return &soap, err
}

func (r *Repository) UpsertSOAP(soap *ConsultationSOAP) error {
	return r.db.
		Where("consultation_id = ?", soap.ConsultationID).
		Assign(soap).
		FirstOrCreate(soap).Error
}

func (r *Repository) GetSpecialtyDataByConsultationID(
	consultationID uint,
) (*ConsultationSpecialtyData, error) {
	var specialtyData ConsultationSpecialtyData

	err := r.db.
		Where("consultation_id = ?", consultationID).
		First(&specialtyData).
		Error

	if err != nil {
		return nil, err
	}

	return &specialtyData, nil
}

func (r *Repository) UpsertSpecialtyData(
	specialtyData *ConsultationSpecialtyData,
) error {
	return r.db.
		Where("consultation_id = ?", specialtyData.ConsultationID).
		Assign(specialtyData).
		FirstOrCreate(specialtyData).
		Error
}
