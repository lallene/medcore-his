package laboratory

import (
	"fmt"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/medical_records"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository struct{ db *gorm.DB }

func NewRepository(db *gorm.DB) *Repository { return &Repository{db: db} }

func (r *Repository) Materialize(authorID uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		type source struct {
			ConsultationID, MedicalExamID, PatientID, PrescribedBy uint
			Priority                                               string
			CreatedAt                                              time.Time
			MedicalRecordID                                        *uint
		}
		var sources []source
		if err := tx.Raw(`SELECT cer.consultation_id, cer.medical_exam_id, c.patient_id,
			cer.prescribed_by, COALESCE(NULLIF(cer.priority,''),'ROUTINE') priority,
			COALESCE(cer.created_at,c.created_at) created_at, mr.id medical_record_id
			FROM consultation_exam_requests cer JOIN consultations c ON c.id=cer.consultation_id
			JOIN medical_exams me ON me.id=cer.medical_exam_id
			LEFT JOIN medical_records mr ON mr.patient_id=c.patient_id
			LEFT JOIN laboratory_orders lo ON lo.consultation_id=cer.consultation_id AND lo.medical_exam_id=cer.medical_exam_id
			WHERE lo.id IS NULL AND LOWER(BTRIM(me.category)) IN ?`, laboratoryCategories).Scan(&sources).Error; err != nil {
			return err
		}
		for _, s := range sources {
			creator := s.PrescribedBy
			if creator == 0 {
				creator = authorID
			}
			o := Order{RequestNumber: fmt.Sprintf("PENDING-%d-%d", s.ConsultationID, s.MedicalExamID), ConsultationID: s.ConsultationID, MedicalExamID: s.MedicalExamID, PatientID: s.PatientID, MedicalRecordID: s.MedicalRecordID, Priority: s.Priority, Status: StatusOrdered, PrescribedBy: s.PrescribedBy, CreatedBy: creator, UpdatedBy: creator, CreatedAt: s.CreatedAt}
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "consultation_id"}, {Name: "medical_exam_id"}}, DoNothing: true}).Create(&o).Error; err != nil {
				return err
			}
			if o.ID > 0 {
				number := fmt.Sprintf("LAB-%06d", o.ID)
				if err := tx.Model(&o).Update("request_number", number).Error; err != nil {
					return err
				}
				if s.MedicalRecordID != nil {
					if err := createEvent(tx, *s.MedicalRecordID, s.PatientID, "lab_order_created", "Demande de laboratoire créée", number, o.ID, creator); err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
}

func (r *Repository) List(f ListFilter) (*ListResult, error) {
	q := r.db.Table("laboratory_orders lo").Joins("JOIN consultations c ON c.id=lo.consultation_id").Joins("JOIN patients p ON p.id=lo.patient_id").Joins("JOIN medical_exams me ON me.id=lo.medical_exam_id").Joins("LEFT JOIN laboratory_samples ls ON ls.order_id=lo.id")
	q = q.Where("LOWER(BTRIM(me.category)) IN ?", laboratoryCategories)
	if f.Status != "" {
		q = q.Where("lo.status=?", f.Status)
	}
	if f.Priority != "" {
		q = q.Where("lo.priority=?", f.Priority)
	}
	if f.Category != "" {
		q = q.Where("LOWER(me.category)=LOWER(?)", f.Category)
	}
	if f.PatientID != nil {
		q = q.Where("lo.patient_id=?", *f.PatientID)
	}
	if f.ConsultationID != nil {
		q = q.Where("lo.consultation_id=?", *f.ConsultationID)
	}
	if f.Search != "" {
		like := "%" + f.Search + "%"
		q = q.Where("lo.request_number ILIKE ? OR p.nom ILIKE ? OR p.prenoms ILIKE ? OR p.code_patient ILIKE ? OR me.name ILIKE ? OR me.code ILIKE ?", like, like, like, like, like, like)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, err
	}
	items := []ListItem{}
	err := q.Select(`lo.id,lo.request_number,lo.patient_id,TRIM(CONCAT(p.nom,' ',p.prenoms)) patient_name,p.code_patient,lo.medical_record_id,lo.consultation_id,me.code exam_code,me.name exam_name,me.category,c.service,c.doctor_name prescriber,lo.created_at prescribed_at,lo.priority,lo.status,COALESCE(ls.sample_identifier,'') sample_identifier`).Order("lo.created_at DESC, lo.id DESC").Offset((f.Page - 1) * f.Limit).Limit(f.Limit).Scan(&items).Error
	return &ListResult{Data: items, Page: f.Page, Limit: f.Limit, Total: total, TotalPages: int((total + int64(f.Limit) - 1) / int64(f.Limit))}, err
}

func (r *Repository) Find(id uint) (*Order, error) {
	var o Order
	err := r.db.Preload("Sample").Preload("Results").First(&o, id).Error
	if err == nil {
		var context struct{ PatientName, PatientCode, ExamName, ExamCode, Category, Service, Prescriber string }
		err = r.db.Table("laboratory_orders lo").Select("TRIM(CONCAT(p.nom,' ',p.prenoms)) patient_name, p.code_patient patient_code, me.name exam_name, me.code exam_code, me.category, c.service, c.doctor_name prescriber").Joins("JOIN patients p ON p.id=lo.patient_id").Joins("JOIN consultations c ON c.id=lo.consultation_id").Joins("JOIN medical_exams me ON me.id=lo.medical_exam_id").Where("lo.id=?", id).Scan(&context).Error
		o.PatientName, o.PatientCode, o.ExamName, o.ExamCode, o.Category, o.Service, o.Prescriber = context.PatientName, context.PatientCode, context.ExamName, context.ExamCode, context.Category, context.Service, context.Prescriber
	}
	return &o, err
}

func (r *Repository) WithLockedOrder(id uint, fn func(*gorm.DB, *Order) error) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var o Order
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&o, id).Error; err != nil {
			return err
		}
		return fn(tx, &o)
	})
}

func createEvent(tx *gorm.DB, recordID, patientID uint, eventType, title, description string, orderID, authorID uint) error {
	return tx.Create(&medical_records.MedicalTimelineEvent{MedicalRecordID: recordID, PatientID: patientID, EventType: eventType, Category: "laboratory", Title: title, Description: description, ReferenceType: "laboratory_order", ReferenceID: &orderID, Severity: "info", EventDate: time.Now(), CreatedBy: authorID}).Error
}
