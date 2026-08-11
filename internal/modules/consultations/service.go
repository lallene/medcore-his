package consultations

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/medical_records"
	"gorm.io/gorm"
)

var (
	ErrConsultationNotFound = errors.New(
		"consultation introuvable",
	)

	ErrInvalidTransition = errors.New(
		"transition de statut non autorisée",
	)

	ErrConsultationLocked = errors.New(
		"la consultation est verrouillée",
	)

	ErrCancellationReasonRequired = errors.New(
		"le motif d'annulation est obligatoire",
	)

	ErrInvalidSickLeave = errors.New(
		"le nombre de jours de repos doit être supérieur à zéro",
	)

	ErrInvalidReasonIDs = errors.New(
		"un ou plusieurs motifs sont invalides ou inactifs",
	)

	ErrInvalidExamIDs = errors.New(
		"un ou plusieurs examens sont invalides ou inactifs",
	)

	ErrInvalidPresentationID    = errors.New("présentation pharmaceutique invalide")
	ErrInactivePresentation     = errors.New("présentation pharmaceutique inactive")
	ErrPhysicalExamAreaNotFound = errors.New("zone d'examen physique introuvable")
	ErrInactivePhysicalExamArea = errors.New("zone d'examen physique inactive")
)

type Service struct {
	repo                  *Repository
	medicalRecordsService medical_records.Service
}

func NewService(
	repo *Repository,
	medicalRecordsService medical_records.Service,
) *Service {
	return &Service{
		repo:                  repo,
		medicalRecordsService: medicalRecordsService,
	}
}

func (s *Service) GetReasons() ([]ConsultationReason, error) {
	return s.repo.FindReasons()
}

func (s *Service) GetExams() ([]MedicalExam, error) {
	return s.repo.FindExams()
}

func (s *Service) ListConsultations(filter ConsultationListFilter) (*ConsultationListResult, error) {
	return s.repo.List(filter)
}

