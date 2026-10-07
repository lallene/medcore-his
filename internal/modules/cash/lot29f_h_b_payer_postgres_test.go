package cash

import (
	"testing"

	"github.com/lallene/medcore-his/backend/internal/modules/billing"
)

func TestLOT29F_HB_CashPayerAuthority(t *testing.T) {
	t.Run("H_P02_cash_session_patient_payer", func(t *testing.T) {
		db := cashDB(t)
		db.Create(&cashUser{ID: 91, Name: "Caisse HB"})
		db.Create(&cashPatient{ID: 91, Nom: "Pat", Prenoms: "HB", CodePatient: "P-HB02", Telephone: "+2250700000091"})
		s := NewService(db)
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "HB02", Name: "C"}, 91)
		open, e := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 1000, IdempotencyKey: "hb02-o"}, 91)
		if e != nil {
			t.Fatal(e)
		}
		inv := seedCashInvoice(t, db, "INV-HB02", 91, 3000, 91)
		rec, e := s.Pay(open.Session.ID, PaymentRequest{
			InvoiceID: inv.ID, Amount: 3000, PaymentMethod: "CASH", IdempotencyKey: "hb02-p",
			Payer: &PayerRequest{Mode: "PATIENT"},
		}, 91)
		if e != nil {
			t.Fatal(e)
		}
		if !rec.PayerIsPatient || rec.PayerProvenance != billing.PayerProvenanceCaptured || rec.PayerDisplayName == "" {
			t.Fatalf("H-P02 receipt %+v", rec)
		}
		var pay billing.Payment
		if e := db.First(&pay, rec.PaymentID).Error; e != nil {
			t.Fatal(e)
		}
		if !pay.PayerIsPatient || pay.PayerProvenance != billing.PayerProvenanceCaptured || pay.PayerPartyID == nil {
			t.Fatalf("H-P02 payment %+v", pay)
		}
		if pay.ReceivedBy != 91 {
			t.Fatalf("H-P07 cash ReceivedBy=%d", pay.ReceivedBy)
		}
	})

	t.Run("H_P16_H_P17_receipt_payer_display", func(t *testing.T) {
		db := cashDB(t)
		db.Create(&cashUser{ID: 92, Name: "Caisse HB2"})
		db.Create(&cashPatient{ID: 92, Nom: "Pat", Prenoms: "Rec", CodePatient: "P-HB16", Telephone: "+2250700000092"})
		s := NewService(db)
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "HB16", Name: "C"}, 92)
		open, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 500, IdempotencyKey: "hb16-o"}, 92)

		invPat := seedCashInvoice(t, db, "INV-HB17", 92, 2000, 92)
		recPat, e := s.Pay(open.Session.ID, PaymentRequest{
			InvoiceID: invPat.ID, Amount: 2000, PaymentMethod: "CASH", IdempotencyKey: "hb17-p",
			Payer: &PayerRequest{Mode: "PATIENT"},
		}, 92)
		if e != nil {
			t.Fatal(e)
		}
		if !recPat.PayerIsPatient || recPat.PayerProvenance != billing.PayerProvenanceCaptured {
			t.Fatalf("H-P17 %+v", recPat)
		}
		if recPat.PatientName == "" || recPat.PayerDisplayName == "" {
			t.Fatalf("H-P17 patient+payer names %+v", recPat)
		}

		invTP := seedCashInvoice(t, db, "INV-HB16", 92, 1500, 92)
		recTP, e := s.Pay(open.Session.ID, PaymentRequest{
			InvoiceID: invTP.ID, Amount: 1500, PaymentMethod: "CASH", IdempotencyKey: "hb16-p",
			Payer: &PayerRequest{
				Mode: billing.PayerModeIndividual, DisplayName: "Parent HB",
				Phone: "+2250700161616", Relationship: "Parent",
			},
		}, 92)
		if e != nil {
			t.Fatal(e)
		}
		if recTP.PayerIsPatient || recTP.PayerDisplayName != "Parent HB" || recTP.PayerRelationship != "Parent" {
			t.Fatalf("H-P16 third-party receipt %+v", recTP)
		}
		if recTP.PatientCode != "P-HB16" {
			t.Fatalf("H-P16 patient identity preserved %+v", recTP)
		}
	})

	t.Run("H_P12_legacy_payer_pce_unchanged", func(t *testing.T) {
		db := cashDB(t)
		db.Create(&cashUser{ID: 93, Name: "Dir HB"})
		db.Create(&cashPatient{ID: 93, Nom: "Leg", Prenoms: "PCE", CodePatient: "P-HB12", Telephone: "+2250700000093"})
		s := NewService(db)
		bill := billing.NewService(db)
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "HB12", Name: "C"}, 93)
		open, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 8000, IdempotencyKey: "hb12-o"}, 93)
		inv := seedCashInvoice(t, db, "INV-HB12", 93, 4000, 93)
		rec, e := s.Pay(open.Session.ID, PaymentRequest{
			InvoiceID: inv.ID, Amount: 4000, PaymentMethod: "CASH", IdempotencyKey: "hb12-p",
			Payer: &PayerRequest{Mode: "PATIENT"},
		}, 93)
		if e != nil {
			t.Fatal(e)
		}
		// Simulate historical row: payer never captured.
		if e := db.Model(&billing.Payment{}).Where("id=?", rec.PaymentID).Updates(map[string]any{
			"payer_provenance":   billing.PayerProvenanceLegacyUnconfirmed,
			"payer_is_patient":   false,
			"payer_display_name": "",
			"payer_kind":         "",
			"payer_phone":        "",
			"payer_relationship": "",
			"payer_party_id":     nil,
		}).Error; e != nil {
			t.Fatal(e)
		}
		if _, e := s.Close(open.Session.ID, CloseRequest{
			CountedCashAmount: 12000, Note: "hb12", IdempotencyKey: "hb12-c",
		}, 93, false); e != nil {
			t.Fatal(e)
		}
		beforeMov := movementCount(t, db)
		if _, e := bill.ReversePayment(rec.PaymentID, billing.ReversePaymentRequest{
			Reason: "Legacy PCE path", IdempotencyKey: "hb12-r",
		}, 93); e != nil {
			t.Fatalf("H-P12 reverse %v", e)
		}
		if movementCount(t, db) != beforeMov {
			t.Fatal("H-P12 reverse must not create CashMovement")
		}
		var rev billing.PaymentReversal
		if e := db.Where("original_payment_id=?", rec.PaymentID).First(&rev).Error; e != nil {
			t.Fatal(e)
		}
		open2, e := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 5000, IdempotencyKey: "hb12-o2"}, 93)
		if e != nil {
			t.Fatal(e)
		}
		exec, e := s.ExecutePostCloseCorrection(ExecuteCorrectionRequest{
			PaymentReversalID: rev.ID,
			HostSessionID:     &open2.Session.ID,
			Note:              "legacy pce",
			IdempotencyKey:    "hb12-x",
		}, 93)
		if e != nil {
			t.Fatalf("H-P12 PCE %v", e)
		}
		if exec.Amount != 4000 || exec.CashMovementID == nil {
			t.Fatalf("H-P12 PCE result %+v", exec)
		}
		var pay billing.Payment
		if e := db.First(&pay, rec.PaymentID).Error; e != nil {
			t.Fatal(e)
		}
		if pay.PayerProvenance != billing.PayerProvenanceLegacyUnconfirmed {
			t.Fatalf("H-P12 provenance rewritten %+v", pay)
		}
	})
}

