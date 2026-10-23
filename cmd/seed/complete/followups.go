package complete

import (
	"fmt"
	"strings"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/hospitalizations"
	"github.com/lallene/medcore-his/backend/internal/modules/imaging"
	"github.com/lallene/medcore-his/backend/internal/modules/laboratory"
	"github.com/lallene/medcore-his/backend/internal/modules/performed_acts"
	"github.com/lallene/medcore-his/backend/internal/modules/pharmacy"
)

// afterConsultation schedules what follows a completed consultation, each through the
// executing module's service: laboratory, imaging, pharmacy, hospitalization, billing.
func afterConsultation(ctx *StepContext, v *Visit) {
	w := ctx.W
	g := v.g
	w.Sched.At(ctx.Sim.Add(time.Duration(g.Between(10, 50))*time.Minute), "laboratoire.reception-demandes", func(ctx *StepContext) error { return labOrders(ctx, v) })
	w.Sched.At(ctx.Sim.Add(time.Duration(g.Between(15, 70))*time.Minute), "imagerie.reception-demandes", func(ctx *StepContext) error { return imagingOrders(ctx, v) })
	w.Sched.At(ctx.Sim.Add(time.Duration(g.Between(20, 90))*time.Minute), "pharmacie.dispensation", func(ctx *StepContext) error { return dispense(ctx, v) })
	// A quarter of recommended stays are not admitted right away: they join the programming
	// backlog handled by surgeryPlanner (PLANNED stay, RESERVED bed, admission days later).
	if v.Hospitalize && (g.Chance(0.6) || !ctx.Sim.Before(w.Cfg.Anchor.AddDate(0, 0, -7))) {
		w.Sched.At(ctx.Sim.Add(time.Duration(g.Between(15, 60))*time.Minute), "hospitalisation.demande", func(ctx *StepContext) error { return hospitalize(ctx, v) })
	}
	voucherFor(ctx, v)
	// Billing of the consultation (and of anything already produced) at the front desk.
	billAfter(ctx, v.P, ctx.Sim.Add(time.Duration(g.Between(5, 40))*time.Minute), v.Service, g)
}

// ---------------------------------------------------------------------------------------
// Laboratory: ORDERED → SAMPLE_PENDING → SAMPLE_COLLECTED → IN_PROGRESS → RESULT_ENTERED → VALIDATED
// (CANCELLED before validation). Orders are materialised by the laboratory module itself
// from the consultation exam requests, as the API does when the laboratory lists its queue.

func (w *World) memberIn(fn, svc string, g *RNG) *Member {
	list := w.Staff.With(func(m *Member) bool {
		return contains(m.Functions, fn) && (svc == "" || contains(m.Services, svc)) && !m.Tags["restricted"]
	})
	if len(list) == 0 {
		return w.Staff.Admin
	}
	return list[g.Intn(len(list))]
}

