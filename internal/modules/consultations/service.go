package consultations

import (
	"errors"
	"fmt"
	"time"
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
)

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) GetReasons() ([]ConsultationReason, error) {
	return s.repo.FindReasons()
}

func (s *Service) GetExams() ([]MedicalExam, error) {
	return s.repo.FindExams()
}

func (s *Service) CreateConsultation(req CreateConsultationRequest) (*Consultation, error) {
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
		prescriptions = append(prescriptions, ConsultationPrescription{
			MedicationName: item.MedicationName,
			Dosage:         item.Dosage,
			Form:           item.Form,
			Frequency:      item.Frequency,
			Duration:       item.Duration,
			Route:          item.Route,
			Instructions:   item.Instructions,
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

	err = s.repo.Create(consultation)
	if err != nil {
		return nil, err
	}

	return consultation, nil
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

func (s *Service) UpdateStatus(id uint, req UpdateConsultationStatusRequest) (*Consultation, error) {

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

	if err := s.repo.UpdateStatus(id, updates); err != nil {
		return nil, err
	}

	return s.repo.FindByID(id)
}

func (s *Service) UpdateConsultation(id uint, req UpdateConsultationRequest) (*Consultation, error) {

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
			prescriptions = append(prescriptions, ConsultationPrescription{
				MedicationName: item.MedicationName,
				Dosage:         item.Dosage,
				Form:           item.Form,
				Frequency:      item.Frequency,
				Duration:       item.Duration,
				Route:          item.Route,
				Instructions:   item.Instructions,
			})
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

	err = s.repo.UpdateConsultation(
		id,
		updates,
		req.Vitals,
		reasons,
		updateReasons,
		exams,
		updateExams,
		prescriptions,
		updatePrescriptions,
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
