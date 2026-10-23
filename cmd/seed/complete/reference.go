package complete

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/act_catalog"
	"github.com/lallene/medcore-his/backend/internal/modules/billing"
	"github.com/lallene/medcore-his/backend/internal/modules/cash"
	"github.com/lallene/medcore-his/backend/internal/modules/consultations"
	"github.com/lallene/medcore-his/backend/internal/modules/hospitalizations"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/company"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/guarantor"
	"github.com/lallene/medcore-his/backend/internal/modules/patient_queue"
	"github.com/lallene/medcore-his/backend/internal/modules/performed_acts"
	"github.com/lallene/medcore-his/backend/internal/modules/pharmacy"
)

// refState holds the reference configuration used by every journey.
type refState struct {
	Exams         map[string]consultations.MedicalExam // by code
	LabExams      []string
	ImagingExams  []string
	OtherExams    []string
	Reasons       []consultations.ConsultationReason
	Areas         []consultations.PhysicalExamArea
	Acts          map[string]act_catalog.Entry // by act code
	ExamAct       map[string]string            // exam code -> act code
	Tariffs       map[string][]billing.Tariff  // "PERFORMED_ACT:<entry>" | "MEDICATION:<presentation>" | "HOSPITALIZATION"
	Presentations []pharmacy.MedicationPresentation
	Registers     []cash.Register
	ApptTypes     map[string]patient_queue.AppointmentType // by code
	Rooms         map[string][]hospitalizations.Room       // by service code
	Beds          map[string][]hospitalizations.Bed        // by service code
	Companies     []company.InsuranceCompany
	Guarantors    map[uint][]guarantor.InsuranceGuarantor // by company
}

type referencePlanner struct{}

func (referencePlanner) Name() string        { return "Référentiels" }
func (referencePlanner) PlanDay(*World, Day) {}

// Additional exams created through the consultations reference service.
var extraExams = []consultations.CreateReferenceRequest{
	{Code: "MALARIA_RDT", Name: "TDR paludisme", Category: "Parasitologie"},
	{Code: "LIPID_PANEL", Name: "Bilan lipidique", Category: "Biochimie"},
	{Code: "URINE_CBU", Name: "ECBU", Category: "Microbiologie"},
	{Code: "BLOOD_CULTURE", Name: "Hémoculture", Category: "Microbiologie"},
	{Code: "TSH", Name: "TSH", Category: "Biochimie"},
	{Code: "BLOOD_GROUP", Name: "Groupage sanguin ABO-Rh", Category: "Hématologie"},
	{Code: "WIDAL", Name: "Sérodiagnostic de Widal", Category: "Immunologie"},
	{Code: "IONOGRAM", Name: "Ionogramme sanguin", Category: "Biochimie"},
	{Code: "BHCG", Name: "β-HCG plasmatique", Category: "Biochimie"},
	{Code: "UREA", Name: "Urée sanguine", Category: "Biochimie"},
	{Code: "XR_KNEE", Name: "Radiographie du genou", Category: "Imagerie"},
	{Code: "XR_SPINE", Name: "Radiographie du rachis lombaire", Category: "Imagerie"},
	{Code: "XR_LIMB", Name: "Radiographie de membre", Category: "Imagerie"},
	{Code: "PELVIC_ULTRASOUND", Name: "Échographie pelvienne", Category: "Imagerie"},
	{Code: "THYROID_ULTRASOUND", Name: "Échographie thyroïdienne", Category: "Imagerie"},
	{Code: "CT_BRAIN", Name: "Scanner cérébral", Category: "Imagerie"},
	{Code: "CT_ABDOMEN", Name: "Scanner abdomino-pelvien", Category: "Imagerie"},
}

// examPrice returns a deterministic price (XOF) per exam family.
func examPrice(code, category string) int64 {
	switch {
	case strings.HasPrefix(code, "CT_"):
		return 85000
	case code == "MRI":
		return 180000
	case strings.Contains(code, "ULTRASOUND"):
		return 25000
	case strings.Contains(code, "XRAY") || strings.HasPrefix(code, "XR_"):
		return 15000
	case code == "BLOOD_CULTURE" || code == "URINE_CBU":
		return 12000
	case code == "HBA1C" || code == "TSH" || code == "LIPID_PANEL" || code == "LIVER_PANEL" || code == "BHCG":
		return 9000
	case strings.EqualFold(category, "Imagerie"):
		return 20000
	default:
		return 4500
	}
}