func (s *Service) CreateConsultation(req CreateConsultationRequest, authorID uint) (*Consultation, error) {
	reasons, err := s.repo.FindReasonsByIDs(req.ReasonIDs)
	if err != nil {
		return nil, err
	}

	exams, err := s.repo.FindExamsByIDs(req.ExamIDs)
	if err != nil {
		return nil, err
	}

	var startDate *time.Time
	var endDate *time.Time

	if req.SickLeaveRequired && req.SickLeaveDays > 0 {
		start := time.Now()
		end := start.AddDate(0, 0, req.SickLeaveDays)

		startDate = &start
		endDate = &end
	}

	prescriptions := make([]ConsultationPrescription, 0)

	for _, item := range req.Prescriptions {
		presentation, err := s.repo.FindMedicationPresentationByID(item.PresentationID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, ErrInvalidPresentationID
			}

			return nil, err
		}

		if !presentation.IsActive || !presentation.Medication.IsActive {
			return nil, ErrInactivePresentation
		}

		presentationID := presentation.ID

		prescriptions = append(prescriptions, ConsultationPrescription{
			PresentationID: &presentationID,

			MedicationName: presentation.Medication.Name,
			Dosage:         presentation.Dosage,
			Form:           presentation.Form,
			Route:          presentation.Route,

			Quantity: item.Quantity,

			Duration:     item.Duration,
			Instructions: item.Instructions,
		})
	}

	consultation := &Consultation{
		PatientID:  req.PatientID,
		DoctorName: req.DoctorName,
		Service:    req.Service,
		Status:     "draft",

		Diagnosis:    req.Diagnosis,
		Observations: req.Observations,
		Treatment:    req.Treatment,

		SickLeaveRequired:  req.SickLeaveRequired,
		SickLeaveDays:      req.SickLeaveDays,
		SickLeaveStartDate: startDate,
		SickLeaveEndDate:   endDate,

		Reasons:                 reasons,
		Exams:                   exams,
		Prescriptions:           prescriptions,
		HospitalizationRequired: req.HospitalizationRequired,
		HospitalizationReason:   req.HospitalizationReason,
		HospitalizationType:     req.HospitalizationType,
		HospitalizationDuration: req.HospitalizationDuration,

		Vitals: ConsultationVitals{
			Temperature:            req.Vitals.Temperature,
			BloodPressureSystolic:  req.Vitals.BloodPressureSystolic,
			BloodPressureDiastolic: req.Vitals.BloodPressureDiastolic,
			HeartRate:              req.Vitals.HeartRate,
			RespiratoryRate:        req.Vitals.RespiratoryRate,
			OxygenSaturation:       req.Vitals.OxygenSaturation,
			Weight:                 req.Vitals.Weight,
			Height:                 req.Vitals.Height,
			BloodGlucose:           req.Vitals.BloodGlucose,
			PainScore:              req.Vitals.PainScore,
		},
	}

	if req.Antecedent != nil {
		consultation.Antecedent = ConsultationAntecedent{
			PreviousMedication:     req.Antecedent.PreviousMedication,
			HasHTA:                 req.Antecedent.HasHTA,
			HasDiabetes:            req.Antecedent.HasDiabetes,
			OtherMedical:           req.Antecedent.OtherMedical,
			SurgicalHistory:        req.Antecedent.SurgicalHistory,
			GynecoObstetricHistory: req.Antecedent.GynecoObstetricHistory,
			DDR:                    req.Antecedent.DDR,
			PregnancyOngoing:       req.Antecedent.PregnancyOngoing,
			Tobacco:                req.Antecedent.Tobacco,
			Alcohol:                req.Antecedent.Alcohol,
			VisitType:              req.Antecedent.VisitType,
		}
	}

	for _, item := range req.PhysicalExams {
		area, err := s.repo.FindPhysicalExamAreaByID(item.AreaID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, ErrPhysicalExamAreaNotFound
			}

			return nil, err
		}

		if !area.IsActive {
			return nil, ErrInactivePhysicalExamArea
		}

		consultation.PhysicalExams = append(
			consultation.PhysicalExams,
			ConsultationPhysicalExam{
				AreaID:      area.ID,
				Observation: item.Observation,
			},
		)
	}

	for _, item := range req.AdministeredTreatments {
		presentation, err := s.repo.FindMedicationPresentationByID(item.PresentationID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, ErrInvalidPresentationID
			}

			return nil, err
		}

		if !presentation.IsActive || !presentation.Medication.IsActive {
			return nil, ErrInactivePresentation
		}

		presentationID := presentation.ID

		consultation.AdministeredTreatments = append(
			consultation.AdministeredTreatments,
			ConsultationAdministeredTreatment{
				PresentationID: &presentationID,
				MedicationName: presentation.Medication.Name,
				Dosage:         presentation.Dosage,
				Form:           presentation.Form,
				Route:          presentation.Route,
				Quantity:       item.Quantity,
				Instructions:   item.Instructions,
			},
		)
	}

	for _, item := range req.PreviousMedications {
		previousMedication, err := s.buildPreviousMedication(item)
		if err != nil {
			return nil, err
		}

		consultation.PreviousMedications = append(
			consultation.PreviousMedications,
			*previousMedication,
		)
	}

	for _, item := range req.SurgicalHistories {
		consultation.SurgicalHistories = append(
			consultation.SurgicalHistories,
			ConsultationSurgicalHistory{
				ProcedureName: item.ProcedureName,
				ProcedureDate: item.ProcedureDate,
				Indication:    item.Indication,
				Complications: item.Complications,
				Notes:         item.Notes,
			},
		)
	}

	for _, item := range req.GynecoObstetricHistories {
		consultation.GynecoObstetricHistories = append(
			consultation.GynecoObstetricHistories,
			ConsultationGynecoObstetricHistory{
				EventType: item.EventType,
				EventDate: item.EventDate,
				Outcome:   item.Outcome,
				Notes:     item.Notes,
			},
		)
	}

	err = s.repo.Create(consultation, authorID)
	if err != nil {
		return nil, err
	}

	if s.medicalRecordsService != nil {
		err = s.medicalRecordsService.RecordConsultationCreated(
			consultation.PatientID,
			consultation.ID,
			consultation.Service,
			consultation.DoctorName,
			authorID,
		)
		if err != nil {
			return nil, err
		}
	}

	if s.medicalRecordsService != nil {
		for _, exam := range exams {
			_ = s.medicalRecordsService.RecordExamRequested(
				consultation.PatientID,
				consultation.ID,
				exam.Name,
				consultation.Service,
				authorID,
			)
		}
	}

	if s.medicalRecordsService != nil {
		for _, prescription := range prescriptions {
			_ = s.medicalRecordsService.RecordMedicationPrescribed(
				consultation.PatientID,
				consultation.ID,
				prescription.MedicationName,
				prescription.Dosage,
				consultation.Service,
				authorID,
			)
		}
	}

	return s.repo.FindByID(consultation.ID)
}

