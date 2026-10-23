package complete

import (
	"fmt"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/insurance/voucher"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance_receivables"
	"github.com/lallene/medcore-his/backend/internal/modules/receivables"
)

// Insurance back-office through the real services:
//   - PEC vouchers (bons de prise en charge) draft → submitted → controlled → validated |
//     rejected | cancelled, which writes workflow_histories (the insurance dashboard source).
//     No staff function holds insurance.voucher.* in the RBAC matrix: the workflow is run by
//     the administrator account (reported as a product finding).
//   - insurer receivables: monthly submission batches (DRAFT → SUBMITTED), settlements
//     (received → allocated → POSTED, or CANCELLED), follow-ups and due dates.
//   - patient receivables: due dates, reminders, payment promises.

type insuranceState struct{}

type insurancePlanner struct{}

func (insurancePlanner) Name() string         { return "Assurance / créances" }
func (insurancePlanner) Setup(w *World) error { return nil }

// voucherFor is called after an insured consultation (see afterConsultation).
func voucherFor(ctx *StepContext, v *Visit) {
	w := ctx.W
	g := v.g
	if !v.P.Insured(ctx.Sim) || !g.Chance(0.45) {
		return
	}
	admin := w.Staff.Admin
	w.Sched.At(ctx.Sim.Add(time.Duration(g.Between(30, 240))*time.Minute), "assurance.bon-pec", func(ctx *StepContext) error {
		cid := v.ConsultID
		gross := float64(map[bool]int{true: 20000, false: 15000}[v.Service == "URG"])
		vc, err := w.Svc.Voucher.Create(voucher.CreateVoucherRequest{CoverageID: v.P.CoverageID, ConsultationID: &cid, IssueDate: ctx.Sim.Format("2006-01-02"), GrossAmount: gross, Notes: "Bon de prise en charge consultation"})
		if err != nil {
			return err
		}
		actor := admin.WorkflowActor()
		step := func(at time.Time, action, reason string, next func(ctx *StepContext) error) {
			w.Sched.At(at, "assurance.bon-pec-workflow", func(ctx *StepContext) error {
				if _, err := w.Svc.Voucher.ApplyWorkflow(vc.ID, voucher.WorkflowActionRequest{Action: action, Reason: reason}, actor); err != nil {
					return err
				}
				if next != nil {
					return next(ctx)
				}
				return nil
			})
		}
		if g.Chance(0.06) {
			step(ctx.Sim.Add(time.Duration(g.Between(1, 48))*time.Hour), "cancel", "Bon émis par erreur", nil)
			return nil
		}
		step(ctx.Sim.Add(time.Duration(g.Between(1, 30))*time.Hour), "submit", "", func(ctx *StepContext) error {
			if g.Chance(0.08) {
				step(ctx.Sim.AddDate(0, 0, g.Between(1, 6)), "reject", Pick(g, []string{"Assuré non à jour de cotisation", "Acte hors garantie"}), nil)
				return nil
			}
			step(ctx.Sim.AddDate(0, 0, g.Between(1, 5)), "control", "", func(ctx *StepContext) error {
				switch r := g.Float(); {
				case r < 0.08:
					step(ctx.Sim.AddDate(0, 0, g.Between(1, 4)), "reject", "Pièces non conformes au contrôle", nil)
				case r < 0.12:
					step(ctx.Sim.AddDate(0, 0, g.Between(1, 4)), "cancel", "Annulé après contrôle", nil)
				default:
					step(ctx.Sim.AddDate(0, 0, g.Between(1, 6)), "validate", "", nil)
				}
				return nil
			})
			return nil
		})
		return nil
	})
}

