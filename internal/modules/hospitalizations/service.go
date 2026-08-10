package hospitalizations

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/modules/consultations"
	"github.com/lallene/medcore-his/backend/internal/modules/medical_records"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
	"gorm.io/gorm"
)

type Service struct {
	db   *gorm.DB
	repo *Repository
}

func NewService(db *gorm.DB, repo *Repository) *Service { return &Service{db: db, repo: repo} }

func parseOptionalDate(value string) (*time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, coreerrors.BadRequest("date invalide, format RFC3339 attendu")
	}
	return &parsed, nil
}

func (s *Service) Create(req CreateRequest, authorID uint) (*Hospitalization, bool, error) {
	if existing, err := s.repo.FindByConsultation(req.SourceConsultationID); err == nil {
		if existing.PatientID != req.PatientID {
			return nil, false, coreerrors.Conflict("la consultation appartient à un autre patient")
		}
		return existing, false, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, err
	}

	expected, err := parseOptionalDate(req.ExpectedDischargeAt)
	if err != nil {
		return nil, false, err
	}
	var created Hospitalization
	err = s.db.Transaction(func(tx *gorm.DB) error {
		var patient patients.Patient
		if err := tx.First(&patient, req.PatientID).Error; err != nil {
			return coreerrors.NotFound("PATIENT")
		}
		var consultation consultations.Consultation
		if err := tx.First(&consultation, req.SourceConsultationID).Error; err != nil {
			return coreerrors.NotFound("CONSULTATION")
		}
		if consultation.PatientID != req.PatientID {
			return coreerrors.Conflict("la consultation appartient à un autre patient")
		}
		if !consultation.HospitalizationRequired {
			return coreerrors.Conflict("aucune hospitalisation n'est recommandée par cette consultation")
		}
		if expected == nil && consultation.HospitalizationDuration > 0 {
			value := time.Now().AddDate(0, 0, consultation.HospitalizationDuration)
			expected = &value
		}
		var record medical_records.MedicalRecord
		err := tx.Where("patient_id = ?", req.PatientID).First(&record).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			record = medical_records.MedicalRecord{PatientID: req.PatientID, RecordNumber: fmt.Sprintf("DM-%06d", req.PatientID), Status: "active"}
			if err := tx.Create(&record).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		createdBy := authorID
		created = Hospitalization{PatientID: req.PatientID, MedicalRecordID: record.ID, SourceConsultationID: consultation.ID,
			AdmissionNumber:     "HOSP-" + time.Now().Format("2006") + "-" + strings.ToUpper(uuid.NewString()[:8]),
			HospitalizationType: consultation.HospitalizationType, AdmissionReason: consultation.HospitalizationReason,
			AdmissionDiagnosis: strings.TrimSpace(req.AdmissionDiagnosis), Department: consultation.Service,
			Status: StatusPlanned, ExpectedDischargeAt: expected}
		created.CreatedBy, created.UpdatedBy = &createdBy, &createdBy
		if err := tx.Create(&created).Error; err != nil {
			return err
		}
		return createTimeline(tx, &created, "hospitalization_created", "Hospitalisation planifiée", authorID, time.Now())
	})
	if isDuplicate(err) {
		existing, findErr := s.repo.FindByConsultation(req.SourceConsultationID)
		return existing, false, findErr
	}
	if err != nil {
		return nil, false, err
	}
	item, err := s.repo.FindByID(created.ID)
	return item, true, err
}

func (s *Service) FindByID(id uint) (*Hospitalization, error) {
	item, err := s.repo.FindByID(id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, coreerrors.NotFound("HOSPITALIZATION")
	}
	return item, err
}
func (s *Service) FindByConsultation(id uint) (*Hospitalization, error) {
	item, err := s.repo.FindByConsultation(id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, coreerrors.NotFound("HOSPITALIZATION")
	}
	return item, err
}
func (s *Service) List(filter ListFilter) (*ListResult, error) { return s.repo.List(filter) }

func (s *Service) Admit(id uint, req AdmitRequest, authorID uint) (*Hospitalization, error) {
	at, err := parseOptionalDate(req.AdmittedAt)
	if err != nil {
		return nil, err
	}
	if at == nil {
		now := time.Now()
		at = &now
	}
	err = s.transition(id, StatusPlanned, StatusAdmitted, authorID, "hospitalization_admitted", "Patient admis", *at, func(item *Hospitalization) {
		item.AdmittedAt = at
		if strings.TrimSpace(req.AdmissionDiagnosis) != "" {
			item.AdmissionDiagnosis = strings.TrimSpace(req.AdmissionDiagnosis)
		}
	})
	if err != nil {
		return nil, err
	}
	return s.repo.FindByID(id)
}
func (s *Service) Discharge(id uint, req DischargeRequest, authorID uint) (*Hospitalization, error) {
	at, err := parseOptionalDate(req.DischargedAt)
	if err != nil {
		return nil, err
	}
	if at == nil {
		now := time.Now()
		at = &now
	}
	current, err := s.FindByID(id)
	if err != nil {
		return nil, err
	}
	if current.AdmittedAt != nil && at.Before(*current.AdmittedAt) {
		return nil, coreerrors.BadRequest("la sortie ne peut pas précéder l'admission")
	}
	err = s.transition(id, StatusAdmitted, StatusDischarged, authorID, "hospitalization_discharged", "Sortie du patient enregistrée", *at, func(item *Hospitalization) {
		item.DischargedAt = at
		item.DischargeDiagnosis = strings.TrimSpace(req.DischargeDiagnosis)
		item.DischargeSummary = strings.TrimSpace(req.DischargeSummary)
	})
	if err != nil {
		return nil, err
	}
	return s.repo.FindByID(id)
}
func (s *Service) Cancel(id uint, authorID uint) (*Hospitalization, error) {
	now := time.Now()
	err := s.transition(id, StatusPlanned, StatusCancelled, authorID, "hospitalization_cancelled", "Hospitalisation annulée", now, func(*Hospitalization) {})
	if err != nil {
		return nil, err
	}
	return s.repo.FindByID(id)
}

func (s *Service) transition(id uint, from, to string, authorID uint, eventType, title string, eventDate time.Time, mutate func(*Hospitalization)) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		item, err := lockByID(tx, id)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return coreerrors.NotFound("HOSPITALIZATION")
		}
		if err != nil {
			return err
		}
		if item.Status != from {
			return coreerrors.Conflict(fmt.Sprintf("transition %s vers %s interdite", item.Status, to))
		}
		mutate(item)
		item.Status = to
		updatedBy := authorID
		item.UpdatedBy = &updatedBy
		if err := tx.Save(item).Error; err != nil {
			return err
		}
		return createTimeline(tx, item, eventType, title, authorID, eventDate)
	})
}

func createTimeline(tx *gorm.DB, item *Hospitalization, eventType, title string, authorID uint, eventDate time.Time) error {
	referenceID := item.ID
	event := medical_records.MedicalTimelineEvent{MedicalRecordID: item.MedicalRecordID, PatientID: item.PatientID,
		EventType: eventType, Category: "hospitalization", Title: title, Description: item.AdmissionReason,
		ReferenceType: "hospitalization", ReferenceID: &referenceID, Severity: "info", EventDate: eventDate, CreatedBy: authorID}
	return tx.Create(&event).Error
}
