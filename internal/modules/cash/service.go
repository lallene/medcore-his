package cash

import (
	"errors"
	"fmt"
	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/modules/billing"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strings"
	"time"
)

type Service struct {
	db      *gorm.DB
	billing *billing.Service
}

func NewService(db *gorm.DB) *Service { return &Service{db: db, billing: billing.NewService(db)} }
func (s *Service) Registers() ([]Register, error) {
	var x []Register
	e := s.db.Order("code").Find(&x).Error
	return x, e
}
func (s *Service) SaveRegister(id uint, r RegisterRequest, u uint) (*Register, error) {
	active := true
	if r.Active != nil {
		active = *r.Active
	}
	x := Register{ID: id, Code: strings.ToUpper(strings.TrimSpace(r.Code)), Name: strings.TrimSpace(r.Name), Location: strings.TrimSpace(r.Location), Active: active, CreatedBy: u, UpdatedBy: u}
	if id == 0 {
		if e := s.db.Create(&x).Error; e != nil {
			return nil, coreerrors.Conflict("Code caisse déjà utilisé")
		}
	} else {
		var old Register
		if e := s.db.First(&old, id).Error; e != nil {
			return nil, coreerrors.NotFound("CASH_REGISTER")
		}
		old.Name = x.Name
		old.Location = x.Location
		old.Active = x.Active
		old.UpdatedBy = u
		if e := s.db.Save(&old).Error; e != nil {
			return nil, e
		}
		x = old
	}
	return &x, nil
}
func (s *Service) Open(r OpenRequest, u uint) (*SessionSummary, error) {
	if r.OpeningFloat < 0 {
		return nil, coreerrors.BadRequest("Fond initial négatif")
	}
	var id uint
	e := s.db.Transaction(func(tx *gorm.DB) error {
		var reg Register
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&reg, r.CashRegisterID).Error; e != nil {
			return coreerrors.NotFound("CASH_REGISTER")
		}
		if !reg.Active {
			return coreerrors.Conflict("Caisse inactive")
		}
		x := Session{CashRegisterID: reg.ID, OpenedBy: u, OpenedAt: time.Now(), OpeningFloat: r.OpeningFloat, OpeningNote: strings.TrimSpace(r.Note), Status: SessionOpen}
		if e := tx.Create(&x).Error; e != nil {
			return coreerrors.Conflict("Cette caisse possède déjà une session ouverte")
		}
		id = x.ID
		return nil
	})
	if e != nil {
		return nil, e
	}
	return s.Get(id)
}
func (s *Service) Current(user uint) (*SessionSummary, error) {
	var x Session
	e := s.db.Where("status=? AND opened_by=?", SessionOpen, user).Order("opened_at DESC").First(&x).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	return s.Get(x.ID)
}
func (s *Service) Sessions() ([]Session, error) {
	var x []Session
	e := s.db.Preload("Register").Order("opened_at DESC").Find(&x).Error
	return x, e
}
func (s *Service) Get(id uint) (*SessionSummary, error) {
	var x Session
	if e := s.db.Preload("Register").First(&x, id).Error; e != nil {
		return nil, coreerrors.NotFound("CASH_SESSION")
	}
	z := SessionSummary{Session: x}
	rows := []struct {
		Method string
		Total  int64
		Count  int64
	}{}
	s.db.Table("billing_payments").Select("payment_method method,COALESCE(SUM(amount),0) total,COUNT(*) count").Where("cash_session_id=?", id).Group("payment_method").Scan(&rows)
	for _, r := range rows {
		z.TotalPayments += r.Total
		z.OperationCount += r.Count
		switch r.Method {
		case "CASH":
			z.CashPayments = r.Total
		case "CARD":
			z.CardPayments = r.Total
		case "MOBILE_MONEY":
			z.MobileMoneyPayments = r.Total
		case "BANK_TRANSFER":
			z.BankTransferPayments = r.Total
		case "CHECK":
			z.CheckPayments = r.Total
		}
	}
	z.ExpectedCash = x.OpeningFloat + z.CashPayments
	return &z, nil
}
func (s *Service) Pay(sessionID uint, r PaymentRequest, u uint) (*Receipt, error) {
	method := strings.ToUpper(strings.TrimSpace(r.PaymentMethod))
	if (method == "BANK_TRANSFER" || method == "CHECK") && strings.TrimSpace(r.ExternalReference) == "" {
		return nil, coreerrors.BadRequest("Référence obligatoire")
	}
	if method == "MOBILE_MONEY" && strings.TrimSpace(r.MobileOperator) == "" {
		return nil, coreerrors.BadRequest("Opérateur Mobile Money obligatoire")
	}
	var receiptID uint
	e := s.db.Transaction(func(tx *gorm.DB) error {
		var prior Receipt
		if e := tx.Joins("JOIN billing_payments p ON p.id=cash_receipts.payment_id").Where("p.idempotency_key=?", r.IdempotencyKey).First(&prior).Error; e == nil {
			if prior.InvoiceID != r.InvoiceID || prior.CashSessionID != sessionID || prior.Amount != r.Amount || prior.PaymentMethod != method {
				return coreerrors.Conflict("Clé d'idempotence déjà utilisée")
			}
			receiptID = prior.ID
			return nil
		}
		var session Session
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Preload("Register").First(&session, sessionID).Error; e != nil {
			return coreerrors.NotFound("CASH_SESSION")
		}
		if session.Status != SessionOpen {
			return coreerrors.Conflict("Session de caisse fermée")
		}
		// Recheck after the session lock: concurrent retries may both miss the fast path above.
		if e := tx.Joins("JOIN billing_payments p ON p.id=cash_receipts.payment_id").Where("p.idempotency_key=?", r.IdempotencyKey).First(&prior).Error; e == nil {
			if prior.InvoiceID != r.InvoiceID || prior.CashSessionID != sessionID || prior.Amount != r.Amount || prior.PaymentMethod != method {
				return coreerrors.Conflict("Clé d'idempotence déjà utilisée")
			}
			receiptID = prior.ID
			return nil
		}
		var inv billing.Invoice
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&inv, r.InvoiceID).Error; e != nil {
			return coreerrors.NotFound("INVOICE")
		}
		before := inv.PaidAmount
		p, e := s.billing.PayInTransaction(tx, r.InvoiceID, billing.PaymentRequest{Amount: r.Amount, PaymentMethod: method, Reference: r.ExternalReference, IdempotencyKey: r.IdempotencyKey, MobileOperator: r.MobileOperator}, u, &sessionID)
		if e != nil {
			return e
		}
		var patient struct{ Nom, Prenoms, CodePatient string }
		tx.Table("patients").Select("nom,prenoms,code_patient").Where("id=?", inv.PatientID).Scan(&patient)
		var cashier struct{ Name string }
		tx.Table("users").Select("name").Where("id=?", u).Scan(&cashier)
		rec := Receipt{ReceiptNumber: fmt.Sprintf("TMP-%d", time.Now().UnixNano()), PaymentID: p.ID, InvoiceID: inv.ID, PatientID: inv.PatientID, CashSessionID: sessionID, Amount: p.Amount, PaymentMethod: p.PaymentMethod, ExternalReference: p.Reference, MobileOperator: p.MobileOperator, IssuedBy: u, IssuedAt: time.Now(), InvoiceNumber: inv.Number, PatientName: strings.TrimSpace(patient.Prenoms + " " + patient.Nom), PatientCode: patient.CodePatient, CashierName: cashier.Name, RegisterCode: session.Register.Code, RegisterName: session.Register.Name, InvoiceGrossAmount: inv.GrossAmount, InsuranceAmount: inv.InsuranceAmount, PatientAmount: inv.PatientAmount, PaidBefore: before, BalanceAfter: inv.BalanceAmount - r.Amount}
		if e := tx.Create(&rec).Error; e != nil {
			return e
		}
		rec.ReceiptNumber = fmt.Sprintf("REC-%06d", rec.ID)
		if e := tx.Model(&rec).Update("receipt_number", rec.ReceiptNumber).Error; e != nil {
			return e
		}
		receiptID = rec.ID
		return nil
	})
	if e != nil {
		return nil, e
	}
	return s.Receipt(receiptID)
}
func (s *Service) Close(id uint, r CloseRequest, u uint) (*SessionSummary, error) {
	if r.CountedCashAmount < 0 {
		return nil, coreerrors.BadRequest("Montant compté négatif")
	}
	e := s.db.Transaction(func(tx *gorm.DB) error {
		var x Session
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&x, id).Error; e != nil {
			return coreerrors.NotFound("CASH_SESSION")
		}
		if x.Status != SessionOpen {
			return coreerrors.Conflict("Session déjà fermée")
		}
		var cash int64
		tx.Table("billing_payments").Where("cash_session_id=? AND payment_method='CASH'", id).Select("COALESCE(SUM(amount),0)").Scan(&cash)
		expected := x.OpeningFloat + cash
		diff := r.CountedCashAmount - expected
		if diff != 0 && strings.TrimSpace(r.Note) == "" {
			return coreerrors.BadRequest("Justification obligatoire en cas d'écart")
		}
		now := time.Now()
		x.Status = SessionClosed
		x.ClosedBy = &u
		x.ClosedAt = &now
		x.ExpectedCashAmount = &expected
		x.CountedCashAmount = &r.CountedCashAmount
		x.CashDifference = &diff
		x.ClosingNote = strings.TrimSpace(r.Note)
		return tx.Save(&x).Error
	})
	if e != nil {
		return nil, e
	}
	return s.Get(id)
}
func (s *Service) Receipt(id uint) (*Receipt, error) {
	var x Receipt
	if e := s.db.First(&x, id).Error; e != nil {
		return nil, coreerrors.NotFound("CASH_RECEIPT")
	}
	return &x, nil
}
func (s *Service) Receipts(session uint) ([]Receipt, error) {
	var x []Receipt
	q := s.db.Order("issued_at DESC")
	if session > 0 {
		q = q.Where("cash_session_id=?", session)
	}
	e := q.Find(&x).Error
	return x, e
}
