package billing

import (
	"errors"
	"testing"
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"gorm.io/gorm"
)

func appErrCode(e error) string {
	var app *coreerrors.AppError
	if errors.As(e, &app) {
		return app.Code
	}
	return ""
}

func specificTariff(t *testing.T, db *gorm.DB, typ, code string, ref uint, price int64, active bool) uint {
	t.Helper()
	x := Tariff{ActType: typ, ReferenceID: &ref, Code: code, Label: code, UnitPrice: price, Currency: "XOF", EffectiveFrom: time.Now().Add(-time.Hour), IsActive: true, CreatedBy: 7, UpdatedBy: 7}
	if e := db.Create(&x).Error; e != nil {
		t.Fatal(e)
	}
	if !active {
		if e := db.Model(&x).Update("is_active", false).Error; e != nil {
			t.Fatal(e)
		}
	}
	return x.ID
}

// LOT30-A′ — client tariffId is validated against the server-computed eligible set.
func TestLOT30APrime_TariffAuthority(t *testing.T) {
	db := billingDB(t)
	s := NewService(db)
	p, _ := seedPatient(t, db, "PRICE")
	e1 := billingExam{Name: "NFS"}
	e2 := billingExam{Name: "Glycémie"}
	db.Create(&e1)
	db.Create(&e2)
	order := billingLabOrder{PatientID: p, MedicalExamID: e1.ID, RequestNumber: "LAB-PRICE-1", Status: "VALIDATED", CreatedAt: time.Now()}
	db.Create(&order)
	consTariff := tariff(t, db, "CONSULTATION", "A30-CONS", 20000)
	genericLab := tariff(t, db, "LABORATORY", "A30-LAB-GEN", 2000)
	specE1 := specificTariff(t, db, "LABORATORY", "A30-LAB-E1", e1.ID, 8000, true)
	specE2 := specificTariff(t, db, "LABORATORY", "A30-LAB-E2", e2.ID, 3000, true)
	inactiveE1 := specificTariff(t, db, "LABORATORY", "A30-LAB-E1-OFF", e1.ID, 1000, false)

	create := func(tariffID uint) (*Invoice, error) {
		return s.CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{{ActType: "LABORATORY", ReferenceID: order.ID, TariffID: tariffID}}}, 42)
	}
	for name, tc := range map[string]uint{
		"PRICE01_incompatible_type":            consTariff,
		"PRICE02_specific_of_other_occurrence": specE2,
		"PRICE03_inactive":                     inactiveE1,
		"PRICE05_generic_when_specific_exists": genericLab,
		"PRICE06_unknown_tariff":               999999,
	} {
		if _, e := create(tc); appErrCode(e) != CodeTariffNotEligible {
			t.Fatalf("%s: want %s, got %v", name, CodeTariffNotEligible, e)
		}
	}
	var lines int64
	db.Model(&InvoiceLine{}).Count(&lines)
	if lines != 0 {
		t.Fatalf("rejected tariffs must not create lines: %d", lines)
	}

	t.Run("PRICE04_valid_specific_tariff", func(t *testing.T) {
		inv, e := create(specE1)
		if e != nil {
			t.Fatal(e)
		}
		if inv.GrossAmount != 8000 || len(inv.Lines) != 1 || inv.Lines[0].TariffID != specE1 || inv.Lines[0].UnitPrice != 8000 {
			t.Fatalf("server gross must follow eligible tariff: %#v", inv)
		}
	})

	t.Run("PRICE07_billable_acts_expose_eligible_set", func(t *testing.T) {
		order2 := billingLabOrder{PatientID: p, MedicalExamID: e1.ID, RequestNumber: "LAB-PRICE-2", Status: "VALIDATED", CreatedAt: time.Now()}
		db.Create(&order2)
		acts, e := s.BillableActs(p)
		if e != nil {
			t.Fatal(e)
		}
		for _, a := range acts {
			if a.ActType == "LABORATORY" && a.ReferenceID == order2.ID {
				if a.Tariff == nil || a.Tariff.ID != specE1 || len(a.EligibleTariffs) != 1 || a.EligibleTariffs[0].ID != specE1 {
					t.Fatalf("eligible set must be the dedicated tariff only: %#v", a)
				}
				return
			}
		}
		t.Fatal("lab order not listed")
	})

	t.Run("PRICE08_generic_choice_set_without_dedicated_tariff", func(t *testing.T) {
		second := tariff(t, db, "CONSULTATION", "A30-CONS-2", 15000)
		c := consultation(t, db, p, "Médecine")
		acts, e := s.BillableActs(p)
		if e != nil {
			t.Fatal(e)
		}
		for _, a := range acts {
			if a.ActType == "CONSULTATION" && a.ReferenceID == c {
				if len(a.EligibleTariffs) != 2 {
					t.Fatalf("both generic consultation tariffs are eligible: %#v", a.EligibleTariffs)
				}
			}
		}
		inv, e := s.CreateInvoice(CreateInvoiceRequest{PatientID: p, Lines: []InvoiceLineRequest{{ActType: "CONSULTATION", ReferenceID: c, TariffID: second}}}, 42)
		if e != nil || inv.GrossAmount != 15000 {
			t.Fatalf("choice inside eligible set accepted: %v %#v", e, inv)
		}
	})
}

