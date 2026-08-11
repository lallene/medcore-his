package main

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"gorm.io/gorm"

	"github.com/lallene/medcore-his/backend/internal/modules/consultations"
	"github.com/lallene/medcore-his/backend/internal/modules/medical_records"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
	"github.com/lallene/medcore-his/backend/internal/modules/pharmacy"
)

const demoSeedUserID uint = 1

var demoAnchorDate = time.Date(
	2026,
	time.July,
	12,
	12,
	0,
	0,
	0,
	time.Local,
)

type clinicalPatientScenario struct {
	LastName        string
	FirstNames      string
	Sex             string
	BirthDate       time.Time
	Phone           string
	Address         string
	Contact         string
	IsInsured       bool
	CoverageRate    float64
	InsuranceNumber string

	Email          string
	Profession     string
	MaritalStatus  string
	BloodGroup     string
	Rhesus         string
	InsuranceName  string
	MutualName     string
	EmergencyName  string
	EmergencyPhone string

	Tobacco          string
	Alcohol          string
	PhysicalActivity string
	Diet             string
}

type consultationSeed struct {
	Key                     string
	DaysAgo                 int
	Hour                    int
	Service                 string
	Doctor                  string
	Status                  string
	Diagnosis               string
	Observations            string
	Treatment               string
	HospitalizationRequired bool
	HospitalizationReason   string
	HospitalizationType     string
	HospitalizationDuration int

	ReasonCodes []string
	ExamCodes   []string

	Temperature        float64
	SystolicBP         int
	DiastolicBP        int
	HeartRate          int
	RespiratoryRate    int
	OxygenSaturation   int
	Weight             float64
	Height             float64
	BloodGlucose       float64
	PainScore          int
	Prescriptions      []prescriptionSeed
	SpecialtyCode      string
	SpecialtyData      map[string]any
	ChiefComplaint     string
	PresentIllness     string
	AssociatedSymptoms string
	GeneralAppearance  string
	Consciousness      string
	PhysicalSummary    string
	ClinicalImpression string
	InvestigationPlan  string
	FollowUpPlan       string
	PatientAdvice      string
	Disposition        string
}

type prescriptionSeed struct {
	PresentationCode string
	MedicationName   string
	Dosage           string
	Form             string
	Route            string
	Quantity         float64
	Frequency        string
	Duration         string
	Instructions     string
}

func seedClinicalDemo(
	db *gorm.DB,
	patientItems []patients.Patient,
) {
	if len(patientItems) < 8 {
		log.Println("Seed clinique ignoré : au moins 8 patients sont nécessaires")
		return
	}

	log.Println("Création des données cliniques de démonstration...")

	scenarioPatients := configureClinicalPatients(
		db,
		patientItems[:8],
	)

	reasons := seedConsultationReasonCatalog(db)
	exams := seedMedicalExams(db)
	presentations := seedPharmacyCatalog(db)

	for index := range scenarioPatients {
		patient := &scenarioPatients[index]

		record := seedMedicalRecord(db, *patient)

		seedMedicalProfile(db, *patient, record)
		seedLifestyle(db, *patient, record)

		switch index {
		case 0:
			seedStablePatient(
				db,
				*patient,
				record,
				reasons,
				exams,
				presentations,
			)

		case 1:
			seedCardiologyDiabetesPatient(
				db,
				*patient,
				record,
				reasons,
				exams,
				presentations,
			)

		case 2:
			seedCriticalAllergyPatient(
				db,
				*patient,
				record,
				reasons,
				exams,
				presentations,
			)

		case 3:
			seedMaternityPatient(
				db,
				*patient,
				record,
				reasons,
				exams,
				presentations,
			)

		case 4:
			seedPediatricPatient(
				db,
				*patient,
				record,
				reasons,
				exams,
				presentations,
			)

		case 5:
			seedEmergencyPatient(
				db,
				*patient,
				record,
				reasons,
				exams,
				presentations,
			)

		case 6:
			seedSurgeryPatient(
				db,
				*patient,
				record,
				reasons,
				exams,
				presentations,
			)

		case 7:
			seedENTPatient(
				db,
				*patient,
				record,
				reasons,
				exams,
				presentations,
			)
		}
	}

	log.Println("Données cliniques de démonstration créées")
}

func configureClinicalPatients(
	db *gorm.DB,
	items []patients.Patient,
) []patients.Patient {
	now := time.Now()

	scenarios := []clinicalPatientScenario{
		{
			LastName:         "N'GUESSAN",
			FirstNames:       "Emmanuel",
			Sex:              "M",
			BirthDate:        now.AddDate(-34, -2, 0),
			Phone:            "+2250700000001",
			Address:          "Abobo",
			Contact:          "Grâce N'Guessan - +2250501010101",
			IsInsured:        true,
			CoverageRate:     80,
			InsuranceNumber:  "CNAM-000001",
			Email:            "emmanuel.nguessan@demo.local",
			Profession:       "Informaticien",
			MaritalStatus:    "Marié",
			BloodGroup:       "O",
			Rhesus:           "+",
			InsuranceName:    "CNAM",
			MutualName:       "Mutuelle entreprise",
			EmergencyName:    "Grâce N'Guessan",
			EmergencyPhone:   "+2250501010101",
			Tobacco:          "Non-fumeur",
			Alcohol:          "Occasionnel",
			PhysicalActivity: "Modérée",
			Diet:             "Alimentation variée",
		},
		{
			LastName:         "KOUASSI",
			FirstNames:       "Jean Marc",
			Sex:              "M",
			BirthDate:        now.AddDate(-58, -5, 0),
			Phone:            "+2250700000002",
			Address:          "Cocody",
			Contact:          "Awa Kouassi - +2250502020202",
			IsInsured:        true,
			CoverageRate:     90,
			InsuranceNumber:  "NSIA-000002",
			Email:            "jean.kouassi@demo.local",
			Profession:       "Comptable",
			MaritalStatus:    "Marié",
			BloodGroup:       "A",
			Rhesus:           "+",
			InsuranceName:    "NSIA",
			MutualName:       "MUGEFCI",
			EmergencyName:    "Awa Kouassi",
			EmergencyPhone:   "+2250502020202",
			Tobacco:          "Ancien fumeur",
			Alcohol:          "Occasionnel",
			PhysicalActivity: "Faible",
			Diet:             "Régime pauvre en sel et en sucres",
		},
		{
			LastName:         "KONE",
			FirstNames:       "Mariam",
			Sex:              "F",
			BirthDate:        now.AddDate(-42, -1, 0),
			Phone:            "+2250700000003",
			Address:          "Yopougon",
			Contact:          "Ibrahim Koné - +2250503030303",
			IsInsured:        true,
			CoverageRate:     70,
			InsuranceNumber:  "SUNU-000003",
			Email:            "mariam.kone@demo.local",
			Profession:       "Enseignante",
			MaritalStatus:    "Mariée",
			BloodGroup:       "B",
			Rhesus:           "+",
			InsuranceName:    "SUNU",
			EmergencyName:    "Ibrahim Koné",
			EmergencyPhone:   "+2250503030303",
			Tobacco:          "Non-fumeuse",
			Alcohol:          "Aucun",
			PhysicalActivity: "Modérée",
			Diet:             "Sans particularité",
		},
		{
			LastName:         "TRAORE",
			FirstNames:       "Fatou",
			Sex:              "F",
			BirthDate:        now.AddDate(-29, -7, 0),
			Phone:            "+2250700000004",
			Address:          "Bingerville",
			Contact:          "Moussa Traoré - +2250504040404",
			IsInsured:        true,
			CoverageRate:     100,
			InsuranceNumber:  "AXA-000004",
			Email:            "fatou.traore@demo.local",
			Profession:       "Juriste",
			MaritalStatus:    "Mariée",
			BloodGroup:       "O",
			Rhesus:           "-",
			InsuranceName:    "AXA",
			EmergencyName:    "Moussa Traoré",
			EmergencyPhone:   "+2250504040404",
			Tobacco:          "Non-fumeuse",
			Alcohol:          "Aucun",
			PhysicalActivity: "Marche régulière",
			Diet:             "Alimentation adaptée à la grossesse",
		},
		{
			LastName:         "YAO",
			FirstNames:       "Junior",
			Sex:              "M",
			BirthDate:        now.AddDate(-7, -3, 0),
			Phone:            "+2250700000005",
			Address:          "Marcory",
			Contact:          "Nadia Yao - +2250505050505",
			IsInsured:        true,
			CoverageRate:     80,
			InsuranceNumber:  "CNAM-000005",
			Email:            "parent.junior.yao@demo.local",
			Profession:       "Élève",
			MaritalStatus:    "Mineur",
			BloodGroup:       "AB",
			Rhesus:           "+",
			InsuranceName:    "CNAM",
			EmergencyName:    "Nadia Yao",
			EmergencyPhone:   "+2250505050505",
			Tobacco:          "Non applicable",
			Alcohol:          "Non applicable",
			PhysicalActivity: "Active",
			Diet:             "Alimentation pédiatrique équilibrée",
		},
		{
			LastName:         "BAMBA",
			FirstNames:       "Serge",
			Sex:              "M",
			BirthDate:        now.AddDate(-47, -8, 0),
			Phone:            "+2250700000006",
			Address:          "Treichville",
			Contact:          "Patrick Bamba - +2250506060606",
			IsInsured:        false,
			CoverageRate:     0,
			Email:            "serge.bamba@demo.local",
			Profession:       "Chauffeur",
			MaritalStatus:    "Marié",
			BloodGroup:       "A",
			Rhesus:           "-",
			EmergencyName:    "Patrick Bamba",
			EmergencyPhone:   "+2250506060606",
			Tobacco:          "10 cigarettes par jour",
			Alcohol:          "Régulier",
			PhysicalActivity: "Faible",
			Diet:             "Alimentation riche en sel",
		},
		{
			LastName:         "KOFFI",
			FirstNames:       "Grace",
			Sex:              "F",
			BirthDate:        now.AddDate(-36, -4, 0),
			Phone:            "+2250700000007",
			Address:          "Cocody",
			Contact:          "Eric Koffi - +2250507070707",
			IsInsured:        true,
			CoverageRate:     90,
			InsuranceNumber:  "ALLIANZ-000007",
			Email:            "grace.koffi@demo.local",
			Profession:       "Entrepreneure",
			MaritalStatus:    "Célibataire",
			BloodGroup:       "B",
			Rhesus:           "-",
			InsuranceName:    "ALLIANZ",
			EmergencyName:    "Eric Koffi",
			EmergencyPhone:   "+2250507070707",
			Tobacco:          "Non-fumeuse",
			Alcohol:          "Occasionnel",
			PhysicalActivity: "Modérée",
			Diet:             "Alimentation équilibrée",
		},
		{
			LastName:         "DIABATE",
			FirstNames:       "Nadia",
			Sex:              "F",
			BirthDate:        now.AddDate(-31, -11, 0),
			Phone:            "+2250700000008",
			Address:          "Yopougon",
			Contact:          "Mamadou Diabaté - +2250508080808",
			IsInsured:        true,
			CoverageRate:     70,
			InsuranceNumber:  "ASCOMA-000008",
			Email:            "nadia.diabate@demo.local",
			Profession:       "Commerciale",
			MaritalStatus:    "Célibataire",
			BloodGroup:       "O",
			Rhesus:           "+",
			InsuranceName:    "ASCOMA",
			EmergencyName:    "Mamadou Diabaté",
			EmergencyPhone:   "+2250508080808",
			Tobacco:          "Non-fumeuse",
			Alcohol:          "Aucun",
			PhysicalActivity: "Modérée",
			Diet:             "Sans particularité",
		},
	}

	result := make([]patients.Patient, 0, len(scenarios))

	for index, scenario := range scenarios {
		patient := items[index]
		age := calculateAge(scenario.BirthDate)

		updates := map[string]any{
			"nom":              scenario.LastName,
			"prenoms":          scenario.FirstNames,
			"sexe":             scenario.Sex,
			"date_naissance":   scenario.BirthDate,
			"age":              age,
			"telephone":        scenario.Phone,
			"quartier":         scenario.Address,
			"personne_contact": scenario.Contact,
			"is_assure":        scenario.IsInsured,
			"taux_couverture":  scenario.CoverageRate,
			"matricule_assure": scenario.InsuranceNumber,
		}

		must(db.Model(&patient).Updates(updates).Error)

		must(db.First(&patient, patient.ID).Error)

		result = append(result, patient)
	}

	return result
}

