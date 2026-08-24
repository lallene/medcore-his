package main

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/billing"
	"github.com/lallene/medcore-his/backend/internal/modules/cash"
	"github.com/lallene/medcore-his/backend/internal/modules/consultations"
	"github.com/lallene/medcore-his/backend/internal/modules/hospitalizations"
	"github.com/lallene/medcore-his/backend/internal/modules/imaging"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/authorization"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/company"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/coverage"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/guarantor"
	insurance_receivables "github.com/lallene/medcore-his/backend/internal/modules/insurance_receivables"
	"github.com/lallene/medcore-his/backend/internal/modules/laboratory"
	"github.com/lallene/medcore-his/backend/internal/modules/medical_records"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
	"github.com/lallene/medcore-his/backend/internal/modules/pharmacy"
	"github.com/lallene/medcore-his/backend/internal/modules/receivables"
	"gorm.io/gorm"
)

// seedFullDemo enrichit exclusivement une base locale de démonstration. Chaque
// ressource possède une clé DEMO stable afin qu'un second passage soit sans effet.
func seedFullDemo(db *gorm.DB) {
	requireDemoDatabase(db)
	seedAdmin(db)
	var adminID uint
	db.Table("users").Select("id").Where("email = ?", "admin@medcore.local").Scan(&adminID)
	if adminID == 0 {
		log.Fatal("administrateur DEMO introuvable")
	}

	now := time.Now().UTC().Truncate(time.Second)
	patientsByCode := map[string]*patients.Patient{}
	profiles := []struct{ code, nom, prenoms, sexe string }{
		{"P-DEMO-001", "SANS-ASSURANCE", "Alice", "F"}, {"P-DEMO-002", "ALLIANZ", "Boris", "M"},
		{"P-DEMO-003", "CNAM", "Carole", "F"}, {"P-DEMO-004", "PEC-REFUSEE", "David", "M"},
		{"P-DEMO-005", "PEC-PARTIELLE", "Emma", "F"}, {"P-DEMO-006", "HOSPITALISE", "Fabrice", "M"},
		{"P-DEMO-007", "PHARMACIE", "Grace", "F"},
	}
	for i, p := range profiles {
		item := patients.Patient{CodePatient: p.code, NumeroDossier: "D-" + p.code, Nom: p.nom, Prenoms: p.prenoms, Sexe: p.sexe, Telephone: fmt.Sprintf("070000%04d", i+1), Quartier: "DEMO", IsAssure: i > 0}
		if err := db.Where("code_patient = ?", p.code).FirstOrCreate(&item).Error; err != nil {
			log.Fatal(err)
		}
		record := medical_records.MedicalRecord{PatientID: item.ID, RecordNumber: "MR-" + p.code, Status: "active"}
		if err := db.Where("patient_id = ?", item.ID).FirstOrCreate(&record).Error; err != nil {
			log.Fatal(err)
		}
		patientsByCode[p.code] = &item
	}

	companies := map[string]*company.InsuranceCompany{}
	for _, spec := range []struct {
		code, name string
		rate       float64
	}{{"DEMO-ALLIANZ", "Allianz Côte d’Ivoire DEMO", 90}, {"DEMO-CNAM", "CNAM DEMO", 80}, {"DEMO-NSIA", "NSIA DEMO", 70}} {
		c := company.InsuranceCompany{Code: spec.code, Name: spec.name, Description: "Assureur DEMO", Country: "Côte d'Ivoire", IsActive: true}
		if err := db.Where("code = ?", spec.code).FirstOrCreate(&c).Error; err != nil {
			log.Fatal(err)
		}
		g := guarantor.InsuranceGuarantor{CompanyID: c.ID, Code: spec.code + "-GAR", Name: spec.name + " Garant", DefaultCoverageRate: spec.rate, IsActive: true}
		if err := db.Where("company_id=? AND code=?", c.ID, g.Code).FirstOrCreate(&g).Error; err != nil {
			log.Fatal(err)
		}
		companies[spec.code] = &c
		for i, patientCode := range []string{"P-DEMO-002", "P-DEMO-003", "P-DEMO-004", "P-DEMO-005", "P-DEMO-006", "P-DEMO-007"} {
			if (spec.code == "DEMO-ALLIANZ" && i != 0 && i != 4) || (spec.code == "DEMO-CNAM" && i != 1 && i != 2 && i != 3) || (spec.code == "DEMO-NSIA" && i != 5) {
				continue
			}
			from, to := now.AddDate(0, -1, 0), now.AddDate(1, 0, 0)
			cov := coverage.PatientCoverage{PatientID: patientsByCode[patientCode].ID, CompanyID: c.ID, GuarantorID: g.ID, MemberNumber: "DEMO-MEMBER-" + patientCode + "-" + spec.code, Subscriber: "Souscripteur DEMO", Beneficiary: patientCode, CoverageRate: spec.rate, ValidFrom: &from, ValidTo: &to, IsPrincipal: true, IsActive: true}
			if err := db.Where("member_number = ?", cov.MemberNumber).FirstOrCreate(&cov).Error; err != nil {
				log.Fatal(err)
			}
		}
	}
	// Couverture secondaire active et couverture expirée pour les cas de sélection.
	addDemoCoverage(db, patientsByCode["P-DEMO-002"].ID, companies["DEMO-NSIA"].ID, "DEMO-MEMBER-SECONDARY", 70, false, now.AddDate(1, 0, 0))
	addDemoCoverage(db, patientsByCode["P-DEMO-003"].ID, companies["DEMO-ALLIANZ"].ID, "DEMO-MEMBER-EXPIRED", 90, false, now.AddDate(0, -1, 0))

	services := []string{"Urgences", "Médecine générale", "Cardiologie", "ORL", "Gynécologie", "Chirurgie", "Pharmacie"}
	consults := map[string]*consultations.Consultation{}
	for i, p := range profiles {
		key := "DEMO-CONSULTATION-" + p.code
		started := now.AddDate(0, 0, -i-1)
		c := consultations.Consultation{PatientID: patientsByCode[p.code].ID, DoctorName: "Dr Admin DEMO", Service: services[i], Status: consultations.ConsultationStatusCompleted, StartedAt: &started, CompletedAt: &started, Diagnosis: key, Observations: "Données de démonstration"}
		if err := db.Where("diagnosis = ?", key).FirstOrCreate(&c).Error; err != nil {
			log.Fatal(err)
		}
		consults[p.code] = &c
	}

	exams := map[string]*consultations.MedicalExam{}
	for _, e := range []struct{ code, name, category string }{{"DEMO-NFS", "NFS DEMO", "Laboratoire"}, {"DEMO-CRP", "CRP DEMO", "Laboratoire"}, {"DEMO-GLY", "Glycémie DEMO", "Laboratoire"}, {"DEMO-CREA", "Créatinine DEMO", "Laboratoire"}, {"DEMO-XRAY", "Radiographie thoracique DEMO", "Imagerie"}, {"DEMO-US", "Échographie abdominale DEMO", "Imagerie"}, {"DEMO-OBUS", "Échographie obstétricale DEMO", "Imagerie"}, {"DEMO-CT", "Scanner DEMO", "Imagerie"}} {
		x := consultations.MedicalExam{Code: e.code, Name: e.name, Category: e.category, IsActive: true}
		if err := db.Where("code = ?", e.code).FirstOrCreate(&x).Error; err != nil {
			log.Fatal(err)
		}
		exams[e.code] = &x
	}
	labStatuses := []string{laboratory.StatusOrdered, laboratory.StatusSampleCollected, laboratory.StatusInProgress, laboratory.StatusValidated}
	for i, code := range []string{"DEMO-NFS", "DEMO-CRP", "DEMO-GLY", "DEMO-CREA"} {
		p := profiles[(i+1)%len(profiles)].code
		c := consults[p]
		e := exams[code]
		db.Where("consultation_id=? AND medical_exam_id=?", c.ID, e.ID).FirstOrCreate(&consultations.ConsultationExamRequest{ConsultationID: c.ID, MedicalExamID: e.ID, Status: "requested", Priority: "ROUTINE", PrescribedBy: adminID})
		var rec medical_records.MedicalRecord
		db.Where("patient_id=?", c.PatientID).First(&rec)
		o := laboratory.Order{RequestNumber: fmt.Sprintf("LAB-DEMO-%03d", i+1), ConsultationID: c.ID, MedicalExamID: e.ID, PatientID: c.PatientID, MedicalRecordID: &rec.ID, Priority: "ROUTINE", Status: labStatuses[i], PrescribedBy: adminID, CreatedBy: adminID, UpdatedBy: adminID}
		if err := db.Where("request_number=?", o.RequestNumber).FirstOrCreate(&o).Error; err != nil {
			log.Fatal(err)
		}
	}
	imgStatuses := []string{imaging.StatusOrdered, imaging.StatusScheduled, imaging.StatusInProgress, imaging.StatusValidated}
	for i, code := range []string{"DEMO-XRAY", "DEMO-US", "DEMO-OBUS", "DEMO-CT"} {
		p := profiles[(i+2)%len(profiles)].code
		c := consults[p]
		e := exams[code]
		db.Where("consultation_id=? AND medical_exam_id=?", c.ID, e.ID).FirstOrCreate(&consultations.ConsultationExamRequest{ConsultationID: c.ID, MedicalExamID: e.ID, Status: "requested", Priority: "ROUTINE", PrescribedBy: adminID})
		var rec medical_records.MedicalRecord
		db.Where("patient_id=?", c.PatientID).First(&rec)
		o := imaging.Order{OrderNumber: fmt.Sprintf("IMG-DEMO-%03d", i+1), AccessionNumber: fmt.Sprintf("ACC-DEMO-%03d", i+1), ConsultationID: c.ID, MedicalExamID: e.ID, PatientID: c.PatientID, MedicalRecordID: &rec.ID, Modality: "DEMO", Priority: "ROUTINE", Status: imgStatuses[i], PrescribedBy: adminID, CreatedBy: adminID, UpdatedBy: adminID}
		if err := db.Where("order_number=?", o.OrderNumber).FirstOrCreate(&o).Error; err != nil {
			log.Fatal(err)
		}
	}
	for i, status := range []string{hospitalizations.StatusPlanned, hospitalizations.StatusAdmitted, hospitalizations.StatusDischarged} {
		p := profiles[i+4].code
		c := consults[p]
		var rec medical_records.MedicalRecord
		db.Where("patient_id=?", c.PatientID).First(&rec)
		h := hospitalizations.Hospitalization{PatientID: c.PatientID, MedicalRecordID: rec.ID, SourceConsultationID: c.ID, AdmissionNumber: fmt.Sprintf("HOSP-DEMO-%03d", i+1), HospitalizationType: "Médicale", AdmissionReason: "Séjour DEMO", Department: services[i], Status: status}
		if err := db.Where("admission_number=?", h.AdmissionNumber).FirstOrCreate(&h).Error; err != nil {
			log.Fatal(err)
		}
	}
	seedDemoAuthorizations(db, adminID)
	seedDemoBillingAndCash(db, adminID, consults)
	seedPharmacyCatalog(db)
	seedDemoPharmacyWorkflow(db, adminID, consults["P-DEMO-007"])
}

