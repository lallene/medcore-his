package insurance_receivables

import (
	"errors"
	"fmt"
	"strings"
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/modules/billing"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Service struct{ db *gorm.DB }

func NewService(db *gorm.DB) *Service { return &Service{db: db} }

func insuranceStatus(balance, paid int64, due *time.Time) string {
	if balance <= 0 {
		return "PAID"
	}
	if due != nil && due.Before(time.Now().Truncate(24*time.Hour)) {
		return "OVERDUE"
	}
	if paid > 0 {
		return "PARTIALLY_PAID"
	}
	return "UNPAID"
}

func (s *Service) projection() *gorm.DB {
	return s.db.Table("billing_invoice_lines l").Select(`l.id invoice_line_id,l.invoice_id,i.number invoice_number,i.patient_id,TRIM(CONCAT(p.prenoms,' ',p.nom)) patient_name,p.code_patient patient_code,a.insurance_company_id,c.name company_name,l.authorization_id,l.authorization_number,l.coverage_resolution,l.act_type,l.description,i.created_at invoice_date,l.gross_amount,l.insurance_amount insurance_due,COALESCE(sa.paid,0) insurance_paid,GREATEST(l.insurance_amount-COALESCE(sa.paid,0),0) insurance_balance,m.due_date,COALESCE(b.batch_number,'') batch_number`).
		Joins("JOIN billing_invoices i ON i.id=l.invoice_id").Joins("JOIN patients p ON p.id=i.patient_id").
		Joins("JOIN insurance_authorizations a ON a.id=l.authorization_id").Joins("JOIN insurance_companies c ON c.id=a.insurance_company_id").
		Joins("LEFT JOIN insurance_receivable_metadata m ON m.invoice_line_id=l.id").
		Joins("LEFT JOIN (SELECT al.invoice_line_id,SUM(al.amount) paid FROM insurance_settlement_allocations al JOIN insurance_settlements s ON s.id=al.settlement_id AND s.status='POSTED' GROUP BY al.invoice_line_id) sa ON sa.invoice_line_id=l.id").
		Joins("LEFT JOIN (SELECT DISTINCT ON (bi.invoice_line_id) bi.invoice_line_id,bi.batch_id FROM insurance_submission_batch_items bi JOIN insurance_submission_batches bx ON bx.id=bi.batch_id AND bx.status<>'CLOSED' ORDER BY bi.invoice_line_id,bi.id DESC) bi ON bi.invoice_line_id=l.id").
		Joins("LEFT JOIN insurance_submission_batches b ON b.id=bi.batch_id").
		Where("l.is_active AND l.insurance_amount>0 AND l.coverage_resolution IN ('DIRECT','COVERED') AND a.status IN ('APPROVED','PARTIALLY_APPROVED') AND NOT i.coverage_pending AND i.status IN ?", []string{billing.InvoiceIssued, billing.InvoicePartiallyPaid, billing.InvoicePaid})
}

func applyFilters(q *gorm.DB, f Filter) *gorm.DB {
	if f.CompanyID > 0 {
		q = q.Where("a.insurance_company_id=?", f.CompanyID)
	}
	if f.PatientID > 0 {
		q = q.Where("i.patient_id=?", f.PatientID)
	}
	if f.BatchID > 0 {
		q = q.Where("bi.batch_id=?", f.BatchID)
	}
	if x := strings.TrimSpace(f.Search); x != "" {
		like := "%" + strings.ToLower(x) + "%"
		q = q.Where("LOWER(CONCAT(c.name,' ',p.nom,' ',p.prenoms,' ',p.code_patient,' ',i.number,' ',l.authorization_number,' ',a.external_reference)) LIKE ?", like)
	}
	if f.DateFrom != "" {
		q = q.Where("i.created_at::date>=?", f.DateFrom)
	}
	if f.DateTo != "" {
		q = q.Where("i.created_at::date<=?", f.DateTo)
	}
	if f.Overdue == "true" || f.Status == "OVERDUE" {
		q = q.Where("m.due_date<CURRENT_DATE AND GREATEST(l.insurance_amount-COALESCE(sa.paid,0),0)>0")
	}
	switch f.Status {
	case "UNPAID":
		q = q.Where("COALESCE(sa.paid,0)=0 AND (m.due_date IS NULL OR m.due_date>=CURRENT_DATE)")
	case "PARTIALLY_PAID":
		q = q.Where("COALESCE(sa.paid,0)>0 AND GREATEST(l.insurance_amount-COALESCE(sa.paid,0),0)>0 AND (m.due_date IS NULL OR m.due_date>=CURRENT_DATE)")
	case "PAID":
		q = q.Where("GREATEST(l.insurance_amount-COALESCE(sa.paid,0),0)=0")
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
	q := applyFilters(s.projection(), f)
	var total int64
	if e := q.Session(&gorm.Session{}).Count(&total).Error; e != nil {
		return nil, e
	}
	var rows []Item
	if e := q.Order("COALESCE(m.due_date,'9999-12-31'),i.created_at,l.id").Offset((f.Page - 1) * f.Limit).Limit(f.Limit).Scan(&rows).Error; e != nil {
		return nil, e
	}
	for i := range rows {
		rows[i].Status = insuranceStatus(rows[i].InsuranceBalance, rows[i].InsurancePaid, rows[i].DueDate)
	}
	return &Page{Items: rows, Page: f.Page, Limit: f.Limit, Total: total, TotalPages: int((total + int64(f.Limit) - 1) / int64(f.Limit))}, nil
}

func (s *Service) Receivable(lineID uint) (*Item, error) {
	var row Item
	if e := s.projection().Where("l.id=?", lineID).Scan(&row).Error; e != nil || row.InvoiceLineID == 0 {
		return nil, coreerrors.NotFound("INSURANCE_RECEIVABLE")
	}
	row.Status = insuranceStatus(row.InsuranceBalance, row.InsurancePaid, row.DueDate)
	return &row, nil
}

func (s *Service) ReceivableDetail(lineID uint) (*ReceivableView, error) {
	row, e := s.Receivable(lineID)
	if e != nil {
		return nil, e
	}
	v := &ReceivableView{Item: *row, FollowUps: []FollowUp{}, Allocations: []SettlementAllocation{}}
	if e = s.db.Where("invoice_line_id=?", lineID).Order("followed_up_at DESC,id DESC").Find(&v.FollowUps).Error; e != nil {
		return nil, e
	}
	if e = s.db.Table("insurance_settlement_allocations al").Joins("JOIN insurance_settlements s ON s.id=al.settlement_id AND s.status='POSTED'").Where("al.invoice_line_id=?", lineID).Order("al.id DESC").Scan(&v.Allocations).Error; e != nil {
		return nil, e
	}
	return v, nil
}

func (s *Service) AddFollowUp(lineID uint, r FollowUpRequest, user uint) (*FollowUp, error) {
	typ := strings.ToUpper(strings.TrimSpace(r.Type))
	if typ == "" {
		typ = "NOTE"
	}
	if strings.TrimSpace(r.Note) == "" {
		return nil, coreerrors.BadRequest("Note de relance obligatoire")
	}
	if _, e := s.Receivable(lineID); e != nil {
		return nil, e
	}
	x := &FollowUp{InvoiceLineID: lineID, Type: typ, Note: strings.TrimSpace(r.Note), FollowedUpAt: time.Now(), CreatedBy: user}
	if e := s.db.Create(x).Error; e != nil {
		return nil, e
	}
	return x, nil
}

func (s *Service) KPIs() (*KPIs, error) {
	var rows []Item
	if e := s.projection().Scan(&rows).Error; e != nil {
		return nil, e
	}
	k := &KPIs{}
	companies := map[uint]bool{}
	invoices := map[uint]bool{}
	for _, r := range rows {
		if r.InsuranceBalance > 0 {
			companies[r.InsuranceCompanyID] = true
			invoices[r.InvoiceID] = true
		}
		k.TotalReceivables += r.InsuranceBalance
		k.SettledAmount += r.InsurancePaid
		if insuranceStatus(r.InsuranceBalance, r.InsurancePaid, r.DueDate) == "OVERDUE" {
			k.OverdueAmount += r.InsuranceBalance
		}
	}
	k.DebtorCompanies = int64(len(companies))
	k.PendingInvoices = int64(len(invoices))
	if e := s.db.Table("insurance_settlements s").Select("COALESCE(SUM(GREATEST(s.total_amount-COALESCE(a.allocated,0),0)),0)").Joins("LEFT JOIN (SELECT settlement_id,SUM(amount) allocated FROM insurance_settlement_allocations GROUP BY settlement_id) a ON a.settlement_id=s.id").Where("s.status='POSTED'").Scan(&k.UnallocatedAmount).Error; e != nil {
		return nil, e
	}
	return k, nil
}

func (s *Service) Companies() ([]CompanySummary, error) {
	var rows []Item
	if e := s.projection().Scan(&rows).Error; e != nil {
		return nil, e
	}
	type agg struct {
		CompanySummary
		inv, pat map[uint]bool
	}
	m := map[uint]*agg{}
	for _, r := range rows {
		x := m[r.InsuranceCompanyID]
		if x == nil {
			x = &agg{CompanySummary: CompanySummary{InsuranceCompanyID: r.InsuranceCompanyID, CompanyName: r.CompanyName}, inv: map[uint]bool{}, pat: map[uint]bool{}}
			m[r.InsuranceCompanyID] = x
		}
		x.Billed += r.InsuranceDue
		x.Paid += r.InsurancePaid
		x.Balance += r.InsuranceBalance
		x.inv[r.InvoiceID] = true
		x.pat[r.PatientID] = true
	}
	var settlements []struct {
		CompanyID uint
		Amount    int64
	}
	s.db.Table("insurance_settlements s").Select("s.insurance_company_id company_id,COALESCE(SUM(GREATEST(s.total_amount-COALESCE(a.allocated,0),0)),0) amount").Joins("LEFT JOIN (SELECT settlement_id,SUM(amount) allocated FROM insurance_settlement_allocations GROUP BY settlement_id) a ON a.settlement_id=s.id").Where("s.status='POSTED'").Group("s.insurance_company_id").Scan(&settlements)
	for _, u := range settlements {
		if m[u.CompanyID] != nil {
			m[u.CompanyID].Unallocated = u.Amount
		}
	}
	out := make([]CompanySummary, 0, len(m))
	for _, x := range m {
		x.Invoices = int64(len(x.inv))
		x.Patients = int64(len(x.pat))
		out = append(out, x.CompanySummary)
	}
	return out, nil
}

func parseDate(value string) (time.Time, error) {
	t, e := time.Parse("2006-01-02", strings.TrimSpace(value))
	if e != nil {
		return t, coreerrors.BadRequest("Date invalide")
	}
	return t, nil
}

var settlementMethods = map[string]bool{"BANK_TRANSFER": true, "CHECK": true, "OTHER": true}

func (s *Service) CreateSettlement(r SettlementRequest, user uint) (*SettlementView, error) {
	method := strings.ToUpper(strings.TrimSpace(r.PaymentMethod))
	received, e := parseDate(r.ReceivedAt)
	if e != nil {
		return nil, e
	}
	if r.InsuranceCompanyID == 0 || r.TotalAmount <= 0 || !settlementMethods[method] || strings.TrimSpace(r.ExternalReference) == "" || strings.TrimSpace(r.IdempotencyKey) == "" {
		return nil, coreerrors.BadRequest("Règlement invalide")
	}
	var id uint
	e = s.db.Transaction(func(tx *gorm.DB) error {
		var prior Settlement
		if x := tx.Where("idempotency_key=?", r.IdempotencyKey).First(&prior).Error; x == nil {
			if prior.InsuranceCompanyID != r.InsuranceCompanyID || prior.TotalAmount != r.TotalAmount || prior.ExternalReference != strings.TrimSpace(r.ExternalReference) {
				return coreerrors.Conflict("Clé d'idempotence déjà utilisée")
			}
			id = prior.ID
			return nil
		}
		var company int64
		if x := tx.Table("insurance_companies").Where("id=?", r.InsuranceCompanyID).Count(&company).Error; x != nil {
			return x
		}
		if company == 0 {
			return coreerrors.NotFound("INSURANCE_COMPANY")
		}
		x := Settlement{SettlementNumber: fmt.Sprintf("TMP-%d", time.Now().UnixNano()), InsuranceCompanyID: r.InsuranceCompanyID, GuarantorID: r.GuarantorID, CoverageReference: strings.TrimSpace(r.CoverageReference), ExternalReference: strings.TrimSpace(r.ExternalReference), ReceivedAt: received, TotalAmount: r.TotalAmount, PaymentMethod: method, BankReference: strings.TrimSpace(r.BankReference), Comment: strings.TrimSpace(r.Comment), Status: SettlementDraft, IdempotencyKey: strings.TrimSpace(r.IdempotencyKey), CreatedBy: user}
		if x := tx.Create(&x).Error; x != nil {
			return x
		}
		x.SettlementNumber = fmt.Sprintf("INS-SET-%06d", x.ID)
		if x := tx.Model(&x).Update("settlement_number", x.SettlementNumber).Error; x != nil {
			return x
		}
		id = x.ID
		return nil
	})
	if e != nil {
		if strings.Contains(e.Error(), "ux_ins_settlement_external") {
			return nil, coreerrors.Conflict("Référence assureur déjà enregistrée")
		}
		return nil, e
	}
	return s.Settlement(id)
}

func (s *Service) Settlement(id uint) (*SettlementView, error) {
	var x Settlement
	if e := s.db.First(&x, id).Error; e != nil {
		return nil, coreerrors.NotFound("INSURANCE_SETTLEMENT")
	}
	v := &SettlementView{Settlement: x, Allocations: []SettlementAllocation{}}
	s.db.Table("insurance_companies").Select("name").Where("id=?", x.InsuranceCompanyID).Scan(&v.CompanyName)
	s.db.Where("settlement_id=?", id).Order("id").Find(&v.Allocations)
	for _, a := range v.Allocations {
		v.AllocatedAmount += a.Amount
	}
	v.UnallocatedAmount = x.TotalAmount - v.AllocatedAmount
	return v, nil
}

func (s *Service) ListSettlements(company uint) ([]SettlementView, error) {
	var xs []Settlement
	q := s.db.Order("received_at DESC,id DESC")
	if company > 0 {
		q = q.Where("insurance_company_id=?", company)
	}
	if e := q.Find(&xs).Error; e != nil {
		return nil, e
	}
	out := make([]SettlementView, 0, len(xs))
	for _, x := range xs {
		v, e := s.Settlement(x.ID)
		if e != nil {
			return nil, e
		}
		out = append(out, *v)
	}
	return out, nil
}

func (s *Service) Allocate(id uint, r AllocationRequest, user uint) (*SettlementView, error) {
	if r.InvoiceLineID == 0 || r.Amount <= 0 {
		return nil, coreerrors.BadRequest("Allocation invalide")
	}
	e := s.db.Transaction(func(tx *gorm.DB) error {
		var settlement Settlement
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&settlement, id).Error; e != nil {
			return coreerrors.NotFound("INSURANCE_SETTLEMENT")
		}
		if settlement.Status != SettlementDraft {
			return coreerrors.Conflict("Seul un règlement brouillon peut être rapproché")
		}
		var line billing.InvoiceLine
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&line, r.InvoiceLineID).Error; e != nil {
			return coreerrors.NotFound("INVOICE_LINE")
		}
		if line.InsuranceAmount <= 0 || line.AuthorizationID == nil {
			return coreerrors.Conflict("Cette ligne ne porte aucune créance assureur")
		}
		receivable, e := NewService(tx).Receivable(line.ID)
		if e != nil || receivable.InsuranceBalance <= 0 {
			return coreerrors.Conflict("Cette ligne ne porte aucune créance assureur ouverte")
		}
		var company uint
		if e := tx.Table("insurance_authorizations").Select("insurance_company_id").Where("id=?", *line.AuthorizationID).Scan(&company).Error; e != nil || company != settlement.InsuranceCompanyID {
			return coreerrors.Conflict("La créance appartient à un autre assureur")
		}
		var allocatedSettlement int64
		tx.Model(&SettlementAllocation{}).Where("settlement_id=?", id).Select("COALESCE(SUM(amount),0)").Scan(&allocatedSettlement)
		if allocatedSettlement+r.Amount > settlement.TotalAmount {
			return coreerrors.Conflict("Allocation supérieure au montant disponible du règlement")
		}
		var allocatedLine int64
		tx.Table("insurance_settlement_allocations al").Select("COALESCE(SUM(al.amount),0)").Joins("JOIN insurance_settlements s ON s.id=al.settlement_id AND s.status<>'CANCELLED'").Where("al.invoice_line_id=?", line.ID).Scan(&allocatedLine)
		if allocatedLine+r.Amount > line.InsuranceAmount {
			return coreerrors.Conflict("Allocation supérieure au solde assureur de la ligne")
		}
		a := SettlementAllocation{SettlementID: id, InvoiceID: line.InvoiceID, InvoiceLineID: line.ID, InsuranceAuthorizationID: line.AuthorizationID, Amount: r.Amount, CreatedBy: user}
		if e := tx.Create(&a).Error; e != nil {
			if strings.Contains(e.Error(), "ux_ins_settlement_line") {
				return coreerrors.Conflict("Cette ligne est déjà allouée sur ce règlement")
			}
			return e
		}
		return nil
	})
	if e != nil {
		return nil, e
	}
	return s.Settlement(id)
}

