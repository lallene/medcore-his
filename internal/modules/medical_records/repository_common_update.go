package medical_records

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrCommonMedicalRecordConflict = errors.New("le dossier médical a été modifié depuis son chargement")
	ErrCommonMedicalRecordChild    = errors.New("élément introuvable dans ce dossier médical")
	ErrCommonMedicalRecordInvalid  = errors.New("mise à jour du dossier médical invalide")
)

func (r *repository) saveCommonMedicalRecordNonDestructive(
	record *MedicalRecord,
	req UpdateCommonMedicalRecordRequest,
	authorID uint,
) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var current MedicalRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&current, record.ID).Error; err != nil {
			return err
		}

		if req.ExpectedUpdatedAt != nil && !current.UpdatedAt.Equal(*req.ExpectedUpdatedAt) {
			return ErrCommonMedicalRecordConflict
		}

		changed := false
		steps := []func(*gorm.DB, *MedicalRecord, UpdateCommonMedicalRecordRequest) (bool, error){
			updateProfile,
			updateAllergies,
			updateMedicalHistories,
			updateSurgicalHistories,
			updateFamilyMedicalHistories,
			updateRegularTreatments,
			updateVaccinations,
			updateDisabilities,
			updateLifestyle,
			updateMedicalDevices,
			updateVitalSigns,
			updateDocuments,
		}

		for _, step := range steps {
			stepChanged, err := step(tx, &current, withTrustedAuthor(req, authorID))
			if err != nil {
				return err
			}
			changed = changed || stepChanged
		}

		if !changed {
			return nil
		}

		now := time.Now()
		result := tx.Model(&MedicalRecord{}).
			Where("id = ? AND updated_at = ?", current.ID, current.UpdatedAt).
			Update("updated_at", now)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrCommonMedicalRecordConflict
		}
		record.UpdatedAt = now
		return nil
	})
}

func withTrustedAuthor(req UpdateCommonMedicalRecordRequest, authorID uint) UpdateCommonMedicalRecordRequest {
	req.authorID = authorID
	return req
}

func updateProfile(tx *gorm.DB, record *MedicalRecord, req UpdateCommonMedicalRecordRequest) (bool, error) {
	if req.Profile == nil {
		return false, nil
	}
	updates := map[string]any{}
	putString(updates, "email", req.Profile.Email)
	putString(updates, "address", req.Profile.Address)
	putString(updates, "marital_status", req.Profile.MaritalStatus)
	putString(updates, "profession", req.Profile.Profession)
	putString(updates, "photo_url", req.Profile.PhotoURL)
	putString(updates, "emergency_contact_first_name", req.Profile.EmergencyContactFirstName)
	putString(updates, "emergency_contact_last_name", req.Profile.EmergencyContactLastName)
	putString(updates, "emergency_contact_relationship", req.Profile.EmergencyContactRelationship)
	putString(updates, "emergency_contact_phone", req.Profile.EmergencyContactPhone)
	putString(updates, "legal_guardian_name", req.Profile.LegalGuardianName)
	putString(updates, "legal_guardian_relationship", req.Profile.LegalGuardianRelationship)
	putString(updates, "legal_guardian_phone", req.Profile.LegalGuardianPhone)
	putString(updates, "legal_guardian_address", req.Profile.LegalGuardianAddress)
	putString(updates, "insurance_name", req.Profile.InsuranceName)
	putString(updates, "mutual_name", req.Profile.MutualName)
	putString(updates, "insurance_number", req.Profile.InsuranceNumber)
	putString(updates, "coverage_organization", req.Profile.CoverageOrganization)
	putString(updates, "blood_group", req.Profile.BloodGroup)
	putString(updates, "rhesus", req.Profile.Rhesus)
	if len(updates) == 0 {
		return false, nil
	}
	updates["updated_by"] = req.authorID

	var profile PatientMedicalProfile
	created := false
	err := tx.Where("medical_record_id = ?", record.ID).First(&profile).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		profile = PatientMedicalProfile{MedicalRecordID: record.ID, PatientID: record.PatientID}
		if err := tx.Create(&profile).Error; err != nil {
			return false, err
		}
		created = true
	} else if err != nil {
		return false, err
	}
	different, err := childUpdatesDiffer(tx, &PatientMedicalProfile{}, record.ID, profile.ID, updates)
	if err != nil {
		return false, err
	}
	if !different {
		return created, nil
	}
	return true, tx.Model(&PatientMedicalProfile{}).
		Where("id = ? AND medical_record_id = ?", profile.ID, record.ID).
		Updates(updates).Error
}

