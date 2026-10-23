package complete

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/billing"
	"github.com/lallene/medcore-his/backend/internal/modules/cash"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/authorization"
)

// Finance through billing.Service / cash.Service / authorization.Service:
//
//	daily cash sessions (open → payments → movements → close with counted cash and variances)
//	billable acts → [PEC: create → submit → (pending) → decision] → invoice → issue
//	→ payments (CASH via cash session only, MOBILE_MONEY, CARD at the desk, BANK_TRANSFER /
//	CHECK sessionless) by patient / relative / organization; partial, multiple, unpaid
//	→ reversals (same-session; post-close + physical cash correction), credit notes,
//	credit applications (+ reversal), refunds REQUESTED → APPROVED | REJECTED | CANCELLED
//	→ EXECUTED with separation of duties (requester ≠ approver ≠ executor).

type openSession struct {
	ID      uint
	Cashier *Member
}

type paidRef struct {
	InvoiceID, PaymentID uint
	Patient              *Profile
	Amount               int64
	Method               string
	At                   time.Time
	Register             string
}

type financeState struct {
	sessions   map[string]*openSession // by register code
	billQueued map[uint]time.Time      // patient -> next scheduled billing step
	paid       []paidRef               // recent cash-desk payments (corrections, credit notes)
	creditors  []*Profile              // patients holding customer credit
	invoices   int
	refundSeq  int
}

func (f *financeState) init() {
	if f.sessions == nil {
		f.sessions = map[string]*openSession{}
		f.billQueued = map[uint]time.Time{}
	}
}

var registerFor = map[string]string{"URG": "CAISSE-URG", "MAT": "CAISSE-MAT", "PED": "CAISSE-MAT", "GYNE": "CAISSE-MAT"}

func (w *World) register(code string) cash.Register {
	for _, r := range w.Ref.Registers {
		if r.Code == code {
			return r
		}
	}
	return w.Ref.Registers[0]
}

func (w *World) cashierFor(register string, d time.Time) *Member {
	pick := func(tag, email string) *Member {
		if m := w.Staff.Tagged(tag); m != nil {
			return m
		}
		return w.Staff.byEmail[StaffEmailPrefix+email+StaffEmailDomain]
	}
	switch register {
	case "CAISSE-URG":
		return pick("_none", "caisse.3")
	case "CAISSE-MAT":
		return pick("multi-role", "facturation.caisse")
	}
	if d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
		return pick("_none", "caisse.2")
	}
	return pick("refund-executor", "caisse.1") // executes CASH refunds from its own open session
}

type financePlanner struct{}

func (financePlanner) Name() string { return "Facturation / caisse" }
func (financePlanner) Setup(w *World) error {
	w.Finance.init()
	return nil
}

func (financePlanner) PlanDay(w *World, d Day) {
	g := w.RNG.Stream("finance.day", uint64(d.Index))
	for _, reg := range w.Ref.Registers {
		reg := reg
		if reg.Code != "CAISSE-URG" && d.Date.Weekday() == time.Sunday {
			continue
		}
		open, close := d.At(7, 0), d.At(19, 30)
		if reg.Code == "CAISSE-URG" {
			open, close = d.At(6, 30), d.At(22, 0)
		}
		w.Sched.At(open, "caisse.ouverture", func(ctx *StepContext) error { return openCash(ctx, reg, g) })
		w.Sched.At(close, "caisse.cloture", func(ctx *StepContext) error { return closeCash(ctx, reg, g) })
	}
	// Manual movements (complementary float, petty-cash expenses, bank deposit).
	if g.Chance(0.25) {
		w.Sched.At(d.At(10, 15), "caisse.mouvement-entree", func(ctx *StepContext) error {
			return movement(ctx, "CAISSE-PRINC", cash.MovementIn, cash.MovementManualIn, int64(g.Between(2, 10))*5000, "Fonds de caisse complémentaire")
		})
	}
	if g.Chance(0.3) {
		w.Sched.At(d.At(17, 40), "caisse.mouvement-sortie", func(ctx *StepContext) error {
			return movement(ctx, Pick(g, []string{"CAISSE-PRINC", "CAISSE-URG"}), cash.MovementOut, cash.MovementManualOut, int64(g.Between(1, 8))*2500, Pick(g, []string{"Achat de fournitures", "Dépôt bancaire partiel", "Remboursement de frais de transport"}))
		})
	}
	// Corrections, credit notes, credit and refunds on recent activity.
	if d.Index > 3 {
		if g.Chance(0.35) {
			w.Sched.At(d.At(11, 0), "facturation.contrepassation", func(ctx *StepContext) error { return reverseSomePayment(ctx, g, false) })
		}
		if g.Chance(0.2) {
			w.Sched.At(d.At(9, 30), "caisse.correction-apres-cloture", func(ctx *StepContext) error { return reverseSomePayment(ctx, g, true) })
		}
		if g.Chance(0.45) {
			w.Sched.At(d.At(15, 0), "facturation.avoir", func(ctx *StepContext) error { return creditNote(ctx, g) })
		}
		if g.Chance(0.3) && d.Date.Weekday() >= time.Monday && d.Date.Weekday() <= time.Friday {
			w.Sched.At(d.At(10, 30), "facturation.remboursement", func(ctx *StepContext) error { return refund(ctx, g) })
		}
	}
}

