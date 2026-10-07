package billing

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
	"gorm.io/gorm"
)

// earnCredit creates paid invoice + CN producing `credit` XOF for the patient payer holder.
func earnCredit(t *testing.T, s *Service, db *gorm.DB, p uint, credit int64, tag string, user uint) (holder uint) {
	t.Helper()
	inv := issuedPayReady(t, s, p, consultation(t, db, p, tag+"-earn"), tariff(t, db, "CONSULTATION", tag+"-earn", 50000))
	paySessionless(t, s, inv.ID, 50000, "CASH", tag+"-earn-pay", user)
	out, e := s.IssueCreditNote(inv.ID, CreditNoteRequest{Amount: credit, Reason: "Crédit source " + tag, IdempotencyKey: tag + "-earn-cn"}, user)
	if e != nil || out.CreditHolderPartyID == nil || out.CustomerCreditAmount != credit {
		t.Fatalf("earnCredit %s %+v %v", tag, out, e)
	}
	return *out.CreditHolderPartyID
}

func TestLOT29F_HD_CreditApplicationMatrix(t *testing.T) {
	t.Run("H_D01_partial_apply", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, _, _ := seedBilling(t, db)
		holder := earnCredit(t, s, db, p, 20000, "hd01", 301)
		target := issuedPayReady(t, s, p, consultation(t, db, p, "HD01t"), tariff(t, db, "CONSULTATION", "HD01t", 50000))
		res, e := s.ApplyCredit(target.ID, CreditApplicationRequest{HolderPartyID: holder, Amount: 10000, IdempotencyKey: "hd01"}, 301)
		if e != nil || res.AmountApplied != 10000 || res.RemainingAvailable != 10000 || res.RemainingReceivable != 40000 {
			t.Fatalf("H-D01 %+v %v", res, e)
		}
		got, _ := s.GetInvoice(target.ID)
		if got.Status != InvoicePartiallyPaid || got.CreditAppliedAmount != 10000 || got.PaidAmount != 0 {
			t.Fatalf("H-D01/24 invoice %+v", got)
		}
	})

	t.Run("H_D02_H_D03_H_D25_full_credit_only_settlement", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, _, _ := seedBilling(t, db)
		holder := earnCredit(t, s, db, p, 30000, "hd02", 302)
		target := issuedPayReady(t, s, p, consultation(t, db, p, "HD02t"), tariff(t, db, "CONSULTATION", "HD02t", 30000))
		res, e := s.ApplyCredit(target.ID, CreditApplicationRequest{HolderPartyID: holder, Amount: 30000, IdempotencyKey: "hd02"}, 302)
		if e != nil || res.RemainingReceivable != 0 || res.InvoiceStatus != InvoicePaid {
			t.Fatalf("H-D02/03 %+v %v", res, e)
		}
		got, _ := s.GetInvoice(target.ID)
		if got.Status != InvoicePaid || got.PaidAmount != 0 || got.CreditAppliedAmount != 30000 || got.BalanceAmount != 0 {
			t.Fatalf("H-D03/25 %+v", got)
		}
		var pays int64
		db.Model(&Payment{}).Where("invoice_id=?", target.ID).Count(&pays)
		if pays != 0 {
			t.Fatal("H-D03 no Payment")
		}
	})

	t.Run("H_D04_H_D05_available_and_receivable", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, _, _ := seedBilling(t, db)
		holder := earnCredit(t, s, db, p, 15000, "hd04", 303)
		target := issuedPayReady(t, s, p, consultation(t, db, p, "HD04t"), tariff(t, db, "CONSULTATION", "HD04t", 40000))
		beforeAvail, _ := AvailableCredit(db, holder, p)
		beforeBal := target.BalanceAmount
		res, e := s.ApplyCredit(target.ID, CreditApplicationRequest{HolderPartyID: holder, Amount: 5000, IdempotencyKey: "hd04"}, 303)
		if e != nil {
			t.Fatal(e)
		}
		afterAvail, _ := AvailableCredit(db, holder, p)
		got, _ := s.GetInvoice(target.ID)
		if beforeAvail-afterAvail != 5000 || beforeBal-got.BalanceAmount != 5000 || res.AmountApplied != 5000 {
			t.Fatalf("H-D04/05 beforeAvail=%d after=%d bal %d->%d", beforeAvail, afterAvail, beforeBal, got.BalanceAmount)
		}
	})

	t.Run("H_D06_H_D08_H_D09_H_D10_no_cash_effects", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, _, _ := seedBilling(t, db)
		holder := earnCredit(t, s, db, p, 10000, "hd06", 304)
		target := issuedPayReady(t, s, p, consultation(t, db, p, "HD06t"), tariff(t, db, "CONSULTATION", "HD06t", 20000))
		var movBefore, rcptBefore int64
		db.Table("cash_movements").Count(&movBefore)
		db.Table("cash_receipts").Count(&rcptBefore)
		if _, e := s.ApplyCredit(target.ID, CreditApplicationRequest{HolderPartyID: holder, Amount: 10000, IdempotencyKey: "hd06"}, 304); e != nil {
			t.Fatal(e)
		}
		var movAfter, rcptAfter int64
		db.Table("cash_movements").Count(&movAfter)
		db.Table("cash_receipts").Count(&rcptAfter)
		if movAfter != movBefore || rcptAfter != rcptBefore {
			t.Fatalf("H-D08/09 movements %d->%d receipts %d->%d", movBefore, movAfter, rcptBefore, rcptAfter)
		}
	})

	t.Run("H_D11_H_D12_H_D13_amount_guards", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, _, _ := seedBilling(t, db)
		holder := earnCredit(t, s, db, p, 10000, "hd11", 305)
		target := issuedPayReady(t, s, p, consultation(t, db, p, "HD11t"), tariff(t, db, "CONSULTATION", "HD11t", 8000))
		if _, e := s.ApplyCredit(target.ID, CreditApplicationRequest{HolderPartyID: holder, Amount: 0, IdempotencyKey: "hd11z"}, 305); !isBadRequest(e) {
			t.Fatalf("H-D11 %v", e)
		}
		if _, e := s.ApplyCredit(target.ID, CreditApplicationRequest{HolderPartyID: holder, Amount: 15000, IdempotencyKey: "hd12"}, 305); creditErrCode(e) != CodeCreditApplicationInsufficient {
			t.Fatalf("H-D12 %v", e)
		}
		if _, e := s.ApplyCredit(target.ID, CreditApplicationRequest{HolderPartyID: holder, Amount: 9000, IdempotencyKey: "hd13"}, 305); creditErrCode(e) != CodeCreditApplicationExceedsBalance {
			t.Fatalf("H-D13 %v", e)
		}
	})

	t.Run("H_D14_wrong_patient_rejected", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p1, _, _ := seedBilling(t, db)
		holder := earnCredit(t, s, db, p1, 10000, "hd14a", 306)
		p2 := patients.Patient{CodePatient: "HD14B", NumeroDossier: "HD14B-D", Nom: "Autre", Prenoms: "Patient"}
		if e := db.Create(&p2).Error; e != nil {
			t.Fatal(e)
		}
		target := issuedPayReady(t, s, p2.ID, consultation(t, db, p2.ID, "HD14t"), tariff(t, db, "CONSULTATION", "HD14t", 20000))
		// Available for holder+p2 is 0 → insufficient (cannot spend p1-scoped credit on p2 invoice via same holder+wrong patient scope)
		if _, e := s.ApplyCredit(target.ID, CreditApplicationRequest{HolderPartyID: holder, Amount: 5000, IdempotencyKey: "hd14"}, 306); creditErrCode(e) != CodeCreditApplicationInsufficient {
			t.Fatalf("H-D14 %v code=%s", e, creditErrCode(e))
		}
		sum, _ := AvailableCredit(db, holder, p1)
		if sum != 10000 {
			t.Fatalf("H-D14 credit untouched %d", sum)
		}
	})

	t.Run("H_D15_cancelled_rejected", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, _, _ := seedBilling(t, db)
		holder := earnCredit(t, s, db, p, 5000, "hd15", 307)
		target := issuedPayReady(t, s, p, consultation(t, db, p, "HD15t"), tariff(t, db, "CONSULTATION", "HD15t", 10000))
		if _, e := s.Cancel(target.ID, "Annulation test", 307); e != nil {
			t.Fatal(e)
		}
		if _, e := s.ApplyCredit(target.ID, CreditApplicationRequest{HolderPartyID: holder, Amount: 1000, IdempotencyKey: "hd15"}, 307); creditErrCode(e) != CodeCreditApplicationInvoiceIneligible {
			t.Fatalf("H-D15 %v", e)
		}
	})

	t.Run("H_D16_H_D17_multi_holder_explicit", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, _, _ := seedBilling(t, db)
		_ = db.Model(&patients.Patient{}).Where("id=?", p).Updates(map[string]any{"telephone": "+2250700001617"})
		// Holder A: patient payer
		holderA := earnCredit(t, s, db, p, 10000, "hd16a", 308)
		// Holder B: third-party payer on another earn invoice
		invB := issuedPayReady(t, s, p, consultation(t, db, p, "HD16b"), tariff(t, db, "CONSULTATION", "HD16b", 50000))
		if _, e := s.Pay(invB.ID, PaymentRequest{
			Amount: 50000, PaymentMethod: "CASH", IdempotencyKey: "hd16b-pay",
			Payer: &PayerRequest{Mode: PayerModeIndividual, DisplayName: "Tuteur HD16", Phone: "+2250700161616", Relationship: "Tuteur"},
		}, 308); e != nil {
			t.Fatal(e)
		}
		outB, e := s.IssueCreditNote(invB.ID, CreditNoteRequest{Amount: 20000, Reason: "Crédit tuteur", IdempotencyKey: "hd16b-cn"}, 308)
		if e != nil || outB.CreditHolderPartyID == nil {
			t.Fatalf("holder B %v", e)
		}
		holderB := *outB.CreditHolderPartyID
		if holderA == holderB {
			t.Fatal("holders must differ")
		}
		target := issuedPayReady(t, s, p, consultation(t, db, p, "HD16t"), tariff(t, db, "CONSULTATION", "HD16t", 25000))
		res, e := s.ApplyCredit(target.ID, CreditApplicationRequest{HolderPartyID: holderA, Amount: 10000, IdempotencyKey: "hd16"}, 308)
		if e != nil || res.HolderPartyID != holderA {
			t.Fatalf("H-D16 %+v %v", res, e)
		}
		availB, _ := AvailableCredit(db, holderB, p)
		if availB != 20000 {
			t.Fatalf("H-D17 holder B untouched %d", availB)
		}
		// receivable left = 15k; ask 20k (available) → exceeds invoice balance (checked after available)
		if _, e := s.ApplyCredit(target.ID, CreditApplicationRequest{HolderPartyID: holderB, Amount: 20000, IdempotencyKey: "hd17-over"}, 308); creditErrCode(e) != CodeCreditApplicationExceedsBalance {
			t.Fatalf("H-D17 over receivable %v code=%s", e, creditErrCode(e))
		}
		res2, e := s.ApplyCredit(target.ID, CreditApplicationRequest{HolderPartyID: holderB, Amount: 15000, IdempotencyKey: "hd17"}, 308)
		if e != nil || res2.RemainingReceivable != 0 {
			t.Fatalf("H-D17 apply B %+v %v", res2, e)
		}
		availA, _ := AvailableCredit(db, holderA, p)
		availB2, _ := AvailableCredit(db, holderB, p)
		if availA != 0 || availB2 != 5000 {
			t.Fatalf("H-D17 avails A=%d B=%d", availA, availB2)
		}
	})

	t.Run("H_D18_H_D19_H_D20_idempotency", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, _, _ := seedBilling(t, db)
		holder := earnCredit(t, s, db, p, 20000, "hd18", 309)
		target := issuedPayReady(t, s, p, consultation(t, db, p, "HD18t"), tariff(t, db, "CONSULTATION", "HD18t", 40000))
		a, e := s.ApplyCredit(target.ID, CreditApplicationRequest{HolderPartyID: holder, Amount: 5000, IdempotencyKey: "hd18"}, 309)
		if e != nil {
			t.Fatal(e)
		}
		b, e := s.ApplyCredit(target.ID, CreditApplicationRequest{HolderPartyID: holder, Amount: 5000, IdempotencyKey: "hd18"}, 309)
		if e != nil || a.Application.ID != b.Application.ID {
			t.Fatalf("H-D18 replay %+v %+v %v", a, b, e)
		}
		var n int64
		db.Model(&CreditApplication{}).Count(&n)
		if n != 1 {
			t.Fatalf("H-D18 apps=%d", n)
		}
		if _, e := s.ApplyCredit(target.ID, CreditApplicationRequest{HolderPartyID: holder, Amount: 6000, IdempotencyKey: "hd18"}, 309); creditErrCode(e) != CodeCreditApplicationIdempotencyConflict {
			t.Fatalf("H-D19 %v", e)
		}
		invB := issuedPayReady(t, s, p, consultation(t, db, p, "HD20b"), tariff(t, db, "CONSULTATION", "HD20b", 50000))
		if _, e := s.Pay(invB.ID, PaymentRequest{
			Amount: 50000, PaymentMethod: "CASH", IdempotencyKey: "hd20b-pay",
			Payer: &PayerRequest{Mode: PayerModeIndividual, DisplayName: "Autre HD20", Phone: "+2250700202020", Relationship: "Ami"},
		}, 309); e != nil {
			t.Fatal(e)
		}
		outB, e := s.IssueCreditNote(invB.ID, CreditNoteRequest{Amount: 10000, Reason: "Holder B", IdempotencyKey: "hd20b-cn"}, 309)
		if e != nil || outB.CreditHolderPartyID == nil {
			t.Fatal(e)
		}
		holder2 := *outB.CreditHolderPartyID
		if holder2 == holder {
			t.Fatal("H-D20 holders must differ")
		}
		if _, e := s.ApplyCredit(target.ID, CreditApplicationRequest{HolderPartyID: holder2, Amount: 5000, IdempotencyKey: "hd18"}, 309); creditErrCode(e) != CodeCreditApplicationIdempotencyConflict {
			t.Fatalf("H-D20 %v code=%s", e, creditErrCode(e))
		}
	})

	t.Run("H_D21_ledger_1to1", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, _, _ := seedBilling(t, db)
		holder := earnCredit(t, s, db, p, 8000, "hd21", 310)
		target := issuedPayReady(t, s, p, consultation(t, db, p, "HD21t"), tariff(t, db, "CONSULTATION", "HD21t", 10000))
		res, e := s.ApplyCredit(target.ID, CreditApplicationRequest{HolderPartyID: holder, Amount: 8000, IdempotencyKey: "hd21"}, 310)
		if e != nil {
			t.Fatal(e)
		}
		var entry CreditLedgerEntry
		if e := db.Where("source_type=? AND source_id=?", CreditSourceCreditApplication, res.Application.ID).First(&entry).Error; e != nil {
			t.Fatal(e)
		}
		if entry.EntryType != CreditEntryApply || entry.Amount != 8000 {
			t.Fatalf("H-D21 %+v", entry)
		}
		var n int64
		db.Model(&CreditLedgerEntry{}).Where("entry_type=?", CreditEntryApply).Count(&n)
		if n != 1 {
			t.Fatalf("H-D21 apply rows=%d", n)
		}
	})

	t.Run("H_D26_mixed_payment_and_credit", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, _, _ := seedBilling(t, db)
		holder := earnCredit(t, s, db, p, 30000, "hd26", 311)
		target := issuedPayReady(t, s, p, consultation(t, db, p, "HD26t"), tariff(t, db, "CONSULTATION", "HD26t", 50000))
		paySessionless(t, s, target.ID, 20000, "CASH", "hd26-pay", 311)
		res, e := s.ApplyCredit(target.ID, CreditApplicationRequest{HolderPartyID: holder, Amount: 30000, IdempotencyKey: "hd26"}, 311)
		if e != nil {
			t.Fatal(e)
		}
		got, _ := s.GetInvoice(target.ID)
		if got.Status != InvoicePaid || got.PaidAmount != 20000 || got.CreditAppliedAmount != 30000 || got.BalanceAmount != 0 || res.RemainingReceivable != 0 {
			t.Fatalf("H-D26 %+v res=%+v", got, res)
		}
	})

	t.Run("H_D27_unused_credit_no_effect", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, _, _ := seedBilling(t, db)
		_ = earnCredit(t, s, db, p, 10000, "hd27", 312)
		other := issuedPayReady(t, s, p, consultation(t, db, p, "HD27t"), tariff(t, db, "CONSULTATION", "HD27t", 25000))
		got, _ := s.GetInvoice(other.ID)
		if got.BalanceAmount != 25000 || got.CreditAppliedAmount != 0 {
			t.Fatalf("H-D27 %+v", got)
		}
	})

	t.Run("H_D28_D11_with_applied_credit", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, _, _ := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, consultation(t, db, p, "HD28"), tariff(t, db, "CONSULTATION", "HD28", 40000))
		paid := paySessionless(t, s, inv.ID, 40000, "CASH", "hd28-pay", 313)
		cn, e := s.IssueCreditNote(inv.ID, CreditNoteRequest{Amount: 10000, Reason: "Crédit HD28", IdempotencyKey: "hd28-cn"}, 313)
		if e != nil {
			t.Fatal(e)
		}
		holder := *cn.CreditHolderPartyID
		target := issuedPayReady(t, s, p, consultation(t, db, p, "HD28t"), tariff(t, db, "CONSULTATION", "HD28t", 10000))
		if _, e := s.ApplyCredit(target.ID, CreditApplicationRequest{HolderPartyID: holder, Amount: 10000, IdempotencyKey: "hd28-app"}, 313); e != nil {
			t.Fatal(e)
		}
		if _, e := s.ReversePayment(paid.Payments[0].ID, ReversePaymentRequest{Reason: "Bloqué appliqué", IdempotencyKey: "hd28-rev"}, 313); creditErrCode(e) != CodeCreditReversalBlocked {
			t.Fatalf("H-D28 %v", e)
		}
	})

	t.Run("H_D30_cn_producer_unchanged", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, _, _ := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, consultation(t, db, p, "HD30"), tariff(t, db, "CONSULTATION", "HD30", 30000))
		paySessionless(t, s, inv.ID, 30000, "CASH", "hd30-pay", 314)
		out, e := s.IssueCreditNote(inv.ID, CreditNoteRequest{Amount: 5000, Reason: "Toujours producteur", IdempotencyKey: "hd30"}, 314)
		if e != nil || out.CustomerCreditAmount != 5000 {
			t.Fatalf("H-D30 %+v %v", out, e)
		}
	})

	t.Run("H_D31_history_includes_apply", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, _, _ := seedBilling(t, db)
		holder := earnCredit(t, s, db, p, 7000, "hd31", 315)
		target := issuedPayReady(t, s, p, consultation(t, db, p, "HD31t"), tariff(t, db, "CONSULTATION", "HD31t", 10000))
		if _, e := s.ApplyCredit(target.ID, CreditApplicationRequest{HolderPartyID: holder, Amount: 7000, IdempotencyKey: "hd31"}, 315); e != nil {
			t.Fatal(e)
		}
		rows, e := s.ListCreditLedger(holder, p)
		if e != nil {
			t.Fatal(e)
		}
		var sawApply bool
		for _, r := range rows {
			if r.EntryType == CreditEntryApply && r.Amount == 7000 {
				sawApply = true
			}
		}
		if !sawApply {
			t.Fatalf("H-D31 %+v", rows)
		}
		sum, _ := s.GetCreditSummary(holder, p)
		if sum.TotalApplied != 7000 || sum.AvailableCredit != 0 {
			t.Fatalf("H-D31 summary %+v", sum)
		}
	})

	t.Run("H_D32_H_D33_H_D34_rbac", func(t *testing.T) {
		clinic := rbac.EffectiveStaffPermissions("staff", []string{"INFIRMIER"}, nil)
		if hasPerm(clinic, "billing.credit.apply") {
			t.Fatal("H-D33 clinical must not apply")
		}
		caissier := rbac.EffectiveStaffPermissions("staff", []string{"CAISSIER"}, nil)
		if hasPerm(caissier, "billing.credit.apply") {
			t.Fatal("H-D32 caissier must not apply (cash ≠ credit allocation)")
		}
		for _, role := range []string{"FACTURATION", "COMPTABLE", "DIRECTEUR_ADMINISTRATIF"} {
			perms := rbac.EffectiveStaffPermissions("staff", []string{role}, nil)
			if !hasPerm(perms, "billing.credit.apply") || !hasPerm(perms, "billing.credit.read") {
				t.Fatalf("H-D34 %s missing apply/read", role)
			}
		}
	})

	t.Run("H_D35_legacy_payer_cannot_bypass", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, _, _ := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, consultation(t, db, p, "HD35"), tariff(t, db, "CONSULTATION", "HD35", 20000))
		paid := paySessionless(t, s, inv.ID, 20000, "CASH", "hd35-pay", 316)
		if e := db.Model(&Payment{}).Where("id=?", paid.Payments[0].ID).Updates(map[string]any{
			"payer_provenance": PayerProvenanceLegacyUnconfirmed, "payer_party_id": nil, "payer_is_patient": false,
		}).Error; e != nil {
			t.Fatal(e)
		}
		if _, e := s.IssueCreditNote(inv.ID, CreditNoteRequest{Amount: 5000, Reason: "Legacy HD35", IdempotencyKey: "hd35-cn"}, 316); creditErrCode(e) != CodeCreditLegacyPayerUnresolved {
			t.Fatalf("H-D35 CN %v", e)
		}
		// No credit earned → nothing to apply; inventing a holder cannot create spendable balance
		target := issuedPayReady(t, s, p, consultation(t, db, p, "HD35t"), tariff(t, db, "CONSULTATION", "HD35t", 10000))
		var party FinancialParty
		if e := db.Where("patient_id=?", p).First(&party).Error; e != nil {
			pid := p
			party = FinancialParty{Kind: PartyKindIndividual, DisplayName: "Patient HD35", PatientID: &pid}
			if e := db.Create(&party).Error; e != nil {
				t.Fatal(e)
			}
		}
		if _, e := s.ApplyCredit(target.ID, CreditApplicationRequest{HolderPartyID: party.ID, Amount: 1000, IdempotencyKey: "hd35-app"}, 316); creditErrCode(e) != CodeCreditApplicationInsufficient {
			t.Fatalf("H-D35 apply %v", e)
		}
	})

	t.Run("H_D36_H_D37_H_D38_H_D39_reversal", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, _, _ := seedBilling(t, db)
		holder := earnCredit(t, s, db, p, 12000, "hd36", 317)
		target := issuedPayReady(t, s, p, consultation(t, db, p, "HD36t"), tariff(t, db, "CONSULTATION", "HD36t", 20000))
		app, e := s.ApplyCredit(target.ID, CreditApplicationRequest{HolderPartyID: holder, Amount: 12000, IdempotencyKey: "hd36"}, 317)
		if e != nil {
			t.Fatal(e)
		}
		var movBefore int64
		db.Table("cash_movements").Count(&movBefore)
		rev, e := s.ReverseCreditApplication(app.Application.ID, CreditApplicationReversalRequest{Reason: "Mauvaise facture cible", IdempotencyKey: "hd36-rev"}, 317)
		if e != nil {
			t.Fatal(e)
		}
		avail, _ := AvailableCredit(db, holder, p)
		got, _ := s.GetInvoice(target.ID)
		if avail != 12000 || got.BalanceAmount != 20000 || got.CreditAppliedAmount != 0 || got.Status != InvoiceIssued {
			t.Fatalf("H-D36/37 avail=%d inv=%+v rev=%+v", avail, got, rev)
		}
		var movAfter, pays int64
		db.Table("cash_movements").Count(&movAfter)
		db.Model(&Payment{}).Where("invoice_id=?", target.ID).Count(&pays)
		if movAfter != movBefore || pays != 0 {
			t.Fatal("H-D38 cash/payment effect")
		}
		if _, e := s.ReverseCreditApplication(app.Application.ID, CreditApplicationReversalRequest{Reason: "Double annulation refusée", IdempotencyKey: "hd36-rev2"}, 317); creditErrCode(e) != CodeCreditApplicationAlreadyReversed {
			t.Fatalf("H-D39 %v", e)
		}
		again, e := s.ReverseCreditApplication(app.Application.ID, CreditApplicationReversalRequest{Reason: "Mauvaise facture cible", IdempotencyKey: "hd36-rev"}, 317)
		if e != nil || again.Application.ID != app.Application.ID {
			t.Fatalf("H-D39 idempotent replay %v", e)
		}
	})
}