func (s *Service) GetConsultation(id uint) (*Consultation, error) {
	return s.repo.FindByID(id)
}

func (s *Service) GetPatientConsultations(patientID uint) ([]Consultation, error) {
	return s.repo.FindByPatientID(patientID)
}

func (s *Service) CreateReason(req CreateReferenceRequest) (*ConsultationReason, error) {
	reason := &ConsultationReason{
		Code:     req.Code,
		Name:     req.Name,
		Category: req.Category,
		IsActive: true,
	}

	err := s.repo.CreateReason(reason)
	return reason, err
}

func (s *Service) UpdateReason(id uint, req UpdateReferenceRequest) error {
	return s.repo.UpdateReason(id, req)
}

func (s *Service) DeleteReason(id uint) error {
	return s.repo.DeleteReason(id)
}

func (s *Service) CreateExam(req CreateReferenceRequest) (*MedicalExam, error) {
	exam := &MedicalExam{
		Code:     req.Code,
		Name:     req.Name,
		Category: req.Category,
		IsActive: true,
	}

	err := s.repo.CreateExam(exam)
	return exam, err
}

func (s *Service) UpdateExam(id uint, req UpdateReferenceRequest) error {
	return s.repo.UpdateExam(id, req)
}

func (s *Service) DeleteExam(id uint) error {
	return s.repo.DeleteExam(id)
}

func canTransitionConsultationStatus(currentStatus, newStatus string) bool {
	switch currentStatus {
	case ConsultationStatusDraft:
		return newStatus == ConsultationStatusInProgress ||
			newStatus == ConsultationStatusCancelled

	case ConsultationStatusInProgress:
		return newStatus == ConsultationStatusCompleted ||
			newStatus == ConsultationStatusCancelled

	case ConsultationStatusCompleted, ConsultationStatusCancelled:
		return false

	default:
		return false
	}
}

func (s *Service) UpdateStatus(id uint, req UpdateConsultationStatusRequest, authorID uint) (*Consultation, error) {

	consultation, err := s.repo.FindByID(id)
	if err != nil {
		return nil, ErrConsultationNotFound
	}

	if !canTransitionConsultationStatus(
		consultation.Status,
		req.Status,
	) {
		return nil, ErrInvalidTransition
	}

	now := time.Now()

	updates := map[string]interface{}{
		"status": req.Status,
	}

	switch req.Status {

	case ConsultationStatusInProgress:
		updates["started_at"] = now

	case ConsultationStatusCompleted:
		updates["completed_at"] = now

	case ConsultationStatusCancelled:
		if req.CancellationReason == "" {
			return nil, ErrCancellationReasonRequired
		}

		updates["cancelled_at"] = now
		updates["cancellation_reason"] = req.CancellationReason
	}

	oldStatus := consultation.Status

	if err := s.repo.UpdateStatus(id, updates); err != nil {
		return nil, err
	}

	if s.medicalRecordsService != nil {
		_ = s.medicalRecordsService.RecordConsultationStatusChanged(
			consultation.PatientID,
			consultation.ID,
			oldStatus,
			req.Status,
			authorID,
		)
	}

	return s.repo.FindByID(id)
}

