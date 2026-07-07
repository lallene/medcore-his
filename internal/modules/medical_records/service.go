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

	ListVitalSigns(recordID uint) ([]VitalSign, error)
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

	return vital, nil
}

func (s *service) ListVitalSigns(recordID uint) ([]VitalSign, error) {
	return s.repo.ListVitalSigns(recordID)
}

func generateRecordNumber(patientID uint) string {
	return fmt.Sprintf("DM-%d-%d", time.Now().Year(), patientID)
}