func updateAllergies(tx *gorm.DB, record *MedicalRecord, req UpdateCommonMedicalRecordRequest) (bool, error) {
	return applyCollection(tx, record.ID, req.Allergies, &Allergy{}, func(item AllergyRequest) (bool, error) {
		updates := map[string]any{}
		putString(updates, "allergen_type", item.AllergenType)
		putString(updates, "allergen_name", item.AllergenName)
		putString(updates, "reaction", item.Reaction)
		putString(updates, "severity", item.Severity)
		putString(updates, "comment", item.Comment)
		putBool(updates, "is_active", item.IsActive)
		if item.ID > 0 {
			return updateChild(tx, &Allergy{}, record.ID, item.ID, updates)
		}
		if item.AllergenName == nil || *item.AllergenName == "" || item.AllergenType == nil || *item.AllergenType == "" {
			return false, invalid("allergy", "allergen_name et allergen_type sont obligatoires")
		}
		entity := Allergy{MedicalRecordID: record.ID, PatientID: record.PatientID, AllergenName: *item.AllergenName, AllergenType: *item.AllergenType, Severity: "medium", IsActive: true, CreatedBy: req.authorID}
		applyAllergy(&entity, item)
		return true, tx.Create(&entity).Error
	})
}

func updateMedicalHistories(tx *gorm.DB, record *MedicalRecord, req UpdateCommonMedicalRecordRequest) (bool, error) {
	return applyCollection(tx, record.ID, req.MedicalHistories, &MedicalHistory{}, func(item MedicalHistoryRequest) (bool, error) {
		updates := map[string]any{}
		putString(updates, "type", item.Type)
		putString(updates, "title", item.Title)
		putString(updates, "description", item.Description)
		putNullableTime(updates, "start_date", item.StartDate)
		putNullableTime(updates, "end_date", item.EndDate)
		putString(updates, "status", item.Status)
		putString(updates, "severity", item.Severity)
		putString(updates, "comment", item.Comment)
		if item.ID > 0 {
			return updateChild(tx, &MedicalHistory{}, record.ID, item.ID, updates)
		}
		if item.Title == nil || *item.Title == "" || item.Type == nil || *item.Type == "" {
			return false, invalid("medical_history", "title et type sont obligatoires")
		}
		entity := MedicalHistory{MedicalRecordID: record.ID, PatientID: record.PatientID, Title: *item.Title, Type: *item.Type, Status: "active", Severity: "medium", CreatedBy: req.authorID}
		applyMedicalHistory(&entity, item)
		return true, tx.Create(&entity).Error
	})
}

func updateSurgicalHistories(tx *gorm.DB, record *MedicalRecord, req UpdateCommonMedicalRecordRequest) (bool, error) {
	return applyCollection(tx, record.ID, req.SurgicalHistories, &SurgicalHistory{}, func(item SurgicalHistoryRequest) (bool, error) {
		updates := map[string]any{}
		putString(updates, "procedure_name", item.ProcedureName)
		putNullableTime(updates, "procedure_date", item.ProcedureDate)
		putString(updates, "facility", item.Facility)
		putString(updates, "complications", item.Complications)
		putString(updates, "comment", item.Comment)
		if item.ID > 0 {
			return updateChild(tx, &SurgicalHistory{}, record.ID, item.ID, updates)
		}
		if item.ProcedureName == nil || *item.ProcedureName == "" {
			return false, invalid("surgical_history", "procedure_name est obligatoire")
		}
		entity := SurgicalHistory{MedicalRecordID: record.ID, PatientID: record.PatientID, ProcedureName: *item.ProcedureName, CreatedBy: req.authorID}
		applySurgicalHistory(&entity, item)
		return true, tx.Create(&entity).Error
	})
}

