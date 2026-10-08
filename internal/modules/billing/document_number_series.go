package billing

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Document numbering for official clinic documents that require transactional gapless series.
//
// Guarantee (NOT a PostgreSQL SEQUENCE):
//
//	SELECT … FOR UPDATE on (document_type, year) → LastNumber++ → format → persist in same TX.
//
// Rollback undoes the counter increment, so failed executions consume no committed number.
// Concurrent successful commits get distinct consecutive values within the year.
const (
	DocumentTypeRefund = "REFUND"
	RefundNumberPrefix = "RMB"
)

// DocumentNumberSeries is the transactional counter authority for gapless yearly document numbers.
type DocumentNumberSeries struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	DocumentType string    `gorm:"size:40;not null;uniqueIndex:ux_doc_number_series_type_year,priority:1" json:"documentType"`
	Year         int       `gorm:"not null;uniqueIndex:ux_doc_number_series_type_year,priority:2" json:"year"`
	LastNumber   int64     `gorm:"not null" json:"lastNumber"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

func (DocumentNumberSeries) TableName() string { return "billing_document_number_series" }

// refundDocumentLocation resolves the calendar zone for RMB year boundaries.
// Prefer MEDCORE_BUSINESS_TIMEZONE when set/valid (clinic config); otherwise UTC
// — matching MedCore's explicit-UTC default (never process time.Local).
func refundDocumentLocation() *time.Location {
	name := strings.TrimSpace(os.Getenv("MEDCORE_BUSINESS_TIMEZONE"))
	if name == "" {
		return time.UTC
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return loc
}

// RefundDocumentYear returns the calendar year used for RMB numbering.
func RefundDocumentYear(at time.Time) int {
	return at.In(refundDocumentLocation()).Year()
}

// FormatRefundNumber builds RMB-YYYY-NNNNNN (zero-padded 6 digits).
func FormatRefundNumber(year int, seq int64) string {
	return fmt.Sprintf("%s-%04d-%06d", RefundNumberPrefix, year, seq)
}

// AllocateRefundNumber locks the REFUND series row for the year, increments, and returns the official number.
// Must run inside the same database transaction as Refund execution.
func AllocateRefundNumber(tx *gorm.DB, at time.Time) (string, error) {
	year := RefundDocumentYear(at)
	now := at.UTC()

	var series DocumentNumberSeries
	e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("document_type=? AND year=?", DocumentTypeRefund, year).
		First(&series).Error
	if e != nil {
		if e != gorm.ErrRecordNotFound {
			return "", e
		}
		// Insert seed row then lock it (handles concurrent first-of-year race via unique constraint).
		series = DocumentNumberSeries{
			DocumentType: DocumentTypeRefund,
			Year:         year,
			LastNumber:   0,
			UpdatedAt:    now,
		}
		if createErr := tx.Create(&series).Error; createErr != nil {
			// Concurrent insert won — lock the winner.
			if e2 := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("document_type=? AND year=?", DocumentTypeRefund, year).
				First(&series).Error; e2 != nil {
				return "", e2
			}
		} else {
			// Re-select FOR UPDATE to serialize with other TX that may have raced past Create.
			if e2 := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("id=?", series.ID).First(&series).Error; e2 != nil {
				return "", e2
			}
		}
	}

	series.LastNumber++
	series.UpdatedAt = now
	if e := tx.Model(&series).Updates(map[string]any{
		"last_number": series.LastNumber,
		"updated_at":  series.UpdatedAt,
	}).Error; e != nil {
		return "", e
	}
	return FormatRefundNumber(year, series.LastNumber), nil
}