func openCash(ctx *StepContext, reg cash.Register, g *RNG) error {
	w := ctx.W
	if _, ok := w.Finance.sessions[reg.Code]; ok {
		return nil
	}
	cashier := w.cashierFor(reg.Code, ctx.Sim)
	float := int64(map[string]int{"CAISSE-PRINC": 50000, "CAISSE-URG": 30000, "CAISSE-MAT": 20000}[reg.Code])
	s, err := w.Svc.Cash.Open(cash.OpenRequest{CashRegisterID: reg.ID, OpeningFloat: float, Note: "Ouverture " + ctx.Sim.Format("02/01"), IdempotencyKey: fmt.Sprintf("dc-open-%s-%s", reg.Code, ctx.Sim.Format("20060102"))}, cashier.UserID)
	if err != nil {
		return err
	}
	w.Finance.sessions[reg.Code] = &openSession{ID: s.Session.ID, Cashier: cashier}
	return nil
}

func closeCash(ctx *StepContext, reg cash.Register, g *RNG) error {
	w := ctx.W
	s, ok := w.Finance.sessions[reg.Code]
	if !ok {
		return nil
	}
	sum, err := w.Svc.Cash.Get(s.ID)
	if err != nil {
		return err
	}
	counted := sum.ExpectedCash
	note := "Clôture sans écart"
	if g.Chance(0.07) { // shortage or surplus
		delta := int64(g.Between(1, 10)) * 500
		if g.Chance(0.6) {
			delta = -delta
		}
		counted += delta
		note = "Écart constaté au comptage"
	}
	if counted < 0 {
		counted = 0
	}
	if _, err := w.Svc.Cash.Close(s.ID, cash.CloseRequest{CountedCashAmount: counted, Note: note, IdempotencyKey: fmt.Sprintf("dc-close-%d", s.ID)}, s.Cashier.UserID, false); err != nil {
		return err
	}
	delete(w.Finance.sessions, reg.Code)
	return nil
}

func movement(ctx *StepContext, regCode, dir, typ string, amount int64, reason string) error {
	w := ctx.W
	s, ok := w.Finance.sessions[regCode]
	if !ok {
		return nil
	}
	if dir == cash.MovementOut {
		sum, err := w.Svc.Cash.Get(s.ID)
		if err != nil {
			return err
		}
		if sum.ExpectedCash < amount {
			return nil
		}
	}
	_, err := w.Svc.Cash.CreateMovement(s.ID, cash.MovementRequest{Direction: dir, Type: typ, Amount: amount, Reason: reason, IdempotencyKey: fmt.Sprintf("dc-mvt-%d-%d", s.ID, ctx.Sim.Unix())}, s.Cashier.UserID)
	return err
}

// ---------------------------------------------------------------------------------------
// Billing.

// billAfter schedules (debounced) the billing of every not-yet-billed act of the patient.
func billAfter(ctx *StepContext, p *Profile, at time.Time, service string, g *RNG) {
	w := ctx.W
	w.Finance.init()
	if next, ok := w.Finance.billQueued[p.PatientID]; ok && !at.Before(next.Add(-20*time.Minute)) && at.Before(next.Add(2*time.Hour)) {
		return
	}
	w.Finance.billQueued[p.PatientID] = at
	w.Sched.At(at, "facturation.facture", func(ctx *StepContext) error {
		delete(ctx.W.Finance.billQueued, p.PatientID)
		return invoicePatient(ctx, p, service, g)
	})
}

func consultTariff(acts []billing.Tariff, service string) uint {
	want := "TAR-CS-SPE"
	switch service {
	case "URG":
		want = "TAR-CS-URG"
	case "GEN", "PED":
		want = "TAR-CS-GEN"
	}
	for _, t := range acts {
		if t.Code == want {
			return t.ID
		}
	}
	return acts[0].ID
}

