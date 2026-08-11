package laboratory

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/medical_records"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrInvalidTransition = errors.New("transition laboratoire non autorisée")
var ErrValidated = errors.New("un résultat validé est immuable")

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

func (s *Service) PrepareSample(id, user uint) (*Order, error) {
	return s.transition(id, user, StatusOrdered, StatusSamplePending, "", "")
}

func (s *Service) Collect(id, user uint, req CollectRequest) (*Order, error) {
	err := s.repo.WithLockedOrder(id, func(tx *gorm.DB, o *Order) error {
		if o.Status != StatusSamplePending {
			return ErrInvalidTransition
		}
		now := time.Now()
		identifier := fmt.Sprintf("SMP-%06d", o.ID)
		sample := Sample{OrderID: o.ID, SampleIdentifier: identifier, SampleType: req.SampleType, Status: "COLLECTED", Comment: req.Comment, CollectedBy: user, CollectedAt: now}
		if err := tx.Create(&sample).Error; err != nil {
			return err
		}
		if err := tx.Model(o).Updates(map[string]interface{}{"status": StatusSampleCollected, "updated_by": user}).Error; err != nil {
			return err
		}
		if o.MedicalRecordID != nil {
			return createEvent(tx, *o.MedicalRecordID, o.PatientID, "lab_sample_collected", "Prélèvement collecté", identifier, o.ID, user)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.repo.Find(id)
}
func (s *Service) Start(id, user uint) (*Order, error) {
	return s.transition(id, user, StatusSampleCollected, StatusInProgress, "lab_analysis_started", "Analyse démarrée")
}
func (s *Service) EnterResults(id, user uint, req EnterResultsRequest) (*Order, error) {
	err := s.repo.WithLockedOrder(id, func(tx *gorm.DB, o *Order) error {
		if o.Status == StatusValidated {
			return ErrValidated
		}
		if o.Status != StatusInProgress && o.Status != StatusResultEntered {
			return ErrInvalidTransition
		}
		for _, in := range req.Results {
			flag, numeric := computeFlag(in)
			r := Result{OrderID: o.ID, Parameter: in.Parameter, Value: in.Value, NumericValue: numeric, Unit: in.Unit, ReferenceMin: in.ReferenceMin, ReferenceMax: in.ReferenceMax, ReferenceText: in.ReferenceText, Flag: flag, Comment: in.Comment, EnteredBy: user}
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "order_id"}, {Name: "parameter"}}, DoUpdates: clause.AssignmentColumns([]string{"value", "numeric_value", "unit", "reference_min", "reference_max", "reference_text", "flag", "comment", "entered_by", "updated_at"})}).Create(&r).Error; err != nil {
				return err
			}
			if flag == "CRITICAL" && o.MedicalRecordID != nil {
				alert := medical_records.MedicalAlert{MedicalRecordID: *o.MedicalRecordID, PatientID: o.PatientID, Type: "critical_result", Title: "Résultat critique " + o.RequestNumber + " — " + in.Parameter, Description: in.Value + " " + in.Unit, Severity: "critical", IsActive: true, CreatedBy: user}
				if err := tx.Where("medical_record_id=? AND title=?", *o.MedicalRecordID, alert.Title).FirstOrCreate(&alert).Error; err != nil {
					return err
				}
			}
		}
		if err := tx.Model(o).Updates(map[string]interface{}{"status": StatusResultEntered, "updated_by": user}).Error; err != nil {
			return err
		}
		if o.MedicalRecordID != nil {
			return createEvent(tx, *o.MedicalRecordID, o.PatientID, "lab_result_entered", "Résultat de laboratoire saisi", o.RequestNumber, o.ID, user)
		}
		return nil
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
		if o.Status != StatusResultEntered {
			return ErrInvalidTransition
		}
		var count int64
		if err := tx.Model(&Result{}).Where("order_id=?", id).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return ErrInvalidTransition
		}
		now := time.Now()
		if err := tx.Model(o).Updates(map[string]interface{}{"status": StatusValidated, "validated_at": now, "validated_by": user, "updated_by": user}).Error; err != nil {
			return err
		}
		if o.MedicalRecordID != nil {
			return createEvent(tx, *o.MedicalRecordID, o.PatientID, "lab_result_validated", "Résultat de laboratoire validé", o.RequestNumber, o.ID, user)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.repo.Find(id)
}
func (s *Service) Cancel(id, user uint, reason string) (*Order, error) {
	err := s.repo.WithLockedOrder(id, func(tx *gorm.DB, o *Order) error {
		if o.Status == StatusValidated || o.Status == StatusCancelled {
			return ErrInvalidTransition
		}
		if err := tx.Model(o).Updates(map[string]interface{}{"status": StatusCancelled, "cancelled_reason": reason, "updated_by": user}).Error; err != nil {
			return err
		}
		if o.MedicalRecordID != nil {
			return createEvent(tx, *o.MedicalRecordID, o.PatientID, "lab_order_cancelled", "Demande de laboratoire annulée", reason, o.ID, user)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.repo.Find(id)
}
func (s *Service) transition(id, user uint, from, to, event, title string) (*Order, error) {
	err := s.repo.WithLockedOrder(id, func(tx *gorm.DB, o *Order) error {
		if o.Status != from {
			return ErrInvalidTransition
		}
		if err := tx.Model(o).Updates(map[string]interface{}{"status": to, "updated_by": user}).Error; err != nil {
			return err
		}
		if event != "" && o.MedicalRecordID != nil {
			return createEvent(tx, *o.MedicalRecordID, o.PatientID, event, title, o.RequestNumber, o.ID, user)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.repo.Find(id)
}
func computeFlag(in ResultInput) (string, *float64) {
	v, e := strconv.ParseFloat(in.Value, 64)
	if e != nil {
		return "NORMAL", nil
	}
	if in.CriticalMin != nil && v < *in.CriticalMin {
		return "CRITICAL", &v
	}
	if in.CriticalMax != nil && v > *in.CriticalMax {
		return "CRITICAL", &v
	}
	if in.ReferenceMin != nil && v < *in.ReferenceMin {
		return "LOW", &v
	}
	if in.ReferenceMax != nil && v > *in.ReferenceMax {
		return "HIGH", &v
	}
	return "NORMAL", &v
}
