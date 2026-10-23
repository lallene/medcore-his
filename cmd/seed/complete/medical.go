package complete

import (
	"fmt"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/medical_records"
)

// Medical record content through medical_records.Service (alerts, allergies, histories,
// vital signs, common medical record: profile, lifestyle, surgical/family history, regular
// treatments, vaccinations, disabilities, devices, documents). Every write is audited and
// produces the timeline events of the real application.

var allergens = []struct{ typ, name, reaction, severity string }{
	{"medication", "Pénicilline", "Urticaire généralisée", "high"},
	{"medication", "Aspirine", "Bronchospasme", "high"},
	{"medication", "Sulfamides", "Éruption cutanée", "medium"},
	{"food", "Arachide", "Œdème de Quincke", "critical"},
	{"food", "Crustacés", "Prurit, œdème labial", "medium"},
	{"environment", "Acariens", "Rhinite allergique", "low"},
	{"environment", "Pollen", "Conjonctivite", "low"},
	{"medication", "Iode (produit de contraste)", "Malaise, hypotension", "high"},
}

var conditionHistory = map[string]struct{ title, desc, severity string }{
	"HTA":       {"Hypertension artérielle", "HTA essentielle sous traitement", "medium"},
	"DIABETE":   {"Diabète de type 2", "Diabète non insulino-requérant, suivi trimestriel", "medium"},
	"ASTHME":    {"Asthme", "Asthme intermittent, crises saisonnières", "low"},
	"DREPANO":   {"Drépanocytose SS", "Crises vaso-occlusives récurrentes", "high"},
	"CARDIO":    {"Insuffisance cardiaque", "Cardiopathie hypertensive, FEVG altérée", "high"},
	"ARTHROSE":  {"Gonarthrose", "Arthrose bilatérale des genoux", "low"},
	"MIGRAINE":  {"Migraine", "Migraine sans aura", "low"},
	"GROSSESSE": {"Grossesse en cours", "Suivi prénatal", "medium"},
}

var conditionTreatment = map[string]struct{ med, dosage, freq string }{
	"HTA":      {"Amlodipine", "5 mg", "1 fois par jour"},
	"DIABETE":  {"Metformine", "850 mg", "2 fois par jour"},
	"ASTHME":   {"Salbutamol", "100 µg", "si besoin"},
	"DREPANO":  {"Acide folique", "5 mg", "1 fois par jour"},
	"CARDIO":   {"Furosémide", "40 mg", "1 fois par jour"},
	"ARTHROSE": {"Paracétamol", "1 g", "3 fois par jour si douleur"},
}

var vaccines = []string{"BCG", "DTC-Polio", "Rougeole-Rubéole", "Fièvre jaune", "Hépatite B", "Tétanos (rappel)", "COVID-19", "Méningite A"}

