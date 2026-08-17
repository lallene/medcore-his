package billing

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/authorization"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/coverage"
	"github.com/lallene/medcore-his/backend/internal/modules/medical_records"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var actTypes = map[string]bool{"CONSULTATION": true, "LABORATORY": true, "IMAGING": true, "HOSPITALIZATION": true, "MEDICATION": true}
var paymentMethods = map[string]bool{"CASH": true, "CARD": true, "MOBILE_MONEY": true, "BANK_TRANSFER": true, "OTHER": true}

type Service struct {
	db             *gorm.DB
	authorizations *authorization.Service
}

func NewService(db *gorm.DB) *Service {
	return &Service{db: db, authorizations: authorization.NewService(db)}
}

func parseDay(raw string, fallback time.Time) (time.Time, error) {
	if strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	v, e := time.Parse("2006-01-02", raw)
	if e != nil {
		return time.Time{}, coreerrors.BadRequest("Date invalide")
	}
	return v, nil
}
func (s *Service) CreateTariff(req TariffRequest, user uint) (*Tariff, error) {
	typ := strings.ToUpper(strings.TrimSpace(req.ActType))
	if !actTypes[typ] {
		return nil, coreerrors.BadRequest("Type d'acte invalide")
	}
	if req.UnitPrice <= 0 {
		return nil, coreerrors.BadRequest("Le tarif doit être positif")
	}
	from, e := parseDay(req.EffectiveFrom, time.Now())
	if e != nil {
		return nil, e
	}
	var to *time.Time
	if req.EffectiveTo != "" {
		v, e := parseDay(req.EffectiveTo, time.Now())
		if e != nil {
			return nil, e
		}
		to = &v
	}
	active := true
	if req.IsActive != nil {
		active = *req.IsActive
	}
	item := Tariff{ActType: typ, ReferenceID: req.ReferenceID, Code: strings.TrimSpace(req.Code), Label: strings.TrimSpace(req.Label), UnitPrice: req.UnitPrice, Currency: "XOF", EffectiveFrom: from, EffectiveTo: to, IsActive: active, CreatedBy: user, UpdatedBy: user}
	if item.Code == "" || item.Label == "" {
		return nil, coreerrors.BadRequest("Code et libellé obligatoires")
	}
	if e := s.db.Create(&item).Error; e != nil {
		return nil, e
	}
	return &item, nil
}
func (s *Service) UpdateTariff(id uint, req TariffRequest, user uint) (*Tariff, error) {
	var item Tariff
	if e := s.db.First(&item, id).Error; e != nil {
		return nil, coreerrors.NotFound("TARIFF")
	}
	if req.UnitPrice <= 0 {
		return nil, coreerrors.BadRequest("Le tarif doit être positif")
	}
	from, e := parseDay(req.EffectiveFrom, item.EffectiveFrom)
	if e != nil {
		return nil, e
	}
	item.Label = strings.TrimSpace(req.Label)
	item.UnitPrice = req.UnitPrice
	item.EffectiveFrom = from
	item.UpdatedBy = user
	if req.IsActive != nil {
		item.IsActive = *req.IsActive
	}
	if e := s.db.Save(&item).Error; e != nil {
		return nil, e
	}
	return &item, nil
}
func (s *Service) ListTariffs() ([]Tariff, error) {
	var x []Tariff
	e := s.db.Order("act_type,label,effective_from DESC").Find(&x).Error
	return x, e
}

type actSnapshot struct {
	key, label, date      string
	quantity              float64
	tariffReferenceID     uint
	coverageReferenceType string
	coverageReferenceID   uint
}