func invoicePatient(ctx *StepContext, p *Profile, service string, g *RNG) error {
	w := ctx.W
	acts, err := w.Svc.Billing.BillableActs(p.PatientID)
	if err != nil {
		return err
	}
	sortBillableActs(acts)
	var lines []billing.InvoiceLineRequest
	var paIDs []uint
	var gross float64
	for _, a := range acts {
		if a.AlreadyBilled || len(a.EligibleTariffs) == 0 {
			continue
		}
		tid := a.EligibleTariffs[0].ID
		if len(a.EligibleTariffs) > 1 {
			tid = consultTariff(a.EligibleTariffs, service)
		}
		lines = append(lines, billing.InvoiceLineRequest{ActType: a.ActType, ReferenceID: a.ReferenceID, TariffID: tid})
		if a.ActType == authorization.ReferencePerformedAct && a.CoverageResolution == "NONE" {
			paIDs = append(paIDs, a.ReferenceID)
			for _, t := range a.EligibleTariffs {
				if t.ID == tid {
					gross += float64(t.UnitPrice) * a.Quantity
				}
			}
		}
	}
	if len(lines) == 0 {
		return nil
	}
	// A PEC awaiting decision blocks invoicing of the patient's acts: billed after the decision.
	var open int64
	w.DB.Table("insurance_authorizations").Where("patient_id = ? AND status IN ?", p.PatientID, []string{authorization.StatusDraft, authorization.StatusSubmitted, authorization.StatusPending}).Count(&open)
	if open > 0 {
		return nil
	}
	if p.Insured(ctx.Sim) && len(paIDs) > 0 {
		pending, err := requestCoverage(ctx, p, paIDs, gross, service, g)
		if err != nil {
			w.Stats.fail("assurance.pec", err)
			return nil
		}
		w.Stats.ok("assurance.pec")
		if pending {
			return nil // invoicing is refused while a PEC decision is pending: billed after the decision
		}
	}
	agent := w.memberIn("FACTURATION", "FACT", g)
	inv, err := w.Svc.Billing.CreateInvoice(billing.CreateInvoiceRequest{PatientID: p.PatientID, Lines: lines}, agent.UserID)
	if err != nil {
		return err
	}
	w.Finance.invoices++
	if g.Chance(0.025) { // left as draft (e.g. waiting for a missing act)
		return nil
	}
	if inv, err = w.Svc.Billing.Issue(inv.ID, agent.UserID); err != nil {
		return err
	}
	if g.Chance(0.02) {
		w.Sched.At(ctx.Sim.Add(time.Duration(g.Between(10, 240))*time.Minute), "facturation.annulation", func(ctx *StepContext) error {
			_, err := w.Svc.Billing.Cancel(inv.ID, Pick(g, []string{"Erreur de saisie", "Acte facturé en double", "Prise en charge à refaire"}), agent.UserID)
			return err
		})
		return nil
	}
	// Customer credit available (credit note): apply it first.
	if applied := applyCredit(ctx, p, inv, g); applied {
		if inv, err = w.Svc.Billing.GetInvoice(inv.ID); err != nil {
			return err
		}
	}
	if inv.BalanceAmount <= 0 {
		return nil
	}
	planPayments(ctx, p, inv.ID, inv.BalanceAmount, service, g)
	return nil
}

// sortBillableActs makes invoice and PEC line allocation independent from PostgreSQL's
// unspecified row order and from real-time microsecond offsets preserved by the timewarp.
// CreateInvoice consumes an authorization ceiling in request-line order, so use only the
// stable business identity of each occurrence for the total order.
func sortBillableActs(acts []billing.BillableAct) {
	sort.Slice(acts, func(i, j int) bool {
		if acts[i].ActType != acts[j].ActType {
			return acts[i].ActType < acts[j].ActType
		}
		if acts[i].ReferenceID != acts[j].ReferenceID {
			return acts[i].ReferenceID < acts[j].ReferenceID
		}
		return acts[i].BillableKey < acts[j].BillableKey
	})
}