func updateFamilyMedicalHistories(tx *gorm.DB, record *MedicalRecord, req UpdateCommonMedicalRecordRequest) (bool, error) {
	return applyCollection(tx, record.ID, req.FamilyMedicalHistories, &FamilyMedicalHistory{}, func(item FamilyMedicalHistoryRequest) (bool, error) {
		updates := map[string]any{}
		putString(updates, "disease", item.Disease)
		putString(updates, "relationship", item.Relationship)
		putString(updates, "comment", item.Comment)
		if item.ID > 0 {
			return updateChild(tx, &FamilyMedicalHistory{}, record.ID, item.ID, updates)
		}
		if item.Disease == nil || *item.Disease == "" {
			return false, invalid("family_medical_history", "disease est obligatoire")
		}
		entity := FamilyMedicalHistory{MedicalRecordID: record.ID, PatientID: record.PatientID, Disease: *item.Disease, CreatedBy: req.authorID}
		applyFamilyHistory(&entity, item)
		return true, tx.Create(&entity).Error
	})
}

func updateRegularTreatments(tx *gorm.DB, record *MedicalRecord, req UpdateCommonMedicalRecordRequest) (bool, error) {
	return applyCollection(tx, record.ID, req.RegularTreatments, &RegularTreatment{}, func(item RegularTreatmentRequest) (bool, error) {
		updates := map[string]any{}
		putString(updates, "medication_name", item.MedicationName)
		putString(updates, "dosage", item.Dosage)
		putString(updates, "frequency", item.Frequency)
		putNullableTime(updates, "start_date", item.StartDate)
		putString(updates, "prescriber", item.Prescriber)
		putBool(updates, "is_active", item.IsActive)
		if item.ID > 0 {
			return updateChild(tx, &RegularTreatment{}, record.ID, item.ID, updates)
		}
		if item.MedicationName == nil || *item.MedicationName == "" {
			return false, invalid("regular_treatment", "medication_name est obligatoire")
		}
		entity := RegularTreatment{MedicalRecordID: record.ID, PatientID: record.PatientID, MedicationName: *item.MedicationName, IsActive: true, CreatedBy: req.authorID}
		applyRegularTreatment(&entity, item)
		return true, tx.Create(&entity).Error
	})
}

func updateVaccinations(tx *gorm.DB, record *MedicalRecord, req UpdateCommonMedicalRecordRequest) (bool, error) {
	return applyCollection(tx, record.ID, req.Vaccinations, &Vaccination{}, func(item VaccinationRequest) (bool, error) {
		updates := map[string]any{}
		putString(updates, "vaccine_name", item.VaccineName)
		putString(updates, "dose", item.Dose)
		putNullableTime(updates, "vaccination_date", item.VaccinationDate)
		putNullableTime(updates, "next_booster_date", item.NextBoosterDate)
		putString(updates, "status", item.Status)
		if item.ID > 0 {
			return updateChild(tx, &Vaccination{}, record.ID, item.ID, updates)
		}
		if item.VaccineName == nil || *item.VaccineName == "" {
			return false, invalid("vaccination", "vaccine_name est obligatoire")
		}
		entity := Vaccination{MedicalRecordID: record.ID, PatientID: record.PatientID, VaccineName: *item.VaccineName, CreatedBy: req.authorID}
		applyVaccination(&entity, item)
		return true, tx.Create(&entity).Error
	})
}

