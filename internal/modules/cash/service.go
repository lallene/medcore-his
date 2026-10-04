package cash

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
		// GORM default:true skips bool false on insert — force inactive when requested.
		if !active {
			if e := s.db.Model(&x).Update("active", false).Error; e != nil {
				return nil, e
			}
			x.Active = false
		}
	} else {
		var old Register
		if e := s.db.First(&old, id).Error; e != nil {
			return nil, coreerrors.NotFound("CASH_REGISTER")
		}
		// Policy A: reject deactivation while an OPEN session exists.
		if old.Active && !active {
			var n int64
			if e := s.db.Model(&Session{}).Where("cash_register_id=? AND status=?", old.ID, SessionOpen).Count(&n).Error; e != nil {
				return nil, e
			}
			if n > 0 {
				return nil, coreerrors.Conflict("Impossible de désactiver une caisse avec une session ouverte")
			}
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
	key, err := NormalizeSessionCommandKey(r.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	note := strings.TrimSpace(r.Note)
	var id uint
	e := s.db.Transaction(func(tx *gorm.DB) error {
		var prior Session
		if e := tx.Where("open_idempotency_key=?", key).First(&prior).Error; e == nil {
			if e := openFingerprintConflict(prior, r.CashRegisterID, r.OpeningFloat, note); e != nil {
				return e
			}
			id = prior.ID
			return nil
		} else if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}

		var reg Register
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&reg, r.CashRegisterID).Error; e != nil {
			return coreerrors.NotFound("CASH_REGISTER")
		}
		if !reg.Active {
			return coreerrors.Conflict("Caisse inactive")
		}
		x := Session{
			CashRegisterID:     reg.ID,
			OpenedBy:           u,
			OpenedAt:           time.Now(),
			OpeningFloat:       r.OpeningFloat,
			OpeningNote:        note,
			OpenIdempotencyKey: key,
			Status:             SessionOpen,
		}
		if e := tx.Create(&x).Error; e != nil {
			if isSessionIdempotencyUniqueViolation(e) {
				var raced Session
				if load := tx.Where("open_idempotency_key=?", key).First(&raced).Error; load == nil {
					if e := openFingerprintConflict(raced, r.CashRegisterID, r.OpeningFloat, note); e != nil {
						return e
					}
					id = raced.ID
					return nil
				}
				return coreerrors.Conflict("Cette caisse possède déjà une session ouverte")
			}
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

// ListSessions returns historical CashSession rows with deterministic ordering and pagination.
// Does not load journals or payment aggregates (history list is not financial authority).
func (s *Service) ListSessions(f SessionListFilter) (*SessionListPage, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.Limit < 1 || f.Limit > 100 {
		f.Limit = 20
	}
	q := s.db.Model(&Session{})
	if st := strings.TrimSpace(strings.ToUpper(f.Status)); st != "" {
		q = q.Where("status = ?", st)
	}
	if f.CashRegisterID > 0 {
		q = q.Where("cash_register_id = ?", f.CashRegisterID)
	}
	if from := strings.TrimSpace(f.DateFrom); from != "" {
		q = q.Where("opened_at::date >= ?", from)
	}
	if to := strings.TrimSpace(f.DateTo); to != "" {
		q = q.Where("opened_at::date <= ?", to)
	}
	var total int64
	if e := q.Count(&total).Error; e != nil {
		return nil, e
	}
	var items []Session
	e := q.Preload("Register").
		Order("opened_at DESC, id DESC").
		Offset((f.Page - 1) * f.Limit).
		Limit(f.Limit).
		Find(&items).Error
	if e != nil {
		return nil, e
	}
	pages := int((total + int64(f.Limit) - 1) / int64(f.Limit))
	return &SessionListPage{Items: items, Page: f.Page, Limit: f.Limit, Total: total, TotalPages: pages}, nil
}
func (s *Service) Get(id uint) (*SessionSummary, error) {
	var x Session
	if e := s.db.Preload("Register").First(&x, id).Error; e != nil {
		return nil, coreerrors.NotFound("CASH_SESSION")
	}
	totals, e := loadSessionPaymentTotals(s.db, id)
	if e != nil {
		return nil, e
	}
	movements, e := loadSessionMovementTotals(s.db, id)
	if e != nil {
		return nil, e
	}
	z := assembleSessionSummary(x, totals, movements)
	return &z, nil
}

func (s *Service) Pay(sessionID uint, r PaymentRequest, u uint) (*Receipt, error) {
	method := strings.ToUpper(strings.TrimSpace(r.PaymentMethod))
	key, err := billing.NormalizePaymentIdempotencyKey(r.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	r.IdempotencyKey = key
	extRef := strings.TrimSpace(r.ExternalReference)
	mobile := strings.TrimSpace(r.MobileOperator)
	if (method == "BANK_TRANSFER" || method == "CHECK") && extRef == "" {
		return nil, coreerrors.BadRequest("Référence obligatoire")
	}
	if method == "MOBILE_MONEY" && mobile == "" {
		return nil, coreerrors.BadRequest("Opérateur Mobile Money obligatoire")
	}
	var receiptID uint
	e := s.db.Transaction(func(tx *gorm.DB) error {
		var prior Receipt
		if e := tx.Joins("JOIN billing_payments p ON p.id=cash_receipts.payment_id").Where("p.idempotency_key=?", key).First(&prior).Error; e == nil {
			if prior.InvoiceID != r.InvoiceID || receiptSessionID(prior.CashSessionID) != sessionID || prior.Amount != r.Amount || prior.PaymentMethod != method || prior.ExternalReference != extRef || prior.MobileOperator != mobile {
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
		if session.OpenedBy != u {
			return coreerrors.Forbidden("Seule la caissière / le caissier ouvreur peut encaisser sur cette session")
		}
		if !session.Register.Active {
			return coreerrors.Conflict("Caisse inactive")
		}
		// Recheck after the session lock: concurrent retries may both miss the fast path above.
		if e := tx.Joins("JOIN billing_payments p ON p.id=cash_receipts.payment_id").Where("p.idempotency_key=?", key).First(&prior).Error; e == nil {
			if prior.InvoiceID != r.InvoiceID || receiptSessionID(prior.CashSessionID) != sessionID || prior.Amount != r.Amount || prior.PaymentMethod != method || prior.ExternalReference != extRef || prior.MobileOperator != mobile {
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
		p, e := s.billing.PayInTransaction(tx, r.InvoiceID, billing.PaymentRequest{Amount: r.Amount, PaymentMethod: method, Reference: extRef, IdempotencyKey: key, MobileOperator: mobile}, u, &sessionID)
		if e != nil {
			return e
		}
		var patient struct{ Nom, Prenoms, CodePatient string }
		tx.Table("patients").Select("nom,prenoms,code_patient").Where("id=?", inv.PatientID).Scan(&patient)
		var cashier struct{ Name string }
		tx.Table("users").Select("name").Where("id=?", u).Scan(&cashier)
		sid := sessionID
		rec, e := IssueReceiptInTx(tx, ReceiptIssueInput{
			PaymentID: p.ID, InvoiceID: inv.ID, PatientID: inv.PatientID, CashSessionID: &sid,
			Amount: p.Amount, PaymentMethod: p.PaymentMethod, ExternalReference: p.Reference, MobileOperator: p.MobileOperator,
			IssuedBy: u, InvoiceNumber: inv.Number, PatientName: strings.TrimSpace(patient.Prenoms + " " + patient.Nom),
			PatientCode: patient.CodePatient, CashierName: cashier.Name, RegisterCode: session.Register.Code, RegisterName: session.Register.Name,
			InvoiceGrossAmount: inv.GrossAmount, InsuranceAmount: inv.InsuranceAmount, PatientAmount: inv.PatientAmount,
			PaidBefore: before, BalanceAfter: inv.BalanceAmount - r.Amount,
		})
		if e != nil {
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

func isCashReceiptPaymentUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, fragment := range []string{"duplicate key", "unique constraint", "sqlstate 23505", "payment_id"} {
		if strings.Contains(msg, fragment) {
			return true
		}
	}
	return false
}

// Close closes an OPEN session. canCloseAny allows non-opener recovery (requires note).
func (s *Service) Close(id uint, r CloseRequest, u uint, canCloseAny bool) (*SessionSummary, error) {
	if r.CountedCashAmount < 0 {
		return nil, coreerrors.BadRequest("Montant compté négatif")
	}
	key, err := NormalizeSessionCommandKey(r.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	note := strings.TrimSpace(r.Note)
	e := s.db.Transaction(func(tx *gorm.DB) error {
		var priorByKey Session
		if e := tx.Where("close_idempotency_key=?", key).First(&priorByKey).Error; e == nil {
			if e := closeFingerprintConflict(priorByKey, id, r.CountedCashAmount, note); e != nil {
				return e
			}
			return nil
		} else if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}

		var x Session
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&x, id).Error; e != nil {
			return coreerrors.NotFound("CASH_SESSION")
		}
		if x.Status == SessionClosed {
			if x.CloseIdempotencyKey != nil && *x.CloseIdempotencyKey == key {
				if e := closeFingerprintConflict(x, id, r.CountedCashAmount, note); e != nil {
					return e
				}
				return nil
			}
			return coreerrors.Conflict("Session déjà fermée")
		}
		if x.Status != SessionOpen {
			return coreerrors.Conflict("Session déjà fermée")
		}

		isOwner := x.OpenedBy == u
		if !isOwner && !canCloseAny {
			return coreerrors.Forbidden("Seule la caissière / le caissier ouvreur peut clôturer cette session")
		}
		if !isOwner {
			// Recovery: mandatory note even if difference is zero.
			if note == "" {
				return coreerrors.BadRequest("Justification obligatoire pour la clôture de récupération")
			}
		}

		totals, e := loadSessionPaymentTotals(tx, id)
		if e != nil {
			return e
		}
		movements, e := loadSessionMovementTotals(tx, id)
		if e != nil {
			return e
		}
		// Same authority as SessionSummary OPEN expected (no formula drift).
		expected := liveExpectedCash(x.OpeningFloat, totals.Cash, movements.In, movements.Out)
		diff := r.CountedCashAmount - expected
		if diff != 0 && note == "" {
			return coreerrors.BadRequest("Justification obligatoire en cas d'écart")
		}
		now := time.Now()
		x.Status = SessionClosed
		x.ClosedBy = &u
		x.ClosedAt = &now
		x.ExpectedCashAmount = &expected
		x.CountedCashAmount = &r.CountedCashAmount
		x.CashDifference = &diff
		x.ClosingNote = note
		x.CloseIdempotencyKey = &key
		if e := tx.Save(&x).Error; e != nil {
			if isSessionIdempotencyUniqueViolation(e) {
				var raced Session
				if load := tx.Where("close_idempotency_key=?", key).First(&raced).Error; load == nil {
					return closeFingerprintConflict(raced, id, r.CountedCashAmount, note)
				}
			}
			return e
		}
		return nil
	})
	if e != nil {
		return nil, e
	}
	return s.Get(id)
}

func (s *Service) decorateReceiptReversal(x *Receipt) {
	if x == nil {
		return
	}
	var at time.Time
	if e := s.db.Table("billing_payment_reversals").Select("reversed_at").Where("original_payment_id=?", x.PaymentID).Scan(&at).Error; e != nil || at.IsZero() {
		return
	}
	x.PaymentReversed = true
	x.PaymentReversedAt = &at
	if x.CashSessionID == nil {
		return
	}
	var closedAt time.Time
	if e := s.db.Table("cash_sessions").Select("closed_at").Where("id=? AND closed_at IS NOT NULL", *x.CashSessionID).Scan(&closedAt).Error; e != nil || closedAt.IsZero() {
		return
	}
	x.PostCloseCorrection = at.After(closedAt)
}

func (s *Service) Receipt(id uint) (*Receipt, error) {
	var x Receipt
	if e := s.db.First(&x, id).Error; e != nil {
		return nil, coreerrors.NotFound("CASH_RECEIPT")
	}
	s.decorateReceiptReversal(&x)
	return &x, nil
}
func (s *Service) Receipts(session uint) ([]Receipt, error) {
	var x []Receipt
	q := s.db.Order("issued_at DESC")
	if session > 0 {
		q = q.Where("cash_session_id=?", session)
	}
	if e := q.Find(&x).Error; e != nil {
		return nil, e
	}
	for i := range x {
		s.decorateReceiptReversal(&x[i])
	}
	return x, nil
}

// EnsureSessionCommandKeys backfills open idempotency keys for legacy rows before NOT NULL uniqueness.
func EnsureSessionCommandKeys(db *gorm.DB) error {
	if err := db.Exec(`
		UPDATE cash_sessions
		SET open_idempotency_key = CONCAT('legacy-open-', id::text)
		WHERE open_idempotency_key IS NULL OR open_idempotency_key = ''
	`).Error; err != nil {
		return fmt.Errorf("backfill open_idempotency_key: %w", err)
	}
	return nil
}
