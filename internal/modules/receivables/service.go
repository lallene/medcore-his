package receivables

import (
	"errors"
	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/modules/billing"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strings"
	"time"
)

type Service struct{ db *gorm.DB }

func NewService(db *gorm.DB) *Service { return &Service{db: db} }
func debtStatus(balance, paid int64, due *time.Time) string {
	if balance <= 0 {
		return "PAID"
	}
	if due != nil && due.Before(time.Now().Truncate(24*time.Hour)) {
		return "OVERDUE"
	}
	if paid > 0 {
		return "PARTIALLY_PAID"
	}
	return "DUE"
}
func (s *Service) base(patient uint) *gorm.DB {
	q := s.db.Table("billing_invoices i").Select(`i.id invoice_id,i.number invoice_number,i.patient_id,TRIM(CONCAT(p.prenoms,' ',p.nom)) patient_name,p.code_patient patient_code,i.created_at invoice_date,i.status invoice_status,i.gross_amount,i.insurance_amount,i.patient_amount patient_due,COALESCE(pay.paid,0) patient_paid,GREATEST(i.patient_amount-COALESCE(pay.paid,0),0) patient_balance,m.due_date,i.coverage_pending,COALESCE(pay.last_payment_at::text,'') last_payment_at,COALESCE(lines.descriptions,'') descriptions`).Joins("JOIN patients p ON p.id=i.patient_id").Joins("LEFT JOIN patient_receivable_metadata m ON m.invoice_id=i.id").Joins("LEFT JOIN (SELECT invoice_id,SUM(amount) paid,MAX(paid_at) last_payment_at FROM billing_payments GROUP BY invoice_id) pay ON pay.invoice_id=i.id").Joins("LEFT JOIN (SELECT invoice_id,string_agg(description, ', ' ORDER BY id) descriptions FROM billing_invoice_lines WHERE is_active GROUP BY invoice_id) lines ON lines.invoice_id=i.id")
	if patient > 0 {
		q = q.Where("i.patient_id=?", patient)
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
	q := s.base(f.PatientID).Where("i.status IN ? AND NOT i.coverage_pending", []string{billing.InvoiceIssued, billing.InvoicePartiallyPaid}).Where("GREATEST(i.patient_amount-COALESCE(pay.paid,0),0)>0")
	if x := strings.TrimSpace(f.Search); x != "" {
		n := "%" + strings.ToLower(x) + "%"
		q = q.Where("LOWER(CONCAT(p.nom,' ',p.prenoms,' ',p.code_patient,' ',i.number)) LIKE ?", n)
	}
	if f.DateFrom != "" {
		q = q.Where("i.created_at::date>=?", f.DateFrom)
	}
	if f.DateTo != "" {
		q = q.Where("i.created_at::date<=?", f.DateTo)
	}
	if f.MinAmount > 0 {
		q = q.Where("GREATEST(i.patient_amount-COALESCE(pay.paid,0),0)>=?", f.MinAmount)
	}
	if f.MaxAmount > 0 {
		q = q.Where("GREATEST(i.patient_amount-COALESCE(pay.paid,0),0)<=?", f.MaxAmount)
	}
	if f.Due == "OVERDUE" {
		q = q.Where("m.due_date<CURRENT_DATE")
	} else if f.Due == "FUTURE" {
		q = q.Where("m.due_date>=CURRENT_DATE")
	} else if f.Due == "NONE" {
		q = q.Where("m.due_date IS NULL")
	}
	switch f.Status {
	case "OVERDUE":
		q = q.Where("m.due_date<CURRENT_DATE")
	case "PARTIALLY_PAID":
		q = q.Where("COALESCE(pay.paid,0)>0 AND (m.due_date IS NULL OR m.due_date>=CURRENT_DATE)")
	case "DUE":
		q = q.Where("COALESCE(pay.paid,0)=0 AND (m.due_date IS NULL OR m.due_date>=CURRENT_DATE)")
	}
	var rows []Item
	var total int64
	if e := q.Session(&gorm.Session{}).Count(&total).Error; e != nil {
		return nil, e
	}
	if e := q.Order("COALESCE(m.due_date,'9999-12-31'),i.created_at").Offset((f.Page - 1) * f.Limit).Limit(f.Limit).Scan(&rows).Error; e != nil {
		return nil, e
	}
	out := make([]Item, 0, len(rows))
	for _, r := range rows {
		r.Status = debtStatus(r.PatientBalance, r.PatientPaid, r.DueDate)
		out = append(out, r)
	}
	return &Page{Items: out, Page: f.Page, Limit: f.Limit, Total: total, TotalPages: int((total + int64(f.Limit) - 1) / int64(f.Limit))}, nil
}
func (s *Service) KPIs() (*KPIs, error) {
	var rows []Item
	if e := s.base(0).
		Where("i.status IN ? AND NOT i.coverage_pending", []string{billing.InvoiceIssued, billing.InvoicePartiallyPaid}).
		Where("GREATEST(i.patient_amount-COALESCE(pay.paid,0),0)>0").
		Scan(&rows).Error; e != nil {
		return nil, e
	}
	k := &KPIs{}
	seen := map[uint]bool{}
	for _, r := range rows {
		r.Status = debtStatus(r.PatientBalance, r.PatientPaid, r.DueDate)
		k.TotalReceivables += r.PatientBalance
		k.UnpaidInvoices++
		seen[r.PatientID] = true
		if r.Status == "OVERDUE" {
			k.OverdueReceivables += r.PatientBalance
		} else {
			k.NonOverdueReceivables += r.PatientBalance
		}
	}
	k.DebtorPatients = int64(len(seen))
	s.db.Table("billing_payments bp").Joins("JOIN billing_invoices i ON i.id=bp.invoice_id AND i.status<>'CANCELLED'").Select("COALESCE(SUM(bp.amount),0)").Scan(&k.CollectedAmount)
	return k, nil
}
func (s *Service) Detail(id uint) (*Detail, error) {
	var row Item
	if e := s.base(0).Where("i.id=?", id).Scan(&row).Error; e != nil {
		return nil, e
	}
	if row.InvoiceID == 0 {
		return nil, coreerrors.NotFound("RECEIVABLE")
	}
	row.Status = debtStatus(row.PatientBalance, row.PatientPaid, row.DueDate)
	d := &Detail{Item: row, Lines: []Line{}, Payments: []Payment{}, FollowUps: []FollowUp{}}
	s.db.Table("billing_invoice_lines").Select("description,act_type,gross_amount,insurance_amount,patient_amount").Where("invoice_id=? AND is_active", id).Order("id").Scan(&d.Lines)
	s.db.Table("billing_payments p").Select("p.id,p.amount,p.payment_method,p.reference,p.paid_at,COALESCE(r.id,0) receipt_id,COALESCE(r.receipt_number,'') receipt_number").Joins("LEFT JOIN cash_receipts r ON r.payment_id=p.id").Where("p.invoice_id=?", id).Order("p.paid_at").Scan(&d.Payments)
	s.db.Where("invoice_id=?", id).Order("created_at DESC").Find(&d.FollowUps)
	return d, nil
}
func parseOptionalDate(v *string) (*time.Time, error) {
	if v == nil || strings.TrimSpace(*v) == "" {
		return nil, nil
	}
	t, e := time.Parse("2006-01-02", *v)
	if e != nil {
		return nil, coreerrors.BadRequest("Date invalide")
	}
	return &t, nil
}

func activeReceivableInvoice(tx *gorm.DB, id uint) (*billing.Invoice, error) {
	var invoice billing.Invoice
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&invoice, id).Error; err != nil {
		return nil, coreerrors.NotFound("INVOICE")
	}
	var paid int64
	if err := tx.Model(&billing.Payment{}).Where("invoice_id=?", id).Select("COALESCE(SUM(amount),0)").Scan(&paid).Error; err != nil {
		return nil, err
	}
	if invoice.CoveragePending || (invoice.Status != billing.InvoiceIssued && invoice.Status != billing.InvoicePartiallyPaid) || invoice.PatientAmount-paid <= 0 {
		return nil, coreerrors.Conflict("La facture n'est pas une créance patient active")
	}
	return &invoice, nil
}