// requestCoverage: PEC (insurance authorization) for the performed acts, requested by the
// physician of the service, decision recorded as received from the insurer.
func requestCoverage(ctx *StepContext, p *Profile, paIDs []uint, gross float64, service string, g *RNG) (bool, error) {
	w := ctx.W
	docs := w.Clinic.byService[service]
	if len(docs) == 0 {
		docs = w.Clinic.byService["GEN"]
	}
	doc := docs[g.Intn(len(docs))]
	var covered []authorization.ActRequest
	for _, id := range paIDs[1:] {
		covered = append(covered, authorization.ActRequest{ReferenceType: authorization.ReferencePerformedAct, ReferenceID: id})
	}
	amount := gross
	auth, err := w.Svc.Auth.Create(authorization.CreateRequest{PatientID: p.PatientID, PatientCoverageID: p.CoverageID, ReferenceType: authorization.ReferencePerformedAct, ReferenceID: paIDs[0],
		RequestedAmount: &amount, Comment: "Demande de prise en charge", CoveredActs: covered}, doc.UserID)
	if err != nil {
		return false, err
	}
	// Submission, decision and cancellation are service-scoped: done by a member of the
	// PEC's service holding the permission (the gestionnaire/admin otherwise).
	a := doc.AuthAccess()
	if auth.ServiceID != nil {
		if m := serviceMemberWith(w, *auth.ServiceID, "insurance.authorization.submit", g); m != nil {
			doc, a = m, m.AuthAccess()
		} else {
			doc, a = w.Staff.Admin, w.Staff.Admin.AuthAccess()
		}
	}
	if g.Chance(0.03) { // drafted, never sent: abandoned (cancelled) a few days later
		if ctx.Sim.After(w.Cfg.Anchor.AddDate(0, 0, -10)) {
			return true, nil // recent draft, still to be sent
		}
		w.Sched.At(ctx.Sim.AddDate(0, 0, g.Between(3, 10)), "assurance.pec-abandonnee", func(ctx *StepContext) error {
			if _, err := w.Svc.Auth.Cancel(auth.ID, doc.UserID, a); err != nil {
				return err
			}
			billAfter(ctx, p, ctx.Sim.Add(30*time.Minute), service, g)
			return nil
		})
		return true, nil
	}
	if _, err := w.Svc.Auth.Submit(auth.ID, authorization.SubmitRequest{ExternalReference: fmt.Sprintf("ASS-%06d", auth.ID), SubmittedAt: ctx.Sim.Format("2006-01-02")}, doc.UserID, a); err != nil {
		return false, err
	}
	if g.Chance(0.04) { // cancelled after submission
		_, err := w.Svc.Auth.Cancel(auth.ID, doc.UserID, a)
		return false, err
	}
	decide := func(ctx *StepContext) error {
		rate := p.Insurance.Rate
		req := authorization.DecisionRequest{ExternalReference: fmt.Sprintf("DEC-%06d", auth.ID), ExternalDecisionDate: ctx.Sim.Format("2006-01-02")}
		switch r := g.Float(); {
		case r < 0.72:
			req.Status, req.ApprovedRate = authorization.StatusApproved, &rate
		case r < 0.88:
			partial := rate - 20
			if partial < 30 {
				partial = 30
			}
			ceiling := gross * 0.5
			req.Status, req.ApprovedRate, req.CeilingAmount = authorization.StatusPartiallyApproved, &partial, &ceiling
		default:
			req.Status, req.RejectionReason = authorization.StatusRejected, Pick(g, []string{"Acte non couvert par le contrat", "Plafond annuel atteint", "Pièces justificatives manquantes"})
		}
		_, err := w.Svc.Auth.Decide(auth.ID, req, doc.UserID, a)
		return err
	}
	if g.Chance(0.75) {
		return false, decide(ctx)
	}
	// Pending: marked as being processed by the insurer, decided days later (the invoice is
	// issued meanwhile with the insurer share pending).
	if g.Chance(0.5) {
		if _, err := w.Svc.Auth.MarkPending(auth.ID, doc.UserID, a); err != nil {
			return true, err
		}
	}
	w.Sched.At(ctx.Sim.AddDate(0, 0, g.Between(1, 9)), "assurance.decision-differee", func(ctx *StepContext) error {
		if err := decide(ctx); err != nil {
			return err
		}
		billAfter(ctx, p, ctx.Sim.Add(time.Duration(g.Between(10, 240))*time.Minute), service, g)
		return nil
	})
	return true, nil
}

