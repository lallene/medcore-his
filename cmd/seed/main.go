package main

import (
	"fmt"
	"log"
	"math/rand"
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
	logger.Init(cfg.AppEnv)

	db := database.Connect(cfg.DatabaseURL)
	rand.Seed(time.Now().UnixNano())

	seedAdmin(db)
	patientItems := seedPatients(db, 100)
	companyItems := seedCompanies(db)
	guarantorItems := seedGuarantors(db, companyItems)
	coverageItems := seedCoverages(db, patientItems, companyItems, guarantorItems, 70)
	seedVouchers(db, coverageItems, 200)

	log.Println("Seed Demo Hospital exécuté avec succès")
	log.Println("Login: admin@medcore.local / admin123")
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

func seedPatients(db *gorm.DB, total int) []patients.Patient {
	lastNames := []string{"KOUASSI", "KONE", "TRAORE", "YAO", "ASSI", "BAMBA", "KOFFI", "KOUAME", "N'GUESSAN", "DIABATE", "SANGARE", "AKA"}
	firstNames := []string{"Jean", "Marie", "Cedric", "Awa", "Mariam", "Fatou", "Serge", "Patrick", "Grace", "Emmanuel", "Nadia", "Eric"}

	items := make([]patients.Patient, 0, total)

	for i := 1; i <= total; i++ {
		p := patients.Patient{
			CodePatient:   fmt.Sprintf("P%04d", i),
			NumeroDossier: fmt.Sprintf("PAT-2026-%04d", i),
			Nom:           lastNames[rand.Intn(len(lastNames))],
			Prenoms:       firstNames[rand.Intn(len(firstNames))],
			Sexe:          []string{"M", "F"}[rand.Intn(2)],
			Telephone:     fmt.Sprintf("+225070000%04d", i),
			Quartier:      []string{"Cocody", "Yopougon", "Marcory", "Abobo", "Treichville", "Bingerville"}[rand.Intn(6)],
		}

		db.Where(patients.Patient{NumeroDossier: p.NumeroDossier}).FirstOrCreate(&p)
		items = append(items, p)
	}

	return items
}

func seedCompanies(db *gorm.DB) []company.InsuranceCompany {
	names := []string{"ASCOMA", "CNAM", "MUGEF-CI", "NSIA", "SUNU", "AXA", "ALLIANZ", "MSH", "SAHAM", "ATLANTIQUE"}

	items := make([]company.InsuranceCompany, 0, len(names))

	for _, name := range names {
		c := company.InsuranceCompany{
			Code:        name,
			Name:        name,
			Description: "Compagnie assurance " + name,
			Phone:       "+2250000000000",
			Email:       fmt.Sprintf("contact@%s.local", name),
			City:        "Abidjan",
			Country:     "Côte d'Ivoire",
			IsActive:    true,
		}

		db.Where(company.InsuranceCompany{Code: c.Code}).FirstOrCreate(&c)
		items = append(items, c)
	}

	return items
}

func seedGuarantors(db *gorm.DB, companies []company.InsuranceCompany) []guarantor.InsuranceGuarantor {
	guarantorNames := []string{
		"GNA", "STANDARD", "VIP", "ENTREPRISE", "FONCTIONNAIRE",
		"RETRAITE", "CONJOINT", "ENFANT", "ACTIF", "PREMIUM",
		"BASIC", "GOLD", "SILVER", "PLATINUM", "CORPORATE",
		"MINISTERE", "PUBLIC", "PRIVE", "SANTE PLUS", "FAMILLE",
		"ETUDIANT", "SPECIAL", "AMBULATOIRE", "HOSPITALISATION", "URGENCE",
	}

	items := make([]guarantor.InsuranceGuarantor, 0, len(guarantorNames))

	for i, name := range guarantorNames {
		c := companies[i%len(companies)]

		g := guarantor.InsuranceGuarantor{
			CompanyID:           c.ID,
			Code:                name,
			Name:                name,
			Description:         "Garant " + name + " lié à " + c.Name,
			DefaultCoverageRate: []float64{50, 60, 70, 80, 90, 100}[rand.Intn(6)],
			PaymentDelayDays:    []int{15, 30, 45, 60}[rand.Intn(4)],
			IsActive:            true,
		}

		db.Where(guarantor.InsuranceGuarantor{
			CompanyID: g.CompanyID,
			Code:      g.Code,
		}).FirstOrCreate(&g)

		items = append(items, g)
	}

	return items
}

func seedCoverages(
	db *gorm.DB,
	patientItems []patients.Patient,
	companyItems []company.InsuranceCompany,
	guarantorItems []guarantor.InsuranceGuarantor,
	total int,
) []coverage.PatientCoverage {
	items := make([]coverage.PatientCoverage, 0, total)

	validFrom, _ := time.Parse("2006-01-02", "2026-01-01")
	validTo, _ := time.Parse("2006-01-02", "2026-12-31")
	expiredTo, _ := time.Parse("2006-01-02", "2025-12-31")

	for i := 0; i < total; i++ {
		p := patientItems[i%len(patientItems)]
		g := guarantorItems[i%len(guarantorItems)]

		endDate := validTo
		isActive := true

		if i >= 60 {
			endDate = expiredTo
			isActive = false
		}

		item := coverage.PatientCoverage{
			PatientID:    p.ID,
			CompanyID:    g.CompanyID,
			GuarantorID:  g.ID,
			MemberNumber: fmt.Sprintf("MAT-%04d", i+1),
			Subscriber:   p.Prenoms + " " + p.Nom,
			Beneficiary:  p.Prenoms + " " + p.Nom,
			CoverageRate: g.DefaultCoverageRate,
			ValidFrom:    &validFrom,
			ValidTo:      &endDate,
			IsPrincipal:  true,
			IsActive:     isActive,
		}

		db.Where(coverage.PatientCoverage{
			PatientID:    item.PatientID,
			MemberNumber: item.MemberNumber,
		}).FirstOrCreate(&item)

		items = append(items, item)
	}

	return items
}

func seedVouchers(db *gorm.DB, coverages []coverage.PatientCoverage, total int) {
	amounts := []float64{5000, 7500, 10000, 15000, 25000, 50000, 100000}

	for i := 1; i <= total; i++ {
		cov := coverages[rand.Intn(len(coverages))]
		issueDate := time.Now().AddDate(0, 0, -rand.Intn(60))

		gross := amounts[rand.Intn(len(amounts))]
		covered := gross * cov.CoverageRate / 100
		patientPart := gross - covered

		status := statusForIndex(i)

		v := voucher.InsuranceVoucher{
			VoucherNumber: fmt.Sprintf("BPC-2026-%06d", i),
			CoverageID:    cov.ID,
			PatientID:     cov.PatientID,
			CompanyID:     cov.CompanyID,
			GuarantorID:   cov.GuarantorID,
			Status:        status,
			IssueDate:     &issueDate,
			GrossAmount:   gross,
			CoveredAmount: covered,
			PatientAmount: patientPart,
			CoverageRate:  cov.CoverageRate,
			Notes:         "Bon assurance généré par Demo Hospital",
		}

		db.Where(voucher.InsuranceVoucher{
			VoucherNumber: v.VoucherNumber,
		}).FirstOrCreate(&v)

		seedWorkflowHistory(db, v)
	}
}

func statusForIndex(i int) string {
	switch {
	case i <= 40:
		return "draft"
	case i <= 100:
		return []string{"submitted", "controlled"}[rand.Intn(2)]
	case i <= 170:
		return "validated"
	case i <= 190:
		return "rejected"
	default:
		return "cancelled"
	}
}

func seedWorkflowHistory(db *gorm.DB, v voucher.InsuranceVoucher) {
	actorID := uint(1)

	addHistory := func(from string, to string, action string, reason string) {
		h := workflow.History{
			WorkflowName: "insurance_voucher",
			EntityName:   "InsuranceVoucher",
			EntityID:     v.ID,
			FromState:    from,
			ToState:      to,
			Action:       action,
			UserID:       &actorID,
			Role:         "admin",
			Reason:       reason,
			OccurredAt:   time.Now(),
		}

		db.Where(workflow.History{
			WorkflowName: h.WorkflowName,
			EntityName:   h.EntityName,
			EntityID:     h.EntityID,
			Action:       h.Action,
			ToState:      h.ToState,
		}).FirstOrCreate(&h)
	}

	switch v.Status {
	case "submitted":
		addHistory("draft", "submitted", "submit", "Bon soumis pour contrôle")
	case "controlled":
		addHistory("draft", "submitted", "submit", "Bon soumis pour contrôle")
		addHistory("submitted", "controlled", "control", "Pièces vérifiées")
	case "validated":
		addHistory("draft", "submitted", "submit", "Bon soumis pour contrôle")
		addHistory("submitted", "controlled", "control", "Pièces vérifiées")
		addHistory("controlled", "validated", "validate", "Bon validé")
	case "rejected":
		addHistory("draft", "submitted", "submit", "Bon soumis pour contrôle")
		addHistory("submitted", "rejected", "reject", "Informations assurance incorrectes")
	case "cancelled":
		addHistory("draft", "cancelled", "cancel", "Bon annulé")
	}
}