func labOrders(ctx *StepContext, v *Visit) error {
	w := ctx.W
	g := v.g
	bio := w.memberIn("BIOLOGISTE", "LAB", g)
	if err := w.Svc.LabRepo.Materialize(bio.UserID); err != nil {
		return err
	}
	// Order ids come from the laboratory materialisation (unordered query in the module):
	// iterate on the business key and give each order its own random stream, so that the
	// dataset never depends on PostgreSQL's row order.
	var orders []laboratory.Order
	if err := w.DB.Where("consultation_id = ?", v.ConsultID).Order("medical_exam_id").Find(&orders).Error; err != nil {
		return err
	}
	for i := range orders {
		o := orders[i]
		g := v.g.Stream("lab-order", uint64(o.MedicalExamID))
		code := w.examCode(o.MedicalExamID)
		a := bio.LabAccess()
		t := ctx.Sim.Add(w.Slow(ctx.Sim, time.Duration(g.Between(0, 30))*time.Minute))
		if g.Chance(0.02) {
			continue // the patient never went to the laboratory: the order stays ORDERED
		}
		if g.Chance(0.04) {
			w.Sched.At(t.Add(time.Duration(g.Between(5, 120))*time.Minute), "laboratoire.annulation", func(ctx *StepContext) error {
				_, err := w.Svc.Lab.Cancel(o.ID, a, Pick(g, []string{"Prélèvement impossible", "Demande en double", "Patient parti"}))
				return err
			})
			continue
		}
		w.Sched.At(t, "laboratoire.prelevement-attendu", func(ctx *StepContext) error {
			if _, err := w.Svc.Lab.PrepareSample(o.ID, a); err != nil {
				return err
			}
			if g.Chance(0.01) {
				return nil // sample awaited, never brought (SAMPLE_PENDING)
			}
			stuck := g.Chance(0.012) // sample collected, analyser down: awaiting analysis
			w.Sched.At(ctx.Sim.Add(w.Slow(ctx.Sim, time.Duration(g.Between(5, 60))*time.Minute)), "laboratoire.prelevement", func(ctx *StepContext) error {
				if _, err := w.Svc.Lab.Collect(o.ID, a, laboratory.CollectRequest{SampleType: sampleType(code), Comment: "Prélèvement réalisé au laboratoire"}); err != nil {
					return err
				}
				if stuck {
					return nil
				}
				w.Sched.At(ctx.Sim.Add(w.Slow(ctx.Sim, time.Duration(g.Between(15, 150))*time.Minute)), "laboratoire.analyse", func(ctx *StepContext) error {
					if _, err := w.Svc.Lab.Start(o.ID, a); err != nil {
						return err
					}
					analysis := w.Slow(ctx.Sim, time.Duration(g.Between(20, 240))*time.Minute)
					if code == "BLOOD_CULTURE" || code == "URINE_CBU" { // cultures: results after 2-5 days
						analysis = time.Duration(g.Between(48, 120)) * time.Hour
					}
					w.Sched.At(ctx.Sim.Add(analysis), "laboratoire.resultats", func(ctx *StepContext) error {
						if _, err := w.Svc.Lab.EnterResults(o.ID, a, laboratory.EnterResultsRequest{Results: labResults(code, g)}); err != nil {
							return err
						}
						validator := w.memberIn("BIOLOGISTE", "LAB", g)
						delay := time.Duration(g.Between(10, 180)) * time.Minute
						if g.Chance(0.2) {
							delay += 18 * time.Hour // validated the next morning
						}
						w.Sched.At(ctx.Sim.Add(delay), "laboratoire.validation", func(ctx *StepContext) error {
							if _, err := w.Svc.Lab.Validate(o.ID, validator.LabAccess()); err != nil {
								return err
							}
							billAfter(ctx, v.P, ctx.Sim.Add(time.Duration(g.Between(30, 300))*time.Minute), v.Service, g)
							return nil
						})
						return nil
					})
					return nil
				})
				return nil
			})
			return nil
		})
	}
	return nil
}

func (w *World) examCode(id uint) string {
	for code, e := range w.Ref.Exams {
		if e.ID == id {
			return code
		}
	}
	return ""
}

func sampleType(code string) string {
	switch {
	case code == "URINE_CBU":
		return "Urines"
	case code == "BLOOD_CULTURE":
		return "Sang (flacons d'hémoculture)"
	case code == "MALARIA_RDT" || code == "THICK_DROP":
		return "Sang capillaire"
	}
	return "Sang veineux"
}

type labParam struct {
	name, unit     string
	lo, hi         float64
	critLo, critHi float64
	text           string
}

var labPanels = map[string][]labParam{
	"NFS":             {{"Hémoglobine", "g/dL", 12, 16, 7, 20, ""}, {"Leucocytes", "G/L", 4, 10, 1, 30, ""}, {"Plaquettes", "G/L", 150, 400, 50, 1000, ""}},
	"CBC":             {{"Hémoglobine", "g/dL", 12, 16, 7, 20, ""}, {"Leucocytes", "G/L", 4, 10, 1, 30, ""}},
	"CRP":             {{"CRP", "mg/L", 0, 6, 0, 200, ""}},
	"GLYCEMIA":        {{"Glycémie", "g/L", 0.7, 1.1, 0.4, 4, ""}},
	"FASTING_GLUCOSE": {{"Glycémie à jeun", "g/L", 0.7, 1.1, 0.4, 4, ""}},
	"HBA1C":           {{"HbA1c", "%", 4, 6, 0, 15, ""}},
	"CREATININE":      {{"Créatinine", "mg/L", 6, 12, 0, 60, ""}},
	"UREA":            {{"Urée", "g/L", 0.15, 0.45, 0, 2, ""}},
	"LIVER_PANEL":     {{"ASAT", "UI/L", 5, 40, 0, 1000, ""}, {"ALAT", "UI/L", 5, 40, 0, 1000, ""}},
	"LIPID_PANEL":     {{"Cholestérol total", "g/L", 1.5, 2, 0, 5, ""}, {"LDL", "g/L", 0.7, 1.6, 0, 4, ""}, {"Triglycérides", "g/L", 0.5, 1.5, 0, 10, ""}},
	"IONOGRAM":        {{"Sodium", "mmol/L", 136, 145, 120, 160, ""}, {"Potassium", "mmol/L", 3.5, 5, 2.5, 6.5, ""}},
	"TSH":             {{"TSH", "mUI/L", 0.4, 4, 0, 100, ""}},
	"BHCG":            {{"β-HCG", "UI/L", 0, 5, 0, 300000, ""}},
	"MALARIA_RDT":     {{"TDR paludisme", "", 0, 0, 0, 0, "Négatif / Positif"}},
	"THICK_DROP":      {{"Goutte épaisse", "trophozoïtes/µL", 0, 0, 0, 0, "Absence de parasites"}},
	"WIDAL":           {{"Widal TO", "titre", 0, 0, 0, 0, "< 1/80"}},
	"URINE_CBU":       {{"Leucocyturie", "/mm3", 0, 10, 0, 0, ""}, {"Culture", "", 0, 0, 0, 0, "Stérile"}},
	"BLOOD_CULTURE":   {{"Hémoculture", "", 0, 0, 0, 0, "Stérile à 5 jours"}},
	"BLOOD_GROUP":     {{"Groupe ABO-Rh", "", 0, 0, 0, 0, "Groupe déterminé"}},
}