func (referencePlanner) Setup(w *World) error {
	admin := w.Staff.Admin.UserID
	r := &refState{Exams: map[string]consultations.MedicalExam{}, Acts: map[string]act_catalog.Entry{}, ExamAct: map[string]string{},
		Tariffs: map[string][]billing.Tariff{}, ApptTypes: map[string]patient_queue.AppointmentType{},
		Rooms: map[string][]hospitalizations.Room{}, Beds: map[string][]hospitalizations.Bed{}, Guarantors: map[uint][]guarantor.InsuranceGuarantor{}}
	w.Ref = r

	// Clinical references: module seeders (as the API does), legacy catalogs, extra exams.
	consultations.SeedConsultationReferences(w.DB)
	if err := consultations.SeedPhysicalExamAreas(w.DB); err != nil {
		return err
	}
	if w.Opts.ReferenceCatalogs != nil {
		w.Opts.ReferenceCatalogs(w.DB)
	}
	for _, e := range extraExams {
		if _, err := w.Svc.Consult.CreateExam(e); err != nil && !isDuplicateErr(err) {
			return wrapf(err, "examen %s", e.Code)
		}
	}
	exams, err := w.Svc.Consult.GetExams()
	if err != nil {
		return err
	}
	for _, e := range exams {
		r.Exams[e.Code] = e
		switch {
		case isLabCategory(e.Category):
			r.LabExams = append(r.LabExams, e.Code)
		case strings.EqualFold(strings.TrimSpace(e.Category), "Imagerie"):
			if e.Code != "MRI" { // no MRI equipment: never ordered, left without producer map
				r.ImagingExams = append(r.ImagingExams, e.Code)
			}
		default:
			r.OtherExams = append(r.OtherExams, e.Code)
		}
	}
	if r.Reasons, err = w.Svc.Consult.GetReasons(); err != nil {
		return err
	}
	if r.Areas, err = w.Svc.Consult.GetPhysicalExamAreas(); err != nil {
		return err
	}

	// Act catalogue (+ inactive / non-billable / non-eligible entries for the admin screens).
	addAct := func(code, label, category string, price int64, billable, eligible, active bool) error {
		e, err := w.Svc.ActCatalog.Create(act_catalog.CreateRequest{Code: code, Label: label, Category: category, BasePrice: price,
			Currency: "XOF", Billable: boolPtr(billable), InsuranceEligible: boolPtr(eligible), IsActive: boolPtr(active)}, admin)
		if err != nil {
			return wrapf(err, "acte %s", code)
		}
		if !billable || !eligible || !active {
			// Product defect (reported): Create persists false flags as true (GORM
			// default:true on a zero bool). Same sequence as an operator: create, then edit.
			if e, err = w.Svc.ActCatalog.Update(e.ID, act_catalog.UpdateRequest{Label: label, Category: category, BasePrice: price, Currency: "XOF",
				Billable: boolPtr(billable), InsuranceEligible: boolPtr(eligible), IsActive: boolPtr(active)}, admin); err != nil {
				return wrapf(err, "acte %s (drapeaux)", code)
			}
		}
		r.Acts[code] = *e
		return nil
	}
	actSpecs := []struct {
		code, label, cat           string
		price                      int64
		billable, eligible, active bool
	}{
		{"CS-STD", "Consultation médicale", "CONSULTATION", 10000, true, true, true},
		{"HOSP-NUIT-STD", "Nuitée d'hospitalisation — chambre commune", "HOSPITALIZATION", 25000, true, true, true},
		{"HOSP-NUIT-IND", "Nuitée d'hospitalisation — chambre individuelle", "HOSPITALIZATION", 45000, true, true, true},
		{"HOSP-NUIT-REA", "Nuitée en réanimation", "HOSPITALIZATION", 90000, true, true, true},
		{"SOINS-INF", "Soins infirmiers", "PROCEDURE", 3000, true, true, true},
		{"PANSEMENT", "Pansement simple", "PROCEDURE", 5000, true, true, true},
		{"PERFUSION", "Pose de perfusion", "PROCEDURE", 7500, true, true, true},
		{"ACCOUCHEMENT", "Accouchement par voie basse", "PROCEDURE", 150000, true, true, true},
		{"PETITE-CHIR", "Petite chirurgie (suture, incision)", "PROCEDURE", 45000, true, true, true},
		{"CHIR-INTERV", "Intervention chirurgicale programmée", "PROCEDURE", 350000, true, true, true},
		{"KINE", "Séance de kinésithérapie", "OTHER", 8000, true, false, true},
		{"CERTIF-MED", "Certificat médical", "OTHER", 5000, true, false, true},
		{"SOINS-CONFORT", "Soins de confort (non facturés)", "OTHER", 0, false, false, true},
		{"ACTE-RETIRE", "Ancien acte retiré du catalogue", "PROCEDURE", 12000, true, true, false},
	}
	for _, a := range actSpecs {
		if err := addAct(a.code, a.label, a.cat, a.price, a.billable, a.eligible, a.active); err != nil {
			return err
		}
	}
	for _, code := range append(append([]string{}, r.LabExams...), r.ImagingExams...) {
		e := r.Exams[code]
		cat := "LABORATORY"
		prefix := "LAB-"
		if !isLabCategory(e.Category) {
			cat, prefix = "IMAGING", "IMG-"
		}
		act := prefix + code
		if err := addAct(act, e.Name, cat, examPrice(code, e.Category), true, true, true); err != nil {
			return err
		}
		r.ExamAct[code] = act
	}

	// Producer maps: consultation default + one per lab/imaging exam.
	maps := []performed_acts.UpsertProducerMapRequest{{SourceType: performed_acts.SourceConsultation, ClinicalKey: performed_acts.ConsultationClinicalKey, ActCatalogEntryID: r.Acts["CS-STD"].ID}}
	for exam, act := range r.ExamAct {
		src := performed_acts.SourceLaboratory
		if strings.HasPrefix(act, "IMG-") {
			src = performed_acts.SourceImaging
		}
		maps = append(maps, performed_acts.UpsertProducerMapRequest{SourceType: src, ClinicalKey: exam, ActCatalogEntryID: r.Acts[act].ID})
	}
	sortMaps(maps)
	for _, m := range maps {
		if _, err := w.Svc.Acts.UpsertProducerMap(m, admin); err != nil {
			return wrapf(err, "producer map %s/%s", m.SourceType, m.ClinicalKey)
		}
	}

	// Tariffs. Validity starts before the simulated year; one historical tariff expired.
	from := w.Cfg.FirstDay().AddDate(0, 0, -30).Format("2006-01-02")
	addTariff := func(key, actType string, ref *uint, code, label string, price int64, effFrom, effTo string, active bool) error {
		t, err := w.Svc.Billing.CreateTariff(billing.TariffRequest{ActType: actType, ReferenceID: ref, Code: code, Label: label, UnitPrice: price, EffectiveFrom: effFrom, EffectiveTo: effTo, IsActive: boolPtr(active)}, admin)
		if err != nil {
			return wrapf(err, "tarif %s", code)
		}
		r.Tariffs[key] = append(r.Tariffs[key], *t)
		return nil
	}
	actCodes := make([]string, 0, len(r.Acts))
	for code := range r.Acts {
		actCodes = append(actCodes, code)
	}
	sort.Strings(actCodes) // deterministic creation order (and ids)
	for _, code := range actCodes {
		e := r.Acts[code]
		if !e.Billable || !e.IsActive {
			continue
		}
		ref := e.ID
		key := fmt.Sprintf("PERFORMED_ACT:%d", e.ID)
		if code == "CS-STD" {
			// Several eligible tariffs for the consultation act (server-authoritative choice).
			for _, t := range []struct {
				c, l string
				p    int64
			}{{"TAR-CS-GEN", "Consultation de médecine générale", 10000}, {"TAR-CS-SPE", "Consultation spécialisée", 15000}, {"TAR-CS-URG", "Consultation d'urgence", 20000}} {
				if err := addTariff(key, "PERFORMED_ACT", &ref, t.c, t.l, t.p, from, "", true); err != nil {
					return err
				}
			}
			histTo := w.Cfg.FirstDay().AddDate(0, 0, 60).Format("2006-01-02")
			if err := addTariff("history", "PERFORMED_ACT", &ref, "TAR-CS-GEN-2025", "Consultation (ancien tarif)", 8000, w.Cfg.FirstDay().AddDate(-1, 0, 0).Format("2006-01-02"), histTo, true); err != nil {
				return err
			}
			continue
		}
		if err := addTariff(key, "PERFORMED_ACT", &ref, "TAR-"+code, e.Label, e.BasePrice, from, "", true); err != nil {
			return err
		}
	}
	if err := addTariff("HOSPITALIZATION", "HOSPITALIZATION", nil, "TAR-HOSP-ADM", "Frais d'admission", 15000, from, "", true); err != nil {
		return err
	}
	pres, err := w.Svc.Pharmacy.GetPresentations()
	if err != nil {
		return err
	}
	pr := w.RNG.Stream("tariffs.medication")
	var activeMeds []uint
	if err := w.DB.Table("medication_presentations p").Joins("JOIN medications m ON m.id = p.medication_id").
		Where("p.is_active AND m.is_active").Pluck("p.id", &activeMeds).Error; err != nil {
		return err
	}
	usable := map[uint]bool{}
	for _, id := range activeMeds {
		usable[id] = true
	}
	for _, p := range pres {
		if !usable[p.ID] {
			continue
		}
		r.Presentations = append(r.Presentations, p)
		ref := p.ID
		price := int64(pr.Between(3, 60)) * 250
		if err := addTariff(fmt.Sprintf("MEDICATION:%d", p.ID), "MEDICATION", &ref, "TAR-MED-"+p.Code, "Médicament "+p.Code, price, from, "", true); err != nil {
			return err
		}
	}

	// Cash registers.
	for _, reg := range []cash.RegisterRequest{
		{Code: "CAISSE-PRINC", Name: "Caisse principale", Location: "Hall d'accueil"},
		{Code: "CAISSE-URG", Name: "Caisse des urgences", Location: "Urgences"},
		{Code: "CAISSE-MAT", Name: "Caisse maternité", Location: "Pôle mère-enfant"},
	} {
		x, err := w.Svc.Cash.SaveRegister(0, reg, admin)
		if err != nil {
			return wrapf(err, "caisse %s", reg.Code)
		}
		r.Registers = append(r.Registers, *x)
	}

	if err := setupScheduling(w, r); err != nil {
		return err
	}
	if err := setupBeds(w, r); err != nil {
		return err
	}
	if err := setupInsurers(w, r); err != nil {
		return err
	}
	return w.Svc.Ticketing.SeedDefaults(admin)
}