func addDemoCoverage(db *gorm.DB, patientID, companyID uint, member string, rate float64, principal bool, validTo time.Time) {
	var g guarantor.InsuranceGuarantor
	db.Where("company_id=?", companyID).First(&g)
	from := time.Now().AddDate(0, -2, 0)
	c := coverage.PatientCoverage{PatientID: patientID, CompanyID: companyID, GuarantorID: g.ID, MemberNumber: member, Subscriber: "DEMO", Beneficiary: "DEMO", CoverageRate: rate, ValidFrom: &from, ValidTo: &validTo, IsPrincipal: principal, IsActive: true}
	if err := db.Where("member_number=?", member).FirstOrCreate(&c).Error; err != nil {
		log.Fatal(err)
	}
}

func seedDemoAuthorizations(db *gorm.DB, user uint) {
	types := []string{"CONSULTATION", "IMAGING", "LABORATORY", "LABORATORY", "CONSULTATION"}
	statuses := []string{authorization.StatusPending, authorization.StatusApproved, authorization.StatusPartiallyApproved, authorization.StatusRejected, authorization.StatusApproved}
	requested := []float64{20000, 50000, 120000, 40000, 50000}
	for i := range types {
		var patient patients.Patient
		db.Where("code_patient=?", fmt.Sprintf("P-DEMO-%03d", i+2)).First(&patient)
		var rec medical_records.MedicalRecord
		db.Where("patient_id=?", patient.ID).First(&rec)
		var cov coverage.PatientCoverage
		db.Where("patient_id=? AND is_active", patient.ID).Order("is_principal DESC").First(&cov)
		var refID uint
		switch types[i] {
		case "CONSULTATION":
			db.Table("consultations").Select("id").Where("patient_id=?", patient.ID).Scan(&refID)
		case "LABORATORY":
			db.Table("laboratory_orders").Select("id").Where("patient_id=?", patient.ID).Scan(&refID)
		case "IMAGING":
			db.Table("imaging_orders").Select("id").Where("patient_id=?", patient.ID).Scan(&refID)
		}
		if refID == 0 || cov.ID == 0 {
			continue
		}
		number := fmt.Sprintf("PEC-DEMO-%03d", i+1)
		amount := requested[i]
		item := authorization.InsuranceAuthorization{AuthorizationNumber: number, PatientID: patient.ID, MedicalRecordID: rec.ID, PatientCoverageID: cov.ID, InsuranceCompanyID: cov.CompanyID, GuarantorID: cov.GuarantorID, ReferenceType: types[i], ReferenceID: refID, Service: "Service DEMO", RequestedAmount: &amount, RequestedAt: time.Now(), RequestedBy: user, Status: statuses[i], ExternalReference: "DEMO-EXT-" + number, Comment: "Scénario DEMO", CreatedBy: user, UpdatedBy: user}
		if statuses[i] == authorization.StatusApproved {
			rate, insured, patientAmount := 70.0, 35000.0, 15000.0
			item.ApprovedRate = &rate
			item.ApprovedAmount = &insured
			item.InsuranceAmount = &insured
			item.PatientAmount = &patientAmount
		}
		if statuses[i] == authorization.StatusPartiallyApproved {
			rate, insured, patientAmount, cap := 80.0, 70000.0, 50000.0, 70000.0
			item.ApprovedRate = &rate
			item.ApprovedAmount = &insured
			item.InsuranceAmount = &insured
			item.PatientAmount = &patientAmount
			item.CeilingAmount = &cap
		}
		if statuses[i] == authorization.StatusRejected {
			zero := 0.0
			item.InsuranceAmount = &zero
			item.PatientAmount = &amount
			item.RejectionReason = "Refus DEMO"
		}
		if err := db.Where("authorization_number=?", number).FirstOrCreate(&item).Error; err != nil {
			log.Fatal(err)
		}
		if i == 4 {
			var imagingID uint
			db.Table("imaging_orders").Select("id").Where("patient_id=?", patient.ID).Scan(&imagingID)
			if imagingID > 0 {
				link := authorization.InsuranceAuthorizationAct{InsuranceAuthorizationID: item.ID, PatientID: patient.ID, PatientCoverageID: cov.ID, ReferenceType: "IMAGING", ReferenceID: imagingID, RelationType: authorization.RelationCovered, IsActive: true, CreatedBy: user}
				if err := db.Where("patient_id=? AND patient_coverage_id=? AND reference_type='IMAGING' AND reference_id=? AND is_active", patient.ID, cov.ID, imagingID).FirstOrCreate(&link).Error; err != nil {
					log.Fatal(err)
				}
			}
		}
	}
}