func seedMedicalRecord(
	db *gorm.DB,
	patient patients.Patient,
) medical_records.MedicalRecord {
	record := medical_records.MedicalRecord{
		PatientID:    patient.ID,
		RecordNumber: fmt.Sprintf("DM-2026-%04d", patient.ID),
		Status:       "active",
	}

	must(
		db.
			Where("patient_id = ?", patient.ID).
			Assign(map[string]any{
				"record_number": record.RecordNumber,
				"status":        record.Status,
			}).
			FirstOrCreate(&record).
			Error,
	)

	return record
}

func seedMedicalProfile(
	db *gorm.DB,
	patient patients.Patient,
	record medical_records.MedicalRecord,
) {
	scenario := clinicalScenarioForPatient(patient)

	profile := medical_records.PatientMedicalProfile{
		MedicalRecordID: record.ID,
		PatientID:       patient.ID,

		Email:         scenario.Email,
		Address:       scenario.Address,
		MaritalStatus: scenario.MaritalStatus,
		Profession:    scenario.Profession,

		EmergencyContactFirstName:    scenario.EmergencyName,
		EmergencyContactRelationship: "Proche",
		EmergencyContactPhone:        scenario.EmergencyPhone,

		LegalGuardianName:         scenario.EmergencyName,
		LegalGuardianRelationship: "Parent ou proche",
		LegalGuardianPhone:        scenario.EmergencyPhone,
		LegalGuardianAddress:      scenario.Address,

		InsuranceName:        scenario.InsuranceName,
		MutualName:           scenario.MutualName,
		InsuranceNumber:      scenario.InsuranceNumber,
		CoverageOrganization: scenario.InsuranceName,

		BloodGroup: scenario.BloodGroup,
		Rhesus:     scenario.Rhesus,
		UpdatedBy:  demoSeedUserID,
	}

	must(
		db.
			Where("medical_record_id = ?", record.ID).
			Assign(profile).
			FirstOrCreate(&profile).
			Error,
	)
}

func seedLifestyle(
	db *gorm.DB,
	patient patients.Patient,
	record medical_records.MedicalRecord,
) {
	scenario := clinicalScenarioForPatient(patient)

	lifestyle := medical_records.Lifestyle{
		MedicalRecordID:  record.ID,
		PatientID:        patient.ID,
		Tobacco:          scenario.Tobacco,
		Alcohol:          scenario.Alcohol,
		PhysicalActivity: scenario.PhysicalActivity,
		Diet:             scenario.Diet,
		UpdatedBy:        demoSeedUserID,
	}

	must(
		db.
			Where("medical_record_id = ?", record.ID).
			Assign(lifestyle).
			FirstOrCreate(&lifestyle).
			Error,
	)
}

func clinicalScenarioForPatient(
	patient patients.Patient,
) clinicalPatientScenario {
	scenarios := map[string]clinicalPatientScenario{
		"P0001": {
			Email: "emmanuel.nguessan@demo.local", Address: "Abobo",
			MaritalStatus: "Marié", Profession: "Informaticien",
			BloodGroup: "O", Rhesus: "+", InsuranceName: "CNAM",
			MutualName:      "Mutuelle entreprise",
			InsuranceNumber: "CNAM-000001",
			EmergencyName:   "Grâce N'Guessan",
			EmergencyPhone:  "+2250501010101",
			Tobacco:         "Non-fumeur", Alcohol: "Occasionnel",
			PhysicalActivity: "Modérée", Diet: "Alimentation variée",
		},
		"P0002": {
			Email: "jean.kouassi@demo.local", Address: "Cocody",
			MaritalStatus: "Marié", Profession: "Comptable",
			BloodGroup: "A", Rhesus: "+", InsuranceName: "NSIA",
			MutualName: "MUGEFCI", InsuranceNumber: "NSIA-000002",
			EmergencyName:  "Awa Kouassi",
			EmergencyPhone: "+2250502020202",
			Tobacco:        "Ancien fumeur", Alcohol: "Occasionnel",
			PhysicalActivity: "Faible",
			Diet:             "Régime pauvre en sel et en sucres",
		},
		"P0003": {
			Email: "mariam.kone@demo.local", Address: "Yopougon",
			MaritalStatus: "Mariée", Profession: "Enseignante",
			BloodGroup: "B", Rhesus: "+", InsuranceName: "SUNU",
			InsuranceNumber: "SUNU-000003",
			EmergencyName:   "Ibrahim Koné",
			EmergencyPhone:  "+2250503030303",
			Tobacco:         "Non-fumeuse", Alcohol: "Aucun",
			PhysicalActivity: "Modérée", Diet: "Sans particularité",
		},
		"P0004": {
			Email: "fatou.traore@demo.local", Address: "Bingerville",
			MaritalStatus: "Mariée", Profession: "Juriste",
			BloodGroup: "O", Rhesus: "-", InsuranceName: "AXA",
			InsuranceNumber: "AXA-000004",
			EmergencyName:   "Moussa Traoré",
			EmergencyPhone:  "+2250504040404",
			Tobacco:         "Non-fumeuse", Alcohol: "Aucun",
			PhysicalActivity: "Marche régulière",
			Diet:             "Alimentation adaptée à la grossesse",
		},
		"P0005": {
			Email: "parent.junior.yao@demo.local", Address: "Marcory",
			MaritalStatus: "Mineur", Profession: "Élève",
			BloodGroup: "AB", Rhesus: "+", InsuranceName: "CNAM",
			InsuranceNumber: "CNAM-000005",
			EmergencyName:   "Nadia Yao",
			EmergencyPhone:  "+2250505050505",
			Tobacco:         "Non applicable", Alcohol: "Non applicable",
			PhysicalActivity: "Active",
			Diet:             "Alimentation pédiatrique équilibrée",
		},
		"P0006": {
			Email: "serge.bamba@demo.local", Address: "Treichville",
			MaritalStatus: "Marié", Profession: "Chauffeur",
			BloodGroup: "A", Rhesus: "-",
			EmergencyName:  "Patrick Bamba",
			EmergencyPhone: "+2250506060606",
			Tobacco:        "10 cigarettes par jour", Alcohol: "Régulier",
			PhysicalActivity: "Faible",
			Diet:             "Alimentation riche en sel",
		},
		"P0007": {
			Email: "grace.koffi@demo.local", Address: "Cocody",
			MaritalStatus: "Célibataire", Profession: "Entrepreneure",
			BloodGroup: "B", Rhesus: "-", InsuranceName: "ALLIANZ",
			InsuranceNumber: "ALLIANZ-000007",
			EmergencyName:   "Eric Koffi",
			EmergencyPhone:  "+2250507070707",
			Tobacco:         "Non-fumeuse", Alcohol: "Occasionnel",
			PhysicalActivity: "Modérée",
			Diet:             "Alimentation équilibrée",
		},
		"P0008": {
			Email: "nadia.diabate@demo.local", Address: "Yopougon",
			MaritalStatus: "Célibataire", Profession: "Commerciale",
			BloodGroup: "O", Rhesus: "+", InsuranceName: "ASCOMA",
			InsuranceNumber: "ASCOMA-000008",
			EmergencyName:   "Mamadou Diabaté",
			EmergencyPhone:  "+2250508080808",
			Tobacco:         "Non-fumeuse", Alcohol: "Aucun",
			PhysicalActivity: "Modérée", Diet: "Sans particularité",
		},
	}

	return scenarios[patient.CodePatient]
}

func attachConsultationReasons(
	db *gorm.DB,
	consultation consultations.Consultation,
	codes []string,
	reasonCatalog map[string]consultations.ConsultationReason,
) {
	selected := make(
		[]consultations.ConsultationReason,
		0,
		len(codes),
	)

	for _, code := range codes {
		item, exists := reasonCatalog[code]
		if exists {
			selected = append(selected, item)
		}
	}

	if len(selected) == 0 {
		return
	}

	err := db.
		Model(&consultation).
		Association("Reasons").
		Replace(selected)

	must(err)
}

func seedMedicalExams(
	db *gorm.DB,
) map[string]consultations.MedicalExam {
	items := []consultations.MedicalExam{
		{Code: "CBC", Name: "Numération formule sanguine", Category: "Laboratoire", IsActive: true},
		{Code: "CRP", Name: "CRP", Category: "Laboratoire", IsActive: true},
		{Code: "FASTING_GLUCOSE", Name: "Glycémie à jeun", Category: "Laboratoire", IsActive: true},
		{Code: "HBA1C", Name: "HbA1c", Category: "Laboratoire", IsActive: true},
		{Code: "CREATININE", Name: "Créatinine", Category: "Laboratoire", IsActive: true},
		{Code: "LIVER_PANEL", Name: "Bilan hépatique", Category: "Laboratoire", IsActive: true},
		{Code: "ECG", Name: "Électrocardiogramme", Category: "Cardiologie", IsActive: true},
		{Code: "CARDIAC_ULTRASOUND", Name: "Échographie cardiaque", Category: "Imagerie", IsActive: true},
		{Code: "OBSTETRIC_ULTRASOUND", Name: "Échographie obstétricale", Category: "Imagerie", IsActive: true},
		{Code: "ABDOMINAL_ULTRASOUND", Name: "Échographie abdominale", Category: "Imagerie", IsActive: true},
		{Code: "CHEST_XRAY", Name: "Radiographie thoracique", Category: "Imagerie", IsActive: true},
		{Code: "AUDIOMETRY", Name: "Audiométrie", Category: "ORL", IsActive: true},
		{Code: "TYMPANOMETRY", Name: "Tympanométrie", Category: "ORL", IsActive: true},
	}

	result := make(map[string]consultations.MedicalExam)

	for _, item := range items {
		must(
			db.
				Where("code = ?", item.Code).
				Assign(item).
				FirstOrCreate(&item).
				Error,
		)

		result[item.Code] = item
	}

	return result
}

