package coverage

import (
	"strings"
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	coreRepo "github.com/lallene/medcore-his/backend/internal/core/repository"
)

type Service interface {
	Create(req CreateCoverageRequest) (*PatientCoverage, error)
	Update(id uint, req UpdateCoverageRequest) (*PatientCoverage, error)
	Delete(id uint) error
	FindByID(id uint) (*PatientCoverage, error)
	List() ([]PatientCoverage, error)
	FindActiveByPatient(patientID uint) ([]PatientCoverage, error)
}

type service struct {
	repo Repository
}

func NewService(repo Repository) Service {
	return &service{repo: repo}
}

func parseDate(value string) *time.Time {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return nil
	}

	return &parsed
}

func (s *service) Create(req CreateCoverageRequest) (*PatientCoverage, error) {
	if req.PatientID == 0 || req.CompanyID == 0 || req.GuarantorID == 0 {
		return nil, coreerrors.BadRequest("Patient, compagnie et garant sont obligatoires")
	}

	item := &PatientCoverage{
		PatientID:    req.PatientID,
		CompanyID:    req.CompanyID,
		GuarantorID:  req.GuarantorID,
		MemberNumber: strings.ToUpper(strings.TrimSpace(req.MemberNumber)),
		Subscriber:   strings.TrimSpace(req.Subscriber),
		Beneficiary:  strings.TrimSpace(req.Beneficiary),
		CoverageRate: req.CoverageRate,
		ValidFrom:    parseDate(req.ValidFrom),
		ValidTo:      parseDate(req.ValidTo),
		IsPrincipal:  req.IsPrincipal,
		IsActive:     true,
	}

	if item.MemberNumber == "" {
		return nil, coreerrors.BadRequest("Le matricule assuré est obligatoire")
	}

	if err := s.repo.Create(item); err != nil {
		return nil, err
	}

	return s.FindByID(item.ID)
}

func (s *service) Update(id uint, req UpdateCoverageRequest) (*PatientCoverage, error) {
	item, err := s.repo.FindByID(id)
	if err != nil {
		return nil, coreerrors.NotFound("PATIENT_COVERAGE")
	}

	if req.CompanyID != 0 {
		item.CompanyID = req.CompanyID
	}

	if req.GuarantorID != 0 {
		item.GuarantorID = req.GuarantorID
	}

	if strings.TrimSpace(req.MemberNumber) != "" {
		item.MemberNumber = strings.ToUpper(strings.TrimSpace(req.MemberNumber))
	}

	item.Subscriber = strings.TrimSpace(req.Subscriber)
	item.Beneficiary = strings.TrimSpace(req.Beneficiary)
	item.CoverageRate = req.CoverageRate
	item.ValidFrom = parseDate(req.ValidFrom)
	item.ValidTo = parseDate(req.ValidTo)

	if req.IsPrincipal != nil {
		item.IsPrincipal = *req.IsPrincipal
	}

	if req.IsActive != nil {
		item.IsActive = *req.IsActive
	}

	if err := s.repo.Update(item); err != nil {
		return nil, err
	}

	return s.FindByID(item.ID)
}

func (s *service) Delete(id uint) error {
	return s.repo.Delete(id)
}

func (s *service) FindByID(id uint) (*PatientCoverage, error) {
	item, err := s.repo.FindByID(
		id,
		coreRepo.Preload("Patient", "Company", "Guarantor"),
	)

	if err != nil {
		return nil, coreerrors.NotFound("PATIENT_COVERAGE")
	}

	return item, nil
}

func (s *service) List() ([]PatientCoverage, error) {
	return s.repo.FindAll(
		coreRepo.Preload("Patient", "Company", "Guarantor"),
		coreRepo.OrderBy("id", "DESC"),
	)
}

func (s *service) FindActiveByPatient(patientID uint) ([]PatientCoverage, error) {
	return s.repo.FindActiveByPatient(patientID)
}