func (s *Service) UpdateConsultation(id uint, req UpdateConsultationRequest, authorID uint) (*Consultation, error) {

	var antecedent *ConsultationAntecedent
	var physicalExams *[]ConsultationPhysicalExam
	var administeredTreatments *[]ConsultationAdministeredTreatment
	var previousMedications *[]ConsultationPreviousMedication
	var surgicalHistories *[]ConsultationSurgicalHistory
	var gynecoObstetricHistories *[]ConsultationGynecoObstetricHistory

	consultation, err := s.repo.FindByID(id)
	if err != nil {
		return nil, ErrConsultationNotFound
	}

	if consultation.Status == ConsultationStatusCompleted ||
		consultation.Status == ConsultationStatusCancelled {
		return nil, ErrConsultationLocked
	}

	updates := map[string]interface{}{}

	if req.DoctorName != nil {
		updates["doctor_name"] = *req.DoctorName
	}

	if req.Service != nil {
		updates["service"] = *req.Service
	}

	if req.Diagnosis != nil {
		updates["diagnosis"] = *req.Diagnosis
	}

	if req.Observations != nil {
		updates["observations"] = *req.Observations
	}

	if req.Treatment != nil {
		updates["treatment"] = *req.Treatment
	}

	var reasons []ConsultationReason
	updateReasons := req.ReasonIDs != nil

	if updateReasons {
		reasons, err = s.repo.FindReasonsByIDs(*req.ReasonIDs)
		if err != nil {
			return nil, err
		}

		if len(reasons) != len(*req.ReasonIDs) {
			return nil, ErrInvalidReasonIDs
		}
	}

	var exams []MedicalExam
	updateExams := req.ExamIDs != nil

	if updateExams {
		exams, err = s.repo.FindExamsByIDs(*req.ExamIDs)
		if err != nil {
			return nil, err
		}

		if len(exams) != len(*req.ExamIDs) {
			return nil, ErrInvalidExamIDs
		}
	}

	if req.SickLeaveRequired != nil {
		updates["sick_leave_required"] = *req.SickLeaveRequired

		if !*req.SickLeaveRequired {
			updates["sick_leave_days"] = 0
			updates["sick_leave_start_date"] = nil
			updates["sick_leave_end_date"] = nil
		}
	}

	if req.SickLeaveDays != nil {
		if *req.SickLeaveDays <= 0 {
			return nil, ErrInvalidSickLeave
		}

		start := time.Now()
		end := start.AddDate(0, 0, *req.SickLeaveDays)

		updates["sick_leave_required"] = true
		updates["sick_leave_days"] = *req.SickLeaveDays
		updates["sick_leave_start_date"] = start
		updates["sick_leave_end_date"] = end
	}

	var prescriptions []ConsultationPrescription
	updatePrescriptions := req.Prescriptions != nil

	if updatePrescriptions {
		for _, item := range *req.Prescriptions {
			presentation, err := s.repo.FindMedicationPresentationByID(
				item.PresentationID,
			)
			if err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil, ErrInvalidPresentationID
				}

				return nil, err
			}

			if !presentation.IsActive || !presentation.Medication.IsActive {
				return nil, ErrInactivePresentation
			}

			presentationID := presentation.ID

			prescriptions = append(
				prescriptions,
				ConsultationPrescription{
					PresentationID: &presentationID,

					MedicationName: presentation.Medication.Name,
					Dosage:         presentation.Dosage,
					Form:           presentation.Form,
					Route:          presentation.Route,

					Quantity: item.Quantity,

					Duration:     item.Duration,
					Instructions: item.Instructions,
				},
			)
		}
	}

	if req.HospitalizationRequired != nil {
		updates["hospitalization_required"] = *req.HospitalizationRequired

		if !*req.HospitalizationRequired {
			updates["hospitalization_reason"] = ""
			updates["hospitalization_type"] = ""
			updates["hospitalization_duration"] = 0
		}
	}

	if req.HospitalizationReason != nil {
		updates["hospitalization_reason"] = *req.HospitalizationReason
	}

	if req.HospitalizationType != nil {
		updates["hospitalization_type"] = *req.HospitalizationType
	}

	if req.HospitalizationDuration != nil {
		updates["hospitalization_duration"] = *req.HospitalizationDuration
	}

	if req.Antecedent != nil {
		antecedent = &ConsultationAntecedent{
			PreviousMedication:     req.Antecedent.PreviousMedication,
			HasHTA:                 req.Antecedent.HasHTA,
			HasDiabetes:            req.Antecedent.HasDiabetes,
			OtherMedical:           req.Antecedent.OtherMedical,
			SurgicalHistory:        req.Antecedent.SurgicalHistory,
			GynecoObstetricHistory: req.Antecedent.GynecoObstetricHistory,
			DDR:                    req.Antecedent.DDR,
			PregnancyOngoing:       req.Antecedent.PregnancyOngoing,
			Tobacco:                req.Antecedent.Tobacco,
			Alcohol:                req.Antecedent.Alcohol,
			VisitType:              req.Antecedent.VisitType,
		}
	}

	if req.PhysicalExams != nil {
		items := make([]ConsultationPhysicalExam, 0, len(*req.PhysicalExams))

		for _, item := range *req.PhysicalExams {
			area, err := s.repo.FindPhysicalExamAreaByID(item.AreaID)
			if err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil, ErrPhysicalExamAreaNotFound
				}

				return nil, err
			}

			if !area.IsActive {
				return nil, ErrInactivePhysicalExamArea
			}

			items = append(items, ConsultationPhysicalExam{
				AreaID:      area.ID,
				Observation: item.Observation,
			})
		}

		physicalExams = &items
	}

	if req.AdministeredTreatments != nil {
		items := make([]ConsultationAdministeredTreatment, 0, len(*req.AdministeredTreatments))

		for _, item := range *req.AdministeredTreatments {
			presentation, err := s.repo.FindMedicationPresentationByID(item.PresentationID)
			if err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil, ErrInvalidPresentationID
				}

				return nil, err
			}

			if !presentation.IsActive || !presentation.Medication.IsActive {
				return nil, ErrInactivePresentation
			}

			presentationID := presentation.ID

			items = append(items, ConsultationAdministeredTreatment{
				PresentationID: &presentationID,
				MedicationName: presentation.Medication.Name,
				Dosage:         presentation.Dosage,
				Form:           presentation.Form,
				Route:          presentation.Route,
				Quantity:       item.Quantity,
				Instructions:   item.Instructions,
			})
		}

		administeredTreatments = &items
	}

	if req.PreviousMedications != nil {
		items := make(
			[]ConsultationPreviousMedication,
			0,
			len(*req.PreviousMedications),
		)

		for _, item := range *req.PreviousMedications {
			previousMedication, err := s.buildPreviousMedication(item)
			if err != nil {
				return nil, err
			}

			items = append(items, *previousMedication)
		}

		previousMedications = &items
	}

	if req.SurgicalHistories != nil {
		items := make(
			[]ConsultationSurgicalHistory,
			0,
			len(*req.SurgicalHistories),
		)

		for _, item := range *req.SurgicalHistories {
			items = append(items, ConsultationSurgicalHistory{
				ProcedureName: item.ProcedureName,
				ProcedureDate: item.ProcedureDate,
				Indication:    item.Indication,
				Complications: item.Complications,
				Notes:         item.Notes,
			})
		}

		surgicalHistories = &items
	}

	if req.GynecoObstetricHistories != nil {
		items := make(
			[]ConsultationGynecoObstetricHistory,
			0,
			len(*req.GynecoObstetricHistories),
		)

		for _, item := range *req.GynecoObstetricHistories {
			items = append(items, ConsultationGynecoObstetricHistory{
				EventType: item.EventType,
				EventDate: item.EventDate,
				Outcome:   item.Outcome,
				Notes:     item.Notes,
			})
		}

		gynecoObstetricHistories = &items
	}

	err = s.repo.UpdateConsultation(
		id,
		authorID,
		updates,
		req.Vitals,
		reasons,
		updateReasons,
		exams,
		updateExams,
		prescriptions,
		updatePrescriptions,
		antecedent,
		physicalExams,
		administeredTreatments,
		previousMedications,
		surgicalHistories,
		gynecoObstetricHistories,
	)

	if err != nil {
		return nil, err
	}

	return s.repo.FindByID(id)
}