// planPayments: full now, partial + rest later, sessionless transfer/check, or unpaid.
func planPayments(ctx *StepContext, p *Profile, invoiceID uint, balance int64, service string, g *RNG) {
	w := ctx.W
	reg := registerFor[service]
	if reg == "" {
		reg = "CAISSE-PRINC"
	}
	switch r := g.Float(); {
	case r < 0.66:
		pay(ctx, p, invoiceID, balance, reg, ctx.Sim.Add(time.Duration(g.Between(2, 30))*time.Minute), g)
	case r < 0.76:
		part := balance * int64(g.Between(30, 70)) / 100
		pay(ctx, p, invoiceID, part, reg, ctx.Sim.Add(time.Duration(g.Between(2, 30))*time.Minute), g)
		if g.Chance(0.7) {
			pay(ctx, p, invoiceID, balance-part, reg, ctx.Sim.AddDate(0, 0, g.Between(2, 25)).Add(time.Duration(g.Between(-3, 3))*time.Hour), g)
		}
	case r < 0.88:
		pay(ctx, p, invoiceID, balance, reg, ctx.Sim.AddDate(0, 0, g.Between(1, 12)).Add(time.Duration(g.Between(-2, 4))*time.Hour), g) // pays on a later visit
	case r < 0.92:
		at := ctx.Sim.AddDate(0, 0, g.Between(2, 15))
		w.Sched.At(at, "facturation.paiement-hors-caisse", func(ctx *StepContext) error {
			method := Pick(g, []string{"BANK_TRANSFER", "CHECK"})
			_, err := w.Svc.Billing.Pay(invoiceID, billing.PaymentRequest{Amount: balance, PaymentMethod: method, Reference: fmt.Sprintf("VIR-%d", invoiceID),
				IdempotencyKey: fmt.Sprintf("dc-bpay-%d", invoiceID), Payer: &billing.PayerRequest{Mode: "ORGANIZATION", DisplayName: Pick(g, employers)}}, w.memberIn("FACTURATION", "FACT", g).UserID)
			return err
		})
	default:
		// Unpaid: patient receivable (follow-ups by accounting, see receivables.go).
	}
}

var employers = []string{"Groupe industriel Lagunaire", "Mairie de Cocody", "Société de transport Abidjanaise", "Clinique partenaire", "Ambassade (prise en charge)"}

func pay(ctx *StepContext, p *Profile, invoiceID uint, amount int64, reg string, at time.Time, g *RNG) {
	w := ctx.W
	if amount <= 0 {
		return
	}
	method := Pick(g, []string{"CASH", "CASH", "CASH", "CASH", "MOBILE_MONEY", "MOBILE_MONEY", "CARD"})
	payer := cash.PayerRequest{Mode: "PATIENT"}
	switch r := g.Float(); {
	case r < 0.1 || p.AgeAt(at) < 16:
		rel := newIdentity(g, "")
		payer = cash.PayerRequest{Mode: "INDIVIDUAL", DisplayName: rel.Full(), Phone: phone("07", 40000000+int(invoiceID)), Relationship: Pick(g, []string{"Mère", "Père", "Conjoint", "Fils", "Fille"})}
	case r < 0.13:
		payer = cash.PayerRequest{Mode: "ORGANIZATION", DisplayName: Pick(g, employers)}
	}
	var try func(ctx *StepContext) error
	attempts := 0
	try = func(ctx *StepContext) error {
		s, ok := w.Finance.sessions[reg]
		if !ok { // desk closed: the patient pays when it reopens
			if attempts++; attempts > 3 {
				return nil
			}
			next := ctx.Sim.Truncate(24*time.Hour).AddDate(0, 0, 1).Add(time.Duration(g.Between(8*60, 11*60)) * time.Minute)
			w.Sched.At(next, "caisse.encaissement", try)
			return nil
		}
		req := cash.PaymentRequest{InvoiceID: invoiceID, Amount: amount, PaymentMethod: method, IdempotencyKey: fmt.Sprintf("dc-pay-%d-%d", invoiceID, at.Unix()), Payer: &payer}
		if method == "MOBILE_MONEY" {
			req.MobileOperator, req.ExternalReference = Pick(g, []string{"ORANGE", "MTN", "MOOV", "WAVE"}), fmt.Sprintf("MM%010d", invoiceID*7+uint(attempts))
		}
		if method == "CARD" {
			req.ExternalReference = fmt.Sprintf("TPE-%08d", invoiceID)
		}
		rcpt, err := w.Svc.Cash.Pay(s.ID, req, s.Cashier.UserID)
		if err != nil {
			if strings.Contains(err.Error(), "solde") || strings.Contains(strings.ToLower(err.Error()), "montant") {
				return nil // already settled meanwhile (credit, other payment)
			}
			return err
		}
		w.Finance.paid = append(w.Finance.paid, paidRef{InvoiceID: invoiceID, PaymentID: rcpt.PaymentID, Patient: p, Amount: amount, Method: method, At: ctx.Sim, Register: reg})
		if len(w.Finance.paid) > 400 {
			w.Finance.paid = w.Finance.paid[len(w.Finance.paid)-400:]
		}
		return nil
	}
	w.Sched.At(at, "caisse.encaissement", try)
}

