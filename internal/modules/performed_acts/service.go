package performed_acts

import (
	"errors"
	"math"
	"strings"
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/modules/act_catalog"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
	"gorm.io/gorm"
)

type Service struct {
	db *gorm.DB
}

func NewService(db *gorm.DB) *Service {
	return &Service{db: db}
}

func (s *Service) Create(req CreateRequest, actorID uint) (*Act, error) {
	if req.PatientID == 0 || req.ActCatalogEntryID == 0 {
		return nil, coreerrors.BadRequest("Patient et acte catalogue obligatoires")
	}
	qty := req.Quantity
	if qty == 0 {
		qty = 1
	}
	if qty <= 0 {
		return nil, coreerrors.BadRequest("La quantité doit être strictement positive")
	}

	performedAt := time.Now()
	if raw := strings.TrimSpace(req.PerformedAt); raw != "" {
		v, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return nil, coreerrors.BadRequest("Date de réalisation invalide")
		}
		performedAt = v
	}

	var patient patients.Patient
	if err := s.db.Select("id").First(&patient, req.PatientID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, coreerrors.NotFound("PATIENT")
		}
		return nil, err
	}

	var catalog act_catalog.Entry
	if err := s.db.First(&catalog, req.ActCatalogEntryID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, coreerrors.NotFound("ACT_CATALOG")
		}
		return nil, err
	}
	if !catalog.IsActive {
		return nil, coreerrors.BadRequest("L'acte catalogue est inactif")
	}

	if err := s.validateOptionalContext(req); err != nil {
		return nil, err
	}

	item := Act{
		PatientID:         req.PatientID,
		ActCatalogEntryID: catalog.ID,
		ActCode:           catalog.Code,
		ActLabel:          catalog.Label,
		ActDescription:    catalog.Description,
		ActCategory:       catalog.Category,
		BasePrice:         catalog.BasePrice,
		Currency:          catalog.Currency,
		Billable:          catalog.Billable,
		InsuranceEligible: catalog.InsuranceEligible,
		Quantity:          qty,
		PerformedAt:       performedAt,
		PerformedBy:       actorID,
		Status:            StatusPerformed,
		ConsultationID:    req.ConsultationID,
		AppointmentID:     req.AppointmentID,
		HospitalizationID: req.HospitalizationID,
		SourceType:        strings.TrimSpace(req.SourceType),
		SourceID:          req.SourceID,
		CreatedBy:         actorID,
		UpdatedBy:         actorID,
	}

	if err := s.db.Create(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *Service) validateOptionalContext(req CreateRequest) error {
	if req.ConsultationID != nil {
		var n int64
		if err := s.db.Table("consultations").Where("id = ?", *req.ConsultationID).Count(&n).Error; err != nil {
			return err
		}
		if n == 0 {
			return coreerrors.NotFound("CONSULTATION")
		}
	}
	if req.AppointmentID != nil {
		var n int64
		if err := s.db.Table("patient_queue_appointments").Where("id = ?", *req.AppointmentID).Count(&n).Error; err != nil {
			return err
		}
		if n == 0 {
			return coreerrors.NotFound("APPOINTMENT")
		}
	}
	if req.HospitalizationID != nil {
		var n int64
		if err := s.db.Table("hospitalizations").Where("id = ?", *req.HospitalizationID).Count(&n).Error; err != nil {
			return err
		}
		if n == 0 {
			return coreerrors.NotFound("HOSPITALIZATION")
		}
	}
	return nil
}

func (s *Service) GetByID(id uint) (*Act, error) {
	var item Act
	if err := s.db.First(&item, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, coreerrors.NotFound("PERFORMED_ACT")
		}
		return nil, err
	}
	return &item, nil
}

func (s *Service) List(f ListFilter) (*Page, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.Limit < 1 || f.Limit > 100 {
		f.Limit = 20
	}

	q := s.db.Model(&Act{})
	if f.PatientID > 0 {
		q = q.Where("patient_id = ?", f.PatientID)
	}
	if status := strings.ToUpper(strings.TrimSpace(f.Status)); status != "" {
		if status != StatusPerformed && status != StatusVoided {
			return nil, coreerrors.BadRequest("Statut invalide")
		}
		q = q.Where("status = ?", status)
	}
	if cat := strings.ToUpper(strings.TrimSpace(f.Category)); cat != "" {
		if !act_catalog.ValidCategories[cat] {
			return nil, coreerrors.BadRequest("Catégorie invalide")
		}
		q = q.Where("act_category = ?", cat)
	}
	if f.ConsultationID > 0 {
		q = q.Where("consultation_id = ?", f.ConsultationID)
	}
	if from := strings.TrimSpace(f.PerformedFrom); from != "" {
		v, err := time.Parse("2006-01-02", from)
		if err != nil {
			return nil, coreerrors.BadRequest("Date de début invalide")
		}
		q = q.Where("performed_at >= ?", v)
	}
	if to := strings.TrimSpace(f.PerformedTo); to != "" {
		v, err := time.Parse("2006-01-02", to)
		if err != nil {
			return nil, coreerrors.BadRequest("Date de fin invalide")
		}
		q = q.Where("performed_at < ?", v.Add(24*time.Hour))
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, err
	}

	var rows []Act
	if err := q.Order("performed_at DESC, id DESC").
		Offset((f.Page - 1) * f.Limit).
		Limit(f.Limit).
		Find(&rows).Error; err != nil {
		return nil, err
	}

	pages := 0
	if total > 0 {
		pages = int(math.Ceil(float64(total) / float64(f.Limit)))
	}
	return &Page{Data: rows, Page: f.Page, Limit: f.Limit, Total: total, TotalPages: pages}, nil
}

func (s *Service) Void(id uint, req VoidRequest, actorID uint) (*Act, error) {
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		return nil, coreerrors.BadRequest("Motif d'annulation obligatoire")
	}
	if len(reason) > 240 {
		return nil, coreerrors.BadRequest("Motif d'annulation trop long")
	}

	item, err := s.GetByID(id)
	if err != nil {
		return nil, err
	}
	if item.Status != StatusPerformed {
		return nil, coreerrors.Conflict("Seul un acte réalisé peut être annulé")
	}

	now := time.Now()
	item.Status = StatusVoided
	item.VoidedAt = &now
	item.VoidedBy = &actorID
	item.VoidReason = reason
	item.UpdatedBy = actorID

	if err := s.db.Save(item).Error; err != nil {
		return nil, err
	}
	return item, nil
}