func (s *Service) GetPatient360(patientID uint) (*Patient360Response, error) {
	consultations, err := s.repo.FindByPatientID(patientID)
	if err != nil {
		return nil, err
	}

	documents := make([]PatientDocumentItem, 0)

	for _, consultation := range consultations {
		base := "/api/consultations/" + fmt.Sprintf("%d", consultation.ID)

		documents = append(documents,
			PatientDocumentItem{
				ConsultationID: consultation.ID,
				Type:           "report",
				Label:          "Compte rendu de consultation",
				URL:            base + "/report/pdf",
			},
		)

		if consultation.SickLeaveRequired {
			documents = append(documents,
				PatientDocumentItem{
					ConsultationID: consultation.ID,
					Type:           "sick_leave",
					Label:          "Fiche de repos maladie",
					URL:            base + "/sick-leave/pdf",
				},
			)
		}

		if len(consultation.Exams) > 0 {
			documents = append(documents,
				PatientDocumentItem{
					ConsultationID: consultation.ID,
					Type:           "exam_request",
					Label:          "Demande / autorisation d'examens",
					URL:            base + "/exam-request/pdf",
				},
			)
		}

		if len(consultation.Prescriptions) > 0 {
			documents = append(documents,
				PatientDocumentItem{
					ConsultationID: consultation.ID,
					Type:           "prescription",
					Label:          "Ordonnance",
					URL:            base + "/prescription/pdf",
				},
			)
		}

		if consultation.HospitalizationRequired {
			documents = append(documents,
				PatientDocumentItem{
					ConsultationID: consultation.ID,
					Type:           "hospitalization",
					Label:          "Fiche d'hospitalisation",
					URL:            base + "/hospitalization/pdf",
				},
			)
		}
	}

	return &Patient360Response{
		PatientID:     patientID,
		Consultations: consultations,
		Documents:     documents,
	}, nil
}
func (s *Service) GetPhysicalExamAreas() ([]PhysicalExamArea, error) {
	return s.repo.FindPhysicalExamAreas()
}

