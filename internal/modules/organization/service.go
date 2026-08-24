package organization

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"gorm.io/gorm"
)

type OrganizationService struct{ db *gorm.DB }

func NewService(db *gorm.DB) *OrganizationService { return &OrganizationService{db: db} }

var validTypes = map[string]bool{TypeClinical: true, TypeSurgical: true, TypeMaternity: true, TypeDiagnostic: true, TypePharmacy: true, TypeAdministrative: true, TypeFinancial: true, TypeEmergency: true, TypeOther: true}

func code(value string) string { return strings.ToUpper(strings.TrimSpace(value)) }

func (s *OrganizationService) Catalog(activeOnly bool) (*Catalog, error) {
	q := s.db.Preload("Services", func(db *gorm.DB) *gorm.DB {
		if activeOnly {
			db = db.Where("active")
		}
		return db.Order("sort_order,name")
	}).Order("sort_order,name")
	if activeOnly {
		q = q.Where("active")
	}
	var departments []Department
	if err := q.Find(&departments).Error; err != nil {
		return nil, err
	}
	return &Catalog{Departments: departments}, nil
}

func (s *OrganizationService) Departments() ([]Department, error) {
	var x []Department
	e := s.db.Preload("Services").Order("sort_order,name").Find(&x).Error
	return x, e
}
func (s *OrganizationService) Services(activeOnly bool) ([]Service, error) {
	var x []Service
	q := s.db.Preload("Department").Order("sort_order,name")
	if activeOnly {
		q = q.Where("organization_services.active")
	}
	e := q.Find(&x).Error
	return x, e
}
func (s *OrganizationService) FindService(id uint) (*Service, error) {
	var x Service
	e := s.db.Preload("Department").First(&x, id).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, coreerrors.NotFound("SERVICE")
	}
	return &x, e
}

func (s *OrganizationService) SaveDepartment(id, actor uint, r DepartmentRequest) (*Department, error) {
	r.Code, r.Name = code(r.Code), strings.TrimSpace(r.Name)
	if r.Code == "" || r.Name == "" {
		return nil, coreerrors.BadRequest("Code et nom du département obligatoires")
	}
	var x Department
	if id > 0 {
		if e := s.db.First(&x, id).Error; e != nil {
			return nil, coreerrors.NotFound("DEPARTMENT")
		}
	} else {
		x.Active = true
	}
	x.Code, x.Name, x.Description, x.SortOrder, x.UpdatedBy = r.Code, r.Name, strings.TrimSpace(r.Description), r.SortOrder, actor
	if x.ID == 0 {
		x.CreatedBy = actor
	}
	if r.Active != nil {
		x.Active = *r.Active
	}
	if !x.Active {
		var n int64
		s.db.Model(&Service{}).Where("department_id=? AND active", x.ID).Count(&n)
		if n > 0 {
			return nil, coreerrors.Conflict("Désactivation impossible : services actifs rattachés")
		}
	}
	if e := s.db.Select("*").Save(&x).Error; e != nil {
		if strings.Contains(e.Error(), "duplicate") {
			return nil, coreerrors.Conflict("Code département déjà utilisé")
		}
		return nil, e
	}
	if r.Active != nil {
		if e := s.db.Model(&x).Update("active", *r.Active).Error; e != nil {
			return nil, e
		}
		x.Active = *r.Active
	}
	return &x, nil
}

func (s *OrganizationService) SaveService(id, actor uint, r ServiceRequest) (*Service, error) {
	r.Code, r.Name, r.ServiceType = code(r.Code), strings.TrimSpace(r.Name), code(r.ServiceType)
	if r.DepartmentID == 0 || r.Code == "" || r.Name == "" || !validTypes[r.ServiceType] {
		return nil, coreerrors.BadRequest("Département, code, nom et type valide obligatoires")
	}
	var department Department
	if e := s.db.First(&department, r.DepartmentID).Error; e != nil {
		return nil, coreerrors.NotFound("DEPARTMENT")
	}
	if !department.Active {
		return nil, coreerrors.Conflict("Le département est inactif")
	}
	var x Service
	if id > 0 {
		if e := s.db.First(&x, id).Error; e != nil {
			return nil, coreerrors.NotFound("SERVICE")
		}
	} else {
		x.Active = true
	}
	if id > 0 && x.Active && r.Active != nil && !*r.Active {
		var staff, rooms, stays int64
		s.db.Table("staff_service_assignments a").Joins("JOIN staff_profiles p ON p.id=a.profile_id").Where("a.service_id=? AND a.active AND p.active", id).Count(&staff)
		s.db.Table("hospitalization_rooms").Where("service_id=? AND is_active", id).Count(&rooms)
		s.db.Table("hospitalizations").Where("service_id=? AND status IN ('PLANNED','ADMITTED')", id).Count(&stays)
		if staff+rooms+stays > 0 {
			return nil, coreerrors.Conflict("Désactivation impossible : références actives rattachées")
		}
	}
	x.DepartmentID, x.Code, x.Name, x.ShortName, x.ServiceType = r.DepartmentID, r.Code, r.Name, strings.TrimSpace(r.ShortName), r.ServiceType
	x.Clinical, x.SupportsHospitalization, x.SupportsConsultation, x.SupportsBeds, x.SortOrder = r.Clinical, r.SupportsHospitalization, r.SupportsConsultation, r.SupportsBeds, r.SortOrder
	x.UpdatedBy = actor
	if x.ID == 0 {
		x.CreatedBy = actor
	}
	if r.Active != nil {
		x.Active = *r.Active
	}
	if e := s.db.Select("*").Save(&x).Error; e != nil {
		if strings.Contains(e.Error(), "duplicate") {
			return nil, coreerrors.Conflict("Code service déjà utilisé")
		}
		return nil, e
	}
	if r.Active != nil {
		if e := s.db.Model(&x).Update("active", *r.Active).Error; e != nil {
			return nil, e
		}
		x.Active = *r.Active
	}
	return s.FindService(x.ID)
}

func SupportedTypes() []string {
	out := make([]string, 0, len(validTypes))
	for x := range validTypes {
		out = append(out, x)
	}
	sort.Strings(out)
	return out
}

func ValidateService(db *gorm.DB, id uint, capability string) (*Service, error) {
	var x Service
	if e := db.First(&x, id).Error; e != nil {
		return nil, coreerrors.NotFound("SERVICE")
	}
	if !x.Active {
		return nil, coreerrors.Conflict("Le service est inactif")
	}
	ok := capability == "" || (capability == "consultation" && x.SupportsConsultation) || (capability == "hospitalization" && x.SupportsHospitalization) || (capability == "beds" && x.SupportsBeds)
	if !ok {
		return nil, coreerrors.BadRequest(fmt.Sprintf("Le service %s ne supporte pas cette activité", x.Name))
	}
	return &x, nil
}