// ---------------------------------------------------------------------------------------
// Reversals and post-close corrections.

func reverseSomePayment(ctx *StepContext, g *RNG, postClose bool) error {
	w := ctx.W
	acct := w.memberIn("COMPTABLE", "COMPTA", g)
	for try := 0; try < 8 && len(w.Finance.paid) > 0; try++ {
		i := g.Intn(len(w.Finance.paid))
		ref := w.Finance.paid[i]
		age := ctx.Sim.Sub(ref.At)
		// Only CASH collected in a cash session can be reversed (PAYMENT_REVERSAL_SESSION_METHOD_UNSUPPORTED).
		if ref.Method != "CASH" || postClose != (age > 20*time.Hour) || age > 20*24*time.Hour {
			continue
		}
		w.Finance.paid = append(w.Finance.paid[:i], w.Finance.paid[i+1:]...)
		if _, err := w.Svc.Billing.ReversePayment(ref.PaymentID, billing.ReversePaymentRequest{Reason: Pick(g, []string{"Erreur de montant", "Paiement affecté au mauvais patient", "Double encaissement"}), IdempotencyKey: fmt.Sprintf("dc-rev-%d", ref.PaymentID)}, acct.UserID); err != nil {
			w.Stats.ok("refus-attendu.contrepassation-non-eligible") // e.g. customer credit issued since
			continue
		}
		if !postClose {
			return nil
		}
		var rev billing.PaymentReversal
		if err := w.DB.Where("original_payment_id = ?", ref.PaymentID).First(&rev).Error; err != nil {
			return err
		}
		host, ok := w.Finance.sessions[ref.Register]
		if !ok {
			return nil // reversal recorded; the physical correction waits for an open session
		}
		el, err := w.Svc.Cash.CorrectionEligibilityForPayment(ref.PaymentID)
		if err != nil || !el.Eligible {
			return err
		}
		director := w.Staff.Tagged("refund-approver-2")
		if director == nil {
			director = w.Staff.Admin
		}
		_, err = w.Svc.Cash.ExecutePostCloseCorrection(cash.ExecuteCorrectionRequest{PaymentReversalID: rev.ID, HostSessionID: &host.ID, Note: "Restitution des espèces après contrepassation", IdempotencyKey: fmt.Sprintf("dc-pce-%d", rev.ID)}, director.UserID)
		return err
	}
	return nil
}

// ---------------------------------------------------------------------------------------
// Credit notes, credit applications, refunds.

func creditNote(ctx *StepContext, g *RNG) error {
	w := ctx.W
	acct := w.memberIn("COMPTABLE", "COMPTA", g)
	for try := 0; try < 6 && len(w.Finance.paid) > 0; try++ {
		ref := w.Finance.paid[g.Intn(len(w.Finance.paid))]
		inv, err := w.Svc.Billing.GetInvoice(ref.InvoiceID)
		// Invoices with an insurer share require the insurance correction path (LOT30): only
		// patient-only invoices receive a commercial credit note.
		if err != nil || inv.Status != billing.InvoicePaid || inv.CreditedAmount > 0 || inv.InsuranceAmount > 0 {
			continue
		}
		amount := inv.PatientAmount
		if g.Chance(0.6) {
			amount = inv.PatientAmount * int64(g.Between(20, 80)) / 100
		}
		if amount <= 0 {
			continue
		}
		if _, err := w.Svc.Billing.IssueCreditNote(inv.ID, billing.CreditNoteRequest{Amount: amount, Reason: Pick(g, []string{"Acte non réalisé", "Geste commercial", "Erreur de tarification", "Prise en charge assurance obtenue après paiement"}), IdempotencyKey: fmt.Sprintf("dc-cn-%d", inv.ID)}, acct.UserID); err != nil {
			w.Stats.ok("refus-attendu.avoir-non-eligible") // e.g. several distinct payers on the invoice
			continue
		}
		w.Finance.creditors = append(w.Finance.creditors, ref.Patient)
		return nil
	}
	return nil
}