func isLabCategory(c string) bool {
	switch strings.ToLower(strings.TrimSpace(c)) {
	case "laboratoire", "biologie", "biochimie", "hématologie", "hematologie", "microbiologie", "immunologie", "parasitologie":
		return true
	}
	return false
}

func sortMaps(m []performed_acts.UpsertProducerMapRequest) {
	for i := 1; i < len(m); i++ {
		for j := i; j > 0 && m[j].SourceType+m[j].ClinicalKey < m[j-1].SourceType+m[j-1].ClinicalKey; j-- {
			m[j], m[j-1] = m[j-1], m[j]
		}
	}
}

func boolPtr(b bool) *bool { return &b }

func isDuplicateErr(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "duplicate") || strings.Contains(s, "existe déjà") || strings.Contains(s, "23505")
}

// ---------------------------------------------------------------------------------------
// Scheduling configuration: appointment types, working schedules (+ one ended schedule).

var apptTypeSpecs = []struct {
	code, name string
	minutes    int
	service    string
}{
	{"CONSULT", "Consultation", 30, ""},
	{"CTRL", "Contrôle", 15, ""},
	{"URG-CONSULT", "Consultation urgences", 20, "URG"},
	{"CPN", "Consultation prénatale", 30, "MAT"},
	{"PED-SUIVI", "Suivi pédiatrique", 20, "PED"},
	{"CARD-SUIVI", "Suivi cardiologique", 30, "CARD"},
	{"DIAB-SUIVI", "Suivi diabétologique", 30, "DIAB"},
	{"CHIR-PREOP", "Consultation pré-opératoire", 45, "CHIR"},
	{"ANCIEN-TYPE", "Ancien type (désactivé)", 30, ""},
}