func seedPharmacyCatalog(
	db *gorm.DB,
) map[string]pharmacy.MedicationPresentation {
	type batchSeed struct {
		Number    string
		Quantity  float64
		DayOffset int
		Active    bool
	}
	type medicationSeed struct {
		FamilyCode         string
		FamilyName         string
		MedicationCode     string
		MedicationName     string
		GenericName        string
		PresentationCode   string
		Dosage             string
		Form               string
		Route              string
		Unit               string
		Packaging          string
		Stock              float64
		Threshold          float64
		MedicationActive   bool
		PresentationActive bool
		Batches            []batchSeed
	}
	type familySeed struct{ Code, Name string }

	families := []familySeed{{"ANALGESIC", "Antalgiques"}, {"ANTIINFLAMMATORY", "Anti-inflammatoires"}, {"ANTIBIOTIC", "Antibiotiques"}, {"DIABETES", "Antidiabétiques"}, {"CARDIO", "Cardiovasculaires"}, {"DIGESTIVE", "Gastro-entérologie"}, {"RESPIRATORY", "Respiratoire"}, {"ANTIHISTAMINE", "Antihistaminiques"}, {"CORTICOID", "Corticoïdes"}, {"ANTICOAGULANT", "Anticoagulants"}, {"ANTIEMETIC", "Antiémétiques"}, {"OTHER", "Autres"}}
	for _, f := range families {
		family := pharmacy.MedicationFamily{Code: f.Code, Name: f.Name, Description: "Référentiel DEMO MedCore", IsActive: true}
		must(db.Where("code = ?", f.Code).Assign(family).FirstOrCreate(&family).Error)
	}
	valid := func(q float64, days int, number string) []batchSeed { return []batchSeed{{number, q, days, true}} }
	items := []medicationSeed{
		{FamilyCode: "ANALGESIC", MedicationCode: "PARACETAMOL", MedicationName: "DOLIPRANE", GenericName: "Paracétamol", PresentationCode: "PARA-500-TAB", Dosage: "500 mg", Form: "Comprimé", Route: "Orale", Unit: "comprimé", Packaging: "Boîte de 16 comprimés", Stock: 320, Threshold: 50, MedicationActive: true, PresentationActive: true, Batches: []batchSeed{{"LOT-DOL-500-A", 120, 180, true}, {"LOT-DOL-500-B", 200, 365, true}}},
		{FamilyCode: "ANALGESIC", MedicationCode: "PARACETAMOL", MedicationName: "DOLIPRANE", GenericName: "Paracétamol", PresentationCode: "PARA-1000-TAB", Dosage: "1 g", Form: "Comprimé", Route: "Orale", Unit: "comprimé", Packaging: "Boîte de 8 comprimés", Stock: 104, Threshold: 10, MedicationActive: true, PresentationActive: true, Batches: []batchSeed{{"LOT-DOL-1G-A", 4, 120, true}, {"LOT-DOL-1G-B", 100, 300, true}}},
		{FamilyCode: "ANALGESIC", MedicationCode: "EFFERALGAN", MedicationName: "EFFERALGAN", GenericName: "Paracétamol", PresentationCode: "EFF-1G-EFF", Dosage: "1 g", Form: "Comprimé effervescent", Route: "Orale", Unit: "comprimé", Packaging: "Boîte de 8 comprimés", Stock: 60, Threshold: 10, MedicationActive: true, PresentationActive: true, Batches: valid(60, 240, "LOT-EFF-1G-A")},
		{FamilyCode: "ANALGESIC", MedicationCode: "SPASFON", MedicationName: "SPASFON", GenericName: "Phloroglucinol", PresentationCode: "SPA-80-TAB", Dosage: "80 mg", Form: "Comprimé", Route: "Orale", Unit: "comprimé", Packaging: "Boîte de 30 comprimés", Stock: 90, Threshold: 15, MedicationActive: true, PresentationActive: true, Batches: valid(90, 300, "LOT-SPA-80-A")},
		{FamilyCode: "ANTIINFLAMMATORY", MedicationCode: "IBUPROFEN", MedicationName: "ADVIL", GenericName: "Ibuprofène", PresentationCode: "IBU-400-TAB", Dosage: "400 mg", Form: "Comprimé", Route: "Orale", Unit: "comprimé", Packaging: "Boîte de 20 comprimés", Stock: 8, Threshold: 10, MedicationActive: true, PresentationActive: true, Batches: valid(8, 150, "LOT-ADV-400-A")},
		{FamilyCode: "ANTIBIOTIC", MedicationCode: "AUGMENTIN", MedicationName: "AUGMENTIN", GenericName: "Amoxicilline + Acide clavulanique", PresentationCode: "AUG-TAB", Dosage: "875/125 mg", Form: "Comprimé", Route: "Orale", Unit: "comprimé", Packaging: "Boîte de 14 comprimés", Stock: 70, Threshold: 14, MedicationActive: true, PresentationActive: true, Batches: valid(70, 200, "LOT-AUG-A")},
		{FamilyCode: "ANTIBIOTIC", MedicationCode: "AMOXICILLIN", MedicationName: "AMOXIL", GenericName: "Amoxicilline", PresentationCode: "AMOX-500-CAP", Dosage: "500 mg", Form: "Gélule", Route: "Orale", Unit: "gélule", Packaging: "Boîte de 12 gélules", Stock: 0, Threshold: 12, MedicationActive: true, PresentationActive: true},
		{FamilyCode: "ANTIBIOTIC", MedicationCode: "ROCEPHINE", MedicationName: "ROCEPHINE", GenericName: "Ceftriaxone", PresentationCode: "ROC-1G-INJ", Dosage: "1 g", Form: "Injectable", Route: "Injectable", Unit: "flacon", Packaging: "Flacon", Stock: 12, Threshold: 5, MedicationActive: true, PresentationActive: true, Batches: valid(12, 20, "LOT-ROC-1G-SOON")},
		{FamilyCode: "ANTIBIOTIC", MedicationCode: "FLAGYL", MedicationName: "FLAGYL", GenericName: "Métronidazole", PresentationCode: "FLA-500-TAB", Dosage: "500 mg", Form: "Comprimé", Route: "Orale", Unit: "comprimé", Packaging: "Boîte de 20 comprimés", Stock: 0, Threshold: 10, MedicationActive: true, PresentationActive: true, Batches: []batchSeed{{"LOT-FLA-EXPIRED", 25, -10, true}}},
		{FamilyCode: "CARDIO", MedicationCode: "AMLODIPINE", MedicationName: "NORVASC", GenericName: "Amlodipine", PresentationCode: "AMLO-5-TAB", Dosage: "5 mg", Form: "Comprimé", Route: "Orale", Unit: "comprimé", Packaging: "Boîte de 30 comprimés", Stock: 0, Threshold: 10, MedicationActive: true, PresentationActive: true, Batches: []batchSeed{{"LOT-NOR-DEPLETED", 0, 240, true}}},
		{FamilyCode: "CARDIO", MedicationCode: "COZAAR", MedicationName: "COZAAR", GenericName: "Losartan", PresentationCode: "COZ-50-TAB", Dosage: "50 mg", Form: "Comprimé", Route: "Orale", Unit: "comprimé", Packaging: "Boîte de 30 comprimés", Stock: 60, Threshold: 15, MedicationActive: true, PresentationActive: true, Batches: valid(60, 260, "LOT-COZ-A")},
		{FamilyCode: "CARDIO", MedicationCode: "LASILIX", MedicationName: "LASILIX", GenericName: "Furosémide", PresentationCode: "LAS-40-TAB", Dosage: "40 mg", Form: "Comprimé", Route: "Orale", Unit: "comprimé", Packaging: "Boîte de 30 comprimés", Stock: 50, Threshold: 10, MedicationActive: true, PresentationActive: true, Batches: valid(50, 280, "LOT-LAS-A")},
		{FamilyCode: "DIABETES", MedicationCode: "METFORMIN", MedicationName: "GLUCOPHAGE", GenericName: "Metformine", PresentationCode: "METF-500-TAB", Dosage: "500 mg", Form: "Comprimé", Route: "Orale", Unit: "comprimé", Packaging: "Boîte de 30 comprimés", Stock: 120, Threshold: 20, MedicationActive: true, PresentationActive: true, Batches: valid(120, 300, "LOT-GLU-500-A")},
		{FamilyCode: "DIABETES", MedicationCode: "METFORMIN", MedicationName: "GLUCOPHAGE", GenericName: "Metformine", PresentationCode: "METF-850-TAB", Dosage: "850 mg", Form: "Comprimé", Route: "Orale", Unit: "comprimé", Packaging: "Boîte de 30 comprimés", Stock: 8, Threshold: 10, MedicationActive: true, PresentationActive: true, Batches: valid(8, 220, "LOT-GLU-850-A")},
		{FamilyCode: "DIGESTIVE", MedicationCode: "OMEPRAZOLE", MedicationName: "MOPRAL", GenericName: "Oméprazole", PresentationCode: "OMEP-20-CAP", Dosage: "20 mg", Form: "Gélule", Route: "Orale", Unit: "gélule", Packaging: "Boîte de 14 gélules", Stock: 56, Threshold: 14, MedicationActive: true, PresentationActive: true, Batches: valid(56, 250, "LOT-MOP-A")},
		{FamilyCode: "DIGESTIVE", MedicationCode: "SMECTA", MedicationName: "SMECTA", GenericName: "Diosmectite", PresentationCode: "SME-SACH", Dosage: "3 g", Form: "Sachet", Route: "Orale", Unit: "sachet", Packaging: "Boîte de 30 sachets", Stock: 60, Threshold: 15, MedicationActive: true, PresentationActive: true, Batches: valid(60, 300, "LOT-SME-A")},
		{FamilyCode: "RESPIRATORY", MedicationCode: "SALBUTAMOL", MedicationName: "VENTOLINE", GenericName: "Salbutamol", PresentationCode: "SALB-INH", Dosage: "100 µg", Form: "Inhalateur", Route: "Inhalée", Unit: "dose", Packaging: "Inhalateur", Stock: 40, Threshold: 10, MedicationActive: true, PresentationActive: true, Batches: valid(40, 180, "LOT-VEN-A")},
		{FamilyCode: "RESPIRATORY", MedicationCode: "PULMICORT", MedicationName: "PULMICORT", GenericName: "Budésonide", PresentationCode: "PUL-INH", Dosage: "200 µg", Form: "Inhalation", Route: "Inhalée", Unit: "dose", Packaging: "Inhalateur", Stock: 30, Threshold: 8, MedicationActive: true, PresentationActive: true, Batches: valid(30, 200, "LOT-PUL-A")},
		{FamilyCode: "ANTIHISTAMINE", MedicationCode: "ZYRTEC", MedicationName: "ZYRTEC", GenericName: "Cétirizine", PresentationCode: "ZYR-10-TAB", Dosage: "10 mg", Form: "Comprimé", Route: "Orale", Unit: "comprimé", Packaging: "Boîte de 15 comprimés", Stock: 45, Threshold: 10, MedicationActive: true, PresentationActive: true, Batches: valid(45, 210, "LOT-ZYR-A")},
		{FamilyCode: "CORTICOID", MedicationCode: "SOLUPRED", MedicationName: "SOLUPRED", GenericName: "Prednisolone", PresentationCode: "SOL-20-TAB", Dosage: "20 mg", Form: "Comprimé", Route: "Orale", Unit: "comprimé", Packaging: "Boîte de 20 comprimés", Stock: 40, Threshold: 10, MedicationActive: true, PresentationActive: true, Batches: valid(40, 230, "LOT-SOL-A")},
		{FamilyCode: "ANTICOAGULANT", MedicationCode: "LOVENOX", MedicationName: "LOVENOX", GenericName: "Énoxaparine", PresentationCode: "LOV-40-SYR", Dosage: "40 mg", Form: "Seringue préremplie", Route: "Injectable", Unit: "seringue", Packaging: "Boîte de 6 seringues", Stock: 24, Threshold: 6, MedicationActive: true, PresentationActive: true, Batches: valid(24, 190, "LOT-LOV-A")},
		{FamilyCode: "ANTIEMETIC", MedicationCode: "PRIMPERAN", MedicationName: "PRIMPERAN", GenericName: "Métoclopramide", PresentationCode: "PRI-10-TAB", Dosage: "10 mg", Form: "Comprimé", Route: "Orale", Unit: "comprimé", Packaging: "Boîte de 20 comprimés", Stock: 40, Threshold: 10, MedicationActive: true, PresentationActive: true, Batches: valid(40, 260, "LOT-PRI-A")},
		{FamilyCode: "OTHER", MedicationCode: "DEMO-SYRUP", MedicationName: "DEMO SIROP", GenericName: "Solution orale DEMO", PresentationCode: "DEM-SYR", Dosage: "100 ml", Form: "Sirop", Route: "Orale", Unit: "flacon", Packaging: "Flacon", Stock: 12, Threshold: 3, MedicationActive: true, PresentationActive: true, Batches: valid(12, 180, "LOT-DEM-SYR")},
		{FamilyCode: "OTHER", MedicationCode: "DEMO-CREAM", MedicationName: "DEMO CRÈME", GenericName: "Crème cutanée DEMO", PresentationCode: "DEM-CREAM", Dosage: "30 g", Form: "Crème", Route: "Cutanée", Unit: "tube", Packaging: "Tube", Stock: 10, Threshold: 2, MedicationActive: true, PresentationActive: true, Batches: valid(10, 240, "LOT-DEM-CREAM")},
		{FamilyCode: "OTHER", MedicationCode: "DEMO-EYE", MedicationName: "DEMO COLLYRE", GenericName: "Solution ophtalmique DEMO", PresentationCode: "DEM-EYE", Dosage: "10 ml", Form: "Collyre", Route: "Ophtalmique", Unit: "flacon", Packaging: "Flacon", Stock: 6, Threshold: 2, MedicationActive: true, PresentationActive: true, Batches: valid(6, 160, "LOT-DEM-EYE")},
		{FamilyCode: "OTHER", MedicationCode: "DEMO-INACTIVE", MedicationName: "DEMO INACTIF", GenericName: "Produit DEMO inactif", PresentationCode: "DEM-INACTIVE", Dosage: "10 mg", Form: "Pommade", Route: "Cutanée", Unit: "tube", Packaging: "Tube", Stock: 0, Threshold: 0, MedicationActive: false, PresentationActive: true},
		{FamilyCode: "OTHER", MedicationCode: "DEMO-SOLUTION", MedicationName: "DEMO SOLUTION", GenericName: "Solution DEMO", PresentationCode: "DEM-PRES-INACTIVE", Dosage: "5 ml", Form: "Solution", Route: "Injectable", Unit: "ampoule", Packaging: "Ampoule", Stock: 0, Threshold: 0, MedicationActive: true, PresentationActive: false},
	}

	result := make(map[string]pharmacy.MedicationPresentation)

	for _, item := range items {
		family := pharmacy.MedicationFamily{}

		must(
			db.
				Where("code = ?", item.FamilyCode).
				First(&family).
				Error,
		)

		medication := pharmacy.Medication{
			FamilyID:    family.ID,
			Code:        item.MedicationCode,
			Name:        item.MedicationName,
			GenericName: item.GenericName,
			Description: "Donnée DEMO MedCore",
			IsActive:    item.MedicationActive,
		}

		medication.FamilyID = family.ID

		must(
			db.
				Where("code = ?", medication.Code).
				Assign(medication).
				FirstOrCreate(&medication).
				Error,
		)
		must(db.Model(&medication).Updates(map[string]interface{}{
			"family_id": family.ID, "name": item.MedicationName, "generic_name": item.GenericName,
			"description": "Donnée DEMO MedCore", "is_active": item.MedicationActive,
		}).Error)

		presentation := pharmacy.MedicationPresentation{
			MedicationID: medication.ID,
			Code:         item.PresentationCode,
			Dosage:       item.Dosage,
			Form:         item.Form,
			Route:        item.Route,
			Unit:         item.Unit,
			Packaging:    item.Packaging,
			IsActive:     item.PresentationActive,
		}

		must(
			db.
				Where("code = ?", presentation.Code).
				Assign(presentation).
				FirstOrCreate(&presentation).
				Error,
		)
		must(db.Model(&presentation).Updates(map[string]interface{}{
			"medication_id": medication.ID, "dosage": item.Dosage, "form": item.Form,
			"route": item.Route, "unit": item.Unit, "packaging": item.Packaging,
			"is_active": item.PresentationActive,
		}).Error)

		stock := pharmacy.PharmacyStock{
			PresentationID:    presentation.ID,
			QuantityAvailable: item.Stock,
			AlertThreshold:    item.Threshold,
			IsStockManaged:    true,
		}

		must(
			db.
				Where("presentation_id = ?", presentation.ID).
				FirstOrCreate(&stock).
				Error,
		)
		must(db.Model(&stock).Updates(map[string]interface{}{
			"quantity_available": item.Stock, "alert_threshold": item.Threshold,
			"is_stock_managed": true,
		}).Error)

		result[presentation.Code] = presentation
		for _, b := range item.Batches {
			expires := time.Now().AddDate(0, 0, b.DayOffset)
			batch := pharmacy.PharmacyBatch{PresentationID: presentation.ID, BatchNumber: b.Number, QuantityReceived: b.Quantity, QuantityRemaining: b.Quantity, ExpirationDate: &expires, Supplier: "DEMO", IsActive: b.Active}
			must(db.Where("batch_number = ?", b.Number).FirstOrCreate(&batch).Error)
			must(db.Model(&batch).Updates(map[string]interface{}{
				"presentation_id": presentation.ID, "quantity_received": b.Quantity,
				"quantity_remaining": b.Quantity, "expiration_date": expires,
				"supplier": "DEMO", "is_active": b.Active,
			}).Error)
		}
	}

	return result
}