func seedDemoBillingAndCash(db *gorm.DB, user uint, consults map[string]*consultations.Consultation) {
	billingService := billing.NewService(db)
	active := true
	today := time.Now().Format("2006-01-02")
	prices := []int64{10000, 15000, 8000, 6000, 40000, 35000, 120000, 5000}
	for i, typ := range []string{"CONSULTATION", "CONSULTATION", "LABORATORY", "LABORATORY", "IMAGING", "IMAGING", "HOSPITALIZATION", "MEDICATION"} {
		code := fmt.Sprintf("DEMO-TAR-%02d", i+1)
		var found int64
		db.Model(&billing.Tariff{}).Where("code=?", code).Count(&found)
		if found == 0 {
			if _, e := billingService.CreateTariff(billing.TariffRequest{ActType: typ, Code: code, Label: "Tarif " + code, UnitPrice: prices[i], EffectiveFrom: today, IsActive: &active}, user); e != nil {
				log.Fatal(e)
			}
		}
	}
	var register cash.Register
	cashService := cash.NewService(db)
	if db.Where("code=?", "DEMO-CAISSE-PRINCIPALE").First(&register).Error != nil {
		r, e := cashService.SaveRegister(0, cash.RegisterRequest{Code: "DEMO-CAISSE-PRINCIPALE", Name: "Caisse principale DEMO", Location: "Accueil", Active: &active}, user)
		if e != nil {
			log.Fatal(e)
		}
		register = *r
	}
	var open cash.Session
	if db.Where("cash_register_id=? AND status=?", register.ID, cash.SessionOpen).First(&open).Error != nil {
		s, e := cashService.Open(cash.OpenRequest{CashRegisterID: register.ID, OpeningFloat: 50000, Note: "Session DEMO ouverte"}, user)
		if e != nil {
			log.Fatal(e)
		}
		open = s.Session
	}
	for i, code := range []string{"P-DEMO-001", "P-DEMO-002", "P-DEMO-003", "P-DEMO-004", "P-DEMO-005", "P-DEMO-006", "P-DEMO-007"} {
		c := consults[code]
		var tariff billing.Tariff
		db.Where("code=?", fmt.Sprintf("DEMO-TAR-%02d", (i%2)+1)).First(&tariff)
		var line billing.InvoiceLine
		if db.Where("billable_key=?", fmt.Sprintf("CONSULTATION:%d", c.ID)).First(&line).Error == nil {
			continue
		}
		inv, e := billingService.CreateInvoice(billing.CreateInvoiceRequest{PatientID: c.PatientID, Lines: []billing.InvoiceLineRequest{{ActType: "CONSULTATION", ReferenceID: c.ID, TariffID: tariff.ID}}}, user)
		if e != nil {
			log.Fatal(e)
		}
		if i == 0 {
			continue
		}
		if inv.CoveragePending {
			// La facture reste DRAFT tant que la PEC DEMO est en attente.
			continue
		}
		inv, e = billingService.Issue(inv.ID, user)
		if e != nil {
			log.Fatal(e)
		}
		if i == 2 {
			_, e = cashService.Pay(open.ID, cash.PaymentRequest{InvoiceID: inv.ID, Amount: inv.BalanceAmount / 2, PaymentMethod: "CASH", IdempotencyKey: "DEMO-PAY-PARTIAL"}, user)
			if e != nil {
				log.Fatal(e)
			}
		}
		if i == 3 {
			_, e = cashService.Pay(open.ID, cash.PaymentRequest{InvoiceID: inv.ID, Amount: inv.BalanceAmount, PaymentMethod: "MOBILE_MONEY", MobileOperator: "Orange Money", ExternalReference: "DEMO-MM", IdempotencyKey: "DEMO-PAY-FULL"}, user)
			if e != nil {
				log.Fatal(e)
			}
		}
		if i == 4 {
			_, e = billingService.Cancel(inv.ID, "Annulation DEMO", user)
			if e != nil && !strings.Contains(e.Error(), "paiement") {
				log.Fatal(e)
			}
		}
		if i == 6 {
			quarter := inv.BalanceAmount / 4
			payments := []cash.PaymentRequest{
				{InvoiceID: inv.ID, Amount: quarter, PaymentMethod: "CARD", ExternalReference: "DEMO-CARD", IdempotencyKey: "DEMO-PAY-CARD"},
				{InvoiceID: inv.ID, Amount: quarter, PaymentMethod: "MOBILE_MONEY", MobileOperator: "MTN Money", ExternalReference: "DEMO-MM-2", IdempotencyKey: "DEMO-PAY-MM-2"},
				{InvoiceID: inv.ID, Amount: quarter, PaymentMethod: "BANK_TRANSFER", ExternalReference: "DEMO-BANK", IdempotencyKey: "DEMO-PAY-BANK"},
				{InvoiceID: inv.ID, Amount: inv.BalanceAmount - quarter*3, PaymentMethod: "CHECK", ExternalReference: "DEMO-CHECK", IdempotencyKey: "DEMO-PAY-CHECK"},
			}
			for _, payment := range payments {
				if _, e = cashService.Pay(open.ID, payment, user); e != nil {
					log.Fatal(e)
				}
			}
		}
	}
	seedDemoReceivables(db, user, billingService, cashService, open.ID)
	var closed cash.Session
	if db.Where("cash_register_id=? AND status=? AND opening_note=?", register.ID, cash.SessionClosed, "Session DEMO historique").First(&closed).Error != nil {
		past := time.Now().AddDate(0, 0, -1)
		expected, counted, diff := int64(100000), int64(98000), int64(-2000)
		closed = cash.Session{CashRegisterID: register.ID, OpenedBy: user, OpenedAt: past, OpeningFloat: 100000, OpeningNote: "Session DEMO historique", Status: cash.SessionClosed, ClosedBy: &user, ClosedAt: &past, ExpectedCashAmount: &expected, CountedCashAmount: &counted, CashDifference: &diff, ClosingNote: "Écart DEMO de recette"}
		if e := db.Create(&closed).Error; e != nil {
			log.Fatal(e)
		}
	}
}

