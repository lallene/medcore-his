package complete

import (
	"fmt"
	"strings"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/consultations"
)

// Consultation documentation through consultations.Service (UpdateConsultation, SOAP,
// specialty data). The content makes the 5 server PDFs meaningful: sick leave, exam request,
// prescription, consultation report, hospitalization slip.

type clinicalPicture struct {
	diagnosis, observation, treatment string
	reasons                           []string // reason codes
	labs, imaging                     []string // exam codes
	soapComplaint, soapHistory        string
}

var pictures = map[string][]clinicalPicture{
	"URG": {
		{"Paludisme simple", "Fièvre à 39 °C depuis 48 h, frissons, TDR positif", "Antipaludéen oral, antipyrétique, hydratation", []string{"FEVER", "MALARIA"}, []string{"MALARIA_RDT", "NFS"}, nil, "Fièvre et frissons", "Début brutal il y a deux jours"},
		{"Gastro-entérite aiguë", "Vomissements, diarrhée, déshydratation modérée", "Réhydratation, antiémétique", []string{"ABDOMINAL_PAIN"}, []string{"IONOGRAM"}, nil, "Diarrhée et vomissements", "Repas suspect la veille"},
		{"Traumatisme du genou", "Chute, douleur et œdème du genou droit", "Immobilisation, antalgiques", nil, nil, []string{"XR_KNEE"}, "Chute", "Traumatisme direct en descendant un escalier"},
		{"Crise d'asthme modérée", "Sibilants diffus, SpO2 93 %", "Nébulisation, corticothérapie courte", []string{"COUGH"}, nil, []string{"CHEST_XRAY"}, "Gêne respiratoire", "Crise déclenchée par la poussière"},
		{"Douleur thoracique atypique", "ECG sans anomalie, douleur pariétale", "Antalgiques, réévaluation", nil, []string{"CRP"}, []string{"CHEST_XRAY"}, "Douleur thoracique", "Douleur à la mobilisation"},
		{"Plaie de la main", "Plaie superficielle de 3 cm", "Suture, prévention antitétanique", nil, nil, nil, "Plaie", "Coupure accidentelle"},
		{"Crise vaso-occlusive", "Douleurs osseuses diffuses chez un patient drépanocytaire", "Antalgiques de palier 2, hydratation IV", nil, []string{"NFS", "CRP"}, nil, "Douleurs osseuses", "Crise déclenchée par une infection"},
	},
	"GEN": {
		{"Infection respiratoire haute", "Rhinorrhée, odynophagie, apyrexie", "Traitement symptomatique", []string{"COUGH", "FEVER"}, nil, nil, "Toux et mal de gorge", "Évolution depuis 4 jours"},
		{"Lombalgie commune", "Raideur lombaire sans signe neurologique", "Antalgiques, kinésithérapie", []string{"MUSCLE_PAIN"}, nil, []string{"XR_SPINE"}, "Douleur lombaire", "Port de charge lourde"},
		{"Bilan de santé", "Examen clinique normal", "Conseils hygiéno-diététiques", nil, []string{"NFS", "GLYCEMIA", "LIPID_PANEL", "CREATININE"}, nil, "Bilan annuel", "Pas de plainte particulière"},
		{"Fièvre typhoïde suspectée", "Fièvre prolongée, douleurs abdominales", "Antibiothérapie probabiliste", []string{"FEVER", "ABDOMINAL_PAIN"}, []string{"WIDAL", "BLOOD_CULTURE", "NFS"}, nil, "Fièvre persistante", "Fièvre depuis 8 jours"},
		{"Infection urinaire basse", "Brûlures mictionnelles, pollakiurie", "Antibiothérapie courte", nil, []string{"URINE_CBU"}, nil, "Brûlures urinaires", "Depuis 3 jours"},
		{"Céphalées de tension", "Examen neurologique normal", "Antalgiques, gestion du stress", []string{"HEADACHE"}, nil, nil, "Maux de tête", "Céphalées en fin de journée"},
	},
	"PED": {
		{"Paludisme de l'enfant", "Fièvre, vomissements, TDR positif", "Antipaludéen adapté au poids", []string{"FEVER", "MALARIA"}, []string{"MALARIA_RDT", "NFS"}, nil, "Fièvre", "Fièvre depuis 24 h"},
		{"Bronchiolite", "Toux, sibilants, bon état général", "Désobstruction, surveillance", []string{"COUGH"}, nil, []string{"CHEST_XRAY"}, "Toux", "Contexte épidémique"},
		{"Consultation de suivi pédiatrique", "Croissance harmonieuse, vaccins à jour", "Conseils nutritionnels", nil, nil, nil, "Suivi", "Visite systématique"},
		{"Otite moyenne aiguë", "Tympan bombé et inflammatoire", "Antibiothérapie, antalgiques", []string{"FEVER"}, nil, nil, "Douleur d'oreille", "Pleurs nocturnes"},
	},
	"CARD": {
		{"Hypertension artérielle déséquilibrée", "PA 165/100 mmHg", "Ajustement du traitement antihypertenseur", nil, []string{"CREATININE", "IONOGRAM", "LIPID_PANEL"}, []string{"CARDIAC_ULTRASOUND"}, "Suivi tensionnel", "Céphalées matinales"},
		{"Insuffisance cardiaque stable", "Œdèmes discrets des membres inférieurs", "Diurétique, régime hyposodé", nil, []string{"CREATININE", "IONOGRAM"}, []string{"CARDIAC_ULTRASOUND", "CHEST_XRAY"}, "Essoufflement", "Dyspnée d'effort stable"},
		{"Palpitations", "ECG en rythme sinusal", "Réassurance, holter à programmer", nil, []string{"TSH", "IONOGRAM"}, nil, "Palpitations", "Épisodes brefs"},
	},
	"DIAB": {
		{"Diabète de type 2 équilibré", "HbA1c à 6,8 %", "Poursuite de la metformine", nil, []string{"HBA1C", "FASTING_GLUCOSE", "CREATININE"}, nil, "Suivi diabétique", "Bonne observance"},
		{"Diabète de type 2 déséquilibré", "Glycémies capillaires élevées", "Intensification thérapeutique", nil, []string{"HBA1C", "FASTING_GLUCOSE", "LIPID_PANEL"}, nil, "Glycémies élevées", "Écarts alimentaires"},
	},
	"NEURO": {
		{"Migraine sans aura", "Examen neurologique normal", "Traitement de crise et de fond", []string{"HEADACHE"}, nil, nil, "Céphalées", "Crises mensuelles"},
		{"Vertiges", "Nystagmus absent, Romberg négatif", "Traitement symptomatique", nil, nil, []string{"CT_BRAIN"}, "Vertiges", "Depuis une semaine"},
	},
	"RHUM": {
		{"Gonarthrose", "Craquements, épanchement minime", "Antalgiques, kinésithérapie", []string{"MUSCLE_PAIN"}, []string{"CRP"}, []string{"XR_KNEE"}, "Douleur du genou", "Douleur mécanique"},
		{"Lombosciatique", "Lasègue positif à 60°", "AINS, repos relatif", []string{"MUSCLE_PAIN"}, nil, []string{"XR_SPINE", "CT_ABDOMEN"}, "Douleur irradiant à la jambe", "Depuis 10 jours"},
	},
	"ORL": {
		{"Angine érythémateuse", "Amygdales inflammatoires", "Antalgiques, antibiothérapie si besoin", []string{"FEVER"}, nil, nil, "Mal de gorge", "Depuis 2 jours"},
		{"Sinusite maxillaire", "Douleur faciale, rhinorrhée purulente", "Antibiothérapie, lavages", []string{"HEADACHE"}, nil, nil, "Douleur faciale", "Après un rhume"},
		{"Hypoacousie", "Bouchon de cérumen bilatéral", "Lavage auriculaire", nil, nil, nil, "Baisse de l'audition", "Progressive"},
	},
	"GYNE": {
		{"Infection génitale basse", "Leucorrhées", "Traitement local", nil, []string{"URINE_CBU"}, []string{"PELVIC_ULTRASOUND"}, "Pertes", "Depuis une semaine"},
		{"Suivi gynécologique", "Examen normal", "Contraception reconduite", nil, nil, []string{"PELVIC_ULTRASOUND"}, "Suivi", "Visite annuelle"},
	},
	"MAT": {
		{"Grossesse évolutive — consultation prénatale", "Hauteur utérine conforme, BCF présents", "Supplémentation fer et acide folique", nil, []string{"NFS", "BLOOD_GROUP", "BHCG", "GLYCEMIA"}, []string{"OBSTETRIC_ULTRASOUND"}, "Suivi de grossesse", "Grossesse sans particularité"},
		{"Menace d'accouchement prématuré", "Contractions utérines, col modifié", "Repos, tocolyse", nil, []string{"NFS", "CRP", "URINE_CBU"}, []string{"OBSTETRIC_ULTRASOUND"}, "Contractions", "Depuis la veille"},
	},
	"CHIR": {
		{"Hernie inguinale", "Tuméfaction inguinale réductible", "Indication opératoire", []string{"ABDOMINAL_PAIN"}, []string{"NFS", "CREATININE"}, []string{"ABDOMINAL_ULTRASOUND"}, "Tuméfaction", "Depuis plusieurs mois"},
		{"Appendicite aiguë", "Défense en fosse iliaque droite", "Appendicectomie en urgence", []string{"ABDOMINAL_PAIN", "FEVER"}, []string{"NFS", "CRP"}, []string{"ABDOMINAL_ULTRASOUND"}, "Douleur abdominale", "Depuis 24 h"},
		{"Lipome", "Masse sous-cutanée mobile", "Exérèse programmée", nil, nil, nil, "Masse sous la peau", "Évolution lente"},
	},
}