func (s *Service) Post(id, user uint) (*SettlementView, error) {
	e := s.db.Transaction(func(tx *gorm.DB) error {
		var x Settlement
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&x, id).Error; e != nil {
			return coreerrors.NotFound("INSURANCE_SETTLEMENT")
		}
		if x.Status != SettlementDraft {
			return coreerrors.Conflict("Seul un règlement brouillon peut être comptabilisé")
		}
		var allocated int64
		if e := tx.Model(&SettlementAllocation{}).Where("settlement_id=?", id).Select("COALESCE(SUM(amount),0)").Scan(&allocated).Error; e != nil {
			return e
		}
		if allocated > x.TotalAmount {
			return coreerrors.Conflict("Règlement sur-alloué")
		}
		now := time.Now()
		x.Status = SettlementPosted
		x.PostedBy = &user
		x.PostedAt = &now
		return tx.Save(&x).Error
	})
	if e != nil {
		return nil, e
	}
	return s.Settlement(id)
}
func (s *Service) Cancel(id, user uint) (*SettlementView, error) {
	_ = user
	e := s.db.Transaction(func(tx *gorm.DB) error {
		var x Settlement
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&x, id).Error; e != nil {
			return coreerrors.NotFound("INSURANCE_SETTLEMENT")
		}
		if x.Status != SettlementDraft {
			return coreerrors.Conflict("Un règlement comptabilisé nécessite un avoir")
		}
		x.Status = SettlementCancelled
		return tx.Save(&x).Error
	})
	if e != nil {
		return nil, e
	}
	return s.Settlement(id)
}