func hasPerm(perms []string, want string) bool {
	for _, p := range perms {
		if p == want {
			return true
		}
	}
	return false
}

func TestLOT29F_HD_Concurrency(t *testing.T) {
	t.Run("H_DX01_double_spend_two_invoices", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, _, _ := seedBilling(t, db)
		holder := earnCredit(t, s, db, p, 10000, "hdx01", 401)
		x := issuedPayReady(t, s, p, consultation(t, db, p, "HDX01x"), tariff(t, db, "CONSULTATION", "HDX01x", 10000))
		y := issuedPayReady(t, s, p, consultation(t, db, p, "HDX01y"), tariff(t, db, "CONSULTATION", "HDX01y", 10000))
		var ok int32
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, e := s.ApplyCredit(x.ID, CreditApplicationRequest{HolderPartyID: holder, Amount: 10000, IdempotencyKey: "hdx01-x"}, 401); e == nil {
				atomic.AddInt32(&ok, 1)
			}
		}()
		go func() {
			defer wg.Done()
			if _, e := s.ApplyCredit(y.ID, CreditApplicationRequest{HolderPartyID: holder, Amount: 10000, IdempotencyKey: "hdx01-y"}, 401); e == nil {
				atomic.AddInt32(&ok, 1)
			}
		}()
		wg.Wait()
		if ok != 1 {
			t.Fatalf("H-DX01 successes=%d", ok)
		}
		avail, _ := AvailableCredit(db, holder, p)
		if avail != 0 {
			t.Fatalf("H-DX01 avail=%d", avail)
		}
	})

	t.Run("H_DX02_split_20k", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, _, _ := seedBilling(t, db)
		holder := earnCredit(t, s, db, p, 20000, "hdx02", 402)
		x := issuedPayReady(t, s, p, consultation(t, db, p, "HDX02x"), tariff(t, db, "CONSULTATION", "HDX02x", 10000))
		y := issuedPayReady(t, s, p, consultation(t, db, p, "HDX02y"), tariff(t, db, "CONSULTATION", "HDX02y", 10000))
		var ok int32
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, e := s.ApplyCredit(x.ID, CreditApplicationRequest{HolderPartyID: holder, Amount: 10000, IdempotencyKey: "hdx02-x"}, 402); e == nil {
				atomic.AddInt32(&ok, 1)
			}
		}()
		go func() {
			defer wg.Done()
			if _, e := s.ApplyCredit(y.ID, CreditApplicationRequest{HolderPartyID: holder, Amount: 10000, IdempotencyKey: "hdx02-y"}, 402); e == nil {
				atomic.AddInt32(&ok, 1)
			}
		}()
		wg.Wait()
		if ok != 2 {
			t.Fatalf("H-DX02 successes=%d", ok)
		}
		avail, _ := AvailableCredit(db, holder, p)
		if avail != 0 {
			t.Fatalf("H-DX02 avail=%d", avail)
		}
	})

	t.Run("H_DX03_15k_race", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, _, _ := seedBilling(t, db)
		holder := earnCredit(t, s, db, p, 15000, "hdx03", 403)
		x := issuedPayReady(t, s, p, consultation(t, db, p, "HDX03x"), tariff(t, db, "CONSULTATION", "HDX03x", 10000))
		y := issuedPayReady(t, s, p, consultation(t, db, p, "HDX03y"), tariff(t, db, "CONSULTATION", "HDX03y", 10000))
		var ok int32
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, e := s.ApplyCredit(x.ID, CreditApplicationRequest{HolderPartyID: holder, Amount: 10000, IdempotencyKey: "hdx03-x"}, 403); e == nil {
				atomic.AddInt32(&ok, 1)
			}
		}()
		go func() {
			defer wg.Done()
			if _, e := s.ApplyCredit(y.ID, CreditApplicationRequest{HolderPartyID: holder, Amount: 10000, IdempotencyKey: "hdx03-y"}, 403); e == nil {
				atomic.AddInt32(&ok, 1)
			}
		}()
		wg.Wait()
		if ok != 1 {
			t.Fatalf("H-DX03 successes=%d", ok)
		}
		avail, _ := AvailableCredit(db, holder, p)
		if avail < 0 || avail > 5000 {
			t.Fatalf("H-DX03 avail=%d", avail)
		}
	})

	t.Run("H_DX04_payment_vs_credit", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, _, _ := seedBilling(t, db)
		holder := earnCredit(t, s, db, p, 10000, "hdx04", 404)
		target := issuedPayReady(t, s, p, consultation(t, db, p, "HDX04t"), tariff(t, db, "CONSULTATION", "HDX04t", 10000))
		var payOK, creditOK int32
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, e := s.Pay(target.ID, PaymentRequest{Amount: 10000, PaymentMethod: "CASH", IdempotencyKey: "hdx04-pay", Payer: PatientPayerRequest()}, 404); e == nil {
				atomic.AddInt32(&payOK, 1)
			}
		}()
		go func() {
			defer wg.Done()
			if _, e := s.ApplyCredit(target.ID, CreditApplicationRequest{HolderPartyID: holder, Amount: 10000, IdempotencyKey: "hdx04-app"}, 404); e == nil {
				atomic.AddInt32(&creditOK, 1)
			}
		}()
		wg.Wait()
		if payOK+creditOK != 1 {
			t.Fatalf("H-DX04 pay=%d credit=%d", payOK, creditOK)
		}
		got, _ := s.GetInvoice(target.ID)
		settled := got.PaidAmount + got.CreditAppliedAmount
		if settled != 10000 || got.BalanceAmount != 0 {
			t.Fatalf("H-DX04 over-settle %+v", got)
		}
	})

	t.Run("H_DX05_same_key_concurrent", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, _, _ := seedBilling(t, db)
		holder := earnCredit(t, s, db, p, 10000, "hdx05", 405)
		target := issuedPayReady(t, s, p, consultation(t, db, p, "HDX05t"), tariff(t, db, "CONSULTATION", "HDX05t", 10000))
		var ids sync.Map
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				res, e := s.ApplyCredit(target.ID, CreditApplicationRequest{HolderPartyID: holder, Amount: 10000, IdempotencyKey: "hdx05"}, 405)
				if e == nil {
					ids.Store(res.Application.ID, true)
				}
			}()
		}
		wg.Wait()
		var n int
		ids.Range(func(_, _ any) bool { n++; return true })
		if n != 1 {
			t.Fatalf("H-DX05 distinct apps=%d", n)
		}
		var apps, applies int64
		db.Model(&CreditApplication{}).Count(&apps)
		db.Model(&CreditLedgerEntry{}).Where("entry_type=?", CreditEntryApply).Count(&applies)
		if apps != 1 || applies != 1 {
			t.Fatalf("H-DX05 apps=%d applies=%d", apps, applies)
		}
	})

	t.Run("H_DX06_different_keys_no_double_spend", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, _, _ := seedBilling(t, db)
		holder := earnCredit(t, s, db, p, 10000, "hdx06", 406)
		target := issuedPayReady(t, s, p, consultation(t, db, p, "HDX06t"), tariff(t, db, "CONSULTATION", "HDX06t", 10000))
		var ok int32
		var wg sync.WaitGroup
		for i := 0; i < 4; i++ {
			wg.Add(1)
			key := fmt.Sprintf("hdx06-%d", i)
			go func(k string) {
				defer wg.Done()
				if _, e := s.ApplyCredit(target.ID, CreditApplicationRequest{HolderPartyID: holder, Amount: 10000, IdempotencyKey: k}, 406); e == nil {
					atomic.AddInt32(&ok, 1)
				}
			}(key)
		}
		wg.Wait()
		if ok != 1 {
			t.Fatalf("H-DX06 successes=%d", ok)
		}
	})

	t.Run("H_DX07_cn_vs_apply", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, _, _ := seedBilling(t, db)
		holder := earnCredit(t, s, db, p, 10000, "hdx07a", 407)
		// Target invoice partially paid so CN can create more credit while apply races
		target := issuedPayReady(t, s, p, consultation(t, db, p, "HDX07t"), tariff(t, db, "CONSULTATION", "HDX07t", 30000))
		paySessionless(t, s, target.ID, 30000, "CASH", "hdx07-pay", 407)
		other := issuedPayReady(t, s, p, consultation(t, db, p, "HDX07o"), tariff(t, db, "CONSULTATION", "HDX07o", 10000))
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _ = s.IssueCreditNote(target.ID, CreditNoteRequest{Amount: 5000, Reason: "Race CN", IdempotencyKey: "hdx07-cn"}, 407)
		}()
		go func() {
			defer wg.Done()
			_, _ = s.ApplyCredit(other.ID, CreditApplicationRequest{HolderPartyID: holder, Amount: 10000, IdempotencyKey: "hdx07-app"}, 407)
		}()
		wg.Wait()
		avail, e := AvailableCredit(db, holder, p)
		if e != nil || avail < 0 {
			t.Fatalf("H-DX07 avail=%d %v", avail, e)
		}
	})

	t.Run("H_DX08_reversal_vs_apply", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, _, _ := seedBilling(t, db)
		inv := issuedPayReady(t, s, p, consultation(t, db, p, "HDX08"), tariff(t, db, "CONSULTATION", "HDX08", 20000))
		paid := paySessionless(t, s, inv.ID, 20000, "CASH", "hdx08-pay", 408)
		cn, e := s.IssueCreditNote(inv.ID, CreditNoteRequest{Amount: 10000, Reason: "Race D11", IdempotencyKey: "hdx08-cn"}, 408)
		if e != nil {
			t.Fatal(e)
		}
		holder := *cn.CreditHolderPartyID
		target := issuedPayReady(t, s, p, consultation(t, db, p, "HDX08t"), tariff(t, db, "CONSULTATION", "HDX08t", 10000))
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _ = s.ApplyCredit(target.ID, CreditApplicationRequest{HolderPartyID: holder, Amount: 10000, IdempotencyKey: "hdx08-app"}, 408)
		}()
		go func() {
			defer wg.Done()
			_, _ = s.ReversePayment(paid.Payments[0].ID, ReversePaymentRequest{Reason: "Race rev", IdempotencyKey: "hdx08-rev"}, 408)
		}()
		wg.Wait()
		// Either apply succeeded and reversal blocked, or reversal succeeded before credit existed —
		// never both: spent credit without backing CREDIT row is forbidden.
		hasCredit, _ := InvoiceHasPositiveCustomerCredit(db, inv.ID)
		var applied int64
		db.Model(&CreditApplication{}).Where("invoice_id=?", target.ID).Count(&applied)
		var revN int64
		db.Model(&PaymentReversal{}).Where("original_payment_id=?", paid.Payments[0].ID).Count(&revN)
		if applied > 0 && revN > 0 {
			t.Fatal("H-DX08 unbacked spent credit")
		}
		if applied > 0 && !hasCredit {
			t.Fatal("H-DX08 applied without CREDIT backing")
		}
	})

	t.Run("H_DX10_apply_vs_app_reversal", func(t *testing.T) {
		db := billingDB(t)
		s := receiptBilling(t, db)
		_, p, _, _ := seedBilling(t, db)
		holder := earnCredit(t, s, db, p, 10000, "hdx10", 410)
		target := issuedPayReady(t, s, p, consultation(t, db, p, "HDX10t"), tariff(t, db, "CONSULTATION", "HDX10t", 10000))
		app, e := s.ApplyCredit(target.ID, CreditApplicationRequest{HolderPartyID: holder, Amount: 10000, IdempotencyKey: "hdx10"}, 410)
		if e != nil {
			t.Fatal(e)
		}
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _ = s.ReverseCreditApplication(app.Application.ID, CreditApplicationReversalRequest{Reason: "Race reverse", IdempotencyKey: "hdx10-rev"}, 410)
		}()
		go func() {
			defer wg.Done()
			_, _ = s.ReverseCreditApplication(app.Application.ID, CreditApplicationReversalRequest{Reason: "Race reverse 2", IdempotencyKey: "hdx10-rev2"}, 410)
		}()
		wg.Wait()
		var revN int64
		db.Model(&CreditApplicationReversal{}).Where("original_application_id=?", app.Application.ID).Count(&revN)
		if revN != 1 {
			t.Fatalf("H-DX10 reversals=%d", revN)
		}
		avail, _ := AvailableCredit(db, holder, p)
		if avail != 10000 {
			t.Fatalf("H-DX10 avail=%d", avail)
		}
	})
}