// labResults produces normal values most of the time, abnormal ones regularly and a few
// critical values, against the reference ranges stored with each result.
func labResults(code string, g *RNG) []laboratory.ResultInput {
	panel := labPanels[code]
	if len(panel) == 0 {
		panel = []labParam{{"Résultat", "", 0, 0, 0, 0, "Dans les normes"}}
	}
	var out []laboratory.ResultInput
	for _, p := range panel {
		r := laboratory.ResultInput{Parameter: p.name, Unit: p.unit}
		if p.text != "" {
			r.ReferenceText = p.text
			switch {
			case code == "MALARIA_RDT" && g.Chance(0.35):
				r.Value = "Positif"
			case code == "MALARIA_RDT":
				r.Value = "Négatif"
			case code == "BLOOD_GROUP":
				r.Value = Pick(g, []string{"O+", "A+", "B+", "AB+", "O-"})
			case code == "THICK_DROP" && g.Chance(0.3):
				r.Value = fmt.Sprintf("%d trophozoïtes/µL", g.Between(200, 20000))
			default:
				r.Value = p.text
			}
			out = append(out, r)
			continue
		}
		lo, hi := p.lo, p.hi
		r.ReferenceMin, r.ReferenceMax = &lo, &hi
		if p.critHi > 0 {
			cl, ch := p.critLo, p.critHi
			r.CriticalMin, r.CriticalMax = &cl, &ch
		}
		var x float64
		switch q := g.Float(); {
		case q < 0.7:
			x = lo + (hi-lo)*g.Float()
		case q < 0.96:
			if g.Chance(0.5) {
				x = hi * (1.1 + g.Float()*0.6)
			} else {
				x = lo * (0.5 + g.Float()*0.4)
			}
		default: // critical value
			if p.critHi > 0 {
				x = p.critHi * (1 + g.Float()*0.2)
			} else {
				x = hi * 3
			}
			r.Comment = "Valeur critique — clinicien prévenu"
		}
		r.Value = strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", x), "0"), ".")
		out = append(out, r)
	}
	return out
}

// ---------------------------------------------------------------------------------------
// Imaging: ORDERED → SCHEDULED → IN_PROGRESS → REPORT_DRAFTED → VALIDATED
// (REPORT_CLOSED without validation; CANCELLED before the exam).