func spendable(w *World, p *Profile) (uint, int64) {
	bal, err := w.Svc.Billing.ListPatientCreditBalances(p.PatientID)
	if err != nil {
		return 0, 0
	}
	for _, b := range bal {
		if b.AvailableCredit > 0 {
			return b.HolderPartyID, b.AvailableCredit
		}
	}
	return 0, 0
}

func applyCredit(ctx *StepContext, p *Profile, inv *billing.Invoice, g *RNG) bool {
	w := ctx.W
	holder, avail := spendable(w, p)
	if holder == 0 || inv.BalanceAmount <= 0 || inv.InsuranceAmount > 0 || !g.Chance(0.75) {
		return false
	}
	amount := avail
	if amount > inv.BalanceAmount {
		amount = inv.BalanceAmount
	}
	agent := w.memberIn("FACTURATION", "FACT", g)
	res, err := w.Svc.Billing.ApplyCredit(inv.ID, billing.CreditApplicationRequest{HolderPartyID: holder, Amount: amount, Reason: "Utilisation de l'avoir", IdempotencyKey: fmt.Sprintf("dc-apply-%d", inv.ID)}, agent.UserID)
	if err != nil {
		w.Stats.fail("facturation.application-credit", err)
		return false
	}
	w.Stats.ok("facturation.application-credit")
	if g.Chance(0.1) {
		appID := res.Application.ID
		w.Sched.At(ctx.Sim.Add(time.Duration(g.Between(5, 120))*time.Minute), "facturation.annulation-application-credit", func(ctx *StepContext) error {
			_, err := w.Svc.Billing.ReverseCreditApplication(appID, billing.CreditApplicationReversalRequest{Reason: "Application sur la mauvaise facture", IdempotencyKey: fmt.Sprintf("dc-unapply-%d", appID)}, agent.UserID)
			return err
		})
	}
	return true
}

var refundReasons = []string{billing.RefundReasonDuplicateOrOverpayment, billing.RefundReasonServiceCancelledOrNotPerf, billing.RefundReasonInvoiceCorrection, billing.RefundReasonUnusedAdvance, billing.RefundReasonInsuranceAfterPayment}

