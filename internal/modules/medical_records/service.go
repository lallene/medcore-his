package medical_records

import (
	"errors"
	"fmt"
	"strings"
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
	RecordConsultationSOAPUpdated(
		patientID uint,
		consultationID uint,
		updatedBy uint,
	) error

	ListVitalSigns(recordID uint) ([]VitalSign, error)
	RecordConsultationCreated(patientID uint, consultationID uint, department string, doctorName string) error
	RecordConsultationStatusChanged(patientID uint, consultationID uint, oldStatus string, newStatus string) error
	RecordExamRequested(patientID uint, consultationID uint, examName string, service string) error
	RecordMedicationPrescribed(patientID uint, consultationID uint, medicationName string, dosage string, service string) error
	GetPatientMedicalSummary(patientID uint) (*PatientMedicalSummaryResponse, error)
	RecordConsultationSpecialtyUpdated(patientID uint, consultationID uint, specialtyCode string, updatedBy uint) error
	GetPatientSummary(
		patientID uint,
	) (*PatientSummaryResponse, error)

	GetCommonMedicalRecord(
		patientID uint,
	) (*CommonMedicalRecordResponse, error)

	UpdateCommonMedicalRecord(
		patientID uint,
		req UpdateCommonMedicalRecordRequest,
	) (*CommonMedicalRecordResponse, error)
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

	recentConsultations, err := s.repo.ListRecentConsultations(patientID, 10)
	if err != nil {
		return nil, err
	}

	documents := make([]MedicalSummaryDocumentItem, 0)

	for _, consultation := range recentConsultations {
		base := "/api/consultations/" + fmt.Sprintf("%d", consultation.ID)

		documents = append(documents, MedicalSummaryDocumentItem{
			ConsultationID: consultation.ID,
			Type:           "report",
			Label:          "Compte rendu de consultation",
			URL:            base + "/report/pdf",
		})

		if consultation.HasPrescriptions {
			documents = append(documents, MedicalSummaryDocumentItem{
				ConsultationID: consultation.ID,
				Type:           "prescription",
				Label:          "Ordonnance",
				URL:            base + "/prescription/pdf",
			})
		}

		if consultation.HasExams {
			documents = append(documents, MedicalSummaryDocumentItem{
				ConsultationID: consultation.ID,
				Type:           "exam_request",
				Label:          "Demande / autorisation d'examens",
				URL:            base + "/exam-request/pdf",
			})
		}

		if consultation.SickLeaveRequired {
			documents = append(documents, MedicalSummaryDocumentItem{
				ConsultationID: consultation.ID,
				Type:           "sick_leave",
				Label:          "Fiche de repos maladie",
				URL:            base + "/sick-leave/pdf",
			})
		}

		if consultation.HospitalizationRequired {
			documents = append(documents, MedicalSummaryDocumentItem{
				ConsultationID: consultation.ID,
				Type:           "hospitalization",
				Label:          "Fiche d'hospitalisation",
				URL:            base + "/hospitalization/pdf",
			})
		}
	}

	return &PatientMedicalSummaryResponse{
		PatientID:           patientID,
		MedicalRecord:       *record,
		Alerts:              alerts,
		Allergies:           allergies,
		MedicalHistories:    histories,
		LastVitalSigns:      lastVital,
		Timeline:            timeline,
		RecentConsultations: recentConsultations,
		Documents:           documents,
	}, nil
}

func (s *service) RecordConsultationSOAPUpdated(
	patientID uint,
	consultationID uint,
	updatedBy uint,
) error {
	record, err := s.GetOrCreateMedicalRecord(patientID)
	if err != nil {
		return err
	}

	return s.createTimelineEvent(
		record,
		"soap_updated",
		"consultation",
		"Note clinique SOAP mise à jour",
		"Les informations cliniques Subjectif, Objectif, Évaluation et Plan ont été mises à jour.",
		"consultation",
		consultationID,
		"info",
		updatedBy,
	)
}

func (s *service) RecordConsultationSpecialtyUpdated(
	patientID uint,
	consultationID uint,
	specialtyCode string,
	updatedBy uint,
) error {
	record, err := s.GetOrCreateMedicalRecord(patientID)
	if err != nil {
		return err
	}

	return s.createTimelineEvent(
		record,
		"specialty_data_updated",
		"consultation",
		"Volet de spécialité mis à jour",
		"Données de spécialité enregistrées : "+specialtyCode,
		"consultation",
		consultationID,
		"info",
		updatedBy,
	)
}

const (
	SpecialtyGeneralMedicine = "GENERAL_MEDICINE"
	SpecialtyCardiology      = "CARDIOLOGY"
	SpecialtyGynecology      = "GYNECOLOGY"
	SpecialtyPediatrics      = "PEDIATRICS"
	SpecialtyNutrition       = "NUTRITION"
	SpecialtyNeurology       = "NEUROLOGY"
	SpecialtySurgery         = "SURGERY"
)