func imagingOrders(ctx *StepContext, v *Visit) error {
	w := ctx.W
	g := v.g
	head := w.memberIn("RADIOLOGIE", "RAD", g)
	if err := w.Svc.ImagingRepo.Materialize(head.UserID); err != nil {
		return err
	}
	var orders []imaging.Order
	if err := w.DB.Where("consultation_id = ?", v.ConsultID).Order("medical_exam_id").Find(&orders).Error; err != nil {
		return err
	}
	for i := range orders {
		o := orders[i]
		g := v.g.Stream("imaging-order", uint64(o.MedicalExamID))
		rad := radiologistFor(w, o.Modality, g)
		a := rad.ImagingAccess()
		if g.Chance(0.02) {
			continue // not yet scheduled (ORDERED)
		}
		if g.Chance(0.04) {
			w.Sched.At(ctx.Sim.Add(time.Duration(g.Between(10, 600))*time.Minute), "imagerie.annulation", func(ctx *StepContext) error {
				_, err := w.Svc.Imaging.Cancel(o.ID, a, Pick(g, []string{"Examen non indiqué après avis", "Patient non joignable", "Contre-indication"}))
				return err
			})
			continue
		}
		when := ctx.Sim.Add(time.Duration(g.Between(1, 6)) * time.Hour)
		if o.Modality == "CT" || g.Chance(0.3) {
			when = ctx.Sim.AddDate(0, 0, g.Between(1, 4)).Truncate(time.Hour).Add(time.Duration(g.Between(1, 8)) * time.Hour)
		}
		w.Sched.At(ctx.Sim.Add(5*time.Minute), "imagerie.planification", func(ctx *StepContext) error {
			if _, err := w.Svc.Imaging.Schedule(o.ID, a, imaging.ScheduleRequest{ScheduledAt: when, Comment: "Rendez-vous d'imagerie"}); err != nil {
				return err
			}
			w.Sched.At(when.Add(time.Duration(g.Between(-10, 30))*time.Minute), "imagerie.examen", func(ctx *StepContext) error {
				if _, err := w.Svc.Imaging.Start(o.ID, a, imaging.StartRequest{TechnicalNotes: "Examen réalisé selon protocole", ContrastUsed: o.Modality == "CT" && g.Chance(0.5)}); err != nil {
					return err
				}
				billAfter(ctx, v.P, ctx.Sim.Add(time.Duration(g.Between(10, 120))*time.Minute), v.Service, g)
				reportDelay := time.Duration(g.Between(30, 600)) * time.Minute
				if g.Chance(0.1) { // radiologist backlog: report dictated days later
					reportDelay = time.Duration(g.Between(30, 160)) * time.Hour
				}
				w.Sched.At(ctx.Sim.Add(reportDelay), "imagerie.compte-rendu", func(ctx *StepContext) error {
					findings, conclusion := imagingReport(o.Modality, g)
					req := imaging.ReportRequest{ClinicalIndication: "Indication clinique de la consultation", Technique: "Technique standard", Findings: findings, Conclusion: conclusion, Recommendation: "Corrélation clinique"}
					if g.Chance(0.3) {
						req.DocumentURL = fmt.Sprintf("https://pacs.medcore.example/viewer/%d", o.ID)
					}
					if _, err := w.Svc.Imaging.SaveReport(o.ID, a, req); err != nil {
						return err
					}
					if g.Chance(0.05) {
						w.Sched.At(ctx.Sim.Add(time.Duration(g.Between(60, 1440))*time.Minute), "imagerie.cloture-sans-validation", func(ctx *StepContext) error {
							_, err := w.Svc.Imaging.CloseReport(o.ID, a, "Compte rendu clos sans validation (examen incomplet)")
							return err
						})
						return nil
					}
					w.Sched.At(ctx.Sim.Add(time.Duration(g.Between(30, 2000))*time.Minute), "imagerie.validation", func(ctx *StepContext) error {
						_, err := w.Svc.Imaging.Validate(o.ID, a)
						return err
					})
					return nil
				})
				return nil
			})
			return nil
		})
	}
	return nil
}

// radiologistFor assigns an operator habilitated for the modality (capabilities are
// carried by the staff profile; the imaging module does not enforce them — see report).
func radiologistFor(w *World, modality string, g *RNG) *Member {
	want := map[string]string{"XRAY": "XRAY", "ULTRASOUND": "ULTRASOUND", "CT": "CT"}[modality]
	var list []*Member
	for _, m := range w.Staff.ByFunction("RADIOLOGIE") {
		if want == "" || contains(m.Capabilities, want) {
			list = append(list, m)
		}
	}
	if len(list) == 0 {
		return w.memberIn("RADIOLOGIE", "RAD", g)
	}
	return list[g.Intn(len(list))]
}

func imagingReport(modality string, g *RNG) (string, string) {
	normal := g.Chance(0.6)
	switch modality {
	case "XRAY":
		if normal {
			return "Pas de lésion osseuse ou parenchymateuse décelable.", "Examen radiologique normal."
		}
		return "Opacité alvéolaire basale droite.", "Aspect compatible avec une pneumopathie."
	case "ULTRASOUND":
		if normal {
			return "Organes explorés d'échostructure normale.", "Échographie sans anomalie."
		}
		return "Lithiase vésiculaire de 12 mm sans signe de cholécystite.", "Lithiase vésiculaire non compliquée."
	case "CT":
		if normal {
			return "Absence de lésion hémorragique ou ischémique.", "Scanner normal."
		}
		return "Hypodensité focale de 15 mm.", "Lésion à caractériser par IRM."
	}
	return "Examen réalisé.", "Résultat dans les limites de la normale."
}

// ---------------------------------------------------------------------------------------
// Pharmacy: dispensation of the consultation prescriptions (complete, partial, none) through
// the pharmacy service (FEFO batch selection, stock movements OUT, prescription status).

