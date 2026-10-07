package billing

import (
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
)

func TestLOT29F_HB_PayerAuthority(t *testing.T) {
	t.Run("H_P01_billing_patient_payer", func(t *testing.T) {
		db := billingDB(t)
		s := NewService(db).WithReceiptIssuer(localSessionlessReceiptIssuer)
		_, p, c, tariffID := seedBilling(t, db)
		_ = db.Model(&patients.Patient{}).Where("id=?", p).Updates(map[string]any{
			"telephone": "+2250700000001", "nom": "Payeur", "prenoms": "Patient",
		})
		inv := issuedPayReady(t, s, p, c, tariffID)
		out, e := s.Pay(inv.ID, PaymentRequest{
			Amount: inv.BalanceAmount, PaymentMethod: "CASH", IdempotencyKey: "hb-p01",
			Payer: PatientPayerRequest(),
		}, c)
		if e != nil {
			t.Fatal(e)
		}
		pay := out.Payments[len(out.Payments)-1]
		if !pay.PayerIsPatient || pay.PayerProvenance != PayerProvenanceCaptured || pay.PayerPartyID == nil {
			t.Fatalf("H-P01 %+v", pay)
		}
		if pay.PayerKind != PartyKindIndividual || pay.PayerDisplayName == "" {
			t.Fatalf("H-P01 name/kind %+v", pay)
		}
		if pay.ReceivedBy != c {
			t.Fatalf("H-P07 ReceivedBy=%d want %d", pay.ReceivedBy, c)
		}
	})

	t.Run("H_P03_third_party_individual", func(t *testing.T) {
		db := billingDB(t)
		s := NewService(db).WithReceiptIssuer(localSessionlessReceiptIssuer)
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		out, e := s.Pay(inv.ID, PaymentRequest{
			Amount: inv.BalanceAmount, PaymentMethod: "CASH", IdempotencyKey: "hb-p03",
			Payer: &PayerRequest{
				Mode: PayerModeIndividual, DisplayName: "Parent Test",
				Phone: "+2250700112233", Relationship: "Parent",
			},
		}, c)
		if e != nil {
			t.Fatal(e)
		}
		pay := out.Payments[len(out.Payments)-1]
		if pay.PayerIsPatient || pay.PayerRelationship != "Parent" || pay.PayerPhone == "" {
			t.Fatalf("H-P03/H-P06 %+v", pay)
		}
		var party FinancialParty
		if e := db.First(&party, *pay.PayerPartyID).Error; e != nil || party.PatientID != nil {
			t.Fatalf("H-P06 party patient link %+v %v", party, e)
		}
	})

	t.Run("H_P04_organization_payer", func(t *testing.T) {
		db := billingDB(t)
		s := NewService(db).WithReceiptIssuer(localSessionlessReceiptIssuer)
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		out, e := s.Pay(inv.ID, PaymentRequest{
			Amount: inv.BalanceAmount, PaymentMethod: "BANK_TRANSFER", Reference: "ORG-1", IdempotencyKey: "hb-p04",
			Payer: &PayerRequest{Mode: PayerModeOrganization, DisplayName: "NGO Alpha", Phone: "+2250700445566"},
		}, c)
		if e != nil {
			t.Fatal(e)
		}
		pay := out.Payments[len(out.Payments)-1]
		if pay.PayerKind != PartyKindOrganization || pay.PayerIsPatient {
			t.Fatalf("H-P04 %+v", pay)
		}
	})

	t.Run("H_P05_snapshot_immutable_after_party_update", func(t *testing.T) {
		db := billingDB(t)
		s := NewService(db).WithReceiptIssuer(localSessionlessReceiptIssuer)
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		out, e := s.Pay(inv.ID, PaymentRequest{
			Amount: inv.BalanceAmount, PaymentMethod: "CASH", IdempotencyKey: "hb-p05",
			Payer: &PayerRequest{Mode: PayerModeIndividual, DisplayName: "Snap Name", Phone: "+2250700778899", Relationship: "Conjoint"},
		}, c)
		if e != nil {
			t.Fatal(e)
		}
		pay := out.Payments[len(out.Payments)-1]
		origName := pay.PayerDisplayName
		if e := db.Model(&FinancialParty{}).Where("id=?", *pay.PayerPartyID).Update("display_name", "CHANGED MASTER").Error; e != nil {
			t.Fatal(e)
		}
		again, e := s.GetInvoice(inv.ID)
		if e != nil {
			t.Fatal(e)
		}
		var snap *Payment
		for i := range again.Payments {
			if again.Payments[i].ID == pay.ID {
				snap = &again.Payments[i]
			}
		}
		if snap == nil || snap.PayerDisplayName != origName {
			t.Fatalf("H-P05 snapshot mutated: %+v want %q", snap, origName)
		}
	})

	t.Run("H_P08_H_P09_validation", func(t *testing.T) {
		db := billingDB(t)
		s := NewService(db).WithReceiptIssuer(localSessionlessReceiptIssuer)
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		if _, e := s.Pay(inv.ID, PaymentRequest{Amount: inv.BalanceAmount, PaymentMethod: "CASH", IdempotencyKey: "hb-p09"}, c); e == nil {
			t.Fatal("H-P09 expected missing payer reject")
		}
		if _, e := s.Pay(inv.ID, PaymentRequest{
			Amount: inv.BalanceAmount, PaymentMethod: "CASH", IdempotencyKey: "hb-p08",
			Payer: &PayerRequest{Mode: PayerModeIndividual, DisplayName: "X", Phone: "12", Relationship: "Ami"},
		}, c); e == nil {
			t.Fatal("H-P08 expected invalid phone reject")
		}
	})

	t.Run("H_P10_H_P11_legacy_readable_and_reversible", func(t *testing.T) {
		db := billingDB(t)
		s := NewService(db).WithReceiptIssuer(localSessionlessReceiptIssuer)
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		amt := inv.BalanceAmount
		legacy := Payment{
			InvoiceID: inv.ID, Amount: amt, PaymentMethod: "CASH",
			IdempotencyKey: "hb-legacy-10", PaidAt: time.Now(), ReceivedBy: c,
			PayerProvenance: PayerProvenanceLegacyUnconfirmed,
		}
		if e := db.Create(&legacy).Error; e != nil {
			t.Fatal(e)
		}
		var invRow Invoice
		if e := db.First(&invRow, inv.ID).Error; e != nil {
			t.Fatal(e)
		}
		invRow.PaidAmount = amt
		invRow.BalanceAmount = 0
		invRow.Status = InvoicePaid
		if e := db.Save(&invRow).Error; e != nil {
			t.Fatal(e)
		}
		got, e := s.GetInvoice(inv.ID)
		if e != nil {
			t.Fatal(e)
		}
		var legacyPay *Payment
		for i := range got.Payments {
			if got.Payments[i].IdempotencyKey == "hb-legacy-10" {
				legacyPay = &got.Payments[i]
			}
		}
		if legacyPay == nil || legacyPay.PayerProvenance != PayerProvenanceLegacyUnconfirmed {
			t.Fatalf("H-P10 %+v", legacyPay)
		}
		if _, e := s.ReversePayment(legacyPay.ID, ReversePaymentRequest{Reason: "Legacy reverse ok", IdempotencyKey: "hb-p11"}, c); e != nil {
			t.Fatalf("H-P11 %v", e)
		}
	})

	t.Run("H_P13_H_P14_idempotency_includes_payer", func(t *testing.T) {
		db := billingDB(t)
		s := NewService(db).WithReceiptIssuer(localSessionlessReceiptIssuer)
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		req := PaymentRequest{
			Amount: inv.BalanceAmount, PaymentMethod: "CASH", IdempotencyKey: "hb-p13",
			Payer: PatientPayerRequest(),
		}
		a, e := s.Pay(inv.ID, req, c)
		if e != nil {
			t.Fatal(e)
		}
		b, e := s.Pay(inv.ID, req, c)
		if e != nil || a.Payments[0].ID != b.Payments[0].ID {
			t.Fatalf("H-P13 replay %v", e)
		}
		if _, e := s.Pay(inv.ID, PaymentRequest{
			Amount: req.Amount, PaymentMethod: "CASH", IdempotencyKey: "hb-p13",
			Payer: &PayerRequest{Mode: PayerModeOrganization, DisplayName: "Other Org", Phone: "+2250700998877"},
		}, c); e == nil || !isConflict(e) {
			t.Fatalf("H-P14 expected conflict got %v", e)
		}
	})

	t.Run("H_P15_payer_resolve_failure_no_payment", func(t *testing.T) {
		db := billingDB(t)
		s := NewService(db).WithReceiptIssuer(localSessionlessReceiptIssuer)
		_, p, c, tariffID := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, c, tariffID)
		before, _ := EffectivePaidOnInvoice(db, inv.ID)
		badID := uint(999999)
		_, e := s.Pay(inv.ID, PaymentRequest{
			Amount: inv.BalanceAmount, PaymentMethod: "CASH", IdempotencyKey: "hb-p15",
			Payer: &PayerRequest{Mode: PayerModeIndividual, PartyID: &badID, DisplayName: "Ghost", Phone: "+2250700123456", Relationship: "Ami"},
		}, c)
		if e == nil {
			t.Fatal("H-P15 expected failure")
		}
		after, _ := EffectivePaidOnInvoice(db, inv.ID)
		if after != before {
			t.Fatalf("H-P15 side effect paid %d→%d", before, after)
		}
		var n int64
		db.Model(&Payment{}).Where("idempotency_key=?", "hb-p15").Count(&n)
		if n != 0 {
			t.Fatal("H-P15 payment row leaked")
		}
	})

	t.Run("H_P18_H_P20_rbac_payer_phone", func(t *testing.T) {
		clinical := rbac.EffectiveStaffPermissions("staff", []string{"DIRECTEUR_MEDICAL"}, nil)
		if rbac.HasAnyPermission(clinical, "billing.payer.read") {
			t.Fatal("H-P19 clinical must not have billing.payer.read")
		}
		cai := rbac.EffectiveStaffPermissions("staff", []string{"CAISSIER"}, nil)
		comp := rbac.EffectiveStaffPermissions("staff", []string{"COMPTABLE"}, nil)
		dir := rbac.EffectiveStaffPermissions("staff", []string{"DIRECTEUR_ADMINISTRATIF"}, nil)
		if !rbac.HasAnyPermission(cai, "billing.payer.read", "billing.payer.write") {
			t.Fatal("H-P20 CAISSIER payer perms")
		}
		if !rbac.HasAnyPermission(comp, "billing.payer.read") || !rbac.HasAnyPermission(dir, "billing.payer.read") {
			t.Fatal("H-P20 finance payer read")
		}
		inv := &Invoice{Payments: []Payment{{PayerPhone: "+2250700112233", PayerDisplayName: "X"}}}
		RedactPayerContacts(inv)
		if inv.Payments[0].PayerPhone != "" || inv.Payments[0].PayerDisplayName != "X" {
			t.Fatalf("H-P18 redact phone only %+v", inv.Payments[0])
		}
	})
}