// LOT30-A′ — payer FinancialParty references are validated and snapshots come from the server.
func TestLOT30APrime_PayerAuthority(t *testing.T) {
	db := billingDB(t)
	s := NewService(db)
	pa, _ := seedPatient(t, db, "PAYA")
	pb, _ := seedPatient(t, db, "PAYB")
	tar := tariff(t, db, "CONSULTATION", "A30-PAY", 10000)
	invA := func() *Invoice { return issuedPayReady(t, s, pa, consultation(t, db, pa, "Médecine"), tar) }
	invB := func() *Invoice { return issuedPayReady(t, s, pb, consultation(t, db, pb, "Médecine"), tar) }
	card := func(inv *Invoice, key string, payer *PayerRequest) (*Invoice, error) {
		return s.Pay(inv.ID, PaymentRequest{Amount: 1000, PaymentMethod: "CARD", IdempotencyKey: key, Payer: payer}, 5)
	}
	ptr := func(v uint) *uint { return &v }

	t.Run("PAYER01_unknown_party_rejected", func(t *testing.T) {
		inv := invA()
		_, e := card(inv, "a30-p01", &PayerRequest{Mode: PayerModeIndividual, PartyID: ptr(999999), Relationship: "Frère"})
		var app *coreerrors.AppError
		if !errors.As(e, &app) || app.Status != 404 {
			t.Fatalf("unknown partyId must be rejected 404, got %v", e)
		}
		if paymentCount(t, db, inv.ID) != 0 {
			t.Fatal("no payment expected")
		}
	})

	t.Run("PAYER02_patient_party_of_other_patient_rejected", func(t *testing.T) {
		if _, e := card(invA(), "a30-p02-a", PatientPayerRequest()); e != nil {
			t.Fatal(e)
		}
		var partyA FinancialParty
		if e := db.Where("patient_id=?", pa).First(&partyA).Error; e != nil {
			t.Fatal(e)
		}
		inv := invB()
		if _, e := card(inv, "a30-p02-b", &PayerRequest{Mode: PayerModePatient, PartyID: ptr(partyA.ID)}); appErrCode(e) != CodePayerPartyPatientMismatch {
			t.Fatalf("want %s got %v", CodePayerPartyPatientMismatch, e)
		}
		// Patient party of an existing patient cannot be used as a third party either.
		if _, e := card(inv, "a30-p02-c", &PayerRequest{Mode: PayerModeIndividual, PartyID: ptr(partyA.ID), Relationship: "Ami"}); e == nil {
			t.Fatal("patient party reused as INDIVIDUAL must be rejected")
		}
		if paymentCount(t, db, inv.ID) != 0 {
			t.Fatal("no payment expected")
		}
	})

	t.Run("PAYER03_existing_party_snapshot_is_server_authority", func(t *testing.T) {
		first, e := card(invA(), "a30-p03-new", &PayerRequest{Mode: PayerModeIndividual, DisplayName: "Jean Payeur", Phone: "0700000001", Relationship: "Père"})
		if e != nil {
			t.Fatal(e)
		}
		partyID := *first.Payments[0].PayerPartyID
		// Reuse by id only: name / phone come from the master.
		second, e := card(invA(), "a30-p03-reuse", &PayerRequest{Mode: PayerModeIndividual, PartyID: ptr(partyID), Relationship: "Père"})
		if e != nil {
			t.Fatal(e)
		}
		got := second.Payments[0]
		if *got.PayerPartyID != partyID || got.PayerDisplayName != "Jean Payeur" || got.PayerPhone != "0700000001" {
			t.Fatalf("snapshot must come from party master: %#v", got)
		}
		// Conflicting client identity is rejected, never rewritten into the party or the payment.
		inv := invA()
		if _, e := card(inv, "a30-p03-mismatch", &PayerRequest{Mode: PayerModeIndividual, PartyID: ptr(partyID), DisplayName: "Autre Nom", Phone: "0700000001", Relationship: "Père"}); appErrCode(e) != CodePayerPartyMismatch {
			t.Fatalf("want %s got %v", CodePayerPartyMismatch, e)
		}
		if _, e := card(inv, "a30-p03-phone", &PayerRequest{Mode: PayerModeIndividual, PartyID: ptr(partyID), Phone: "0799999999", Relationship: "Père"}); appErrCode(e) != CodePayerPartyMismatch {
			t.Fatalf("phone mismatch want %s got %v", CodePayerPartyMismatch, e)
		}
		var party FinancialParty
		db.First(&party, partyID)
		if party.DisplayName != "Jean Payeur" || party.Phone != "0700000001" || party.Kind != PartyKindIndividual || party.PatientID != nil {
			t.Fatalf("party master mutated: %#v", party)
		}
		// An INDIVIDUAL party unrelated to the invoice patient cannot be borrowed.
		if _, e := card(invB(), "a30-p03-unrelated", &PayerRequest{Mode: PayerModeIndividual, PartyID: ptr(partyID), Relationship: "Voisin"}); appErrCode(e) != CodePayerPartyRelationRequired {
			t.Fatalf("want %s got %v", CodePayerPartyRelationRequired, e)
		}
		// Wrong kind is rejected.
		if _, e := card(invA(), "a30-p03-kind", &PayerRequest{Mode: PayerModeOrganization, PartyID: ptr(partyID)}); e == nil {
			t.Fatal("INDIVIDUAL party used as ORGANIZATION must be rejected")
		}
	})

	t.Run("PAYER03b_organization_shared_across_patients", func(t *testing.T) {
		first, e := card(invA(), "a30-p03b-org", &PayerRequest{Mode: PayerModeOrganization, DisplayName: "Employeur SA", Phone: "0711111111"})
		if e != nil {
			t.Fatal(e)
		}
		orgID := *first.Payments[0].PayerPartyID
		out, e := card(invB(), "a30-p03b-org-b", &PayerRequest{Mode: PayerModeOrganization, PartyID: ptr(orgID)})
		if e != nil {
			t.Fatal(e)
		}
		if out.Payments[0].PayerDisplayName != "Employeur SA" || out.Payments[0].PayerKind != PartyKindOrganization {
			t.Fatalf("org snapshot: %#v", out.Payments[0])
		}
	})

	t.Run("PAYER04_payment_snapshot_immutable_after_party_update", func(t *testing.T) {
		paid, e := card(invA(), "a30-p04-a", &PayerRequest{Mode: PayerModeIndividual, DisplayName: "Awa Tiers", Phone: "0722222222", Relationship: "Tante"})
		if e != nil {
			t.Fatal(e)
		}
		pay := paid.Payments[0]
		if e := db.Model(&FinancialParty{}).Where("id=?", *pay.PayerPartyID).Updates(map[string]any{"display_name": "Awa Renommée", "phone": "0733333333"}).Error; e != nil {
			t.Fatal(e)
		}
		var stored Payment
		db.First(&stored, pay.ID)
		if stored.PayerDisplayName != "Awa Tiers" || stored.PayerPhone != "0722222222" || stored.PayerFingerprint != pay.PayerFingerprint {
			t.Fatalf("historical payer snapshot rewritten: %#v", stored)
		}
		// Patient master refresh on a new payment never rewrites older snapshots either.
		before, e := card(invA(), "a30-p04-pat1", PatientPayerRequest())
		if e != nil {
			t.Fatal(e)
		}
		old := before.Payments[0]
		db.Table("patients").Where("id=?", pa).Update("nom", "Renommé")
		if _, e := card(invA(), "a30-p04-pat2", PatientPayerRequest()); e != nil {
			t.Fatal(e)
		}
		var again Payment
		db.First(&again, old.ID)
		if again.PayerDisplayName != old.PayerDisplayName {
			t.Fatalf("patient payer snapshot rewritten: %q -> %q", old.PayerDisplayName, again.PayerDisplayName)
		}
	})
}
