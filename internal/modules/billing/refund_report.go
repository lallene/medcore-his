package billing

import (
	"math"
	"strings"
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"gorm.io/gorm"
)

// RefundReportFilter scopes EXECUTED refund reporting (LOT29F-I-C).
// Date filters apply to execution date (RefundExecution.ExecutedAt), not request date.
type RefundReportFilter struct {
	DateFrom       string // YYYY-MM-DD inclusive (UTC calendar day)
	DateTo         string // YYYY-MM-DD inclusive
	Method         string
	CashRegisterID uint
	ExecutedBy     uint
	PatientID      uint
	HolderPartyID  uint
	Page           int
	Limit          int
	IncludePII     bool
}

type RefundReportMethodTotal struct {
	Method string `json:"method"`
	Count  int64  `json:"count"`
	Amount int64  `json:"amount"`
}

type RefundReportSummary struct {
	ExecutedRefundCount  int64                     `json:"executedRefundCount"`
	ExecutedRefundAmount int64                     `json:"executedRefundAmount"`
	CashRefundCount      int64                     `json:"cashRefundCount"`
	CashRefundAmount     int64                     `json:"cashRefundAmount"`
	ExternalRefundCount  int64                     `json:"externalRefundCount"`
	ExternalRefundAmount int64                     `json:"externalRefundAmount"`
	ByMethod             []RefundReportMethodTotal `json:"byMethod"`
}

type RefundReportRow struct {
	RefundID               uint      `json:"refundId"`
	RefundNumber           string    `json:"refundNumber"`
	Amount                 int64     `json:"amount"`
	Method                 string    `json:"method"`
	ExecutedAt             time.Time `json:"executedAt"`
	ExecutedBy             uint      `json:"executedBy"`
	ApprovedBy             *uint     `json:"approvedBy,omitempty"`
	PatientID              uint      `json:"patientId"`
	HolderPartyID          uint      `json:"holderPartyId"`
	BeneficiaryDisplayName string    `json:"beneficiaryDisplayName"`
	CashSessionID          *uint     `json:"cashSessionId,omitempty"`
	CashRegisterID         *uint     `json:"cashRegisterId,omitempty"`
	ExternalReference      string    `json:"externalReference,omitempty"`
}

type RefundReportPage struct {
	Summary    RefundReportSummary `json:"summary"`
	Data       []RefundReportRow   `json:"data"`
	Page       int                 `json:"page"`
	Limit      int                 `json:"limit"`
	Total      int64               `json:"total"`
	TotalPages int                 `json:"totalPages"`
}

func (s *Service) refundReportQuery(f RefundReportFilter) (*gorm.DB, error) {
	q := s.db.Table("billing_refunds AS r").
		Joins("JOIN billing_refund_executions AS e ON e.refund_id = r.id").
		Where("r.status = ?", RefundStatusExecuted)

	if f.DateFrom != "" {
		t, e := time.Parse("2006-01-02", f.DateFrom)
		if e != nil {
			return nil, coreerrors.BadRequest("dateFrom invalide")
		}
		q = q.Where("e.executed_at >= ?", t.UTC())
	}
	if f.DateTo != "" {
		t, e := time.Parse("2006-01-02", f.DateTo)
		if e != nil {
			return nil, coreerrors.BadRequest("dateTo invalide")
		}
		q = q.Where("e.executed_at < ?", t.UTC().Add(24*time.Hour))
	}
	if m := strings.TrimSpace(strings.ToUpper(f.Method)); m != "" {
		q = q.Where("e.method = ?", m)
	}
	if f.CashRegisterID > 0 {
		q = q.Where("e.method = ? AND e.cash_register_id = ?", RefundMethodCash, f.CashRegisterID)
	}
	if f.ExecutedBy > 0 {
		q = q.Where("e.executed_by = ?", f.ExecutedBy)
	}
	if f.PatientID > 0 {
		q = q.Where("r.patient_id = ?", f.PatientID)
	}
	if f.HolderPartyID > 0 {
		q = q.Where("r.holder_party_id = ?", f.HolderPartyID)
	}
	return q, nil
}