func (insurancePlanner) PlanDay(w *World, d Day) {
	g := w.RNG.Stream("insurance.day", uint64(d.Index))
	// Monthly submission batches (3rd of the month) for the previous month's insurer shares.
	if d.Date.Day() == 3 && d.Index > 20 {
		from := d.Date.AddDate(0, -1, 0)
		to := d.Date.AddDate(0, 0, -3)
		w.Sched.At(d.At(10, 0), "assurance.bordereaux", func(ctx *StepContext) error { return submissionBatches(ctx, from, to, g) })
	}
	// Settlements (around the 20th), follow-ups (Tuesdays), due dates.
	if d.Date.Day() == 20 && d.Index > 45 {
		w.Sched.At(d.At(11, 0), "assurance.reglements", func(ctx *StepContext) error { return settlements(ctx, g) })
	}
	if d.Date.Weekday() == time.Tuesday && d.Index > 30 {
		w.Sched.At(d.At(14, 0), "assurance.relances", func(ctx *StepContext) error { return insurerFollowUps(ctx, g) })
	}
	if d.Date.Weekday() == time.Wednesday && d.Index > 14 {
		w.Sched.At(d.At(15, 0), "creances.relances-patients", func(ctx *StepContext) error { return patientFollowUps(ctx, g) })
	}
}

func openInsurerLines(w *World, companyID uint) ([]insurance_receivables.Item, error) {
	var out []insurance_receivables.Item
	for page := 1; page <= 20; page++ {
		p, err := w.Svc.InsRec.List(insurance_receivables.Filter{CompanyID: companyID, Page: page, Limit: 100})
		if err != nil {
			return nil, err
		}
		out = append(out, p.Items...)
		if len(p.Items) < 100 {
			break
		}
	}
	return out, nil
}

func submissionBatches(ctx *StepContext, from, to time.Time, g *RNG) error {
	w := ctx.W
	agent := w.memberIn("FACTURATION", "FACT", g)
	for _, c := range w.Ref.Companies {
		lines, err := openInsurerLines(w, c.ID)
		if err != nil {
			return err
		}
		var ids []uint
		for _, l := range lines {
			if l.BatchNumber == "" && l.InsuranceBalance > 0 && !l.InvoiceDate.After(to.AddDate(0, 0, 1)) {
				ids = append(ids, l.InvoiceLineID)
			}
		}
		if len(ids) == 0 {
			continue
		}
		pf, pt := from.Format("2006-01-02"), to.Format("2006-01-02")
		b, err := w.Svc.InsRec.CreateBatch(insurance_receivables.BatchRequest{InsuranceCompanyID: c.ID, PeriodFrom: &pf, PeriodTo: &pt, ExternalReference: fmt.Sprintf("BRD-%s-%s", c.Code, from.Format("200601")), Comment: "Bordereau mensuel", InvoiceLineIDs: ids}, agent.UserID)
		if err != nil {
			return err
		}
		if g.Chance(0.5) || ctx.Sim.Before(w.Cfg.Anchor.AddDate(0, 0, -25)) { // latest month: some batches still being prepared
			if _, err := w.Svc.InsRec.SubmitBatch(b.ID, agent.UserID); err != nil {
				return err
			}
		}
	}
	return nil
}

func settlements(ctx *StepContext, g *RNG) error {
	w := ctx.W
	acct := w.memberIn("COMPTABLE", "COMPTA", g)
	for _, c := range w.Ref.Companies {
		lines, err := openInsurerLines(w, c.ID)
		if err != nil {
			return err
		}
		var pick []insurance_receivables.Item
		var total int64
		for _, l := range lines {
			if l.BatchNumber != "" && l.InsuranceBalance > 0 && l.InvoiceDate.Before(ctx.Sim.AddDate(0, 0, -25)) && g.Chance(0.8) {
				amount := l.InsuranceBalance
				if g.Chance(0.15) {
					amount = amount * int64(g.Between(40, 90)) / 100 // partial payment by the insurer
				}
				l.InsurancePaid = amount // reused as the allocation amount
				pick = append(pick, l)
				total += amount
			}
		}
		if len(pick) == 0 || total <= 0 {
			continue
		}
		s, err := w.Svc.InsRec.CreateSettlement(insurance_receivables.SettlementRequest{InsuranceCompanyID: c.ID, ExternalReference: fmt.Sprintf("REG-%s-%s", c.Code, ctx.Sim.Format("200601")),
			ReceivedAt: ctx.Sim.Format("2006-01-02"), TotalAmount: total, PaymentMethod: "BANK_TRANSFER", BankReference: fmt.Sprintf("VIR%s%04d", ctx.Sim.Format("0601"), c.ID), Comment: "Règlement assureur", IdempotencyKey: fmt.Sprintf("dc-settle-%d-%s", c.ID, ctx.Sim.Format("200601"))}, acct.UserID)
		if err != nil {
			return err
		}
		if g.Chance(0.06) || (c.ID == w.Ref.Companies[0].ID && ctx.Sim.Month()%3 == 0) { // received by mistake (wrong payer): cancelled before allocation
			_, err := w.Svc.InsRec.Cancel(s.ID, acct.UserID)
			return err
		}
		for _, l := range pick {
			if _, err := w.Svc.InsRec.Allocate(s.ID, insurance_receivables.AllocationRequest{InvoiceLineID: l.InvoiceLineID, Amount: l.InsurancePaid}, acct.UserID); err != nil {
				return err
			}
		}
		if g.Chance(0.9) || ctx.Sim.Before(w.Cfg.Anchor.AddDate(0, 0, -15)) {
			if _, err := w.Svc.InsRec.Post(s.ID, acct.UserID); err != nil {
				return err
			}
		}
	}
	return nil
}