func seedDemoReceivables(db *gorm.DB, user uint, billingService *billing.Service, cashService *cash.Service, sessionID uint) {
	active := true
	today := time.Now().Format("2006-01-02")
	ensureTariff := func(code string, amount int64) billing.Tariff {
		var tariff billing.Tariff
		if err := db.Where("code=?", code).First(&tariff).Error; err == nil {
			return tariff
		}
		created, err := billingService.CreateTariff(billing.TariffRequest{ActType: "CONSULTATION", Code: code, Label: "Tarif créance DEMO " + code, UnitPrice: amount, EffectiveFrom: today, IsActive: &active}, user)
		if err != nil {
			log.Fatal(err)
		}
		return *created
	}
	ensureInvoice := func(patientCode, marker string, tariff billing.Tariff, approvedRate *float64) billing.Invoice {
		var patient patients.Patient
		if err := db.Where("code_patient=?", patientCode).First(&patient).Error; err != nil {
			log.Fatal(err)
		}
		var consultation consultations.Consultation
		if err := db.Where("patient_id=? AND diagnosis=?", patient.ID, marker).First(&consultation).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			consultation = consultations.Consultation{PatientID: patient.ID, DoctorName: "Dr DEMO", Service: "Recouvrement", Status: consultations.ConsultationStatusCompleted, Diagnosis: marker, Observations: "Fixture LOT 13B", CreatedAt: time.Now()}
			if err = db.Create(&consultation).Error; err != nil {
				log.Fatal(err)
			}
		} else if err != nil {
			log.Fatal(err)
		}
		if approvedRate != nil {
			var coverageID uint
			if err := db.Table("patient_coverages").Select("id").Where("patient_id=? AND is_active AND (valid_from IS NULL OR valid_from<=CURRENT_DATE) AND (valid_to IS NULL OR valid_to>=CURRENT_DATE)", patient.ID).Order("is_principal DESC,id").Scan(&coverageID).Error; err != nil || coverageID == 0 {
				log.Fatalf("couverture active introuvable pour %s: %v", patientCode, err)
			}
			var authorizationID uint
			db.Table("insurance_authorizations").Select("id").Where("patient_id=? AND patient_coverage_id=? AND reference_type='CONSULTATION' AND reference_id=? AND status<>'CANCELLED'", patient.ID, coverageID, consultation.ID).Scan(&authorizationID)
			if authorizationID == 0 {
				authorizationService := authorization.NewService(db)
				requested := float64(tariff.UnitPrice)
				created, err := authorizationService.Create(authorization.CreateRequest{PatientID: patient.ID, PatientCoverageID: coverageID, ReferenceType: "CONSULTATION", ReferenceID: consultation.ID, Service: consultation.Service, RequestedAmount: &requested, Comment: "PEC DEMO LOT 13B"}, user)
				if err != nil {
					log.Fatal(err)
				}
				if _, err = authorizationService.Submit(created.ID, authorization.SubmitRequest{ExternalReference: "DEMO-PEC-LOT13B"}, user); err != nil {
					log.Fatal(err)
				}
				if _, err = authorizationService.MarkPending(created.ID, user); err != nil {
					log.Fatal(err)
				}
				if _, err = authorizationService.Decide(created.ID, authorization.DecisionRequest{Status: authorization.StatusApproved, ExternalReference: "DEMO-PEC-LOT13B", ExternalDecisionDate: time.Now().Format("2006-01-02"), ApprovedRate: approvedRate}, user); err != nil {
					log.Fatal(err)
				}
			}
		}
		var line billing.InvoiceLine
		if err := db.Where("billable_key=?", fmt.Sprintf("CONSULTATION:%d", consultation.ID)).First(&line).Error; err == nil {
			var invoice billing.Invoice
			if err = db.First(&invoice, line.InvoiceID).Error; err != nil {
				log.Fatal(err)
			}
			return invoice
		}
		invoice, err := billingService.CreateInvoice(billing.CreateInvoiceRequest{PatientID: patient.ID, Lines: []billing.InvoiceLineRequest{{ActType: "CONSULTATION", ReferenceID: consultation.ID, TariffID: tariff.ID}}}, user)
		if err != nil {
			log.Fatal(err)
		}
		if invoice.CoveragePending {
			log.Fatalf("fixture %s restée en attente de couverture", marker)
		}
		invoice, err = billingService.Issue(invoice.ID, user)
		if err != nil {
			log.Fatal(err)
		}
		return *invoice
	}
	payOnce := func(invoice billing.Invoice, amount int64, key string) {
		var count int64
		db.Model(&billing.Payment{}).Where("idempotency_key=?", key).Count(&count)
		if count > 0 {
			return
		}
		if _, err := cashService.Pay(sessionID, cash.PaymentRequest{InvoiceID: invoice.ID, Amount: amount, PaymentMethod: "CASH", IdempotencyKey: key}, user); err != nil {
			log.Fatal(err)
		}
	}

	twenty := ensureTariff("DEMO-REC-20K", 20000)
	fifty := ensureTariff("DEMO-REC-50K", 50000)
	// A/F: non assuré, 20 000, sans paiement, échéance future.
	invoiceA := ensureInvoice("P-DEMO-001", "DEMO-RECEIVABLE-A-UNPAID", twenty, nil)
	// B/E: non assuré, 20 000, payé 5 000, échéance dépassée.
	invoiceB := ensureInvoice("P-DEMO-001", "DEMO-RECEIVABLE-B-PARTIAL", twenty, nil)
	payOnce(invoiceB, 5000, "DEMO-RECEIVABLE-B-PAY-5K")
	// C/I: assuré à 70 %, 50 000 / assurance 35 000 / patient 15 000 / payé 10 000.
	rate70 := 70.0
	invoiceC := ensureInvoice("P-DEMO-006", "DEMO-RECEIVABLE-C-INSURED", fifty, &rate70)
	if invoiceC.InsuranceAmount != 35000 || invoiceC.PatientAmount != 15000 {
		log.Fatalf("fixture assurée LOT 13B incohérente: assurance=%d patient=%d", invoiceC.InsuranceAmount, invoiceC.PatientAmount)
	}
	payOnce(invoiceC, 10000, "DEMO-RECEIVABLE-C-PAY-10K")
	// D: facture entièrement payée, conservée dans Billing mais absente des créances actives.
	invoiceD := ensureInvoice("P-DEMO-001", "DEMO-RECEIVABLE-D-PAID", twenty, nil)
	payOnce(invoiceD, 20000, "DEMO-RECEIVABLE-D-PAY-20K")

	var uninsured, insured billing.Invoice
	db.Joins("JOIN patients p ON p.id=billing_invoices.patient_id").Where("p.code_patient=?", "P-DEMO-001").First(&uninsured)
	db.Joins("JOIN patients p ON p.id=billing_invoices.patient_id").Where("p.code_patient=?", "P-DEMO-006").First(&insured)
	if uninsured.ID > 0 && uninsured.Status == billing.InvoiceDraft {
		updated, err := billingService.Issue(uninsured.ID, user)
		if err != nil {
			log.Fatal(err)
		}
		uninsured = *updated
	}
	if insured.ID > 0 && insured.Status == billing.InvoiceIssued && insured.BalanceAmount > 1 {
		amount := insured.BalanceAmount / 2
		if _, err := cashService.Pay(sessionID, cash.PaymentRequest{InvoiceID: insured.ID, Amount: amount, PaymentMethod: "CASH", IdempotencyKey: "DEMO-RECEIVABLE-INSURED-PARTIAL"}, user); err != nil {
			log.Fatal(err)
		}
	}
	future, past := time.Now().AddDate(0, 0, 30), time.Now().AddDate(0, 0, -15)
	for _, item := range []struct {
		invoice billing.Invoice
		due     time.Time
	}{{invoiceA, future}, {invoiceB, past}, {invoiceC, past}} {
		metadata := receivables.Metadata{InvoiceID: item.invoice.ID, PatientID: item.invoice.PatientID, DueDate: &item.due, UpdatedBy: user}
		if err := db.Where("invoice_id=?", item.invoice.ID).FirstOrCreate(&metadata).Error; err != nil {
			log.Fatal(err)
		}
	}
	promised := int64(5000)
	promise := receivables.FollowUp{InvoiceID: invoiceC.ID, PatientID: invoiceC.PatientID, ActionType: "PAYMENT_PROMISE", Note: "Engagement de paiement DEMO LOT 13B", PromisedPaymentDate: &future, PromisedAmount: &promised, CreatedBy: user, CreatedAt: time.Now()}
	if err := db.Where("invoice_id=? AND action_type=? AND note=?", invoiceC.ID, promise.ActionType, promise.Note).FirstOrCreate(&promise).Error; err != nil {
		log.Fatal(err)
	}
	if uninsured.ID > 0 {
		m := receivables.Metadata{InvoiceID: uninsured.ID, PatientID: uninsured.PatientID, DueDate: &future, UpdatedBy: user}
		if err := db.Where("invoice_id=?", uninsured.ID).FirstOrCreate(&m).Error; err != nil {
			log.Fatal(err)
		}
	}
	if insured.ID > 0 {
		m := receivables.Metadata{InvoiceID: insured.ID, PatientID: insured.PatientID, DueDate: &past, UpdatedBy: user}
		if err := db.Where("invoice_id=?", insured.ID).FirstOrCreate(&m).Error; err != nil {
			log.Fatal(err)
		}
		promised := insured.BalanceAmount
		follow := receivables.FollowUp{InvoiceID: insured.ID, PatientID: insured.PatientID, ActionType: "PAYMENT_PROMISE", Note: "Engagement de paiement DEMO", PromisedPaymentDate: &future, PromisedAmount: &promised, CreatedBy: user, CreatedAt: time.Now()}
		if err := db.Where("invoice_id=? AND action_type=? AND note=?", insured.ID, follow.ActionType, follow.Note).FirstOrCreate(&follow).Error; err != nil {
			log.Fatal(err)
		}
	}
	seedDemoInsuranceReceivables(db, user, ensureTariff, ensureInvoice, payOnce)
}