func (s *Service) buildPreviousMedication(
	item PreviousMedicationRequest,
) (*ConsultationPreviousMedication, error) {
	presentation, err := s.repo.FindMedicationPresentationByID(item.PresentationID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInvalidPresentationID
		}

		return nil, err
	}

	if !presentation.IsActive || !presentation.Medication.IsActive {
		return nil, ErrInactivePresentation
	}

	presentationID := presentation.ID

	status := item.Status
	if status == "" {
		status = "ONGOING"
	}

	return &ConsultationPreviousMedication{
		PresentationID: &presentationID,
		MedicationName: presentation.Medication.Name,
		Dosage:         presentation.Dosage,
		Form:           presentation.Form,
		Route:          presentation.Route,
		Instructions:   item.Instructions,
		Status:         status,
	}, nil
}

func (s *Service) GetSOAP(consultationID uint) (*ConsultationSOAP, error) {
	if _, err := s.repo.FindByID(consultationID); err != nil {
		return nil, err
	}

	return s.repo.GetSOAPByConsultationID(consultationID)
}

func (s *Service) UpsertSOAP(
	consultationID uint,
	req UpsertConsultationSOAPRequest,
	authorID uint,
) (*ConsultationSOAP, error) {
	consultation, err := s.repo.FindByID(consultationID)
	if err != nil {
		return nil, err
	}

	soap := &ConsultationSOAP{
		ConsultationID: consultationID,

		ChiefComplaint:          req.ChiefComplaint,
		HistoryOfPresentIllness: req.HistoryOfPresentIllness,
		AssociatedSymptoms:      req.AssociatedSymptoms,
		PatientReportedNotes:    req.PatientReportedNotes,

		GeneralAppearance:   req.GeneralAppearance,
		Consciousness:       req.Consciousness,
		HydrationStatus:     req.HydrationStatus,
		PhysicalExamSummary: req.PhysicalExamSummary,

		PrimaryDiagnosis:    req.PrimaryDiagnosis,
		AssociatedDiagnoses: req.AssociatedDiagnoses,
		ClinicalImpression:  req.ClinicalImpression,

		TreatmentPlan:     req.TreatmentPlan,
		InvestigationPlan: req.InvestigationPlan,
		FollowUpPlan:      req.FollowUpPlan,
		PatientAdvice:     req.PatientAdvice,
		Disposition:       req.Disposition,

		UpdatedBy: authorID,
	}

	existing, err := s.repo.GetSOAPByConsultationID(consultationID)
	if err == nil {
		soap.ID = existing.ID
		soap.CreatedAt = existing.CreatedAt
		soap.CreatedBy = existing.CreatedBy
	} else if errors.Is(err, gorm.ErrRecordNotFound) {
		soap.CreatedBy = authorID
	} else {
		return nil, err
	}

	if err := s.repo.UpsertSOAP(soap); err != nil {
		return nil, err
	}

	if s.medicalRecordsService != nil {
		if err := s.medicalRecordsService.RecordConsultationSOAPUpdated(
			consultation.PatientID,
			consultation.ID,
			authorID,
		); err != nil {
			return nil, err
		}
	}

	return s.repo.GetSOAPByConsultationID(consultationID)
}

