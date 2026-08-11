package main

import (
	"fmt"
	"log"
	"math/rand"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/lallene/medcore-his/backend/internal/config"
	"github.com/lallene/medcore-his/backend/internal/core/logger"
	"github.com/lallene/medcore-his/backend/internal/core/workflow"
	"github.com/lallene/medcore-his/backend/internal/database"
	"github.com/lallene/medcore-his/backend/internal/modules/auth"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/company"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/coverage"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/guarantor"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/voucher"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
)

func main() {
	cfg := config.Load()
	if strings.EqualFold(cfg.AppEnv, "production") {
		log.Fatal("seed DEMO interdit en environnement production")
	}
	logger.Init(cfg.AppEnv)

	db := database.Connect(cfg.DatabaseURL)
	if len(os.Args) > 1 && os.Args[1] == "--pharmacy-only" {
		seedPharmacyCatalog(db)
		log.Println("Seed DEMO pharmacie exécuté avec succès")
		return
	}
	rand.Seed(20260713)

	seedAdmin(db)

	patientItems := seedPatients(db, 300)
	companyItems := seedCompanies(db)
	guarantorItems := seedGuarantors(db, companyItems)
	coverageItems := seedCoverages(db, patientItems, companyItems, guarantorItems)
	seedVouchers(db, coverageItems, 800)
	seedClinicalDemo(db, patientItems)
	log.Println("Seed Demo Hospital exécuté avec succès")
	log.Println("Login: admin@medcore.local / admin123")
}

type patientSeedProfile struct {
	LastName   string
	FirstNames string
	Sex        string
}

type patientCoverageSeed struct {
	PatientID      uint
	CompanyID      uint
	GuarantorID    uint
	MemberNumber   string
	CoverageRate   float64
	IsActive       bool
	IsPrincipal    bool
	ValidFrom      time.Time
	ValidTo        time.Time
	CoverageStatus string
}

var maleFirstNames = []string{
	"Jean",
	"Emmanuel",
	"Cedric",
	"Serge",
	"Patrick",
	"Eric",
	"Junior",
	"Yannick",
	"Arnaud",
	"Michel",
	"Daniel",
	"Christian",
	"Franck",
	"Alain",
	"Yves",
	"Kevin",
	"Wilfried",
	"Mathieu",
	"Alexandre",
	"Fabrice",
	"Moussa",
	"Ibrahim",
	"Ismaël",
	"Adama",
	"Abdoulaye",
	"Souleymane",
	"Koffi",
	"Kouadio",
	"Yao",
	"Landry",
	"Brice",
	"Stéphane",
	"Joël",
	"Georges",
	"Marcel",
	"Paul",
	"Philippe",
	"David",
	"Arthur",
	"Samuel",
}

var femaleFirstNames = []string{
	"Marie",
	"Awa",
	"Mariam",
	"Fatou",
	"Grace",
	"Nadia",
	"Esther",
	"Carine",
	"Vanessa",
	"Christelle",
	"Sandrine",
	"Prisca",
	"Ruth",
	"Sarah",
	"Estelle",
	"Patricia",
	"Monique",
	"Rosine",
	"Clarisse",
	"Élodie",
	"Aïcha",
	"Aminata",
	"Fanta",
	"Salimata",
	"Kadidia",
	"Assita",
	"Affoué",
	"Akissi",
	"Adjoua",
	"Yasmine",
	"Fatimata",
	"Habiba",
	"Jeanne",
	"Nicole",
	"Caroline",
	"Michelle",
	"Odette",
	"Madeleine",
	"Émilie",
	"Audrey",
}

var patientLastNames = []string{
	"KOUASSI",
	"KONE",
	"TRAORE",
	"YAO",
	"ASSI",
	"BAMBA",
	"KOFFI",
	"KOUAME",
	"N'GUESSAN",
	"DIABATE",
	"SANGARE",
	"AKA",
	"COULIBALY",
	"OUATTARA",
	"FANÉ",
	"TOURÉ",
	"KEITA",
	"BAKAYOKO",
	"KAMARA",
	"DOUMBIA",
	"GBAGBO",
	"DAGO",
	"BLE",
	"AHOUA",
	"YAPI",
	"ADOU",
	"N'DRI",
	"AMANI",
	"AKÉ",
	"ZADI",
}