func seedDemoInsuranceReceivables(
	db *gorm.DB,
	user uint,
	ensureTariff func(string, int64) billing.Tariff,
	ensureInvoice func(string, string, billing.Tariff, *float64) billing.Invoice,
	payOnce func(billing.Invoice, int64, string),
) {
	service := insurance_receivables.NewService(db)
	rate70, rate100 := 70.0, 100.0
	fifty := ensureTariff("DEMO-INSREC-50K", 50000)
	t35 := ensureTariff("DEMO-INSREC-35K", 35000)
	t25 := ensureTariff("DEMO-INSREC-25K", 25000)
	t60 := ensureTariff("DEMO-INSREC-60K", 60000)
	makeLine := func(marker string, tariff billing.Tariff, rate *float64) (billing.Invoice, billing.InvoiceLine) {
		inv := ensureInvoice("P-DEMO-006", marker, tariff, rate)
		var line billing.InvoiceLine
		if e := db.Where("invoice_id=? AND is_active", inv.ID).First(&line).Error; e != nil {
			log.Fatal(e)
		}
		return inv, line
	}
	aInv, a := makeLine("DEMO-INSREC-A-UNPAID", fifty, &rate70)
	payOnce(aInv, aInv.PatientAmount, "DEMO-INSREC-A-PATIENT-PAID")
	_, b := makeLine("DEMO-INSREC-B-PARTIAL", fifty, &rate70)
	_, c := makeLine("DEMO-INSREC-C-PAID", fifty, &rate70)
	_, d1 := makeLine("DEMO-INSREC-D1-MULTI", t35, &rate100)
	_, d2 := makeLine("DEMO-INSREC-D2-MULTI", t25, &rate100)
	_, d3 := makeLine("DEMO-INSREC-D3-MULTI", t60, &rate100)
	_, eLine := makeLine("DEMO-INSREC-E-UNALLOCATED", fifty, &rate70)
	_, g := makeLine("DEMO-INSREC-G-BATCH-DRAFT", fifty, &rate70)
	_, h := makeLine("DEMO-INSREC-H-BATCH-SUBMITTED", fifty, &rate70)
	e2eInv, _ := makeLine("DEMO-INSREC-E2E-PATIENT-PAID", fifty, &rate70)
	payOnce(e2eInv, e2eInv.PatientAmount, "DEMO-INSREC-E2E-PATIENT-PAID")
	if a.InsuranceAmount != 35000 || b.InsuranceAmount != 35000 || c.InsuranceAmount != 35000 {
		log.Fatalf("fixtures Insurance Receivables incohérentes: A=%d B=%d C=%d", a.InsuranceAmount, b.InsuranceAmount, c.InsuranceAmount)
	}
	var companyID uint
	db.Table("insurance_authorizations").Select("insurance_company_id").Where("id=?", a.AuthorizationID).Scan(&companyID)
	if companyID == 0 {
		log.Fatal("assureur DEMO Insurance Receivables introuvable")
	}
	settle := func(key, ref string, total int64, allocations map[uint]int64) {
		view, err := service.CreateSettlement(insurance_receivables.SettlementRequest{InsuranceCompanyID: companyID, ExternalReference: ref, ReceivedAt: time.Now().Format("2006-01-02"), TotalAmount: total, PaymentMethod: "BANK_TRANSFER", Comment: "Fixture DEMO LOT 13C", IdempotencyKey: key}, user)
		if err != nil {
			log.Fatal(err)
		}
		if view.Status == insurance_receivables.SettlementPosted {
			return
		}
		for lineID, amount := range allocations {
			var count int64
			db.Model(&insurance_receivables.SettlementAllocation{}).Where("settlement_id=? AND invoice_line_id=?", view.ID, lineID).Count(&count)
			if count == 0 {
				if _, err = service.Allocate(view.ID, insurance_receivables.AllocationRequest{InvoiceLineID: lineID, Amount: amount}, user); err != nil {
					log.Fatal(err)
				}
			}
		}
		if _, err = service.Post(view.ID, user); err != nil {
			log.Fatal(err)
		}
	}
	settle("DEMO-INSREC-B-20K", "DEMO-VIR-B-20K", 20000, map[uint]int64{b.ID: 20000})
	settle("DEMO-INSREC-C-35K", "DEMO-VIR-C-35K", 35000, map[uint]int64{c.ID: 35000})
	settle("DEMO-INSREC-D-100K", "DEMO-VIR-D-100K", 100000, map[uint]int64{d1.ID: 35000, d2.ID: 25000, d3.ID: 40000})
	settle("DEMO-INSREC-E-30K", "DEMO-VIR-E-30K", 30000, map[uint]int64{eLine.ID: 10000})
	past := time.Now().AddDate(0, 0, -15)
	if _, err := service.SetDue(a.ID, insurance_receivables.DueDateRequest{DueDate: ptrString(past.Format("2006-01-02")), Note: "Échéance dépassée DEMO"}, user); err != nil {
		log.Fatal(err)
	}
	ensureBatch := func(ref string, line billing.InvoiceLine, submit bool) {
		var existing insurance_receivables.SubmissionBatch
		if err := db.Where("external_reference=?", ref).First(&existing).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			created, createErr := service.CreateBatch(insurance_receivables.BatchRequest{InsuranceCompanyID: companyID, ExternalReference: ref, Comment: "Fixture DEMO LOT 13C", InvoiceLineIDs: []uint{line.ID}}, user)
			if createErr != nil {
				log.Fatal(createErr)
			}
			existing = created.SubmissionBatch
		} else if err != nil {
			log.Fatal(err)
		}
		if submit && existing.Status == insurance_receivables.BatchDraft {
			if _, err := service.SubmitBatch(existing.ID, user); err != nil {
				log.Fatal(err)
			}
		}
	}
	ensureBatch("DEMO-BORD-DRAFT", g, false)
	ensureBatch("DEMO-BORD-SUBMITTED", h, true)
	log.Printf("Insurance Receivables DEMO: A=%d B=%d C=%d multi=%d/%d/%d", a.ID, b.ID, c.ID, d1.ID, d2.ID, d3.ID)
}