func (s *Service) snapshot(tx *gorm.DB, patient uint, typ string, id uint) (actSnapshot, error) {
	var a actSnapshot
	a.quantity = 1
	a.coverageReferenceType = typ
	a.coverageReferenceID = id
	switch typ {
	case "CONSULTATION":
		var r struct {
			ID        uint
			PatientID uint
			Service   string
			CreatedAt time.Time
		}
		if e := tx.Table("consultations").First(&r, id).Error; e != nil || r.PatientID != patient {
			return a, coreerrors.Conflict("Consultation invalide pour ce patient")
		}
		a.key = fmt.Sprintf("CONSULTATION:%d", id)
		a.label = "Consultation — " + r.Service
		a.date = r.CreatedAt.Format(time.RFC3339)
	case "LABORATORY":
		var r struct {
			ID, PatientID, MedicalExamID uint
			RequestNumber, ExamName      string
			CreatedAt                    time.Time
		}
		e := tx.Table("laboratory_orders o").Select("o.id,o.patient_id,o.medical_exam_id,o.request_number,e.name exam_name,o.created_at").Joins("JOIN medical_exams e ON e.id=o.medical_exam_id").Where("o.id=?", id).Scan(&r).Error
		if e != nil || r.ID == 0 || r.PatientID != patient {
			return a, coreerrors.Conflict("Examen laboratoire invalide")
		}
		a.key = fmt.Sprintf("LABORATORY:%d", id)
		a.tariffReferenceID = r.MedicalExamID
		a.label = r.RequestNumber + " — " + r.ExamName
		a.date = r.CreatedAt.Format(time.RFC3339)
	case "IMAGING":
		var r struct {
			ID, PatientID, MedicalExamID uint
			OrderNumber, ExamName        string
			CreatedAt                    time.Time
		}
		e := tx.Table("imaging_orders o").Select("o.id,o.patient_id,o.medical_exam_id,o.order_number,e.name exam_name,o.created_at").Joins("JOIN medical_exams e ON e.id=o.medical_exam_id").Where("o.id=?", id).Scan(&r).Error
		if e != nil || r.ID == 0 || r.PatientID != patient {
			return a, coreerrors.Conflict("Examen d'imagerie invalide")
		}
		a.key = fmt.Sprintf("IMAGING:%d", id)
		a.tariffReferenceID = r.MedicalExamID
		a.label = r.OrderNumber + " — " + r.ExamName
		a.date = r.CreatedAt.Format(time.RFC3339)
	case "HOSPITALIZATION":
		var r struct {
			ID, PatientID               uint
			AdmissionNumber, Department string
			CreatedAt                   time.Time
		}
		if e := tx.Table("hospitalizations").First(&r, id).Error; e != nil || r.PatientID != patient {
			return a, coreerrors.Conflict("Hospitalisation invalide")
		}
		a.key = fmt.Sprintf("HOSPITALIZATION:%d", id)
		a.label = r.AdmissionNumber + " — " + r.Department
		a.date = r.CreatedAt.Format(time.RFC3339)
	case "MEDICATION":
		var r struct {
			ID             uint
			PatientID      uint
			PresentationID uint
			Quantity       float64
			ReferenceID    *uint
			MedicationName string
			CreatedAt      time.Time
		}
		e := tx.Table("pharmacy_dispensations d").Select("d.id,d.patient_id,d.presentation_id,d.quantity,d.reference_id,m.name medication_name,d.created_at").Joins("JOIN medication_presentations p ON p.id=d.presentation_id").Joins("JOIN medications m ON m.id=p.medication_id").Where("d.id=? AND d.status='COMPLETED'", id).Scan(&r).Error
		if e != nil || r.ID == 0 || r.PatientID != patient || r.ReferenceID == nil {
			return a, coreerrors.Conflict("Dispensation invalide")
		}
		a.key = fmt.Sprintf("MEDICATION_DISPENSATION:%d", id)
		a.tariffReferenceID = r.PresentationID
		a.label = r.MedicationName
		a.quantity = r.Quantity
		a.date = r.CreatedAt.Format(time.RFC3339)
		a.coverageReferenceID = *r.ReferenceID
	default:
		return a, coreerrors.BadRequest("Type d'acte invalide")
	}
	return a, nil
}
func (s *Service) activeCoverage(tx *gorm.DB, patient uint) (*coverage.PatientCoverage, error) {
	var c coverage.PatientCoverage
	e := tx.Where("patient_id=? AND is_active=true AND (valid_from IS NULL OR valid_from<=?) AND (valid_to IS NULL OR valid_to>=?)", patient, time.Now(), time.Now()).Order("valid_from DESC NULLS LAST,id DESC").First(&c).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &c, e
}
func round(v float64) int64 { return int64(math.Round(v)) }
func allocateInsurance(gross int64, rate *float64, remaining int64) int64 {
	amount := gross
	if rate != nil {
		amount = round(float64(gross) * *rate / 100)
	}
	if remaining < 0 {
		remaining = 0
	}
	if amount > remaining {
		amount = remaining
	}
	if amount > gross {
		amount = gross
	}
	return amount
}
func (s *Service) financialCoverage(tx *gorm.DB, patient uint, a actSnapshot, gross int64) (string, *authorization.Response, int64, bool, error) {
	cov, e := s.activeCoverage(tx, patient)
	if e != nil || cov == nil {
		return "NONE", nil, 0, false, e
	}
	match, e := s.authorizations.FindAuthorizationForAct(patient, cov.ID, a.coverageReferenceType, a.coverageReferenceID)
	if e != nil {
		return "", nil, 0, false, e
	}
	if match.Authorization == nil {
		return "NONE", nil, 0, false, nil
	}
	auth := match.Authorization
	if auth.Status == authorization.StatusRejected {
		return match.MatchType, auth, 0, false, nil
	}
	if auth.Status != authorization.StatusApproved && auth.Status != authorization.StatusPartiallyApproved {
		return match.MatchType, auth, 0, true, nil
	}
	var locked authorization.InsuranceAuthorization
	if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, auth.ID).Error; e != nil {
		return "", nil, 0, false, e
	}
	var used int64
	if e := tx.Model(&AuthorizationAllocation{}).Where("authorization_id=?", auth.ID).Select("COALESCE(SUM(amount),0)").Scan(&used).Error; e != nil {
		return "", nil, 0, false, e
	}
	cap := int64(math.MaxInt64)
	if locked.InsuranceAmount != nil {
		cap = round(*locked.InsuranceAmount)
	}
	remaining := cap - used
	if remaining < 0 {
		remaining = 0
	}
	amount := allocateInsurance(gross, locked.ApprovedRate, remaining)
	return match.MatchType, auth, amount, false, nil
}
func (s *Service) CreateInvoice(req CreateInvoiceRequest, user uint) (*Invoice, error) {
	var id uint
	e := s.db.Transaction(func(tx *gorm.DB) error {
		var count int64
		if e := tx.Table("patients").Where("id=?", req.PatientID).Count(&count).Error; e != nil {
			return e
		}
		if count == 0 {
			return coreerrors.NotFound("PATIENT")
		}
		var record struct{ ID uint }
		_ = tx.Table("medical_records").Select("id").Where("patient_id=?", req.PatientID).First(&record).Error
		inv := Invoice{Number: fmt.Sprintf("TMP-%d-%d", user, time.Now().UnixNano()), PatientID: req.PatientID, Status: InvoiceDraft, CreatedBy: user, UpdatedBy: user}
		if record.ID > 0 {
			inv.MedicalRecordID = &record.ID
		}
		if e := tx.Create(&inv).Error; e != nil {
			return e
		}
		inv.Number = fmt.Sprintf("INV-%06d", inv.ID)
		if e := tx.Model(&inv).Update("number", inv.Number).Error; e != nil {
			return e
		}
		for _, input := range req.Lines {
			typ := strings.ToUpper(input.ActType)
			a, e := s.snapshot(tx, req.PatientID, typ, input.ReferenceID)
			if e != nil {
				return e
			}
			var duplicate int64
			if e := tx.Model(&InvoiceLine{}).Where("billable_key=? AND is_active", a.key).Count(&duplicate).Error; e != nil {
				return e
			}
			if duplicate > 0 {
				return coreerrors.Conflict("Cet acte est déjà facturé")
			}
			var tariff Tariff
			if e := tx.Where("id=? AND is_active AND effective_from<=? AND (effective_to IS NULL OR effective_to>=?)", input.TariffID, time.Now(), time.Now()).First(&tariff).Error; e != nil {
				return coreerrors.Conflict("Tarif inactif ou non applicable")
			}
			if tariff.ActType != typ {
				return coreerrors.Conflict("Le tarif ne correspond pas au type d'acte")
			}
			if tariff.ReferenceID != nil && *tariff.ReferenceID != a.tariffReferenceID {
				return coreerrors.Conflict("Le tarif ne correspond pas au référentiel de cet acte")
			}
			gross := round(a.quantity * float64(tariff.UnitPrice))
			resolution, auth, insurance, pending, e := s.financialCoverage(tx, req.PatientID, a, gross)
			if e != nil {
				return e
			}
			line := InvoiceLine{InvoiceID: inv.ID, TariffID: tariff.ID, ActType: typ, ReferenceID: input.ReferenceID, ClinicalReferenceID: a.coverageReferenceID, BillableKey: a.key, Description: a.label, Quantity: a.quantity, UnitPrice: tariff.UnitPrice, GrossAmount: gross, InsuranceAmount: insurance, PatientAmount: gross - insurance, CoverageResolution: resolution, CoveragePending: pending, IsActive: true}
			if auth != nil {
				line.AuthorizationID = &auth.ID
				line.AuthorizationNumber = auth.AuthorizationNumber
				line.CoverageStatus = auth.Status
			}
			if e := tx.Create(&line).Error; e != nil {
				return e
			}
			if auth != nil && insurance > 0 {
				if e := tx.Create(&AuthorizationAllocation{AuthorizationID: auth.ID, InvoiceLineID: line.ID, Amount: insurance}).Error; e != nil {
					return e
				}
			}
			inv.GrossAmount += gross
			inv.InsuranceAmount += insurance
			inv.PatientAmount += gross - insurance
			inv.CoveragePending = inv.CoveragePending || pending
		}
		inv.BalanceAmount = inv.PatientAmount
		if e := tx.Save(&inv).Error; e != nil {
			return e
		}
		id = inv.ID
		return nil
	})
	if e != nil {
		if strings.Contains(e.Error(), "ux_billing_active_billable_key") {
			return nil, coreerrors.Conflict("Cet acte est déjà facturé")
		}
		return nil, e
	}
	return s.GetInvoice(id)
}
func (s *Service) GetInvoice(id uint) (*Invoice, error) {
	var x Invoice
	e := s.db.Preload("Lines").Preload("Payments").First(&x, id).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, coreerrors.NotFound("INVOICE")
	}
	if e == nil {
		s.decorate(&x)
	}
	return &x, e
}
func (s *Service) decorate(x *Invoice) {
	var p struct{ Nom, Prenoms, CodePatient string }
	s.db.Table("patients").Select("nom,prenoms,code_patient").Where("id=?", x.PatientID).Scan(&p)
	x.PatientName = strings.TrimSpace(p.Prenoms + " " + p.Nom)
	x.PatientCode = p.CodePatient
}
func (s *Service) Issue(id, user uint) (*Invoice, error) {
	e := s.db.Transaction(func(tx *gorm.DB) error {
		var x Invoice
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&x, id).Error; e != nil {
			return coreerrors.NotFound("INVOICE")
		}
		if x.Status != InvoiceDraft {
			return coreerrors.Conflict("Seule une facture brouillon peut être émise")
		}
		if x.CoveragePending {
			var lines []InvoiceLine
			if e := tx.Where("invoice_id=? AND coverage_pending", x.ID).Find(&lines).Error; e != nil {
				return e
			}
			for i := range lines {
				a, e := s.snapshot(tx, x.PatientID, lines[i].ActType, lines[i].ReferenceID)
				if e != nil {
					return e
				}
				resolution, auth, insured, pending, e := s.financialCoverage(tx, x.PatientID, a, lines[i].GrossAmount)
				if e != nil {
					return e
				}
				if pending {
					return coreerrors.Conflict("Une décision PEC est encore en attente")
				}
				lines[i].CoveragePending = false
				lines[i].CoverageResolution = resolution
				lines[i].InsuranceAmount = insured
				lines[i].PatientAmount = lines[i].GrossAmount - insured
				if auth != nil {
					lines[i].AuthorizationID = &auth.ID
					lines[i].AuthorizationNumber = auth.AuthorizationNumber
					lines[i].CoverageStatus = auth.Status
				}
				if e := tx.Save(&lines[i]).Error; e != nil {
					return e
				}
				if auth != nil && insured > 0 {
					if e := tx.Create(&AuthorizationAllocation{AuthorizationID: auth.ID, InvoiceLineID: lines[i].ID, Amount: insured}).Error; e != nil {
						return e
					}
				}
			}
			if e := tx.Model(&InvoiceLine{}).Where("invoice_id=?", x.ID).Select("COALESCE(SUM(insurance_amount),0)").Scan(&x.InsuranceAmount).Error; e != nil {
				return e
			}
			x.PatientAmount = x.GrossAmount - x.InsuranceAmount
			x.BalanceAmount = x.PatientAmount - x.PaidAmount
			x.CoveragePending = false
		}
		now := time.Now()
		x.Status = InvoiceIssued
		x.IssuedAt = &now
		x.IssuedBy = &user
		x.UpdatedBy = user
		if e := tx.Save(&x).Error; e != nil {
			return e
		}
		return s.timeline(tx, &x, "invoice_issued", "Facture émise", user)
	})
	if e != nil {
		return nil, e
	}
	return s.GetInvoice(id)
}
func (s *Service) Pay(id uint, req PaymentRequest, user uint) (*Invoice, error) {
	method := strings.ToUpper(req.PaymentMethod)
	if !paymentMethods[method] || req.Amount <= 0 || strings.TrimSpace(req.IdempotencyKey) == "" {
		return nil, coreerrors.BadRequest("Paiement invalide")
	}
	e := s.db.Transaction(func(tx *gorm.DB) error {
		var prior Payment
		if e := tx.Where("idempotency_key=?", req.IdempotencyKey).First(&prior).Error; e == nil {
			if prior.InvoiceID != id || prior.Amount != req.Amount {
				return coreerrors.Conflict("Clé d'idempotence déjà utilisée")
			}
			return nil
		}
		var x Invoice
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&x, id).Error; e != nil {
			return coreerrors.NotFound("INVOICE")
		}
		if x.Status != InvoiceIssued && x.Status != InvoicePartiallyPaid {
			return coreerrors.Conflict("La facture n'accepte pas de paiement")
		}
		if e := tx.Where("idempotency_key=?", req.IdempotencyKey).First(&prior).Error; e == nil {
			if prior.InvoiceID != id || prior.Amount != req.Amount {
				return coreerrors.Conflict("Clé d'idempotence déjà utilisée")
			}
			return nil
		}
		if req.Amount > x.BalanceAmount {
			return coreerrors.Conflict("Le paiement dépasse le reste dû")
		}
		p := Payment{InvoiceID: id, Amount: req.Amount, PaymentMethod: method, Reference: strings.TrimSpace(req.Reference), IdempotencyKey: req.IdempotencyKey, PaidAt: time.Now(), ReceivedBy: user}
		if e := tx.Create(&p).Error; e != nil {
			return e
		}
		x.PaidAmount += req.Amount
		x.BalanceAmount -= req.Amount
		if x.BalanceAmount == 0 {
			x.Status = InvoicePaid
		} else {
			x.Status = InvoicePartiallyPaid
		}
		x.UpdatedBy = user
		if e := tx.Save(&x).Error; e != nil {
			return e
		}
		event := "payment_received"
		title := "Paiement reçu"
		if x.Status == InvoicePaid {
			event = "invoice_paid"
			title = "Facture payée"
		}
		return s.timeline(tx, &x, event, title, user)
	})
	if e != nil {
		return nil, e
	}
	return s.GetInvoice(id)
}
func (s *Service) Cancel(id uint, reason string, user uint) (*Invoice, error) {
	e := s.db.Transaction(func(tx *gorm.DB) error {
		var x Invoice
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&x, id).Error; e != nil {
			return coreerrors.NotFound("INVOICE")
		}
		if x.PaidAmount > 0 {
			return coreerrors.Conflict("Une facture encaissée nécessite un avoir/remboursement")
		}
		if x.Status != InvoiceDraft && x.Status != InvoiceIssued {
			return coreerrors.Conflict("Cette facture ne peut pas être annulée")
		}
		now := time.Now()
		x.Status = InvoiceCancelled
		x.CancelledAt = &now
		x.CancelledBy = &user
		x.CancellationReason = strings.TrimSpace(reason)
		x.UpdatedBy = user
		if e := tx.Save(&x).Error; e != nil {
			return e
		}
		if e := tx.Model(&InvoiceLine{}).Where("invoice_id=?", id).Update("is_active", false).Error; e != nil {
			return e
		}
		if e := tx.Where("invoice_line_id IN (?)", tx.Model(&InvoiceLine{}).Select("id").Where("invoice_id=?", id)).Delete(&AuthorizationAllocation{}).Error; e != nil {
			return e
		}
		return s.timeline(tx, &x, "invoice_cancelled", "Facture annulée", user)
	})
	if e != nil {
		return nil, e
	}
	return s.GetInvoice(id)
}
func (s *Service) timeline(tx *gorm.DB, x *Invoice, event, title string, user uint) error {
	if x.MedicalRecordID == nil {
		return nil
	}
	ref := x.ID
	return tx.Create(&medical_records.MedicalTimelineEvent{MedicalRecordID: *x.MedicalRecordID, PatientID: x.PatientID, EventType: event, Category: "billing", Title: title, Description: x.Number, ReferenceType: "billing_invoice", ReferenceID: &ref, Severity: "info", EventDate: time.Now(), CreatedBy: user}).Error
}
func (s *Service) List(f ListFilter) (*Page, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.Limit < 1 || f.Limit > 100 {
		f.Limit = 20
	}
	q := s.db.Model(&Invoice{})
	if f.PatientID > 0 {
		q = q.Where("patient_id=?", f.PatientID)
	}
	if f.Status != "" {
		q = q.Where("status=?", strings.ToUpper(f.Status))
	}
	if f.Search != "" {
		q = q.Where("number ILIKE ?", "%"+f.Search+"%")
	}
	var total int64
	if e := q.Count(&total).Error; e != nil {
		return nil, e
	}
	var rows []Invoice
	if e := q.Order("created_at DESC").Offset((f.Page - 1) * f.Limit).Limit(f.Limit).Find(&rows).Error; e != nil {
		return nil, e
	}
	for i := range rows {
		s.decorate(&rows[i])
	}
	return &Page{Data: rows, Page: f.Page, Limit: f.Limit, Total: total, TotalPages: int(math.Ceil(float64(total) / float64(f.Limit)))}, nil
}
func (s *Service) KPIs() (KPIs, error) {
	var k KPIs
	s.db.Model(&Invoice{}).Where("status IN ?", []string{InvoiceIssued, InvoicePartiallyPaid}).Count(&k.PendingInvoices)
	s.db.Model(&Invoice{}).Where("status IN ?", []string{InvoiceIssued, InvoicePartiallyPaid}).Select("COALESCE(SUM(balance_amount),0)").Scan(&k.PatientReceivable)
	s.db.Model(&Invoice{}).Where("status=?", InvoicePaid).Count(&k.PaidInvoices)
	e := s.db.Model(&Invoice{}).Where("status<>?", InvoiceCancelled).Select("COALESCE(SUM(insurance_amount),0)").Scan(&k.InsuranceExpected).Error
	return k, e
}