func seedStablePatient(
	db *gorm.DB,
	patient patients.Patient,
	record medical_records.MedicalRecord,
	reasons map[string]consultations.ConsultationReason,
	exams map[string]consultations.MedicalExam,
	presentations map[string]pharmacy.MedicationPresentation,
) {
	seedAllergy(db, record, patient, "medication", "Pénicilline", "Éruption cutanée", "high", "Éviter les bêta-lactamines")

	seedMedicalHistory(db, record, patient, "medical", "Paludisme traité", "Épisode résolu sans complication", -730, "resolved", "low")
	seedFamilyHistory(db, record, patient, "Hypertension artérielle", "Père", "Diagnostic vers 55 ans")
	seedVaccination(db, record, patient, "Tétanos", "Rappel", -400, 330, "completed")
	seedDocument(db, record, patient, "REPORT", "Compte rendu annuel", -90, "/demo/documents/bilan-annuel.pdf", "Bilan médical annuel")

	seedVitalSeries(
		db,
		record,
		patient,
		[][]float64{
			{-90, 70, 175, 36.7, 120, 78, 72, 16, 98, 0.92, 1},
			{-45, 71, 175, 36.6, 119, 77, 70, 15, 99, 0.89, 0},
			{-7, 72, 175, 36.8, 118, 76, 70, 16, 99, 0.94, 1},
		},
	)

	seedConsultations(
		db,
		patient,
		record,
		reasons,
		exams,
		presentations,
		[]consultationSeed{
			{
				Key: "stable-general-1", DaysAgo: 90, Hour: 10,
				Service: "Médecine générale", Doctor: "Dr Amani",
				Status:       consultations.ConsultationStatusCompleted,
				Diagnosis:    "Paludisme simple traité",
				Observations: "Évolution favorable après traitement",
				Treatment:    "Traitement symptomatique et hydratation",
				ReasonCodes:  []string{"FEVER", "HEADACHE"},
				ExamCodes:    []string{"CBC", "CRP"},
				Temperature:  38.5, SystolicBP: 121, DiastolicBP: 79,
				HeartRate: 88, RespiratoryRate: 18,
				OxygenSaturation: 98, Weight: 70, Height: 175,
				BloodGlucose: 0.92, PainScore: 3,
				Prescriptions: []prescriptionSeed{
					{
						PresentationCode: "PARA-1000-TAB",
						MedicationName:   "Paracétamol",
						Dosage:           "1 g", Form: "Comprimé", Route: "Orale",
						Quantity: 12, Frequency: "3 fois par jour",
						Duration:     "4 jours",
						Instructions: "En cas de fièvre ou douleur",
					},
				},
				SpecialtyCode: "GENERAL_MEDICINE",
				SpecialtyData: map[string]any{
					"generalCondition": "Bon",
					"hydration":        "Correcte",
				},
				ChiefComplaint:     "Fièvre et céphalées",
				PresentIllness:     "Symptômes évoluant depuis deux jours",
				AssociatedSymptoms: "Courbatures",
				GeneralAppearance:  "Patient conscient et coopérant",
				Consciousness:      "Normale",
				PhysicalSummary:    "Pas de signe de gravité",
				ClinicalImpression: "Syndrome fébrile simple",
				InvestigationPlan:  "NFS et CRP",
				FollowUpPlan:       "Contrôle sous 72 heures",
				PatientAdvice:      "Hydratation et repos",
				Disposition:        "home",
			},
			{
				Key: "stable-emergency-2", DaysAgo: 7, Hour: 13,
				Service: "Urgences", Doctor: "Dr Test",
				Status:       consultations.ConsultationStatusCompleted,
				Diagnosis:    "Fièvre sans signe de gravité",
				Observations: "Patient stable",
				Treatment:    "Paracétamol et surveillance",
				ReasonCodes:  []string{"FEVER"},
				ExamCodes:    []string{"CBC"},
				Temperature:  36.8, SystolicBP: 118, DiastolicBP: 76,
				HeartRate: 70, RespiratoryRate: 16,
				OxygenSaturation: 99, Weight: 72, Height: 175,
				BloodGlucose: 0.94, PainScore: 1,
				SpecialtyCode: "EMERGENCY",
				SpecialtyData: map[string]any{
					"arrivalMode":      "Personnel",
					"triageLevel":      "Normal",
					"glasgowScore":     15,
					"finalOrientation": "Domicile",
				},
				ChiefComplaint:     "Fièvre",
				PresentIllness:     "Épisode fébrile transitoire",
				GeneralAppearance:  "Bon état général",
				Consciousness:      "Normale",
				PhysicalSummary:    "Examen clinique rassurant",
				ClinicalImpression: "Syndrome viral probable",
				InvestigationPlan:  "Surveillance clinique",
				FollowUpPlan:       "Revenir en cas d’aggravation",
				PatientAdvice:      "Hydratation",
				Disposition:        "home",
			},
		},
	)
}