func setupScheduling(w *World, r *refState) error {
	a := w.Staff.Admin.QueueAccess(w)
	for _, s := range apptTypeSpecs {
		req := patient_queue.CreateAppointmentTypeRequest{Code: s.code, Name: s.name, DefaultDurationMinutes: s.minutes}
		if s.service != "" {
			id := w.Org.ServiceID(s.service)
			req.ServiceID = &id
		}
		t, err := w.Svc.Queue.CreateAppointmentType(req, a)
		if err != nil {
			return wrapf(err, "type de RDV %s", s.code)
		}
		if s.code == "ANCIEN-TYPE" {
			if t, err = w.Svc.Queue.DisableAppointmentType(t.ID, a); err != nil {
				return err
			}
		}
		r.ApptTypes[s.code] = *t
	}
	validFrom := w.Cfg.FirstDay().AddDate(0, 0, -45)
	physicians := w.Staff.With(func(m *Member) bool { return len(m.Specialties) > 0 && m.PrimaryService() != "" })
	for i, m := range physicians {
		svc := w.Org.ServiceID(m.PrimaryService())
		blocks := [][2]string{{"08:00", "12:30"}, {"14:00", "17:30"}}
		days := []int{1, 2, 3, 4, 5}
		if m.PrimaryService() == "URG" {
			blocks = [][2]string{{"07:00", "19:00"}}
			days = []int{0, 1, 2, 3, 4, 5, 6}
		} else if i%3 == 0 {
			days = append(days, 6) // Saturday morning clinic
		}
		if w.Clinic.workdays == nil {
			w.Clinic.workdays = map[uint]map[time.Weekday]bool{}
		}
		w.Clinic.workdays[m.UserID] = map[time.Weekday]bool{}
		for _, d := range days {
			w.Clinic.workdays[m.UserID][time.Weekday(d)] = true
			for bi, b := range blocks {
				if d == 6 && bi > 0 {
					continue
				}
				req := patient_queue.CreateWorkingScheduleRequest{PractitionerID: m.UserID, ServiceID: svc, Weekday: d, StartTime: b[0], EndTime: b[1], ValidFrom: validFrom}
				if _, err := w.Svc.Queue.CreateWorkingSchedule(req, a); err != nil {
					return wrapf(err, "planning %s", m.Email)
				}
			}
		}
		// Secondary-service schedule (multi-service practitioners): Thursday afternoon.
		if len(m.Services) > 1 && m.Services[1] != "ADMIN" {
			req := patient_queue.CreateWorkingScheduleRequest{PractitionerID: m.UserID, ServiceID: w.Org.ServiceID(m.Services[1]), Weekday: 4, StartTime: "18:00", EndTime: "20:00", ValidFrom: validFrom}
			if _, err := w.Svc.Queue.CreateWorkingSchedule(req, a); err != nil {
				return wrapf(err, "planning secondaire %s", m.Email)
			}
		}
	}
	// An ended schedule (former Saturday evening clinic) for the history screens.
	if len(physicians) > 0 {
		until := w.Cfg.FirstDay().AddDate(0, 3, 0)
		req := patient_queue.CreateWorkingScheduleRequest{PractitionerID: physicians[0].UserID, ServiceID: w.Org.ServiceID(physicians[0].PrimaryService()), Weekday: 6, StartTime: "15:00", EndTime: "18:00", ValidFrom: validFrom, ValidUntil: &until}
		if _, err := w.Svc.Queue.CreateWorkingSchedule(req, a); err != nil {
			return wrapf(err, "planning terminé")
		}
	}
	return nil
}