func (s *Service) BillableActs(patient uint) ([]BillableAct, error) {
	type row struct {
		ActType                        string
		ReferenceID, TariffReferenceID uint
		Label                          string
		Date                           time.Time
		Quantity                       float64
	}
	var rows []row
	query := `SELECT 'CONSULTATION' act_type,c.id reference_id,0 tariff_reference_id,('Consultation — '||COALESCE(NULLIF(c.service,''),'générale')) label,c.created_at date,1::numeric quantity FROM consultations c WHERE c.patient_id=? AND c.status<>'cancelled'
	UNION ALL SELECT 'LABORATORY',o.id,o.medical_exam_id,(o.request_number||' — '||e.name),o.created_at,1 FROM laboratory_orders o JOIN medical_exams e ON e.id=o.medical_exam_id WHERE o.patient_id=? AND o.status<>'CANCELLED'
	UNION ALL SELECT 'IMAGING',o.id,o.medical_exam_id,(o.order_number||' — '||e.name),o.created_at,1 FROM imaging_orders o JOIN medical_exams e ON e.id=o.medical_exam_id WHERE o.patient_id=? AND o.status<>'CANCELLED'
	UNION ALL SELECT 'HOSPITALIZATION',h.id,0,(h.admission_number||' — '||COALESCE(NULLIF(h.department,''),'Hospitalisation')),h.created_at,1 FROM hospitalizations h WHERE h.patient_id=? AND h.status<>'CANCELLED'
	UNION ALL SELECT 'MEDICATION',d.id,d.presentation_id,(m.name||CASE WHEN p.dosage='' THEN '' ELSE ' '||p.dosage END),d.created_at,d.quantity FROM pharmacy_dispensations d JOIN medication_presentations p ON p.id=d.presentation_id JOIN medications m ON m.id=p.medication_id WHERE d.patient_id=? AND d.status='COMPLETED' ORDER BY date DESC`
	if e := s.db.Raw(query, patient, patient, patient, patient, patient).Scan(&rows).Error; e != nil {
		return nil, e
	}
	out := make([]BillableAct, 0, len(rows))
	now := time.Now()
	for _, r := range rows {
		key := fmt.Sprintf("%s:%d", r.ActType, r.ReferenceID)
		if r.ActType == "MEDICATION" {
			key = fmt.Sprintf("MEDICATION_DISPENSATION:%d", r.ReferenceID)
		}
		var billed int64
		if e := s.db.Model(&InvoiceLine{}).Where("billable_key=? AND is_active", key).Count(&billed).Error; e != nil {
			return nil, e
		}
		var tariffs []Tariff
		q := s.db.Where("act_type=? AND is_active AND effective_from<=? AND (effective_to IS NULL OR effective_to>=?)", r.ActType, now, now)
		if r.TariffReferenceID > 0 {
			q = q.Where("reference_id=? OR reference_id IS NULL", r.TariffReferenceID).Order("reference_id NULLS LAST,effective_from DESC")
		} else {
			q = q.Where("reference_id IS NULL").Order("effective_from DESC")
		}
		if e := q.Limit(1).Find(&tariffs).Error; e != nil {
			return nil, e
		}
		item := BillableAct{ActType: r.ActType, ReferenceID: r.ReferenceID, BillableKey: key, Label: r.Label, Date: r.Date.Format(time.RFC3339), Quantity: r.Quantity, AlreadyBilled: billed > 0}
		if len(tariffs) > 0 {
			item.Tariff = &tariffs[0]
		}
		snap, e := s.snapshot(s.db, patient, r.ActType, r.ReferenceID)
		if e == nil {
			cov, _ := s.activeCoverage(s.db, patient)
			if cov != nil {
				match, _ := s.authorizations.FindAuthorizationForAct(patient, cov.ID, snap.coverageReferenceType, snap.coverageReferenceID)
				if match != nil {
					item.CoverageResolution = match.MatchType
					if match.Authorization != nil {
						item.AuthorizationNumber = match.Authorization.AuthorizationNumber
					}
				}
			}
		}
		if item.CoverageResolution == "" {
			item.CoverageResolution = "NONE"
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *Service) ActStatus(patient uint, typ string, reference uint) (ActBillingStatus, error) {
	var row struct {
		InvoiceID      uint
		Number, Status string
	}
	e := s.db.Table("billing_invoice_lines l").Select("i.id invoice_id,i.number,i.status").Joins("JOIN billing_invoices i ON i.id=l.invoice_id").Where("i.patient_id=? AND l.act_type=? AND l.clinical_reference_id=? AND l.is_active", patient, strings.ToUpper(typ), reference).Order("i.created_at DESC").Limit(1).Scan(&row).Error
	if e != nil {
		return ActBillingStatus{}, e
	}
	return ActBillingStatus{Billed: row.InvoiceID > 0, InvoiceID: row.InvoiceID, InvoiceNumber: row.Number, InvoiceStatus: row.Status}, nil
}