func seedCardiologyDiabetesPatient(
	db *gorm.DB,
	patient patients.Patient,
	record medical_records.MedicalRecord,
	reasons map[string]consultations.ConsultationReason,
	exams map[string]consultations.MedicalExam,
	presentations map[string]pharmacy.MedicationPresentation,
) {
	seedMedicalHistory(db, record, patient, "chronic", "Hypertension artérielle", "HTA connue depuis huit ans", -2920, "active", "high")
	seedMedicalHistory(db, record, patient, "chronic", "Diabète de type 2", "Diabète traité par metformine", -1825, "active", "high")
	seedRegularTreatment(db, record, patient, "Amlodipine", "5 mg", "1 comprimé le matin", -900, "Dr Kouadio")
	seedRegularTreatment(db, record, patient, "Metformine", "500 mg", "1 comprimé matin et soir", -700, "Dr Kouadio")
	seedFamilyHistory(db, record, patient, "Accident vasculaire cérébral", "Père", "À l’âge de 67 ans")
	seedDocument(db, record, patient, "REPORT", "Compte rendu cardiologique", -30, "/demo/documents/cardio.pdf", "Consultation de cardiologie")

	seedVitalSeries(
		db,
		record,
		patient,
		[][]float64{
			{-180, 94, 170, 36.6, 168, 102, 92, 20, 97, 1.80, 2},
			{-90, 92, 170, 36.7, 154, 96, 86, 18, 98, 1.65, 1},
			{-15, 90, 170, 36.5, 146, 92, 82, 18, 98, 1.55, 1},
		},
	)

	seedConsultations(
		db,
		patient,
		record,
		reasons,
		exams,
		presentations,
		[]consultationSeed{
			{
				Key: "cardio-diabetes", DaysAgo: 15, Hour: 9,
				Service: "Cardiologie", Doctor: "Dr Kouadio",
				Status:       consultations.ConsultationStatusCompleted,
				Diagnosis:    "HTA insuffisamment contrôlée et diabète type 2",
				Observations: "Surpoids et sédentarité",
				Treatment:    "Poursuite du traitement et adaptation hygiéno-diététique",
				ReasonCodes:  []string{"CHEST_PAIN", "HYPERTENSION_FOLLOWUP", "DIABETES_FOLLOWUP"},
				ExamCodes:    []string{"ECG", "CARDIAC_ULTRASOUND", "HBA1C", "CREATININE"},
				Temperature:  36.5, SystolicBP: 146, DiastolicBP: 92,
				HeartRate: 82, RespiratoryRate: 18,
				OxygenSaturation: 98, Weight: 90, Height: 170,
				BloodGlucose: 1.55, PainScore: 1,
				Prescriptions: []prescriptionSeed{
					{PresentationCode: "AMLO-5-TAB", MedicationName: "Amlodipine", Dosage: "5 mg", Form: "Comprimé", Route: "Orale", Quantity: 30, Frequency: "1 fois par jour", Duration: "30 jours", Instructions: "Le matin"},
					{PresentationCode: "METF-500-TAB", MedicationName: "Metformine", Dosage: "500 mg", Form: "Comprimé", Route: "Orale", Quantity: 60, Frequency: "2 fois par jour", Duration: "30 jours", Instructions: "Pendant les repas"},
				},
				SpecialtyCode: "CARDIOLOGY",
				SpecialtyData: map[string]any{
					"hasHypertension":           true,
					"hypertensionDiagnosisDate": "2018-01-01",
					"hasDiabetes":               true,
					"diabetesType":              "Type 2",
					"chestPain":                 true,
					"palpitations":              true,
					"dyspnea":                   false,
					"heartRhythm":               "Régulier",
					"peripheralPulses":          "Présents",
				},
				ChiefComplaint:     "Contrôle tensionnel",
				PresentIllness:     "TA élevée malgré le traitement",
				AssociatedSymptoms: "Palpitations intermittentes",
				GeneralAppearance:  "Patient en bon état général",
				Consciousness:      "Normale",
				PhysicalSummary:    "TA 146/92, rythme régulier",
				ClinicalImpression: "HTA non équilibrée",
				InvestigationPlan:  "ECG, échographie, HbA1c et fonction rénale",
				FollowUpPlan:       "Contrôle cardiologique dans un mois",
				PatientAdvice:      "Réduction du sel, activité physique",
				Disposition:        "home",
			},
		},
	)
}

func seedCriticalAllergyPatient(
	db *gorm.DB,
	patient patients.Patient,
	record medical_records.MedicalRecord,
	reasons map[string]consultations.ConsultationReason,
	exams map[string]consultations.MedicalExam,
	presentations map[string]pharmacy.MedicationPresentation,
) {
	seedAllergy(
		db,
		record,
		patient,
		"medication",
		"Amoxicilline",
		"Œdème facial et dyspnée",
		"critical",
		"Allergie sévère aux pénicillines",
	)

	seedAllergy(
		db,
		record,
		patient,
		"food",
		"Arachide",
		"Urticaire généralisée",
		"high",
		"Éviction stricte",
	)

	seedMedicalHistory(
		db,
		record,
		patient,
		"medical",
		"Asthme allergique",
		"Asthme intermittent déclenché par les allergènes",
		-3650,
		"active",
		"medium",
	)

	seedRegularTreatment(
		db,
		record,
		patient,
		"Salbutamol",
		"100 µg",
		"2 bouffées si gêne respiratoire",
		-600,
		"Dr N'Dri",
	)

	seedDocument(
		db,
		record,
		patient,
		"REPORT",
		"Compte rendu allergologique",
		-120,
		"/demo/documents/allergologie.pdf",
		"Confirmation d’allergie sévère à l’amoxicilline",
	)

	seedVitalSeries(
		db,
		record,
		patient,
		[][]float64{
			{-120, 68, 165, 36.7, 118, 72, 76, 16, 99, 0.91, 0},
			{-30, 69, 165, 36.8, 120, 74, 78, 17, 98, 0.94, 1},
			{-3, 69, 165, 37.1, 122, 76, 84, 18, 97, 0.96, 2},
		},
	)

	seedConsultations(
		db,
		patient,
		record,
		reasons,
		exams,
		presentations,
		[]consultationSeed{
			{
				Key:              "critical-allergy",
				DaysAgo:          3,
				Hour:             11,
				Service:          "Médecine générale",
				Doctor:           "Dr N'Dri",
				Status:           consultations.ConsultationStatusCompleted,
				Diagnosis:        "Réaction allergique médicamenteuse sévère",
				Observations:     "Antécédent d’œdème facial après prise d’amoxicilline",
				Treatment:        "Éviction stricte des pénicillines et fiche d’alerte",
				ReasonCodes:      []string{"DYSPNEA"},
				ExamCodes:        []string{"CBC", "CRP"},
				Temperature:      37.1,
				SystolicBP:       122,
				DiastolicBP:      76,
				HeartRate:        84,
				RespiratoryRate:  18,
				OxygenSaturation: 97,
				Weight:           69,
				Height:           165,
				BloodGlucose:     0.96,
				PainScore:        2,
				Prescriptions: []prescriptionSeed{
					{
						PresentationCode: "SALB-INH",
						MedicationName:   "Salbutamol",
						Dosage:           "100 µg",
						Form:             "Inhalateur",
						Route:            "Inhalée",
						Quantity:         1,
						Frequency:        "Selon besoin",
						Duration:         "30 jours",
						Instructions:     "2 bouffées en cas de gêne respiratoire",
					},
				},
				SpecialtyCode: "GENERAL_MEDICINE",
				SpecialtyData: map[string]any{
					"allergyRisk":        "critical",
					"penicillinAllergy":  true,
					"asthmaHistory":      true,
					"emergencyPlanGiven": true,
				},
				ChiefComplaint:     "Réaction après prise d’antibiotique",
				PresentIllness:     "Œdème facial survenu rapidement après amoxicilline",
				AssociatedSymptoms: "Dyspnée transitoire et prurit",
				GeneralAppearance:  "État général stable",
				Consciousness:      "Normale",
				PhysicalSummary:    "Aucun signe de détresse au moment de l’examen",
				ClinicalImpression: "Allergie sévère probable aux bêta-lactamines",
				InvestigationPlan:  "Bilan allergologique",
				FollowUpPlan:       "Consultation d’allergologie",
				PatientAdvice:      "Ne jamais reprendre de pénicilline sans avis spécialisé",
				Disposition:        "home",
			},
		},
	)
}

func seedPediatricPatient(
	db *gorm.DB,
	patient patients.Patient,
	record medical_records.MedicalRecord,
	reasons map[string]consultations.ConsultationReason,
	exams map[string]consultations.MedicalExam,
	presentations map[string]pharmacy.MedicationPresentation,
) {
	seedMedicalHistory(
		db,
		record,
		patient,
		"pediatric",
		"Prématurité modérée",
		"Naissance à 35 semaines d’aménorrhée",
		-2555,
		"resolved",
		"medium",
	)

	seedVaccination(
		db,
		record,
		patient,
		"ROR",
		"2e dose",
		-180,
		365,
		"completed",
	)

	seedVaccination(
		db,
		record,
		patient,
		"DTCP",
		"Rappel",
		-300,
		700,
		"completed",
	)

	seedDocument(
		db,
		record,
		patient,
		"REPORT",
		"Courbe de croissance",
		-20,
		"/demo/documents/croissance-enfant.pdf",
		"Suivi poids, taille et IMC",
	)

	seedVitalSeries(
		db,
		record,
		patient,
		[][]float64{
			{-365, 21, 118, 36.7, 102, 64, 88, 20, 99, 0.84, 0},
			{-180, 23, 123, 36.6, 104, 66, 86, 19, 99, 0.86, 0},
			{-10, 25, 128, 37.8, 106, 68, 92, 22, 98, 0.90, 3},
		},
	)

	seedConsultations(
		db,
		patient,
		record,
		reasons,
		exams,
		presentations,
		[]consultationSeed{
			{
				Key:              "pediatric-followup",
				DaysAgo:          10,
				Hour:             15,
				Service:          "Pédiatrie",
				Doctor:           "Dr Ahoua",
				Status:           consultations.ConsultationStatusCompleted,
				Diagnosis:        "Rhinopharyngite virale",
				Observations:     "Croissance harmonieuse, examen respiratoire normal",
				Treatment:        "Traitement symptomatique",
				ReasonCodes:      []string{"CHILD_FOLLOWUP", "FEVER"},
				ExamCodes:        []string{"CBC"},
				Temperature:      37.8,
				SystolicBP:       106,
				DiastolicBP:      68,
				HeartRate:        92,
				RespiratoryRate:  22,
				OxygenSaturation: 98,
				Weight:           25,
				Height:           128,
				BloodGlucose:     0.90,
				PainScore:        3,
				Prescriptions: []prescriptionSeed{
					{
						PresentationCode: "PARA-500-TAB",
						MedicationName:   "Paracétamol",
						Dosage:           "500 mg",
						Form:             "Comprimé",
						Route:            "Orale",
						Quantity:         12,
						Frequency:        "2 fois par jour",
						Duration:         "3 jours",
						Instructions:     "Seulement en cas de fièvre ou douleur",
					},
				},
				SpecialtyCode: "PEDIATRICS",
				SpecialtyData: map[string]any{
					"birthPlace":             "Abidjan",
					"gestationalAgeWeeks":    35,
					"prematurity":            true,
					"birthWeightKg":          2.4,
					"developmentMotor":       "Normal",
					"developmentLanguage":    "Normal",
					"schoolLevel":            "CE1",
					"legalGuardianName":      "Nadia Yao",
					"legalGuardianTelephone": "+2250505050505",
				},
				ChiefComplaint:     "Fièvre et rhinorrhée",
				PresentIllness:     "Symptômes depuis 24 heures",
				AssociatedSymptoms: "Toux légère",
				GeneralAppearance:  "Enfant éveillé et réactif",
				Consciousness:      "Normale",
				PhysicalSummary:    "Auscultation pulmonaire normale",
				ClinicalImpression: "Infection virale des voies aériennes supérieures",
				InvestigationPlan:  "Surveillance clinique",
				FollowUpPlan:       "Contrôle si fièvre persistante",
				PatientAdvice:      "Hydratation et repos",
				Disposition:        "home",
			},
		},
	)
}

