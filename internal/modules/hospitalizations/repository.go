package hospitalizations

import (
	"errors"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository struct{ db *gorm.DB }

func NewRepository(db *gorm.DB) *Repository { return &Repository{db: db} }

func preload(db *gorm.DB) *gorm.DB {
	return db.Preload("Patient").Preload("SourceConsultation")
}

func (r *Repository) FindByID(id uint) (*Hospitalization, error) {
	var item Hospitalization
	err := preload(r.db).First(&item, id).Error
	return &item, err
}

func (r *Repository) FindByConsultation(id uint) (*Hospitalization, error) {
	var item Hospitalization
	err := preload(r.db).Where("source_consultation_id = ?", id).First(&item).Error
	return &item, err
}

func (r *Repository) List(filter ListFilter) (*ListResult, error) {
	query := r.db.Model(&Hospitalization{})
	if filter.PatientID != nil {
		query = query.Where("patient_id = ?", *filter.PatientID)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.Department != "" {
		query = query.Where("LOWER(department) = LOWER(?)", filter.Department)
	}
	if filter.From != nil {
		query = query.Where("created_at >= ?", *filter.From)
	}
	if filter.To != nil {
		query = query.Where("created_at < ?", *filter.To)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}
	items := make([]Hospitalization, 0)
	err := preload(query).Order("created_at DESC, id DESC").Offset((filter.Page - 1) * filter.Limit).Limit(filter.Limit).Find(&items).Error
	if err != nil {
		return nil, err
	}
	return &ListResult{Data: items, Page: filter.Page, Limit: filter.Limit, Total: total, TotalPages: int((total + int64(filter.Limit) - 1) / int64(filter.Limit))}, nil
}

func lockByID(tx *gorm.DB, id uint) (*Hospitalization, error) {
	var item Hospitalization
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, id).Error
	return &item, err
}

func isDuplicate(err error) bool {
	return err != nil && !errors.Is(err, gorm.ErrRecordNotFound) && (errors.Is(err, gorm.ErrDuplicatedKey) || containsDuplicate(err.Error()))
}

func containsDuplicate(value string) bool {
	for _, fragment := range []string{"duplicate key", "unique constraint", "SQLSTATE 23505"} {
		if strings.Contains(value, fragment) {
			return true
		}
	}
	return false
}