func (s *Service) GetRefundReport(f RefundReportFilter) (*RefundReportPage, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.Limit < 1 || f.Limit > 100 {
		f.Limit = 20
	}

	base, err := s.refundReportQuery(f)
	if err != nil {
		return nil, err
	}

	type aggRow struct {
		Method string
		Count  int64
		Amount int64
	}
	var aggs []aggRow
	if e := base.Select("e.method AS method, COUNT(*) AS count, COALESCE(SUM(r.amount),0) AS amount").
		Group("e.method").Scan(&aggs).Error; e != nil {
		return nil, e
	}

	sum := RefundReportSummary{ByMethod: make([]RefundReportMethodTotal, 0, len(aggs))}
	for _, a := range aggs {
		sum.ExecutedRefundCount += a.Count
		sum.ExecutedRefundAmount += a.Amount
		sum.ByMethod = append(sum.ByMethod, RefundReportMethodTotal{
			Method: a.Method, Count: a.Count, Amount: a.Amount,
		})
		if a.Method == RefundMethodCash {
			sum.CashRefundCount += a.Count
			sum.CashRefundAmount += a.Amount
		} else {
			sum.ExternalRefundCount += a.Count
			sum.ExternalRefundAmount += a.Amount
		}
	}

	countQ, err := s.refundReportQuery(f)
	if err != nil {
		return nil, err
	}
	var total int64
	if e := countQ.Count(&total).Error; e != nil {
		return nil, e
	}

	listQ, err := s.refundReportQuery(f)
	if err != nil {
		return nil, err
	}
	type scanRow struct {
		RefundID               uint
		RefundNumber           string
		Amount                 int64
		Method                 string
		ExecutedAt             time.Time
		ExecutedBy             uint
		ApprovedBy             *uint
		PatientID              uint
		HolderPartyID          uint
		BeneficiaryDisplayName string
		CashSessionID          *uint
		CashRegisterID         *uint
		ExternalReference      string
	}
	var scanned []scanRow
	if e := listQ.Select(`r.id AS refund_id, r.refund_number, r.amount, e.method, e.executed_at, e.executed_by,
			r.approved_by, r.patient_id, r.holder_party_id, r.beneficiary_display_name,
			e.cash_session_id, e.cash_register_id, e.external_reference`).
		Order("e.executed_at DESC, r.id DESC").
		Offset((f.Page - 1) * f.Limit).Limit(f.Limit).
		Scan(&scanned).Error; e != nil {
		return nil, e
	}

	rows := make([]RefundReportRow, 0, len(scanned))
	for _, srow := range scanned {
		ext := srow.ExternalReference
		if !f.IncludePII {
			ext = maskRailRef(ext)
		}
		rows = append(rows, RefundReportRow{
			RefundID:               srow.RefundID,
			RefundNumber:           srow.RefundNumber,
			Amount:                 srow.Amount,
			Method:                 srow.Method,
			ExecutedAt:             srow.ExecutedAt,
			ExecutedBy:             srow.ExecutedBy,
			ApprovedBy:             srow.ApprovedBy,
			PatientID:              srow.PatientID,
			HolderPartyID:          srow.HolderPartyID,
			BeneficiaryDisplayName: srow.BeneficiaryDisplayName,
			CashSessionID:          srow.CashSessionID,
			CashRegisterID:         srow.CashRegisterID,
			ExternalReference:      ext,
		})
	}

	tp := int(math.Ceil(float64(total) / float64(f.Limit)))
	if tp == 0 {
		tp = 1
	}
	return &RefundReportPage{
		Summary: sum, Data: rows, Page: f.Page, Limit: f.Limit, Total: total, TotalPages: tp,
	}, nil
}