func (s *service) GetCommonMedicalRecord(
	patientID uint,
) (*CommonMedicalRecordResponse, error) {
	record, err := s.GetOrCreateMedicalRecord(patientID)
	if err != nil {
		return nil, err
	}

	return s.repo.GetCommonMedicalRecord(record.ID)
}

func (s *service) UpdateCommonMedicalRecord(
	patientID uint,
	req UpdateCommonMedicalRecordRequest,
) (*CommonMedicalRecordResponse, error) {
	record, err := s.GetOrCreateMedicalRecord(patientID)
	if err != nil {
		return nil, err
	}

	if err := s.repo.SaveCommonMedicalRecord(record, req); err != nil {
		return nil, err
	}

	_ = s.createTimelineEvent(
		record,
		"common_medical_record_updated",
		"medical_record",
		"Dossier médical mis à jour",
		"Les informations longitudinales du patient ont été mises à jour.",
		"medical_record",
		record.ID,
		"info",
		req.UpdatedBy,
	)

	return s.repo.GetCommonMedicalRecord(record.ID)
}

func (s *service) GetPatientSummary(
	patientID uint,
) (*PatientSummaryResponse, error) {
	patient, err := s.repo.GetPatientSummaryIdentity(patientID)
	if err != nil {
		return nil, err
	}

	record, err := s.GetOrCreateMedicalRecord(patientID)
	if err != nil {
		return nil, err
	}

	allergies, err := s.repo.ListAllergies(record.ID)
	if err != nil {
		return nil, err
	}

	histories, err := s.repo.ListMedicalHistories(record.ID)
	if err != nil {
		return nil, err
	}

	commonRecord, err := s.repo.GetCommonMedicalRecord(record.ID)
	if err != nil {
		return nil, err
	}

	lastVital, err := s.repo.GetLastVitalSign(record.ID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		lastVital = nil
	} else if err != nil {
		return nil, err
	}

	lastConsultation, err := s.repo.GetLastConsultationSummary(patientID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		lastConsultation = nil
	} else if err != nil {
		return nil, err
	}

	statistics, err := s.repo.GetPatientSummaryStatistics(
		patientID,
		record.ID,
	)
	if err != nil {
		return nil, err
	}

	allergyItems := make([]AllergySummary, 0, len(allergies))

	for _, allergy := range allergies {
		allergyItems = append(allergyItems, AllergySummary{
			ID:         allergy.ID,
			Name:       allergy.AllergenName,
			Type:       allergy.AllergenType,
			Reaction:   allergy.Reaction,
			Severity:   allergy.Severity,
			IsHighRisk: isHighRiskAllergy(allergy.Severity),
		})
	}

	chronicDiseases := make([]ChronicDiseaseSummary, 0)

	for _, history := range histories {
		if !isChronicMedicalHistory(history) {
			continue
		}

		chronicDiseases = append(
			chronicDiseases,
			ChronicDiseaseSummary{
				ID:          history.ID,
				Name:        history.Title,
				Severity:    history.Severity,
				DiagnosedAt: history.StartDate,
			},
		)
	}

	activeTreatments := make([]TreatmentSummary, 0)

	for _, treatment := range commonRecord.RegularTreatments {
		if !treatment.IsActive {
			continue
		}

		activeTreatments = append(
			activeTreatments,
			TreatmentSummary{
				ID:             treatment.ID,
				MedicationName: treatment.MedicationName,
				Dosage:         treatment.Dosage,
				Frequency:      treatment.Frequency,
				StartDate:      treatment.StartDate,
				Prescriber:     treatment.Prescriber,
			},
		)
	}

	alerts := buildClinicalAlerts(
		allergies,
		lastVital,
	)

	var lastVitalSummary *LastVitalSummary

	if lastVital != nil {
		lastVitalSummary = &LastVitalSummary{
			ID:                   lastVital.ID,
			MeasuredAt:           lastVital.MeasuredAt,
			WeightKg:             lastVital.WeightKg,
			HeightCm:             lastVital.HeightCm,
			BMI:                  lastVital.BMI,
			SystolicBP:           lastVital.SystolicBP,
			DiastolicBP:          lastVital.DiastolicBP,
			HeartRate:            lastVital.HeartRate,
			Temperature:          lastVital.TemperatureC,
			RespiratoryRate:      lastVital.RespiratoryRate,
			OxygenSaturation:     lastVital.OxygenSaturation,
			BloodGlucose:         lastVital.BloodGlucose,
			PainScore:            lastVital.PainScore,
			WaistCircumferenceCm: lastVital.WaistCircumferenceCm,
		}
	}

	return &PatientSummaryResponse{
		Patient: *patient,

		MedicalRecord: MedicalRecordSummary{
			ID:                record.ID,
			RecordNumber:      record.RecordNumber,
			Status:            record.Status,
			ActiveAllergies:   int64(len(allergyItems)),
			ChronicDiseases:   int64(len(chronicDiseases)),
			CurrentTreatments: int64(len(activeTreatments)),
		},

		Allergies:        allergyItems,
		ChronicDiseases:  chronicDiseases,
		ActiveTreatments: activeTreatments,
		LastVitals:       lastVitalSummary,
		LastConsultation: lastConsultation,
		ClinicalAlerts:   alerts,
		Statistics:       *statistics,
	}, nil
}