var patientDistricts = []string{
	"Cocody",
	"Yopougon",
	"Marcory",
	"Abobo",
	"Treichville",
	"Bingerville",
	"Port-Bouët",
	"Adjamé",
	"Plateau",
	"Attécoubé",
	"Koumassi",
	"Anyama",
	"Songon",
	"Riviera",
	"Angré",
}

func seedAdmin(db *gorm.DB) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("admin123"), bcrypt.DefaultCost)

	user := auth.User{
		Name:         "Administrateur",
		Email:        "admin@medcore.local",
		PasswordHash: string(hash),
		Role:         "admin",
		IsActive:     true,
	}

	db.Where(auth.User{Email: user.Email}).FirstOrCreate(&user)
}

func seedPatients(
	db *gorm.DB,
	total int,
) []patients.Patient {
	items := make([]patients.Patient, 0, total)

	for i := 1; i <= total; i++ {
		sex := "M"

		if i%2 == 0 {
			sex = "F"
		}

		firstName := randomFirstName(sex)
		lastName := patientLastNames[rand.Intn(len(patientLastNames))]

		birthDate := generateBirthDate(i)
		age := ageAtDate(birthDate, time.Now())

		isInsured := patientShouldBeInsured(i)

		coverageRate := float64(0)
		insuranceNumber := ""

		if isInsured {
			coverageRate = patientCoverageRate(i)
			insuranceNumber = fmt.Sprintf("ASS-%06d", i)
		}

		contactSex := "F"

		if sex == "F" {
			contactSex = "M"
		}

		contactLastName := patientLastNames[rand.Intn(len(patientLastNames))]

		contactName := fmt.Sprintf(
			"%s %s",
			randomFirstName(contactSex),
			contactLastName,
		)

		patient := patients.Patient{
			CodePatient:   fmt.Sprintf("P%05d", i),
			NumeroDossier: fmt.Sprintf("PAT-2026-%05d", i),

			Nom:           lastName,
			Prenoms:       firstName,
			Sexe:          sex,
			DateNaissance: &birthDate,
			Age:           &age,

			Telephone: fmt.Sprintf("+22507%08d", i),
			Quartier:  patientDistricts[rand.Intn(len(patientDistricts))],

			PersonneContact: fmt.Sprintf(
				"%s - +22505%08d",
				contactName,
				i,
			),

			IsAssure:        isInsured,
			TauxCouverture:  coverageRate,
			MatriculeAssure: insuranceNumber,
		}

		err := db.
			Where(
				"numero_dossier = ?",
				patient.NumeroDossier,
			).
			Assign(map[string]any{
				"code_patient":     patient.CodePatient,
				"nom":              patient.Nom,
				"prenoms":          patient.Prenoms,
				"sexe":             patient.Sexe,
				"date_naissance":   patient.DateNaissance,
				"age":              patient.Age,
				"telephone":        patient.Telephone,
				"quartier":         patient.Quartier,
				"personne_contact": patient.PersonneContact,
				"is_assure":        patient.IsAssure,
				"taux_couverture":  patient.TauxCouverture,
				"matricule_assure": patient.MatriculeAssure,
			}).
			FirstOrCreate(&patient).
			Error

		must(err)

		items = append(items, patient)
	}

	log.Printf(
		"%d patients créés ou mis à jour",
		len(items),
	)

	return items
}

func randomFirstName(sex string) string {
	if sex == "F" {
		return femaleFirstNames[rand.Intn(len(femaleFirstNames))]
	}

	return maleFirstNames[rand.Intn(len(maleFirstNames))]
}

func generateBirthDate(index int) time.Time {
	now := time.Now()

	var minimumAge int
	var maximumAge int

	switch {
	case index%20 == 0:
		// Nourrissons et très jeunes enfants
		minimumAge = 0
		maximumAge = 3

	case index%10 == 0:
		// Enfants et adolescents
		minimumAge = 4
		maximumAge = 17

	case index%7 == 0:
		// Personnes âgées
		minimumAge = 65
		maximumAge = 90

	case index%5 == 0:
		// Adultes plus âgés
		minimumAge = 45
		maximumAge = 64

	default:
		// Adultes
		minimumAge = 18
		maximumAge = 44
	}

	age := minimumAge +
		rand.Intn(maximumAge-minimumAge+1)

	month := time.Month(rand.Intn(12) + 1)
	day := rand.Intn(28) + 1

	return time.Date(
		now.Year()-age,
		month,
		day,
		0,
		0,
		0,
		0,
		time.Local,
	)
}