func updateDisabilities(tx *gorm.DB, record *MedicalRecord, req UpdateCommonMedicalRecordRequest) (bool, error) {
	return applyCollection(tx, record.ID, req.Disabilities, &Disability{}, func(item DisabilityRequest) (bool, error) {
		updates := map[string]any{}
		putString(updates, "type", item.Type)
		putString(updates, "level", item.Level)
		putString(updates, "special_needs", item.SpecialNeeds)
		if item.ID > 0 {
			return updateChild(tx, &Disability{}, record.ID, item.ID, updates)
		}
		if item.Type == nil || *item.Type == "" {
			return false, invalid("disability", "type est obligatoire")
		}
		entity := Disability{MedicalRecordID: record.ID, PatientID: record.PatientID, Type: *item.Type, CreatedBy: req.authorID}
		applyDisability(&entity, item)
		return true, tx.Create(&entity).Error
	})
}

func updateLifestyle(tx *gorm.DB, record *MedicalRecord, req UpdateCommonMedicalRecordRequest) (bool, error) {
	if req.Lifestyle == nil {
		return false, nil
	}
	updates := map[string]any{}
	putString(updates, "tobacco", req.Lifestyle.Tobacco)
	putString(updates, "alcohol", req.Lifestyle.Alcohol)
	putString(updates, "physical_activity", req.Lifestyle.PhysicalActivity)
	putString(updates, "diet", req.Lifestyle.Diet)
	if len(updates) == 0 {
		return false, nil
	}
	updates["updated_by"] = req.authorID
	var lifestyle Lifestyle
	created := false
	err := tx.Where("medical_record_id = ?", record.ID).First(&lifestyle).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		lifestyle = Lifestyle{MedicalRecordID: record.ID, PatientID: record.PatientID}
		if err := tx.Create(&lifestyle).Error; err != nil {
			return false, err
		}
		created = true
	} else if err != nil {
		return false, err
	}
	different, err := childUpdatesDiffer(tx, &Lifestyle{}, record.ID, lifestyle.ID, updates)
	if err != nil {
		return false, err
	}
	if !different {
		return created, nil
	}
	return true, tx.Model(&Lifestyle{}).Where("id = ? AND medical_record_id = ?", lifestyle.ID, record.ID).Updates(updates).Error
}

func updateMedicalDevices(tx *gorm.DB, record *MedicalRecord, req UpdateCommonMedicalRecordRequest) (bool, error) {
	return applyCollection(tx, record.ID, req.MedicalDevices, &MedicalDevice{}, func(item MedicalDeviceRequest) (bool, error) {
		updates := map[string]any{}
		putString(updates, "type", item.Type)
		putString(updates, "name", item.Name)
		putString(updates, "reference", item.Reference)
		putNullableTime(updates, "implantation_date", item.ImplantationDate)
		putString(updates, "comment", item.Comment)
		putBool(updates, "is_active", item.IsActive)
		if item.ID > 0 {
			return updateChild(tx, &MedicalDevice{}, record.ID, item.ID, updates)
		}
		if item.Type == nil || *item.Type == "" {
			return false, invalid("medical_device", "type est obligatoire")
		}
		entity := MedicalDevice{MedicalRecordID: record.ID, PatientID: record.PatientID, Type: *item.Type, IsActive: true, CreatedBy: req.authorID}
		applyMedicalDevice(&entity, item)
		return true, tx.Create(&entity).Error
	})
}

func updateVitalSigns(tx *gorm.DB, record *MedicalRecord, req UpdateCommonMedicalRecordRequest) (bool, error) {
	return applyCollection(tx, record.ID, req.VitalSigns, &VitalSign{}, func(item VitalSignRequest) (bool, error) {
		updates := vitalUpdates(item)
		updates["measured_by"] = req.authorID
		if item.ID > 0 {
			return updateChild(tx, &VitalSign{}, record.ID, item.ID, updates)
		}
		entity := VitalSign{MedicalRecordID: record.ID, PatientID: record.PatientID, MeasuredAt: time.Now(), MeasuredBy: req.authorID}
		applyVital(&entity, item)
		if entity.WeightKg != nil && entity.HeightCm != nil && *entity.HeightCm > 0 {
			height := *entity.HeightCm / 100
			bmi := *entity.WeightKg / (height * height)
			entity.BMI = &bmi
		}
		return true, tx.Create(&entity).Error
	})
}