func seedEmergencyPatient(
	db *gorm.DB,
	patient patients.Patient,
	record medical_records.MedicalRecord,
	reasons map[string]consultations.ConsultationReason,
	exams map[string]consultations.MedicalExam,
	presentations map[string]pharmacy.MedicationPresentation,
) {
	seedMedicalHistory(
		db,
		record,
		patient,
		"chronic",
		"Hypertension artérielle",
		"HTA connue mais suivi irrégulier",
		-1460,
		"active",
		"high",
	)

	seedAllergy(
		db,
		record,
		patient,
		"medication",
		"Ibuprofène",
		"Crise d’asthme",
		"high",
		"Éviter les AINS",
	)

	seedVitalSeries(
		db,
		record,
		patient,
		[][]float64{
			{-60, 102, 172, 37.0, 162, 98, 96, 20, 96, 1.10, 4},
			{-15, 104, 172, 38.2, 176, 110, 112, 26, 93, 1.26, 7},
			{-1, 105, 172, 39.4, 188, 124, 132, 32, 88, 1.40, 9},
		},
	)

	seedConsultations(
		db,
		patient,
		record,
		reasons,
		exams,
		presentations,
		[]consultationSeed{
			{
				Key:                     "emergency-critical",
				DaysAgo:                 1,
				Hour:                    22,
				Service:                 "Urgences",
				Doctor:                  "Dr Yapi",
				Status:                  consultations.ConsultationStatusInProgress,
				Diagnosis:               "Détresse respiratoire fébrile avec poussée hypertensive",
				Observations:            "Patient dyspnéique avec saturation basse",
				Treatment:               "Oxygénothérapie, voie veineuse et surveillance rapprochée",
				HospitalizationRequired: true,
				HospitalizationReason:   "Surveillance et traitement en unité d’urgence",
				HospitalizationType:     "medicale",
				HospitalizationDuration: 3,
				ReasonCodes:             []string{"FEVER", "DYSPNEA", "CHEST_PAIN"},
				ExamCodes:               []string{"CBC", "CRP", "CHEST_XRAY", "ECG"},
				Temperature:             39.4,
				SystolicBP:              188,
				DiastolicBP:             124,
				HeartRate:               132,
				RespiratoryRate:         32,
				OxygenSaturation:        88,
				Weight:                  105,
				Height:                  172,
				BloodGlucose:            1.40,
				PainScore:               9,
				Prescriptions: []prescriptionSeed{
					{
						PresentationCode: "PARA-1000-TAB",
						MedicationName:   "Paracétamol",
						Dosage:           "1 g",
						Form:             "Comprimé",
						Route:            "Orale",
						Quantity:         4,
						Frequency:        "Toutes les 8 heures",
						Duration:         "24 heures",
						Instructions:     "Surveillance de la température",
					},
					{
						PresentationCode: "SALB-INH",
						MedicationName:   "Salbutamol",
						Dosage:           "100 µg",
						Form:             "Inhalateur",
						Route:            "Inhalée",
						Quantity:         1,
						Frequency:        "Selon protocole",
						Duration:         "Urgence",
						Instructions:     "Sous surveillance médicale",
					},
				},
				SpecialtyCode: "EMERGENCY",
				SpecialtyData: map[string]any{
					"arrivalMode":      "Ambulance",
					"triageLevel":      "Critique",
					"priorityScore":    1,
					"glasgowScore":     15,
					"consciousness":    "Altérée légère",
					"finalOrientation": "Hospitalisation",
					"monitoring":       "Continue",
				},
				ChiefComplaint:     "Dyspnée aiguë et douleur thoracique",
				PresentIllness:     "Aggravation depuis plusieurs heures",
				AssociatedSymptoms: "Fièvre élevée et palpitations",
				GeneralAppearance:  "Patient en détresse",
				Consciousness:      "Conscient mais anxieux",
				PhysicalSummary:    "SpO₂ 88 %, TA 188/124, FR 32",
				ClinicalImpression: "Tableau clinique sévère nécessitant prise en charge urgente",
				InvestigationPlan:  "NFS, CRP, radiographie thorax et ECG",
				FollowUpPlan:       "Surveillance continue",
				PatientAdvice:      "Hospitalisation expliquée au patient",
				Disposition:        "hospitalization",
			},
		},
	)
}

func seedMaternityPatient(
	db *gorm.DB,
	patient patients.Patient,
	record medical_records.MedicalRecord,
	reasons map[string]consultations.ConsultationReason,
	exams map[string]consultations.MedicalExam,
	presentations map[string]pharmacy.MedicationPresentation,
) {
	seedMedicalHistory(
		db,
		record,
		patient,
		"gyneco",
		"Grossesse en cours",
		"Grossesse simple suivie régulièrement",
		-190,
		"active",
		"medium",
	)

	seedVaccination(
		db,
		record,
		patient,
		"Tétanos",
		"Rappel grossesse",
		-60,
		365,
		"completed",
	)

	seedDocument(
		db,
		record,
		patient,
		"REPORT",
		"Compte rendu échographie obstétricale",
		-15,
		"/demo/documents/echographie-obstetricale.pdf",
		"Grossesse évolutive avec biométrie adaptée au terme",
	)

	seedVitalSeries(
		db,
		record,
		patient,
		[][]float64{
			{-120, 61, 166, 36.6, 112, 70, 78, 17, 99, 0.84, 0},
			{-60, 64, 166, 36.7, 116, 72, 80, 18, 99, 0.87, 1},
			{-8, 67, 166, 36.8, 120, 76, 84, 18, 98, 0.90, 2},
		},
	)

	seedConsultations(
		db,
		patient,
		record,
		reasons,
		exams,
		presentations,
		[]consultationSeed{
			{
				Key:              "maternity-followup",
				DaysAgo:          8,
				Hour:             9,
				Service:          "Maternité",
				Doctor:           "Dr Coulibaly",
				Status:           consultations.ConsultationStatusCompleted,
				Diagnosis:        "Grossesse évolutive de 27 semaines d’aménorrhée",
				Observations:     "Mouvements fœtaux présents, absence de saignement",
				Treatment:        "Supplémentation et suivi prénatal",
				ReasonCodes:      []string{"PREGNANCY_FOLLOWUP"},
				ExamCodes:        []string{"OBSTETRIC_ULTRASOUND", "CBC"},
				Temperature:      36.8,
				SystolicBP:       120,
				DiastolicBP:      76,
				HeartRate:        84,
				RespiratoryRate:  18,
				OxygenSaturation: 98,
				Weight:           67,
				Height:           166,
				BloodGlucose:     0.90,
				PainScore:        2,
				Prescriptions: []prescriptionSeed{
					{
						MedicationName: "Fer et acide folique",
						Dosage:         "1 comprimé",
						Form:           "Comprimé",
						Route:          "Orale",
						Quantity:       30,
						Frequency:      "1 fois par jour",
						Duration:       "30 jours",
						Instructions:   "À prendre pendant le repas",
					},
				},
				SpecialtyCode: "MATERNITY",
				SpecialtyData: map[string]any{
					"pregnancyNumber":       2,
					"lastMenstrualPeriod":   "2026-01-02",
					"estimatedDeliveryDate": "2026-10-09",
					"gestationalAgeWeeks":   27,
					"pregnancyType":         "Simple",
					"fetusCount":            1,
					"pregnancyOrigin":       "Naturelle",
					"obstetricRisk":         "Normale",
					"fetalMovements":        "Présents",
					"fetalHeartRate":        148,
					"fetalPresentation":     "Céphalique",
					"uterineHeightCm":       26,
					"bleeding":              false,
					"contractions":          false,
					"amnioticFluidLoss":     false,
				},
				ChiefComplaint:     "Consultation prénatale",
				PresentIllness:     "Grossesse suivie sans complication",
				AssociatedSymptoms: "Fatigue légère",
				GeneralAppearance:  "Bon état général",
				Consciousness:      "Normale",
				PhysicalSummary:    "TA normale, mouvements fœtaux présents",
				ClinicalImpression: "Grossesse évolutive sans signe de gravité",
				InvestigationPlan:  "Échographie obstétricale et NFS",
				FollowUpPlan:       "Prochaine consultation dans quatre semaines",
				PatientAdvice:      "Poursuivre supplémentation et surveillance",
				Disposition:        "home",
			},
		},
	)
}

func seedSurgeryPatient(
	db *gorm.DB,
	patient patients.Patient,
	record medical_records.MedicalRecord,
	reasons map[string]consultations.ConsultationReason,
	exams map[string]consultations.MedicalExam,
	presentations map[string]pharmacy.MedicationPresentation,
) {
	seedSurgicalHistory(
		db,
		record,
		patient,
		"Appendicectomie",
		-3650,
		"CHU de Cocody",
		"Aucune complication",
		"Évolution postopératoire favorable",
	)

	seedSurgicalHistory(
		db,
		record,
		patient,
		"Cholécystectomie laparoscopique",
		-20,
		"Clinique MedCore",
		"Aucune complication peropératoire",
		"Contrôle postopératoire programmé",
	)

	seedDocument(
		db,
		record,
		patient,
		"REPORT",
		"Compte rendu opératoire",
		-20,
		"/demo/documents/compte-rendu-operatoire.pdf",
		"Cholécystectomie laparoscopique sans incident",
	)

	seedDocument(
		db,
		record,
		patient,
		"CERTIFICATE",
		"Certificat de repos postopératoire",
		-20,
		"/demo/documents/repos-postoperatoire.pdf",
		"Repos médical de vingt et un jours",
	)

	seedVitalSeries(
		db,
		record,
		patient,
		[][]float64{
			{-20, 74, 168, 37.4, 126, 82, 92, 20, 98, 1.02, 7},
			{-10, 73, 168, 36.9, 122, 78, 80, 18, 99, 0.98, 4},
			{-2, 72, 168, 36.6, 118, 76, 74, 16, 99, 0.95, 2},
		},
	)

	seedConsultations(
		db,
		patient,
		record,
		reasons,
		exams,
		presentations,
		[]consultationSeed{
			{
				Key:              "general-surgery-followup",
				DaysAgo:          2,
				Hour:             10,
				Service:          "Chirurgie générale",
				Doctor:           "Dr Assi",
				Status:           consultations.ConsultationStatusCompleted,
				Diagnosis:        "Suites simples de cholécystectomie",
				Observations:     "Plaie propre, absence de fièvre, transit repris",
				Treatment:        "Pansement simple et antalgiques",
				ReasonCodes:      []string{"SURGERY_FOLLOWUP", "ABDOMINAL_PAIN"},
				ExamCodes:        []string{"CBC", "CRP", "ABDOMINAL_ULTRASOUND"},
				Temperature:      36.6,
				SystolicBP:       118,
				DiastolicBP:      76,
				HeartRate:        74,
				RespiratoryRate:  16,
				OxygenSaturation: 99,
				Weight:           72,
				Height:           168,
				BloodGlucose:     0.95,
				PainScore:        2,
				Prescriptions: []prescriptionSeed{
					{
						PresentationCode: "PARA-1000-TAB",
						MedicationName:   "Paracétamol",
						Dosage:           "1 g",
						Form:             "Comprimé",
						Route:            "Orale",
						Quantity:         12,
						Frequency:        "3 fois par jour",
						Duration:         "4 jours",
						Instructions:     "En cas de douleur",
					},
					{
						PresentationCode: "OMEP-20-CAP",
						MedicationName:   "Oméprazole",
						Dosage:           "20 mg",
						Form:             "Gélule",
						Route:            "Orale",
						Quantity:         7,
						Frequency:        "1 fois par jour",
						Duration:         "7 jours",
						Instructions:     "Le matin avant le repas",
					},
				},
				SpecialtyCode: "GENERAL_SURGERY",
				SpecialtyData: map[string]any{
					"primaryDiagnosis":   "Lithiase vésiculaire opérée",
					"surgicalIndication": "Coliques hépatiques récidivantes",
					"plannedProcedure":   "Cholécystectomie laparoscopique",
					"urgencyLevel":       "Programmée",
					"procedureDate":      "2026-06-22",
					"anesthesiaType":     "Générale",
					"operativeTechnique": "Laparoscopie",
					"operativeReport":    "Intervention réalisée sans incident",
					"incidents":          "Aucun",
					"complications":      "Aucune",
					"woundStatus":        "Propre et sèche",
					"dressingStatus":     "Bon état",
					"bowelTransit":       "Repris",
					"feedingStatus":      "Normale",
					"mobilizationStatus": "Autonome",
					"nextControlDate":    "2026-07-20",
				},
				ChiefComplaint:     "Contrôle postopératoire",
				PresentIllness:     "Évolution favorable depuis l’intervention",
				AssociatedSymptoms: "Douleur légère au niveau des orifices",
				GeneralAppearance:  "Bon état général",
				Consciousness:      "Normale",
				PhysicalSummary:    "Plaies propres, abdomen souple",
				ClinicalImpression: "Suites postopératoires simples",
				InvestigationPlan:  "NFS, CRP et échographie de contrôle",
				FollowUpPlan:       "Contrôle chirurgical dans deux semaines",
				PatientAdvice:      "Éviter les efforts importants",
				Disposition:        "home",
			},
		},
	)
}

