package performed_acts

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/modules/act_catalog"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/authorization"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Service struct {
	db               *gorm.DB
	producersEnabled bool
}

func NewService(db *gorm.DB) *Service {
	return &Service{db: db}
}

// WithProducersEnabled returns a shallow copy reflecting whether clinical
// producers are fail-closed enforced (PERFORMED_ACT_PRODUCERS_ENABLED).
func (s *Service) WithProducersEnabled(enabled bool) *Service {
	if s == nil {
		return NewService(nil)
	}
	out := *s
	out.producersEnabled = enabled
	return &out
}

func (s *Service) ProducersEnabled() bool {
	if s == nil {
		return false
	}
	return s.producersEnabled
}

func (s *Service) Create(req CreateRequest, actorID uint) (*Act, error) {
	if req.PatientID == 0 || req.ActCatalogEntryID == 0 {
		return nil, coreerrors.BadRequest("Patient et acte catalogue obligatoires")
	}
	qty := req.Quantity
	if qty == 0 {
		qty = 1
	}
	if qty <= 0 || math.IsNaN(qty) || math.IsInf(qty, 0) {
		return nil, coreerrors.BadRequest("La quantité doit être strictement positive")
	}
	if qty > 1_000_000 {
		return nil, coreerrors.BadRequest("La quantité dépasse la limite autorisée")
	}
	// Manual create must not forge producer identity (source_type, source_id).
	if strings.TrimSpace(req.SourceType) != "" || req.SourceID != nil {
		return nil, coreerrors.BadRequest("sourceType/sourceId réservés aux producteurs cliniques")
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

	if err := s.validateOptionalContextTx(s.db, req.PatientID, req); err != nil {
		return nil, err
	}

	item := Act{
		PatientID:         req.PatientID,
		Quantity:          qty,
		PerformedAt:       performedAt,
		PerformedBy:       actorID,
		Status:            StatusPerformed,
		ConsultationID:    req.ConsultationID,
		AppointmentID:     req.AppointmentID,
		HospitalizationID: req.HospitalizationID,
		CreatedBy:         actorID,
		UpdatedBy:         actorID,
	}
	applyCatalogSnapshot(&item, catalog)

	// LOT28E-B3: same-TX fail-closed — PA row + performed_act_performed.
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := s.validateOptionalContextTx(tx, req.PatientID, req); err != nil {
			return err
		}
		record, err := resolveMedicalRecordForPatientTx(tx, req.PatientID)
		if err != nil {
			return err
		}
		if err := tx.Create(&item).Error; err != nil {
			return err
		}
		return recordPerformedActPerformedTimeline(tx, record, &item, actorID)
	})
	if err != nil {
		return nil, err
	}
	return &item, nil
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

	var voided Act
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&voided, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return coreerrors.NotFound("PERFORMED_ACT")
			}
			return err
		}
		if voided.Status != StatusPerformed {
			return coreerrors.Conflict("Seul un acte réalisé peut être annulé")
		}

		billableKey := fmt.Sprintf("%s:%d", authorization.ReferencePerformedAct, voided.ID)
		if tx.Migrator().HasTable("billing_invoice_lines") {
			var activeBilling int64
			if err := tx.Table("billing_invoice_lines").
				Where("billable_key = ? AND is_active", billableKey).
				Count(&activeBilling).Error; err != nil {
				return err
			}
			if activeBilling > 0 {
				return coreerrors.Conflict("Impossible d'annuler un acte encore facturé activement")
			}
		}

		auth := authorization.NewService(s.db)
		if tx.Migrator().HasTable("insurance_authorizations") {
			if err := auth.CancelOpenPerformedActAuthorizationsInTx(tx, voided.PatientID, voided.ID, actorID); err != nil {
				return err
			}
		}
		if tx.Migrator().HasTable("insurance_authorization_acts") {
			if err := auth.DeactivatePerformedActCoveredLinksInTx(tx, voided.ID); err != nil {
				return err
			}
		}

		now := time.Now()
		voided.Status = StatusVoided
		voided.VoidedAt = &now
		voided.VoidedBy = &actorID
		voided.VoidReason = reason
		voided.UpdatedBy = actorID
		if err := tx.Save(&voided).Error; err != nil {
			return err
		}
		// LOT28E-B3: same-TX fail-closed void chronology (EventDate = VoidedAt).
		record, err := resolveMedicalRecordForPatientTx(tx, voided.PatientID)
		if err != nil {
			return err
		}
		return recordPerformedActVoidedTimeline(tx, record, &voided, actorID, now)
	})
	if err != nil {
		return nil, err
	}
	return &voided, nil
}