func enrichRecord(ctx *StepContext, p *Profile) error {
	w := ctx.W
	if p.Tier == TierMinimal {
		return nil
	}
	g := w.RNG.Stream("medical", uint64(p.Index))
	author := pickAuthor(w, g)
	now := ctx.Sim
	svc := w.Svc.MedRec

	// Standard: profile, lifestyle, blood group, first vital signs.
	str := func(s string) *string { return &s }
	tp := func(t time.Time) medical_records.NullableTimePatch {
		return medical_records.NullableTimePatch{Set: true, Value: &t}
	}
	blood := Pick(g, []string{"O", "O", "A", "B", "AB"})
	rh := Pick(g, []string{"+", "+", "+", "-"})
	req := medical_records.UpdateCommonMedicalRecordRequest{
		Profile: &medical_records.PatientMedicalProfileRequest{
			Address: str(p.District + ", Abidjan"), MaritalStatus: str(Pick(g, []string{"Célibataire", "Marié(e)", "Marié(e)", "Veuf(ve)", "Divorcé(e)"})),
			Profession: str(Pick(g, []string{"Commerçant(e)", "Enseignant(e)", "Fonctionnaire", "Étudiant(e)", "Retraité(e)", "Artisan", "Informaticien(ne)", "Agriculteur", "Sans emploi", "Élève"})),
			BloodGroup: str(blood), Rhesus: str(rh),
		},
		Lifestyle: &medical_records.LifestyleRequest{Tobacco: str(Pick(g, []string{"Non-fumeur", "Non-fumeur", "Ancien fumeur", "Fumeur"})), Alcohol: str(Pick(g, []string{"Aucun", "Occasionnel", "Régulier"})),
			PhysicalActivity: str(Pick(g, []string{"Sédentaire", "Modérée", "Régulière"})), Diet: str(Pick(g, []string{"Variée", "Hyposodée", "Diabétique", "Riche en féculents"}))},
	}
	if p.Ident.Sex == "F" {
		req.Profile.EmergencyContactRelationship = str("Conjoint")
	}
	if p.AgeAt(now) < 18 {
		guardian := newIdentity(g, "")
		req.Profile.LegalGuardianName = str(guardian.Full())
		req.Profile.LegalGuardianRelationship = str(Pick(g, []string{"Mère", "Père", "Tante"}))
		req.Profile.LegalGuardianPhone = str(phone("05", 30000000+p.Index))
	}
	if p.Insurance != nil {
		req.Profile.InsuranceName = str(w.Ref.Companies[p.Insurance.CompanyIdx].Name)
		req.Profile.InsuranceNumber = str(p.Insurance.Member)
	}
	// Vaccinations (children and some adults).
	if p.AgeAt(now) < 15 || g.Chance(0.3) {
		var vs []medical_records.VaccinationRequest
		for k := 0; k < g.Between(1, 4); k++ {
			name := vaccines[(p.Index+k)%len(vaccines)]
			date := p.Birth.AddDate(0, g.Between(1, 60), 0)
			if date.After(now) {
				date = now.AddDate(0, -g.Between(1, 6), 0)
			}
			v := medical_records.VaccinationRequest{VaccineName: str(name), Dose: str(fmt.Sprintf("Dose %d", k+1)), VaccinationDate: tp(date), Status: str("done")}
			if g.Chance(0.3) {
				v.NextBoosterDate = tp(now.AddDate(0, g.Between(1, 18), 0))
			}
			vs = append(vs, v)
		}
		req.Vaccinations = medical_records.PatchCollection[medical_records.VaccinationRequest]{Present: true, Upsert: vs}
	}
	if p.Tier == TierRich || p.Tier == TierComplex {
		// Family history, regular treatments, surgical history, documents.
		req.FamilyMedicalHistories = medical_records.PatchCollection[medical_records.FamilyMedicalHistoryRequest]{Present: true, Upsert: []medical_records.FamilyMedicalHistoryRequest{
			{Disease: str(Pick(g, []string{"Hypertension artérielle", "Diabète", "Drépanocytose", "Cancer du sein", "AVC"})), Relationship: str(Pick(g, []string{"Mère", "Père", "Grand-mère", "Frère"}))},
		}}
		var treatments []medical_records.RegularTreatmentRequest
		for _, c := range p.Conditions {
			if t, ok := conditionTreatment[c]; ok {
				treatments = append(treatments, medical_records.RegularTreatmentRequest{MedicationName: str(t.med), Dosage: str(t.dosage), Frequency: str(t.freq), StartDate: tp(now.AddDate(-g.Between(1, 6), 0, 0)), Prescriber: str("Médecin traitant"), IsActive: boolPtr(true)})
			}
		}
		if len(treatments) > 0 {
			req.RegularTreatments = medical_records.PatchCollection[medical_records.RegularTreatmentRequest]{Present: true, Upsert: treatments}
		}
		if g.Chance(0.35) {
			req.SurgicalHistories = medical_records.PatchCollection[medical_records.SurgicalHistoryRequest]{Present: true, Upsert: []medical_records.SurgicalHistoryRequest{
				{ProcedureName: str(Pick(g, []string{"Appendicectomie", "Césarienne", "Cholécystectomie", "Cure de hernie inguinale", "Amygdalectomie"})), ProcedureDate: tp(now.AddDate(-g.Between(2, 20), 0, 0)), Facility: str("CHU de Cocody")},
			}}
		}
		docs := []medical_records.MedicalDocumentRequest{{Type: str("report"), Label: str("Compte rendu d'hospitalisation antérieure"), DocumentDate: tp(now.AddDate(0, -g.Between(2, 30), 0)),
			FileName: str("cr-hospitalisation.pdf"), MimeType: str("application/pdf"), FileURL: str(fmt.Sprintf("https://documents.medcore.example/dc/%d/cr-hospitalisation.pdf", p.Index))}}
		if g.Chance(0.5) {
			docs = append(docs, medical_records.MedicalDocumentRequest{Type: str("imaging"), Label: str("Imagerie réalisée en externe"), DocumentDate: tp(now.AddDate(0, -g.Between(1, 12), 0)),
				FileName: str("imagerie-externe.pdf"), MimeType: str("application/pdf"), FileURL: str(fmt.Sprintf("https://documents.medcore.example/dc/%d/imagerie-externe.pdf", p.Index))})
		}
		req.Documents = medical_records.PatchCollection[medical_records.MedicalDocumentRequest]{Present: true, Upsert: docs}
	}
	if p.Tier == TierComplex {
		if g.Chance(0.25) {
			req.Disabilities = medical_records.PatchCollection[medical_records.DisabilityRequest]{Present: true, Upsert: []medical_records.DisabilityRequest{{Type: str(Pick(g, []string{"Motrice", "Visuelle", "Auditive"})), Level: str(Pick(g, []string{"Légère", "Modérée"})), SpecialNeeds: str("Accompagnement lors des déplacements")}}}
		}
		if p.Has("CARDIO") || g.Chance(0.1) {
			req.MedicalDevices = medical_records.PatchCollection[medical_records.MedicalDeviceRequest]{Present: true, Upsert: []medical_records.MedicalDeviceRequest{{Type: str("implant"), Name: str(Pick(g, []string{"Stimulateur cardiaque", "Prothèse de hanche", "Stent coronaire"})), Reference: str(fmt.Sprintf("DEV-%05d", p.Index)), ImplantationDate: tp(now.AddDate(-g.Between(1, 8), 0, 0)), IsActive: boolPtr(true)}}}
		}
	}
	if _, err := svc.UpdateCommonMedicalRecord(p.PatientID, req, author); err != nil {
		return wrapf(err, "dossier commun")
	}
	// Allergies (+ allergy alerts), conditions as medical histories, clinical alerts.
	if g.Chance(map[string]float64{TierStandard: 0.12, TierRich: 0.35, TierComplex: 0.6}[p.Tier]) {
		a := allergens[g.Intn(len(allergens))]
		if _, err := svc.AddAllergy(p.RecordID, medical_records.CreateAllergyRequest{AllergenType: a.typ, AllergenName: a.name, Reaction: a.reaction, Severity: a.severity}, author); err != nil {
			return wrapf(err, "allergie")
		}
	}
	for _, c := range p.Conditions {
		h := conditionHistory[c]
		start := now.AddDate(-g.Between(1, 10), -g.Between(0, 11), 0)
		if c == "GROSSESSE" {
			start = now.AddDate(0, -g.Between(1, 6), 0)
		}
		if _, err := svc.AddMedicalHistory(p.RecordID, medical_records.CreateMedicalHistoryRequest{Type: "medical", Title: h.title, Description: h.desc, StartDate: &start, Status: "active", Severity: h.severity}, author); err != nil {
			return wrapf(err, "antécédent")
		}
	}
	if p.Has("DREPANO") || p.Has("CARDIO") || (p.Tier == TierComplex && g.Chance(0.3)) {
		title := "Patient à risque"
		if p.Has("DREPANO") {
			title = "Drépanocytaire SS — risque de crise vaso-occlusive"
		}
		if _, err := svc.AddAlert(p.RecordID, medical_records.CreateAlertRequest{Type: "clinical", Title: title, Description: "Alerte saisie lors de l'ouverture du dossier", Severity: "high"}, author); err != nil {
			return wrapf(err, "alerte")
		}
	}
	// Baseline vital signs; chronic patients get a measured series over the past months.
	series := 1
	if p.Has("HTA") || p.Has("DIABETE") || p.Has("CARDIO") {
		series = g.Between(3, 6)
	}
	for k := 0; k < series; k++ {
		at := now.AddDate(0, 0, -k*g.Between(20, 45))
		if _, err := addVitals(w, g, p, at, author, nil); err != nil {
			return err
		}
	}
	return nil
}