func dispense(ctx *StepContext, v *Visit) error {
	w := ctx.W
	g := v.g
	var rx []struct {
		ID             uint
		PresentationID uint
		Quantity       float64
	}
	if err := w.DB.Table("consultation_prescriptions").Select("id, presentation_id, quantity").Where("consultation_id = ?", v.ConsultID).Order("id").Scan(&rx).Error; err != nil {
		return err
	}
	if len(rx) == 0 {
		return nil
	}
	ph := w.memberIn("PHARMACIEN", "PHARM", g)
	mode := g.Weighted([]float64{68, 12, 20}) // complete, partial, not dispensed (patient buys elsewhere)
	if mode == 2 {
		return nil
	}
	pid := v.P.PatientID
	dispensed := false
	for _, p := range rx {
		qty := p.Quantity
		if mode == 1 {
			qty = float64(int(p.Quantity / 2))
			if qty < 1 {
				qty = 1
			}
		}
		rxID := p.ID
		if _, err := w.Svc.Pharmacy.CreateDispensation(pharmacy.CreateDispensationRequest{PresentationID: p.PresentationID, Quantity: qty, PatientID: &pid, PrescriptionID: &rxID,
			Notes: "Délivrance au comptoir", IdempotencyKey: fmt.Sprintf("dc-disp-%d-%d", v.ID, p.ID)}, ph.UserID); err != nil {
			w.Stats.ok("refus-attendu.pharmacie-rupture") // stock-out: the prescription stays pending
			_ = err
			continue
		}
		dispensed = true
		if mode == 1 && g.Chance(0.4) {
			rest := p.Quantity - qty
			w.Sched.At(ctx.Sim.AddDate(0, 0, g.Between(1, 5)), "pharmacie.complement", func(ctx *StepContext) error {
				_, err := w.Svc.Pharmacy.CreateDispensation(pharmacy.CreateDispensationRequest{PresentationID: p.PresentationID, Quantity: rest, PatientID: &pid, PrescriptionID: &rxID,
					Notes: "Complément de délivrance", IdempotencyKey: fmt.Sprintf("dc-disp2-%d-%d", v.ID, p.ID)}, ph.UserID)
				if err != nil {
					ctx.W.Stats.ok("refus-attendu.pharmacie-rupture")
					return nil
				}
				billAfter(ctx, v.P, ctx.Sim.Add(10*time.Minute), "PHARM", g)
				return nil
			})
		}
	}
	if dispensed {
		billAfter(ctx, v.P, ctx.Sim.Add(time.Duration(g.Between(5, 30))*time.Minute), "PHARM", g)
	}
	return nil
}

// ---------------------------------------------------------------------------------------
// Hospitalization: PLANNED → ADMITTED → DISCHARGED (CANCELLED), bed assignment, transfer,
// release, nightly performed acts (HOSPITALIZATION / PROCEDURE catalogue entries).

var hospServiceFor = map[string]string{"URG": "SOINS", "GEN": "GEN", "CARD": "GEN", "PED": "PED", "MAT": "MAT", "CHIR": "CHIR"}