func ageAtDate(
	birthDate time.Time,
	referenceDate time.Time,
) int {
	age := referenceDate.Year() - birthDate.Year()

	if referenceDate.Month() < birthDate.Month() {
		age--
	} else if referenceDate.Month() == birthDate.Month() &&
		referenceDate.Day() < birthDate.Day() {
		age--
	}

	return age
}

func patientShouldBeInsured(index int) bool {
	// Environ 70 % de patients assurés.
	return index%10 < 7
}

func patientCoverageRate(index int) float64 {
	rates := []float64{
		50,
		60,
		70,
		75,
		80,
		85,
		90,
		100,
	}

	return rates[index%len(rates)]
}

func seedCompanies(
	db *gorm.DB,
) []company.InsuranceCompany {
	type companySeed struct {
		Code        string
		Name        string
		Description string
		Phone       string
		Email       string
		City        string
		Country     string
	}

	seeds := []companySeed{
		{
			Code:        "CNAM",
			Name:        "Caisse Nationale d’Assurance Maladie",
			Description: "Couverture maladie universelle",
			Phone:       "+2252720251000",
			Email:       "contact@cnam.demo",
			City:        "Abidjan",
			Country:     "Côte d'Ivoire",
		},
		{
			Code:        "MUGEF-CI",
			Name:        "MUGEFCI",
			Description: "Mutuelle générale des fonctionnaires",
			Phone:       "+2252720411000",
			Email:       "contact@mugefci.demo",
			City:        "Abidjan",
			Country:     "Côte d'Ivoire",
		},
		{
			Code:        "NSIA",
			Name:        "NSIA Assurances",
			Description: "Assurance santé privée",
			Phone:       "+2252720204000",
			Email:       "sante@nsia.demo",
			City:        "Abidjan",
			Country:     "Côte d'Ivoire",
		},
		{
			Code:        "SUNU",
			Name:        "SUNU Assurances",
			Description: "Assurance santé et prévoyance",
			Phone:       "+2252720217000",
			Email:       "sante@sunu.demo",
			City:        "Abidjan",
			Country:     "Côte d'Ivoire",
		},
		{
			Code:        "AXA",
			Name:        "AXA Côte d’Ivoire",
			Description: "Assurance santé entreprise et individuelle",
			Phone:       "+2252720206000",
			Email:       "contact@axa.demo",
			City:        "Abidjan",
			Country:     "Côte d'Ivoire",
		},
		{
			Code:        "ALLIANZ",
			Name:        "Allianz Côte d’Ivoire",
			Description: "Assurance santé premium",
			Phone:       "+2252720257000",
			Email:       "contact@allianz.demo",
			City:        "Abidjan",
			Country:     "Côte d'Ivoire",
		},
		{
			Code:        "ASCOMA",
			Name:        "ASCOMA Santé",
			Description: "Courtier et gestionnaire santé",
			Phone:       "+2252720223000",
			Email:       "sante@ascoma.demo",
			City:        "Abidjan",
			Country:     "Côte d'Ivoire",
		},
		{
			Code:        "MSH",
			Name:        "MSH International",
			Description: "Couverture internationale",
			Phone:       "+2252720248000",
			Email:       "contact@msh.demo",
			City:        "Abidjan",
			Country:     "Côte d'Ivoire",
		},
		{
			Code:        "ATLANTIQUE",
			Name:        "Atlantique Assurances",
			Description: "Assurance santé et entreprise",
			Phone:       "+2252720301000",
			Email:       "sante@atlantique.demo",
			City:        "Abidjan",
			Country:     "Côte d'Ivoire",
		},
		{
			Code:        "SAHAM",
			Name:        "Saham Assurance",
			Description: "Assurance médicale",
			Phone:       "+2252720279000",
			Email:       "contact@saham.demo",
			City:        "Abidjan",
			Country:     "Côte d'Ivoire",
		},
	}

	items := make(
		[]company.InsuranceCompany,
		0,
		len(seeds),
	)

	for _, seed := range seeds {
		item := company.InsuranceCompany{
			Code:        seed.Code,
			Name:        seed.Name,
			Description: seed.Description,
			Phone:       seed.Phone,
			Email:       seed.Email,
			City:        seed.City,
			Country:     seed.Country,
			IsActive:    true,
		}

		must(
			db.
				Where("code = ?", item.Code).
				Assign(item).
				FirstOrCreate(&item).
				Error,
		)

		items = append(items, item)
	}

	return items
}