func updateDocuments(tx *gorm.DB, record *MedicalRecord, req UpdateCommonMedicalRecordRequest) (bool, error) {
	return applyCollection(tx, record.ID, req.Documents, &MedicalDocument{}, func(item MedicalDocumentRequest) (bool, error) {
		updates := map[string]any{}
		putNullableUint(updates, "consultation_id", item.ConsultationID)
		putString(updates, "type", item.Type)
		putString(updates, "label", item.Label)
		putNullableTime(updates, "document_date", item.DocumentDate)
		putString(updates, "file_name", item.FileName)
		putString(updates, "mime_type", item.MimeType)
		putString(updates, "file_url", item.FileURL)
		putString(updates, "description", item.Description)
		updates["uploaded_by"] = req.authorID
		if item.ID > 0 {
			return updateChild(tx, &MedicalDocument{}, record.ID, item.ID, updates)
		}
		if item.Label == nil || *item.Label == "" || item.Type == nil || *item.Type == "" {
			return false, invalid("document", "label et type sont obligatoires")
		}
		entity := MedicalDocument{MedicalRecordID: record.ID, PatientID: record.PatientID, Label: *item.Label, Type: *item.Type, UploadedBy: req.authorID}
		applyDocument(&entity, item)
		return true, tx.Create(&entity).Error
	})
}

func applyCollection[T any](tx *gorm.DB, recordID uint, patch PatchCollection[T], model any, upsert func(T) (bool, error)) (bool, error) {
	if !patch.Present {
		return false, nil
	}
	seen := map[uint]struct{}{}
	changed := false
	for _, id := range patch.DeleteIDs {
		if id == 0 {
			return false, invalid("delete_ids", "un identifiant ne peut pas être nul")
		}
		if _, exists := seen[id]; exists {
			return false, invalid("delete_ids", "identifiant dupliqué")
		}
		seen[id] = struct{}{}
		var count int64
		if err := tx.Model(model).Where("id = ? AND medical_record_id = ?", id, recordID).Count(&count).Error; err != nil {
			return false, err
		}
		if count != 1 {
			return false, fmt.Errorf("%w: id=%d", ErrCommonMedicalRecordChild, id)
		}
		if err := tx.Where("id = ? AND medical_record_id = ?", id, recordID).Delete(model).Error; err != nil {
			return false, err
		}
		changed = true
	}
	for _, item := range patch.Upsert {
		itemChanged, err := upsert(item)
		if err != nil {
			return false, err
		}
		changed = changed || itemChanged
	}
	return changed, nil
}

func updateChild(tx *gorm.DB, model any, recordID, id uint, updates map[string]any) (bool, error) {
	var count int64
	if err := tx.Model(model).Where("id = ? AND medical_record_id = ?", id, recordID).Count(&count).Error; err != nil {
		return false, err
	}
	if count != 1 {
		return false, fmt.Errorf("%w: id=%d", ErrCommonMedicalRecordChild, id)
	}
	if len(updates) == 0 {
		return false, nil
	}
	different, err := childUpdatesDiffer(tx, model, recordID, id, updates)
	if err != nil || !different {
		return false, err
	}
	return true, tx.Model(model).Where("id = ? AND medical_record_id = ?", id, recordID).Updates(updates).Error
}