func hospitalize(ctx *StepContext, v *Visit) error {
	w := ctx.W
	g := v.g
	stay := g.Between(2, 9)
	expected := ctx.Sim.AddDate(0, 0, stay)
	h, _, err := w.Svc.Hosp.Create(hospitalizations.CreateRequest{PatientID: v.P.PatientID, SourceConsultationID: v.ConsultID, AdmissionDiagnosis: "Admission depuis la consultation", ExpectedDischargeAt: expected.Format(time.RFC3339)}, v.Doctor.UserID)
	if err != nil {
		return err
	}
	if g.Chance(0.08) {
		w.Sched.At(ctx.Sim.Add(time.Duration(g.Between(30, 240))*time.Minute), "hospitalisation.annulation", func(ctx *StepContext) error {
			_, err := w.Svc.Hosp.Cancel(h.ID, v.Doctor.UserID, v.Doctor.HospAccess())
			return err
		})
		return nil
	}
	ward := hospServiceFor[v.Service]
	if ward == "" {
		ward = "GEN"
	}
	if g.Chance(0.06) {
		ward = "REA"
	}
	// The stay belongs to the service of the source consultation: admission is recorded by a
	// nurse of that service (hospitalizations.update, service-scoped); bed-side acts by the
	// ward nurse.
	admitter := w.memberIn("INFIRMIER", v.Service, g)
	nurse := w.memberIn("INFIRMIER", ward, g)
	admitAt := ctx.Sim.Add(time.Duration(g.Between(30, 300)) * time.Minute)
	// Programmed surgery: the stay stays PLANNED with a RESERVED bed until the admission day.
	if g.Chance(map[string]float64{"CHIR": 0.6, "MAT": 0.4}[ward] + 0.12) {
		admitAt = ctx.Sim.Truncate(24*time.Hour).AddDate(0, 0, g.Between(1, 4)).Add(7*time.Hour + 30*time.Minute)
		expected = admitAt.AddDate(0, 0, stay)
		w.Sched.At(ctx.Sim.Add(20*time.Minute), "hospitalisation.reservation-lit", func(ctx *StepContext) error {
			if bed := freeBed(w, ward); bed != 0 {
				_, err := w.Svc.Hosp.AssignBed(h.ID, bed, nurse.UserID)
				return err
			}
			return nil
		})
	}
	w.Sched.At(admitAt, "hospitalisation.admission", func(ctx *StepContext) error {
		var status string
		w.DB.Table("hospitalizations").Select("status").Where("id = ?", h.ID).Scan(&status)
		if status != hospitalizations.StatusPlanned {
			return nil
		}
		if _, err := w.Svc.Hosp.Admit(h.ID, hospitalizations.AdmitRequest{AdmittedAt: ctx.Sim.Format(time.RFC3339), AdmissionDiagnosis: "Admis en " + ward}, admitter.UserID, admitter.HospAccess()); err != nil {
			if strings.Contains(err.Error(), "hospitalisation active") {
				w.Stats.ok("refus-attendu.admission-doublon")
				_, err = w.Svc.Hosp.Cancel(h.ID, v.Doctor.UserID, v.Doctor.HospAccess())
			}
			return err
		}
		var bed uint
		w.DB.Table("hospitalization_bed_assignments").Select("bed_id").Where("hospitalization_id = ? AND released_at IS NULL", h.ID).Scan(&bed)
		if bed == 0 {
			// Ward full: the patient is admitted without a bed for now (visible in the bed
			// board); care, acts and discharge still follow.
			if bed = freeBed(w, ward); bed != 0 {
				if _, err := w.Svc.Hosp.AssignBed(h.ID, bed, nurse.UserID); err != nil {
					return err
				}
			}
		}
		nightAct := map[string]string{"REA": "HOSP-NUIT-REA", "CHIR": "HOSP-NUIT-IND", "MAT": "HOSP-NUIT-IND"}[ward]
		if nightAct == "" {
			nightAct = "HOSP-NUIT-STD"
		}
		for n := 1; n <= stay; n++ {
			night := ctx.Sim.AddDate(0, 0, n).Truncate(24 * time.Hour).Add(6 * time.Hour)
			hid := h.ID
			w.Sched.At(night, "actes.hospitalisation", func(ctx *StepContext) error {
				return createAct(ctx, v.P, nightAct, &hid, nurse, 1)
			})
			if g.Chance(0.5) {
				proc := Pick(g, []string{"SOINS-INF", "PERFUSION", "PANSEMENT", "SOINS-CONFORT"})
				w.Sched.At(night.Add(3*time.Hour), "actes.soins", func(ctx *StepContext) error {
					id, err := createActID(ctx, v.P, proc, &hid, nurse, 1)
					if err != nil || !g.Chance(0.06) {
						return err
					}
					// Entered by mistake: voided by the ward physician before billing.
					doc := wardPhysician(w, hid, g, v.Doctor)
					_, err = w.Svc.Acts.Void(id, performed_acts.VoidRequest{Reason: "Acte saisi par erreur"}, doc.UserID)
					return err
				})
			}
		}
		if ward == "MAT" && v.P.Has("GROSSESSE") {
			hid := h.ID
			w.Sched.At(ctx.Sim.Add(time.Duration(g.Between(2, 20))*time.Hour), "actes.accouchement", func(ctx *StepContext) error { return createAct(ctx, v.P, "ACCOUCHEMENT", &hid, nurse, 1) })
		}
		if ward == "CHIR" {
			hid := h.ID
			w.Sched.At(ctx.Sim.Add(time.Duration(g.Between(12, 30))*time.Hour), "actes.intervention", func(ctx *StepContext) error { return createAct(ctx, v.P, "CHIR-INTERV", &hid, nurse, 1) })
		}
		if stay >= 2 && g.Chance(0.15) { // transfer to another bed
			w.Sched.At(ctx.Sim.AddDate(0, 0, 1).Add(time.Duration(g.Between(1, 6))*time.Hour), "hospitalisation.transfert", func(ctx *StepContext) error {
				// Only a patient who currently occupies a bed can be transferred (a patient
				// admitted while the ward was full has none).
				var current uint
				w.DB.Table("hospitalization_bed_assignments").Select("bed_id").Where("hospitalization_id = ? AND released_at IS NULL", h.ID).Scan(&current)
				if current == 0 {
					return nil
				}
				if nb := freeBed(w, ward); nb != 0 && nb != current {
					_, err := w.Svc.Hosp.TransferBed(h.ID, nb, nurse.UserID)
					return err
				}
				return nil
			})
		}
		dischargeAt := expected.Truncate(time.Hour).Add(time.Duration(g.Between(1, 6)) * time.Hour)
		w.Sched.At(dischargeAt, "hospitalisation.sortie", func(ctx *StepContext) error {
			// The stay follows the bed's service: discharge by a physician of that service.
			doc := wardPhysician(w, h.ID, g, v.Doctor)
			if _, err := w.Svc.Hosp.Discharge(h.ID, hospitalizations.DischargeRequest{DischargedAt: ctx.Sim.Format(time.RFC3339), DischargeDiagnosis: "Évolution favorable",
				DischargeSummary: "Sortie à domicile avec traitement et rendez-vous de contrôle"}, doc.UserID, doc.HospAccess()); err != nil {
				return err
			}
			var active int64
			w.DB.Table("hospitalization_bed_assignments").Where("hospitalization_id = ? AND released_at IS NULL", h.ID).Count(&active)
			if active > 0 {
				if _, err := w.Svc.Hosp.ReleaseBed(h.ID, nurse.UserID); err != nil {
					return err
				}
			}
			billAfter(ctx, v.P, ctx.Sim.Add(time.Duration(g.Between(20, 180))*time.Minute), ward, g)
			return nil
		})
		return nil
	})
	return nil
}

