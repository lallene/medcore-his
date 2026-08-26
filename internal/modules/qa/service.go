package qa

import (
	"errors"
	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"gorm.io/gorm"
	"time"
)

type Service struct{ db *gorm.DB }

func NewService(db *gorm.DB) *Service { return &Service{db: db} }
func (s *Service) query(f Filter) *gorm.DB {
	q := s.db.Model(&Campaign{})
	if f.Environment != "" {
		q = q.Where("environment=?", f.Environment)
	}
	if f.Status != "" {
		q = q.Where("status=?", f.Status)
	}
	if f.DateFrom != "" {
		q = q.Where("started_at>=?", f.DateFrom)
	}
	if f.DateTo != "" {
		if d, e := time.Parse("2006-01-02", f.DateTo); e == nil {
			q = q.Where("started_at<?", d.AddDate(0, 0, 1))
		}
	}
	if f.Suite != "" {
		q = q.Where("EXISTS (SELECT 1 FROM qa_test_results r WHERE r.campaign_id=qa_campaigns.id AND r.suite=?)", f.Suite)
	}
	return q
}
func (s *Service) List(f Filter) (*Page, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.Limit < 1 || f.Limit > 100 {
		f.Limit = 20
	}
	q := s.query(f)
	var total int64
	if e := q.Count(&total).Error; e != nil {
		return nil, e
	}
	var data []Campaign
	e := q.Order("started_at DESC,id DESC").Offset((f.Page - 1) * f.Limit).Limit(f.Limit).Find(&data).Error
	return &Page{Data: data, Meta: PageMeta{Page: f.Page, Limit: f.Limit, Total: total, TotalPages: int((total + int64(f.Limit) - 1) / int64(f.Limit))}}, e
}
func (s *Service) Get(id uint) (*Campaign, error) {
	var x Campaign
	e := s.db.Preload("Artifacts").First(&x, id).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, coreerrors.NotFound("QA_CAMPAIGN")
	}
	return &x, e
}
func (s *Service) Results(id uint) ([]TestResult, error) {
	if _, e := s.Get(id); e != nil {
		return nil, e
	}
	var x []TestResult
	e := s.db.Preload("Artifacts").Where("campaign_id=?", id).Order("suite,test_key").Find(&x).Error
	return x, e
}
func (s *Service) KPIs() (*KPIs, error) {
	var o KPIs
	if e := s.db.Model(&Campaign{}).Count(&o.Campaigns).Error; e != nil {
		return nil, e
	}
	s.db.Model(&Campaign{}).Where("status=?", StatusPassed).Count(&o.Passed)
	s.db.Model(&Campaign{}).Where("status=?", StatusFailed).Count(&o.Failed)
	var last Campaign
	if e := s.db.Order("started_at DESC,id DESC").First(&last).Error; e == nil {
		o.LastCampaign = &last
	} else if !errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, e
	}
	if o.Campaigns > 0 {
		o.PassRate = float64(o.Passed) * 100 / float64(o.Campaigns)
	}
	return &o, nil
}