func childUpdatesDiffer(tx *gorm.DB, model any, recordID, id uint, updates map[string]any) (bool, error) {
	keys := make([]string, 0, len(updates))
	for key := range updates {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	conditions := make([]string, 0, len(keys))
	args := make([]any, 0, len(keys)+2)
	args = append(args, id, recordID)
	for _, key := range keys {
		conditions = append(conditions, fmt.Sprintf("%s IS DISTINCT FROM ?", key))
		args = append(args, updates[key])
	}

	var count int64
	err := tx.Model(model).
		Where("id = ? AND medical_record_id = ?", args[0], args[1]).
		Where("("+strings.Join(conditions, " OR ")+")", args[2:]...).
		Count(&count).Error
	return count == 1, err
}

func invalid(section, message string) error {
	return fmt.Errorf("%w: %s: %s", ErrCommonMedicalRecordInvalid, section, message)
}
func putString(m map[string]any, key string, value *string) {
	if value != nil {
		m[key] = *value
	}
}
func putBool(m map[string]any, key string, value *bool) {
	if value != nil {
		m[key] = *value
	}
}
func putUint(m map[string]any, key string, value *uint) {
	if value != nil {
		m[key] = *value
	}
}
func putNullableTime(m map[string]any, key string, value NullableTimePatch) {
	if value.Set {
		m[key] = value.Value
	}
}
func putNullableUint(m map[string]any, key string, value NullableUintPatch) {
	if value.Set {
		m[key] = value.Value
	}
}
func putNullableValue[T any](m map[string]any, key string, value NullableValuePatch[T]) {
	if value.Set {
		m[key] = value.Value
	}
}

func applyAllergy(e *Allergy, p AllergyRequest) {
	if p.Reaction != nil {
		e.Reaction = *p.Reaction
	}
	if p.Severity != nil {
		e.Severity = *p.Severity
	}
	if p.Comment != nil {
		e.Comment = *p.Comment
	}
	if p.IsActive != nil {
		e.IsActive = *p.IsActive
	}
}
func applyMedicalHistory(e *MedicalHistory, p MedicalHistoryRequest) {
	if p.Description != nil {
		e.Description = *p.Description
	}
	if p.StartDate.Set {
		e.StartDate = p.StartDate.Value
	}
	if p.EndDate.Set {
		e.EndDate = p.EndDate.Value
	}
	if p.Status != nil {
		e.Status = *p.Status
	}
	if p.Severity != nil {
		e.Severity = *p.Severity
	}
	if p.Comment != nil {
		e.Comment = *p.Comment
	}
}
func applySurgicalHistory(e *SurgicalHistory, p SurgicalHistoryRequest) {
	if p.ProcedureDate.Set {
		e.ProcedureDate = p.ProcedureDate.Value
	}
	if p.Facility != nil {
		e.Facility = *p.Facility
	}
	if p.Complications != nil {
		e.Complications = *p.Complications
	}
	if p.Comment != nil {
		e.Comment = *p.Comment
	}
}
func applyFamilyHistory(e *FamilyMedicalHistory, p FamilyMedicalHistoryRequest) {
	if p.Relationship != nil {
		e.Relationship = *p.Relationship
	}
	if p.Comment != nil {
		e.Comment = *p.Comment
	}
}
func applyRegularTreatment(e *RegularTreatment, p RegularTreatmentRequest) {
	if p.Dosage != nil {
		e.Dosage = *p.Dosage
	}
	if p.Frequency != nil {
		e.Frequency = *p.Frequency
	}
	if p.StartDate.Set {
		e.StartDate = p.StartDate.Value
	}
	if p.Prescriber != nil {
		e.Prescriber = *p.Prescriber
	}
	if p.IsActive != nil {
		e.IsActive = *p.IsActive
	}
}
func applyVaccination(e *Vaccination, p VaccinationRequest) {
	if p.Dose != nil {
		e.Dose = *p.Dose
	}
	if p.VaccinationDate.Set {
		e.VaccinationDate = p.VaccinationDate.Value
	}
	if p.NextBoosterDate.Set {
		e.NextBoosterDate = p.NextBoosterDate.Value
	}
	if p.Status != nil {
		e.Status = *p.Status
	}
}
func applyDisability(e *Disability, p DisabilityRequest) {
	if p.Level != nil {
		e.Level = *p.Level
	}
	if p.SpecialNeeds != nil {
		e.SpecialNeeds = *p.SpecialNeeds
	}
}
func applyMedicalDevice(e *MedicalDevice, p MedicalDeviceRequest) {
	if p.Name != nil {
		e.Name = *p.Name
	}
	if p.Reference != nil {
		e.Reference = *p.Reference
	}
	if p.ImplantationDate.Set {
		e.ImplantationDate = p.ImplantationDate.Value
	}
	if p.Comment != nil {
		e.Comment = *p.Comment
	}
	if p.IsActive != nil {
		e.IsActive = *p.IsActive
	}
}

func vitalUpdates(p VitalSignRequest) map[string]any {
	m := map[string]any{}
	putNullableUint(m, "consultation_id", p.ConsultationID)
	putNullableValue(m, "weight_kg", p.WeightKg)
	putNullableValue(m, "height_cm", p.HeightCm)
	putNullableValue(m, "temperature_c", p.TemperatureC)
	putNullableValue(m, "systolic_bp", p.SystolicBP)
	putNullableValue(m, "diastolic_bp", p.DiastolicBP)
	putNullableValue(m, "heart_rate", p.HeartRate)
	putNullableValue(m, "respiratory_rate", p.RespiratoryRate)
	putNullableValue(m, "oxygen_saturation", p.OxygenSaturation)
	putNullableValue(m, "blood_glucose", p.BloodGlucose)
	putNullableValue(m, "waist_circumference_cm", p.WaistCircumferenceCm)
	putNullableValue(m, "pain_score", p.PainScore)
	putString(m, "pain_location", p.PainLocation)
	putString(m, "pain_type", p.PainType)
	putString(m, "pain_duration", p.PainDuration)
	putNullableTime(m, "measured_at", p.MeasuredAt)
	putString(m, "comment", p.Comment)
	if p.WeightKg.Set && p.HeightCm.Set {
		m["bmi"] = nil
		if p.WeightKg.Value != nil && p.HeightCm.Value != nil && *p.HeightCm.Value > 0 {
			height := *p.HeightCm.Value / 100
			m["bmi"] = *p.WeightKg.Value / (height * height)
		}
	}
	return m
}

func applyVital(e *VitalSign, p VitalSignRequest) {
	if p.ConsultationID.Set {
		e.ConsultationID = p.ConsultationID.Value
	}
	if p.WeightKg.Set {
		e.WeightKg = p.WeightKg.Value
	}
	if p.HeightCm.Set {
		e.HeightCm = p.HeightCm.Value
	}
	if p.TemperatureC.Set {
		e.TemperatureC = p.TemperatureC.Value
	}
	if p.SystolicBP.Set {
		e.SystolicBP = p.SystolicBP.Value
	}
	if p.DiastolicBP.Set {
		e.DiastolicBP = p.DiastolicBP.Value
	}
	if p.HeartRate.Set {
		e.HeartRate = p.HeartRate.Value
	}
	if p.RespiratoryRate.Set {
		e.RespiratoryRate = p.RespiratoryRate.Value
	}
	if p.OxygenSaturation.Set {
		e.OxygenSaturation = p.OxygenSaturation.Value
	}
	if p.BloodGlucose.Set {
		e.BloodGlucose = p.BloodGlucose.Value
	}
	if p.WaistCircumferenceCm.Set {
		e.WaistCircumferenceCm = p.WaistCircumferenceCm.Value
	}
	if p.PainScore.Set {
		e.PainScore = p.PainScore.Value
	}
	if p.PainLocation != nil {
		e.PainLocation = *p.PainLocation
	}
	if p.PainType != nil {
		e.PainType = *p.PainType
	}
	if p.PainDuration != nil {
		e.PainDuration = *p.PainDuration
	}
	if p.MeasuredAt.Set && p.MeasuredAt.Value != nil {
		e.MeasuredAt = *p.MeasuredAt.Value
	}
	if p.Comment != nil {
		e.Comment = *p.Comment
	}
}
func applyDocument(e *MedicalDocument, p MedicalDocumentRequest) {
	if p.ConsultationID.Set {
		e.ConsultationID = p.ConsultationID.Value
	}
	if p.DocumentDate.Set {
		e.DocumentDate = p.DocumentDate.Value
	}
	if p.FileName != nil {
		e.FileName = *p.FileName
	}
	if p.MimeType != nil {
		e.MimeType = *p.MimeType
	}
	if p.FileURL != nil {
		e.FileURL = *p.FileURL
	}
	if p.Description != nil {
		e.Description = *p.Description
	}
}