var physicalFindings = []string{"Normal", "Sans particularité", "RAS", "Discret œdème", "Sensibilité à la palpation", "Murmure vésiculaire conservé", "Bruits du cœur réguliers"}

func (v *Visit) document(ctx *StepContext) error {
	w := ctx.W
	g := v.g
	access := v.Doctor.ConsultAccess()
	c, err := w.Svc.Consult.GetConsultation(v.ConsultID, access)
	if err != nil {
		return err
	}
	pics := pictures[v.Service]
	pic := pics[g.Intn(len(pics))]
	if v.P.Has("DREPANO") && v.Service == "URG" {
		pic = pics[len(pics)-1]
	}
	age := v.P.AgeAt(ctx.Sim)
	str := func(s string) *string { return &s }
	req := consultations.UpdateConsultationRequest{ExpectedVersion: c.Version, Diagnosis: str(pic.diagnosis), Observations: str(pic.observation), Treatment: str(pic.treatment)}
	reasonIDs := w.reasonIDs(pic.reasons)
	req.ReasonIDs = &reasonIDs
	// Exams: the picture's typical exams, sometimes reduced, sometimes none.
	var examIDs []uint
	if g.Chance(0.75) {
		for _, code := range pic.labs {
			if e, ok := w.Ref.Exams[code]; ok && g.Chance(0.8) {
				examIDs = append(examIDs, e.ID)
			}
		}
	}
	if g.Chance(0.7) {
		for _, code := range pic.imaging {
			if e, ok := w.Ref.Exams[code]; ok && g.Chance(0.75) {
				examIDs = append(examIDs, e.ID)
			}
		}
	}
	if v.Service == "CARD" && g.Chance(0.5) {
		if e, ok := w.Ref.Exams["ECG"]; ok {
			examIDs = append(examIDs, e.ID) // non-lab, non-imaging exam: exam request PDF only
		}
	}
	req.ExamIDs = &examIDs
	// Prescriptions (pharmacy queue) and administered treatments.
	var rx []consultations.PrescriptionRequest
	if len(w.Ref.Presentations) > 0 && g.Chance(0.62) {
		for k := 0; k < g.Between(1, 3); k++ {
			p := w.Ref.Presentations[g.Intn(len(w.Ref.Presentations))]
			rx = append(rx, consultations.PrescriptionRequest{PresentationID: p.ID, Quantity: float64(g.Between(1, 3) * 10), Duration: fmt.Sprintf("%d jours", g.Between(3, 30)), Instructions: Pick(g, posologies)})
		}
	}
	req.Prescriptions = &rx
	if len(w.Ref.Presentations) > 0 && v.Service == "URG" && g.Chance(0.4) {
		p := w.Ref.Presentations[g.Intn(len(w.Ref.Presentations))]
		at := []consultations.AdministeredTreatmentRequest{{PresentationID: p.ID, Quantity: 1, Instructions: "Administré aux urgences"}}
		req.AdministeredTreatments = &at
	}
	if len(w.Ref.Presentations) > 0 && len(v.P.Conditions) > 0 && g.Chance(0.6) {
		p := w.Ref.Presentations[g.Intn(len(w.Ref.Presentations))]
		pm := []consultations.PreviousMedicationRequest{{PresentationID: p.ID, Instructions: "Traitement de fond", Status: "ongoing"}}
		req.PreviousMedications = &pm
	}
	// Physical examination.
	if len(w.Ref.Areas) > 0 {
		var pe []consultations.PhysicalExamRequest
		for k := 0; k < g.Between(2, 4); k++ {
			pe = append(pe, consultations.PhysicalExamRequest{AreaID: w.Ref.Areas[g.Intn(len(w.Ref.Areas))].ID, Observation: Pick(g, physicalFindings)})
		}
		areas := dedupAreas(pe)
		req.PhysicalExams = &areas
	}
	// Antecedents (visit type, cardiovascular risk, pregnancy).
	visit := "PREMIERE"
	if v.P.Visits > 0 {
		visit = "CONTROLE"
	}
	if v.Walkin && v.Service == "URG" {
		visit = "URGENCE"
	}
	ant := &consultations.AntecedentRequest{VisitType: visit, HasHTA: boolPtr(v.P.Has("HTA")), HasDiabetes: boolPtr(v.P.Has("DIABETE")), Tobacco: boolPtr(g.Chance(0.12)), Alcohol: boolPtr(g.Chance(0.2))}
	if v.P.Has("GROSSESSE") {
		ant.PregnancyOngoing = boolPtr(true)
		ant.DDR = ctx.Sim.AddDate(0, -g.Between(2, 8), 0).Format("2006-01-02")
		ant.GynecoObstetricHistory = fmt.Sprintf("G%dP%d", g.Between(1, 5), g.Between(0, 3))
	}
	if len(v.P.Conditions) > 0 {
		ant.OtherMedical = strings.Join(v.P.Conditions, ", ")
	}
	req.Antecedent = ant
	if v.Service == "CHIR" && g.Chance(0.4) {
		sh := []consultations.SurgicalHistoryRequest{{ProcedureName: Pick(g, []string{"Appendicectomie", "Césarienne", "Hernie ombilicale"}), ProcedureDate: ctx.Sim.AddDate(-g.Between(2, 15), 0, 0).Format("2006-01-02"), Indication: "Antécédent opératoire"}}
		req.SurgicalHistories = &sh
	}
	if (v.Service == "MAT" || v.Service == "GYNE") && g.Chance(0.5) {
		gh := []consultations.GynecoObstetricHistoryRequest{{EventType: Pick(g, []string{"ACCOUCHEMENT", "FAUSSE_COUCHE", "CESARIENNE"}), EventDate: ctx.Sim.AddDate(-g.Between(1, 10), 0, 0).Format("2006-01-02"), Outcome: "Favorable"}}
		req.GynecoObstetricHistories = &gh
	}
	// Sick leave for working-age adults.
	if age >= 18 && age < 65 && g.Chance(0.14) {
		req.SickLeaveRequired, req.SickLeaveDays = boolPtr(true), intPtr(g.Between(1, 10))
	}
	// Hospitalization decision.
	v.Disposition = "DISCHARGED"
	hospProb := map[string]float64{"URG": 0.13, "CHIR": 0.35, "MAT": 0.2, "CARD": 0.08, "PED": 0.08, "GEN": 0.05, "DIAB": 0.03, "NEURO": 0.04}[v.Service]
	if !ctx.Sim.Before(w.Cfg.Anchor.AddDate(0, 0, -7)) {
		hospProb *= 2 // busy last week: wards populated at the present instant
	}
	if g.Chance(hospProb) {
		v.Hospitalize, v.Disposition = true, "HOSPITALIZED"
		req.HospitalizationRequired = boolPtr(true)
		req.HospitalizationReason = str("Surveillance et traitement — " + pic.diagnosis)
		req.HospitalizationType = str(Pick(g, []string{"Médicale", "Chirurgicale", "Observation"}))
		req.HospitalizationDuration = intPtr(g.Between(2, 7))
	} else {
		switch r := g.Float(); {
		case r < 0.05:
			v.Disposition = "REFERRED"
		case r < 0.07:
			v.Disposition = "OBSERVATION"
		case r < 0.08:
			v.Disposition = "TRANSFERRED"
		case r < 0.09:
			v.Disposition = "OTHER"
		}
	}
	if _, err := w.Svc.Consult.UpdateConsultation(v.ConsultID, req, v.Doctor.UserID, access); err != nil {
		return err
	}
	if g.Chance(0.75) {
		soap := consultations.UpsertConsultationSOAPRequest{ChiefComplaint: pic.soapComplaint, HistoryOfPresentIllness: pic.soapHistory, GeneralAppearance: "Bon état général",
			Consciousness: "Conscient et orienté", HydrationStatus: Pick(g, []string{"Normale", "Déshydratation légère"}), PhysicalExamSummary: pic.observation,
			PrimaryDiagnosis: pic.diagnosis, ClinicalImpression: "Tableau compatible", TreatmentPlan: pic.treatment, FollowUpPlan: Pick(g, followUps),
			PatientAdvice: "Revenir en cas d'aggravation", Disposition: v.Disposition}
		if _, err := w.Svc.Consult.UpsertSOAP(v.ConsultID, soap, v.Doctor.UserID, access); err != nil {
			return wrapf(err, "SOAP")
		}
	}
	if sp := specialtyData(v, g, ctx.Sim); sp != nil {
		if _, err := w.Svc.Consult.UpsertSpecialtyData(v.ConsultID, *sp, v.Doctor.UserID, access); err != nil {
			return wrapf(err, "données de spécialité")
		}
	}
	w.Sched.At(ctx.Sim.Add(time.Duration(g.Between(5, 20))*time.Minute), "consultations.cloture", func(ctx *StepContext) error { return v.complete(ctx) })
	return nil
}