func optionalDate(v *string) (*time.Time, error) {
	if v == nil || strings.TrimSpace(*v) == "" {
		return nil, nil
	}
	t, e := parseDate(*v)
	return &t, e
}
func (s *Service) SetDue(lineID uint, r DueDateRequest, user uint) (*Item, error) {
	d, e := optionalDate(r.DueDate)
	if e != nil {
		return nil, e
	}
	e = s.db.Transaction(func(tx *gorm.DB) error {
		var line billing.InvoiceLine
		if e := tx.First(&line, lineID).Error; e != nil || line.InsuranceAmount <= 0 {
			return coreerrors.NotFound("INSURANCE_RECEIVABLE")
		}
		var m ReceivableMetadata
		e := tx.Where("invoice_line_id=?", lineID).First(&m).Error
		if errors.Is(e, gorm.ErrRecordNotFound) {
			return tx.Create(&ReceivableMetadata{InvoiceLineID: lineID, DueDate: d, Note: strings.TrimSpace(r.Note), UpdatedBy: user}).Error
		}
		if e != nil {
			return e
		}
		m.DueDate = d
		m.Note = strings.TrimSpace(r.Note)
		m.UpdatedBy = user
		return tx.Save(&m).Error
	})
	if e != nil {
		return nil, e
	}
	var row Item
	if e = s.projection().Where("l.id=?", lineID).Scan(&row).Error; e != nil || row.InvoiceLineID == 0 {
		return nil, coreerrors.NotFound("INSURANCE_RECEIVABLE")
	}
	row.Status = insuranceStatus(row.InsuranceBalance, row.InsurancePaid, row.DueDate)
	return &row, nil
}