func insurerFollowUps(ctx *StepContext, g *RNG) error {
	w := ctx.W
	acct := w.memberIn("COMPTABLE", "COMPTA", g)
	for _, c := range w.Ref.Companies {
		lines, err := openInsurerLines(w, c.ID)
		if err != nil {
			return err
		}
		n := 0
		for _, l := range lines {
			if l.InsuranceBalance <= 0 || !l.InvoiceDate.Before(ctx.Sim.AddDate(0, 0, -40)) || !g.Chance(0.25) {
				continue
			}
			if _, err := w.Svc.InsRec.AddFollowUp(l.InvoiceLineID, insurance_receivables.FollowUpRequest{Type: Pick(g, []string{"EMAIL", "PHONE_CALL", "LETTER", "NOTE"}), Note: "Relance du règlement auprès de l'assureur"}, acct.UserID); err != nil {
				return err
			}
			if g.Chance(0.3) {
				due := ctx.Sim.AddDate(0, 0, g.Between(10, 45)).Format("2006-01-02")
				if _, err := w.Svc.InsRec.SetDue(l.InvoiceLineID, insurance_receivables.DueDateRequest{DueDate: &due, Note: "Échéance convenue avec l'assureur"}, acct.UserID); err != nil {
					return err
				}
			}
			if n++; n >= 6 {
				break
			}
		}
	}
	return nil
}

var followTypes = []string{"REMINDER", "PHONE_CALL", "NOTE", "PAYMENT_PROMISE"}

func patientFollowUps(ctx *StepContext, g *RNG) error {
	w := ctx.W
	acct := w.memberIn("COMPTABLE", "COMPTA", g)
	page, err := w.Svc.Receivables.List(receivables.Filter{Page: 1 + g.Intn(3), Limit: 50})
	if err != nil {
		return err
	}
	n := 0
	for _, it := range page.Items {
		if it.PatientBalance <= 0 || !g.Chance(0.35) {
			continue
		}
		typ := Pick(g, followTypes)
		req := receivables.FollowUpRequest{ActionType: typ, Note: map[string]string{"REMINDER": "SMS de rappel envoyé par l'accueil", "PHONE_CALL": "Appel au patient", "NOTE": "Patient informé lors de sa visite", "PAYMENT_PROMISE": "Le patient s'engage à régler"}[typ]}
		if typ == "PAYMENT_PROMISE" {
			date := ctx.Sim.AddDate(0, 0, g.Between(3, 20)).Format("2006-01-02")
			amount := it.PatientBalance
			req.PromisedPaymentDate, req.PromisedAmount = &date, &amount
		}
		if _, err := w.Svc.Receivables.AddFollowUp(it.InvoiceID, req, acct.UserID); err != nil {
			return err
		}
		if g.Chance(0.35) {
			due := ctx.Sim.AddDate(0, 0, g.Between(-20, 30)).Format("2006-01-02")
			if _, err := w.Svc.Receivables.SetDueDate(it.InvoiceID, receivables.DueDateRequest{DueDate: &due}, acct.UserID); err != nil {
				return err
			}
		}
		if n++; n >= 8 {
			break
		}
	}
	return nil
}