// ---------------------------------------------------------------------------------------

func setupBeds(w *World, r *refState) error {
	admin := w.Staff.Admin.UserID
	layout := []struct {
		svc, prefix, floor string
		rooms, beds        int
		roomType           string
	}{
		{"URG", "URG", "RDC", 1, 3, "OBSERVATION"},
		{"GEN", "MED", "1", 2, 3, "COMMUNE"},
		{"SOINS", "SOI", "1", 2, 2, "COMMUNE"},
		{"REA", "REA", "2", 1, 2, "REANIMATION"},
		{"CHIR", "CHI", "2", 2, 2, "INDIVIDUELLE"},
		{"PED", "PED", "3", 2, 2, "COMMUNE"},
		{"MAT", "MAT", "3", 2, 2, "INDIVIDUELLE"},
	}
	for _, l := range layout {
		svcID := w.Org.ServiceID(l.svc)
		for i := 1; i <= l.rooms; i++ {
			room, err := w.Svc.Hosp.CreateRoom(hospitalizations.CreateRoomRequest{Code: fmt.Sprintf("%s-%d%02d", l.prefix, i, i), Name: fmt.Sprintf("Chambre %s %d", l.prefix, i), ServiceID: &svcID, Floor: l.floor, RoomType: l.roomType}, admin)
			if err != nil {
				return wrapf(err, "chambre %s", l.prefix)
			}
			r.Rooms[l.svc] = append(r.Rooms[l.svc], *room)
			for b := 1; b <= l.beds; b++ {
				bed, err := w.Svc.Hosp.CreateBed(hospitalizations.CreateBedRequest{RoomID: room.ID, Code: fmt.Sprintf("%s-L%d", room.Code, b), Label: fmt.Sprintf("Lit %d", b), BedType: "STANDARD"}, admin)
				if err != nil {
					return wrapf(err, "lit")
				}
				r.Beds[l.svc] = append(r.Beds[l.svc], *bed)
			}
		}
	}
	// Out-of-service beds (maintenance) through the bed service.
	for _, svc := range []string{"GEN", "CHIR"} {
		beds := r.Beds[svc]
		if len(beds) == 0 {
			continue
		}
		status := hospitalizations.BedOutOfService
		if _, err := w.Svc.Hosp.UpdateBed(beds[len(beds)-1].ID, hospitalizations.UpdateBedRequest{Status: &status}, admin); err != nil {
			return wrapf(err, "lit hors service")
		}
		r.Beds[svc] = beds[:len(beds)-1]
	}
	return nil
}

