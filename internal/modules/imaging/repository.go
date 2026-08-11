package imaging

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
			ConsultationID, MedicalExamID, PatientID uint
			PrescribedBy                             *uint
			Priority, ExamCode                       string
			CreatedAt                                time.Time
			MedicalRecordID                          *uint
		}
		var sources []source
		if err := tx.Raw(`SELECT cer.consultation_id,cer.medical_exam_id,c.patient_id,cer.prescribed_by,
			COALESCE(NULLIF(cer.priority,''),'ROUTINE') priority,me.code exam_code,
			COALESCE(cer.created_at,c.created_at) created_at,mr.id medical_record_id
			FROM consultation_exam_requests cer JOIN consultations c ON c.id=cer.consultation_id
			JOIN medical_exams me ON me.id=cer.medical_exam_id LEFT JOIN medical_records mr ON mr.patient_id=c.patient_id
			LEFT JOIN imaging_orders io ON io.consultation_id=cer.consultation_id AND io.medical_exam_id=cer.medical_exam_id
			WHERE io.id IS NULL AND LOWER(BTRIM(me.category))='imagerie'`).Scan(&sources).Error; err != nil {
			return err
		}
		for _, src := range sources {
			creator, prescribed := authorID, uint(0)
			if src.PrescribedBy != nil && *src.PrescribedBy > 0 {
				creator, prescribed = *src.PrescribedBy, *src.PrescribedBy
			}
			o := Order{OrderNumber: fmt.Sprintf("PENDING-%d-%d", src.ConsultationID, src.MedicalExamID), AccessionNumber: fmt.Sprintf("PENDING-%d-%d", src.ConsultationID, src.MedicalExamID), ConsultationID: src.ConsultationID, MedicalExamID: src.MedicalExamID, PatientID: src.PatientID, MedicalRecordID: src.MedicalRecordID, Modality: modalityForExamCode(src.ExamCode), Priority: src.Priority, Status: StatusOrdered, PrescribedBy: prescribed, CreatedBy: creator, UpdatedBy: creator, CreatedAt: src.CreatedAt}
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "consultation_id"}, {Name: "medical_exam_id"}}, DoNothing: true}).Create(&o).Error; err != nil {
				return err
			}
			if o.ID > 0 {
				number := fmt.Sprintf("IMG-%06d", o.ID)
				if err := tx.Model(&o).Updates(map[string]interface{}{"order_number": number, "accession_number": number}).Error; err != nil {
					return err
				}
				if src.MedicalRecordID != nil {
					if err := createEvent(tx, *src.MedicalRecordID, src.PatientID, "imaging_order_created", "Demande d’imagerie créée", number, o.ID, creator); err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
}

func (r *Repository) List(f ListFilter) (*ListResult, error) {
	q := r.db.Table("imaging_orders io").Joins("JOIN consultations c ON c.id=io.consultation_id").Joins("JOIN patients p ON p.id=io.patient_id").Joins("JOIN medical_exams me ON me.id=io.medical_exam_id").Joins("LEFT JOIN imaging_reports ir ON ir.order_id=io.id").Where("LOWER(BTRIM(me.category))='imagerie'")
	if f.Status != "" {
		q = q.Where("io.status=?", f.Status)
	}
	if f.Priority != "" {
		q = q.Where("io.priority=?", f.Priority)
	}
	if f.Modality != "" {
		q = q.Where("io.modality=?", f.Modality)
	}
	if f.Service != "" {
		q = q.Where("LOWER(c.service)=LOWER(?)", f.Service)
	}
	if f.Date != "" {
		q = q.Where("DATE(COALESCE(ir.validated_at,io.performed_at,io.scheduled_at,io.created_at))=?", f.Date)
	}
	if f.PatientID != nil {
		q = q.Where("io.patient_id=?", *f.PatientID)
	}
	if f.ConsultationID != nil {
		q = q.Where("io.consultation_id=?", *f.ConsultationID)
	}
	if f.Search != "" {
		like := "%" + f.Search + "%"
		q = q.Where("io.order_number ILIKE ? OR p.nom ILIKE ? OR p.prenoms ILIKE ? OR p.code_patient ILIKE ? OR me.name ILIKE ? OR me.code ILIKE ?", like, like, like, like, like, like)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, err
	}
	items := []ListItem{}
	err := q.Select(`io.id,io.order_number,io.patient_id,TRIM(CONCAT(p.nom,' ',p.prenoms)) patient_name,p.code_patient patient_code,io.consultation_id,me.code exam_code,me.name exam_name,me.category,io.modality,c.service,c.doctor_name prescriber,io.created_at prescribed_at,io.priority,io.status,io.scheduled_at,io.performed_at`).Order("io.created_at DESC,io.id DESC").Offset((f.Page - 1) * f.Limit).Limit(f.Limit).Scan(&items).Error
	return &ListResult{Data: items, Page: f.Page, Limit: f.Limit, Total: total, TotalPages: int((total + int64(f.Limit) - 1) / int64(f.Limit))}, err
}

func (r *Repository) Find(id uint) (*Order, error) {
	var o Order
	err := r.db.Preload("Report").First(&o, id).Error
	if err == nil {
		var c struct{ PatientName, PatientCode, ExamName, ExamCode, Category, Service, Prescriber string }
		err = r.db.Table("imaging_orders io").Select("TRIM(CONCAT(p.nom,' ',p.prenoms)) patient_name,p.code_patient patient_code,me.name exam_name,me.code exam_code,me.category,c.service,c.doctor_name prescriber").Joins("JOIN patients p ON p.id=io.patient_id").Joins("JOIN consultations c ON c.id=io.consultation_id").Joins("JOIN medical_exams me ON me.id=io.medical_exam_id").Where("io.id=?", id).Scan(&c).Error
		o.PatientName, o.PatientCode, o.ExamName, o.ExamCode, o.Category, o.Service, o.Prescriber = c.PatientName, c.PatientCode, c.ExamName, c.ExamCode, c.Category, c.Service, c.Prescriber
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
	return tx.Create(&medical_records.MedicalTimelineEvent{MedicalRecordID: recordID, PatientID: patientID, EventType: eventType, Category: "imaging", Title: title, Description: description, ReferenceType: "imaging_order", ReferenceID: &orderID, Severity: "info", EventDate: time.Now(), CreatedBy: authorID}).Error
}
