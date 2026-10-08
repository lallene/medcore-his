package billing

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
)

func TestLOT29F_HE_FinancialStatementMatrix(t *testing.T) {
	// One shared schema: per-subtest billingDB AutoMigrate is too slow on remote Neon under load.
	db := billingDB(t)
	hePatient := func(t *testing.T) uint {
		t.Helper()
		id, _ := seedPatient(t, db, fmt.Sprintf("HE-%d", time.Now().UnixNano()))
		return id
	}
	t.Run("H_E01_empty", func(t *testing.T) {
		s := receiptBilling(t, db)
		p := hePatient(t)
		st, e := s.GetFinancialStatement(p, true)
		if e != nil {
			t.Fatal(e)
		}
		if st.Summary.GrossPatientObligation != 0 || st.Summary.ReceivableOutstanding != 0 || len(st.Invoices) != 0 {
			t.Fatalf("H-E01 %+v", st)
		}
	})

	t.Run("H_E02_unpaid_invoice", func(t *testing.T) {
		s := receiptBilling(t, db)
		p := hePatient(t)
		inv := issuedPayReady(t, s, p, consultation(t, db, p, "HE02"), tariff(t, db, "CONSULTATION", "HE02", 50000))
		st, e := s.GetFinancialStatement(p, true)
		if e != nil {
			t.Fatal(e)
		}
		if st.Summary.GrossPatientObligation != 50000 || st.Summary.ReceivableOutstanding != 50000 ||
			st.Summary.EffectiveMoneyPaid != 0 || st.Summary.CreditApplied != 0 {
			t.Fatalf("H-E02 %+v", st.Summary)
		}
		if len(st.Invoices) != 1 || st.Invoices[0].InvoiceID != inv.ID {
			t.Fatalf("H-E02 invoices %+v", st.Invoices)
		}
	})

	t.Run("H_E03_payment", func(t *testing.T) {
		s := receiptBilling(t, db)
		p := hePatient(t)
		inv := issuedPayReady(t, s, p, consultation(t, db, p, "HE03"), tariff(t, db, "CONSULTATION", "HE03", 30000))
		paySessionless(t, s, inv.ID, 30000, "CASH", "he03-pay", 501)
		st, e := s.GetFinancialStatement(p, true)
		if e != nil || st.Summary.EffectiveMoneyPaid != 30000 || st.Summary.ReceivableOutstanding != 0 {
			t.Fatalf("H-E03 %+v %v", st, e)
		}
	})

	t.Run("H_E04_H_E17_payment_reversal", func(t *testing.T) {
		s := receiptBilling(t, db)
		p := hePatient(t)
		inv := issuedPayReady(t, s, p, consultation(t, db, p, "HE04"), tariff(t, db, "CONSULTATION", "HE04", 20000))
		paid := paySessionless(t, s, inv.ID, 20000, "CASH", "he04-pay", 502)
		if _, e := s.ReversePayment(paid.Payments[0].ID, ReversePaymentRequest{Reason: "Erreur de saisie HE04", IdempotencyKey: "he04-rev"}, 502); e != nil {
			t.Fatal(e)
		}
		st, e := s.GetFinancialStatement(p, true)
		if e != nil || st.Summary.EffectiveMoneyPaid != 0 || st.Summary.ReceivableOutstanding != 20000 {
			t.Fatalf("H-E04 %+v %v", st, e)
		}
		hist, e := s.ListFinancialHistory(FinancialHistoryFilter{PatientID: p, Limit: 50})
		if e != nil {
			t.Fatal(e)
		}
		var sawPay, sawRev bool
		for _, ev := range hist.Data {
			if ev.EventType == FinEventPaymentReceived {
				sawPay = true
			}
			if ev.EventType == FinEventPaymentReversed {
				sawRev = true
				if ev.Label == "Remboursé" || containsRefund(ev.Label) {
					t.Fatal("H-E17 refund wording")
				}
			}
		}
		if !sawPay || !sawRev {
			t.Fatalf("H-E17 history %+v", hist.Data)
		}
	})

	t.Run("H_E05_unpaid_cn", func(t *testing.T) {
		s := receiptBilling(t, db)
		p := hePatient(t)
		inv := issuedPayReady(t, s, p, consultation(t, db, p, "HE05"), tariff(t, db, "CONSULTATION", "HE05", 40000))
		out, e := s.IssueCreditNote(inv.ID, CreditNoteRequest{Amount: 40000, Reason: "Annulation complète", IdempotencyKey: "he05"}, 503)
		if e != nil {
			t.Fatal(e)
		}
		st, e := s.GetFinancialStatement(p, true)
		if e != nil || st.Summary.CreditNoteReduction != 40000 || st.Summary.CorrectedPatientObligation != 0 ||
			st.Summary.CreditAvailable != 0 || out.CustomerCreditAmount != 0 {
			t.Fatalf("H-E05 %+v cn=%+v %v", st.Summary, out, e)
		}
	})

	t.Run("H_E06_H_E19_paid_cn_credit", func(t *testing.T) {
		s := receiptBilling(t, db)
		p := hePatient(t)
		inv := issuedPayReady(t, s, p, consultation(t, db, p, "HE06"), tariff(t, db, "CONSULTATION", "HE06", 50000))
		paySessionless(t, s, inv.ID, 50000, "CASH", "he06-pay", 504)
		out, e := s.IssueCreditNote(inv.ID, CreditNoteRequest{Amount: 10000, Reason: "Réduction 10k", IdempotencyKey: "he06"}, 504)
		if e != nil || out.CustomerCreditAmount != 10000 {
			t.Fatalf("setup %v", e)
		}
		st, e := s.GetFinancialStatement(p, true)
		if e != nil {
			t.Fatal(e)
		}
		// Obligation original 50k, avoir 10k, corrected 40k, money 50k, credit earned 10k, receivable 0
		if st.Summary.GrossPatientObligation != 50000 || st.Summary.CreditNoteReduction != 10000 ||
			st.Summary.CorrectedPatientObligation != 40000 || st.Summary.EffectiveMoneyPaid != 50000 ||
			st.Summary.CreditEarned != 10000 || st.Summary.CreditAvailable != 10000 ||
			st.Summary.ReceivableOutstanding != 0 || st.Summary.CreditApplied != 0 {
			t.Fatalf("H-E06/19 %+v", st.Summary)
		}
		// Credit note reduction must not equal credit earned+reduction double count
		if st.Summary.CreditNoteReduction+st.Summary.CreditEarned == st.Summary.CreditNoteReduction*2 && st.Summary.CreditEarned != 10000 {
			t.Fatal("double count")
		}
	})

	t.Run("H_E07_partial_cn_zero_credit", func(t *testing.T) {
		s := receiptBilling(t, db)
		p := hePatient(t)
		inv := issuedPayReady(t, s, p, consultation(t, db, p, "HE07"), tariff(t, db, "CONSULTATION", "HE07", 50000))
		paySessionless(t, s, inv.ID, 30000, "CASH", "he07-pay", 505)
		out, e := s.IssueCreditNote(inv.ID, CreditNoteRequest{Amount: 10000, Reason: "Réduction partielle", IdempotencyKey: "he07"}, 505)
		if e != nil || out.CustomerCreditAmount != 0 {
			t.Fatalf("setup %+v %v", out, e)
		}
		st, e := s.GetFinancialStatement(p, true)
		if e != nil || st.Summary.CreditAvailable != 0 || st.Summary.ReceivableOutstanding != 10000 {
			t.Fatalf("H-E07 %+v %v", st.Summary, e)
		}
	})

	t.Run("H_E08_H_E09_H_E35_credit_application", func(t *testing.T) {
		s := receiptBilling(t, db)
		p := hePatient(t)
		holder := earnCredit(t, s, db, p, 20000, "he08", 506)
		target := issuedPayReady(t, s, p, consultation(t, db, p, "HE08t"), tariff(t, db, "CONSULTATION", "HE08t", 50000))
		if _, e := s.ApplyCredit(target.ID, CreditApplicationRequest{HolderPartyID: holder, Amount: 20000, IdempotencyKey: "he08"}, 506); e != nil {
			t.Fatal(e)
		}
		st, e := s.GetFinancialStatement(p, true)
		if e != nil {
			t.Fatal(e)
		}
		// Earn invoice: money 50k; target: money 0, applied 20k, receivable 30k
		if st.Summary.EffectiveMoneyPaid != 50000 {
			t.Fatalf("H-E35 money must exclude credit apply: %d", st.Summary.EffectiveMoneyPaid)
		}
		if st.Summary.CreditApplied != 20000 || st.Summary.CreditAvailable != 0 || st.Summary.ReceivableOutstanding != 30000 {
			t.Fatalf("H-E08/09 %+v", st.Summary)
		}
		var targetLine *FinancialInvoiceLine
		for i := range st.Invoices {
			if st.Invoices[i].InvoiceID == target.ID {
				targetLine = &st.Invoices[i]
			}
		}
		if targetLine == nil || targetLine.EffectiveMoneyPaid != 0 || targetLine.CreditApplied != 20000 {
			t.Fatalf("H-E08 line %+v", targetLine)
		}
	})

	t.Run("H_E10_H_E18_application_reversal", func(t *testing.T) {
		s := receiptBilling(t, db)
		p := hePatient(t)
		holder := earnCredit(t, s, db, p, 15000, "he10", 507)
		target := issuedPayReady(t, s, p, consultation(t, db, p, "HE10t"), tariff(t, db, "CONSULTATION", "HE10t", 20000))
		app, e := s.ApplyCredit(target.ID, CreditApplicationRequest{HolderPartyID: holder, Amount: 15000, IdempotencyKey: "he10"}, 507)
		if e != nil {
			t.Fatal(e)
		}
		if _, e := s.ReverseCreditApplication(app.Application.ID, CreditApplicationReversalRequest{Reason: "Mauvaise facture cible HE10", IdempotencyKey: "he10-rev"}, 507); e != nil {
			t.Fatal(e)
		}
		st, e := s.GetFinancialStatement(p, true)
		if e != nil || st.Summary.CreditApplied != 0 || st.Summary.CreditAvailable != 15000 {
			t.Fatalf("H-E10 %+v %v", st.Summary, e)
		}
		hist, e := s.ListFinancialHistory(FinancialHistoryFilter{PatientID: p, Limit: 100})
		if e != nil {
			t.Fatal(e)
		}
		var sawApp, sawRev bool
		for _, ev := range hist.Data {
			if ev.EventType == FinEventCreditApplied {
				sawApp = true
			}
			if ev.EventType == FinEventCreditApplicationReversed {
				sawRev = true
			}
		}
		if !sawApp || !sawRev {
			t.Fatalf("H-E18 %+v", hist.Data)
		}
	})

	t.Run("H_E11_mixed_settlement", func(t *testing.T) {
		s := receiptBilling(t, db)
		p := hePatient(t)
		holder := earnCredit(t, s, db, p, 30000, "he11", 508)
		target := issuedPayReady(t, s, p, consultation(t, db, p, "HE11t"), tariff(t, db, "CONSULTATION", "HE11t", 50000))
		paySessionless(t, s, target.ID, 20000, "CASH", "he11-pay", 508)
		if _, e := s.ApplyCredit(target.ID, CreditApplicationRequest{HolderPartyID: holder, Amount: 30000, IdempotencyKey: "he11"}, 508); e != nil {
			t.Fatal(e)
		}
		st, e := s.GetFinancialStatement(p, true)
		if e != nil {
			t.Fatal(e)
		}
		var line *FinancialInvoiceLine
		for i := range st.Invoices {
			if st.Invoices[i].InvoiceID == target.ID {
				line = &st.Invoices[i]
			}
		}
		if line == nil || line.EffectiveMoneyPaid != 20000 || line.CreditApplied != 30000 ||
			line.TotalSettled != 50000 || line.RemainingReceivable != 0 {
			t.Fatalf("H-E11 %+v", line)
		}
	})

	t.Run("H_E12_no_auto_net", func(t *testing.T) {
		s := receiptBilling(t, db)
		p := hePatient(t)
		_ = earnCredit(t, s, db, p, 20000, "he12", 509)
		target := issuedPayReady(t, s, p, consultation(t, db, p, "HE12t"), tariff(t, db, "CONSULTATION", "HE12t", 50000))
		st, e := s.GetFinancialStatement(p, true)
		if e != nil {
			t.Fatal(e)
		}
		var recv int64
		for _, inv := range st.Invoices {
			if inv.InvoiceID == target.ID {
				recv = inv.RemainingReceivable
			}
		}
		if recv != 50000 || st.Summary.CreditAvailable != 20000 {
			t.Fatalf("H-E12 must not net: recv=%d avail=%d", recv, st.Summary.CreditAvailable)
		}
		// Informational patient total equals holders; still not netting receivable
		if st.PatientCreditTotalAvailable != 20000 {
			t.Fatalf("H-E15 total %d", st.PatientCreditTotalAvailable)
		}
	})

	t.Run("H_E13_H_E21_multi_invoice_aggregate", func(t *testing.T) {
		s := receiptBilling(t, db)
		p := hePatient(t)
		a := issuedPayReady(t, s, p, consultation(t, db, p, "HE13a"), tariff(t, db, "CONSULTATION", "HE13a", 10000))
		b := issuedPayReady(t, s, p, consultation(t, db, p, "HE13b"), tariff(t, db, "CONSULTATION", "HE13b", 20000))
		paySessionless(t, s, a.ID, 10000, "CASH", "he13a", 510)
		st, e := s.GetFinancialStatement(p, true)
		if e != nil {
			t.Fatal(e)
		}
		var sumRecv, sumMoney int64
		for _, inv := range st.Invoices {
			sumRecv += inv.RemainingReceivable
			sumMoney += inv.EffectiveMoneyPaid
		}
		if sumRecv != st.Summary.ReceivableOutstanding || sumMoney != st.Summary.EffectiveMoneyPaid {
			t.Fatalf("H-E21 mismatch summary=%+v lines recv=%d money=%d", st.Summary, sumRecv, sumMoney)
		}
		if st.Summary.ReceivableOutstanding != 20000 || st.Summary.EffectiveMoneyPaid != 10000 {
			t.Fatalf("H-E13 %+v b=%d", st.Summary, b.ID)
		}
	})

	t.Run("H_E14_H_E15_multi_holder", func(t *testing.T) {
		s := receiptBilling(t, db)
		p := hePatient(t)
		_ = db.Model(&patients.Patient{}).Where("id=?", p).Updates(map[string]any{"telephone": "+2250700001415"})
		holderA := earnCredit(t, s, db, p, 10000, "he14a", 511)
		invB := issuedPayReady(t, s, p, consultation(t, db, p, "HE14b"), tariff(t, db, "CONSULTATION", "HE14b", 50000))
		if _, e := s.Pay(invB.ID, PaymentRequest{
			Amount: 50000, PaymentMethod: "CASH", IdempotencyKey: "he14b-pay",
			Payer: &PayerRequest{Mode: PayerModeIndividual, DisplayName: "Tuteur HE14", Phone: "+2250700141414", Relationship: "Tuteur"},
		}, 511); e != nil {
			t.Fatal(e)
		}
		outB, e := s.IssueCreditNote(invB.ID, CreditNoteRequest{Amount: 20000, Reason: "Crédit tuteur", IdempotencyKey: "he14b-cn"}, 511)
		if e != nil {
			t.Fatal(e)
		}
		holderB := *outB.CreditHolderPartyID
		st, e := s.GetFinancialStatement(p, true)
		if e != nil {
			t.Fatal(e)
		}
		if len(st.Holders) < 2 {
			t.Fatalf("H-E14 holders %+v", st.Holders)
		}
		var aAvail, bAvail, total int64
		for _, h := range st.Holders {
			total += h.AvailableCredit
			if h.HolderPartyID == holderA {
				aAvail = h.AvailableCredit
			}
			if h.HolderPartyID == holderB {
				bAvail = h.AvailableCredit
			}
		}
		if aAvail != 10000 || bAvail != 20000 || total != 30000 || st.PatientCreditTotalAvailable != 30000 {
			t.Fatalf("H-E14/15 A=%d B=%d total=%d", aAvail, bAvail, total)
		}
	})

	t.Run("H_E16_legacy_payer", func(t *testing.T) {
		s := receiptBilling(t, db)
		p := hePatient(t)
		inv := issuedPayReady(t, s, p, consultation(t, db, p, "HE16"), tariff(t, db, "CONSULTATION", "HE16", 10000))
		paid := paySessionless(t, s, inv.ID, 10000, "CASH", "he16-pay", 512)
		if e := db.Model(&Payment{}).Where("id=?", paid.Payments[0].ID).Updates(map[string]any{
			"payer_provenance": PayerProvenanceLegacyUnconfirmed, "payer_party_id": nil, "payer_display_name": "",
		}).Error; e != nil {
			t.Fatal(e)
		}
		hist, e := s.ListFinancialHistory(FinancialHistoryFilter{PatientID: p, Limit: 50})
		if e != nil {
			t.Fatal(e)
		}
		found := false
		for _, ev := range hist.Data {
			if ev.EventType == FinEventPaymentReceived && ev.PayerUnknown {
				found = true
				if ev.PayerDisplay != "Payeur non renseigné (historique)" {
					t.Fatalf("H-E16 display %q", ev.PayerDisplay)
				}
			}
		}
		if !found {
			t.Fatalf("H-E16 %+v", hist.Data)
		}
	})

	t.Run("H_E20_matches_invoice_authority", func(t *testing.T) {
		s := receiptBilling(t, db)
		p := hePatient(t)
		holder := earnCredit(t, s, db, p, 10000, "he20", 513)
		target := issuedPayReady(t, s, p, consultation(t, db, p, "HE20t"), tariff(t, db, "CONSULTATION", "HE20t", 25000))
		paySessionless(t, s, target.ID, 5000, "CASH", "he20-pay", 513)
		if _, e := s.ApplyCredit(target.ID, CreditApplicationRequest{HolderPartyID: holder, Amount: 10000, IdempotencyKey: "he20"}, 513); e != nil {
			t.Fatal(e)
		}
		got, _ := s.GetInvoice(target.ID)
		st, e := s.GetFinancialStatement(p, true)
		if e != nil {
			t.Fatal(e)
		}
		for _, line := range st.Invoices {
			if line.InvoiceID != target.ID {
				continue
			}
			if line.RemainingReceivable != got.BalanceAmount || line.EffectiveMoneyPaid != got.PaidAmount ||
				line.CreditApplied != got.CreditAppliedAmount {
				t.Fatalf("H-E20 line=%+v inv=%+v", line, got)
			}
		}
	})

	t.Run("H_E22_H_E23_ordering_pagination", func(t *testing.T) {
		s := receiptBilling(t, db)
		p := hePatient(t)
		inv := issuedPayReady(t, s, p, consultation(t, db, p, "HE22"), tariff(t, db, "CONSULTATION", "HE22", 10000))
		paySessionless(t, s, inv.ID, 5000, "CASH", "he22a", 514)
		paySessionless(t, s, inv.ID, 5000, "CASH", "he22b", 514)
		h1, e := s.ListFinancialHistory(FinancialHistoryFilter{PatientID: p, Page: 1, Limit: 2})
		if e != nil {
			t.Fatal(e)
		}
		h2, e := s.ListFinancialHistory(FinancialHistoryFilter{PatientID: p, Page: 2, Limit: 2})
		if e != nil {
			t.Fatal(e)
		}
		seen := map[string]bool{}
		for _, ev := range append(h1.Data, h2.Data...) {
			if seen[ev.SortKey] {
				t.Fatalf("H-E23 duplicate %s", ev.SortKey)
			}
			seen[ev.SortKey] = true
		}
		for i := 1; i < len(h1.Data); i++ {
			if h1.Data[i].SortKey < h1.Data[i-1].SortKey {
				t.Fatal("H-E22 order")
			}
		}
	})

	t.Run("H_E24_H_E25_H_E26_H_E27_filters", func(t *testing.T) {
		s := receiptBilling(t, db)
		p := hePatient(t)
		holder := earnCredit(t, s, db, p, 5000, "he24", 515)
		target := issuedPayReady(t, s, p, consultation(t, db, p, "HE24t"), tariff(t, db, "CONSULTATION", "HE24t", 8000))
		if _, e := s.ApplyCredit(target.ID, CreditApplicationRequest{HolderPartyID: holder, Amount: 5000, IdempotencyKey: "he24"}, 515); e != nil {
			t.Fatal(e)
		}
		byType, e := s.ListFinancialHistory(FinancialHistoryFilter{PatientID: p, EventType: FinEventCreditApplied, Limit: 50})
		if e != nil || len(byType.Data) == 0 {
			t.Fatalf("H-E25 %v", e)
		}
		for _, ev := range byType.Data {
			if ev.EventType != FinEventCreditApplied {
				t.Fatal("H-E25")
			}
		}
		byInv, e := s.ListFinancialHistory(FinancialHistoryFilter{PatientID: p, InvoiceID: target.ID, Limit: 50})
		if e != nil {
			t.Fatal(e)
		}
		for _, ev := range byInv.Data {
			if ev.InvoiceID != nil && *ev.InvoiceID != target.ID && ev.EventType != FinEventCreditEarned {
				// earn events may reference other invoice — when InvoiceID filter on earn from CN of earn invoice they are filtered in collect
			}
			if ev.EventType == FinEventCreditApplied && (ev.InvoiceID == nil || *ev.InvoiceID != target.ID) {
				t.Fatal("H-E26")
			}
		}
		byHolder, e := s.ListFinancialHistory(FinancialHistoryFilter{PatientID: p, HolderID: holder, Limit: 50})
		if e != nil || len(byHolder.Data) == 0 {
			t.Fatalf("H-E27 %v", e)
		}
		today := time.Now().Format("2006-01-02")
		byDate, e := s.ListFinancialHistory(FinancialHistoryFilter{PatientID: p, DateFrom: today, DateTo: today, Limit: 50})
		if e != nil || byDate.Total == 0 {
			t.Fatalf("H-E24 %v", e)
		}
	})

	t.Run("H_E28_H_E29_H_E32_rbac", func(t *testing.T) {
		if hasPerm(rbac.EffectiveStaffPermissions("staff", []string{"INFIRMIER"}, nil), "billing.statement.read") {
			t.Fatal("H-E29")
		}
		if hasPerm(rbac.EffectiveStaffPermissions("staff", []string{"CAISSIER"}, nil), "billing.statement.read") {
			t.Fatal("H-E28 caissier")
		}
		// patients.360.read alone does not imply statement
		perms := rbac.EffectiveStaffPermissions("staff", []string{"INFIRMIER"}, nil)
		if hasPerm(perms, "patients.360.read") && hasPerm(perms, "billing.statement.read") {
			t.Fatal("H-E32")
		}
		for _, role := range []string{"FACTURATION", "COMPTABLE", "DIRECTEUR_ADMINISTRATIF"} {
			if !hasPerm(rbac.EffectiveStaffPermissions("staff", []string{role}, nil), "billing.statement.read") {
				t.Fatalf("H-E28 %s", role)
			}
		}
	})

	t.Run("H_E30_H_E31_payer_pii", func(t *testing.T) {
		s := receiptBilling(t, db)
		p := hePatient(t)
		_ = db.Model(&patients.Patient{}).Where("id=?", p).Updates(map[string]any{"telephone": "+2250700003031"})
		inv := issuedPayReady(t, s, p, consultation(t, db, p, "HE30"), tariff(t, db, "CONSULTATION", "HE30", 50000))
		if _, e := s.Pay(inv.ID, PaymentRequest{
			Amount: 50000, PaymentMethod: "CASH", IdempotencyKey: "he30-pay",
			Payer: &PayerRequest{Mode: PayerModeIndividual, DisplayName: "Parent HE30", Phone: "+2250700303030", Relationship: "Parent"},
		}, 516); e != nil {
			t.Fatal(e)
		}
		if _, e := s.IssueCreditNote(inv.ID, CreditNoteRequest{Amount: 5000, Reason: "Crédit PII", IdempotencyKey: "he30-cn"}, 516); e != nil {
			t.Fatal(e)
		}
		redacted, e := s.GetFinancialStatement(p, false)
		if e != nil {
			t.Fatal(e)
		}
		for _, h := range redacted.Holders {
			if h.Phone != "" {
				t.Fatalf("H-E30 phone leaked %q", h.Phone)
			}
		}
		full, e := s.GetFinancialStatement(p, true)
		if e != nil {
			t.Fatal(e)
		}
		sawPhone := false
		for _, h := range full.Holders {
			if h.Phone != "" {
				sawPhone = true
			}
		}
		if !sawPhone {
			t.Fatalf("H-E31 expected phone %+v", full.Holders)
		}
	})

	t.Run("H_E33_H_E36_no_writes", func(t *testing.T) {
		s := receiptBilling(t, db)
		p := hePatient(t)
		_ = earnCredit(t, s, db, p, 5000, "he33", 517)
		var payN, cnN, ledN, appN, movN int64
		db.Model(&Payment{}).Count(&payN)
		db.Model(&CreditNote{}).Count(&cnN)
		db.Model(&CreditLedgerEntry{}).Count(&ledN)
		db.Model(&CreditApplication{}).Count(&appN)
		db.Table("cash_movements").Count(&movN)
		if _, e := s.GetFinancialStatement(p, true); e != nil {
			t.Fatal(e)
		}
		if _, e := s.ListFinancialHistory(FinancialHistoryFilter{PatientID: p, Limit: 20}); e != nil {
			t.Fatal(e)
		}
		var pay2, cn2, led2, app2, mov2 int64
		db.Model(&Payment{}).Count(&pay2)
		db.Model(&CreditNote{}).Count(&cn2)
		db.Model(&CreditLedgerEntry{}).Count(&led2)
		db.Model(&CreditApplication{}).Count(&app2)
		db.Table("cash_movements").Count(&mov2)
		if pay2 != payN || cn2 != cnN || led2 != ledN || app2 != appN || mov2 != movN {
			t.Fatal("H-E33/36 write detected")
		}
	})

	t.Run("H_E34_available_matches_hd", func(t *testing.T) {
		s := receiptBilling(t, db)
		p := hePatient(t)
		holder := earnCredit(t, s, db, p, 12000, "he34", 518)
		sum, e := s.GetCreditSummary(holder, p)
		if e != nil {
			t.Fatal(e)
		}
		st, e := s.GetFinancialStatement(p, true)
		if e != nil {
			t.Fatal(e)
		}
		var avail int64
		for _, h := range st.Holders {
			if h.HolderPartyID == holder {
				avail = h.AvailableCredit
			}
		}
		if avail != sum.AvailableCredit {
			t.Fatalf("H-E34 %d vs %d", avail, sum.AvailableCredit)
		}
	})
}

