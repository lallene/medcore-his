package company

import (
	"errors"
	"strings"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	coreRepo "github.com/lallene/medcore-his/backend/internal/core/repository"
	"gorm.io/gorm"
)

type Service interface {
	Create(req CreateCompanyRequest) (*InsuranceCompany, error)
	Update(id uint, req UpdateCompanyRequest) (*InsuranceCompany, error)
	Delete(id uint) error
	FindByID(id uint) (*InsuranceCompany, error)
	List() ([]InsuranceCompany, error)
}

type service struct {
	repo Repository
}

func NewService(repo Repository) Service {
	return &service{repo: repo}
}

func (s *service) Create(req CreateCompanyRequest) (*InsuranceCompany, error) {
	code := strings.ToUpper(strings.TrimSpace(req.Code))
	name := strings.ToUpper(strings.TrimSpace(req.Name))

	if code == "" || name == "" {
		return nil, coreerrors.BadRequest("Le code et le nom sont obligatoires")
	}

	if _, err := s.repo.FindByCode(code); err == nil {
		return nil, coreerrors.Conflict("Ce code compagnie existe déjà")
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	if _, err := s.repo.FindByName(name); err == nil {
		return nil, coreerrors.Conflict("Cette compagnie existe déjà")
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	item := &InsuranceCompany{
		Code:        code,
		Name:        name,
		Description: strings.TrimSpace(req.Description),
		Phone:       strings.TrimSpace(req.Phone),
		Email:       strings.TrimSpace(req.Email),
		Address:     strings.TrimSpace(req.Address),
		City:        strings.TrimSpace(req.City),
		Country:     strings.TrimSpace(req.Country),
		IsActive:    true,
	}

	if item.Country == "" {
		item.Country = "Côte d'Ivoire"
	}

	if err := s.repo.Create(item); err != nil {
		return nil, err
	}

	return item, nil
}

func (s *service) Update(id uint, req UpdateCompanyRequest) (*InsuranceCompany, error) {
	item, err := s.repo.FindByID(id)
	if err != nil {
		return nil, coreerrors.NotFound("INSURANCE_COMPANY")
	}

	if strings.TrimSpace(req.Code) != "" {
		item.Code = strings.ToUpper(strings.TrimSpace(req.Code))
	}

	if strings.TrimSpace(req.Name) != "" {
		item.Name = strings.ToUpper(strings.TrimSpace(req.Name))
	}

	item.Description = strings.TrimSpace(req.Description)
	item.Phone = strings.TrimSpace(req.Phone)
	item.Email = strings.TrimSpace(req.Email)
	item.Address = strings.TrimSpace(req.Address)
	item.City = strings.TrimSpace(req.City)

	if strings.TrimSpace(req.Country) != "" {
		item.Country = strings.TrimSpace(req.Country)
	}

	if req.IsActive != nil {
		item.IsActive = *req.IsActive
	}

	if err := s.repo.Update(item); err != nil {
		return nil, err
	}

	return item, nil
}

func (s *service) Delete(id uint) error {
	return s.repo.Delete(id)
}

func (s *service) FindByID(id uint) (*InsuranceCompany, error) {
	item, err := s.repo.FindByID(id)
	if err != nil {
		return nil, coreerrors.NotFound("INSURANCE_COMPANY")
	}

	return item, nil
}

func (s *service) List() ([]InsuranceCompany, error) {
	return s.repo.FindAll(
		coreRepo.OrderBy("id", "DESC"),
	)
}