func seedENTPatient(
	db *gorm.DB,
	patient patients.Patient,
	record medical_records.MedicalRecord,
	reasons map[string]consultations.ConsultationReason,
	exams map[string]consultations.MedicalExam,
	presentations map[string]pharmacy.MedicationPresentation,
) {
	seedMedicalHistory(
		db,
		record,
		patient,
		"medical",
		"Otites récidivantes",
		"Plusieurs épisodes depuis l’enfance",
		-3650,
		"active",
		"medium",
	)

	seedDocument(
		db,
		record,
		patient,
		"REPORT",
		"Résultat audiométrique",
		-5,
		"/demo/documents/audiometrie.pdf",
		"Hypoacousie légère de transmission à droite",
	)

	seedVitalSeries(
		db,
		record,
		patient,
		[][]float64{
			{-120, 59, 162, 36.6, 114, 72, 74, 16, 99, 0.88, 0},
			{-30, 60, 162, 36.7, 116, 74, 76, 17, 99, 0.90, 1},
			{-5, 60, 162, 37.3, 118, 76, 80, 18, 98, 0.92, 4},
		},
	)

	seedConsultations(
		db,
		patient,
		record,
		reasons,
		exams,
		presentations,
		[]consultationSeed{
			{
				Key:              "ent-consultation",
				DaysAgo:          5,
				Hour:             14,
				Service:          "ORL",
				Doctor:           "Dr Bédi",
				Status:           consultations.ConsultationStatusCompleted,
				Diagnosis:        "Otite moyenne aiguë droite",
				Observations:     "Tympan droit inflammatoire sans perforation",
				Treatment:        "Traitement symptomatique et surveillance",
				ReasonCodes:      []string{"EAR_PAIN"},
				ExamCodes:        []string{"AUDIOMETRY", "TYMPANOMETRY"},
				Temperature:      37.3,
				SystolicBP:       118,
				DiastolicBP:      76,
				HeartRate:        80,
				RespiratoryRate:  18,
				OxygenSaturation: 98,
				Weight:           60,
				Height:           162,
				BloodGlucose:     0.92,
				PainScore:        4,
				Prescriptions: []prescriptionSeed{
					{
						PresentationCode: "PARA-500-TAB",
						MedicationName:   "Paracétamol",
						Dosage:           "500 mg",
						Form:             "Comprimé",
						Route:            "Orale",
						Quantity:         12,
						Frequency:        "3 fois par jour",
						Duration:         "4 jours",
						Instructions:     "En cas de douleur",
					},
				},
				SpecialtyCode: "ENT",
				SpecialtyData: map[string]any{
					"rightEarPain":       true,
					"hearingLoss":        true,
					"tinnitus":           false,
					"earDischarge":       false,
					"dizziness":          false,
					"nasalObstruction":   false,
					"soreThroat":         false,
					"rightEarCanal":      "Normal",
					"rightEardrum":       "Inflammatoire",
					"leftEarCanal":       "Normal",
					"leftEardrum":        "Normal",
					"audiometryResult":   "Hypoacousie légère de transmission",
					"tympanometryResult": "Courbe de type B à droite",
				},
				ChiefComplaint:     "Otalgie droite",
				PresentIllness:     "Douleur évoluant depuis trois jours",
				AssociatedSymptoms: "Baisse légère de l’audition",
				GeneralAppearance:  "Bon état général",
				Consciousness:      "Normale",
				PhysicalSummary:    "Tympan droit inflammatoire",
				ClinicalImpression: "Otite moyenne aiguë droite",
				InvestigationPlan:  "Audiométrie et tympanométrie",
				FollowUpPlan:       "Contrôle ORL sous dix jours",
				PatientAdvice:      "Éviter l’introduction d’eau dans l’oreille",
				Disposition:        "home",
			},
		},
	)
}

func seedAllergy(
	db *gorm.DB,
	record medical_records.MedicalRecord,
	patient patients.Patient,
	allergenType string,
	name string,
	reaction string,
	severity string,
	comment string,
) {
	item := medical_records.Allergy{
		MedicalRecordID: record.ID,
		PatientID:       patient.ID,
		AllergenType:    allergenType,
		AllergenName:    name,
		Reaction:        reaction,
		Severity:        severity,
		Comment:         comment,
		IsActive:        true,
		CreatedBy:       demoSeedUserID,
	}

	must(
		db.
			Where(
				"medical_record_id = ? AND allergen_name = ?",
				record.ID,
				name,
			).
			Assign(item).
			FirstOrCreate(&item).
			Error,
	)

	alert := medical_records.MedicalAlert{
		MedicalRecordID: record.ID,
		PatientID:       patient.ID,
		Type:            "allergy",
		Title:           "Allergie : " + name,
		Description:     reaction,
		Severity:        severity,
		IsActive:        true,
		CreatedBy:       demoSeedUserID,
	}

	must(
		db.
			Where(
				"medical_record_id = ? AND type = ? AND title = ?",
				record.ID,
				alert.Type,
				alert.Title,
			).
			Assign(alert).
			FirstOrCreate(&alert).
			Error,
	)

	seedTimelineEvent(
		db,
		record,
		patient,
		"allergy_added",
		"medical_record",
		"Allergie enregistrée",
		fmt.Sprintf("%s — %s", name, reaction),
		"Allergy",
		item.ID,
		severity,
		demoAnchorDate.AddDate(0, 0, -30),
	)
}

func seedMedicalHistory(
	db *gorm.DB,
	record medical_records.MedicalRecord,
	patient patients.Patient,
	historyType string,
	title string,
	description string,
	startDays int,
	status string,
	severity string,
) {
	startDate := demoAnchorDate.AddDate(0, 0, startDays)

	item := medical_records.MedicalHistory{
		MedicalRecordID: record.ID,
		PatientID:       patient.ID,
		Type:            historyType,
		Title:           title,
		Description:     description,
		StartDate:       &startDate,
		Status:          status,
		Severity:        severity,
		Comment:         description,
		CreatedBy:       demoSeedUserID,
	}

	must(
		db.
			Where(
				"medical_record_id = ? AND title = ?",
				record.ID,
				title,
			).
			Assign(item).
			FirstOrCreate(&item).
			Error,
	)
}

func seedSurgicalHistory(
	db *gorm.DB,
	record medical_records.MedicalRecord,
	patient patients.Patient,
	procedureName string,
	daysAgo int,
	facility string,
	complications string,
	comment string,
) {
	procedureDate := demoAnchorDate.AddDate(0, 0, daysAgo)

	item := medical_records.SurgicalHistory{
		MedicalRecordID: record.ID,
		PatientID:       patient.ID,
		ProcedureName:   procedureName,
		ProcedureDate:   &procedureDate,
		Facility:        facility,
		Complications:   complications,
		Comment:         comment,
		CreatedBy:       demoSeedUserID,
	}

	must(
		db.
			Where(
				"medical_record_id = ? AND procedure_name = ?",
				record.ID,
				procedureName,
			).
			Assign(item).
			FirstOrCreate(&item).
			Error,
	)
}

func seedFamilyHistory(
	db *gorm.DB,
	record medical_records.MedicalRecord,
	patient patients.Patient,
	disease string,
	relationship string,
	comment string,
) {
	item := medical_records.FamilyMedicalHistory{
		MedicalRecordID: record.ID,
		PatientID:       patient.ID,
		Disease:         disease,
		Relationship:    relationship,
		Comment:         comment,
		CreatedBy:       demoSeedUserID,
	}

	must(
		db.
			Where(
				"medical_record_id = ? AND disease = ? AND relationship = ?",
				record.ID,
				disease,
				relationship,
			).
			Assign(item).
			FirstOrCreate(&item).
			Error,
	)
}

func seedRegularTreatment(
	db *gorm.DB,
	record medical_records.MedicalRecord,
	patient patients.Patient,
	medicationName string,
	dosage string,
	frequency string,
	startDays int,
	prescriber string,
) {
	startDate := demoAnchorDate.AddDate(0, 0, startDays)

	item := medical_records.RegularTreatment{
		MedicalRecordID: record.ID,
		PatientID:       patient.ID,
		MedicationName:  medicationName,
		Dosage:          dosage,
		Frequency:       frequency,
		StartDate:       &startDate,
		Prescriber:      prescriber,
		IsActive:        true,
		CreatedBy:       demoSeedUserID,
	}

	must(
		db.
			Where(
				"medical_record_id = ? AND medication_name = ?",
				record.ID,
				medicationName,
			).
			Assign(item).
			FirstOrCreate(&item).
			Error,
	)
}

func seedVaccination(
	db *gorm.DB,
	record medical_records.MedicalRecord,
	patient patients.Patient,
	vaccineName string,
	dose string,
	vaccinationDays int,
	boosterDays int,
	status string,
) {
	vaccinationDate := demoAnchorDate.AddDate(0, 0, vaccinationDays)
	nextBoosterDate := demoAnchorDate.AddDate(0, 0, boosterDays)

	item := medical_records.Vaccination{
		MedicalRecordID: record.ID,
		PatientID:       patient.ID,
		VaccineName:     vaccineName,
		Dose:            dose,
		VaccinationDate: &vaccinationDate,
		NextBoosterDate: &nextBoosterDate,
		Status:          status,
		CreatedBy:       demoSeedUserID,
	}

	must(
		db.
			Where(
				"medical_record_id = ? AND vaccine_name = ? AND dose = ?",
				record.ID,
				vaccineName,
				dose,
			).
			Assign(item).
			FirstOrCreate(&item).
			Error,
	)
}

func seedDocument(
	db *gorm.DB,
	record medical_records.MedicalRecord,
	patient patients.Patient,
	documentType string,
	label string,
	daysAgo int,
	fileURL string,
	description string,
) {
	documentDate := demoAnchorDate.AddDate(0, 0, daysAgo)

	item := medical_records.MedicalDocument{
		MedicalRecordID: record.ID,
		PatientID:       patient.ID,
		Type:            documentType,
		Label:           label,
		FileName:        label + ".pdf",
		MimeType:        "application/pdf",
		FileURL:         fileURL,
		Description:     description,
		DocumentDate:    &documentDate,
		UploadedBy:      demoSeedUserID,
	}

	must(
		db.
			Where(
				"medical_record_id = ? AND label = ?",
				record.ID,
				label,
			).
			Assign(item).
			FirstOrCreate(&item).
			Error,
	)

	seedTimelineEvent(
		db,
		record,
		patient,
		"document_added",
		"document",
		"Document médical ajouté",
		label,
		"MedicalDocument",
		item.ID,
		"info",
		documentDate,
	)
}

