package imaging

import (
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrInvalidTransition = errors.New("transition d’imagerie non autorisée")
var ErrValidated = errors.New("un compte rendu validé est immuable")

type Service struct{ repo *Repository }

func NewService(r *Repository) *Service { return &Service{repo: r} }

func (s *Service) List(f ListFilter, a Access) (*ListResult, error) {
	if err := s.repo.Materialize(a.UserID); err != nil {
		return nil, err
	}
	unrestricted, ids, err := s.assignedExecutingServiceIDs(a)
	if err != nil {
		return nil, err
	}
	return s.repo.List(f, unrestricted, ids)
}

func (s *Service) Get(id uint, a Access) (*Order, error) {
	if err := s.repo.Materialize(a.UserID); err != nil {
		return nil, err
	}
	o, err := s.repo.Find(id)
	if err != nil {
		return nil, err
	}
	if err := s.assertCanAccessExecutingOrder(o, a); err != nil {
		return nil, err
	}
	return o, nil
}

func (s *Service) Schedule(id uint, a Access, req ScheduleRequest) (*Order, error) {
	err := s.repo.WithLockedOrder(id, func(tx *gorm.DB, o *Order) error {
		if err := s.assertCanAccessExecutingOrder(o, a); err != nil {
			return err
		}
		if o.Status != StatusOrdered {
			return ErrInvalidTransition
		}
		return s.updateAndEvent(tx, o, map[string]interface{}{"status": StatusScheduled, "scheduled_at": req.ScheduledAt, "scheduled_by": a.UserID, "schedule_comment": req.Comment, "updated_by": a.UserID}, "imaging_scheduled", "Imagerie planifiée", req.ScheduledAt.Format(time.RFC3339), a.UserID)
	})
	if err != nil {
		return nil, err
	}
	return s.Get(id, a)
}

func (s *Service) Start(id uint, a Access, req StartRequest) (*Order, error) {
	err := s.repo.WithLockedOrder(id, func(tx *gorm.DB, o *Order) error {
		if err := s.assertCanAccessExecutingOrder(o, a); err != nil {
			return err
		}
		if o.Status != StatusOrdered && o.Status != StatusScheduled {
			return ErrInvalidTransition
		}
		now := time.Now()
		return s.updateAndEvent(tx, o, map[string]interface{}{"status": StatusInProgress, "performed_at": now, "performed_by": a.UserID, "technical_notes": req.TechnicalNotes, "contrast_used": req.ContrastUsed, "contrast_product": req.ContrastProduct, "study_instance_uid": req.StudyInstanceUID, "external_viewer_url": req.ExternalViewerURL, "updated_by": a.UserID}, "imaging_started", "Examen d’imagerie démarré", o.OrderNumber, a.UserID)
	})
	if err != nil {
		return nil, err
	}
	return s.Get(id, a)
}

func (s *Service) SaveReport(id uint, a Access, req ReportRequest) (*Order, error) {
	err := s.repo.WithLockedOrder(id, func(tx *gorm.DB, o *Order) error {
		if err := s.assertCanAccessExecutingOrder(o, a); err != nil {
			return err
		}
		if o.Status == StatusValidated {
			return ErrValidated
		}
		if o.Status != StatusInProgress && o.Status != StatusReportDrafted {
			return ErrInvalidTransition
		}
		now := time.Now()
		report := Report{OrderID: o.ID, ClinicalIndication: req.ClinicalIndication, Technique: req.Technique, Findings: req.Findings, Conclusion: req.Conclusion, Recommendation: req.Recommendation, DocumentURL: req.DocumentURL, DraftedBy: a.UserID, DraftedAt: now}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "order_id"}}, DoUpdates: clause.AssignmentColumns([]string{"clinical_indication", "technique", "findings", "conclusion", "recommendation", "document_url", "drafted_by", "drafted_at", "updated_at"})}).Create(&report).Error; err != nil {
			return err
		}
		return s.updateAndEvent(tx, o, map[string]interface{}{"status": StatusReportDrafted, "updated_by": a.UserID}, "imaging_report_drafted", "Compte rendu d’imagerie rédigé", o.OrderNumber, a.UserID)
	})
	if err != nil {
		return nil, err
	}
	return s.Get(id, a)
}

func (s *Service) Validate(id uint, a Access) (*Order, error) {
	err := s.repo.WithLockedOrder(id, func(tx *gorm.DB, o *Order) error {
		if err := s.assertCanAccessExecutingOrder(o, a); err != nil {
			return err
		}
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
		if err := tx.Model(&report).Updates(map[string]interface{}{"validated_by": a.UserID, "validated_at": now}).Error; err != nil {
			return err
		}
		return s.updateAndEvent(tx, o, map[string]interface{}{"status": StatusValidated, "updated_by": a.UserID}, "imaging_report_validated", "Compte rendu d’imagerie validé", o.OrderNumber, a.UserID)
	})
	if err != nil {
		return nil, err
	}
	return s.Get(id, a)
}

func (s *Service) Cancel(id uint, a Access, reason string) (*Order, error) {
	err := s.repo.WithLockedOrder(id, func(tx *gorm.DB, o *Order) error {
		if err := s.assertCanAccessExecutingOrder(o, a); err != nil {
			return err
		}
		if o.Status != StatusOrdered && o.Status != StatusScheduled {
			return ErrInvalidTransition
		}
		return s.updateAndEvent(tx, o, map[string]interface{}{"status": StatusCancelled, "cancelled_reason": reason, "updated_by": a.UserID}, "imaging_cancelled", "Demande d’imagerie annulée", reason, a.UserID)
	})
	if err != nil {
		return nil, err
	}
	return s.Get(id, a)
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
