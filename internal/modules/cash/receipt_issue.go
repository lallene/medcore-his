package cash

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/billing"
	"gorm.io/gorm"
)

// ReceiptIssueInput is the server-side snapshot for a canonical cash_receipts row.
type ReceiptIssueInput struct {
	PaymentID          uint
	InvoiceID          uint
	PatientID          uint
	CashSessionID      *uint
	Amount             int64
	PaymentMethod      string
	ExternalReference  string
	MobileOperator     string
	IssuedBy           uint
	InvoiceNumber      string
	PatientName        string
	PatientCode        string
	CashierName        string
	RegisterCode       string
	RegisterName       string
	InvoiceGrossAmount int64
	InsuranceAmount    int64
	PatientAmount      int64
	PaidBefore         int64
	BalanceAfter       int64
}

// IssueReceiptInTx creates or recovers the canonical receipt for a payment (1:1 on payment_id).
func IssueReceiptInTx(tx *gorm.DB, in ReceiptIssueInput) (*Receipt, error) {
	var prior Receipt
	if e := tx.Where("payment_id=?", in.PaymentID).First(&prior).Error; e == nil {
		return &prior, nil
	} else if !errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, e
	}
	rec := Receipt{
		ReceiptNumber:      fmt.Sprintf("TMP-%d", time.Now().UnixNano()),
		PaymentID:          in.PaymentID,
		InvoiceID:          in.InvoiceID,
		PatientID:          in.PatientID,
		CashSessionID:      in.CashSessionID,
		Amount:             in.Amount,
		PaymentMethod:      in.PaymentMethod,
		ExternalReference:  in.ExternalReference,
		MobileOperator:     in.MobileOperator,
		IssuedBy:           in.IssuedBy,
		IssuedAt:           time.Now(),
		InvoiceNumber:      in.InvoiceNumber,
		PatientName:        in.PatientName,
		PatientCode:        in.PatientCode,
		CashierName:        in.CashierName,
		RegisterCode:       in.RegisterCode,
		RegisterName:       in.RegisterName,
		InvoiceGrossAmount: in.InvoiceGrossAmount,
		InsuranceAmount:    in.InsuranceAmount,
		PatientAmount:      in.PatientAmount,
		PaidBefore:         in.PaidBefore,
		BalanceAfter:       in.BalanceAfter,
	}
	if e := tx.Create(&rec).Error; e != nil {
		if isCashReceiptPaymentUniqueViolation(e) {
			var raced Receipt
			if load := tx.Where("payment_id=?", in.PaymentID).First(&raced).Error; load == nil {
				return &raced, nil
			}
		}
		return nil, e
	}
	rec.ReceiptNumber = fmt.Sprintf("REC-%06d", rec.ID)
	if e := tx.Model(&rec).Update("receipt_number", rec.ReceiptNumber).Error; e != nil {
		return nil, e
	}
	return &rec, nil
}

// IssueBillingReceipt is the billing-path adapter: sessionless canonical receipt from persisted payment.
func IssueBillingReceipt(tx *gorm.DB, payment *billing.Payment, invoice *billing.Invoice, paidBefore, balanceAfter int64, user uint) error {
	if payment == nil || invoice == nil {
		return fmt.Errorf("receipt issue requires payment and invoice")
	}
	var patient struct{ Nom, Prenoms, CodePatient string }
	if e := tx.Table("patients").Select("nom,prenoms,code_patient").Where("id=?", invoice.PatientID).Scan(&patient).Error; e != nil {
		return e
	}
	var cashier struct{ Name string }
	if e := tx.Table("users").Select("name").Where("id=?", user).Scan(&cashier).Error; e != nil {
		return e
	}
	name := strings.TrimSpace(cashier.Name)
	if name == "" {
		name = fmt.Sprintf("Utilisateur #%d", user)
	}
	_, err := IssueReceiptInTx(tx, ReceiptIssueInput{
		PaymentID:          payment.ID,
		InvoiceID:          invoice.ID,
		PatientID:          invoice.PatientID,
		CashSessionID:      nil,
		Amount:             payment.Amount,
		PaymentMethod:      payment.PaymentMethod,
		ExternalReference:  payment.Reference,
		MobileOperator:     payment.MobileOperator,
		IssuedBy:           user,
		InvoiceNumber:      invoice.Number,
		PatientName:        strings.TrimSpace(patient.Prenoms + " " + patient.Nom),
		PatientCode:        patient.CodePatient,
		CashierName:        name,
		RegisterCode:       "",
		RegisterName:       "",
		InvoiceGrossAmount: invoice.GrossAmount,
		InsuranceAmount:    invoice.InsuranceAmount,
		PatientAmount:      invoice.PatientAmount,
		PaidBefore:         paidBefore,
		BalanceAfter:       balanceAfter,
	})
	return err
}

// EnsureReceiptSessionNullable allows sessionless billing receipts (LOT29D-B).
// Idempotent on PostgreSQL; safe when AutoMigrate already created a nullable column.
func EnsureReceiptSessionNullable(db *gorm.DB) error {
	return db.Exec(`ALTER TABLE cash_receipts ALTER COLUMN cash_session_id DROP NOT NULL`).Error
}

func receiptSessionID(p *uint) uint {
	if p == nil {
		return 0
	}
	return *p
}