func (s *Service) SetDueDate(id uint, r DueDateRequest, user uint) (*Detail, error) {
	date, e := parseOptionalDate(r.DueDate)
	if e != nil {
		return nil, e
	}
	e = s.db.Transaction(func(tx *gorm.DB) error {
		inv, err := activeReceivableInvoice(tx, id)
		if err != nil {
			return err
		}
		var m Metadata
		e := tx.Where("invoice_id=?", id).First(&m).Error
		if errors.Is(e, gorm.ErrRecordNotFound) {
			m = Metadata{InvoiceID: id, PatientID: inv.PatientID, DueDate: date, UpdatedBy: user}
			return tx.Create(&m).Error
		}
		if e != nil {
			return e
		}
		m.DueDate = date
		m.UpdatedBy = user
		return tx.Save(&m).Error
	})
	if e != nil {
		return nil, e
	}
	return s.Detail(id)
}

var followTypes = map[string]bool{"NOTE": true, "REMINDER": true, "PHONE_CALL": true, "PAYMENT_PROMISE": true, "OTHER": true}

func (s *Service) AddFollowUp(id uint, r FollowUpRequest, user uint) (*FollowUp, error) {
	typ := strings.ToUpper(strings.TrimSpace(r.ActionType))
	if !followTypes[typ] || strings.TrimSpace(r.Note) == "" {
		return nil, coreerrors.BadRequest("Relance invalide")
	}
	date, e := parseOptionalDate(r.PromisedPaymentDate)
	if e != nil {
		return nil, e
	}
	if r.PromisedAmount != nil && *r.PromisedAmount < 0 {
		return nil, coreerrors.BadRequest("Montant promis invalide")
	}
	var x FollowUp
	e = s.db.Transaction(func(tx *gorm.DB) error {
		inv, err := activeReceivableInvoice(tx, id)
		if err != nil {
			return err
		}
		x = FollowUp{InvoiceID: id, PatientID: inv.PatientID, ActionType: typ, Note: strings.TrimSpace(r.Note), PromisedPaymentDate: date, PromisedAmount: r.PromisedAmount, CreatedBy: user, CreatedAt: time.Now()}
		return tx.Create(&x).Error
	})
	if e != nil {
		return nil, e
	}
	return &x, nil
}