func seedVitalSeries(
	db *gorm.DB,
	record medical_records.MedicalRecord,
	patient patients.Patient,
	rows [][]float64,
) {
	for _, row := range rows {
		if len(row) < 11 {
			continue
		}

		measuredAt := demoAnchorDate.AddDate(
			0,
			0,
			int(row[0]),
		)

		weight := row[1]
		height := row[2]
		temperature := row[3]
		systolic := int(row[4])
		diastolic := int(row[5])
		heartRate := int(row[6])
		respiratoryRate := int(row[7])
		saturation := row[8]
		glucose := row[9]
		painScore := int(row[10])

		var bmi float64

		if height > 0 {
			heightMetres := height / 100
			bmi = weight / (heightMetres * heightMetres)
		}

		item := medical_records.VitalSign{
			MedicalRecordID:  record.ID,
			PatientID:        patient.ID,
			WeightKg:         float64Ptr(weight),
			HeightCm:         float64Ptr(height),
			BMI:              float64Ptr(bmi),
			TemperatureC:     float64Ptr(temperature),
			SystolicBP:       intPtr(systolic),
			DiastolicBP:      intPtr(diastolic),
			HeartRate:        intPtr(heartRate),
			RespiratoryRate:  intPtr(respiratoryRate),
			OxygenSaturation: float64Ptr(saturation),
			BloodGlucose:     float64Ptr(glucose),
			PainScore:        intPtr(painScore),
			PainLocation:     "",
			PainType:         "",
			PainDuration:     "",
			Comment:          "Mesure générée par le seeder clinique",
			MeasuredBy:       demoSeedUserID,
			MeasuredAt:       measuredAt,
		}

		must(
			db.
				Where(
					"medical_record_id = ? AND measured_at = ?",
					record.ID,
					measuredAt,
				).
				Assign(item).
				FirstOrCreate(&item).
				Error,
		)
	}
}

func seedTimelineEvent(
	db *gorm.DB,
	record medical_records.MedicalRecord,
	patient patients.Patient,
	eventType string,
	category string,
	title string,
	description string,
	referenceType string,
	referenceID uint,
	severity string,
	eventDate time.Time,
) {
	referenceIDCopy := referenceID

	item := medical_records.MedicalTimelineEvent{
		MedicalRecordID: record.ID,
		PatientID:       patient.ID,
		EventType:       eventType,
		Category:        category,
		Title:           title,
		Description:     description,
		ReferenceType:   referenceType,
		ReferenceID:     &referenceIDCopy,
		Severity:        severity,
		EventDate:       eventDate,
		CreatedBy:       demoSeedUserID,
	}

	must(
		db.
			Where(
				"medical_record_id = ? AND event_type = ? AND reference_type = ? AND reference_id = ?",
				record.ID,
				eventType,
				referenceType,
				referenceID,
			).
			Assign(item).
			FirstOrCreate(&item).
			Error,
	)
}

func seedConsultationVitals(
	db *gorm.DB,
	consultation consultations.Consultation,
	seed consultationSeed,
) {
	item := consultations.ConsultationVitals{
		ConsultationID:         consultation.ID,
		Temperature:            float64Ptr(seed.Temperature),
		BloodPressureSystolic:  intPtr(seed.SystolicBP),
		BloodPressureDiastolic: intPtr(seed.DiastolicBP),
		HeartRate:              intPtr(seed.HeartRate),
		RespiratoryRate:        intPtr(seed.RespiratoryRate),
		OxygenSaturation:       intPtr(seed.OxygenSaturation),
		Weight:                 float64Ptr(seed.Weight),
		Height:                 float64Ptr(seed.Height),
		BloodGlucose:           float64Ptr(seed.BloodGlucose),
		PainScore:              intPtr(seed.PainScore),
	}

	must(
		db.
			Where("consultation_id = ?", consultation.ID).
			Assign(item).
			FirstOrCreate(&item).
			Error,
	)
}

func seedConsultations(
	db *gorm.DB,
	patient patients.Patient,
	record medical_records.MedicalRecord,
	reasons map[string]consultations.ConsultationReason,
	exams map[string]consultations.MedicalExam,
	presentations map[string]pharmacy.MedicationPresentation,
	items []consultationSeed,
) {
	for _, seed := range items {
		eventDate := demoAnchorDate.
			AddDate(0, 0, -seed.DaysAgo).
			Add(time.Duration(seed.Hour-12) * time.Hour)

		startedAt := eventDate
		completedAt := eventDate.Add(45 * time.Minute)

		consultation := consultations.Consultation{
			PatientID: patient.ID,

			DoctorName: seed.Doctor,
			Service:    seed.Service,
			Status:     seed.Status,
			StartedAt:  &startedAt,

			Diagnosis:    seed.Diagnosis,
			Observations: seed.Observations,
			Treatment:    seed.Treatment,

			HospitalizationRequired: seed.HospitalizationRequired,
			HospitalizationReason:   seed.HospitalizationReason,
			HospitalizationType:     seed.HospitalizationType,
			HospitalizationDuration: seed.HospitalizationDuration,

			CreatedAt: eventDate,
			UpdatedAt: eventDate,
		}

		if seed.Status == consultations.ConsultationStatusCompleted {
			consultation.CompletedAt = &completedAt
		}

		var existing consultations.Consultation

		err := db.
			Where(
				"patient_id = ? AND service = ? AND doctor_name = ? AND created_at = ?",
				patient.ID,
				seed.Service,
				seed.Doctor,
				eventDate,
			).
			First(&existing).
			Error

		switch {
		case err == nil:
			consultation.ID = existing.ID

			must(
				db.
					Model(&existing).
					Updates(map[string]any{
						"status":                   consultation.Status,
						"started_at":               consultation.StartedAt,
						"completed_at":             consultation.CompletedAt,
						"diagnosis":                consultation.Diagnosis,
						"observations":             consultation.Observations,
						"treatment":                consultation.Treatment,
						"hospitalization_required": consultation.HospitalizationRequired,
						"hospitalization_reason":   consultation.HospitalizationReason,
						"hospitalization_type":     consultation.HospitalizationType,
						"hospitalization_duration": consultation.HospitalizationDuration,
						"updated_at":               consultation.UpdatedAt,
					}).
					Error,
			)

			must(db.First(&consultation, existing.ID).Error)

		case err == gorm.ErrRecordNotFound:
			must(db.Create(&consultation).Error)

		default:
			must(err)
		}

		seedConsultationVitals(
			db,
			consultation,
			seed,
		)

		attachConsultationReasons(
			db,
			consultation,
			seed.ReasonCodes,
			reasons,
		)

		seedConsultationExams(
			db,
			consultation,
			seed.ExamCodes,
			exams,
		)

		seedConsultationPrescriptions(
			db,
			consultation,
			seed.Prescriptions,
			presentations,
		)

		seedConsultationSOAP(
			db,
			consultation,
			seed,
		)

		seedConsultationSpecialty(
			db,
			consultation,
			seed,
		)

		seedTimelineEvent(
			db,
			record,
			patient,
			"consultation_created",
			"consultation",
			"Consultation "+seed.Service,
			seed.Diagnosis,
			"Consultation",
			consultation.ID,
			"info",
			eventDate,
		)

		if seed.Status == consultations.ConsultationStatusCompleted {
			seedTimelineEvent(
				db,
				record,
				patient,
				"consultation_completed",
				"consultation",
				"Consultation terminée",
				seed.Diagnosis,
				"ConsultationCompleted",
				consultation.ID,
				"low",
				completedAt,
			)
		}

		if seed.HospitalizationRequired {
			seedTimelineEvent(
				db,
				record,
				patient,
				"hospitalization_required",
				"hospitalization",
				"Hospitalisation recommandée",
				seed.HospitalizationReason,
				"ConsultationHospitalization",
				consultation.ID,
				"high",
				eventDate.Add(30*time.Minute),
			)
		}
	}
}

func seedConsultationExams(
	db *gorm.DB,
	consultation consultations.Consultation,
	codes []string,
	examCatalog map[string]consultations.MedicalExam,
) {
	for _, code := range codes {
		exam, exists := examCatalog[code]

		if !exists {
			continue
		}

		request := consultations.ConsultationExamRequest{
			ConsultationID: consultation.ID,
			MedicalExamID:  exam.ID,
			Status:         "requested",
			Notes:          "Examen demandé par le seeder clinique",
		}

		must(
			db.
				Where(
					"consultation_id = ? AND medical_exam_id = ?",
					consultation.ID,
					exam.ID,
				).
				Assign(request).
				FirstOrCreate(&request).
				Error,
		)
	}
}

func seedConsultationPrescriptions(
	db *gorm.DB,
	consultation consultations.Consultation,
	items []prescriptionSeed,
	presentations map[string]pharmacy.MedicationPresentation,
) {
	for _, seed := range items {
		var presentationID *uint

		if presentation, exists := presentations[seed.PresentationCode]; exists {
			id := presentation.ID
			presentationID = &id
		}

		item := consultations.ConsultationPrescription{
			ConsultationID: consultation.ID,
			PresentationID: presentationID,
			MedicationName: seed.MedicationName,
			Dosage:         seed.Dosage,
			Form:           seed.Form,
			Route:          seed.Route,
			Quantity:       seed.Quantity,
			Frequency:      seed.Frequency,
			Duration:       seed.Duration,
			Instructions:   seed.Instructions,
		}

		must(
			db.
				Where(
					"consultation_id = ? AND medication_name = ? AND dosage = ?",
					consultation.ID,
					seed.MedicationName,
					seed.Dosage,
				).
				Assign(item).
				FirstOrCreate(&item).
				Error,
		)
	}
}

func seedConsultationSOAP(
	db *gorm.DB,
	consultation consultations.Consultation,
	seed consultationSeed,
) {
	item := consultations.ConsultationSOAP{
		ConsultationID: consultation.ID,

		ChiefComplaint:          seed.ChiefComplaint,
		HistoryOfPresentIllness: seed.PresentIllness,
		AssociatedSymptoms:      seed.AssociatedSymptoms,
		PatientReportedNotes:    seed.Observations,

		GeneralAppearance:   seed.GeneralAppearance,
		Consciousness:       seed.Consciousness,
		HydrationStatus:     "Correct",
		PhysicalExamSummary: seed.PhysicalSummary,

		PrimaryDiagnosis:    seed.Diagnosis,
		AssociatedDiagnoses: "",
		ClinicalImpression:  seed.ClinicalImpression,

		TreatmentPlan:     seed.Treatment,
		InvestigationPlan: seed.InvestigationPlan,
		FollowUpPlan:      seed.FollowUpPlan,
		PatientAdvice:     seed.PatientAdvice,
		Disposition:       seed.Disposition,

		CreatedBy: demoSeedUserID,
		UpdatedBy: demoSeedUserID,
	}

	must(
		db.
			Where("consultation_id = ?", consultation.ID).
			Assign(item).
			FirstOrCreate(&item).
			Error,
	)
}

func seedConsultationSpecialty(
	db *gorm.DB,
	consultation consultations.Consultation,
	seed consultationSeed,
) {
	if seed.SpecialtyCode == "" {
		return
	}

	data, err := json.Marshal(seed.SpecialtyData)
	must(err)

	item := consultations.ConsultationSpecialtyData{
		ConsultationID: consultation.ID,
		SpecialtyCode:  seed.SpecialtyCode,
		Data:           string(data),
		CreatedBy:      demoSeedUserID,
		UpdatedBy:      demoSeedUserID,
	}

	must(
		db.
			Where("consultation_id = ?", consultation.ID).
			Assign(item).
			FirstOrCreate(&item).
			Error,
	)
}

func calculateAge(birthDate time.Time) int {
	now := demoAnchorDate
	age := now.Year() - birthDate.Year()

	birthMonth := birthDate.Month()
	birthDay := birthDate.Day()

	if now.Month() < birthMonth {
		age--
	} else if now.Month() == birthMonth && now.Day() < birthDay {
		age--
	}

	return age
}

func float64Ptr(value float64) *float64 {
	return &value
}

func intPtr(value int) *int {
	return &value
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func seedConsultationReasonCatalog(
	db *gorm.DB,
) map[string]consultations.ConsultationReason {
	items := []consultations.ConsultationReason{
		{
			Code:     "FEVER",
			Name:     "Fièvre",
			Category: "Général",
			IsActive: true,
		},
		// ...
	}

	result := make(
		map[string]consultations.ConsultationReason,
	)

	for _, item := range items {
		must(
			db.
				Where("code = ?", item.Code).
				Assign(item).
				FirstOrCreate(&item).
				Error,
		)

		result[item.Code] = item
	}

	return result
}
