package act_catalog

import (
	"errors"
	"math"
	"strings"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"gorm.io/gorm"
)

type Service struct {
	db *gorm.DB
}

func NewService(db *gorm.DB) *Service {
	return &Service{db: db}
}

func normalizeCode(raw string) string {
	return strings.ToUpper(strings.TrimSpace(raw))
}

func normalizeCategory(raw string) string {
	return strings.ToUpper(strings.TrimSpace(raw))
}

func normalizeCurrency(raw string) string {
	c := strings.ToUpper(strings.TrimSpace(raw))
	if c == "" {
		return "XOF"
	}
	return c
}

func validateCategory(cat string) error {
	if !ValidCategories[cat] {
		return coreerrors.BadRequest("Catégorie d'acte invalide")
	}
	return nil
}

func validatePrice(price int64) error {
	if price < 0 {
		return coreerrors.BadRequest("Le prix de base doit être positif ou nul")
	}
	return nil
}

func isDuplicate(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, fragment := range []string{"duplicate key", "unique constraint", "sqlstate 23505"} {
		if strings.Contains(msg, fragment) {
			return true
		}
	}
	return false
}

func (s *Service) Create(req CreateRequest, userID uint) (*Entry, error) {
	code := normalizeCode(req.Code)
	label := strings.TrimSpace(req.Label)
	cat := normalizeCategory(req.Category)
	if code == "" || label == "" {
		return nil, coreerrors.BadRequest("Code et libellé obligatoires")
	}
	if err := validateCategory(cat); err != nil {
		return nil, err
	}
	if err := validatePrice(req.BasePrice); err != nil {
		return nil, err
	}
	currency := normalizeCurrency(req.Currency)
	if len(currency) != 3 {
		return nil, coreerrors.BadRequest("Devise invalide")
	}

	billable := true
	if req.Billable != nil {
		billable = *req.Billable
	}
	insurable := true
	if req.InsuranceEligible != nil {
		insurable = *req.InsuranceEligible
	}
	active := true
	if req.IsActive != nil {
		active = *req.IsActive
	}

	item := Entry{
		Code:              code,
		Label:             label,
		Description:       strings.TrimSpace(req.Description),
		Category:          cat,
		BasePrice:         req.BasePrice,
		Currency:          currency,
		Billable:          billable,
		InsuranceEligible: insurable,
		IsActive:          active,
		CreatedBy:         userID,
		UpdatedBy:         userID,
	}
	if err := s.db.Create(&item).Error; err != nil {
		if isDuplicate(err) {
			return nil, coreerrors.Conflict("Code d'acte déjà utilisé")
		}
		return nil, err
	}
	return &item, nil
}

func (s *Service) GetByID(id uint) (*Entry, error) {
	var item Entry
	if err := s.db.First(&item, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, coreerrors.NotFound("ACT_CATALOG")
		}
		return nil, err
	}
	return &item, nil
}

func (s *Service) List(f ListFilter) (*Page, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.Limit < 1 || f.Limit > 100 {
		f.Limit = 20
	}

	q := s.db.Model(&Entry{})
	if f.Active != nil {
		q = q.Where("is_active = ?", *f.Active)
	} else {
		// Default selector: active only.
		q = q.Where("is_active = ?", true)
	}
	if cat := normalizeCategory(f.Category); cat != "" {
		if err := validateCategory(cat); err != nil {
			return nil, err
		}
		q = q.Where("category = ?", cat)
	}
	if search := strings.TrimSpace(f.Search); search != "" {
		like := "%" + search + "%"
		q = q.Where("code ILIKE ? OR label ILIKE ?", like, like)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, err
	}

	var rows []Entry
	if err := q.Order("category, label, code").
		Offset((f.Page - 1) * f.Limit).
		Limit(f.Limit).
		Find(&rows).Error; err != nil {
		return nil, err
	}

	pages := 0
	if total > 0 {
		pages = int(math.Ceil(float64(total) / float64(f.Limit)))
	}
	return &Page{Data: rows, Page: f.Page, Limit: f.Limit, Total: total, TotalPages: pages}, nil
}

func (s *Service) Update(id uint, req UpdateRequest, userID uint) (*Entry, error) {
	item, err := s.GetByID(id)
	if err != nil {
		return nil, err
	}

	label := strings.TrimSpace(req.Label)
	cat := normalizeCategory(req.Category)
	if label == "" {
		return nil, coreerrors.BadRequest("Libellé obligatoire")
	}
	if err := validateCategory(cat); err != nil {
		return nil, err
	}
	if err := validatePrice(req.BasePrice); err != nil {
		return nil, err
	}
	currency := normalizeCurrency(req.Currency)
	if len(currency) != 3 {
		return nil, coreerrors.BadRequest("Devise invalide")
	}

	item.Label = label
	item.Description = strings.TrimSpace(req.Description)
	item.Category = cat
	item.BasePrice = req.BasePrice
	item.Currency = currency
	item.UpdatedBy = userID
	if req.Billable != nil {
		item.Billable = *req.Billable
	}
	if req.InsuranceEligible != nil {
		item.InsuranceEligible = *req.InsuranceEligible
	}
	if req.IsActive != nil {
		item.IsActive = *req.IsActive
	}

	if err := s.db.Save(item).Error; err != nil {
		if isDuplicate(err) {
			return nil, coreerrors.Conflict("Code d'acte déjà utilisé")
		}
		return nil, err
	}
	return item, nil
}