func pickAuthor(w *World, g *RNG) uint {
	nurses := w.Staff.ByFunction("INFIRMIER")
	if len(nurses) == 0 {
		return w.Staff.Admin.UserID
	}
	return nurses[g.Intn(len(nurses))].UserID
}

// addVitals records a vital-signs measurement consistent with the patient's conditions.
func addVitals(w *World, g *RNG, p *Profile, at time.Time, author uint, consultationID *uint) (uint, error) {
	age := p.AgeAt(at)
	f := func(lo, hi float64) *float64 { v := float64(int((lo+g.Float()*(hi-lo))*10)) / 10; return &v }
	i := func(lo, hi int) *int { v := g.Between(lo, hi); return &v }
	req := medical_records.CreateVitalSignRequest{ConsultationID: consultationID, TemperatureC: f(36.2, 37.6), HeartRate: i(60, 98), RespiratoryRate: i(12, 20), OxygenSaturation: f(95, 100), MeasuredAt: &at}
	switch {
	case age < 2:
		req.WeightKg, req.HeightCm, req.HeartRate, req.RespiratoryRate = f(3, 12), f(50, 85), i(100, 150), i(25, 45)
	case age < 15:
		req.WeightKg, req.HeightCm = f(12, 50), f(85, 165)
		req.SystolicBP, req.DiastolicBP = i(90, 115), i(55, 75)
	default:
		req.WeightKg, req.HeightCm = f(48, 105), f(150, 192)
		req.SystolicBP, req.DiastolicBP = i(105, 135), i(65, 85)
	}
	if p.Has("HTA") {
		req.SystolicBP, req.DiastolicBP = i(135, 175), i(85, 105)
	}
	if p.Has("DIABETE") {
		req.BloodGlucose = f(1.1, 2.6)
	}
	if g.Chance(0.08) { // febrile episode
		req.TemperatureC = f(38.2, 40.1)
		req.HeartRate = i(95, 125)
	}
	vs, err := w.Svc.MedRec.AddVitalSign(p.RecordID, req, author)
	if err != nil {
		return 0, wrapf(err, "constantes")
	}
	return vs.ID, nil
}