func TestLOT29F_HC_PCE_NoCreditEffect(t *testing.T) {
	db := cashDB(t)
	db.Create(&cashUser{ID: 94, Name: "HC14"})
	db.Create(&cashPatient{ID: 94, Nom: "HC", Prenoms: "14", CodePatient: "P-HC14", Telephone: "+2250700000094"})
	s := NewService(db)
	bill := billing.NewService(db)
	reg, _ := s.SaveRegister(0, RegisterRequest{Code: "HC14", Name: "C"}, 94)
	open, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 5000, IdempotencyKey: "hc14-o"}, 94)
	inv := seedCashInvoice(t, db, "INV-HC14", 94, 3000, 94)
	rec, e := s.Pay(open.Session.ID, PaymentRequest{
		InvoiceID: inv.ID, Amount: 3000, PaymentMethod: "CASH", IdempotencyKey: "hc14-p",
		Payer: &PayerRequest{Mode: "PATIENT"},
	}, 94)
	if e != nil {
		t.Fatal(e)
	}
	if _, e := s.Close(open.Session.ID, CloseRequest{CountedCashAmount: 8000, Note: "c", IdempotencyKey: "hc14-c"}, 94, false); e != nil {
		t.Fatal(e)
	}
	if _, e := bill.ReversePayment(rec.PaymentID, billing.ReversePaymentRequest{Reason: "PCE path", IdempotencyKey: "hc14-r"}, 94); e != nil {
		t.Fatal(e)
	}
	var rev billing.PaymentReversal
	if e := db.Where("original_payment_id=?", rec.PaymentID).First(&rev).Error; e != nil {
		t.Fatal(e)
	}
	open2, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 10000, IdempotencyKey: "hc14-o2"}, 94)
	if _, e := s.ExecutePostCloseCorrection(ExecuteCorrectionRequest{
		PaymentReversalID: rev.ID, HostSessionID: &open2.Session.ID, Note: "pce", IdempotencyKey: "hc14-x",
	}, 94); e != nil {
		t.Fatal(e)
	}
	var n int64
	db.Model(&billing.CreditLedgerEntry{}).Count(&n)
	if n != 0 {
		t.Fatalf("H-C14 PCE must not create credit ledger rows n=%d", n)
	}
}