func (s *Service) GetSpecialtyData(
	consultationID uint,
) (*ConsultationSpecialtyData, error) {
	if _, err := s.repo.FindByID(consultationID); err != nil {
		return nil, err
	}

	return s.repo.GetSpecialtyDataByConsultationID(consultationID)
}

func (s *Service) UpsertSpecialtyData(
	consultationID uint,
	req UpsertConsultationSpecialtyRequest,
	authorID uint,
) (*ConsultationSpecialtyData, error) {
	consultation, err := s.repo.FindByID(consultationID)
	if err != nil {
		return nil, err
	}

	dataJSON, err := json.Marshal(req.Data)
	if err != nil {
		return nil, err
	}

	specialtyData := &ConsultationSpecialtyData{
		ConsultationID: consultationID,
		SpecialtyCode:  req.SpecialtyCode,
		Data:           string(dataJSON),
		UpdatedBy:      authorID,
	}

	existing, err := s.repo.GetSpecialtyDataByConsultationID(consultationID)
	if err == nil {
		specialtyData.ID = existing.ID
		specialtyData.CreatedBy = existing.CreatedBy
		specialtyData.CreatedAt = existing.CreatedAt
	} else if errors.Is(err, gorm.ErrRecordNotFound) {
		specialtyData.CreatedBy = authorID
	} else {
		return nil, err
	}

	if err := s.repo.UpsertSpecialtyData(specialtyData); err != nil {
		return nil, err
	}

	if s.medicalRecordsService != nil {
		_ = s.medicalRecordsService.RecordConsultationSpecialtyUpdated(
			consultation.PatientID,
			consultation.ID,
			req.SpecialtyCode,
			authorID,
		)
	}

	return s.repo.GetSpecialtyDataByConsultationID(consultationID)
}
