package guarantor

import (
	"errors"
	"strings"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	coreRepo "github.com/lallene/medcore-his/backend/internal/core/repository"
	"gorm.io/gorm"
)

type Service interface {
	Create(req CreateGuarantorRequest) (*InsuranceGuarantor, error)
	Update(id uint, req UpdateGuarantorRequest) (*InsuranceGuarantor, error)
	Delete(id uint) error
	FindByID(id uint) (*InsuranceGuarantor, error)
	List() ([]InsuranceGuarantor, error)
}

type service struct {
	repo Repository
}

func NewService(repo Repository) Service {
	return &service{repo: repo}
}

func (s *service) Create(req CreateGuarantorRequest) (*InsuranceGuarantor, error) {
	code := strings.ToUpper(strings.TrimSpace(req.Code))
	name := strings.ToUpper(strings.TrimSpace(req.Name))

	if req.CompanyID == 0 {
		return nil, coreerrors.BadRequest("La compagnie est obligatoire")
	}

	if code == "" || name == "" {
		return nil, coreerrors.BadRequest("Le code et le nom du garant sont obligatoires")
	}

	if _, err := s.repo.FindByCompanyAndCode(req.CompanyID, code); err == nil {
		return nil, coreerrors.Conflict("Ce code garant existe déjà pour cette compagnie")
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	item := &InsuranceGuarantor{
		CompanyID:           req.CompanyID,
		Code:                code,
		Name:                name,
		Description:         strings.TrimSpace(req.Description),
		DefaultCoverageRate: req.DefaultCoverageRate,
		PaymentDelayDays:    req.PaymentDelayDays,
		IsActive:            true,
	}

	if err := s.repo.Create(item); err != nil {
		return nil, err
	}

	return item, nil
}

func (s *service) Update(id uint, req UpdateGuarantorRequest) (*InsuranceGuarantor, error) {
	item, err := s.repo.FindByID(id)

	if err != nil {
		return nil, coreerrors.NotFound("INSURANCE_GUARANTOR")
	}

	if req.CompanyID != 0 {
		item.CompanyID = req.CompanyID
	}

	if strings.TrimSpace(req.Code) != "" {
		item.Code = strings.ToUpper(strings.TrimSpace(req.Code))
	}

	if strings.TrimSpace(req.Name) != "" {
		item.Name = strings.ToUpper(strings.TrimSpace(req.Name))
	}

	item.Description = strings.TrimSpace(req.Description)
	item.DefaultCoverageRate = req.DefaultCoverageRate
	item.PaymentDelayDays = req.PaymentDelayDays

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

func (s *service) FindByID(id uint) (*InsuranceGuarantor, error) {
	item, err := s.repo.FindByID(
		id,
		coreRepo.Preload("Company"),
	)

	if err != nil {
		return nil, coreerrors.NotFound("INSURANCE_GUARANTOR")
	}

	return item, nil
}

func (s *service) List() ([]InsuranceGuarantor, error) {
	return s.repo.FindAll(
		coreRepo.Preload("Company"),
		coreRepo.OrderBy("id", "DESC"),
	)
}