func seedGuarantors(
	db *gorm.DB,
	companies []company.InsuranceCompany,
) []guarantor.InsuranceGuarantor {
	type guarantorSeed struct {
		Code         string
		Name         string
		CoverageRate float64
		PaymentDelay int
	}

	seeds := []guarantorSeed{
		{"STANDARD", "Formule standard", 70, 30},
		{"BASIC", "Formule essentielle", 50, 30},
		{"SILVER", "Formule Silver", 75, 30},
		{"GOLD", "Formule Gold", 85, 30},
		{"PREMIUM", "Formule Premium", 90, 15},
		{"PLATINUM", "Formule Platinum", 100, 15},
		{"ENTREPRISE", "Contrat entreprise", 80, 45},
		{"CORPORATE", "Contrat Corporate", 90, 30},
		{"FONCTIONNAIRE", "Fonctionnaire", 80, 45},
		{"RETRAITE", "Retraité", 70, 45},
		{"ETUDIANT", "Étudiant", 60, 30},
		{"FAMILLE", "Formule famille", 75, 30},
		{"CONJOINT", "Ayant droit conjoint", 70, 45},
		{"ENFANT", "Ayant droit enfant", 80, 45},
		{"AMBULATOIRE", "Soins ambulatoires", 70, 30},
		{"HOSPITALISATION", "Hospitalisation", 90, 60},
		{"URGENCE", "Prise en charge urgence", 100, 15},
		{"MATERNITE", "Forfait maternité", 90, 30},
		{"DENTAIRE", "Couverture dentaire", 60, 30},
		{"OPTIQUE", "Couverture optique", 60, 30},
	}

	items := make(
		[]guarantor.InsuranceGuarantor,
		0,
		len(companies)*len(seeds),
	)

	for _, currentCompany := range companies {
		for _, seed := range seeds {
			item := guarantor.InsuranceGuarantor{
				CompanyID: currentCompany.ID,

				Code: fmt.Sprintf(
					"%s-%s",
					currentCompany.Code,
					seed.Code,
				),

				Name: fmt.Sprintf(
					"%s - %s",
					currentCompany.Name,
					seed.Name,
				),

				Description: fmt.Sprintf(
					"%s proposée par %s",
					seed.Name,
					currentCompany.Name,
				),

				DefaultCoverageRate: seed.CoverageRate,
				PaymentDelayDays:    seed.PaymentDelay,
				IsActive:            true,
			}

			must(
				db.
					Where(
						"company_id = ? AND code = ?",
						item.CompanyID,
						item.Code,
					).
					Assign(item).
					FirstOrCreate(&item).
					Error,
			)

			items = append(items, item)
		}
	}

	return items
}

