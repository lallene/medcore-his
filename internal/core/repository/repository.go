package repository

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
)

type Option func(*gorm.DB) *gorm.DB

type PageResult[T any] struct {
	Data       []T   `json:"data"`
	Page       int   `json:"page"`
	Limit      int   `json:"limit"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"totalPages"`
}

type Repository[T any] interface {
	Create(entity *T) error
	Update(entity *T) error
	Delete(id uint) error
	FindByID(id uint, opts ...Option) (*T, error)
	FindAll(opts ...Option) ([]T, error)
	Paginate(page int, limit int, opts ...Option) (*PageResult[T], error)
	Count(opts ...Option) (int64, error)
	Exists(id uint) (bool, error)
	Transaction(fn func(tx *gorm.DB) error) error
	DB() *gorm.DB
}

type repository[T any] struct {
	db *gorm.DB
}

func New[T any](db *gorm.DB) Repository[T] {
	return &repository[T]{db: db}
}

func (r *repository[T]) DB() *gorm.DB {
	return r.db
}

func (r *repository[T]) Create(entity *T) error {
	return r.db.Create(entity).Error
}

func (r *repository[T]) Update(entity *T) error {
	return r.db.Save(entity).Error
}

func (r *repository[T]) Delete(id uint) error {
	var entity T

	return r.db.Delete(&entity, id).Error
}

func (r *repository[T]) FindByID(id uint, opts ...Option) (*T, error) {
	var entity T

	query := r.db.Model(&entity)

	for _, opt := range opts {
		query = opt(query)
	}

	if err := query.First(&entity, id).Error; err != nil {
		return nil, err
	}

	return &entity, nil
}

func (r *repository[T]) FindAll(opts ...Option) ([]T, error) {
	var entities []T
	var entity T

	query := r.db.Model(&entity)

	for _, opt := range opts {
		query = opt(query)
	}

	if err := query.Find(&entities).Error; err != nil {
		return nil, err
	}

	return entities, nil
}

func (r *repository[T]) Paginate(page int, limit int, opts ...Option) (*PageResult[T], error) {
	var entities []T
	var entity T
	var total int64

	if page < 1 {
		page = 1
	}

	if limit < 1 {
		limit = 20
	}

	if limit > 100 {
		limit = 100
	}

	query := r.db.Model(&entity)

	for _, opt := range opts {
		query = opt(query)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}

	offset := (page - 1) * limit

	if err := query.Offset(offset).Limit(limit).Find(&entities).Error; err != nil {
		return nil, err
	}

	totalPages := int((total + int64(limit) - 1) / int64(limit))

	return &PageResult[T]{
		Data:       entities,
		Page:       page,
		Limit:      limit,
		Total:      total,
		TotalPages: totalPages,
	}, nil
}

func (r *repository[T]) Count(opts ...Option) (int64, error) {
	var entity T
	var count int64

	query := r.db.Model(&entity)

	for _, opt := range opts {
		query = opt(query)
	}

	err := query.Count(&count).Error

	return count, err
}

func (r *repository[T]) Exists(id uint) (bool, error) {
	var entity T
	var count int64

	err := r.db.Model(&entity).Where("id = ?", id).Count(&count).Error

	return count > 0, err
}

func (r *repository[T]) Transaction(fn func(tx *gorm.DB) error) error {
	return r.db.Transaction(fn)
}

func Where(field string, operator string, value any) Option {
	return func(db *gorm.DB) *gorm.DB {
		return db.Where(fmt.Sprintf("%s %s ?", field, operator), value)
	}
}

func Filter(field string, value any) Option {
	return Where(field, "=", value)
}

func Like(field string, value string) Option {
	return func(db *gorm.DB) *gorm.DB {
		return db.Where(fmt.Sprintf("%s ILIKE ?", field), "%"+value+"%")
	}
}

func Search(value string, fields ...string) Option {
	return func(db *gorm.DB) *gorm.DB {
		value = strings.TrimSpace(value)

		if value == "" || len(fields) == 0 {
			return db
		}

		conditions := make([]string, 0, len(fields))
		args := make([]any, 0, len(fields))

		for _, field := range fields {
			conditions = append(conditions, fmt.Sprintf("%s ILIKE ?", field))
			args = append(args, "%"+value+"%")
		}

		return db.Where(strings.Join(conditions, " OR "), args...)
	}
}

func OrderBy(field string, direction string) Option {
	return func(db *gorm.DB) *gorm.DB {
		direction = strings.ToUpper(direction)

		if direction != "ASC" && direction != "DESC" {
			direction = "ASC"
		}

		return db.Order(fmt.Sprintf("%s %s", field, direction))
	}
}

func Preload(relations ...string) Option {
	return func(db *gorm.DB) *gorm.DB {
		for _, relation := range relations {
			db = db.Preload(relation)
		}

		return db
	}
}