func freeBed(w *World, ward string) uint {
	var id uint
	w.DB.Raw(`SELECT b.id FROM hospitalization_beds b JOIN hospitalization_rooms r ON r.id = b.room_id
		WHERE r.service_id = ? AND b.status = 'AVAILABLE' AND b.is_active ORDER BY b.id LIMIT 1`, w.Org.ServiceID(ward)).Scan(&id)
	return id
}

func createAct(ctx *StepContext, p *Profile, code string, hospID *uint, by *Member, qty float64) error {
	_, err := createActID(ctx, p, code, hospID, by, qty)
	return err
}

func createActID(ctx *StepContext, p *Profile, code string, hospID *uint, by *Member, qty float64) (uint, error) {
	w := ctx.W
	e, ok := w.Ref.Acts[code]
	if !ok {
		return 0, fmt.Errorf("acte %s inconnu", code)
	}
	a, err := w.Svc.Acts.Create(performed_acts.CreateRequest{PatientID: p.PatientID, ActCatalogEntryID: e.ID, Quantity: qty, PerformedAt: ctx.Sim.Format(time.RFC3339), HospitalizationID: hospID}, by.UserID)
	if err != nil {
		return 0, err
	}
	return a.ID, nil
}

func wardPhysician(w *World, hospitalizationID uint, g *RNG, fallback *Member) *Member {
	var code string
	w.DB.Raw(`SELECT s.code FROM hospitalizations h JOIN organization_services s ON s.id = h.service_id WHERE h.id = ?`, hospitalizationID).Scan(&code)
	list := w.Staff.With(func(m *Member) bool { return len(m.Specialties) > 0 && contains(m.Services, code) })
	if len(list) == 0 {
		return w.Staff.Admin
	}
	for _, m := range list {
		if m.UserID == fallback.UserID {
			return m
		}
	}
	return list[g.Intn(len(list))]
}

// ---------------------------------------------------------------------------------------
// Programmed surgery: a recent surgical consultation leads to a stay planned days ahead
// (PLANNED, bed RESERVED), admitted on the day (OCCUPIED), discharged, billed.

type surgeryPlanner struct{}

func (surgeryPlanner) Name() string         { return "Chirurgie programmée" }
func (surgeryPlanner) Setup(w *World) error { return nil }