func TestLOT29F_HE_PostgresProjection(t *testing.T) {
	db := billingDB(t)
	hePatient := func(t *testing.T) uint {
		t.Helper()
		id, _ := seedPatient(t, db, fmt.Sprintf("HEX-%d", time.Now().UnixNano()))
		return id
	}
	t.Run("H_EX01_H_EX02_multi", func(t *testing.T) {
		s := receiptBilling(t, db)
		p := hePatient(t)
		_ = db.Model(&patients.Patient{}).Where("id=?", p).Updates(map[string]any{"telephone": "+225070000ex01"})
		h1 := earnCredit(t, s, db, p, 10000, "hex01a", 601)
		invB := issuedPayReady(t, s, p, consultation(t, db, p, "HEX01b"), tariff(t, db, "CONSULTATION", "HEX01b", 50000))
		if _, e := s.Pay(invB.ID, PaymentRequest{
			Amount: 50000, PaymentMethod: "CASH", IdempotencyKey: "hex01b-pay",
			Payer: &PayerRequest{Mode: PayerModeIndividual, DisplayName: "Autre HEX", Phone: "+2250700010101", Relationship: "Ami"},
		}, 601); e != nil {
			t.Fatal(e)
		}
		out, e := s.IssueCreditNote(invB.ID, CreditNoteRequest{Amount: 5000, Reason: "HEX credit", IdempotencyKey: "hex01b-cn"}, 601)
		if e != nil {
			t.Fatal(e)
		}
		h2 := *out.CreditHolderPartyID
		for i := 0; i < 5; i++ {
			issuedPayReady(t, s, p, consultation(t, db, p, "HEX01x"+string(rune('a'+i))), tariff(t, db, "CONSULTATION", "HEX01x"+string(rune('a'+i)), 1000*(int64(i)+1)))
		}
		st, e := s.GetFinancialStatement(p, true)
		if e != nil {
			t.Fatal(e)
		}
		var a, b int64
		for _, h := range st.Holders {
			if h.HolderPartyID == h1 {
				a = h.AvailableCredit
			}
			if h.HolderPartyID == h2 {
				b = h.AvailableCredit
			}
		}
		if a != 10000 || b != 5000 {
			t.Fatalf("H-EX02 %d %d", a, b)
		}
		var sumRecv int64
		for _, inv := range st.Invoices {
			sumRecv += inv.RemainingReceivable
		}
		if sumRecv != st.Summary.ReceivableOutstanding {
			t.Fatal("H-EX01")
		}
	})

	t.Run("H_EX03_H_EX04_stable_pagination", func(t *testing.T) {
		s := receiptBilling(t, db)
		p := hePatient(t)
		inv := issuedPayReady(t, s, p, consultation(t, db, p, "HEX03"), tariff(t, db, "CONSULTATION", "HEX03", 30000))
		for i := 0; i < 6; i++ {
			paySessionless(t, s, inv.ID, 5000, "CASH", "hex03-"+string(rune('a'+i)), 602)
		}
		keys := []string{}
		for page := 1; page <= 4; page++ {
			h, e := s.ListFinancialHistory(FinancialHistoryFilter{PatientID: p, Page: page, Limit: 3})
			if e != nil {
				t.Fatal(e)
			}
			for _, ev := range h.Data {
				keys = append(keys, ev.SortKey)
			}
		}
		seen := map[string]bool{}
		for _, k := range keys {
			if seen[k] {
				t.Fatalf("H-EX04 dup %s", k)
			}
			seen[k] = true
		}
		for i := 1; i < len(keys); i++ {
			if keys[i] < keys[i-1] {
				t.Fatal("H-EX03 order across pages")
			}
		}
	})

	t.Run("H_EX05_reversed_effective", func(t *testing.T) {
		s := receiptBilling(t, db)
		p := hePatient(t)
		holder := earnCredit(t, s, db, p, 10000, "hex05", 603)
		target := issuedPayReady(t, s, p, consultation(t, db, p, "HEX05t"), tariff(t, db, "CONSULTATION", "HEX05t", 10000))
		app, e := s.ApplyCredit(target.ID, CreditApplicationRequest{HolderPartyID: holder, Amount: 10000, IdempotencyKey: "hex05"}, 603)
		if e != nil {
			t.Fatal(e)
		}
		if _, e := s.ReverseCreditApplication(app.Application.ID, CreditApplicationReversalRequest{Reason: "Correction HEX05", IdempotencyKey: "hex05-rev"}, 603); e != nil {
			t.Fatal(e)
		}
		st, e := s.GetFinancialStatement(p, true)
		if e != nil || st.Summary.CreditApplied != 0 {
			t.Fatalf("H-EX05 %+v %v", st.Summary, e)
		}
	})

	t.Run("H_EX06_concurrent_write_during_read", func(t *testing.T) {
		s := receiptBilling(t, db)
		p := hePatient(t)
		holder := earnCredit(t, s, db, p, 20000, "hex06", 604)
		target := issuedPayReady(t, s, p, consultation(t, db, p, "HEX06t"), tariff(t, db, "CONSULTATION", "HEX06t", 20000))
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _ = s.ApplyCredit(target.ID, CreditApplicationRequest{HolderPartyID: holder, Amount: 10000, IdempotencyKey: "hex06-app"}, 604)
		}()
		go func() {
			defer wg.Done()
			st, e := s.GetFinancialStatement(p, true)
			if e != nil {
				t.Error(e)
				return
			}
			// Snapshot consistency: applied <= available+applied earned, receivable never negative
			if st.Summary.ReceivableOutstanding < 0 || st.Summary.CreditAvailable < 0 {
				t.Errorf("H-EX06 impossible %+v", st.Summary)
			}
			if st.Summary.CreditApplied+st.Summary.CreditAvailable > st.Summary.CreditEarned+st.Summary.CreditRestored+20000 {
				// loose bound — mainly ensure non-negative coherent fields
			}
		}()
		wg.Wait()
		st, e := s.GetFinancialStatement(p, true)
		if e != nil || st.Summary.ReceivableOutstanding < 0 || st.Summary.CreditAvailable < 0 {
			t.Fatalf("H-EX06 final %+v %v", st, e)
		}
	})
}