func (s *Service) CreateBatch(r BatchRequest, user uint) (*BatchView, error) {
	if r.InsuranceCompanyID == 0 || len(r.InvoiceLineIDs) == 0 {
		return nil, coreerrors.BadRequest("Bordereau invalide")
	}
	from, e := optionalDate(r.PeriodFrom)
	if e != nil {
		return nil, e
	}
	to, e := optionalDate(r.PeriodTo)
	if e != nil {
		return nil, e
	}
	var id uint
	e = s.db.Transaction(func(tx *gorm.DB) error {
		x := SubmissionBatch{BatchNumber: fmt.Sprintf("TMP-%d", time.Now().UnixNano()), InsuranceCompanyID: r.InsuranceCompanyID, PeriodFrom: from, PeriodTo: to, ExternalReference: strings.TrimSpace(r.ExternalReference), Comment: strings.TrimSpace(r.Comment), Status: BatchDraft, CreatedBy: user}
		if e := tx.Create(&x).Error; e != nil {
			return e
		}
		x.BatchNumber = fmt.Sprintf("BORD-%06d", x.ID)
		if e := tx.Model(&x).Update("batch_number", x.BatchNumber).Error; e != nil {
			return e
		}
		for _, lineID := range r.InvoiceLineIDs {
			var line billing.InvoiceLine
			if e := tx.First(&line, lineID).Error; e != nil || line.InsuranceAmount <= 0 || line.AuthorizationID == nil {
				return coreerrors.Conflict("Ligne de bordereau invalide")
			}
			receivable, findErr := NewService(tx).Receivable(line.ID)
			if findErr != nil || receivable.InsuranceBalance <= 0 {
				return coreerrors.Conflict("Ligne de bordereau sans créance assureur ouverte")
			}
			if receivable.InsuranceCompanyID != r.InsuranceCompanyID {
				return coreerrors.Conflict("Les lignes doivent appartenir au même assureur")
			}
			var duplicate int64
			tx.Table("insurance_submission_batch_items bi").Joins("JOIN insurance_submission_batches b ON b.id=bi.batch_id AND b.status IN ('DRAFT','SUBMITTED','ACKNOWLEDGED')").Where("bi.invoice_line_id=?", lineID).Count(&duplicate)
			if duplicate > 0 {
				return coreerrors.Conflict("Ligne déjà présente dans un bordereau actif")
			}
			if e := tx.Create(&SubmissionBatchItem{BatchID: x.ID, InvoiceID: line.InvoiceID, InvoiceLineID: line.ID, Amount: receivable.InsuranceDue, CreatedAt: time.Now()}).Error; e != nil {
				return e
			}
		}
		id = x.ID
		return nil
	})
	if e != nil {
		return nil, e
	}
	return s.Batch(id)
}
func (s *Service) Batch(id uint) (*BatchView, error) {
	var x SubmissionBatch
	if e := s.db.First(&x, id).Error; e != nil {
		return nil, coreerrors.NotFound("INSURANCE_BATCH")
	}
	v := &BatchView{SubmissionBatch: x, Items: []SubmissionBatchItem{}}
	s.db.Table("insurance_companies").Select("name").Where("id=?", x.InsuranceCompanyID).Scan(&v.CompanyName)
	s.db.Where("batch_id=?", id).Find(&v.Items)
	v.InvoiceCount = int64(len(v.Items))
	for _, i := range v.Items {
		v.TotalAmount += i.Amount
	}
	return v, nil
}
func (s *Service) ListBatches(company uint) ([]BatchView, error) {
	var xs []SubmissionBatch
	q := s.db.Order("created_at DESC,id DESC")
	if company > 0 {
		q = q.Where("insurance_company_id=?", company)
	}
	if e := q.Find(&xs).Error; e != nil {
		return nil, e
	}
	out := make([]BatchView, 0, len(xs))
	for _, x := range xs {
		v, e := s.Batch(x.ID)
		if e != nil {
			return nil, e
		}
		out = append(out, *v)
	}
	return out, nil
}
func (s *Service) SubmitBatch(id, user uint) (*BatchView, error) {
	e := s.db.Transaction(func(tx *gorm.DB) error {
		var x SubmissionBatch
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&x, id).Error; e != nil {
			return coreerrors.NotFound("INSURANCE_BATCH")
		}
		if x.Status != BatchDraft {
			return coreerrors.Conflict("Seul un bordereau brouillon peut être soumis")
		}
		now := time.Now()
		x.Status = BatchSubmitted
		x.SubmittedAt = &now
		x.SubmittedBy = &user
		return tx.Save(&x).Error
	})
	if e != nil {
		return nil, e
	}
	return s.Batch(id)
}
