package service

import (
	"github.com/lallene/medcore-his/backend/internal/core/repository"
)

type CRUDService[T any] interface {
	Create(entity *T) (*T, error)
	Update(entity *T) (*T, error)
	Delete(id uint) error
	FindByID(id uint) (*T, error)
	FindAll() ([]T, error)
	Count() (int64, error)
}

type crudService[T any] struct {
	repo repository.Repository[T]
}

func NewCRUDService[T any](repo repository.Repository[T]) CRUDService[T] {
	return &crudService[T]{repo: repo}
}

func (s *crudService[T]) Create(entity *T) (*T, error) {
	if err := s.repo.Create(entity); err != nil {
		return nil, err
	}

	return entity, nil
}

func (s *crudService[T]) Update(entity *T) (*T, error) {
	if err := s.repo.Update(entity); err != nil {
		return nil, err
	}

	return entity, nil
}

func (s *crudService[T]) Delete(id uint) error {
	return s.repo.Delete(id)
}

func (s *crudService[T]) FindByID(id uint) (*T, error) {
	return s.repo.FindByID(id)
}

func (s *crudService[T]) FindAll() ([]T, error) {
	return s.repo.FindAll()
}

func (s *crudService[T]) Count() (int64, error) {
	return s.repo.Count()
}