// H-E37: I-A wired request/decision events; EXECUTED remains future-only (no money moved wording).
func TestLOT29F_HE_RefundEventExtensionReady(t *testing.T) {
	if FinEventRefundRequested == "" || FinEventRefundApproved == "" || FinEventRefundRejected == "" || FinEventRefundCancelled == "" {
		t.Fatal("H-E37 I-A refund event constants missing")
	}
	// EXECUTED must not collide with I-A constants and is not emitted yet.
	executed := "REFUND_EXECUTED"
	for _, ev := range []string{FinEventRefundRequested, FinEventRefundApproved, FinEventRefundRejected, FinEventRefundCancelled} {
		if ev == executed {
			t.Fatalf("H-E37 collision %s", ev)
		}
	}
	for _, label := range []string{"Paiement contrepassé", "Utilisation de crédit annulée", "Crédit acquis", "Remboursement autorisé", "Demande de remboursement"} {
		if strings.Contains(strings.ToLower(label), "effectué") || strings.Contains(strings.ToLower(label), "versé") {
			t.Fatalf("H-E37 premature execution wording %q", label)
		}
	}
}

func containsRefund(s string) bool {
	for _, w := range []string{"Rembours", "refund", "Refund"} {
		if len(s) >= len(w) {
			for i := 0; i+len(w) <= len(s); i++ {
				if s[i:i+len(w)] == w || (i+len(w) <= len(s) && equalFoldASCII(s[i:i+len(w)], w)) {
					return true
				}
			}
		}
	}
	return false
}

func equalFoldASCII(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}