func dedupAreas(in []consultations.PhysicalExamRequest) []consultations.PhysicalExamRequest {
	seen := map[uint]bool{}
	var out []consultations.PhysicalExamRequest
	for _, x := range in {
		if !seen[x.AreaID] {
			seen[x.AreaID] = true
			out = append(out, x)
		}
	}
	return out
}

func specialtyData(v *Visit, g *RNG, at time.Time) *consultations.UpsertConsultationSpecialtyRequest {
	switch v.Service {
	case "CARD":
		return &consultations.UpsertConsultationSpecialtyRequest{SpecialtyCode: "CARDIOLOGIE", Data: map[string]any{"nyhaClass": Pick(g, []string{"I", "II", "III"}), "ecgRhythm": "Sinusal", "chestPain": g.Chance(0.2)}}
	case "MAT", "GYNE":
		return &consultations.UpsertConsultationSpecialtyRequest{SpecialtyCode: "GYNECOLOGIE", Data: map[string]any{"gestationalAgeWeeks": g.Between(8, 38), "fetalHeartRate": g.Between(120, 160), "fundalHeightCm": g.Between(12, 34)}}
	case "PED":
		return &consultations.UpsertConsultationSpecialtyRequest{SpecialtyCode: "PEDIATRIE", Data: map[string]any{"growthPercentile": g.Between(10, 90), "vaccinationUpToDate": g.Chance(0.85)}}
	case "DIAB":
		return &consultations.UpsertConsultationSpecialtyRequest{SpecialtyCode: "DIABETOLOGIE", Data: map[string]any{"hba1c": float64(g.Between(58, 95)) / 10, "footExam": "Normal"}}
	case "ORL":
		return &consultations.UpsertConsultationSpecialtyRequest{SpecialtyCode: "ORL", Data: map[string]any{"otoscopy": Pick(g, []string{"Normale", "Tympan inflammatoire"}), "hearing": "Conservée"}}
	}
	return nil
}

func (w *World) reasonIDs(codes []string) []uint {
	var out []uint
	for _, c := range codes {
		for _, r := range w.Ref.Reasons {
			if r.Code == c {
				out = append(out, r.ID)
				break
			}
		}
	}
	return out
}

func intPtr(i int) *int { return &i }

var posologies = []string{"1 comprimé matin et soir", "1 comprimé 3 fois par jour après les repas", "2 comprimés le soir", "1 sachet par jour", "1 application matin et soir", "1 comprimé si douleur, maximum 4 par jour"}
var followUps = []string{"Contrôle dans 15 jours", "Revoir avec les résultats", "Contrôle dans 3 mois", "Pas de suivi nécessaire", "Rendez-vous de suivi programmé"}