func refund(ctx *StepContext, g *RNG) error {
	w := ctx.W
	requester, approver, executor := w.Staff.Tagged("refund-requester"), w.Staff.Tagged("refund-approver"), w.Staff.Tagged("refund-executor")
	if requester == nil || approver == nil || executor == nil {
		return errors.New("comptes de séparation des tâches absents")
	}
	for try := 0; try < len(w.Finance.creditors); try++ {
		p := w.Finance.creditors[g.Intn(len(w.Finance.creditors))]
		holder, avail := spendable(w, p)
		if holder == 0 {
			continue
		}
		w.Finance.refundSeq++
		seq := w.Finance.refundSeq
		amount := avail
		if g.Chance(0.3) {
			amount = avail / 2
		}
		method := Pick(g, []string{"CASH", "CASH", "MOBILE_MONEY", "TRANSFER"})
		var kind string
		w.DB.Table("billing_financial_parties").Select("kind").Where("id = ?", holder).Scan(&kind)
		if kind == "ORGANIZATION" {
			method = "TRANSFER" // organisation refunds: cash not allowed
		}
		reason := Pick(g, refundReasons)
		if g.Chance(0.08) {
			reason = billing.RefundReasonOther
		}
		req := billing.RefundRequest{PatientID: p.PatientID, HolderPartyID: holder, Amount: amount, ReasonCode: reason, ReasonComment: "Demande du patient au guichet",
			BeneficiaryMode: billing.RefundBeneficiaryHolder, IntendedMethod: method, IdempotencyKey: fmt.Sprintf("dc-refund-%d", seq)}
		if reason == billing.RefundReasonServiceCancelledOrNotPerf {
			req.ClinicalAttestationRef = fmt.Sprintf("ATT-NR-%05d", seq) // clinical attestation of non-performance
		}
		r, err := w.Svc.Billing.RequestRefund(req, requester.UserID)
		if err != nil {
			return err
		}
		// Separation of duties: the requester attempting to approve its own request is refused.
		if seq%7 == 1 {
			if _, err := w.Svc.Billing.ApproveRefund(r.ID, billing.RefundDecisionRequest{Reason: "Auto-validation", IdempotencyKey: fmt.Sprintf("dc-refund-self-%d", seq)}, requester.UserID); err == nil {
				return errors.New("séparation des tâches non appliquée : le demandeur a validé sa propre demande")
			}
			w.Stats.ok("refus-attendu.remboursement-auto-validation")
		}
		decision := billing.RefundDecisionRequest{Reason: "Contrôle effectué", IdempotencyKey: fmt.Sprintf("dc-refund-dec-%d", seq), ManagerialApproval: reason == billing.RefundReasonOther}
		if g.Chance(0.08) || ctx.Sim.After(w.Cfg.Anchor.AddDate(0, 0, -3)) {
			return nil // awaiting the approver's decision (REQUESTED)
		}
		switch x := g.Float(); {
		case x < 0.12:
			w.Sched.At(ctx.Sim.Add(time.Duration(g.Between(30, 300))*time.Minute), "facturation.remboursement-rejet", func(ctx *StepContext) error {
				decision.Reason = "Justificatif insuffisant"
				_, err := w.Svc.Billing.RejectRefund(r.ID, decision, approver.UserID)
				return err
			})
			return nil
		case x < 0.2:
			w.Sched.At(ctx.Sim.Add(time.Duration(g.Between(10, 120))*time.Minute), "facturation.remboursement-annulation", func(ctx *StepContext) error {
				decision.Reason = "Patient a finalement conservé l'avoir"
				_, err := w.Svc.Billing.CancelRefund(r.ID, decision, requester.UserID)
				return err
			})
			return nil
		}
		approveAt := ctx.Sim.Add(time.Duration(g.Between(30, 240)) * time.Minute)
		w.Sched.At(approveAt, "facturation.remboursement-autorisation", func(ctx *StepContext) error {
			if _, err := w.Svc.Billing.ApproveRefund(r.ID, decision, approver.UserID); err != nil {
				return err
			}
			if g.Chance(0.15) { // approved, not yet executed
				return nil
			}
			execAt := ctx.Sim.Add(time.Duration(g.Between(20, 180)) * time.Minute)
			if execAt.Hour() >= 19 {
				execAt = execAt.Add(14 * time.Hour)
			}
			attempts := 0
			var execute func(ctx *StepContext) error
			execute = func(ctx *StepContext) error {
				// The approver attempting to execute is refused (separation of duties).
				if seq%9 == 2 && attempts == 0 {
					if _, err := w.Svc.Billing.ExecuteRefund(r.ID, billing.RefundExecuteRequest{Method: method, IdempotencyKey: fmt.Sprintf("dc-refund-exec-self-%d", seq)}, approver.UserID); err == nil {
						return errors.New("séparation des tâches non appliquée : l'autorisateur a exécuté le remboursement")
					}
					w.Stats.ok("refus-attendu.remboursement-auto-execution")
				}
				req := billing.RefundExecuteRequest{Method: method, IdempotencyKey: fmt.Sprintf("dc-refund-exec-%d", seq)}
				if method == "CASH" {
					// The authorised method is binding: wait for the executor's own open session
					// with enough cash rather than changing the method.
					s, ok := w.Finance.sessions["CAISSE-PRINC"]
					enough := false
					if ok && s.Cashier.UserID == executor.UserID {
						if sum, err := w.Svc.Cash.Get(s.ID); err == nil && sum.ExpectedCash >= r.Amount {
							enough = true
						}
					}
					if !enough {
						if attempts++; attempts <= 4 {
							w.Sched.At(ctx.Sim.Truncate(24*time.Hour).AddDate(0, 0, 1).Add(time.Duration(g.Between(9*60, 16*60))*time.Minute), "facturation.remboursement-execution", execute)
						}
						return nil
					}
				} else {
					req.ExternalReference = fmt.Sprintf("RMB-EXT-%08d", r.ID)
					req.BeneficiaryRailRef = phone("07", 50000000+int(r.ID)) // fictitious mobile wallet
					if method == "TRANSFER" {
						req.BeneficiaryRailRef = fmt.Sprintf("CI93 0000 0000 %012d", r.ID)
					}
				}
				_, err := w.Svc.Billing.ExecuteRefund(r.ID, req, executor.UserID)
				return err
			}
			w.Sched.At(execAt, "facturation.remboursement-execution", execute)
			return nil
		})
		return nil
	}
	return nil
}

func serviceMemberWith(w *World, serviceID uint, perm string, g *RNG) *Member {
	var code string
	for c, svc := range w.Org.Services {
		if svc.ID == serviceID {
			code = c
		}
	}
	list := w.Staff.With(func(m *Member) bool { return contains(m.Services, code) && m.Has(perm) })
	if len(list) == 0 {
		return nil
	}
	return list[g.Intn(len(list))]
}