func seedCoverages(
	db *gorm.DB,
	patientItems []patients.Patient,
	companyItems []company.InsuranceCompany,
	guarantorItems []guarantor.InsuranceGuarantor,
) []coverage.PatientCoverage {
	items := make(
		[]coverage.PatientCoverage,
		0,
		len(patientItems),
	)

	now := time.Now()

	for index, patient := range patientItems {
		if !patient.IsAssure {
			continue
		}

		currentCompany := companyItems[index%len(companyItems)]

		companyGuarantors := guarantorsForCompany(
			guarantorItems,
			currentCompany.ID,
		)

		if len(companyGuarantors) == 0 {
			continue
		}

		currentGuarantor := companyGuarantors[index%len(companyGuarantors)]

		validFrom := now.AddDate(
			-1,
			-rand.Intn(6),
			0,
		)

		validTo := now.AddDate(
			1,
			rand.Intn(6),
			0,
		)

		isActive := true

		// Environ 10 % des couvertures sont expirées.
		if index%10 == 7 {
			validTo = now.AddDate(0, -1, 0)
			isActive = false
		}

		// Environ 5 % sont suspendues ou inactives.
		if index%20 == 11 {
			isActive = false
		}

		rate := patient.TauxCouverture

		if rate <= 0 {
			rate = currentGuarantor.DefaultCoverageRate
		}

		item := coverage.PatientCoverage{
			PatientID:   patient.ID,
			CompanyID:   currentCompany.ID,
			GuarantorID: currentGuarantor.ID,

			MemberNumber: fmt.Sprintf(
				"%s-%06d",
				currentCompany.Code,
				patient.ID,
			),

			Subscriber: fmt.Sprintf(
				"%s %s",
				patient.Prenoms,
				patient.Nom,
			),

			Beneficiary: fmt.Sprintf(
				"%s %s",
				patient.Prenoms,
				patient.Nom,
			),

			CoverageRate: rate,
			ValidFrom:    &validFrom,
			ValidTo:      &validTo,
			IsPrincipal:  true,
			IsActive:     isActive,
		}

		must(
			db.
				Where(
					"patient_id = ? AND member_number = ?",
					item.PatientID,
					item.MemberNumber,
				).
				Assign(item).
				FirstOrCreate(&item).
				Error,
		)

		items = append(items, item)

		// Certains patients disposent d’une couverture complémentaire.
		if index%8 == 0 {
			secondaryCompany := companyItems[(index+3)%len(companyItems)]

			secondaryGuarantors := guarantorsForCompany(
				guarantorItems,
				secondaryCompany.ID,
			)

			if len(secondaryGuarantors) > 0 {
				secondaryGuarantor := secondaryGuarantors[index%len(secondaryGuarantors)]

				secondaryRate := float64(20)

				secondary := coverage.PatientCoverage{
					PatientID:   patient.ID,
					CompanyID:   secondaryCompany.ID,
					GuarantorID: secondaryGuarantor.ID,

					MemberNumber: fmt.Sprintf(
						"COMP-%s-%06d",
						secondaryCompany.Code,
						patient.ID,
					),

					Subscriber: fmt.Sprintf(
						"%s %s",
						patient.Prenoms,
						patient.Nom,
					),

					Beneficiary: fmt.Sprintf(
						"%s %s",
						patient.Prenoms,
						patient.Nom,
					),

					CoverageRate: secondaryRate,
					ValidFrom:    &validFrom,
					ValidTo:      &validTo,
					IsPrincipal:  false,
					IsActive:     isActive,
				}

				must(
					db.
						Where(
							"patient_id = ? AND member_number = ?",
							secondary.PatientID,
							secondary.MemberNumber,
						).
						Assign(secondary).
						FirstOrCreate(&secondary).
						Error,
				)

				items = append(items, secondary)
			}
		}
	}

	log.Printf(
		"%d couvertures créées ou mises à jour",
		len(items),
	)

	return items
}

func guarantorsForCompany(
	items []guarantor.InsuranceGuarantor,
	companyID uint,
) []guarantor.InsuranceGuarantor {
	result := make(
		[]guarantor.InsuranceGuarantor,
		0,
	)

	for _, item := range items {
		if item.CompanyID == companyID {
			result = append(result, item)
		}
	}

	return result
}

func seedVouchers(
	db *gorm.DB,
	coverages []coverage.PatientCoverage,
	total int,
) {
	if len(coverages) == 0 {
		log.Println(
			"Aucun bon créé : aucune couverture disponible",
		)
		return
	}

	amounts := []float64{
		5000,
		7500,
		10000,
		12500,
		15000,
		20000,
		25000,
		35000,
		50000,
		75000,
		100000,
		150000,
		250000,
		500000,
		750000,
		1000000,
	}

	notes := []string{
		"Consultation externe",
		"Consultation spécialisée",
		"Prise en charge des urgences",
		"Hospitalisation médicale",
		"Hospitalisation chirurgicale",
		"Bilan biologique",
		"Imagerie médicale",
		"Suivi de grossesse",
		"Soins pédiatriques",
		"Soins dentaires",
		"Traitement ambulatoire",
	}

	for i := 1; i <= total; i++ {
		currentCoverage := coverages[rand.Intn(len(coverages))]

		issueDate := time.Now().
			AddDate(
				0,
				-rand.Intn(12),
				-rand.Intn(28),
			)

		gross := amounts[rand.Intn(len(amounts))]

		covered := gross *
			currentCoverage.CoverageRate /
			100

		patientPart := gross - covered

		status := voucherStatusForIndex(i)

		item := voucher.InsuranceVoucher{
			VoucherNumber: fmt.Sprintf(
				"BPC-2026-%07d",
				i,
			),

			CoverageID:  currentCoverage.ID,
			PatientID:   currentCoverage.PatientID,
			CompanyID:   currentCoverage.CompanyID,
			GuarantorID: currentCoverage.GuarantorID,

			Status:    status,
			IssueDate: &issueDate,

			GrossAmount:   gross,
			CoveredAmount: covered,
			PatientAmount: patientPart,
			CoverageRate:  currentCoverage.CoverageRate,

			Notes: notes[rand.Intn(len(notes))],
		}

		must(
			db.
				Where(
					"voucher_number = ?",
					item.VoucherNumber,
				).
				Assign(item).
				FirstOrCreate(&item).
				Error,
		)

		seedWorkflowHistory(
			db,
			item,
		)
	}

	log.Printf(
		"%d bons de prise en charge créés ou mis à jour",
		total,
	)
}