func isHighRiskAllergy(severity string) bool {
	switch normalizeClinicalValue(severity) {
	case "high", "severe", "critical", "anaphylaxis":
		return true
	default:
		return false
	}
}

func isChronicMedicalHistory(history MedicalHistory) bool {
	historyType := normalizeClinicalValue(history.Type)
	status := normalizeClinicalValue(history.Status)

	return historyType == "chronic" &&
		status != "resolved" &&
		status != "inactive"
}

func normalizeClinicalValue(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func buildClinicalAlerts(
	allergies []Allergy,
	vital *VitalSign,
) []ClinicalAlertSummary {
	alerts := make([]ClinicalAlertSummary, 0)

	for _, allergy := range allergies {
		if !isHighRiskAllergy(allergy.Severity) {
			continue
		}

		description := allergy.AllergenName

		if allergy.Reaction != "" {
			description += " — " + allergy.Reaction
		}

		alerts = append(alerts, ClinicalAlertSummary{
			Severity:    "critical",
			Code:        "HIGH_RISK_ALLERGY",
			Title:       "Allergie à haut risque",
			Description: description,
			Source:      "allergy",
		})
	}

	if vital == nil {
		return alerts
	}

	if vital.SystolicBP != nil && *vital.SystolicBP >= 180 {
		alerts = append(alerts, ClinicalAlertSummary{
			Severity: "critical",
			Code:     "SEVERE_HYPERTENSION",
			Title:    "Hypertension sévère",
			Description: fmt.Sprintf(
				"Pression artérielle systolique mesurée à %d mmHg.",
				*vital.SystolicBP,
			),
			Source: "vital_sign",
		})
	}

	if vital.DiastolicBP != nil && *vital.DiastolicBP >= 120 {
		alerts = append(alerts, ClinicalAlertSummary{
			Severity: "critical",
			Code:     "SEVERE_DIASTOLIC_HYPERTENSION",
			Title:    "Hypertension diastolique sévère",
			Description: fmt.Sprintf(
				"Pression artérielle diastolique mesurée à %d mmHg.",
				*vital.DiastolicBP,
			),
			Source: "vital_sign",
		})
	}

	if vital.OxygenSaturation != nil &&
		*vital.OxygenSaturation < 90 {
		alerts = append(alerts, ClinicalAlertSummary{
			Severity: "critical",
			Code:     "LOW_OXYGEN_SATURATION",
			Title:    "Saturation en oxygène basse",
			Description: fmt.Sprintf(
				"SpO₂ mesurée à %.1f %%.",
				*vital.OxygenSaturation,
			),
			Source: "vital_sign",
		})
	}

	if vital.TemperatureC != nil &&
		*vital.TemperatureC >= 39 {
		alerts = append(alerts, ClinicalAlertSummary{
			Severity: "high",
			Code:     "HIGH_FEVER",
			Title:    "Fièvre élevée",
			Description: fmt.Sprintf(
				"Température mesurée à %.1f °C.",
				*vital.TemperatureC,
			),
			Source: "vital_sign",
		})
	}

	if vital.BMI != nil && *vital.BMI >= 40 {
		alerts = append(alerts, ClinicalAlertSummary{
			Severity: "high",
			Code:     "SEVERE_OBESITY",
			Title:    "Obésité sévère",
			Description: fmt.Sprintf(
				"IMC calculé à %.1f kg/m².",
				*vital.BMI,
			),
			Source: "vital_sign",
		})
	}

	if vital.PainScore != nil && *vital.PainScore >= 8 {
		alerts = append(alerts, ClinicalAlertSummary{
			Severity: "high",
			Code:     "SEVERE_PAIN",
			Title:    "Douleur intense",
			Description: fmt.Sprintf(
				"Score de douleur évalué à %d/10.",
				*vital.PainScore,
			),
			Source: "vital_sign",
		})
	}

	if vital.TemperatureC != nil &&
		vital.HeartRate != nil &&
		vital.RespiratoryRate != nil &&
		*vital.TemperatureC >= 39 &&
		*vital.HeartRate >= 120 &&
		*vital.RespiratoryRate >= 30 {
		alerts = append(alerts, ClinicalAlertSummary{
			Severity:    "critical",
			Code:        "SEPSIS_WARNING",
			Title:       "Signes cliniques préoccupants",
			Description: "Fièvre élevée associée à une tachycardie et une fréquence respiratoire élevée. Une évaluation médicale urgente est nécessaire.",
			Source:      "clinical_rule",
		})
	}

	return alerts
}
