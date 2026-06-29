package voucher

import (
	"fmt"
	"strings"
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	coreRepo "github.com/lallene/medcore-his/backend/internal/core/repository"
	"github.com/lallene/medcore-his/backend/internal/core/workflow"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/coverage"
	"gorm.io/gorm"
)

type Service interface {
	Create(req CreateVoucherRequest) (*InsuranceVoucher, error)
	Update(id uint, req UpdateVoucherRequest) (*InsuranceVoucher, error)
	Delete(id uint) error
	FindByID(id uint) (*InsuranceVoucher, error)
	List() ([]InsuranceVoucher, error)
	ApplyWorkflow(id uint, req WorkflowActionRequest, actor workflow.Actor) (*InsuranceVoucher, error)
}

type service struct {
	repo         Repository
	db           *gorm.DB
	coverageRepo coverage.Repository
	workflow     *workflow.Engine
}

func NewService(repo Repository, db *gorm.DB, coverageRepo coverage.Repository) Service {
	return &service{
		repo:         repo,
		db:           db,
		coverageRepo: coverageRepo,
		workflow:     workflow.New(db, VoucherWorkflow, nil),
	}
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

func (s *service) Create(req CreateVoucherRequest) (*InsuranceVoucher, error) {
	if req.CoverageID == 0 {
		return nil, coreerrors.BadRequest("La couverture patient est obligatoire")
	}

	cov, err := s.coverageRepo.FindByID(
		req.CoverageID,
		coreRepo.Preload("Patient", "Company", "Guarantor"),
	)

	if err != nil {
		return nil, coreerrors.NotFound("PATIENT_COVERAGE")
	}

	voucherNumber, err := s.generateVoucherNumber()

	if err != nil {
		return nil, err
	}

	coverageRate := cov.CoverageRate
	grossAmount := req.GrossAmount
	coveredAmount := grossAmount * coverageRate / 100
	patientAmount := grossAmount - coveredAmount

	item := &InsuranceVoucher{
		VoucherNumber:  voucherNumber,
		CoverageID:     cov.ID,
		PatientID:      cov.PatientID,
		CompanyID:      cov.CompanyID,
		GuarantorID:    cov.GuarantorID,
		ConsultationID: req.ConsultationID,
		Status:         string(VoucherWorkflow.Initial),
		IssueDate:      parseDate(req.IssueDate),
		GrossAmount:    grossAmount,
		CoveredAmount:  coveredAmount,
		PatientAmount:  patientAmount,
		CoverageRate:   coverageRate,
		Notes:          strings.TrimSpace(req.Notes),
	}

	if item.IssueDate == nil {
		now := time.Now()
		item.IssueDate = &now
	}

	if err := s.repo.Create(item); err != nil {
		return nil, err
	}

	return s.FindByID(item.ID)
}

func (s *service) Update(id uint, req UpdateVoucherRequest) (*InsuranceVoucher, error) {
	item, err := s.repo.FindByID(id)

	if err != nil {
		return nil, coreerrors.NotFound("INSURANCE_VOUCHER")
	}

	if item.Status != "draft" {
		return nil, coreerrors.BadRequest("Seul un bon en brouillon peut être modifié")
	}

	item.IssueDate = parseDate(req.IssueDate)
	item.GrossAmount = req.GrossAmount
	item.CoveredAmount = item.GrossAmount * item.CoverageRate / 100
	item.PatientAmount = item.GrossAmount - item.CoveredAmount
	item.Notes = strings.TrimSpace(req.Notes)

	if err := s.repo.Update(item); err != nil {
		return nil, err
	}

	return s.FindByID(item.ID)
}

func (s *service) Delete(id uint) error {
	item, err := s.repo.FindByID(id)

	if err != nil {
		return coreerrors.NotFound("INSURANCE_VOUCHER")
	}

	if item.Status != "draft" {
		return coreerrors.BadRequest("Seul un bon en brouillon peut être supprimé")
	}

	return s.repo.Delete(id)
}

func (s *service) FindByID(id uint) (*InsuranceVoucher, error) {
	item, err := s.repo.FindByID(
		id,
		coreRepo.Preload("Coverage", "Coverage.Patient", "Coverage.Company", "Coverage.Guarantor"),
	)

	if err != nil {
		return nil, coreerrors.NotFound("INSURANCE_VOUCHER")
	}

	return item, nil
}

func (s *service) List() ([]InsuranceVoucher, error) {
	return s.repo.FindAll(
		coreRepo.OrderBy("id", "DESC"),
	)
}

func (s *service) ApplyWorkflow(id uint, req WorkflowActionRequest, actor workflow.Actor) (*InsuranceVoucher, error) {
	item, err := s.repo.FindByID(id)

	if err != nil {
		return nil, coreerrors.NotFound("INSURANCE_VOUCHER")
	}

	if err := s.workflow.Apply(
		item,
		workflow.Action(req.Action),
		actor,
		strings.TrimSpace(req.Reason),
	); err != nil {
		return nil, coreerrors.BadRequest(err.Error())
	}

	if err := s.repo.Update(item); err != nil {
		return nil, err
	}

	return s.FindByID(item.ID)
}

func (s *service) generateVoucherNumber() (string, error) {
	count, err := s.repo.Count()

	if err != nil {
		return "", err
	}

	return fmt.Sprintf("BPC-%d-%06d", time.Now().Year(), count+1), nil
}
