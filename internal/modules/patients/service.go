package patients

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lallene/medcore-his/backend/internal/core/audit"
	"github.com/lallene/medcore-his/backend/internal/core/events"
	coreRepo "github.com/lallene/medcore-his/backend/internal/core/repository"

	"gorm.io/gorm"
)

type Service interface {
	Create(req CreatePatientRequest) (*Patient, error)
	Update(id uint, req UpdatePatientRequest) (*Patient, error)
	Delete(id uint) error
	FindByID(id uint) (*Patient, error)
	List() ([]Patient, error)
	Count() (int64, error)
	ListPaginated(page int, limit int, search string) (*coreRepo.PageResult[Patient], error)
}

type service struct {
	repo Repository
}

func NewService(repo Repository) Service {
	return &service{repo: repo}
}

func (s *service) Create(req CreatePatientRequest) (*Patient, error) {
	nom := strings.TrimSpace(strings.ToUpper(req.Nom))

	if nom == "" {
		return nil, errors.New("le nom est obligatoire")
	}

	if req.Telephone != "" {
		existing, err := s.repo.FindByTelephone(strings.TrimSpace(req.Telephone))

		if err == nil && existing != nil {
			return nil, errors.New("ce téléphone est déjà utilisé")
		}

		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}

	count, err := s.repo.Count()

	if err != nil {
		return nil, err
	}

	year := time.Now().Year()
	numeroDossier := fmt.Sprintf("PAT-%d-%04d", year, count+1)
	codePatient := fmt.Sprintf("P%04d", count+1)

	dateNaissance := parseDate(req.DateNaissance)

	patient := &Patient{
		CodePatient:     codePatient,
		NumeroDossier:   numeroDossier,
		Nom:             nom,
		Prenoms:         strings.TrimSpace(req.Prenoms),
		Sexe:            strings.TrimSpace(req.Sexe),
		DateNaissance:   dateNaissance,
		Age:             req.Age,
		Telephone:       strings.TrimSpace(req.Telephone),
		Quartier:        strings.TrimSpace(req.Quartier),
		PersonneContact: strings.TrimSpace(req.PersonneContact),
		IsAssure:        req.IsAssure,
		TauxCouverture:  req.TauxCouverture,
		MatriculeAssure: strings.TrimSpace(req.MatriculeAssure),
	}

	if err := s.repo.Create(patient); err != nil {
		return nil, err
	}

	_ = events.DefaultBus.Publish(
		PatientCreated{
			PatientID: patient.ID,
		},
	)
	_ = events.DefaultBus.Publish(
		audit.AuditEvent{
			Module:   "PATIENT",
			Action:   "CREATE",
			RecordID: patient.ID,
			NewValue: fmt.Sprintf("Patient %s %s créé", patient.Nom, patient.Prenoms),
			At:       time.Now(),
		},
	)

	return patient, nil
}

func (s *service) Update(id uint, req UpdatePatientRequest) (*Patient, error) {
	patient, err := s.repo.FindByID(id)

	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(req.Nom) != "" {
		patient.Nom = strings.ToUpper(strings.TrimSpace(req.Nom))
	}

	patient.Prenoms = strings.TrimSpace(req.Prenoms)
	patient.Sexe = strings.TrimSpace(req.Sexe)
	patient.DateNaissance = parseDate(req.DateNaissance)
	patient.Age = req.Age
	patient.Telephone = strings.TrimSpace(req.Telephone)
	patient.Quartier = strings.TrimSpace(req.Quartier)
	patient.PersonneContact = strings.TrimSpace(req.PersonneContact)
	patient.IsAssure = req.IsAssure
	patient.TauxCouverture = req.TauxCouverture
	patient.MatriculeAssure = strings.TrimSpace(req.MatriculeAssure)

	if err := s.repo.Update(patient); err != nil {
		return nil, err
	}

	return patient, nil
}

func (s *service) Delete(id uint) error {
	return s.repo.Delete(id)
}

func (s *service) FindByID(id uint) (*Patient, error) {
	return s.repo.FindByID(id)
}

func (s *service) List() ([]Patient, error) {
	return s.repo.FindAll(
		coreRepo.OrderBy("id", "DESC"),
	)
}

func (s *service) Count() (int64, error) {
	return s.repo.Count()
}

func parseDate(value string) *time.Time {
	if value == "" {
		return nil
	}

	parsed, err := time.Parse("2006-01-02", value)

	if err != nil {
		return nil
	}

	return &parsed
}
func (s *service) ListPaginated(page int, limit int, search string) (*coreRepo.PageResult[Patient], error) {
	return s.repo.Paginate(
		page,
		limit,
		coreRepo.Search(search, "nom", "prenoms", "telephone", "numero_dossier"),
		coreRepo.OrderBy("id", "DESC"),
	)
}