func ptrString(value string) *string { return &value }

func seedDemoPharmacyWorkflow(db *gorm.DB, user uint, consultation *consultations.Consultation) {
	var presentation pharmacy.MedicationPresentation
	if err := db.Preload("Medication").Joins("JOIN pharmacy_stocks s ON s.presentation_id=medication_presentations.id AND s.is_stock_managed").Where("dosage=?", "1 g").Order("s.quantity_available DESC").First(&presentation).Error; err != nil {
		log.Fatal("présentation FEFO DEMO introuvable: ", err)
	}
	name := "DEMO Prescription pharmacie FEFO"
	prescription := consultations.ConsultationPrescription{ConsultationID: consultation.ID, PresentationID: &presentation.ID, MedicationName: name, Dosage: presentation.Dosage, Form: presentation.Form, Route: presentation.Route, Quantity: 12, Instructions: "8 unités puis reliquat de 4 — DEMO"}
	if err := db.Where("consultation_id=? AND medication_name=?", consultation.ID, name).FirstOrCreate(&prescription).Error; err != nil {
		log.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return pharmacy.MaterializeVoucher(tx, consultation.ID, &user) }); err != nil {
		log.Fatal(err)
	}
	service := pharmacy.NewService(pharmacy.NewRepository(db))
	patientID := consultation.PatientID
	if _, err := service.CreateDispensation(pharmacy.CreateDispensationRequest{PresentationID: presentation.ID, PrescriptionID: &prescription.ID, PatientID: &patientID, Quantity: 8, Notes: "Première délivrance partielle DEMO", IdempotencyKey: "DEMO-PHARMACY-PARTIAL-8"}, user); err != nil {
		log.Fatal(err)
	}
	partial, err := service.GetPrescriptionDispensationStatus(prescription.ID)
	if err != nil {
		log.Fatal(err)
	}
	partialStatus := "PARTIAL"
	if partial.IsFullyDispensed {
		partialStatus = "COMPLETED (rejeu idempotent)"
	}
	log.Printf("Pharmacy DEMO après première clé: délivré %.0f / %.0f, reste %.0f, statut %s", partial.DispensedQuantity, partial.PrescribedQuantity, partial.RemainingQuantity, partialStatus)
	if _, err := service.CreateDispensation(pharmacy.CreateDispensationRequest{PresentationID: presentation.ID, PrescriptionID: &prescription.ID, PatientID: &patientID, Quantity: 4, Notes: "Délivrance du reliquat DEMO", IdempotencyKey: "DEMO-PHARMACY-REMAINDER-4"}, user); err != nil {
		log.Fatal(err)
	}
	completed, err := service.GetPrescriptionDispensationStatus(prescription.ID)
	if err != nil {
		log.Fatal(err)
	}
	finalStatus := "PARTIAL"
	if completed.IsFullyDispensed {
		finalStatus = "COMPLETED"
	}
	log.Printf("Pharmacy DEMO final: délivré %.0f / %.0f, reste %.0f, statut %s", completed.DispensedQuantity, completed.PrescribedQuantity, completed.RemainingQuantity, finalStatus)
}

func seedFullDemoPharmacy(db *gorm.DB) {
	requireDemoDatabase(db)
	var adminID uint
	db.Table("users").Select("id").Where("email=?", "admin@medcore.local").Scan(&adminID)
	if adminID == 0 {
		log.Fatal("administrateur DEMO introuvable")
	}
	var consultation consultations.Consultation
	if err := db.Joins("JOIN patients p ON p.id=consultations.patient_id").Where("p.code_patient=?", "P-DEMO-007").First(&consultation).Error; err != nil {
		log.Fatal("consultation Pharmacy DEMO introuvable: ", err)
	}
	seedDemoPharmacyWorkflow(db, adminID, &consultation)
}

func requireDemoDatabase(db *gorm.DB) {
	var name string
	if err := db.Raw("SELECT current_database()").Scan(&name).Error; err != nil {
		log.Fatal(err)
	}
	if name != "medcore_full_demo" && name != "medcore_lot12_demo" {
		log.Fatalf("seed DEMO refusé sur la base %q", name)
	}
}
