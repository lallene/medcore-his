package medical_records

import (
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

type Service interface {
	CreateMedicalRecord(patientID uint) (*MedicalRecord, error)
	GetOrCreateMedicalRecord(patientID uint) (*MedicalRecord, error)
	GetOverview(recordID uint) (*MedicalRecordOverviewResponse, error)

	AddAlert(recordID uint, req CreateAlertRequest) (*MedicalAlert, error)
	AddAllergy(recordID uint, req CreateAllergyRequest) (*Allergy, error)
	AddMedicalHistory(recordID uint, req CreateMedicalHistoryRequest) (*MedicalHistory, error)
	AddVitalSign(recordID uint, req CreateVitalSignRequest) (*VitalSign, error)
	ListTimelineEvents(recordID uint) ([]MedicalTimelineEvent, error)

	ListVitalSigns(recordID uint) ([]VitalSign, error)
	RecordConsultationCreated(patientID uint, consultationID uint, department string, doctorName string) error
	RecordConsultationStatusChanged(patientID uint, consultationID uint, oldStatus string, newStatus string) error
	RecordExamRequested(patientID uint, consultationID uint, examName string, service string) error
	RecordMedicationPrescribed(patientID uint, consultationID uint, medicationName string, dosage string, service string) error
	GetPatientMedicalSummary(patientID uint) (*PatientMedicalSummaryResponse, error)
}

type service struct {
	repo Repository
}

func NewService(repo Repository) Service {
	return &service{repo: repo}
}

func (s *service) CreateMedicalRecord(patientID uint) (*MedicalRecord, error) {
	existing, err := s.repo.GetMedicalRecordByPatientID(patientID)
	if err == nil {
		return existing, nil
	}

	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	record := &MedicalRecord{
		PatientID:    patientID,
		RecordNumber: generateRecordNumber(patientID),
		Status:       "active",
	}

	if err := s.repo.CreateMedicalRecord(record); err != nil {
		return nil, err
	}

	return record, nil
}

func (s *service) GetOrCreateMedicalRecord(patientID uint) (*MedicalRecord, error) {
	return s.CreateMedicalRecord(patientID)
}

func (s *service) GetOverview(recordID uint) (*MedicalRecordOverviewResponse, error) {
	record, err := s.repo.GetMedicalRecordByID(recordID)
	if err != nil {
		return nil, err
	}

	alerts, _ := s.repo.ListAlerts(recordID)
	allergies, _ := s.repo.ListAllergies(recordID)
	histories, _ := s.repo.ListMedicalHistories(recordID)

	lastVital, err := s.repo.GetLastVitalSign(recordID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		lastVital = nil
	} else if err != nil {
		return nil, err
	}

	return &MedicalRecordOverviewResponse{
		MedicalRecord:    *record,
		Alerts:           alerts,
		Allergies:        allergies,
		MedicalHistories: histories,
		LastVitalSigns:   lastVital,
	}, nil
}

func (s *service) AddAlert(recordID uint, req CreateAlertRequest) (*MedicalAlert, error) {
	record, err := s.repo.GetMedicalRecordByID(recordID)
	if err != nil {
		return nil, err
	}

	if req.Severity == "" {
		req.Severity = "medium"
	}

	alert := &MedicalAlert{
		MedicalRecordID: record.ID,
		PatientID:       record.PatientID,
		Type:            req.Type,
		Title:           req.Title,
		Description:     req.Description,
		Severity:        req.Severity,
		IsActive:        true,
		CreatedBy:       req.CreatedBy,
	}

	if err := s.repo.CreateAlert(alert); err != nil {
		return nil, err
	}

	return alert, nil
}

func (s *service) AddAllergy(recordID uint, req CreateAllergyRequest) (*Allergy, error) {
	record, err := s.repo.GetMedicalRecordByID(recordID)
	if err != nil {
		return nil, err
	}

	if req.Severity == "" {
		req.Severity = "medium"
	}

	allergy := &Allergy{
		MedicalRecordID: record.ID,
		PatientID:       record.PatientID,
		AllergenType:    req.AllergenType,
		AllergenName:    req.AllergenName,
		Reaction:        req.Reaction,
		Severity:        req.Severity,
		Comment:         req.Comment,
		IsActive:        true,
		CreatedBy:       req.CreatedBy,
	}

	if err := s.repo.CreateAllergy(allergy); err != nil {
		return nil, err
	}

	_ = s.createTimelineEvent(
		record,
		"allergy_added",
		"allergy",
		"Allergie ajoutée",
		fmt.Sprintf(
			"%s — réaction : %s",
			allergy.AllergenName,
			allergy.Reaction,
		),
		"allergy",
		allergy.ID,
		allergy.Severity,
		req.CreatedBy,
	)

	return allergy, nil
}

func (s *service) AddMedicalHistory(recordID uint, req CreateMedicalHistoryRequest) (*MedicalHistory, error) {
	record, err := s.repo.GetMedicalRecordByID(recordID)
	if err != nil {
		return nil, err
	}

	if req.Status == "" {
		req.Status = "active"
	}

	if req.Severity == "" {
		req.Severity = "medium"
	}

	history := &MedicalHistory{
		MedicalRecordID: record.ID,
		PatientID:       record.PatientID,
		Type:            req.Type,
		Title:           req.Title,
		Description:     req.Description,
		StartDate:       req.StartDate,
		EndDate:         req.EndDate,
		Status:          req.Status,
		Severity:        req.Severity,
		Comment:         req.Comment,
		CreatedBy:       req.CreatedBy,
	}

	if err := s.repo.CreateMedicalHistory(history); err != nil {
		return nil, err
	}
	_ = s.createTimelineEvent(
		record,
		"medical_history_added",
		"medical_history",
		"Antécédent ajouté",
		history.Title,
		"medical_history",
		history.ID,
		history.Severity,
		req.CreatedBy,
	)

	return history, nil
}

func (s *service) AddVitalSign(recordID uint, req CreateVitalSignRequest) (*VitalSign, error) {
	record, err := s.repo.GetMedicalRecordByID(recordID)
	if err != nil {
		return nil, err
	}

	measuredAt := time.Now()
	if req.MeasuredAt != nil {
		measuredAt = *req.MeasuredAt
	}

	vital := &VitalSign{
		MedicalRecordID:      record.ID,
		PatientID:            record.PatientID,
		ConsultationID:       req.ConsultationID,
		WeightKg:             req.WeightKg,
		HeightCm:             req.HeightCm,
		TemperatureC:         req.TemperatureC,
		SystolicBP:           req.SystolicBP,
		DiastolicBP:          req.DiastolicBP,
		HeartRate:            req.HeartRate,
		RespiratoryRate:      req.RespiratoryRate,
		OxygenSaturation:     req.OxygenSaturation,
		BloodGlucose:         req.BloodGlucose,
		WaistCircumferenceCm: req.WaistCircumferenceCm,
		PainScore:            req.PainScore,
		MeasuredBy:           req.MeasuredBy,
		MeasuredAt:           measuredAt,
		Comment:              req.Comment,
	}

	if req.WeightKg != nil && req.HeightCm != nil && *req.HeightCm > 0 {
		heightM := *req.HeightCm / 100
		bmi := *req.WeightKg / (heightM * heightM)
		vital.BMI = &bmi
	}

	if err := s.repo.CreateVitalSign(vital); err != nil {
		return nil, err
	}

	description := fmt.Sprintf(
		"TA %s — FC %s — Température %s — SpO2 %s",
		formatBloodPressure(vital.SystolicBP, vital.DiastolicBP),
		formatIntValue(vital.HeartRate, " bpm"),
		formatFloatValue(vital.TemperatureC, " °C"),
		formatFloatValue(vital.OxygenSaturation, " %"),
	)

	_ = s.createTimelineEvent(
		record,
		"vital_signs_recorded",
		"vital_signs",
		"Constantes enregistrées",
		description,
		"vital_sign",
		vital.ID,
		"info",
		req.MeasuredBy,
	)

	return vital, nil
}

func (s *service) ListVitalSigns(recordID uint) ([]VitalSign, error) {
	return s.repo.ListVitalSigns(recordID)
}

func generateRecordNumber(patientID uint) string {
	return fmt.Sprintf("DM-%d-%d", time.Now().Year(), patientID)
}

func (s *service) createTimelineEvent(
	record *MedicalRecord,
	eventType string,
	category string,
	title string,
	description string,
	referenceType string,
	referenceID uint,
	severity string,
	createdBy uint,
) error {
	event := &MedicalTimelineEvent{
		MedicalRecordID: record.ID,
		PatientID:       record.PatientID,
		EventType:       eventType,
		Category:        category,
		Title:           title,
		Description:     description,
		ReferenceType:   referenceType,
		ReferenceID:     &referenceID,
		Severity:        severity,
		EventDate:       time.Now(),
		CreatedBy:       createdBy,
	}

	return s.repo.CreateTimelineEvent(event)
}

func formatBloodPressure(systolic, diastolic *int) string {
	if systolic == nil || diastolic == nil {
		return "non renseignée"
	}

	return fmt.Sprintf("%d/%d mmHg", *systolic, *diastolic)
}

func formatIntValue(value *int, unit string) string {
	if value == nil {
		return "non renseigné"
	}

	return fmt.Sprintf("%d%s", *value, unit)
}

func formatFloatValue(value *float64, unit string) string {
	if value == nil {
		return "non renseigné"
	}

	return fmt.Sprintf("%.1f%s", *value, unit)
}

func (s *service) ListTimelineEvents(recordID uint) ([]MedicalTimelineEvent, error) {
	if _, err := s.repo.GetMedicalRecordByID(recordID); err != nil {
		return nil, err
	}

	return s.repo.ListTimelineEvents(recordID)
}

func (s *service) RecordConsultationCreated(
	patientID uint,
	consultationID uint,
	department string,
	doctorName string,
) error {
	record, err := s.GetOrCreateMedicalRecord(patientID)
	if err != nil {
		return err
	}

	title := "Consultation créée"

	description := "Nouvelle consultation"

	if department != "" {
		description = fmt.Sprintf(
			"Nouvelle consultation — Service : %s",
			department,
		)
	}

	if doctorName != "" {
		description += fmt.Sprintf(
			" — Médecin : %s",
			doctorName,
		)
	}

	return s.createTimelineEvent(
		record,
		"consultation_created",
		"consultation",
		title,
		description,
		"consultation",
		consultationID,
		"info",
		0,
	)
}
func (s *service) RecordConsultationStatusChanged(
	patientID uint,
	consultationID uint,
	oldStatus string,
	newStatus string,
) error {
	record, err := s.GetOrCreateMedicalRecord(patientID)
	if err != nil {
		return err
	}

	description := fmt.Sprintf(
		"Statut consultation : %s → %s",
		oldStatus,
		newStatus,
	)

	return s.createTimelineEvent(
		record,
		"consultation_status_changed",
		"consultation",
		"Statut de consultation modifié",
		description,
		"consultation",
		consultationID,
		"info",
		0,
	)
}

func (s *service) RecordExamRequested(
	patientID uint,
	consultationID uint,
	examName string,
	service string,
) error {
	record, err := s.GetOrCreateMedicalRecord(patientID)
	if err != nil {
		return err
	}

	description := fmt.Sprintf("Examen demandé : %s", examName)

	if service != "" {
		description = fmt.Sprintf(
			"Examen demandé : %s — Service : %s",
			examName,
			service,
		)
	}

	return s.createTimelineEvent(
		record,
		"exam_requested",
		"exam",
		"Examen prescrit",
		description,
		"consultation",
		consultationID,
		"info",
		0,
	)
}

func (s *service) RecordMedicationPrescribed(
	patientID uint,
	consultationID uint,
	medicationName string,
	dosage string,
	service string,
) error {
	record, err := s.GetOrCreateMedicalRecord(patientID)
	if err != nil {
		return err
	}

	description := fmt.Sprintf("Médicament prescrit : %s", medicationName)

	if dosage != "" {
		description = fmt.Sprintf(
			"Médicament prescrit : %s %s",
			medicationName,
			dosage,
		)
	}

	if service != "" {
		description += fmt.Sprintf(" — Service : %s", service)
	}

	return s.createTimelineEvent(
		record,
		"medication_prescribed",
		"prescription",
		"Médicament prescrit",
		description,
		"consultation",
		consultationID,
		"info",
		0,
	)
}

func (s *service) GetPatientMedicalSummary(patientID uint) (*PatientMedicalSummaryResponse, error) {
	record, err := s.GetOrCreateMedicalRecord(patientID)
	if err != nil {
		return nil, err
	}

	alerts, _ := s.repo.ListAlerts(record.ID)
	allergies, _ := s.repo.ListAllergies(record.ID)
	histories, _ := s.repo.ListMedicalHistories(record.ID)

	lastVital, err := s.repo.GetLastVitalSign(record.ID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		lastVital = nil
	} else if err != nil {
		return nil, err
	}

	timeline, err := s.repo.ListRecentTimelineEvents(record.ID, 20)
	if err != nil {
		return nil, err
	}

	return &PatientMedicalSummaryResponse{
		MedicalRecord:    *record,
		Alerts:           alerts,
		Allergies:        allergies,
		MedicalHistories: histories,
		LastVitalSigns:   lastVital,
		Timeline:         timeline,
	}, nil
}