func voucherStatusForIndex(index int) string {
	modulo := index % 100

	switch {
	case modulo < 10:
		return "draft"

	case modulo < 25:
		return "submitted"

	case modulo < 40:
		return "controlled"

	case modulo < 80:
		return "validated"

	case modulo < 92:
		return "rejected"

	default:
		return "cancelled"
	}
}

func seedWorkflowHistory(
	db *gorm.DB,
	item voucher.InsuranceVoucher,
) {
	actorID := demoSeedUserID

	occurredAt := time.Now()

	if item.IssueDate != nil {
		occurredAt = *item.IssueDate
	}

	addHistory := func(
		from string,
		to string,
		action string,
		reason string,
		offset time.Duration,
	) {
		history := workflow.History{
			WorkflowName: "insurance_voucher",
			EntityName:   "InsuranceVoucher",
			EntityID:     item.ID,

			FromState: from,
			ToState:   to,
			Action:    action,

			UserID: &actorID,
			Role:   "admin",
			Reason: reason,

			OccurredAt: occurredAt.Add(offset),
		}

		must(
			db.
				Where(
					"workflow_name = ? AND entity_name = ? AND entity_id = ? AND action = ? AND to_state = ?",
					history.WorkflowName,
					history.EntityName,
					history.EntityID,
					history.Action,
					history.ToState,
				).
				Assign(history).
				FirstOrCreate(&history).
				Error,
		)
	}

	switch item.Status {
	case "draft":
		return

	case "submitted":
		addHistory(
			"draft",
			"submitted",
			"submit",
			"Bon soumis pour contrôle",
			2*time.Hour,
		)

	case "controlled":
		addHistory(
			"draft",
			"submitted",
			"submit",
			"Bon soumis pour contrôle",
			2*time.Hour,
		)

		addHistory(
			"submitted",
			"controlled",
			"control",
			"Identité, couverture et pièces justificatives vérifiées",
			6*time.Hour,
		)

	case "validated":
		addHistory(
			"draft",
			"submitted",
			"submit",
			"Bon soumis pour contrôle",
			2*time.Hour,
		)

		addHistory(
			"submitted",
			"controlled",
			"control",
			"Pièces justificatives vérifiées",
			6*time.Hour,
		)

		addHistory(
			"controlled",
			"validated",
			"validate",
			"Prise en charge validée",
			24*time.Hour,
		)

	case "rejected":
		addHistory(
			"draft",
			"submitted",
			"submit",
			"Bon soumis pour contrôle",
			2*time.Hour,
		)

		addHistory(
			"submitted",
			"rejected",
			"reject",
			randomRejectionReason(),
			8*time.Hour,
		)

	case "cancelled":
		addHistory(
			"draft",
			"cancelled",
			"cancel",
			randomCancellationReason(),
			3*time.Hour,
		)
	}
}

func randomRejectionReason() string {
	reasons := []string{
		"Couverture expirée",
		"Matricule assuré invalide",
		"Pièce justificative manquante",
		"Acte non couvert par le contrat",
		"Plafond de prise en charge atteint",
		"Demande déjà existante",
	}

	return reasons[rand.Intn(len(reasons))]
}

func randomCancellationReason() string {
	reasons := []string{
		"Demande annulée par le patient",
		"Erreur de saisie",
		"Changement d’établissement",
		"Consultation non réalisée",
		"Bon remplacé par une nouvelle demande",
	}

	return reasons[rand.Intn(len(reasons))]
}