func (surgeryPlanner) PlanDay(w *World, d Day) {
	if d.Kind == "WEEKEND" || d.Index < 10 {
		return
	}
	g := w.RNG.Stream("surgery.day", uint64(d.Index))
	if !g.Chance(0.55) {
		return
	}
	w.Sched.At(d.At(16, 0).Add(g.Minutes(0, 90)), "hospitalisation.programmation", func(ctx *StepContext) error {
		var row struct {
			ConsultationID, PatientID uint
		}
		// Backlog: consultations recommending a stay that has not been created yet.
		w.DB.Raw(`SELECT c.id consultation_id, c.patient_id FROM consultations c WHERE c.status = 'completed' AND c.hospitalization_required AND c.completed_at > ?
			AND NOT EXISTS (SELECT 1 FROM hospitalizations h WHERE h.source_consultation_id = c.id)
			AND NOT EXISTS (SELECT 1 FROM hospitalizations h WHERE h.patient_id = c.patient_id AND h.status IN ('PLANNED','ADMITTED'))
			ORDER BY c.completed_at LIMIT 1`, ctx.Sim.AddDate(0, 0, -30)).Scan(&row)
		if row.ConsultationID == 0 {
			return nil
		}
		var p *Profile
		for _, x := range w.People.Pool {
			if x.PatientID == row.PatientID {
				p = x
				break
			}
		}
		var svcCode string
		w.DB.Raw(`SELECT s.code FROM consultations c JOIN organization_services s ON s.id = c.service_id WHERE c.id = ?`, row.ConsultationID).Scan(&svcCode)
		docs := w.Clinic.byService[svcCode]
		if p == nil || len(docs) == 0 {
			return nil
		}
		v := &Visit{P: p, Service: svcCode, Doctor: docs[g.Intn(len(docs))], ConsultID: row.ConsultationID, g: g}
		ward := hospServiceFor[svcCode]
		if ward == "" {
			ward = "GEN"
		}
		stay := g.Between(2, 5)
		admitAt := ctx.Sim.Truncate(24*time.Hour).AddDate(0, 0, g.Between(3, 14)).Add(7*time.Hour + 30*time.Minute)
		h, _, err := w.Svc.Hosp.Create(hospitalizations.CreateRequest{PatientID: p.PatientID, SourceConsultationID: row.ConsultationID, AdmissionDiagnosis: "Intervention programmée",
			ExpectedDischargeAt: admitAt.AddDate(0, 0, stay).Format(time.RFC3339)}, v.Doctor.UserID)
		if err != nil {
			return err
		}
		nurse := w.memberIn("INFIRMIER", ward, g)
		admitter := w.memberIn("INFIRMIER", svcCode, g)
		if bed := freeBed(w, ward); bed != 0 {
			if _, err := w.Svc.Hosp.AssignBed(h.ID, bed, nurse.UserID); err != nil {
				return err
			}
		}
		w.Sched.At(admitAt, "hospitalisation.admission-programmee", func(ctx *StepContext) error {
			var status string
			w.DB.Table("hospitalizations").Select("status").Where("id = ?", h.ID).Scan(&status)
			if status != hospitalizations.StatusPlanned {
				return nil
			}
			if _, err := w.Svc.Hosp.Admit(h.ID, hospitalizations.AdmitRequest{AdmittedAt: ctx.Sim.Format(time.RFC3339), AdmissionDiagnosis: "Admission programmée"}, admitter.UserID, admitter.HospAccess()); err != nil {
				if strings.Contains(err.Error(), "hospitalisation active") {
					// Already admitted meanwhile (emergency stay): the programmed stay is cancelled.
					w.Stats.ok("refus-attendu.admission-doublon")
					_, err = w.Svc.Hosp.Cancel(h.ID, v.Doctor.UserID, v.Doctor.HospAccess())
				}
				return err
			}
			hid := h.ID
			w.Sched.At(ctx.Sim.Add(time.Duration(g.Between(2, 6))*time.Hour), "actes.intervention", func(ctx *StepContext) error { return createAct(ctx, p, "CHIR-INTERV", &hid, nurse, 1) })
			for n := 1; n <= stay; n++ {
				w.Sched.At(ctx.Sim.AddDate(0, 0, n).Truncate(24*time.Hour).Add(6*time.Hour), "actes.hospitalisation", func(ctx *StepContext) error { return createAct(ctx, p, "HOSP-NUIT-IND", &hid, nurse, 1) })
			}
			w.Sched.At(ctx.Sim.AddDate(0, 0, stay).Add(time.Duration(g.Between(2, 6))*time.Hour), "hospitalisation.sortie", func(ctx *StepContext) error {
				doc := wardPhysician(w, h.ID, g, v.Doctor)
				if _, err := w.Svc.Hosp.Discharge(h.ID, hospitalizations.DischargeRequest{DischargedAt: ctx.Sim.Format(time.RFC3339), DischargeDiagnosis: "Suites opératoires simples", DischargeSummary: "Sortie à J" + fmt.Sprint(stay) + ", contrôle en consultation"}, doc.UserID, doc.HospAccess()); err != nil {
					return err
				}
				var active int64
				w.DB.Table("hospitalization_bed_assignments").Where("hospitalization_id = ? AND released_at IS NULL", h.ID).Count(&active)
				if active > 0 {
					if _, err := w.Svc.Hosp.ReleaseBed(h.ID, nurse.UserID); err != nil {
						return err
					}
				}
				billAfter(ctx, p, ctx.Sim.Add(time.Hour), "CHIR", g)
				return nil
			})
			return nil
		})
		return nil
	})
}
