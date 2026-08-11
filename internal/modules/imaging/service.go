package imaging

import (
	"errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

var ErrInvalidTransition = errors.New("transition d’imagerie non autorisée")
var ErrValidated = errors.New("un compte rendu validé est immuable")

type Service struct{ repo *Repository }

func NewService(r *Repository) *Service { return &Service{repo: r} }
func (s *Service) List(f ListFilter, user uint) (*ListResult, error) {
	if err := s.repo.Materialize(user); err != nil {
		return nil, err
	}
	return s.repo.List(f)
}
func (s *Service) Get(id, user uint) (*Order, error) {
	if err := s.repo.Materialize(user); err != nil {
		return nil, err
	}
	return s.repo.Find(id)
}
func (s *Service) Schedule(id, user uint, req ScheduleRequest) (*Order, error) {
	err := s.repo.WithLockedOrder(id, func(tx *gorm.DB, o *Order) error {
		if o.Status != StatusOrdered {
			return ErrInvalidTransition
		}
		return s.updateAndEvent(tx, o, map[string]interface{}{"status": StatusScheduled, "scheduled_at": req.ScheduledAt, "scheduled_by": user, "schedule_comment": req.Comment, "updated_by": user}, "imaging_scheduled", "Imagerie planifiée", req.ScheduledAt.Format(time.RFC3339), user)
	})
	if err != nil {
		return nil, err
	}
	return s.repo.Find(id)
}
func (s *Service) Start(id, user uint, req StartRequest) (*Order, error) {
	err := s.repo.WithLockedOrder(id, func(tx *gorm.DB, o *Order) error {
		if o.Status != StatusOrdered && o.Status != StatusScheduled {
			return ErrInvalidTransition
		}
		now := time.Now()
		return s.updateAndEvent(tx, o, map[string]interface{}{"status": StatusInProgress, "performed_at": now, "performed_by": user, "technical_notes": req.TechnicalNotes, "contrast_used": req.ContrastUsed, "contrast_product": req.ContrastProduct, "study_instance_uid": req.StudyInstanceUID, "external_viewer_url": req.ExternalViewerURL, "updated_by": user}, "imaging_started", "Examen d’imagerie démarré", o.OrderNumber, user)
	})
	if err != nil {
		return nil, err
	}
	return s.repo.Find(id)
}
func (s *Service) SaveReport(id, user uint, req ReportRequest) (*Order, error) {
	err := s.repo.WithLockedOrder(id, func(tx *gorm.DB, o *Order) error {
		if o.Status == StatusValidated {
			return ErrValidated
		}
		if o.Status != StatusInProgress && o.Status != StatusReportDrafted {
			return ErrInvalidTransition
		}
		now := time.Now()
		report := Report{OrderID: o.ID, ClinicalIndication: req.ClinicalIndication, Technique: req.Technique, Findings: req.Findings, Conclusion: req.Conclusion, Recommendation: req.Recommendation, DocumentURL: req.DocumentURL, DraftedBy: user, DraftedAt: now}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "order_id"}}, DoUpdates: clause.AssignmentColumns([]string{"clinical_indication", "technique", "findings", "conclusion", "recommendation", "document_url", "drafted_by", "drafted_at", "updated_at"})}).Create(&report).Error; err != nil {
			return err
		}
		return s.updateAndEvent(tx, o, map[string]interface{}{"status": StatusReportDrafted, "updated_by": user}, "imaging_report_drafted", "Compte rendu d’imagerie rédigé", o.OrderNumber, user)
	})
	if err != nil {
		return nil, err
	}
	return s.repo.Find(id)
}
func (s *Service) Validate(id, user uint) (*Order, error) {
	err := s.repo.WithLockedOrder(id, func(tx *gorm.DB, o *Order) error {
		if o.Status == StatusValidated {
			return ErrValidated
		}
		if o.Status != StatusReportDrafted {
			return ErrInvalidTransition
		}
		var report Report
		if err := tx.Where("order_id=?", id).First(&report).Error; err != nil {
			return ErrInvalidTransition
		}
		now := time.Now()
		if err := tx.Model(&report).Updates(map[string]interface{}{"validated_by": user, "validated_at": now}).Error; err != nil {
			return err
		}
		return s.updateAndEvent(tx, o, map[string]interface{}{"status": StatusValidated, "updated_by": user}, "imaging_report_validated", "Compte rendu d’imagerie validé", o.OrderNumber, user)
	})
	if err != nil {
		return nil, err
	}
	return s.repo.Find(id)
}
func (s *Service) Cancel(id, user uint, reason string) (*Order, error) {
	err := s.repo.WithLockedOrder(id, func(tx *gorm.DB, o *Order) error {
		if o.Status != StatusOrdered && o.Status != StatusScheduled {
			return ErrInvalidTransition
		}
		return s.updateAndEvent(tx, o, map[string]interface{}{"status": StatusCancelled, "cancelled_reason": reason, "updated_by": user}, "imaging_cancelled", "Demande d’imagerie annulée", reason, user)
	})
	if err != nil {
		return nil, err
	}
	return s.repo.Find(id)
}
func (s *Service) updateAndEvent(tx *gorm.DB, o *Order, updates map[string]interface{}, event, title, description string, user uint) error {
	if err := tx.Model(o).Updates(updates).Error; err != nil {
		return err
	}
	if o.MedicalRecordID != nil {
		return createEvent(tx, *o.MedicalRecordID, o.PatientID, event, title, description, o.ID, user)
	}
	return nil
}