// ---------------------------------------------------------------------------------------

var insurerSpecs = []struct {
	code, name, city string
	active           bool
	guarantors       []struct {
		code, name string
		rate       float64
		delay      int
	}
}{
	{"CNAM", "Caisse Nationale d'Assurance Maladie", "Abidjan", true, []struct {
		code, name string
		rate       float64
		delay      int
	}{{"CNAM-CMU", "Couverture Maladie Universelle", 70, 90}, {"CNAM-FP", "Fonction publique", 80, 60}}},
	{"ALLIANZ", "Allianz Côte d'Ivoire", "Abidjan", true, []struct {
		code, name string
		rate       float64
		delay      int
	}{{"ALZ-ENT-A", "Groupe industriel Lagunaire", 90, 45}, {"ALZ-ENT-B", "Banque Atlantique Plateau", 80, 30}}},
	{"NSIA", "NSIA Assurances", "Abidjan", true, []struct {
		code, name string
		rate       float64
		delay      int
	}{{"NSIA-PORT", "Port Autonome (salariés)", 85, 60}}},
	{"SUNU", "SUNU Assurances Vie & Santé", "Abidjan", true, []struct {
		code, name string
		rate       float64
		delay      int
	}{{"SUNU-IND", "Contrats individuels", 70, 45}}},
	{"MCI", "Mutuelle des Cadres Ivoiriens", "Abidjan", true, []struct {
		code, name string
		rate       float64
		delay      int
	}{{"MCI-STD", "Formule standard", 75, 30}}},
	{"ASSUR-ANCIEN", "Assureur résilié", "Bouaké", false, nil},
}

func setupInsurers(w *World, r *refState) error {
	for _, s := range insurerSpecs {
		c, err := w.Svc.Company.Create(company.CreateCompanyRequest{Code: s.code, Name: s.name, Description: "Assureur DEMO (fictif)", City: s.city, Country: "Côte d'Ivoire", Phone: "+225 27 20 00 00 00"})
		if err != nil {
			return wrapf(err, "assureur %s", s.code)
		}
		for _, g := range s.guarantors {
			x, err := w.Svc.Guarantor.Create(guarantor.CreateGuarantorRequest{CompanyID: c.ID, Code: g.code, Name: g.name, DefaultCoverageRate: g.rate, PaymentDelayDays: g.delay})
			if err != nil {
				return wrapf(err, "garant %s", g.code)
			}
			r.Guarantors[c.ID] = append(r.Guarantors[c.ID], *x)
		}
		if !s.active {
			if c, err = w.Svc.Company.Update(c.ID, company.UpdateCompanyRequest{IsActive: boolPtr(false)}); err != nil {
				return err
			}
		} else {
			r.Companies = append(r.Companies, *c)
		}
	}
	return nil
}
