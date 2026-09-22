package act_catalog

import "time"

// ValidCategories is the LOT27B bounded category allow-list.
// MEDICATION is intentionally excluded.
var ValidCategories = map[string]bool{
	"CONSULTATION":    true,
	"LABORATORY":      true,
	"IMAGING":         true,
	"HOSPITALIZATION": true,
	"PROCEDURE":       true,
	"OTHER":           true,
}

// Entry is the canonical ACTES catalogue row (LOT27B).
// Independent from billing.Tariff, MedicalExam, and future performed acts.
type Entry struct {
	ID                uint      `gorm:"primaryKey" json:"id"`
	Code              string    `gorm:"size:60;not null;uniqueIndex" json:"code"`
	Label             string    `gorm:"size:200;not null" json:"label"`
	Description       string    `gorm:"type:text" json:"description"`
	Category          string    `gorm:"size:30;not null;index:idx_act_catalog_category_active,priority:1;check:act_catalog_category_valid,category IN ('CONSULTATION','LABORATORY','IMAGING','HOSPITALIZATION','PROCEDURE','OTHER')" json:"category"`
	BasePrice         int64     `gorm:"not null;check:act_catalog_base_price_nonnegative,base_price >= 0" json:"basePrice"`
	Currency          string    `gorm:"size:3;not null;default:XOF" json:"currency"`
	Billable          bool      `gorm:"not null;default:true" json:"billable"`
	InsuranceEligible bool      `gorm:"not null;default:true" json:"insuranceEligible"`
	IsActive          bool      `gorm:"not null;default:true;index;index:idx_act_catalog_category_active,priority:2" json:"isActive"`
	CreatedBy         uint      `gorm:"not null;index" json:"createdBy"`
	UpdatedBy         uint      `gorm:"not null;index" json:"updatedBy"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

func (Entry) TableName() string { return "act_catalog_entries" }
